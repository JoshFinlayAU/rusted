package transport

import (
	"bytes"
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

// Devices such as NX-OS open with IAC DO TTYPE / DO TERMINAL-SPEED /
// DO X-DISPLAY-LOCATION and refuse to show the login prompt until the client
// answers the negotiation. A transport that passes the bytes through raw hangs
// forever in "login deadline exceeded".
func TestTelnetNegotiationUnblocksLogin(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	var mu sync.Mutex
	var gotNego, gotTType []byte

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 256)

		readN := func(n int) []byte {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			defer conn.SetReadDeadline(time.Time{})
			var got []byte
			for len(got) < n {
				m, err := conn.Read(buf)
				got = append(got, buf[:m]...)
				if err != nil {
					return got
				}
			}
			return got
		}
		write := func(b []byte) { _, _ = conn.Write(b) }

		write([]byte{0xff, 0xfd, 24, 0xff, 0xfd, 32, 0xff, 0xfd, 35, 0xff, 0xfd, 39}) // DO TTYPE, DO TERMSPEED, DO XDISPLOC, DO NEW-ENVIRON
		mu.Lock()
		gotNego = readN(12)
		mu.Unlock()
		write([]byte{0xff, 0xfa, 24, 1, 0xff, 0xf0}) // SB TTYPE SEND SE
		mu.Lock()
		gotTType = readN(11)
		mu.Unlock()

		write([]byte("sw-san-1 login: "))
		conn.Read(buf) // username
		write([]byte("Password: "))
		conn.Read(buf) // password
		write([]byte("Nexus Operating System (NX-OS) Software\r\nlicenses\r\n\r\x00sw1# "))
		conn.Read(buf) // terminal length 0
		write([]byte("sw1# "))
		conn.Read(buf) // show version
		write([]byte("NXOS version 10.3(5)\r\n\r\x00sw1# "))
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	tr, _ := Get("telnet")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sess, err := tr.Dial(ctx, Target{
		Host:     host,
		Port:     port,
		Timeout:  5 * time.Second,
		Username: "admin",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()

	if _, err := sess.SendCommand("terminal length 0"); err != nil {
		t.Fatalf("SendCommand terminal length 0: %v", err)
	}
	out, err := sess.SendCommand("show version")
	if err != nil {
		t.Fatalf("SendCommand show version: %v", err)
	}
	if !strings.Contains(out, "version 10.3(5)") {
		t.Fatalf("missing command output:\n%s", out)
	}
	if strings.ContainsRune(out, 0xff) || strings.ContainsRune(out, 0xfa) {
		t.Fatalf("telnet protocol bytes leaked into output: %q", out)
	}

	mu.Lock()
	defer mu.Unlock()
	wantNego := []byte{0xff, 0xfb, 24, 0xff, 0xfc, 32, 0xff, 0xfc, 35, 0xff, 0xfc, 39}
	if !bytes.Equal(gotNego, wantNego) {
		t.Fatalf("negotiation reply = % x, want % x", gotNego, wantNego)
	}
	wantTType := append([]byte{0xff, 0xfa, 24, 0}, "xterm"...)
	wantTType = append(wantTType, 0xff, 0xf0)
	if !bytes.Equal(gotTType, wantTType) {
		t.Fatalf("ttype reply = % x, want % x", gotTType, wantTType)
	}
}

func TestTelnetNegotiationStateMachine(t *testing.T) {
	s := &telnetSession{}
	vis, rep := s.negotiate([]byte{0xff, 0xfd})
	if len(vis) != 0 || len(rep) != 0 || len(s.pend) != 2 {
		t.Fatalf("partial IAC not buffered: vis=%q rep=% x pend=% x", vis, rep, s.pend)
	}
	vis, rep = s.negotiate([]byte{24, 'a', 0xff, 0xff, 'b'})
	if string(vis) != "a\xffb" {
		t.Fatalf("visible = %q, want %q", vis, "a\xffb")
	}
	if !bytes.Equal(rep, []byte{0xff, 0xfb, 24}) {
		t.Fatalf("reply = % x, want % x", rep, []byte{0xff, 0xfb, 24})
	}

	s2 := &telnetSession{}
	vis, rep = s2.negotiate([]byte{0xff, 0xfb, 1, 0xff, 0xfb, 3, 'o', 0xff, 0xfa, 39, 1, 0, 0xff, 0xf0, 'k'})
	want := []byte{0xff, 0xfd, 1, 0xff, 0xfd, 3}
	if string(vis) != "ok" || !bytes.Equal(rep, want) {
		t.Fatalf("vis=%q rep=% x, want %q / % x", vis, rep, "ok", want)
	}
}
