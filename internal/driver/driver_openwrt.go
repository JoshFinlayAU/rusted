package driver

import (
	"encoding/base64"
	"strings"
)

// postProcessOpenWRT formats the concatenated device dump entirely driver-side
// so the device needs nothing beyond ash builtins and cat:
//
//   - sections are introduced by "## <path>" marker lines emitted by the
//     Config commands;
//   - any section whose first 4096 bytes contain a NUL byte is considered
//     binary and replaced by its marker, a "## binary file, base64 follows"
//     note and the stdlib base64 of the EXACT bytes received (the exec
//     channel is byte-safe: no PTY, no CRLF folding);
//   - text sections pass through untouched.
//
// This replaces the previous od/head/sort/find pipeline: none of those applets
// are guaranteed to exist in a given busybox build.
func postProcessOpenWRT(raw string) string {
	type section struct {
		path string
		body []string
	}
	var secs []*section
	var cur *section
	for _, ln := range strings.Split(raw, "\n") {
		if strings.HasPrefix(ln, "## ") {
			cur = &section{path: strings.TrimSpace(strings.TrimPrefix(ln, "## "))}
			secs = append(secs, cur)
			continue
		}
		if cur != nil {
			cur.body = append(cur.body, ln)
		}
	}

	var b strings.Builder
	for _, s := range secs {
		content := strings.Join(s.body, "\n")
		b.WriteString("\n## ")
		b.WriteString(s.path)
		b.WriteString("\n")
		if i := strings.IndexByte(content, 0); i >= 0 && i < 4096 {
			// Binary: emit the EXACT bytes received (the engine adds no
			// framing newline for this driver, so nothing was mangled).
			b.WriteString("## binary file, base64 follows\n")
			b.WriteString(wrapBase64(content))
			b.WriteString("\n")
			continue
		}
		// Text: drop the trailing blank line(s) the shell echo framing adds.
		body := s.body
		for len(body) > 0 && strings.TrimRight(body[len(body)-1], " \t") == "" {
			body = body[:len(body)-1]
		}
		b.WriteString(strings.Join(body, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// wrapBase64 encodes s as standard base64 wrapped at 76 columns, matching what
// coreutils' base64 emits.
func wrapBase64(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}
