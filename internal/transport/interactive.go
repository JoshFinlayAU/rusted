package transport

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// shell is the prompt-driven command loop shared by the interactive
// transports (ssh, telnet). The transport owns the connection: its reader
// goroutine hands device output to deliver and closes out when the stream
// ends, and send writes keystrokes. shell decides when a command is done.
type shell struct {
	send        func(string) error // write raw keystrokes to the device
	eol         string             // line terminator appended to commands
	out         chan []byte        // device output; closed when the stream ends
	quit        chan struct{}      // closed by stop; releases a blocked reader
	quitOnce    sync.Once
	idle        time.Duration
	readTimeout time.Duration
	enable      string
	debug       io.Writer
	erase       bool // a pager prompt was just answered; trim its erase sequence
}

func newShell(t Target, eol string, send func(string) error) *shell {
	return &shell{
		send:        send,
		eol:         eol,
		out:         make(chan []byte, 64),
		quit:        make(chan struct{}),
		idle:        idleTimeout(t),
		readTimeout: cmdTimeout(t),
		enable:      t.Enable,
		debug:       syncTrace(t.Debug),
	}
}

// deliver passes one chunk of device output to drain. It returns false once
// the session has been closed: a device that keeps talking after the last
// read would otherwise leave the reader blocked on out forever.
func (s *shell) deliver(chunk []byte) bool {
	tracef(s.debug, "<<< %s", chunk)
	select {
	case s.out <- chunk:
		return true
	case <-s.quit:
		return false
	}
}

func (s *shell) stop() { s.quitOnce.Do(func() { close(s.quit) }) }

// How a drain ended.
type drainEnd int

const (
	endPrompt   drainEnd = iota // a bare device prompt is the last thing on screen
	endIdle                     // the device replied, then went quiet
	endPassword                 // the device is waiting at a Password: prompt
	endTimeout                  // the hard per-command timeout fired
	endClosed                   // the stream ended mid-command
)

// drain collects output until a bare device prompt (see trailingMatches)
// closes it, or the device has replied (anything beyond the echo of cmd) and
// then stayed quiet for s.idle. The idle fallback is what keeps prompts the
// regex does not know from hanging until maxWait. A Password: prompt at the
// tail ends the drain at once so the caller can answer it, and a pager prompt
// at the tail is answered here with a space and cut from the capture.
func (s *shell) drain(cmd string, maxWait time.Duration) (string, drainEnd) {
	var buf bytes.Buffer
	idle := time.NewTimer(s.idle)
	defer idle.Stop()
	hard := time.NewTimer(maxWait)
	defer hard.Stop()
	for {
		select {
		case chunk, ok := <-s.out:
			if !ok {
				return buf.String(), endClosed
			}
			if s.erase {
				chunk = pagerEraseRe.ReplaceAll(chunk, nil)
				s.erase = len(chunk) == 0
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
			case trailingMatches(b, pagerRe):
				buf.Truncate(lastLineStart(b))
				tracef(s.debug, ">>> [space]\n")
				if err := s.send(" "); err != nil {
					return buf.String(), endClosed
				}
				s.erase = true
			case trailingMatches(b, promptRe):
				return buf.String(), endPrompt
			case trailingMatches(b, passwordPrompt):
				return buf.String(), endPassword
			}
		case <-idle.C:
			if hasReply(buf.Bytes(), cmd) {
				return buf.String(), endIdle
			}
			idle.Reset(s.idle)
		case <-hard.C:
			return buf.String(), endTimeout
		}
	}
}

// SendCommand implements Session. If the command leaves the device at a
// Password: prompt (a driver's "enable"), the credential's enable password is
// sent and the rest of the output collected; with no enable password set that
// is an error, since anything typed next would be taken as the password.
func (s *shell) SendCommand(cmd string) (string, error) {
	tracef(s.debug, ">>> %s\n", cmd)
	if err := s.send(cmd + s.eol); err != nil {
		return "", err
	}
	raw, end := s.drain(cmd, s.readTimeout)
	if end == endPassword {
		if s.enable == "" {
			return "", fmt.Errorf("command %q: device prompted for a password and the credential has no enable password", cmd)
		}
		tracef(s.debug, ">>> [enable password]\n")
		if err := s.send(s.enable + s.eol); err != nil {
			return "", err
		}
		var more string
		more, end = s.drain("", s.readTimeout)
		raw += more
		if end == endPassword {
			return "", fmt.Errorf("command %q: enable password rejected", cmd)
		}
	}
	switch end {
	case endTimeout:
		return "", fmt.Errorf("command %q timed out after %s", cmd, s.readTimeout)
	case endClosed:
		return "", fmt.Errorf("command %q did not complete: device closed the session mid-output (captured %d bytes)", cmd, len(raw))
	}
	return cleanOutput(cmd, raw), nil
}

// passwordPrompt matches "Password:" / "Passphrase:", for the telnet login and
// for "enable". The colon is required so text that merely mentions a password
// cannot match.
var passwordPrompt = regexp.MustCompile(`(?i)pass(word|phrase)\s*:$`)

// usernamePrompt matches a login prompt: "Username:", "login:", or
// "<hostname> login:" as NX-OS and Linux print it. It is anchored to the whole
// line, so banner text that ends in "log in:" does not trigger it.
var usernamePrompt = regexp.MustCompile(`(?i)^\s*(?:[\w.\-]+ )?(?:user ?name|user|login)\s*:$`)

// pagerRe matches a pager prompt that is the whole line: "--More--",
// " -- More -- ", "<--- More --->", "---(more 25%)---".
var pagerRe = regexp.MustCompile(`(?i)^\s*<?-+\s*\(?more\b[^-]*-+>?$`)

// pagerEraseRe matches what a device prints to wipe its pager prompt after
// the space: backspaces over spaces (IOS), a carriage-return blank-out, or an
// erase-line sequence (NX-OS). Only applied to the chunk right after a pager.
var pagerEraseRe = regexp.MustCompile(`^(?:\x08+ *\x08*|\r *\r|\r?\x1b\[[0-9;]*K)+`)

// trailingMatches reports whether re matches the bare last line of b: the
// text after the final line break, with ANSI escapes and trailing blanks
// removed. A real prompt is printed without a terminating newline while the
// device waits for input, whereas every content line ends in one, so
// prompt-shaped lines inside a config dump (banners, motd, HTML) never match.
func trailingMatches(b []byte, re *regexp.Regexp) bool {
	line := b[lastLineStart(b):]
	if bytes.IndexByte(line, 0x1b) >= 0 {
		line = ansiRe.ReplaceAll(line, nil)
	}
	line = bytes.TrimRight(line, " \t")
	return len(line) > 0 && re.Match(line)
}

// lastLineStart is the offset just past the final \n or \r in b.
func lastLineStart(b []byte) int {
	return bytes.LastIndexAny(b, "\r\n") + 1
}

// endsWithPrompt reports whether b ends with a bare device prompt.
func endsWithPrompt(b []byte) bool { return trailingMatches(b, promptRe) }

// hasReply reports whether b holds anything beyond the (possibly partial)
// echo of cmd, i.e. whether the device has actually started answering. A
// command that is slow to produce its first line (NX-OS building its running
// config) must not be cut off by the idle timer straight after the echo.
func hasReply(b []byte, cmd string) bool {
	got := strings.TrimSpace(ansiRe.ReplaceAllString(string(b), ""))
	return got != "" && !strings.HasPrefix(strings.TrimSpace(cmd), got)
}
