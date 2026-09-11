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
// trailing whitespace. It covers bracketed prompts too (MikroTik's
// "[admin@router] >") and Linux-style ones with the home directory
// ("root@openwrt:~#", dropbear/busybox ash). It is deliberately permissive;
// bare-trailing-prompt anchoring (trailingMatches) is the real gate.
var promptRe = regexp.MustCompile(`(?m)^[\w.@:()\[\]\-/ ~]+[#>$]\s*$`)

// morePromptRe detects pager prompts like "--More--" or " -- More --" that some
// devices emit when output exceeds one screen. When seen, drain() sends a
// space to advance past the prompt.
var morePromptRe = regexp.MustCompile(`(?i)--?\s*[Mm]ore\s*--?`)

// ansiRe matches ANSI/VT100 escape sequences. Some platforms (notably MikroTik
// RouterOS) colourise their CLI even over a programmatic session.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

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

	s := &sshSession{
		client:      client,
		sess:        sess,
		stdin:       stdin,
		out:         make(chan []byte, 64),
		done:        make(chan error, 1),
		idle:        idleTimeout(t),
		readTimeout: cmdTimeout(t),
		enable:      t.Enable,
		debug:       t.Debug,
	}
	go s.reader(stdout)

	// Consume the login banner / initial prompt so it does not contaminate the
	// first command's output.
	s.drain(10 * time.Second)

	// Cisco IOS (and similar) land in user-exec mode ("Router>") after login;
	// privileged mode ("Router#") is required for commands like "show
	// running-config". If an enable password was supplied, send "enable" and
	// elevate now, before the driver starts emitting Init/Config commands.
	if t.Enable != "" {
		if err := s.elevate(t.Enable); err != nil {
			client.Close()
			sess.Close()
			return nil, err
		}
	}
	return s, nil
}

// usernamePrompt matches "Username:" / "login:" styles.
var usernamePrompt = regexp.MustCompile(`(?i)(u?ser ?name|log ?in)[:\s]*$`)

// passwordPrompt matches "Password:" / "Passphrase:" — used both for the initial
// telnet login and for the Cisco "enable" privilege escalation. The colon is
// required so config text merely containing the word "password" cannot match.
var passwordPrompt = regexp.MustCompile(`(?i)pass(word|phrase):\s*$`)

// privilegedPromptRe matches a prompt ending in "#" (Cisco IOS/NX-OS/ASA
// enable mode, Linux root shells like "root@openwrt:~#"), as opposed to ">"
// (user exec) or "$" (shells).
var privilegedPromptRe = regexp.MustCompile(`(?m)^[\w.@:()\[\]\-/ ~]+#\s*$`)

// elevate sends "enable" and the privileged-mode password, then waits for the
// device to land in enable mode (prompts ending in "#"). If the device does not
// prompt for a password, it is assumed to already be in enable mode (or does
// not require enable at all) and the call succeeds.
func (s *sshSession) elevate(enablePassword string) error {
	s.debugf(">>> enable\n")
	_, _ = io.WriteString(s.stdin, "enable\n")
	out, _, _ := s.drain(s.readTimeout)

	if !endsWithPasswordPrompt([]byte(out)) {
		// No password prompt — either already privileged or enable isn't needed.
		if endsWithPrivilegedPrompt([]byte(out)) {
			return nil
		}
		return nil
	}

	s.debugf(">>> [enable password]\n")
	_, _ = io.WriteString(s.stdin, enablePassword+"\n")
	out, _, _ = s.drain(s.readTimeout)
	if !endsWithPrivilegedPrompt([]byte(out)) {
		return fmt.Errorf("did not reach privileged mode after enable")
	}
	return nil
}

type sshSession struct {
	client      *ssh.Client
	sess        *ssh.Session
	stdin       io.WriteCloser
	out         chan []byte
	done        chan error
	idle        time.Duration
	readTimeout time.Duration
	enable      string
	debug       io.Writer
}

func (s *sshSession) debugf(format string, args ...any) {
	if s.debug != nil {
		fmt.Fprintf(s.debug, format, args...)
	}
}

func (s *sshSession) reader(r io.Reader) {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.debugf("<<< %s", string(chunk))
			s.out <- chunk
		}
		if err != nil {
			s.done <- err
			close(s.out)
			return
		}
	}
}

// Drain outcomes for everything that is not a clean prompt return.
const (
	drainTimeout = "timeout" // hard per-command timeout fired
	drainClosed  = "closed"  // device/network closed the session mid-command
)

// drain reads output until a bare device prompt (no trailing newline —
// see trailingMatches) appears at the end of the buffer, the stream goes
// idle for s.idle, or maxWait elapses.
//
// Returns (output, incomplete, reason). incomplete=false always means the
// device prompt was seen (or a Password: prompt is pending). When
// incomplete=true, reason explains why:
//   - drainTimeout: the hard timeout fired — the command did not finish.
//   - drainClosed: the session ended mid-command. This used to be treated as
//     success, silently saving truncated configs (e.g. a FortiGate dropping
//     the SSH session partway through show full-configuration); it now fails
//     loudly instead.
//
// On the idle timer: a Password: prompt at the tail completes (caller
// responds); a pager prompt gets a space; with data already received the idle
// timer resets and draining continues (devices pause between bursts on large
// dumps) — only the hard timeout can then end the command.
func (s *sshSession) drain(maxWait time.Duration) (string, bool, string) {
	var buf bytes.Buffer
	hasData := false
	idle := time.NewTimer(s.idle)
	defer idle.Stop()
	hard := time.NewTimer(maxWait)
	defer hard.Stop()
	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				return buf.String(), true, drainClosed
			}
			hasData = true
			buf.Write(chunk)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(s.idle)
			// Detect a pager prompt like "--More--" and send space to
			// advance past it.
			if morePromptRe.MatchString(lastLine(buf.Bytes())) {
				s.debugf(">>> [space]\n")
				_, _ = io.WriteString(s.stdin, " ")
				// Keep the current idle but note we responded.
			}
			if endsWithPrompt(buf.Bytes()) {
				return buf.String(), false, ""
			}
		case <-idle.C:
			// Device may be waiting for a password (e.g. after "enable").
			if endsWithPasswordPrompt(buf.Bytes()) {
				return buf.String(), false, ""
			}
			// Device may be waiting at a pager prompt.
			if morePromptRe.MatchString(lastLine(buf.Bytes())) {
				s.debugf(">>> [space]\n")
				_, _ = io.WriteString(s.stdin, " ")
				idle.Reset(s.idle)
				continue
			}
			if !hasData {
				return buf.String(), true, drainTimeout
			}
			idle.Reset(s.idle)
		case <-hard.C:
			return buf.String(), true, drainTimeout
		}
	}
}

// SendCommand implements Session. It writes the command, waits for the device's
// prompt to reappear, and strips the echoed command + trailing prompt — reusing
// the shared cleanOutput helper. If the device responds with a Password: prompt
// (as happens when a driver sends "enable" to enter privileged mode on Cisco
// IOS), SendCommand automatically sends Target.Enable and reads the follow-up
// output.
func (s *sshSession) SendCommand(cmd string) (string, error) {
	s.debugf(">>> %s\n", cmd)
	if _, err := io.WriteString(s.stdin, cmd+"\n"); err != nil {
		return "", err
	}
	raw, timedOut, reason := s.drain(s.readTimeout)
	// Even on timeout, check whether the device is waiting for an enable
	// password (Cisco IOS prompts for one after "enable"). The idle timer
	// fires while the device waits for input, so we must respond to the
	// password prompt BEFORE reporting a timeout.
	if s.enable != "" && endsWithPasswordPrompt([]byte(raw)) {
		raw = s.respondToEnablePrompt(raw)
		if !endsWithPrompt([]byte(raw)) {
			return "", fmt.Errorf("command %q did not complete (%s)", cmd, reason)
		}
		return cleanOutput(cmd, raw), nil
	}
	if timedOut {
		if reason == drainClosed {
			return "", fmt.Errorf("command %q did not complete: device closed the session mid-output (captured %d bytes)", cmd, len(raw))
		}
		return "", fmt.Errorf("command %q timed out after %s", cmd, s.readTimeout)
	}
	raw = s.respondToEnablePrompt(raw)
	return cleanOutput(cmd, raw), nil
}

// respondToEnablePrompt detects an interactive Password: prompt (triggered by
// the "enable" Init command on Cisco IOS) and, if an enable password is
// configured, sends it and reads the follow-up output.
func (s *sshSession) respondToEnablePrompt(raw string) string {
	if s.enable == "" {
		return raw
	}
	if !endsWithPasswordPrompt([]byte(raw)) {
		return raw
	}
	s.debugf(">>> [enable password]\n")
	if _, err := io.WriteString(s.stdin, s.enable+"\n"); err != nil {
		return raw
	}
	raw += func() string {
		s2, _, _ := s.drain(s.readTimeout)
		return s2
	}()
	return raw
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

// defaultCmdTimeout is the per-command hard timeout when Target.CmdTimeout is
// not set (0). Slower devices can raise this via the --cmd-timeout flag.
const defaultCmdTimeout = 60 * time.Second

// defaultIdleTimeout is the idle window after which a non-prompting stream is
// considered incomplete. Network config dumps can have natural pauses >700 ms
// between output bursts; 5 s accommodates large configs while still catching
// truly hung sessions. Raise it further per-device with --idle-timeout.
const defaultIdleTimeout = 5000 * time.Millisecond

func cmdTimeout(t Target) time.Duration {
	if t.CmdTimeout > 0 {
		return t.CmdTimeout
	}
	return defaultCmdTimeout
}

func idleTimeout(t Target) time.Duration {
	if t.IdleTimeout > 0 {
		return t.IdleTimeout
	}
	return defaultIdleTimeout
}
