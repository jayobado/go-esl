# go-esl

A lightweight FreeSWITCH ESL (Event Socket Library) client for Go.

## Installation
```bash
go get github.com/jayobado/go-esl
```

## Usage

### Connecting
```go
import "github.com/jayobado/go-esl"

client := freeswitch.NewClient(freeswitch.ClientOptions{
    Host:     "127.0.0.1",
    Port:     8021,
    Password: "ClueCon",
    Timeout:  10 * time.Second,
})

ctx := context.Background()
if err := client.Connect(ctx); err != nil {
    log.Fatal(err)
}
defer client.Disconnect()
```

`Timeout` defaults to 30 seconds if not set. It applies to both the initial dial and to individual command responses.

### Sending commands
```go
event, err := client.SendCommand(ctx, "api status")
if err != nil {
    log.Fatal(err)
}

if event.IsSuccess() {
    fmt.Println(event.Body)
}
```

### Using a context deadline
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

event, err := client.SendCommand(ctx, "api sofia status")
if err != nil {
    // err is context.DeadlineExceeded if the deadline fired
    log.Fatal(err)
}
```

### Disconnecting
```go
if err := client.Disconnect(); err != nil {
    log.Println("disconnect error:", err)
}
```

`Disconnect` is safe to call multiple times.

## Working with events

### EslEvent

Every response from FreeSWITCH is returned as an `*EslEvent`.
```go
// Get a header value (key lookup is case-insensitive)
replyText := event.Get("Reply-Text")

// Check outcome
if event.IsSuccess() { ... }
if event.IsError()   { ... }

// Access the response body (e.g. for api commands)
fmt.Println(event.Body)

// Print the full event
fmt.Println(event.String())
```

### IsSuccess and IsError

| Method | Condition |
|--------|-----------|
| `IsSuccess()` | `Reply-Text` starts with `+OK` |
| `IsError()` | `Reply-Text` starts with `-ERR` |

## Common commands
```go
// Check FreeSWITCH status
event, err := client.SendCommand(ctx, "api status")

// List active calls
event, err := client.SendCommand(ctx, "api show calls")

// Originate a call
event, err := client.SendCommand(ctx, "api originate sofia/gateway/mygw/15551234567 &echo")

// Reload a module
event, err := client.SendCommand(ctx, "api reload mod_sofia")

// Send a background API command
event, err := client.SendCommand(ctx, "bgapi status")
```

## Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Host` | `string` | — | FreeSWITCH ESL host |
| `Port` | `int` | — | ESL port (default FreeSWITCH port is `8021`) |
| `Password` | `string` | — | ESL password (set in `event_socket.conf.xml`) |
| `Timeout` | `time.Duration` | `30s` | Dial timeout and per-command response timeout |

## Notes

- The client handles one command at a time in strict FIFO order — responses are matched to commands in the order they were sent. This matches FreeSWITCH's ESL protocol guarantee.
- Only `command/reply` and `api/response` content types are currently handled. Inbound events (`text/event-plain`, `text/event-json`) are not dispatched to handlers in this version.
- If the connection drops, all pending `SendCommand` calls unblock immediately with an error.
- `Connect` must be called again after a disconnection — the client does not reconnect automatically. Wrap `Connect` in a retry loop if you need reconnection, following the same pattern used in [go-grpc-client](https://github.com/jayobado).

## License

[MIT](LICENSE)