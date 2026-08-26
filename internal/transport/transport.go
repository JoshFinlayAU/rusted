// Package transport defines the pluggable mechanism rusted uses to reach a
// device and run commands against it. SSH is the only built-in transport, but
// additional transports (telnet, serial console servers, REST, NETCONF, ...)
// can be added by implementing Transport and calling Register.
//
// See docs/transport-modules.md for a guide to building a transport module.
package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
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
	Timeout    time.Duration // dial/connect timeout (SSH handshake, TCP connect)
	// CmdTimeout is the hard timeout for a single SendCommand call. Zero means
	// the engine default (60s). Override for slow devices that take longer to
	// emit their full config.
	CmdTimeout time.Duration
	// IdleTimeout is the idle window after which a non-prompting stream is
	// considered complete (default 700ms). Increase for devices that pause
	// mid-output for longer than 700ms.
	IdleTimeout time.Duration
	// Debug, when non-nil, receives every raw byte read from and written to
	// the device, prefixed with <<< and >>> respectively. Set by --debug (-vv).
	Debug io.Writer
	// RawOutput disables the transport's output normalisation (CRLF folding,
	// ANSI stripping). Set for drivers whose capture must stay byte-exact —
	// e.g. OpenWrt, which pulls binary files over the exec channel and lets
	// the driver post-process them.
	RawOutput bool
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

// lastLine returns the final non-empty line of b with trailing whitespace
// removed. Used for pager-prompt detection.
func lastLine(b []byte) string {
	s := strings.TrimRight(string(b), " \t\r\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// trailingMatches reports whether re matches a bare prompt at the very end of
// b: the final (unterminated) line, ignoring trailing spaces/tabs. A real CLI
// prompt is emitted WITHOUT a terminating newline — the device prints it and
// waits for input — whereas every content line ends in \n. Anchoring here
// keeps prompt-shaped content lines (banners, FortiGate replacement-message
// HTML, motd) from completing a command mid-dump.
func trailingMatches(b []byte, re *regexp.Regexp) bool {
	t := bytes.TrimRight(b, " \t")
	if len(t) == 0 || t[len(t)-1] == '\n' || t[len(t)-1] == '\r' {
		return false
	}
	line := t
	if i := bytes.LastIndexByte(t, '\n'); i >= 0 {
		line = t[i+1:]
	}
	return re.Match(line)
}

// endsWithPrompt reports whether b ends with a bare device prompt.
func endsWithPrompt(b []byte) bool { return trailingMatches(b, promptRe) }

// endsWithPrivilegedPrompt reports whether b ends with a bare "#" prompt.
func endsWithPrivilegedPrompt(b []byte) bool { return trailingMatches(b, privilegedPromptRe) }

// endsWithPasswordPrompt reports whether b ends with a bare Password:/Passphrase:
// prompt (colon required, so config text mentioning "password" cannot trigger it).
func endsWithPasswordPrompt(b []byte) bool {
	s := strings.TrimRight(string(b), " \t\r\n")
	return passwordPrompt.MatchString(s)
}
