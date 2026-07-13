# Writing a transport module

A **transport** is how rusted reaches a device and exchanges commands with it.
SSH is built in; everything else (telnet, a serial console server, a REST or
NETCONF endpoint, a vendor cloud API) can be added by implementing one small
interface and registering it.

Transports are decoupled from **drivers**. A driver knows *what* to type
(`show running-config`, `/export terse`); a transport knows *how* to deliver
those keystrokes and read the reply. Any driver works over any transport.

## The interface

From `internal/transport/transport.go`:

```go
type Session interface {
    // SendCommand writes cmd to the device, waits for it to complete, and
    // returns the output with the echoed command and trailing prompt removed.
    SendCommand(cmd string) (string, error)
    Close() error
}

type Transport interface {
    Name() string                                       // unique selector, e.g. "telnet"
    Dial(ctx context.Context, t Target) (Session, error)
}
```

`Target` carries everything needed to connect and authenticate:

```go
type Target struct {
    Name       string
    Host       string
    Port       int
    Username   string
    Password   string
    PrivateKey []byte        // optional PEM key
    Enable     string        // optional privileged-mode password
    Timeout    time.Duration
}
```

## Steps

1. **Create a file** under `internal/transport/`, e.g. `telnet.go`, in
   `package transport`.

2. **Implement `Transport`.** `Dial` opens the connection, performs login, and
   returns a `Session`. Keep any per-connection state (sockets, buffers,
   detected prompt) on your session struct.

3. **Implement `Session`.** `SendCommand` should write the command, then read
   until the device is done. The built-in SSH transport reads until the device
   prompt reappears *or* the stream goes idle for ~700 ms — the idle fallback is
   what makes it robust against unknown prompts. Reuse that strategy.

4. **Register it** from an `init()` so it is available everywhere the binary is
   used:

   ```go
   func init() { Register(&Telnet{}) }
   ```

5. **Select it.** The backup engine asks for a transport by name
   (`Engine.Transport`, default `"ssh"`). Wire your transport name through
   wherever you want it used.

## Minimal skeleton

```go
package transport

import "context"

func init() { Register(&Telnet{}) }

type Telnet struct{}

func (*Telnet) Name() string { return "telnet" }

func (*Telnet) Dial(ctx context.Context, t Target) (Session, error) {
    // dial t.Host:t.Port, send t.Username / t.Password, detect the prompt...
    return &telnetSession{ /* ... */ }, nil
}

type telnetSession struct{ /* conn, reader, prompt ... */ }

func (s *telnetSession) SendCommand(cmd string) (string, error) {
    // write cmd + "\n", read until prompt or idle, strip echo + prompt
    return "", nil
}

func (s *telnetSession) Close() error { return nil }
```

## Tips

- **Disable paging via the driver, not the transport.** Drivers emit `Init`
  commands (`terminal length 0`, `set cli screen-length 0`) for exactly this.
- **Normalise line endings** to `\n` and strip ANSI escapes in your output —
  see `cleanOutput`/`ansiRe` in `ssh.go` for a reference implementation.
- **Honour `ctx`** for the connect phase so a hung device cannot stall a
  whole `--all` run.
- **Privileged mode:** if a platform needs an enable step, prefer doing it from
  the driver's `Init` commands; use `Target.Enable` only when the transport
  itself must handle an interactive enable prompt.
