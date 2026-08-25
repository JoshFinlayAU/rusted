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
	// Transport is this platform's preferred transport when a device doesn't pin
	// its own. Empty = the engine default (interactive ssh). Set "ssh-exec" for a
	// platform whose interactive CLI is unreliable to script (e.g. MikroTik, whose
	// "/export" starves the interactive reader while it gathers the config).
	Transport string
	// Init commands run once after login, e.g. to disable paging.
	Init []string
	// Config commands whose combined output forms the saved configuration.
	Config []string
	// Strip drops whole matching lines from the captured config. Use for
	// volatile lines such as "Building configuration...", "!Time: ..." or
	// FortiOS's rotating "set password ENC ..." secrets. These rules define
	// what is ignored by default; --raw skips them entirely.
	Strip []*regexp.Regexp
	// StripBlocks drops multi-line regions: from the first line matching
	// Start through the first following line matching End (both inclusive).
	// Use for volatile PEM blocks — FortiOS re-encrypts stored private keys
	// with fresh salt/IV on every save, so "set private-key \"-----BEGIN
	// ENCRYPTED PRIVATE KEY-----\" changes wholesale each dump. Skipped by
	// --raw like Strip.
	StripBlocks []*BlockStrip
	// BinarySafe marks drivers whose Config commands emit byte-exact output:
	// the engine then runs them over an exec transport with output
	// normalisation disabled (Target.RawOutput), so binary payloads survive.
	BinarySafe bool
	// PostProcess, when non-nil, rewrites the raw captured output BEFORE
	// Clean — used to do driver-side formatting that would otherwise need
	// applets on the device (e.g. OpenWrt encodes binary files as base64 in
	// Go instead of relying on od/base64 on busybox).
	PostProcess func(raw string) string
	// RawNormalize, when true, disables the generic dynamic-string normaliser
	// (timestamp/date/uptime masking). Leave false unless a platform's config
	// is corrupted by masking.
	RawNormalize bool
}

// BlockStrip is one multi-line region to drop; see Driver.StripBlocks.
type BlockStrip struct {
	Start *regexp.Regexp
	End   *regexp.Regexp
}

// Clean produces the canonical, change-stable form of a captured config:
//  1. drop whole volatile lines matched by the driver's Strip rules;
//  2. drop regions matched by the driver's StripBlocks rules;
//  3. mask inline dynamic strings (timestamps, dates, uptimes) so they do not
//     trigger spurious "changed" results — unless RawNormalize is set;
//  4. trim trailing whitespace and ensure a single trailing newline.
func (d Driver) Clean(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	skip := -1 // index into d.StripBlocks of the block being skipped, -1 = none
nextLine:
	for _, ln := range lines {
		if skip >= 0 {
			if d.StripBlocks[skip].End.MatchString(ln) {
				skip = -1
			}
			continue
		}
		for i, b := range d.StripBlocks {
			if b.Start.MatchString(ln) {
				skip = i
				continue nextLine
			}
		}
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

// CleanRaw is Clean without the change-stability steps: no Strip rules are
// applied and dynamic strings are not masked. Only line endings and trailing
// whitespace are normalised so a verbatim capture stays diff-friendly. Used by
// 'rusted backup run --raw' to store everything the device emitted.
func (d Driver) CleanRaw(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		out = append(out, strings.TrimRight(ln, " \t"))
	}
	return strings.Trim(strings.Join(out, "\n"), "\n") + "\n"
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
			Init:        []string{"enable", "terminal length 0", "terminal width 0"},
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
			// Capture over an exec channel, not the interactive shell: RouterOS
			// gathers the whole "/export" before emitting anything, which starves
			// the interactive idle-timeout reader and yields an empty capture
			// (rusted#2). `ssh host "/export terse"` reads to EOF and works.
			Transport: "ssh-exec",
			// "/export terse" emits one full path per line, which diffs far more
			// cleanly than the default sectioned output. Sensitive values are
			// hidden by default in v7 (good for a git-stored backup).
			Config: []string{"/export terse"},
			Strip: []*regexp.Regexp{
				re(`^\[[^\]]*\] >`),     // echoed prompt+command line ("[user@host] > /export terse")
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
			Name:        "openwrt",
			Description: "OpenWrt (/etc/config files + /etc/sysupgrade.conf entries; binaries as base64)",
			// Capture over the exec channel (no PTY): stdout is byte-exact, so
			// binary files survive the trip and the driver can base64 them
			// itself (BinarySafe disables transport output normalisation).
			Transport:  "ssh-exec",
			BinarySafe: true,
			// The device side uses ONLY ash builtins (for/case/read/test/echo)
			// plus cat — no od, find, sort, sed, head or base64 applets, which
			// are not guaranteed in every busybox build. All formatting
			// happens in postProcessOpenWRT on the rusted side.
			Config: []string{
				`for f in /etc/config/*; do [ -f "$f" ] && { echo; echo "## $f"; cat "$f"; }; done`,
				`w(){ for e in "$1"/*; do if [ -f "$e" ]; then echo; echo "## $e"; cat "$e"; elif [ -d "$e" ]; then w "$e"; fi; done; }; if [ -f /etc/sysupgrade.conf ]; then echo; echo "## /etc/sysupgrade.conf"; cat /etc/sysupgrade.conf; while read p; do case "$p" in ''|\#*) continue ;; esac; p="${p%/}"; for e in $p; do if [ -f "$e" ]; then echo; echo "## $e"; cat "$e"; elif [ -d "$e" ]; then w "$e"; fi; done; done < /etc/sysupgrade.conf; fi; exit 0`,
			},
			PostProcess: postProcessOpenWRT,
			// Ignore lists are intentionally empty for now: nothing on a stock
			// OpenWrt is volatile enough to strip. Add Strip/StripBlocks rules
			// here when real-world noise shows up.
		},
		{
			Name:        "fortinet",
			Description: "Fortinet FortiOS",
			Init:        []string{"config system console", "set output standard", "end"},
			Config:      []string{"show full-configuration"},
			// FortiGate rewrites these on every save even when nothing changed:
			// conf_file_ver is a save counter, and stored secrets are re-encrypted
			// with fresh ciphertext each time ("password ENC <blob>"). The
			// ciphertext is not reversible, so it is useless in a backup diff.
			Strip: []*regexp.Regexp{
				re(`^#conf_file_ver=`),
				re(`^\s*set\s+\S+\s+ENC\s+\S+`),
			},
			// Only ENCRYPTED PEM blocks (PKCS#8 private keys) are skipped: they
			// are re-encrypted with a fresh salt/IV on every save, so their
			// base64 never matches between dumps. Plain certificates, CSRs and
			// public keys are deterministic and stay in the backup.
			StripBlocks: []*BlockStrip{
				{
					Start: re(`^\s*set\s+\S+\s+"-----BEGIN ENCRYPTED [A-Z ]+-----`),
					End:   re(`-----END ENCRYPTED [A-Z ]+-----"?\.?\s*$`),
				},
			},
		},
	}
}
