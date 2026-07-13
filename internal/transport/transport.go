// Package transport defines the pluggable mechanism rusted uses to reach a
// device and run commands against it. SSH is the default transport (routeros-api also
// ships for MikroTik's binary API), but
// additional transports (telnet, serial console servers, REST, NETCONF, ...)
// can be added by implementing Transport and calling Register.
//
// See docs/transport-modules.md for a guide to building a transport module.
package transport

import (
	"context"
	"fmt"
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
	PrivateKey []byte // optional PEM private key
	Enable     string // optional privileged-mode password
	Timeout    time.Duration
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
