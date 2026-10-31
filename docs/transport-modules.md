# Writing a transport module

A **transport** is how rusted reaches a device and exchanges commands with it.
Three are built in:

| Name | How it works |
|---|---|
| `ssh` | Interactive PTY shell; output read until the prompt returns (the default). |
| `ssh-exec` | One exec channel per command, read to EOF (MikroTik RouterOS). |
| `telnet` | Raw TCP with option negotiation and an in-band username/password login, then driven like `ssh`. |

Everything else (a serial console server, a REST or NETCONF endpoint, a vendor
cloud API) can be added by implementing one small interface and registering
it.

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
    Name        string
    Host        string
    Port        int
    Username    string
    Password    string
    PrivateKey  []byte        // optional PEM key
    Enable      string        // optional privileged-mode password
    Timeout     time.Duration // connect timeout
    CmdTimeout  time.Duration // hard limit per command (0 = 60s)
    IdleTimeout time.Duration // quiet period that ends an unrecognised prompt (0 = 700ms)
    Debug       io.Writer     // I/O trace for `backup run --debug` (nil = off)
}
```

`CmdTimeout` and `IdleTimeout` come from the device (`rusted device add
--cmd-timeout SECONDS --idle-timeout MS`, or `cmd_timeout` / `idle_timeout` on
`POST /api/devices`). If you honour `Debug`, write device output as `<<< ...`
and what you send as `>>> ...`, and **never write a password to it**: print a
placeholder such as `[password]` instead.

## Steps

1. **Create a file** under `internal/transport/`, e.g. `telnet.go`, in
   `package transport`.

2. **Implement `Transport`.** `Dial` opens the connection, performs login, and
   returns a `Session`. Keep any per-connection state (sockets, buffers,
   detected prompt) on your session struct.

3. **Implement `Session`.** `SendCommand` should write the command, then read
   until the device is done. For a CLI-style transport, reuse the shared
   `shell` in `interactive.go` (as `ssh` and `telnet` do): your reader hands
   output to `shell.deliver`, you supply a write function, and you get
   `SendCommand` with these rules:
   - A **bare prompt** as the last line (no trailing newline, ANSI escapes
     ignored) completes the command at once. Prompt-shaped lines inside the
     output end in a newline and so never do.
   - Otherwise, once the device has replied (anything beyond the echo of the
     command), the stream going **idle** for `IdleTimeout` (700 ms by default)
     completes it. This fallback is what copes with prompts the regex does not
     know.
   - A bare `--More--` style pager prompt is answered with a space and cut out
     of the capture, along with the device's erase sequence.
   - A bare `Password:` prompt (e.g. after `enable`) gets `Target.Enable`; if
     none is set the command fails rather than typing the next command in as a
     password.
   - The session closing mid-output, or `CmdTimeout` passing, is an error,
     never a truncated capture.

4. **Register it** from an `init()` so it is available everywhere the binary is
   used:

   ```go
   func init() { Register(&Telnet{}) }
   ```

5. **Select it.** The backup engine picks a transport by name: the device's
   own `transport` field, else the driver's preferred one, else
   `Engine.Transport` (default `"ssh"`).

## Minimal skeleton

A CLI transport over some byte stream (`telnet.go` is the full version):

```go
package transport

import (
    "context"
    "net"
    "time"
)

func init() { Register(&Console{}) }

type Console struct{}

func (*Console) Name() string { return "console" }

type consoleSession struct {
    *shell // SendCommand, prompt detection, idle fallback
    conn net.Conn
}

func (*Console) Dial(ctx context.Context, t Target) (Session, error) {
    var d net.Dialer
    conn, err := d.DialContext(ctx, "tcp", t.Host+":7001")
    if err != nil {
        return nil, err
    }
    s := &consoleSession{conn: conn}
    s.shell = newShell(t, "\r\n", func(p string) error {
        _, err := conn.Write([]byte(p))
        return err
    })
    go s.reader()
    s.drain("", 10*time.Second) // swallow the banner
    return s, nil
}

func (s *consoleSession) reader() {
    buf := make([]byte, 8192)
    for {
        n, err := s.conn.Read(buf)
        if n > 0 && !s.deliver(append([]byte(nil), buf[:n]...)) {
            return // session closed
        }
        if err != nil {
            close(s.out)
            return
        }
    }
}

func (s *consoleSession) Close() error {
    s.stop() // releases a reader blocked in deliver
    return s.conn.Close()
}
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
