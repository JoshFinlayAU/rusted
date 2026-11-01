// Package transport defines the pluggable mechanism rusted uses to reach a
// device and run commands against it. SSH (interactive and exec) and telnet
// are built in; additional transports (serial console servers, REST, NETCONF,
// ...) can be added by implementing Transport and calling Register.
//
// See docs/transport-modules.md for a guide to building a transport module.
package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// Target describes how and with what credentials to reach a device.
type Target struct {
	Name       string
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey []byte        // optional PEM private key
	Enable     string        // optional privileged-mode password
	Timeout    time.Duration // connect timeout (TCP dial, SSH handshake)
	// CmdTimeout is the hard limit on one SendCommand of an interactive
	// session (ssh, telnet). Zero means DefaultCmdTimeout.
	CmdTimeout time.Duration
	// IdleTimeout is how long an interactive session's output may stay quiet,
	// once the device has replied, before a command whose prompt was not
	// recognised counts as finished. Zero means DefaultIdleTimeout.
	IdleTimeout time.Duration
	// Debug, when non-nil, receives a trace of the session: device output
	// prefixed "<<< " and what was sent prefixed ">>> ". Passwords are never
	// written to it.
	Debug io.Writer
	// RawOutput asks the transport to return command output byte-exact, with
	// no CRLF folding or ANSI stripping, for drivers that capture binary data
	// (driver.BinarySafe). Only ssh-exec honours it.
	RawOutput bool
}

// Defaults for the per-device timeouts of interactive sessions.
const (
	DefaultCmdTimeout  = 60 * time.Second
	DefaultIdleTimeout = 700 * time.Millisecond
)

func cmdTimeout(t Target) time.Duration {
	if t.CmdTimeout > 0 {
		return t.CmdTimeout
	}
	return DefaultCmdTimeout
}

func idleTimeout(t Target) time.Duration {
	if t.IdleTimeout > 0 {
		return t.IdleTimeout
	}
	return DefaultIdleTimeout
}

// tracef writes one line of a Target.Debug trace; a nil w is silent.
func tracef(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}

// lockedWriter serialises writes to a debug trace that is fed from both a
// session's reader goroutine and its caller.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// syncTrace wraps a Target.Debug writer for concurrent use; nil stays nil.
func syncTrace(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	return &lockedWriter{w: w}
}

// Session is an open interactive connection to a device.
type Session interface {
	// SendCommand writes cmd to the device, waits for the command to complete
	// (prompt returns or the device goes idle), and returns the output with the
	// echoed command and trailing prompt stripped.
	SendCommand(cmd string) (string, error)
	// Close terminates the session and releases resources.
	Close() error
}

// Executor is an optional Session capability for running one ad-hoc command
// with bounded output. SendCommand is built for backups, where the whole config
// must come back and a non-zero exit after a good read is tolerated; Exec instead
// stops when ctx ends or the output passes limit bytes, and reports every
// failure alongside whatever output arrived first.
type Executor interface {
	Exec(ctx context.Context, cmd string, limit int) (string, error)
}

// ErrOutputLimit is returned by Exec when a command wrote more than the limit.
var ErrOutputLimit = errors.New("output limit exceeded")

// Transport opens sessions to devices over a particular protocol.
type Transport interface {
	// Name is the unique identifier used to select this transport.
	Name() string
	// Dial establishes a Session to the target. The caller owns Close.
	Dial(ctx context.Context, t Target) (Session, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Transport{}
)

// Register makes a transport available by name. It panics on duplicate
// registration, which can only happen at init time.
func Register(t Transport) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[t.Name()]; dup {
		panic("transport: duplicate registration of " + t.Name())
	}
	registry[t.Name()] = t
}

// Unregister removes a transport, so tests can register a fake per run.
func Unregister(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(registry, name)
}

// Get returns the named transport.
func Get(name string) (Transport, error) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("transport %q is not registered", name)
	}
	return t, nil
}

// Names lists the registered transport names, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
