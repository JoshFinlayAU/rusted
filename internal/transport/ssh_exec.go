package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

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
	return &execSession{client: client, raw: t.RawOutput, debug: t.Debug}, nil
}

type execSession struct {
	client *ssh.Client
	raw    bool      // Target.RawOutput: SendCommand returns stdout byte-exact
	debug  io.Writer // Target.Debug trace (nil = silent)
}

// SendCommand runs cmd in a new exec channel and returns its stdout. No prompt
// or echo stripping is needed - an exec channel carries only the command's own
// output. A non-zero exit that still produced output is treated as success
// (some CLIs exit non-zero even on a good read); only an error with no output
// is surfaced.
func (s *execSession) SendCommand(cmd string) (string, error) {
	tracef(s.debug, ">>> %s\n", cmd)
	sess, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	runErr := sess.Run(cmd)
	text := stdout.String()
	if !s.raw {
		text = cleanExec(text)
	}
	tracef(s.debug, "<<< %s\n", text)
	if strings.TrimSpace(text) == "" {
		// Nothing on stdout: whatever the device said on stderr is the only clue to
		// why, so return it instead of an empty capture with no reason.
		if msg := strings.TrimSpace(cleanExec(stderr.String())); msg != "" {
			return "", fmt.Errorf("%s: %s", cmd, msg)
		}
		if runErr != nil {
			return "", runErr
		}
	}
	return text, nil
}

// Close implements Session.
func (s *execSession) Close() error { return s.client.Close() }

// Exec implements Executor. stdout and stderr share one capped buffer, like
// `cmd 2>&1`, so a device's error text lands next to whatever it printed first.
// The command is cut off when ctx ends or the output passes limit bytes, and
// every failure - including a non-zero exit - is reported alongside the
// partial output.
func (s *execSession) Exec(ctx context.Context, cmd string, limit int) (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	out := newCappedBuffer(limit)
	sess.Stdout, sess.Stderr = out, out
	if err := sess.Start(cmd); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	var runErr error
	select {
	case runErr = <-done:
	case <-out.full:
		sess.Close()
		<-done
	case <-ctx.Done():
		sess.Close()
		<-done
		runErr = fmt.Errorf("command did not finish: %w", ctx.Err())
	}

	text := cleanExec(out.String())
	if out.overflowed() {
		return text + "\n[output truncated]", fmt.Errorf("%w (%d bytes)", ErrOutputLimit, limit)
	}
	return text, runErr
}

// cappedBuffer keeps the first limit bytes written to it and closes full once
// anything beyond that arrives. ssh copies stdout and stderr on separate
// goroutines, hence the mutex.
type cappedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
	over  bool
	full  chan struct{}
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit, full: make(chan struct{})}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.limit - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		if !c.over {
			c.over = true
			close(c.full)
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *cappedBuffer) overflowed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.over
}

// cleanExec normalises CRLF and strips any stray ANSI (some RouterOS builds
// colourise even a non-PTY session). ansiRe is shared from ssh.go.
func cleanExec(raw string) string {
	raw = ansiRe.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.ReplaceAll(raw, "\r", "\n")
}
