package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// execHandler answers one exec request: it writes to stdout/stderr and returns
// the exit status. closed is closed when the client tears the channel down.
type execHandler func(cmd string, stdout, stderr io.Writer, closed <-chan struct{}) uint32

// startExecServer runs a minimal in-process SSH server that only serves exec
// requests, and returns a Target that reaches it.
func startExecServer(t *testing.T, h execHandler) Target {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) != "secret" {
				return nil, errors.New("bad password")
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveExecConn(nc, cfg, h)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return Target{Host: "127.0.0.1", Port: addr.Port, Username: "u", Password: "secret", Timeout: 5 * time.Second}
}

func serveExecConn(nc net.Conn, cfg *ssh.ServerConfig, h execHandler) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range chReqs {
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				req.Reply(true, nil)
				n := binary.BigEndian.Uint32(req.Payload)
				cmd := string(req.Payload[4 : 4+n])
				closed := make(chan struct{})
				go func() {
					for range chReqs {
					}
					close(closed)
				}()
				status := h(cmd, ch, ch.Stderr(), closed)
				ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, status))
				return
			}
		}()
	}
}

func dialExec(t *testing.T, h execHandler) *execSession {
	t.Helper()
	tr, _ := Get("ssh-exec")
	sess, err := tr.Dial(context.Background(), startExecServer(t, h))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess.(*execSession)
}

// Backups must keep tolerating a non-zero exit after a good read: the ad-hoc
// Exec path is strict, SendCommand is not.
func TestSendCommandToleratesNonZeroExitWithOutput(t *testing.T) {
	s := dialExec(t, func(_ string, out, _ io.Writer, _ <-chan struct{}) uint32 {
		io.WriteString(out, "big config\r\n")
		return 1
	})
	got, err := s.SendCommand("show run")
	if err != nil || got != "big config\n" {
		t.Fatalf("SendCommand = %q, %v; want config and no error", got, err)
	}
}

func TestSendCommandIsNotCapped(t *testing.T) {
	big := strings.Repeat("x", 4*testLimit)
	s := dialExec(t, func(_ string, out, _ io.Writer, _ <-chan struct{}) uint32 {
		io.WriteString(out, big)
		return 0
	})
	got, err := s.SendCommand("/export")
	if err != nil || len(got) != len(big) {
		t.Fatalf("SendCommand returned %d bytes, %v; want %d", len(got), err, len(big))
	}
}

const testLimit = 1024

func TestExecSuccess(t *testing.T) {
	s := dialExec(t, func(cmd string, out, _ io.Writer, _ <-chan struct{}) uint32 {
		io.WriteString(out, "ran: "+cmd+"\r\n")
		return 0
	})
	got, err := s.Exec(context.Background(), "show version", testLimit)
	if err != nil || got != "ran: show version\n" {
		t.Fatalf("Exec = %q, %v", got, err)
	}
}

func TestExecReportsNonZeroExitWithStderr(t *testing.T) {
	s := dialExec(t, func(_ string, out, errw io.Writer, _ <-chan struct{}) uint32 {
		io.WriteString(out, "partial\n")
		io.WriteString(errw, "syntax error\n")
		return 2
	})
	got, err := s.Exec(context.Background(), "bogus", testLimit)
	if err == nil {
		t.Fatal("Exec: want an error for exit status 2")
	}
	var exit *ssh.ExitError
	if !errors.As(err, &exit) || exit.ExitStatus() != 2 {
		t.Fatalf("err = %v; want ExitError status 2", err)
	}
	if !strings.Contains(got, "partial") || !strings.Contains(got, "syntax error") {
		t.Fatalf("output %q should hold both stdout and stderr", got)
	}
}

func TestExecTruncatesAtLimit(t *testing.T) {
	s := dialExec(t, func(_ string, out, _ io.Writer, closed <-chan struct{}) uint32 {
		// Stream until the client hangs up, like an unbounded "ping".
		chunk := strings.Repeat("y", 256)
		for {
			select {
			case <-closed:
				return 0
			default:
			}
			if _, err := io.WriteString(out, chunk); err != nil {
				return 0
			}
		}
	})
	got, err := s.Exec(context.Background(), "ping", testLimit)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("err = %v; want ErrOutputLimit", err)
	}
	if want := strings.Repeat("y", testLimit) + "\n[output truncated]"; got != want {
		t.Fatalf("got %d bytes; want the first %d plus a marker", len(got), testLimit)
	}
}

func TestExecHonoursContextDeadline(t *testing.T) {
	s := dialExec(t, func(_ string, out, _ io.Writer, closed <-chan struct{}) uint32 {
		io.WriteString(out, "started\n")
		<-closed
		return 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := s.Exec(ctx, "hang", testLimit)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; want DeadlineExceeded", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("Exec did not return promptly after the deadline")
	}
	if got != "started\n" {
		t.Fatalf("partial output = %q", got)
	}
}
