package goesl

import (
	"context"
	"time"
)

const (
	defaultTimeout         = 30 * time.Second
	defaultHost            = "127.0.0.1"
	defaultPort            = 8021
	defaultPassword        = "ClueCon"
)

type ClientOptions struct {
	Host            string
	Port            int
	Password        string
	Timeout         time.Duration
	EventBufferSize int
	OnEvent         EventHandler
	OnDisconnect    func(err error)
}

func (o *ClientOptions) withDefaults() ClientOptions {
	out := *o
	if out.Host == "" {
		out.Host = defaultHost
	}
	if out.Port == 0 {
		out.Port = defaultPort
	}
	if out.Password == "" {
		out.Password = defaultPassword
	}
	if out.Timeout == 0 {
		out.Timeout = defaultTimeout
	}
	if out.EventBufferSize == 0 {
		out.EventBufferSize = 64
	}
	return out
}

type EventHandler func(event *EslEvent)

type Client interface {
	Connect(ctx context.Context) error
	Disconnect() error
	SendCommand(ctx context.Context, command string) (*EslEvent, error)
	Done() <-chan struct{}
}