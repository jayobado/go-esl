package goesl

import (
	"fmt"
	"net/textproto"
	"strings"
)

type Event struct {
	Headers map[string]string
	Body    string
}

func (ev *Event) Get(key string) string {
	return ev.Headers[textproto.CanonicalMIMEHeaderKey(key)]
}

func (ev *Event) IsSuccess() bool {
	reply := ev.Get("Reply-Text")
	return strings.HasPrefix(reply, "+OK")
}

func (ev *Event) IsError() bool {
	return strings.HasPrefix(ev.Get("Reply-Text"), "-ERR")
}

func (ev *Event) String() string {
	if ev.Body != "" {
		return fmt.Sprintf("Headers: %v, Body: %s", ev.Headers, ev.Body)
	}
	return fmt.Sprintf("Headers: %v", ev.Headers)
}

type EventHandler func(event *Event)