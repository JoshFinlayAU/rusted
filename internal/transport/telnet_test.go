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

// startTelnet runs script against the first connection to a local listener
// and returns a Target pointing at it.
func startTelnet(t *testing.T, script func(conn net.Conn)) Target {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		script(conn)
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return Target{Host: host, Port: port, Timeout: 5 * time.Second}
}

func dialTelnet(t *testing.T, tgt Target) (Session, error) {
	t.Helper()
	tr, _ := Get("telnet")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	sess, err := tr.Dial(ctx, tgt)
	if err == nil {
		t.Cleanup(func() { _ = sess.Close() })
	}
	return sess, err
}

// A full Cisco IOS style exchange: login, enable with a password, show run.
// The trace must show the username but never either password.
func TestTelnetLoginAndEnable(t *testing.T) {
	var mu sync.Mutex
	var got []string
	tgt := startTelnet(t, func(c net.Conn) {
		rec := func() {
			l := readLine(c)
			mu.Lock()
			got = append(got, l)
			mu.Unlock()
		}
		c.Write([]byte("\r\nUser Access Verification\r\n\r\nUsername: "))
		rec()
		c.Write([]byte("Password: "))
		rec()
		c.Write([]byte("\r\nRouter>"))
		rec() // enable
		c.Write([]byte("Password: "))
		rec()
		c.Write([]byte("\r\nRouter#"))
		rec() // show running-config
		c.Write([]byte("hostname core-sw1\r\ninterface Gi0/1\r\n ip address 10.0.0.1 255.255.255.0\r\nRouter#"))
	})
	var trace bytes.Buffer
	tgt.Username, tgt.Password, tgt.Enable, tgt.Debug = "admin", "login-pw", "enable-pw", &trace
	sess, err := dialTelnet(t, tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := sess.SendCommand("enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	out, err := sess.SendCommand("show running-config")
	if err != nil {
		t.Fatalf("show running-config: %v", err)
	}
	want := "hostname core-sw1\ninterface Gi0/1\n ip address 10.0.0.1 255.255.255.0"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != "admin,login-pw,enable,enable-pw,show running-config" {
		t.Fatalf("device received %q", got)
	}
	tr := trace.String()
	if strings.Contains(tr, "login-pw") || strings.Contains(tr, "enable-pw") {
		t.Fatalf("password leaked into the debug trace:\n%s", tr)
	}
	for _, w := range []string{">>> admin", ">>> [password]", ">>> [enable password]", "<<< hostname core-sw1"} {
		if !strings.Contains(tr, w) {
			t.Fatalf("trace missing %q:\n%s", w, tr)
		}
	}
}

// A banner line ending in "log in:" is not a username prompt; the real
// "Username:" that follows is.
func TestTelnetBannerDoesNotTriggerUsername(t *testing.T) {
	var mu sync.Mutex
	var got []string
	tgt := startTelnet(t, func(c net.Conn) {
		c.Write([]byte("Authorised staff only. Please log in:"))
		time.Sleep(300 * time.Millisecond)
		c.Write([]byte("\r\n\r\nUsername: "))
		u := readLine(c)
		c.Write([]byte("Password: "))
		p := readLine(c)
		mu.Lock()
		got = append(got, u, p)
		mu.Unlock()
		c.Write([]byte("\r\nsw1#"))
		readLine(c)
	})
	tgt.Username, tgt.Password = "admin", "pw"
	if _, err := dialTelnet(t, tgt); err != nil {
		t.Fatalf("dial: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != "admin,pw" {
		t.Fatalf("device received %q", got)
	}
}

// A password-only device that re-prompts has rejected the password: fail at
// once instead of resending it on every prompt.
func TestTelnetPasswordOnlyRejectedFailsOnSecondPrompt(t *testing.T) {
	var mu sync.Mutex
	sent := 0
	tgt := startTelnet(t, func(c net.Conn) {
		for i := 0; i < 3; i++ {
			c.Write([]byte("\r\nPassword: "))
			if readLine(c) == "" {
				return
			}
			mu.Lock()
			sent++
			mu.Unlock()
		}
	})
	tgt.Password = "wrong"
	_, err := dialTelnet(t, tgt)
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("want a password-rejected error, got %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if sent != 1 {
		t.Fatalf("password sent %d times, want 1", sent)
	}
}

// The login wait follows the configured command timeout, not a fixed one.
func TestTelnetLoginHonoursCmdTimeout(t *testing.T) {
	tgt := startTelnet(t, func(c net.Conn) {
		c.Write([]byte("connecting..."))
		time.Sleep(5 * time.Second)
	})
	tgt.Username, tgt.Password, tgt.CmdTimeout = "admin", "pw", time.Second
	start := time.Now()
	_, err := dialTelnet(t, tgt)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a login timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("login wait ignored CmdTimeout: %v", elapsed)
	}
}

func TestTelnetIdleTimeoutConfigurable(t *testing.T) {
	tgt := startTelnet(t, func(c net.Conn) {
		c.Write([]byte("Router# "))
		readLine(c) // show running-config
		c.Write([]byte("line1\r\n"))
		time.Sleep(500 * time.Millisecond)
		c.Write([]byte("line2\r\n"))
		time.Sleep(500 * time.Millisecond)
		c.Write([]byte("Router# "))
	})
	tgt.IdleTimeout = time.Second // longer than the 500ms gaps
	sess, err := dialTelnet(t, tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	out, err := sess.SendCommand("show running-config")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if out != "line1\nline2" {
		t.Fatalf("output = %q", out)
	}
}

// A device that never answers fails at the configured command timeout.
func TestTelnetCmdTimeoutConfigurable(t *testing.T) {
	tgt := startTelnet(t, func(c net.Conn) {
		c.Write([]byte("Router# "))
		readLine(c)
		time.Sleep(4 * time.Second)
	})
	tgt.CmdTimeout = time.Second
	sess, err := dialTelnet(t, tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	start := time.Now()
	_, err = sess.SendCommand("show running-config")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("expected ~1s timeout, got %v", elapsed)
	}
}

// The connection dropping mid-dump is an error, never a truncated capture.
func TestTelnetClosedMidCommandFails(t *testing.T) {
	tgt := startTelnet(t, func(c net.Conn) {
		c.Write([]byte("Router# "))
		readLine(c)
		c.Write([]byte("hostname fgt\r\nconfig system global\r\n"))
	})
	tgt.IdleTimeout = 2 * time.Second
	sess, err := dialTelnet(t, tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_, err = sess.SendCommand("show running-config")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("want a session-closed error, got %v", err)
	}
}

// Devices such as NX-OS open with IAC DO TTYPE / TERMINAL-SPEED / ... and
// show no login prompt until the client answers. Protocol bytes must not leak
// into the output.
func TestTelnetNegotiationUnblocksLogin(t *testing.T) {
	var mu sync.Mutex
	var gotNego, gotTType []byte
	tgt := startTelnet(t, func(c net.Conn) {
		readN := func(n int) []byte {
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			defer c.SetReadDeadline(time.Time{})
			buf := make([]byte, 256)
			var got []byte
			for len(got) < n {
				m, err := c.Read(buf)
				got = append(got, buf[:m]...)
				if err != nil {
					return got
				}
			}
			return got
		}
		c.Write([]byte{0xff, 0xfd, 24, 0xff, 0xfd, 32, 0xff, 0xfd, 35, 0xff, 0xfd, 39})
		nego := readN(12)
		c.Write([]byte{0xff, 0xfa, 24, 1, 0xff, 0xf0}) // SB TTYPE SEND SE
		ttype := readN(11)
		mu.Lock()
		gotNego, gotTType = nego, ttype
		mu.Unlock()

		c.Write([]byte("sw-san-1 login: "))
		readLine(c)
		c.Write([]byte("Password: "))
		readLine(c)
		c.Write([]byte("Nexus Operating System (NX-OS) Software\r\nlicenses\r\n\r\x00sw1# "))
		readLine(c)
		c.Write([]byte("NXOS version 10.3(5)\r\n\r\x00sw1# "))
	})
	tgt.Username, tgt.Password = "admin", "secret"
	sess, err := dialTelnet(t, tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	out, err := sess.SendCommand("show version")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if out != "NXOS version 10.3(5)" {
		t.Fatalf("output = %q", out)
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
