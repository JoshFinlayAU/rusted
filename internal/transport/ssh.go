package transport

import (
	"bytes"
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

// promptRe matches a typical network-device shell prompt at the end of the
// captured buffer: a hostname-like token followed by one of > # $ and optional
// trailing whitespace. It covers bracketed prompts too, e.g. MikroTik's
// "[admin@router] >". It is deliberately permissive; the idle-timeout in
// drain() is the real safety net.
var promptRe = regexp.MustCompile(`(?m)^[\w.@:()\[\]\-/ ]+[#>$]\s*$`)

// ansiRe matches ANSI/VT100 escape sequences. Some platforms (notably MikroTik
// RouterOS) colourise their CLI even over a programmatic session.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// Dial implements Transport.
func (*SSH) Dial(ctx context.Context, t Target) (Session, error) {
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
	client := ssh.NewClient(sc, chans, reqs)

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

	s := &sshSession{
		client:      client,
		sess:        sess,
		stdin:       stdin,
		out:         make(chan []byte, 64),
		done:        make(chan error, 1),
		idle:        700 * time.Millisecond,
		readTimeout: 60 * time.Second,
	}
	go s.reader(stdout)

	// Consume the login banner / initial prompt so it does not contaminate the
	// first command's output.
	s.drain(10 * time.Second)
	return s, nil
}

type sshSession struct {
	client      *ssh.Client
	sess        *ssh.Session
	stdin       io.WriteCloser
	out         chan []byte
	done        chan error
	idle        time.Duration
	readTimeout time.Duration
}

func (s *sshSession) reader(r io.Reader) {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.out <- chunk
		}
		if err != nil {
			s.done <- err
			close(s.out)
			return
		}
	}
}

// drain reads output until the device prompt reappears at the tail of the
// buffer, the stream goes idle for s.idle, or maxWait elapses.
func (s *sshSession) drain(maxWait time.Duration) string {
	var buf bytes.Buffer
	idle := time.NewTimer(s.idle)
	defer idle.Stop()
	hard := time.NewTimer(maxWait)
	defer hard.Stop()
	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				return buf.String()
			}
			buf.Write(chunk)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(s.idle)
			if promptRe.MatchString(tail(buf.Bytes(), 200)) {
				return buf.String()
			}
		case <-idle.C:
			return buf.String()
		case <-hard.C:
			return buf.String()
		}
	}
}

// SendCommand implements Session.
func (s *sshSession) SendCommand(cmd string) (string, error) {
	if _, err := io.WriteString(s.stdin, cmd+"\n"); err != nil {
		return "", err
	}
	raw := s.drain(s.readTimeout)
	return cleanOutput(cmd, raw), nil
}

// Close implements Session.
func (s *sshSession) Close() error {
	_, _ = io.WriteString(s.stdin, "exit\n")
	s.stdin.Close()
	s.sess.Close()
	return s.client.Close()
}

// cleanOutput removes the echoed command (first line) and a trailing prompt
// line from a captured response, and normalises CRLF to LF.
func cleanOutput(cmd, raw string) string {
	raw = ansiRe.ReplaceAllString(raw, "")
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
