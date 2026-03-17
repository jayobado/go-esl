package goesl

import (
	"bufio"
	"container/list"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"sync"
)

type waiter struct {
	ch   chan *EslEvent
	elem *list.Element
}

type clientImpl struct {
	opts      ClientOptions
	address   string
	mu        sync.Mutex
	conn      net.Conn
	waitQueue *list.List
	done      chan struct{}
	closeOnce sync.Once
}

func NewClient(opts ClientOptions) Client {
	opts = opts.withDefaults()
	return &clientImpl{
		opts:      opts,
		address:   fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		waitQueue: list.New(),
		done:      make(chan struct{}),
	}
}

func (c *clientImpl) Done() <-chan struct{} {
	return c.done
}

func (c *clientImpl) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dialer := &net.Dialer{Timeout: c.opts.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return fmt.Errorf("goesl: dial %s: %w", c.address, err)
	}

	reader := textproto.NewReader(bufio.NewReader(conn))

	authHeaders, err := reader.ReadMIMEHeader()
	if err != nil {
		conn.Close()
		return fmt.Errorf("goesl: read auth/request: %w", err)
	}
	if authHeaders.Get("Content-Type") != "auth/request" {
		conn.Close()
		return fmt.Errorf("goesl: unexpected content-type %q during auth", authHeaders.Get("Content-Type"))
	}

	if _, err := fmt.Fprintf(conn, "auth %s\r\n\r\n", c.opts.Password); err != nil {
		conn.Close()
		return fmt.Errorf("goesl: send auth: %w", err)
	}

	replyHeaders, err := reader.ReadMIMEHeader()
	if err != nil {
		conn.Close()
		return fmt.Errorf("goesl: read auth reply: %w", err)
	}
	if reply := replyHeaders.Get("Reply-Text"); reply != "+OK accepted" {
		conn.Close()
		return fmt.Errorf("goesl: authentication failed: %s", reply)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	go c.processLoop(reader)
	return nil
}

func (c *clientImpl) Disconnect() error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil
	}
	// Closing the conn causes processLoop's next read to fail,
	// which triggers shutdown() — the single cleanup path.
	return conn.Close()
}

func (c *clientImpl) SendCommand(ctx context.Context, command string) (*EslEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ch := make(chan *EslEvent, 1)
	w := &waiter{ch: ch}

	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, ErrDisconnected
	}
	w.elem = c.waitQueue.PushBack(w)
	_, writeErr := fmt.Fprintf(c.conn, "%s\r\n\r\n", command)
	c.mu.Unlock()

	if writeErr != nil {
		c.mu.Lock()
		c.waitQueue.Remove(w.elem)
		c.mu.Unlock()
		return nil, fmt.Errorf("goesl: write command: %w", writeErr)
	}

	select {
	case ev, ok := <-ch:
		if !ok {
			return nil, ErrDisconnected
		}
		return ev, nil
	case <-ctx.Done():
		c.mu.Lock()
		c.waitQueue.Remove(w.elem)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrDisconnected
	}
}

func (c *clientImpl) processLoop(reader *textproto.Reader) {
	var lastErr error

	for {
		headers, err := reader.ReadMIMEHeader()
		if err != nil {
			lastErr = err
			break
		}

		contentType := headers.Get("Content-Type")
		body, err := readBody(reader.R, headers.Get("Content-Length"))
		if err != nil {
			lastErr = err
			break
		}

		ev := buildEvent(headers, body)

		switch contentType {
		case "command/reply", "api/response":
			c.dispatchReply(ev)
		case "text/disconnect-notice":
			c.dispatchEvent(ev)
			lastErr = io.EOF
			goto done
		default:
			c.dispatchEvent(ev)
		}
	}

done:
	c.shutdown(lastErr)
}

func (c *clientImpl) dispatchReply(ev *EslEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for c.waitQueue.Len() > 0 {
		front := c.waitQueue.Front()
		c.waitQueue.Remove(front)
		w := front.Value.(*waiter)

		select {
		case w.ch <- ev:
			return
		default:
			// Ghost waiter (already cancelled), try next.
		}
	}
}

func (c *clientImpl) dispatchEvent(ev *EslEvent) {
	if c.opts.OnEvent != nil {
		c.opts.OnEvent(ev)
	}
}

func (c *clientImpl) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		conn := c.conn
		c.conn = nil
		for c.waitQueue.Len() > 0 {
			front := c.waitQueue.Front()
			c.waitQueue.Remove(front)
			close(front.Value.(*waiter).ch)
		}
		c.mu.Unlock()

		if conn != nil {
			conn.Close()
		}
		close(c.done)

		if c.opts.OnDisconnect != nil {
			c.opts.OnDisconnect(err)
		}
	})
}

func readBody(r *bufio.Reader, contentLengthStr string) (string, error) {
	if contentLengthStr == "" {
		return "", nil
	}
	length, err := strconv.Atoi(contentLengthStr)
	if err != nil || length <= 0 {
		return "", nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", fmt.Errorf("goesl: read body: %w", err)
	}
	return string(data), nil
}

func buildEvent(headers textproto.MIMEHeader, body string) *EslEvent {
	ev := &EslEvent{
		Headers: make(map[string]string, len(headers)),
		Body:    body,
	}
	for key, values := range headers {
		if len(values) > 0 {
			ev.Headers[key] = values[0]
		}
	}
	if ev.Body == "" {
		ev.Body = ev.Get("Reply-Text")
	}
	return ev
}