package transport

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

func init() { Register(&SSHExec{}) }

// SSHExec runs each command in its own exec channel (no PTY, no interactive
// shell), reading the command's stdout to EOF - exactly what `ssh host "cmd"`
// does. This is the reliable path for platforms whose interactive CLI pauses
// mid-command: notably MikroTik RouterOS, where "/export" gathers the whole
// config before it emits a single byte. The interactive transport's idle-timeout
// reader gives up during that silent gather and captures nothing ("empty
// configuration"), while an exec channel simply waits for the command to finish.
type SSHExec struct{}

// Name implements Transport.
func (*SSHExec) Name() string { return "ssh-exec" }

// Dial implements Transport. It opens the SSH client now; each SendCommand runs
// in a fresh exec session on that client.
func (*SSHExec) Dial(ctx context.Context, t Target) (Session, error) {
	client, err := dialClient(ctx, t)
	if err != nil {
		return nil, err
	}
	return &execSession{client: client, timeout: func() time.Duration {
		if t.Timeout > 0 {
			return t.Timeout
		}
		return 30 * time.Second
	}()}, nil
}

type execSession struct {
	client  *ssh.Client
	timeout time.Duration
}

const MaxExecOutput = 32 * 1024

// SendCommand runs cmd in a new exec channel and returns its stdout. No prompt
// or echo stripping is needed - an exec channel carries only the command's own
// output. Exit errors and output limits are always surfaced, even when the
// device emitted partial output.
func (s *execSession) SendCommand(cmd string) (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := sess.Start(cmd); err != nil {
		return "", err
	}

	var timedOut atomic.Bool
	timer := time.AfterFunc(s.timeout, func() {
		timedOut.Store(true)
		_ = sess.Close()
	})
	defer timer.Stop()

	raw, readErr := io.ReadAll(io.LimitReader(stdout, MaxExecOutput+1))
	waitErr := sess.Wait()
	text := cleanExec(string(raw))
	if len(raw) > MaxExecOutput {
		text = cleanExec(string(raw[:MaxExecOutput])) + "\n[output truncated]"
		return text, fmt.Errorf("command output exceeded %d bytes", MaxExecOutput)
	}
	if timedOut.Load() {
		return text, fmt.Errorf("command timed out after %s", s.timeout)
	}
	if readErr != nil {
		return text, fmt.Errorf("read command output: %w", readErr)
	}
	if waitErr != nil {
		return text, fmt.Errorf("execute command: %w", waitErr)
	}
	return text, nil
}

// Close implements Session.
func (s *execSession) Close() error { return s.client.Close() }

// cleanExec normalises CRLF and strips any stray ANSI (some RouterOS builds
// colourise even a non-PTY session). ansiRe is shared from ssh.go.
func cleanExec(raw string) string {
	raw = ansiRe.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.ReplaceAll(raw, "\r", "\n")
}
