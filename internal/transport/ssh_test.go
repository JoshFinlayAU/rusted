package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Fixtures here must not leak real device identifiers: hosts, hostnames, IPs
// and counters are picked/generated fresh on every run.
var (
	poolPromptHosts = []string{"openwrt-lab", "openwrt-test", "openwrt-dev"}
	poolDeviceHosts = []string{"core-sw1", "edge-rt2", "campus-sw3", "lab-rtr4"}
)

func pick(pool []string) string { return pool[mrand.IntN(len(pool))] }

func fakeIP() string {
	return fmt.Sprintf("10.%d.%d.%d", mrand.IntN(256), mrand.IntN(256), mrand.IntN(254)+1)
}

// startFakeSSH runs a minimal SSH server that accepts any password login and
// drives each shell session through script. It returns the listen address as
// host:port.
func startFakeSSH(t *testing.T, script func(ch ssh.Channel)) string {
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
			sconn, chans, reqs, err := ssh.NewServerConn(c, cfg)
			if err != nil {
				c.Close()
				continue
			}
			go func() {
				defer sconn.Close()
				for req := range reqs {
					if req.WantReply {
						req.Reply(false, nil)
					}
				}
			}()
			for newChan := range chans {
				if newChan.ChannelType() != "session" {
					newChan.Reject(ssh.UnknownChannelType, "unsupported")
					continue
				}
				ch, chReqs, _ := newChan.Accept()
				go handleFakeSSHSession(ch, chReqs, script)
			}
		}
	}()
	return ln.Addr().String()
}

func handleFakeSSHSession(ch ssh.Channel, reqs <-chan *ssh.Request, script func(ch ssh.Channel)) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "pty-req", "env":
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "shell":
			if req.WantReply {
				req.Reply(true, nil)
			}
			go script(ch)
		case "exec":
			// Exec channels (ssh-exec transport): run the same scripted
			// output, then signal a clean exit so Session.Output returns.
			// NOTE: Session.Wait only unblocks when the CHANNEL closes
			// (exit-status alone keeps its request loop running), so a full
			// Close is required here.
			if req.WantReply {
				req.Reply(true, nil)
			}
			go func() {
				script(ch)
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				ch.CloseWrite()
				ch.Close()
			}()
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func readShellLine(ch ssh.Channel) string {
	b := make([]byte, 256)
	n, _ := ch.Read(b)
	return strings.TrimSpace(string(b[:n]))
}

// dialFakeSSH opens a session to a fake SSH device with fast timeouts.
func dialFakeSSH(t *testing.T, addr string) Session {
	t.Helper()
	tr, err := Get("ssh")
	if err != nil {
		t.Fatalf("ssh transport not registered: %v", err)
	}
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	sess, err := tr.Dial(ctx, Target{
		Host:        host,
		Port:        port,
		Username:    "admin",
		Password:    "secret",
		Timeout:     5 * time.Second,
		CmdTimeout:  10 * time.Second,
		IdleTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// The FortiGate failure mode: the device closes the SSH session partway
// through show full-configuration. That must surface as an error — treating
// it as completion silently saved truncated configs as valid backups.
func TestSSHSessionClosedMidCommandFails(t *testing.T) {
	addr := startFakeSSH(t, func(ch ssh.Channel) {
		io.WriteString(ch, "FGT # ")
		readShellLine(ch) // terminal length 0
		io.WriteString(ch, "FGT # ")
		readShellLine(ch) // show full-configuration
		io.WriteString(ch, "#config-version=FG120G\nconfig system global\n")
		// Device dies mid-dump: channel EOF, connection stays open.
		ch.CloseWrite()
	})

	sess := dialFakeSSH(t, addr)
	_, _ = sess.SendCommand("terminal length 0")
	_, err := sess.SendCommand("show full-configuration")
	if err == nil {
		t.Fatal("expected an error when the session closes mid-command")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a session-closed error, got: %v", err)
	}
}

// Prompt-looking lines inside dumped content must not complete the command
// early; only a genuine trailing prompt may (last-line anchored detection).
func TestSSHSessionPromptLikeContentDoesNotComplete(t *testing.T) {
	addr := startFakeSSH(t, func(ch ssh.Channel) {
		write := func(s string) { _, _ = io.WriteString(ch, s) }
		write("FGT # ")
		readShellLine(ch) // terminal length 0
		write("FGT # ")
		readShellLine(ch) // show full-configuration
		write("#conf_file_ver=" + strconv.FormatUint(mrand.Uint64(), 10) + "\n")
		write("set buffer \"<html>lang=en</html>\"\n")
		write("this whole line looks like a prompt#\n")
		time.Sleep(700 * time.Millisecond)
		write("more output after the lookalike\n")
		time.Sleep(700 * time.Millisecond)
		write("final line\nFGT # ")
	})

	sess := dialFakeSSH(t, addr)
	_, _ = sess.SendCommand("terminal length 0")
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

func TestTrailingPromptDetection(t *testing.T) {
	host := pick(poolPromptHosts)
	// A real prompt: emitted bare, without a terminating newline.
	if !endsWithPrompt([]byte("config system global\nend\nFGT # ")) {
		t.Error("bare trailing prompt not detected")
	}
	if !endsWithPrivilegedPrompt([]byte("Router# ")) {
		t.Error("bare trailing # prompt not detected")
	}
	// Linux-style prompts contain the home directory ("~") — OpenWrt/dropbear.
	if !endsWithPrompt([]byte("\nroot@" + host + ":~# ")) {
		t.Error("OpenWrt-style 'user@host:~#' prompt not detected")
	}
	if !endsWithPrivilegedPrompt([]byte("root@" + host + ":~#")) {
		t.Error("OpenWrt-style root prompt not detected as privileged")
	}
	// Prompt-shaped content lines end with a newline and must NOT complete.
	if endsWithPrompt([]byte("this whole line looks like a prompt#\n")) {
		t.Error("newline-terminated lookalike must not be treated as a prompt")
	}
	// NX-OS over telnet emits a stray \r (and NUL) before the login prompt.
	if !endsWithPrompt([]byte("...lgpl-2.1.php\r\n\rsw1# ")) {
		t.Error("bare prompt after stray \\r not detected")
	}
	if endsWithPrompt([]byte("")) {
		t.Error("empty buffer must not look like a prompt")
	}
	// Password prompts require the colon.
	if !endsWithPasswordPrompt([]byte("enable\nPassword: ")) {
		t.Error("trailing Password: prompt not detected")
	}
	if endsWithPasswordPrompt([]byte("set password ENC abc==\n")) {
		t.Error("config text containing 'password' must not look like a prompt")
	}
}

// --debug must trace the command/data exchange on the exec transport too
// (used by openwrt and mikrotik), not just interactive sessions.
func TestSSHExecDebugLogging(t *testing.T) {
	addr := startFakeSSH(t, func(ch ssh.Channel) {
		_, _ = io.WriteString(ch, "config system\n\toption hostname 'rtr'\n")
	})

	tr, err := Get("ssh-exec")
	if err != nil {
		t.Fatalf("ssh-exec not registered: %v", err)
	}
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	var dbg bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sess, err := tr.Dial(ctx, Target{
		Host:      host,
		Port:      port,
		Username:  "admin",
		Password:  "secret",
		Timeout:   5 * time.Second,
		RawOutput: true,
		Debug:     &dbg,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sess.Close()

	out, err := sess.SendCommand("cat /etc/config/system")
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if out != "config system\n\toption hostname 'rtr'\n" {
		t.Fatalf("unexpected output:\n%q", out)
	}
	trace := dbg.String()
	if !strings.Contains(trace, ">>> cat /etc/config/system") {
		t.Fatalf("missing >>> command trace:\n%s", trace)
	}
	if !strings.Contains(trace, "<<< config system") {
		t.Fatalf("missing <<< output trace:\n%s", trace)
	}
}

// Regression for the OpenWrt rollout: the BusyBox login banner is followed by
// a "root@host:~#" prompt whose "~" used to fall outside the prompt regex, so
// every command ran into the hard timeout.
func TestSSHSessionOpenWRTPrompt(t *testing.T) {
	promptHost := pick(poolPromptHosts)
	cfgHost := pick(poolDeviceHosts)
	ip := fakeIP()
	prompt := "root@" + promptHost + ":~# "
	addr := startFakeSSH(t, func(ch ssh.Channel) {
		write := func(s string) { _, _ = io.WriteString(ch, s) }
		write("\nBusyBox v1.37.0 (2026-06-11 08:43:35 UTC) built-in shell (ash)\n\n" +
			" OpenWrt 25.12-snapshot, r0-abcdef012345\n-----------------------------------------------------\n\n")
		write(prompt)
		readShellLine(ch) // first config command
		write("## /etc/config/system\nconfig system\n\toption hostname '" + cfgHost + "'\n\n## /etc/config/network\nconfig interface 'lan'\n\toption ipaddr '" + ip + "'\n\n")
		write(prompt)
		readShellLine(ch) // second config command
		write("## /etc/sysupgrade.conf\n/root/scripts/*\n/etc/init.d/connection_monitor\n\n")
		write(prompt)
	})

	sess := dialFakeSSH(t, addr)
	for _, cmd := range []string{
		`for f in /etc/config/*; do ...; done`,
		`printf '\n## /etc/sysupgrade.conf\n'; cat /etc/sysupgrade.conf; ...`,
	} {
		out, err := sess.SendCommand(cmd)
		if err != nil {
			t.Fatalf("SendCommand(%q): %v", cmd, err)
		}
		if strings.Contains(out, "root@"+promptHost) {
			t.Fatalf("prompt leaked into output:\n%s", out)
		}
		switch {
		case strings.Contains(out, "hostname"):
			if !strings.Contains(out, cfgHost) {
				t.Fatalf("incomplete /etc/config capture:\n%s", out)
			}
		case strings.Contains(out, "sysupgrade"):
			if !strings.Contains(out, "connection_monitor") {
				t.Fatalf("incomplete sysupgrade capture:\n%s", out)
			}
		default:
			t.Fatalf("unexpected command output:\n%s", out)
		}
	}
}
