// Package normalize masks dynamic strings — timestamps, dates, uptimes — that
// appear inside an otherwise-static device configuration.
//
// Network operating systems frequently embed volatile data in their config
// output: "! Last configuration change at 10:02:11 UTC Tue Jun 16 2026",
// "# 2026-06-16 10:02:11 by RouterOS 7.x", "uptime is 4 days, 2 hours". If
// that data is stored verbatim, every backup looks different from the last and
// produces a meaningless git commit. Per-driver line stripping handles whole
// volatile lines; this package handles dynamic substrings embedded in lines we
// otherwise want to keep.
//
// The replacement is a visible placeholder (e.g. <TIMESTAMP>) rather than
// deletion, so a human reading the backup can still see that a timestamp was
// present — only its volatile value is removed from change detection.
package normalize

import "regexp"

// Placeholders substituted for dynamic content.
const (
	phTimestamp = "<TIMESTAMP>"
	phDate      = "<DATE>"
	phTime      = "<TIME>"
	phUptime    = "<UPTIME>"
)

type rule struct {
	re   *regexp.Regexp
	repl string
}

// Rules are applied in order. More specific (longer) patterns come first so
// they win before a broader pattern can partially match the same text.
var rules = []rule{
	// Cisco-style "time-first" stamp: "10:02:11 UTC Tue Jun 16 2026"
	{regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(\.\d+)?\s+[A-Z]{2,4}\s+((Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+)?(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{4}`), phTimestamp},

	// Full ISO-8601 date-time: 2026-06-16T10:02:11.5Z, 2026-06-16 10:02:11 +10:00
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(\s?(Z|UTC|GMT|[A-Z]{2,4}|[+-]\d{2}:?\d{2}))?`), phTimestamp},

	// Verbose date-time: "Tue Jun 16 10:02:11 UTC 2026", "Jun 16 10:02:11 2026"
	{regexp.MustCompile(`\b((Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+)?(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?(\s+[A-Z]{2,4})?\s+\d{4}`), phTimestamp},

	// Verbose date without time: "Tue Jun 16 2026", "Jun 16, 2026"
	{regexp.MustCompile(`\b((Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+)?(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2},?\s+\d{4}`), phDate},

	// Bare ISO date: 2026-06-16
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`), phDate},

	// Time with an explicit timezone or offset (high confidence it is a clock,
	// not a config value): "10:02:11 UTC", "10:02:11.5 +10:00"
	{regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(\.\d+)?\s+(Z|UTC|GMT|[A-Z]{2,4}|[+-]\d{2}:?\d{2})\b`), phTime},

	// Uptime phrases: "uptime is 4 days, 2 hours, 11 minutes", "up 1 week, 3 days"
	{regexp.MustCompile(`(?i)\b(uptime(\s+is)?|up)\s+\d+\s+(year|week|day|hour|min(ute)?|sec(ond)?)s?(,?\s+\d+\s+(year|week|day|hour|min(ute)?|sec(ond)?)s?)*`), phUptime},
}

// Apply replaces dynamic substrings in s with stable placeholders.
func Apply(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
