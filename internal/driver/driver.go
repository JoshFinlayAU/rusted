// Package driver encapsulates per-operating-system knowledge: which commands
// disable terminal paging, which commands dump the running configuration, and
// how to strip volatile lines so that an unchanged config produces an
// unchanged file (and therefore no spurious git commit).
//
// Drivers are data-driven and registered by name. Adding support for a new
// platform is usually a matter of appending a Driver literal in builtins().
package driver

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/athenanetworks/rusted/internal/normalize"
)

// Driver describes how to extract a configuration from one platform.
type Driver struct {
	Name        string
	Description string
	// Init commands run once after login, e.g. to disable paging.
	Init []string
	// Config commands whose combined output forms the saved configuration.
	Config []string
	// Strip drops whole matching lines from the captured config. Use for
	// volatile lines such as "Building configuration..." or "!Time: ...".
	Strip []*regexp.Regexp
	// RawNormalize, when true, disables the generic dynamic-string normaliser
	// (timestamp/date/uptime masking). Leave false unless a platform's config
	// is corrupted by masking.
	RawNormalize bool
}

// Clean produces the canonical, change-stable form of a captured config:
//  1. drop whole volatile lines matched by the driver's Strip rules;
//  2. mask inline dynamic strings (timestamps, dates, uptimes) so they do not
//     trigger spurious "changed" results — unless RawNormalize is set;
//  3. trim trailing whitespace and ensure a single trailing newline.
func (d Driver) Clean(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
nextLine:
	for _, ln := range lines {
		for _, re := range d.Strip {
			if re.MatchString(ln) {
				continue nextLine
			}
		}
		out = append(out, strings.TrimRight(ln, " \t"))
	}
	joined := strings.Trim(strings.Join(out, "\n"), "\n")
	if !d.RawNormalize {
		joined = normalize.Apply(joined)
	}
	return joined + "\n"
}

var (
	mu       sync.RWMutex
	registry = map[string]Driver{}
)

// Register adds a driver to the registry (last write wins, allowing overrides).
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	registry[d.Name] = d
}

// Get returns the named driver, falling back to the generic driver when the
// name is unknown so a misconfigured device still produces something.
func Get(name string) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[name]
	if !ok {
		return registry["generic"], false
	}
	return d, true
}

// List returns all registered drivers sorted by name.
func List() []Driver {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Driver, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func re(p string) *regexp.Regexp { return regexp.MustCompile(p) }

func init() {
	for _, d := range builtins() {
		Register(d)
	}
}

func builtins() []Driver {
	return []Driver{
		{
			Name:        "generic",
			Description: "Unknown platform: run 'show running-config' with no cleanup",
			Config:      []string{"show running-config"},
		},
		{
			Name:        "cisco_ios",
			Description: "Cisco IOS / IOS-XE",
			Init:        []string{"terminal length 0", "terminal width 0"},
			Config:      []string{"show running-config"},
			Strip: []*regexp.Regexp{
				re(`^Building configuration`),
				re(`^Current configuration`),
				re(`^! Last configuration change`),
				re(`^! NVRAM config last updated`),
				re(`^ntp clock-period`),
			},
		},
		{
			Name:        "cisco_nxos",
			Description: "Cisco NX-OS",
			Init:        []string{"terminal length 0"},
			Config:      []string{"show running-config"},
			Strip: []*regexp.Regexp{
				re(`^!Time:`),
				re(`^!Running configuration last done at`),
				re(`^!Startup config saved at`),
			},
		},
		{
			Name:        "cisco_asa",
			Description: "Cisco ASA",
			Init:        []string{"terminal pager 0"},
			Config:      []string{"show running-config"},
			Strip: []*regexp.Regexp{
				re(`^: Written by`),
				re(`^: Saved`),
				re(`^Cryptochecksum:`),
			},
		},
		{
			Name:        "arista_eos",
			Description: "Arista EOS",
			Init:        []string{"terminal length 0"},
			Config:      []string{"show running-config"},
			Strip:       []*regexp.Regexp{re(`^! device:`)},
		},
		{
			Name:        "juniper_junos",
			Description: "Juniper Junos",
			Init:        []string{"set cli screen-length 0", "set cli screen-width 0"},
			Config:      []string{"show configuration | display set"},
			Strip:       []*regexp.Regexp{re(`^## Last commit:`)},
		},
		{
			Name:        "mikrotik_routeros",
			Description: "MikroTik RouterOS v7+",
			// "/export terse" emits one full path per line, which diffs far more
			// cleanly than the default sectioned output. Sensitive values are
			// hidden by default in v7 (good for a git-stored backup).
			Config: []string{"/export terse"},
			Strip: []*regexp.Regexp{
				re(`^# .* by RouterOS`), // leading "# <timestamp> by RouterOS 7.x"
				re(`^# software id =`),
				re(`^# model =`),
				re(`^# serial number =`),
			},
		},
		{
			// DRAFT - drafted from Cambium's ePMP CLI guide + the oxidized cambiumepmp
			// model; validate against real gear before relying on it. ePMP dumps its whole
			// config as JSON over SSH ("config show json"). The cfgUtcTimestamp field is
			// volatile, so drop that line (assumes pretty-printed JSON, one field per line -
			// confirm the output format on your firmware). RawNormalize so the generic
			// timestamp masker doesn't rewrite JSON values.
			Name:         "cambium_epmp",
			Description:  "Cambium ePMP (SSH CLI, JSON export) - DRAFT, validate against gear",
			Config:       []string{"config show json"},
			Strip:        []*regexp.Regexp{re(`cfgUtcTimestamp`)},
			RawNormalize: true,
		},
		{
			// DRAFT - cnMatrix is a Cisco-like switch NOS; "show running-config" over SSH is
			// the expected dump. The paging-disable command below is a best guess - if your
			// cnMatrix pages output (a "--More--" prompt), adjust the Init command to whatever
			// it uses. Validate against real gear.
			Name:        "cambium_cnmatrix",
			Description: "Cambium cnMatrix switch (Cisco-like CLI) - DRAFT, validate against gear",
			Init:        []string{"terminal length 0"},
			Config:      []string{"show running-config"},
			Strip: []*regexp.Regexp{
				re(`^Building configuration`),
				re(`^Current configuration`),
			},
		},
		{
			Name:        "vyos",
			Description: "VyOS / Vyatta",
			Init:        []string{"set terminal length 0"},
			Config:      []string{"show configuration commands"},
		},
		{
			Name:        "fortinet",
			Description: "Fortinet FortiOS",
			Init:        []string{"config system console", "set output standard", "end"},
			Config:      []string{"show full-configuration"},
		},
	}
}
