package transport

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTelnetIsRegistered(t *testing.T) {
	tr, err := Get("telnet")
	if err != nil {
		t.Fatalf("telnet transport not registered: %v", err)
	}
	if tr.Name() != "telnet" {
		t.Fatalf("name = %q, want telnet", tr.Name())
	}
}

// fakeTelnetDevice simulates a Cisco IOS over telnet. It does NOT call
// testing.T methods directly (those would run in a goroutine and trip -vet);
// instead it records mismatches into a slice for the caller to assert.
type telnetServer struct {
	addr     string
	received []string
	mu       sync.Mutex
}

func (s *telnetServer) record(got string) {
	s.mu.Lock()
	s.received = append(s.received, got)
	s.mu.Unlock()
}

func startFakeTelnet(t *testing.T, enablePassword string) *telnetServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	s := &telnetServer{addr: ln.Addr().String()}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		handleTelnetSession(conn, s, enablePassword)
	}()
	return s
}

func handleTelnetSession(conn net.Conn, s *telnetServer, enablePassword string) {
	buf := make([]byte, 256)

	readLine := func() string {
		n, _ := conn.Read(buf)
		return strings.TrimSpace(string(buf[:n]))
	}
	write := func(data string) {
		_, _ = conn.Write([]byte(data))
	}

	// 1. Banner + username prompt
	write("User Access Verification\r\n\r\nUsername: ")
	s.record(readLine()) // "admin"

	// 2. Password prompt
	write("Password: ")
	s.record(readLine()) // "secret"

	// 3. User-exec prompt
	write("Router> ")

	// 4. "enable" command
	s.record(readLine()) // "enable"
	write("Password: ")
	s.record(readLine()) // enable password

	// 5. Privileged prompt
	write("Router# ")

	// 6. "terminal length 0"
	s.record(readLine())
	write("Router# ")

	// 7. "show running-config"
	s.record(readLine())
	write("hostname core-sw1\r\ninterface Gi0/1\r\n ip address 10.0.0.1 255.255.255.0\r\nRouter# ")
}

func TestTelnetIdleTimeoutConfigurable(t *testing.T) {
	// A slow device that pauses 500ms between lines. With the default 700ms idle
	// timeout, this would still work, but a 200ms idle would truncate output.
	// This test verifies that a custom idle timeout is respected.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		// No login prompts — send prompt immediately (no auth device).
		_, _ = conn.Write([]byte("Router# "))
		buf := make([]byte, 256)
		conn.Read(buf) // "enable"
		_, _ = conn.Write([]byte("Router# "))
		conn.Read(buf) // "terminal length 0"
		_, _ = conn.Write([]byte("Router# "))
		conn.Read(buf) // "show running-config"
		// Send config with 500ms gaps between lines.
		_, _ = conn.Write([]byte("line1\r\n"))
		time.Sleep(500 * time.Millisecond)
		_, _ = conn.Write([]byte("line2\r\n"))
		time.Sleep(500 * time.Millisecond)
		_, _ = conn.Write([]byte("Router# "))
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	tr, _ := Get("telnet")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sess, err := tr.Dial(ctx, Target{
		Host:        host,
		Port:        port,
		Timeout:     10 * time.Second,
		IdleTimeout: 1 * time.Second, // >500ms gaps, so output won't be truncated
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()

	_, err = sess.SendCommand("enable")
	if err != nil {
		t.Fatalf("SendCommand enable: %v", err)
	}
	_, err = sess.SendCommand("terminal length 0")
	if err != nil {
		t.Fatalf("SendCommand terminal length 0: %v", err)
	}
	out, err := sess.SendCommand("show running-config")
	if err != nil {
		t.Fatalf("SendCommand config: %v", err)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Fatalf("expected both lines in output, got:\n%s", out)
	}
}

func TestTelnetCmdTimeoutConfigurable(t *testing.T) {
	// Verify that a custom CmdTimeout is used (not the default 60s).
	// We test by sending a command to a device that never responds with a
	// prompt — and keeps the connection open (a device closing it must look
	// like an error, not a completed command) — and checking that the command
	// times out at the custom duration.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("Router# "))
		buf := make([]byte, 256)
		conn.Read(buf) // consume "show running-config"
		// Never send a prompt; hold the connection open well past the 2s
		// command timeout so the hard timeout fires first.
		time.Sleep(4 * time.Second)
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	tr, _ := Get("telnet")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sess, err := tr.Dial(ctx, Target{
		Host:       host,
		Port:       port,
		Timeout:    5 * time.Second,
		CmdTimeout: 2 * time.Second, // custom: should timeout after ~2s, not 60s
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()

	start := time.Now()
	_, err = sess.SendCommand("show running-config")
	if err == nil {
		t.Fatal("expected a timeout error from SendCommand")
	}
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("expected ~2s timeout, got %v", elapsed)
	}
}

// A device (or network) dropping the session mid-dump must surface as an
// error, never as a "successful" capture — silently saving truncated configs
// is exactly how partial FortiGate backups used to be committed.
func TestTelnetClosedMidCommandFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("Router# "))
		buf := make([]byte, 256)
		conn.Read(buf) // enable
		_, _ = conn.Write([]byte("Router# "))
		conn.Read(buf) // terminal length 0
		_, _ = conn.Write([]byte("Router# "))
		conn.Read(buf) // show running-config
		_, _ = conn.Write([]byte("hostname fgt\r\nconfig system global\r\n"))
		// Device dies mid-dump.
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	tr, _ := Get("telnet")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sess, err := tr.Dial(ctx, Target{
		Host:        host,
		Port:        port,
		Timeout:     5 * time.Second,
		CmdTimeout:  10 * time.Second,
		IdleTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()

	_, _ = sess.SendCommand("enable")
	_, _ = sess.SendCommand("terminal length 0")
	_, err = sess.SendCommand("show running-config")
	if err == nil {
		t.Fatal("expected an error when the session closes mid-command")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a session-closed error, got: %v", err)
	}
}

// Prompt-looking lines inside the dumped content (login banners, FortiGate
// replacement-message HTML, ...) must not complete the command early — only a
// genuine trailing prompt may.
func TestTelnetPromptLikeContentDoesNotComplete(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("Router# "))
		buf := make([]byte, 256)
		conn.Read(buf) // enable
		_, _ = conn.Write([]byte("Router# "))
		conn.Read(buf) // terminal length 0
		_, _ = conn.Write([]byte("Router# "))
		conn.Read(buf) // show running-config
		_, _ = conn.Write([]byte("set buffer \"<html>\"\r\n"))
		_, _ = conn.Write([]byte("this whole line looks like a prompt#\r\n")) // matches promptRe mid-content
		time.Sleep(700 * time.Millisecond)
		_, _ = conn.Write([]byte("more output after the lookalike\r\n"))
		time.Sleep(700 * time.Millisecond)
		_, _ = conn.Write([]byte("final line\r\nRouter# "))
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	tr, _ := Get("telnet")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sess, err := tr.Dial(ctx, Target{
		Host:        host,
		Port:        port,
		Timeout:     5 * time.Second,
		CmdTimeout:  10 * time.Second,
		IdleTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()

	_, _ = sess.SendCommand("enable")
	_, _ = sess.SendCommand("terminal length 0")
	out, err := sess.SendCommand("show running-config")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	for _, want := range []string{"lookalike", "more output after", "final line"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output ended prematurely, missing %q:\n%s", want, out)
		}
	}
}
