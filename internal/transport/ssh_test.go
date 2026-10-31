package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startShellServer runs a minimal SSH server that accepts any password and
// drives each interactive shell through script. It returns host:port.
func startShellServer(t *testing.T, script func(ch ssh.Channel)) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host key signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, chans, reqs, err := ssh.NewServerConn(c, cfg)
			if err != nil {
				c.Close()
				continue
			}
			go ssh.DiscardRequests(reqs)
			for nch := range chans {
				if nch.ChannelType() != "session" {
					nch.Reject(ssh.UnknownChannelType, "unsupported")
					continue
				}
				ch, chReqs, err := nch.Accept()
				if err != nil {
					continue
				}
				go func() {
					defer ch.Close()
					for req := range chReqs {
						ok := req.Type == "pty-req" || req.Type == "shell"
						if req.WantReply {
							req.Reply(ok, nil)
						}
						if req.Type == "shell" {
							go script(ch)
						}
					}
				}()
			}
		}
	}()
	return ln.Addr().String()
}

// readLine reads one write from the client (commands arrive in one packet).
func readLine(ch io.Reader) string {
	b := make([]byte, 256)
	n, _ := ch.Read(b)
	return strings.TrimSpace(string(b[:n]))
}

// dialShell opens an interactive ssh session to addr with fast timeouts.
func dialShell(t *testing.T, addr string, mod func(*Target)) Session {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	tgt := Target{
		Host:        host,
		Port:        port,
		Username:    "admin",
		Password:    "secret",
		Timeout:     5 * time.Second,
		CmdTimeout:  10 * time.Second,
		IdleTimeout: time.Second,
	}
	if mod != nil {
		mod(&tgt)
	}
	tr, _ := Get("ssh")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	sess, err := tr.Dial(ctx, tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// A device dropping the session partway through a dump must fail the command,
// not save a truncated config as a good backup.
func TestSSHSessionClosedMidCommandFails(t *testing.T) {
	addr := startShellServer(t, func(ch ssh.Channel) {
		io.WriteString(ch, "FGT # ")
		readLine(ch)
		io.WriteString(ch, "#config-version=FG120G\nconfig system global\n")
		ch.CloseWrite()
	})
	sess := dialShell(t, addr, nil)
	_, err := sess.SendCommand("show full-configuration")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("want a session-closed error, got %v", err)
	}
}

// Prompt-shaped lines inside the dump must not complete the command early;
// only a bare trailing prompt may.
func TestSSHSessionPromptLikeContentDoesNotComplete(t *testing.T) {
	addr := startShellServer(t, func(ch ssh.Channel) {
		io.WriteString(ch, "FGT # ")
		readLine(ch)
		io.WriteString(ch, "set buffer \"<html>lang=en</html>\"\n")
		io.WriteString(ch, "this whole line looks like a prompt#\n")
		time.Sleep(500 * time.Millisecond)
		io.WriteString(ch, "more output after the lookalike\n")
		time.Sleep(500 * time.Millisecond)
		io.WriteString(ch, "final line\nFGT # ")
	})
	sess := dialShell(t, addr, nil)
	out, err := sess.SendCommand("show full-configuration")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	for _, want := range []string{"looks like a prompt#", "more output after", "final line"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output ended prematurely, missing %q:\n%s", want, out)
		}
	}
}

// A prompt wrapped in escape sequences (bash's bracketed-paste toggle, a
// window title) must still complete the command at once, not after the idle
// timeout.
func TestSSHSessionANSIPromptCompletesImmediately(t *testing.T) {
	prompt := "\x1b]0;admin@host: ~\x07\x1b[?2004hadmin@host:~$ "
	addr := startShellServer(t, func(ch ssh.Channel) {
		io.WriteString(ch, prompt)
		readLine(ch)
		io.WriteString(ch, "\x1b[?2004l\rhostname core-sw1\r\n"+prompt)
	})
	sess := dialShell(t, addr, func(t *Target) { t.IdleTimeout = 5 * time.Second })
	start := time.Now()
	out, err := sess.SendCommand("cat /etc/hostname")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("ANSI-wrapped prompt not recognised: took %v", elapsed)
	}
	if strings.TrimSpace(out) != "hostname core-sw1" {
		t.Fatalf("output = %q", out)
	}
}

// A prompt the regex does not know still completes once the output goes
// quiet, instead of waiting for the hard timeout.
func TestSSHSessionUnknownPromptCompletesOnIdle(t *testing.T) {
	addr := startShellServer(t, func(ch ssh.Channel) {
		io.WriteString(ch, "weird% ")
		readLine(ch)
		io.WriteString(ch, "config line\r\nweird% ")
	})
	sess := dialShell(t, addr, func(t *Target) { t.IdleTimeout = 300 * time.Millisecond })
	start := time.Now()
	out, err := sess.SendCommand("show config")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("idle fallback did not complete the command: took %v", elapsed)
	}
	if !strings.Contains(out, "config line") {
		t.Fatalf("output = %q", out)
	}
}

// The echo alone is not a reply: a device that takes longer than the idle
// window to start its output (NX-OS assembling show running-config) must not
// be cut off right after echoing the command.
func TestSSHSessionSlowStartAfterEcho(t *testing.T) {
	addr := startShellServer(t, func(ch ssh.Channel) {
		io.WriteString(ch, "sw1# ")
		cmd := readLine(ch)
		io.WriteString(ch, cmd+"\r\n")
		time.Sleep(800 * time.Millisecond)
		io.WriteString(ch, "hostname sw1\r\nsw1# ")
	})
	sess := dialShell(t, addr, func(t *Target) { t.IdleTimeout = 300 * time.Millisecond })
	out, err := sess.SendCommand("show running-config")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if out != "hostname sw1" {
		t.Fatalf("output = %q", out)
	}
}

// A trailing --More-- is answered with a space and neither the pager prompt
// nor its backspace erase sequence ends up in the capture.
func TestSSHSessionPagerIsAdvancedAndStripped(t *testing.T) {
	var mu sync.Mutex
	var got []string
	addr := startShellServer(t, func(ch ssh.Channel) {
		io.WriteString(ch, "R# ")
		readLine(ch)
		io.WriteString(ch, "interface Gi0/1\r\n description uplink --More-- here\r\n --More-- ")
		b := make([]byte, 16)
		n, _ := ch.Read(b)
		mu.Lock()
		got = append(got, string(b[:n]))
		mu.Unlock()
		io.WriteString(ch, "\b\b\b\b\b\b\b\b\b\b          \b\b\b\b\b\b\b\b\b\b ip address 10.0.0.1 255.255.255.0\r\n<--- More --->")
		n, _ = ch.Read(b)
		mu.Lock()
		got = append(got, string(b[:n]))
		mu.Unlock()
		io.WriteString(ch, "\r              \rend\r\nR# ")
	})
	sess := dialShell(t, addr, nil)
	out, err := sess.SendCommand("show running-config")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	want := "interface Gi0/1\n description uplink --More-- here\n ip address 10.0.0.1 255.255.255.0\nend"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != " " || got[1] != " " {
		t.Fatalf("pager answers = %q, want two spaces", got)
	}
}

// fakeEnableDevice lands in user-exec mode and asks for a password on
// "enable"; it accepts pw and reports what it was sent.
func fakeEnableDevice(pw string, sent chan<- string) func(ssh.Channel) {
	return func(ch ssh.Channel) {
		io.WriteString(ch, "R> ")
		readLine(ch) // enable
		for i := 0; i < 3; i++ {
			io.WriteString(ch, "Password: ")
			got := readLine(ch)
			sent <- got
			if got == pw {
				io.WriteString(ch, "\r\nR# ")
				readLine(ch)
				io.WriteString(ch, "hostname R\r\nR# ")
				return
			}
		}
		io.WriteString(ch, "% Bad passwords\r\nR> ")
	}
}

func TestSSHSessionEnablePassword(t *testing.T) {
	sent := make(chan string, 3)
	addr := startShellServer(t, fakeEnableDevice("s3cret-enable", sent))
	var trace bytes.Buffer
	sess := dialShell(t, addr, func(t *Target) {
		t.Enable = "s3cret-enable"
		t.Debug = &trace
	})
	if _, err := sess.SendCommand("enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if got := <-sent; got != "s3cret-enable" {
		t.Fatalf("device got %q", got)
	}
	out, err := sess.SendCommand("show running-config")
	if err != nil || out != "hostname R" {
		t.Fatalf("show running-config = %q, %v", out, err)
	}
	tr := trace.String()
	if strings.Contains(tr, "s3cret-enable") {
		t.Fatalf("enable password leaked into the debug trace:\n%s", tr)
	}
	for _, want := range []string{">>> enable", ">>> [enable password]", ">>> show running-config", "<<< hostname R"} {
		if !strings.Contains(tr, want) {
			t.Fatalf("trace missing %q:\n%s", want, tr)
		}
	}
}

// Without an enable password, a Password: prompt is an error. Treating it as
// completion would type the next command in as the password and commit the
// device's "% Invalid input" as the backup.
func TestSSHSessionPasswordPromptWithoutEnableFails(t *testing.T) {
	sent := make(chan string, 3)
	addr := startShellServer(t, fakeEnableDevice("x", sent))
	sess := dialShell(t, addr, nil)
	start := time.Now()
	_, err := sess.SendCommand("enable")
	if err == nil || !strings.Contains(err.Error(), "enable password") {
		t.Fatalf("want a missing-enable-password error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("password prompt not detected promptly: %v", elapsed)
	}
}

func TestSSHSessionWrongEnablePasswordFails(t *testing.T) {
	sent := make(chan string, 3)
	addr := startShellServer(t, fakeEnableDevice("right", sent))
	sess := dialShell(t, addr, func(t *Target) { t.Enable = "wrong" })
	_, err := sess.SendCommand("enable")
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("want an enable-rejected error, got %v", err)
	}
	if n := len(sent); n != 1 {
		t.Fatalf("enable password sent %d times, want 1", n)
	}
}

func TestTrailingPromptDetection(t *testing.T) {
	yes := []string{
		"config system global\nend\nFGT # ",
		"\nroot@openwrt-lab:~# ",
		"...lgpl-2.1.php\r\n\rsw1# ",
		"[admin@MikroTik] > ",
		"\x1b[?2004huser@host:~$ ",
		"user@router> \x1b[K",
	}
	for _, s := range yes {
		if !endsWithPrompt([]byte(s)) {
			t.Errorf("prompt not detected at the end of %q", s)
		}
	}
	no := []string{
		"",
		"this whole line looks like a prompt#\n",
		"router#\r\n",
		"router# \x1b[0m\n",
	}
	for _, s := range no {
		if endsWithPrompt([]byte(s)) {
			t.Errorf("%q must not count as a trailing prompt", s)
		}
	}
	if !trailingMatches([]byte("enable\r\nPassword: "), passwordPrompt) {
		t.Error("trailing Password: prompt not detected")
	}
	if trailingMatches([]byte("set password ENC abc==\n"), passwordPrompt) ||
		trailingMatches([]byte("username admin password"), passwordPrompt) {
		t.Error("config text containing 'password' must not look like a prompt")
	}
	for _, s := range []string{"Username: ", "sw-san-1 login: ", "login:"} {
		if !trailingMatches([]byte(s), usernamePrompt) {
			t.Errorf("username prompt %q not detected", s)
		}
	}
	for _, s := range []string{"Authorised users only. Please log in:", "Username: admin\r\n", "You must login: "} {
		if trailingMatches([]byte(s), usernamePrompt) {
			t.Errorf("%q must not look like a username prompt", s)
		}
	}
	for _, s := range []string{" --More-- ", "-- More --", "<--- More --->", "---(more 25%)---"} {
		if !trailingMatches([]byte("x\r\n"+s), pagerRe) {
			t.Errorf("pager prompt %q not detected", s)
		}
	}
	if trailingMatches([]byte("description --More-- here"), pagerRe) {
		t.Error("pager text inside a line must not be answered")
	}
}

// A reader blocked handing output to a session nobody reads any more must
// exit once the session is closed, instead of leaking.
func TestShellReaderReleasedOnStop(t *testing.T) {
	s := newShell(Target{}, "\n", func(string) error { return nil })
	done := make(chan struct{})
	go func() {
		for s.deliver([]byte("flood")) {
		}
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // let out fill up
	s.stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader still blocked after stop")
	}
}

// --debug traces the exec transport too (MikroTik backs up over ssh-exec).
func TestSSHExecDebugTrace(t *testing.T) {
	tgt := startExecServer(t, func(_ string, out, _ io.Writer, _ <-chan struct{}) uint32 {
		io.WriteString(out, "/system identity\r\nset name=rtr\r\n")
		return 0
	})
	var trace bytes.Buffer
	tgt.Debug = &trace
	tr, _ := Get("ssh-exec")
	sess, err := tr.Dial(context.Background(), tgt)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()
	if _, err := sess.SendCommand("/export terse"); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	tr2 := trace.String()
	if !strings.Contains(tr2, ">>> /export terse") || !strings.Contains(tr2, "<<< /system identity") {
		t.Fatalf("trace incomplete:\n%s", tr2)
	}
	if strings.Contains(tr2, "secret") {
		t.Fatalf("password leaked into the trace:\n%s", tr2)
	}
}
