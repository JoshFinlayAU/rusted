package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

func init() { Register(&Telnet{}) }

// Telnet speaks raw TCP (RFC 854/855) to devices that only offer a telnet CLI —
// classic Cisco IOS over telnet, console-server access, etc. It performs the
// interactive username/password login during Dial, then drives the device the same
// way the interactive SSH transport does (prompt detection + idle-timeout drain).
type Telnet struct{}

// Name implements Transport.
func (*Telnet) Name() string { return "telnet" }

// Dial implements Transport.
func (*Telnet) Dial(ctx context.Context, t Target) (Session, error) {
	if t.Port == 0 {
		t.Port = 23
	}
	if t.Timeout == 0 {
		t.Timeout = 30 * time.Second
	}

	d := net.Dialer{Timeout: t.Timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(t.Host, strconv.Itoa(t.Port)))
	if err != nil {
		return nil, err
	}

	s := &telnetSession{
		conn:        conn,
		out:         make(chan []byte, 64),
		done:        make(chan error, 1),
		idle:        idleTimeout(t),
		readTimeout: cmdTimeout(t),
		enable:      t.Enable,
	}
	go s.reader()

	// Perform the interactive login (username / password) that telnet requires,
	// since — unlike SSH — authentication is carried inside the session
	// stream. The login loop itself consumes the initial banner, so we must
	// NOT run a pre-drain here — that would eat the "Username:" prompt.
	if t.Username != "" || t.Password != "" {
		if err := s.login(t.Username, t.Password); err != nil {
			s.conn.Close()
			return nil, err
		}
	} else {
		// No login — consume the initial banner / prompt so it does not
		// contaminate the first command's output — mirrors the SSH
		// transport's initial drain.
		s.drain(10 * time.Second)
	}
	return s, nil
}



// login handles the telnet-level authentication: it waits for the device to
// request a username, sends it, waits for the password prompt, sends the
// password, then drains until the CLI prompt appears (indicating a successful
// login). If the device doesn't prompt for a username at all (some "no login"
// configs), it falls through to waiting for a prompt directly.
func (s *telnetSession) login(username, password string) error {
	loginTimeout := s.readTimeout * 2
	if err := s.conn.SetDeadline(time.Now().Add(loginTimeout)); err != nil {
		return err
	}
	defer s.conn.SetDeadline(time.Time{})
	deadline := time.Now().Add(15 * time.Second)
	hard := time.NewTimer(0)
	if !hard.Stop() {
		<-hard.C
	}
	hard.Reset(deadline.Sub(time.Now()))
	defer hard.Stop()

	loginIdle := s.idle * 4
	idle := time.NewTimer(loginIdle)
	defer idle.Stop()

	var buf bytes.Buffer
	loginStage := 0 // 0 = waiting for username, 1 = waiting for password, 2 = waiting for prompt

	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				return fmt.Errorf("telnet: connection closed during login")
			}
			buf.Write(chunk)
			text := buf.String()

			if loginStage == 0 && passwordPrompt.MatchString(text) && password != "" {
				// Device asked for a password directly (no username prompt).
				if _, err := io.WriteString(s.conn, password+"\r\n"); err != nil {
					return err
				}
				loginStage = 2
				buf.Reset()
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(loginIdle)
			} else if loginStage == 0 && usernamePrompt.MatchString(text) && username != "" {
				if _, err := io.WriteString(s.conn, username+"\r\n"); err != nil {
					return err
				}
				loginStage = 1
				buf.Reset()
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(loginIdle)
			} else if loginStage >= 1 && passwordPrompt.MatchString(text) && password != "" {
				if _, err := io.WriteString(s.conn, password+"\r\n"); err != nil {
					return err
				}
				loginStage = 2
				buf.Reset()
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(loginIdle)
			} else if loginStage >= 1 && promptRe.MatchString(tail(buf.Bytes(), 200)) {
				return nil
			} else if loginStage == 0 && promptRe.MatchString(tail(buf.Bytes(), 200)) {
				// Device didn't require a username (no login / aaa new-model off);
				// just jump straight to the prompt.
				return nil
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(loginIdle)

		case <-idle.C:
			if loginStage == 0 {
				// Didn't see a username prompt; if we have output that looks like a
				// prompt, treat login as complete (no-auth device).
				if promptRe.MatchString(tail(buf.Bytes(), 200)) {
					return nil
				}
				// Keep waiting — some devices have a slow banner.
				idle.Reset(loginIdle)
			} else {
				// Waiting for password prompt or final prompt; give up.
				return fmt.Errorf("telnet: login timed out at stage %d (output: %q)", loginStage, tail(buf.Bytes(), 100))
			}

		case <-hard.C:
			return fmt.Errorf("telnet: login deadline exceeded (output: %q)", tail(buf.Bytes(), 100))
		}
	}
}

type telnetSession struct {
	conn        net.Conn
	out         chan []byte
	done        chan error
	idle        time.Duration
	readTimeout time.Duration
	enable      string
}

func (s *telnetSession) reader() {
	buf := make([]byte, 8192)
	for {
		n, err := s.conn.Read(buf)
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
// buffer, the stream goes idle for s.idle without a prompt, or maxWait
// elapses — identical strategy to the SSH transport (ssh.go). Returns the
// captured output and a boolean indicating whether the drain ended "incomplete"
// — either the hard timeout (maxWait) fired, or the idle timer expired before
// a prompt was seen (command did not complete, e.g. a large config with
// >s.idle pauses between bursts).
func (s *telnetSession) drain(maxWait time.Duration) (string, bool) {
	var buf bytes.Buffer
	idle := time.NewTimer(s.idle)
	defer idle.Stop()
	hard := time.NewTimer(maxWait)
	defer hard.Stop()
	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				return buf.String(), false
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
				return buf.String(), false
			}
		case <-idle.C:
			// Idle without a prompt in the buffer means the command did not
			// complete — treat as incomplete so the caller can report a timeout.
			return buf.String(), true
		case <-hard.C:
			return buf.String(), true
		}
	}
}

// SendCommand implements Session. It writes the command, waits for the device's
// prompt to reappear, and strips the echoed command + trailing prompt — reusing
// the shared cleanOutput helper (ssh.go:230) so telnet and ssh output agree.
// If the device responds with a Password: prompt (as happens when a driver sends
// "enable" to enter privileged mode on Cisco IOS), SendCommand automatically sends
// Target.Enable and reads the follow-up output.
func (s *telnetSession) SendCommand(cmd string) (string, error) {
	deadline := time.Now().Add(s.readTimeout)
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return "", err
	}
	if _, err := io.WriteString(s.conn, cmd+"\r\n"); err != nil {
		return "", err
	}
	raw, timedOut := s.drain(s.readTimeout)
	// Even on timeout, check whether the device is waiting for an enable
	// password (Cisco IOS prompts for one after "enable"). The idle timer
	// fires while the device waits for input, so we must respond to the
	// password prompt BEFORE reporting a timeout.
	if s.enable != "" && passwordPrompt.MatchString(raw) {
		raw = s.respondToEnablePrompt(raw)
		// Re-check: if drain returned incomplete for the enable-response
		// drain too, report a timeout. Otherwise return cleaned output.
		if !promptRe.MatchString(tail([]byte(raw), 200)) {
			return "", fmt.Errorf("command %q timed out after %s", cmd, s.readTimeout)
		}
		return cleanOutput(cmd, raw), nil
	}
	if timedOut {
		return "", fmt.Errorf("command %q timed out after %s", cmd, s.readTimeout)
	}
	raw = s.respondToEnablePrompt(raw)
	return cleanOutput(cmd, raw), nil
}

// respondToEnablePrompt detects an interactive Password: prompt (triggered by the
// "enable" Init command on Cisco IOS) and, if an enable password is configured,
// sends it and reads the follow-up output. If no enable password is set, the raw
// output is returned unchanged so callers see the prompt text.
func (s *telnetSession) respondToEnablePrompt(raw string) string {
	if s.enable == "" {
		return raw
	}
	if !passwordPrompt.MatchString(raw) {
		return raw
	}
	if _, err := io.WriteString(s.conn, s.enable+"\r\n"); err != nil {
		return raw
	}
	raw += func() string {
		s2, _ := s.drain(s.readTimeout)
		return s2
	}()
	return raw
}

// Close implements Session.
func (s *telnetSession) Close() error {
	_ = s.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.WriteString(s.conn, "exit\r\n")
	return s.conn.Close()
}
