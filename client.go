package esl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"sync"
	"time"
)

type Options struct {
	Host     string
	Port     int
	Password string
	Timeout  time.Duration
}

type Client interface {
	Connect(ctx context.Context) error
	Disconnect() error
	SendCommand(ctx context.Context, command string) (*Event, error)
}

type waiter struct {
	ch        chan Event
	cancelled <-chan struct{}
}

type client struct {
	address    string
	password   string
	timeout    time.Duration
	connection net.Conn
	mu         sync.Mutex
	waitQueue  []waiter
	closed     bool
}

func NewClient(opts Options) Client {
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	return &client{
		address:  fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		password: opts.Password,
		timeout:  opts.Timeout,
	}
}

func (cli *client) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	conn, err := net.DialTimeout("tcp", cli.address, cli.timeout)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cli.address, err)
	}

	reader := textproto.NewReader(bufio.NewReader(conn))

	// Expect auth/request
	headers, err := reader.ReadMIMEHeader()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read initial headers: %w", err)
	}
	if headers.Get("Content-Type") != "auth/request" {
		conn.Close()
		return fmt.Errorf("unexpected content-type %q during handshake", headers.Get("Content-Type"))
	}

	// Send auth
	if _, err := fmt.Fprintf(conn, "auth %s\r\n\r\n", cli.password); err != nil {
		conn.Close()
		return fmt.Errorf("send auth: %w", err)
	}

	// Read auth response
	resp, err := reader.ReadMIMEHeader()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read auth response: %w", err)
	}
	if reply := resp.Get("Reply-Text"); reply != "+OK accepted" {
		conn.Close()
		return fmt.Errorf("authentication failed: %s", reply)
	}

	cli.mu.Lock()
	cli.connection = conn
	cli.closed = false
	cli.mu.Unlock()

	go cli.processLoop(reader)

	return nil
}

func (cli *client) Disconnect() error {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	return cli.disconnectLocked()
}


func (cli *client) disconnectLocked() error {
	if cli.closed {
		return nil
	}
	cli.closed = true

	var closeErr error
	if cli.connection != nil {
		closeErr = cli.connection.Close()
		cli.connection = nil
	}

	for _, w := range cli.waitQueue {
		close(w.ch)
	}
	cli.waitQueue = nil

	if closeErr != nil {
		return fmt.Errorf("close connection: %w", closeErr)
	}
	return nil
}

func (cli *client) processLoop(reader *textproto.Reader) {
	for {
		header, err := reader.ReadMIMEHeader()
		if err != nil {
			cli.mu.Lock()
			cli.disconnectLocked()
			cli.mu.Unlock()
			return
		}

		contentType := header.Get("Content-Type")
		if contentType != "api/response" && contentType != "command/reply" {
			continue
		}

		body := ""
		if lengthStr := header.Get("Content-Length"); lengthStr != "" {
			if length, err := strconv.Atoi(lengthStr); err == nil && length > 0 {
				data := make([]byte, length)
				if _, err := io.ReadFull(reader.R, data); err != nil {
					cli.mu.Lock()
					cli.disconnectLocked()
					cli.mu.Unlock()
					return
				}
				body = string(data)
			}
		}
		if body == "" {
			body = header.Get("Reply-Text")
		}

		event := &Event{
			Headers: make(map[string]string),
			Body:    body,
		}
		for key, values := range header {
			if len(values) > 0 {
				event.Headers[key] = values[0]
			}
		}

		cli.dispatch(event)
	}
}

func (cli *client) dispatch(event *Event) {
	cli.mu.Lock()
	defer cli.mu.Unlock()

	for len(cli.waitQueue) > 0 {
		w := cli.waitQueue[0]
		cli.waitQueue = cli.waitQueue[1:]

		select {
		case <-w.cancelled:
			continue
		default:
		}

		w.ch <- *event
		return
	}
}

func (cli *client) SendCommand(ctx context.Context, command string) (*Event, error) {
	ch := make(chan Event, 1)
	w := waiter{ch: ch, cancelled: ctx.Done()}

	cli.mu.Lock()
	if cli.connection == nil || cli.closed {
		cli.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	cli.waitQueue = append(cli.waitQueue, w)
	_, err := fmt.Fprintf(cli.connection, "%s\r\n\r\n", command)
	cli.mu.Unlock()

	if err != nil {
		// Remove our waiter since we never sent the command
		cli.removeWaiter(ch)
		return nil, fmt.Errorf("send command: %w", err)
	}

	select {
	case res, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("disconnected while waiting for response")
		}
		return &res, nil
	case <-ctx.Done():
		// Leave the waiter in the queue — dispatch will skip it via cancelled check
		return nil, ctx.Err()
	case <-time.After(cli.timeout):
		cli.removeWaiter(ch)
		return nil, fmt.Errorf("command timed out after %s", cli.timeout)
	}
}

// removeWaiter removes a specific channel from the wait queue.
func (cli *client) removeWaiter(ch chan Event) {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	for i, w := range cli.waitQueue {
		if w.ch == ch {
			cli.waitQueue = append(cli.waitQueue[:i], cli.waitQueue[i+1:]...)
			return
		}
	}
}