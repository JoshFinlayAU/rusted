package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// Telnet speaks raw TCP (RFC 854/855) to devices that only offer a telnet CLI:
// older Cisco IOS, console servers, lab gear. Dial performs the in-band
// username/password login; after that the device is driven exactly like the
// interactive SSH transport (shared prompt detection and idle fallback).
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
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	s := &telnetSession{conn: conn, writeTimeout: t.Timeout}
	s.shell = newShell(t, "\r\n", func(p string) error { return s.write([]byte(p)) })
	go s.reader()

	// Unlike SSH, authentication happens inside the session stream. login
	// consumes the banner itself; draining first would eat the Username: prompt.
	if t.Username != "" || t.Password != "" {
		if err := s.login(ctx, t.Username, t.Password); err != nil {
			s.stop()
			conn.Close()
			return nil, err
		}
	} else {
		// No login: consume the banner / initial prompt so it does not
		// contaminate the first command's output, as the SSH transport does.
		s.drain("", 10*time.Second)
	}
	return s, nil
}

// telnetSession is a telnet CLI session; the command loop (SendCommand,
// prompt detection) lives in shell.
type telnetSession struct {
	*shell
	conn         net.Conn
	writeTimeout time.Duration
	wmu          sync.Mutex
	pend         []byte // partial IAC sequence carried over to the next read
}

// login answers the device's Username: and Password: prompts and returns once
// the CLI prompt appears. A device with no login prompt at all (no AAA) is
// accepted as soon as its prompt shows. The whole exchange is bounded by the
// per-command timeout and by ctx.
func (s *telnetSession) login(ctx context.Context, username, password string) error {
	const (
		awaitLogin = iota // nothing sent yet
		sentUser          // username sent, expecting Password:
		sentPass          // password sent, expecting the CLI prompt
	)
	hard := time.NewTimer(s.readTimeout)
	defer hard.Stop()
	idle := time.NewTimer(s.idle)
	defer idle.Stop()

	var buf bytes.Buffer
	stage := awaitLogin
	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				return fmt.Errorf("telnet: connection closed during login (output: %q)", tail(buf.Bytes(), 100))
			}
			buf.Write(chunk)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(s.idle)
			b := buf.Bytes()
			switch {
			case trailingMatches(b, passwordPrompt):
				if stage == sentPass {
					// Asked again: the password was wrong. Resending it on
					// every re-prompt would only burn login attempts.
					return errors.New("telnet: login failed: password rejected")
				}
				if password == "" {
					return errors.New("telnet: device asked for a password but the credential has none")
				}
				tracef(s.debug, ">>> [password]\n")
				if err := s.write([]byte(password + "\r\n")); err != nil {
					return err
				}
				stage = sentPass
				buf.Reset()
			case trailingMatches(b, usernamePrompt):
				if stage != awaitLogin {
					return fmt.Errorf("telnet: login failed (output: %q)", tail(b, 100))
				}
				if username == "" {
					return errors.New("telnet: device asked for a username but the credential has none")
				}
				tracef(s.debug, ">>> %s\n", username)
				if err := s.write([]byte(username + "\r\n")); err != nil {
					return err
				}
				stage = sentUser
				buf.Reset()
			case endsWithPrompt(b):
				return nil
			}
		case <-idle.C:
			// Logged in to a prompt promptRe does not know: the device said
			// something after the password and then waited.
			if stage == sentPass && hasReply(buf.Bytes(), "") {
				return nil
			}
			idle.Reset(s.idle)
		case <-hard.C:
			return fmt.Errorf("telnet: login timed out after %s (output: %q)", s.readTimeout, tail(buf.Bytes(), 100))
		case <-ctx.Done():
			return fmt.Errorf("telnet: login: %w", ctx.Err())
		}
	}
}

// write sends p under a write deadline, so a peer that stops reading cannot
// wedge the session, and clears the deadline afterwards.
func (s *telnetSession) write(p []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return err
	}
	_, err := s.conn.Write(p)
	_ = s.conn.SetWriteDeadline(time.Time{})
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
			if len(visible) > 0 && !s.deliver(visible) {
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
func (s *telnetSession) Close() error {
	_ = s.write([]byte("exit\r\n"))
	s.stop()
	return s.conn.Close()
}
