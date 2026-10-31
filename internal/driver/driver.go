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
	// volatile lines such as "Building configuration..." or "!Time: ...".
	Strip []*regexp.Regexp
	// StripBlocks drops (or masks) multi-line volatile regions, such as the
	// PEM private keys FortiOS re-encrypts with a fresh salt/IV on every save.
	// See BlockStrip.
	StripBlocks []*BlockStrip
	// Mask rewrites matching lines in place (Match.ReplaceAllString(line,
	// With)) to hide a volatile value while keeping the line itself, e.g.
	// FortiOS "set password ENC <ciphertext>".
	Mask []*MaskRule
	// RawNormalize, when true, disables the generic dynamic-string normaliser
	// (timestamp/date/uptime masking). Leave false unless a platform's config
	// is corrupted by masking.
	RawNormalize bool
	// BinarySafe marks a driver whose Config output must arrive byte-exact:
	// the engine asks the transport not to normalise it (Target.RawOutput) and
	// adds no framing newline between commands. Pair it with an exec
	// transport; an interactive PTY cannot carry binary data.
	BinarySafe bool
	// PostProcess, when non-nil, turns the combined Config output into the
	// configuration text before Clean, e.g. to base64 binary files on the
	// rusted side instead of relying on applets the device may lack. An
	// error fails the backup (nothing is committed).
	PostProcess func(raw string) (string, error)
	// Prepare, when non-nil, is called once per backup with the registered
	// driver and returns the one to use for that run. It lets a driver bake a
	// per-run value into its Config commands and PostProcess (openwrt's random
	// file boundary).
	Prepare func(Driver) Driver
}

// BlockStrip is one multi-line region: from a line matching Start through the
// first line matching End, both inclusive. End is first tried on the rest of
// the Start line after the Start match, so a region opened and closed on one
// line ends there. A region whose End never matches is left untouched: a
// missing terminator must never silently drop the rest of the config.
type BlockStrip struct {
	Start *regexp.Regexp
	End   *regexp.Regexp
	// Mask, when non-empty, keeps the Start and End lines and replaces the
	// lines between them with this single line, so the config's structure
	// stays visible. Empty drops the whole region.
	Mask string
}

// MaskRule replaces Match in a line with With (regexp replacement syntax, so
// ${1} keeps a capture group).
type MaskRule struct {
	Match *regexp.Regexp
	With  string
}

// Clean produces the canonical, change-stable form of a captured config:
//  1. drop or mask multi-line regions matched by the driver's StripBlocks;
//  2. drop whole volatile lines matched by the driver's Strip rules;
//  3. mask volatile values in place with the driver's Mask rules;
//  4. mask inline dynamic strings (timestamps, dates, uptimes) so they do not
//     trigger spurious "changed" results — unless RawNormalize is set;
//  5. trim trailing whitespace and ensure a single trailing newline.
func (d Driver) Clean(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(d.StripBlocks) > 0 {
		lines = d.stripBlocks(lines)
	}
	out := make([]string, 0, len(lines))
nextLine:
	for _, ln := range lines {
		for _, re := range d.Strip {
			if re.MatchString(ln) {
				continue nextLine
			}
		}
		for _, m := range d.Mask {
			ln = m.Match.ReplaceAllString(ln, m.With)
		}
		out = append(out, strings.TrimRight(ln, " \t"))
	}
	joined := strings.Trim(strings.Join(out, "\n"), "\n")
	if !d.RawNormalize {
		joined = normalize.Apply(joined)
	}
	return joined + "\n"
}

// stripBlocks applies the StripBlocks rules to lines; see BlockStrip.
func (d Driver) stripBlocks(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		end := -1
		var blk *BlockStrip
		for _, b := range d.StripBlocks {
			loc := b.Start.FindStringIndex(ln)
			if loc == nil {
				continue
			}
			if b.End.MatchString(ln[loc[1]:]) {
				end = i
			} else {
				for j := i + 1; j < len(lines); j++ {
					if b.End.MatchString(lines[j]) {
						end = j
						break
					}
				}
			}
			if end >= 0 {
				blk = b
				break
			}
		}
		if blk == nil {
			out = append(out, ln) // no region here, or an unterminated one: keep
			continue
		}
		if blk.Mask != "" {
			out = append(out, ln)
			if end > i+1 {
				out = append(out, blk.Mask)
			}
			if end > i {
				out = append(out, lines[end])
			}
		}
		i = end
	}
	return out
}

// CleanRaw is the verbatim counterpart of Clean, used by 'rusted backup run
// --raw' to show exactly what the device sent: no StripBlocks, Strip or Mask
// rules and no dynamic-string masking. Only line endings and trailing
// whitespace are normalised.
func (d Driver) CleanRaw(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n") + "\n"
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
			Description: "OpenWrt (/etc/config + /etc/sysupgrade.conf entries; binaries as base64)",
			// Capture over the exec channel (no PTY) so stdout is byte-exact and
			// binary files survive; the device only runs read-only shell
			// builtins plus cat and ls, and all formatting happens on the rusted
			// side. Config and PostProcess are filled per run by Prepare. See
			// driver_openwrt.go.
			Transport:  "ssh-exec",
			BinarySafe: true,
			Prepare:    func(d Driver) Driver { return openwrtRun(d, newBoundary()) },
			// Files are stored as they are on the device: never rewrite a date
			// inside a script or config file.
			RawNormalize: true,
		},
		{
			Name:        "fortinet",
			Description: "Fortinet FortiOS",
			Init:        []string{"config system console", "set output standard", "end"},
			Config:      []string{"show full-configuration"},
			// FortiOS rewrites these on every save even when nothing changed:
			// conf_file_ver is a save counter, and stored secrets are
			// re-encrypted with fresh ciphertext each time. The ciphertext is
			// masked rather than dropped so the line (and a secret being added
			// or removed) still shows in the backup; a changed secret does not.
			Strip: []*regexp.Regexp{re(`^#conf_file_ver=`)},
			Mask: []*MaskRule{
				{Match: re(`^(\s*set\s+\S+\s+ENC\s+)\S+`), With: "${1}" + maskEncrypted},
			},
			// Only ENCRYPTED PEM blocks (PKCS#8 private keys) are volatile: they
			// get a fresh salt/IV on every save. Plain certificates and public
			// keys are deterministic and are kept verbatim.
			StripBlocks: []*BlockStrip{{
				Start: re(`^\s*set\s+\S+\s+"-----BEGIN ENCRYPTED [A-Z ]+-----`),
				End:   re(`-----END ENCRYPTED [A-Z ]+-----`),
				Mask:  maskEncrypted,
			}},
		},
	}
}

// maskEncrypted stands in for ciphertext that changes on every save.
const maskEncrypted = "<ENCRYPTED>"
