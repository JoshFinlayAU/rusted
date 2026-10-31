package driver

import (
	"regexp"
	"strings"
	"testing"

	"github.com/athenanetworks/rusted/internal/normalize"
)

// legacyClean is Clean as it was before StripBlocks and Mask existed. Drivers
// that use neither must still produce exactly these bytes.
func legacyClean(d Driver, raw string) string {
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

var cleanCorpus = []string{
	"",
	"\n\n",
	"Building configuration...\r\nCurrent configuration : 1520 bytes\r\n! Last configuration change at 10:02:11 UTC Tue Jun 16 2026 by admin\r\nhostname r1  \r\n!\r\nntp clock-period 17179862\r\n",
	"!Time: Tue Jun 16 10:02:11 2026\n!Running configuration last done at: Tue Jun 16 10:02:11 2026\n!Startup config saved at: Tue Jun 16 10:02:11 2026\nversion 9.3(8)\nhostname nx1\ninterface Ethernet1/1\n  description uplink\t\n",
	"## Last commit: 2026-06-16 10:02:11 UTC by admin\nset system host-name mx1\nset interfaces ge-0/0/0 unit 0 family inet address 10.0.0.1/30\n",
	"[admin@rb1] > /export terse\n# 2026-06-16 10:02:11 by RouterOS 7.15\n# software id = ABCD-1234\n# model = RB5009\n# serial number = HEX123\n/interface bridge add name=bridge1\n/ip address add address=10.0.0.1/24 interface=bridge1\n",
	"#conf_file_ver=1234\nconfig system admin\n    set password ENC SH2abcdef==\n    set private-key \"-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIabc\n-----END ENCRYPTED PRIVATE KEY-----\"\nend\n",
	"{\n  \"cfgUtcTimestamp\": \"1718532131\",\n  \"device\": \"epmp\"\n}\n",
	": Written by enable_15 at 10:02:11.123 UTC Tue Jun 16 2026\n: Saved\nCryptochecksum:abcdef\nhostname asa1\n",
	"System uptime is 4 days, 2 hours\n! device: sw1 (DCS-7050, EOS-4.30)\nset date 2026-06-16\n",
}

// Every driver that does not use StripBlocks or Mask (all but fortinet, and
// openwrt whose Clean is plain) must Clean byte-identically to before.
func TestCleanUnchangedForExistingDrivers(t *testing.T) {
	for _, d := range List() {
		if len(d.StripBlocks) > 0 || len(d.Mask) > 0 {
			if d.Name != "fortinet" {
				t.Errorf("%s: unexpected StripBlocks/Mask; extend this test", d.Name)
			}
			continue
		}
		for _, in := range cleanCorpus {
			if got, want := d.Clean(in), legacyClean(d, in); got != want {
				t.Errorf("%s: Clean(%q)\n got %q\nwant %q", d.Name, in, got, want)
			}
		}
	}
}

func blockDriver(mask string) Driver {
	return Driver{
		RawNormalize: true,
		StripBlocks: []*BlockStrip{{
			Start: regexp.MustCompile(`^BEGIN`),
			End:   regexp.MustCompile(`END$`),
			Mask:  mask,
		}},
	}
}

func TestStripBlocks(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"terminated", "a\nBEGIN\nx\ny\nEND\nb\n", "a\nb\n"},
		{"two blocks", "BEGIN\nx\nEND\na\nBEGIN\nEND\nb\n", "a\nb\n"},
		{"same line", "a\nBEGIN x END\nb\nc END\nd\n", "a\nb\nc END\nd\n"},
		{"unterminated kept", "a\nBEGIN\nx\ny\n", "a\nBEGIN\nx\ny\n"},
		// The first BEGIN finds the END further down: the whole span goes.
		{"nested begin", "BEGIN\na\nBEGIN\nb\nEND\nc\n", "c\n"},
	}
	d := blockDriver("")
	for _, c := range cases {
		if got := d.Clean(c.in); got != c.want {
			t.Errorf("%s: Clean(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestStripBlocksMask(t *testing.T) {
	d := blockDriver("<MASKED>")
	cases := []struct {
		name, in, want string
	}{
		{"body masked", "a\nBEGIN\nx\ny\nEND\nb\n", "a\nBEGIN\n<MASKED>\nEND\nb\n"},
		{"empty body", "BEGIN\nEND\n", "BEGIN\nEND\n"},
		{"same line kept", "BEGIN x END\nb\n", "BEGIN x END\nb\n"},
		{"unterminated kept", "a\nBEGIN\nx\n", "a\nBEGIN\nx\n"},
	}
	for _, c := range cases {
		if got := d.Clean(c.in); got != c.want {
			t.Errorf("%s: Clean(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func fortiConfig(ver, secret, keyBody string) string {
	return "#config-version=FG120G-7.4.12-FW-build2902-260505:opmode=0:vdom=1:user=netops\n" +
		"#conf_file_ver=" + ver + "\n" +
		"#buildno=2902\n" +
		"config system ha\n" +
		"    set group-name \"HACORE\"\n" +
		"    set password ENC " + secret + "\n" +
		"end\n" +
		"config vpn certificate local\n" +
		"    edit \"fw.lab.invalid\"\n" +
		"        set private-key \"-----BEGIN ENCRYPTED PRIVATE KEY-----\n" +
		keyBody +
		"-----END ENCRYPTED PRIVATE KEY-----\"\n" +
		"        set certificate \"-----BEGIN CERTIFICATE-----\n" +
		"MIIcertline1\nMIIcertline2\n" +
		"-----END CERTIFICATE-----\"\n" +
		"    next\n" +
		"end\n"
}

// FortiOS bumps conf_file_ver and re-encrypts every secret on each save; an
// unchanged FortiGate must Clean identically, with the secret lines and the
// key block still present, masked.
func TestFortinetMasksVolatileSecrets(t *testing.T) {
	d, ok := Get("fortinet")
	if !ok {
		t.Fatal("fortinet driver not registered")
	}
	a := d.Clean(fortiConfig("101", "06xAAAA", "MIIkeyA1\nMIIkeyA2\n"))
	b := d.Clean(fortiConfig("102", "06xBBBB", "MIIkeyB1\nMIIkeyB2\nMIIkeyB3\n"))
	if a != b {
		t.Fatalf("volatile fields should not produce a diff:\n--- a ---\n%s--- b ---\n%s", a, b)
	}
	want := "#config-version=FG120G-7.4.12-FW-build2902-260505:opmode=0:vdom=1:user=netops\n" +
		"#buildno=2902\n" +
		"config system ha\n" +
		"    set group-name \"HACORE\"\n" +
		"    set password ENC <ENCRYPTED>\n" +
		"end\n" +
		"config vpn certificate local\n" +
		"    edit \"fw.lab.invalid\"\n" +
		"        set private-key \"-----BEGIN ENCRYPTED PRIVATE KEY-----\n" +
		"<ENCRYPTED>\n" +
		"-----END ENCRYPTED PRIVATE KEY-----\"\n" +
		"        set certificate \"-----BEGIN CERTIFICATE-----\n" +
		"MIIcertline1\nMIIcertline2\n" +
		"-----END CERTIFICATE-----\"\n" +
		"    next\n" +
		"end\n"
	if a != want {
		t.Fatalf("cleaned output:\n%s\nwant:\n%s", a, want)
	}
	c := d.Clean(strings.Replace(fortiConfig("102", "06xBBBB", "MIIkeyB1\n"), "HACORE", "HAEDGE", 1))
	if c == b {
		t.Fatal("real changes must still be detected")
	}

	raw := d.CleanRaw(fortiConfig("101", "06xAAAA", "MIIkeyA1\n"))
	for _, s := range []string{"#conf_file_ver=101", "ENC 06xAAAA", "MIIkeyA1"} {
		if !strings.Contains(raw, s) {
			t.Errorf("CleanRaw dropped %q", s)
		}
	}
}
