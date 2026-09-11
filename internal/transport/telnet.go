package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

func init() { Register(&Telnet{}) }

// Telnet protocol verbs and options (RFC 854/855 and friends).
const (
	iacSE   byte = 240
	iacSB   byte = 250
	iacWILL byte = 251
	iacWONT byte = 252
	iacDO   byte = 253
	iacDONT byte = 254
	iacIAC  byte = 255

	optEcho  byte = 1
	optSGA   byte = 3
	optTType byte = 24
)

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
		debug:       t.Debug,
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
				s.debugf(">>> %s\n", password)
				if err := s.write([]byte(password + "\r\n")); err != nil {
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
				s.debugf(">>> %s\n", username)
				if err := s.write([]byte(username + "\r\n")); err != nil {
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
				s.debugf(">>> %s\n", password)
				if err := s.write([]byte(password + "\r\n")); err != nil {
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
			} else if loginStage >= 1 && endsWithPrompt(buf.Bytes()) {
				return nil
			} else if loginStage == 0 && endsWithPrompt(buf.Bytes()) {
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
				if endsWithPrompt(buf.Bytes()) {
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
	wmu         sync.Mutex
	pend        []byte
	out         chan []byte
	done        chan error
	idle        time.Duration
	readTimeout time.Duration
	enable      string
	debug       io.Writer
}

func (s *telnetSession) debugf(format string, args ...any) {
	if s.debug != nil {
		fmt.Fprintf(s.debug, format, args...)
	}
}

func (s *telnetSession) write(p []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.conn.Write(p)
	return err
}

// negotiate filters one chunk of raw device output through the telnet state
// machine: protocol sequences are answered and stripped from the CLI stream
// the rest of the transport sees, with partial sequences buffered until the
// next chunk completes them.
func (s *telnetSession) negotiate(chunk []byte) (visible, reply []byte) {
	data := append(s.pend, chunk...)
	s.pend = nil
	for i := 0; i < len(data); {
		if data[i] != iacIAC {
			j := bytes.IndexByte(data[i:], iacIAC)
			if j < 0 {
				visible = append(visible, data[i:]...)
				break
			}
			visible = append(visible, data[i:i+j]...)
			i += j
			continue
		}
		if i+1 >= len(data) {
			s.pend = append(s.pend, data[i:]...)
			break
		}
		switch data[i+1] {
		case iacIAC:
			visible = append(visible, iacIAC)
			i += 2
		case iacWILL, iacWONT, iacDO, iacDONT:
			if i+2 >= len(data) {
				s.pend = append(s.pend, data[i:]...)
				i = len(data)
				break
			}
			reply = append(reply, negotiateReply(data[i+1], data[i+2])...)
			i += 3
		case iacSB:
			end := bytes.Index(data[i+2:], []byte{iacIAC, iacSE})
			if end < 0 {
				s.pend = append(s.pend, data[i:]...)
				i = len(data)
				break
			}
			reply = append(reply, subnegotiationReply(data[i+2:i+2+end])...)
			i += end + 4
		default:
			i += 2
		}
	}
	if bytes.IndexByte(visible, 0) >= 0 {
		visible = bytes.ReplaceAll(visible, []byte{0}, nil)
	}
	return visible, reply
}

// negotiateReply is our side of option negotiation: agree to suppress-go-ahead
// and offer a terminal type (devices block until someone answers the TTYPE
// probe), refuse everything else.
func negotiateReply(verb, opt byte) []byte {
	switch verb {
	case iacDO:
		switch opt {
		case optTType, optSGA:
			return []byte{iacIAC, iacWILL, opt}
		}
		return []byte{iacIAC, iacWONT, opt}
	case iacWILL:
		switch opt {
		case optEcho, optSGA:
			return []byte{iacIAC, iacDO, opt}
		}
		return []byte{iacIAC, iacDONT, opt}
	}
	return nil
}

func subnegotiationReply(sub []byte) []byte {
	if len(sub) >= 2 && sub[0] == optTType && sub[1] == 1 {
		return append([]byte{iacIAC, iacSB, optTType, 0}, append([]byte("xterm"), iacIAC, iacSE)...)
	}
	return nil
}

func (s *telnetSession) reader() {
	buf := make([]byte, 8192)
	for {
		n, err := s.conn.Read(buf)
		if n > 0 {
			visible, reply := s.negotiate(buf[:n])
			if len(reply) > 0 {
				_ = s.write(reply)
			}
			if len(visible) > 0 {
				chunk := make([]byte, len(visible))
				copy(chunk, visible)
				s.debugf("<<< %s", chunk)
				s.out <- chunk
			}
		}
		if err != nil {
			s.done <- err
			close(s.out)
			return
		}
	}
}

// drain reads output until the device prompt reappears as the last line of
// the buffer, the stream goes idle for s.idle without a prompt, or maxWait
// elapses — identical strategy to the SSH transport (ssh.go). Returns the
// captured output, whether the drain ended "incomplete" (command did not
// finish), and the reason ("timeout": hard timeout fired; "closed": the
// connection ended mid-command — reported loudly instead of silently saving
// a truncated config).
func (s *telnetSession) drain(maxWait time.Duration) (string, bool, string) {
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
			if morePromptRe.MatchString(lastLine(buf.Bytes())) {
				s.debugf(">>> [space]\n")
				_ = s.write([]byte(" "))
			}
			if endsWithPrompt(buf.Bytes()) {
				return buf.String(), false, ""
			}
		case <-idle.C:
			if endsWithPasswordPrompt(buf.Bytes()) {
				return buf.String(), false, ""
			}
			if morePromptRe.MatchString(lastLine(buf.Bytes())) {
				s.debugf(">>> [space]\n")
				_ = s.write([]byte(" "))
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
// the shared cleanOutput helper (ssh.go:230) so telnet and ssh output agree.
// If the device responds with a Password: prompt (as happens when a driver sends
// "enable" to enter privileged mode on Cisco IOS), SendCommand automatically sends
// Target.Enable and reads the follow-up output.
func (s *telnetSession) SendCommand(cmd string) (string, error) {
	s.debugf(">>> %s\n", cmd)
	deadline := time.Now().Add(s.readTimeout)
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return "", err
	}
	if err := s.write([]byte(cmd + "\r\n")); err != nil {
		return "", err
	}
	raw, timedOut, reason := s.drain(s.readTimeout)
	// Even on timeout, check whether the device is waiting for an enable
	// password (Cisco IOS prompts for one after "enable"). The idle timer
	// fires while the device waits for input, so we must respond to the
	// password prompt BEFORE reporting a timeout.
	if s.enable != "" && endsWithPasswordPrompt([]byte(raw)) {
		raw = s.respondToEnablePrompt(raw)
		// Re-check: if drain returned incomplete for the enable-response
		// drain too, report a timeout. Otherwise return cleaned output.
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

// respondToEnablePrompt detects an interactive Password: prompt (triggered by the
// "enable" Init command on Cisco IOS) and, if an enable password is configured,
// sends it and reads the follow-up output. If no enable password is set, the raw
// output is returned unchanged so callers see the prompt text.
func (s *telnetSession) respondToEnablePrompt(raw string) string {
	if s.enable == "" {
		return raw
	}
	if !endsWithPasswordPrompt([]byte(raw)) {
		return raw
	}
	s.debugf(">>> [enable password]\n")
	if err := s.write([]byte(s.enable + "\r\n")); err != nil {
		return raw
	}
	raw += func() string {
		s2, _, _ := s.drain(s.readTimeout)
		return s2
	}()
	return raw
}

// Close implements Session.
func (s *telnetSession) Close() error {
	_ = s.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = s.write([]byte("exit\r\n"))
	return s.conn.Close()
}
