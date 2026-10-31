package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func init() { Register(&SSH{}) }

// SSH is the built-in interactive-shell transport. It requests a PTY and
// drives the device through its CLI, which is how the great majority of
// network operating systems expect to be automated (a bare "exec" channel is
// frequently unsupported on switches and routers).
type SSH struct{}

// Name implements Transport.
func (*SSH) Name() string { return "ssh" }

// promptRe matches a typical network-device shell prompt: a hostname-like
// token followed by one of > # $ and optional trailing whitespace. It covers
// bracketed prompts (MikroTik's "[admin@router] >") and Linux-style ones with
// a home directory ("root@host:~#"). It is deliberately permissive: drain only
// tries it against the bare last line (trailingMatches), and the idle timeout
// catches prompts it misses.
var promptRe = regexp.MustCompile(`(?m)^[\w.@:()\[\]\-/ ~]+[#>$]\s*$`)

// ansiRe matches ANSI/VT100 escape sequences, plus the OSC "set window title"
// sequences shells emit. Some platforms (notably MikroTik RouterOS) colourise
// their CLI even over a programmatic session, and bash wraps its prompt in
// bracketed-paste toggles (\x1b[?2004h).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// dialClient opens an authenticated SSH client to the target with the widened
// (legacy-friendly) KEX/cipher set network gear often needs. Shared by the
// interactive SSH transport and the exec transport (ssh-exec).
func dialClient(ctx context.Context, t Target) (*ssh.Client, error) {
	if t.Port == 0 {
		t.Port = 22
	}
	if t.Timeout == 0 {
		t.Timeout = 30 * time.Second
	}

	var auths []ssh.AuthMethod
	if len(t.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(t.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if t.Password != "" {
		auths = append(auths, ssh.Password(t.Password),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range qs {
					ans[i] = t.Password
				}
				return ans, nil
			}))
	}

	cfg := &ssh.ClientConfig{
		User: t.Username,
		Auth: auths,
		// Network devices very often present self-signed / unmanaged host
		// keys. We accept any key here; pinning known_hosts is a planned
		// enhancement (see README "Roadmap").
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         t.Timeout,
		Config: ssh.Config{
			// Many older devices only offer legacy KEX/ciphers; widen support.
			KeyExchanges: append(defaultKEX(), "diffie-hellman-group14-sha1", "diffie-hellman-group1-sha1", "diffie-hellman-group-exchange-sha1"),
			Ciphers:      append(defaultCiphers(), "aes128-cbc", "3des-cbc"),
		},
	}

	addr := net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
	d := net.Dialer{Timeout: t.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

// Dial implements Transport.
func (*SSH) Dial(ctx context.Context, t Target) (Session, error) {
	client, err := dialClient(ctx, t)
	if err != nil {
		return nil, err
	}

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 0, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("vt100", 1000, 200, modes); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	sess.Stderr = nil
	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	s := &sshSession{client: client, sess: sess, stdin: stdin}
	s.shell = newShell(t, "\n", func(p string) error {
		_, err := io.WriteString(stdin, p)
		return err
	})
	go s.reader(stdout)

	// Consume the login banner / initial prompt so it does not contaminate the
	// first command's output.
	s.drain("", 10*time.Second)
	return s, nil
}

// sshSession is an interactive PTY shell; the command loop (SendCommand,
// prompt detection) lives in shell.
type sshSession struct {
	*shell
	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser
}

func (s *sshSession) reader(r io.Reader) {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if !s.deliver(chunk) {
				return
			}
		}
		if err != nil {
			close(s.out)
			return
		}
	}
}

// Close implements Session.
func (s *sshSession) Close() error {
	_, _ = io.WriteString(s.stdin, "exit\n")
	s.stop()
	s.stdin.Close()
	s.sess.Close()
	return s.client.Close()
}

// cleanOutput removes the echoed command (first line) and a trailing prompt
// line from a captured response, renders backspaces, and normalises CRLF to
// LF.
func cleanOutput(cmd, raw string) string {
	raw = ansiRe.ReplaceAllString(raw, "")
	if strings.IndexByte(raw, '\b') >= 0 {
		raw = applyBackspaces(raw)
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	// Drop the first line if it is the echoed command.
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == strings.TrimSpace(cmd) {
		lines = lines[1:]
	}
	// Drop a trailing prompt line and any trailing blank lines.
	for len(lines) > 0 {
		last := lines[len(lines)-1]
		if strings.TrimSpace(last) == "" || promptRe.MatchString(last) {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}
	return strings.Join(lines, "\n")
}

// applyBackspaces renders \b the way a terminal would, deleting the character
// before it on the same line. Pager prompts are wiped this way (IOS prints
// "\b\b\b   \b\b\b"), and the leftovers must not reach the backup.
func applyBackspaces(s string) string {
	out := make([]byte, 0, len(s))
	lineStart := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\b':
			if len(out) > lineStart {
				out = out[:len(out)-1]
			}
		case '\n', '\r':
			out = append(out, c)
			lineStart = len(out)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[len(b)-n:])
}

func defaultKEX() []string {
	return []string{"curve25519-sha256", "curve25519-sha256@libssh.org",
		"ecdh-sha2-nistp256", "ecdh-sha2-nistp384", "ecdh-sha2-nistp521",
		"diffie-hellman-group14-sha256"}
}

func defaultCiphers() []string {
	return []string{"chacha20-poly1305@openssh.com", "aes128-gcm@openssh.com",
		"aes256-gcm@openssh.com", "aes128-ctr", "aes192-ctr", "aes256-ctr"}
}
