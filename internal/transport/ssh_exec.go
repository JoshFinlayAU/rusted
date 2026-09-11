package transport

import (
	"context"
	"fmt"
	"io"
	"strings"

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
	raw    bool      // skip output normalisation (byte-exact capture)
	debug  io.Writer // --debug I/O trace (nil = silent)
}

func (s *execSession) debugf(format string, args ...any) {
	if s.debug != nil {
		fmt.Fprintf(s.debug, format, args...)
	}
}

// SendCommand runs cmd in a new exec channel and returns its stdout. No prompt
// or echo stripping is needed - an exec channel carries only the command's own
// output. A non-zero exit that still produced output is treated as success
// (some CLIs exit non-zero even on a good read); only an error with no output
// is surfaced.
func (s *execSession) SendCommand(cmd string) (string, error) {
	s.debugf(">>> %s\n", cmd)
	sess, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	out, runErr := sess.Output(cmd)
	text := string(out)
	if !s.raw {
		// Without a PTY the stream is already byte-exact; normalisation is
		// only for platforms that still emit CRLF/ANSI on exec channels.
		text = cleanExec(text)
	}
	s.debugf("<<< %s\n", text)
	if runErr != nil && strings.TrimSpace(text) == "" {
		return "", runErr
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
