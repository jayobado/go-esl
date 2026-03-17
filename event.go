package goesl

import (
	"fmt"
	"net/textproto"
	"strings"
)

type ReplyStatus int

const (
	ReplyUnknown ReplyStatus = iota
	ReplyOK
	ReplyError
)

type EslEvent struct {
	Headers map[string]string
	Body    string
}

func (ev *EslEvent) Get(key string) string {
	if ev == nil {
		return ""
	}
	return ev.Headers[textproto.CanonicalMIMEHeaderKey(key)]
}

func (ev *EslEvent) Status() ReplyStatus {
	if ev == nil {
		return ReplyUnknown
	}
	replyText := ev.Get("Reply-Text")
	switch {
	case strings.HasPrefix(replyText, "+OK"), replyText == "+OK accepted":
		return ReplyOK
	case strings.HasPrefix(replyText, "-ERR"):
		return ReplyError
	default:
		return ReplyUnknown
	}
}

func (ev *EslEvent) IsSuccess() bool { return ev.Status() == ReplyOK }
func (ev *EslEvent) IsError() bool   { return ev.Status() == ReplyError }

func (ev *EslEvent) String() string {
	if ev == nil {
		return "<nil event>"
	}
	if ev.Body != "" {
		return fmt.Sprintf("Headers: %v, Body: %s", ev.Headers, ev.Body)
	}
	return fmt.Sprintf("Headers: %v", ev.Headers)
}