# go-esl

A production-grade FreeSWITCH ESL client for Go.

---

## Overview

goesl is a thread-safe, production-ready Go module for communicating with FreeSWITCH via its Event Socket Library (ESL). It handles connection lifecycle, authentication, command/reply pairing, and background event dispatch — with clean cancellation semantics throughout.

## Features

- Thread-safe concurrent `SendCommand` calls
- Single shutdown path — no deadlocks or double-close on disconnect
- Cancelled commands removed cleanly from queue (no swallowed replies)
- Background event dispatch via `OnEvent` callback
- Disconnect notification via `OnDisconnect` callback and `Done()` channel
- Sentinel error `ErrDisconnected` for clean programmatic handling

---

## Installation

### Option A — Local module (development)

Place the `goesl` folder alongside your project, then add a `replace` directive to your `go.mod`:
```
require github.com/user/goesl v0.0.0

replace github.com/user/goesl => ../goesl
```

Then run:
```bash
go mod tidy
```

### Option B — Published module

Once published to GitHub, import it directly:
```bash
go get github.com/you/goesl@v1.0.0
```

---

## Quick Start
```go
import "github.com/user/goesl"

cli := goesl.NewClient(goesl.ClientOptions{
    Host:     "127.0.0.1",
    Port:     8021,
    Password: "ClueCon",
    OnEvent: func(ev *goesl.EslEvent) {
        fmt.Println("event:", ev.Get("Event-Name"))
    },
    OnDisconnect: func(err error) {
        log.Println("disconnected:", err)
    },
})

if err := cli.Connect(context.Background()); err != nil {
    log.Fatal(err)
}
defer cli.Disconnect()

ev, err := cli.SendCommand(ctx, "api status")
if err != nil { log.Fatal(err) }
fmt.Println(ev.IsSuccess(), ev.Body)
```

---

## Configuration

### ClientOptions

| Field | Type | Default | Description |
|---|---|---|---|
| `Host` | `string` | `"127.0.0.1"` | FreeSWITCH ESL host |
| `Port` | `int` | `8021` | FreeSWITCH ESL port |
| `Password` | `string` | `"ClueCon"` | ESL password |
| `Timeout` | `time.Duration` | `30s` | Dial timeout and command fallback deadline |
| `EventBufferSize` | `int` | `64` | Background event channel buffer size |
| `OnEvent` | `EventHandler` | `nil` | Called for every non-reply event (heartbeats, channel events, etc.) |
| `OnDisconnect` | `func(error)` | `nil` | Called once when the connection is lost |

---

## Client Interface

| Method | Signature | Description |
|---|---|---|
| `Connect` | `Connect(ctx) error` | Dials and authenticates. Returns error on failure. |
| `Disconnect` | `Disconnect() error` | Closes connection. Unblocks all pending `SendCommand` calls. |
| `SendCommand` | `SendCommand(ctx, cmd) (*EslEvent, error)` | Sends a raw ESL command and waits for its reply. |
| `Done` | `Done() <-chan struct{}` | Channel closed when the connection is lost. |

---

## EslEvent

Every reply and background event is delivered as an `*EslEvent`.

| Method | Signature | Description |
|---|---|---|
| `Get` | `Get(key string) string` | Returns a header value by name (case-insensitive). |
| `IsSuccess` | `IsSuccess() bool` | True if `Reply-Text` starts with `+OK`. |
| `IsError` | `IsError() bool` | True if `Reply-Text` starts with `-ERR`. |
| `Status` | `Status() ReplyStatus` | Returns `ReplyOK`, `ReplyError`, or `ReplyUnknown`. |
| `String` | `String() string` | Human-readable representation for logging. |

---

## Usage Examples

### Subscribing to Events
```go
cli.SendCommand(ctx, "event plain HEARTBEAT CHANNEL_CREATE CHANNEL_DESTROY")
```

FreeSWITCH will push matching events to your `OnEvent` handler.

### Detecting Disconnects
```go
// Block until connection drops:
<-cli.Done()

// Non-blocking check:
select {
case <-cli.Done():
    fmt.Println("lost connection")
default:
}
```

### Error Handling
```go
ev, err := cli.SendCommand(ctx, "api status")
switch {
case errors.Is(err, goesl.ErrDisconnected):
    // reconnect logic
case errors.Is(err, context.DeadlineExceeded):
    // timed out
case err != nil:
    // other error
}
```

### Sending a BGapi Command
```go
ev, err := cli.SendCommand(ctx, "bgapi originate sofia/gateway/mygw/15551234567 &echo")
if err != nil {
    log.Fatal(err)
}
fmt.Println("job id:", ev.Get("Job-Uuid"))
```

---

## Architecture Notes

The client runs a single goroutine (`processLoop`) that reads all frames off the wire in order. Command replies are matched to callers using a FIFO queue of channels — one channel per pending `SendCommand` call. This mirrors FreeSWITCH's guarantee that replies arrive in the same order as commands.

Key design decisions:
- **Single shutdown path** protected by `sync.Once` prevents double-close and deadlocks.
- **Cancelled waiters** are removed from the queue with O(1) list removal, so no future reply is silently swallowed.
- **Background events** (heartbeats, channel events, disconnect notices) are routed to `OnEvent` and never interfere with the reply queue.

> **Note:** goesl does not implement automatic reconnection. Use `Done()` or `OnDisconnect` to detect drops and reconnect with your own backoff strategy.

---

## File Structure
```
goesl/
├── event.go        # EslEvent type and ReplyStatus
├── errors.go       # Sentinel errors (ErrDisconnected)
├── client.go       # Client interface and ClientOptions
├── client_impl.go  # Full implementation
└── client_test.go  # 10 tests including race detector
```
