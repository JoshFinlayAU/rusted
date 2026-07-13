// RouterOS API transport: reaches a MikroTik device over its binary API (port 8728,
// or 8729 for API-SSL) instead of SSH, and runs the driver's commands as API sentences.
// Pair it with the mikrotik_routeros_api driver, whose config command is "/export".
//
// This exists because some operators disable SSH on RouterOS but keep the API enabled.
// It is read-only (it only runs /export) and safe, but "/export" over the API is a
// RouterOS 7 behaviour - validate against your gear before relying on it in production.
package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/go-routeros/routeros/v3"
)

// RouterOS implements Transport over the MikroTik binary API.
type RouterOS struct{}

func init() { Register(&RouterOS{}) }

func (*RouterOS) Name() string { return "routeros-api" }

func (*RouterOS) Dial(ctx context.Context, t Target) (Session, error) {
	port := t.Port
	if port == 0 {
		port = 8728
	}
	addr := fmt.Sprintf("%s:%d", t.Host, port)
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	var (
		client *routeros.Client
		err    error
	)
	if port == 8729 {
		client, err = routeros.DialTLS(addr, t.Username, t.Password, &tls.Config{InsecureSkipVerify: true})
	} else {
		client, err = routeros.DialTimeout(addr, t.Username, t.Password, timeout)
	}
	if err != nil {
		return nil, err
	}
	return &routerosSession{client: client}, nil
}

type routerosSession struct {
	client *routeros.Client
}

// SendCommand runs a console-style command (e.g. "/export") as an API sentence and
// returns the reply text. Init commands like paging-off are no-ops over the API.
func (s *routerosSession) SendCommand(cmd string) (string, error) {
	words := apiWords(cmd)
	if len(words) == 0 {
		return "", nil
	}
	reply, err := s.client.RunArgs(words)
	if err != nil {
		return "", err
	}
	return replyText(reply), nil
}

func (s *routerosSession) Close() error {
	s.client.Close()
	return nil
}

// apiWords turns a console-style command ("/export terse") into API words. The leading
// path is passed through; a bare argument ("terse") becomes an API flag ("=terse=");
// anything already API-shaped (=x=, ?x, .x) is passed through untouched.
func apiWords(cmd string) []string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	out = append(out, fields[0])
	for _, f := range fields[1:] {
		switch f[0] {
		case '=', '?', '.':
			out = append(out, f)
		default:
			out = append(out, "="+f+"=")
		}
	}
	return out
}

// replyText flattens an API reply into config text. RouterOS returns "/export" output
// across the reply's sentences; collect every attribute value (and the trailing ret).
func replyText(reply *routeros.Reply) string {
	var b strings.Builder
	for _, sen := range reply.Re {
		for _, pair := range sen.List {
			if pair.Value != "" {
				b.WriteString(pair.Value)
				b.WriteByte('\n')
			}
		}
	}
	if reply.Done != nil {
		if ret, ok := reply.Done.Map["ret"]; ok && ret != "" {
			b.WriteString(ret)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
