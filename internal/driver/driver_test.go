package driver

import (
	"encoding/base64"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// The fixtures below must never embed real-world material. Credentials, PEM
// payloads and device/object names are generated fresh on every test run; the
// only thing that matters for these tests is that the shapes match what the
// drivers' regexes expect, so names are picked from small synthetic pools.
var (
	poolHostnames = []string{"core-sw1", "edge-rt2", "campus-sw3", "lab-rtr4"}
	poolClusters  = []string{"HACORE", "HAEDGE", "HARC"}
	poolCerts     = []string{"*.lab.invalid", "fw.lab.invalid", "*.test.invalid"}
	poolUsers     = []string{"netops", "svcbackup", "admin"}
)

func pick(pool []string) string { return pool[rand.IntN(len(pool))] }

func fakeBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rand.Uint32())
	}
	return b
}

// fakeSecret mimics a FortiOS "ENC ..." ciphertext: re-encrypted with a fresh
// IV on every save, so any run must produce a different token.
func fakeSecret() string {
	return "06x" + base64.StdEncoding.EncodeToString(fakeBytes(27))
}

// fakePEMBody returns count newline-terminated base64 lines (76 columns),
// standing in for PEM payload bodies.
func fakePEMBody(count int) string {
	var b strings.Builder
	for range count {
		b.WriteString(base64.StdEncoding.EncodeToString(fakeBytes(57)))
		b.WriteString("\n")
	}
	return b.String()
}

// Two captures of an IOS config that differ only in the volatile header and an
// inline timestamp must Clean() to the same bytes, so no spurious commit
// occurs.
func TestCleanStableAcrossBackups(t *testing.T) {
	d, _ := Get("cisco_ios")
	host := pick(poolHostnames)
	user := pick(poolUsers)
	a := "Building configuration...\n" +
		"Current configuration : 1520 bytes\n" +
		"! Last configuration change at 10:02:11 UTC Tue Jun 16 2026 by " + user + "\n" +
		"hostname " + host + "\n!\nntp clock-period 17179862\n"
	b := "Building configuration...\n" +
		"Current configuration : 1640 bytes\n" +
		"! Last configuration change at 23:11:54 UTC Wed Jun 17 2026 by " + user + "\n" +
		"hostname " + host + "\n!\nntp clock-period 17179999\n"
	if got, want := d.Clean(a), d.Clean(b); got != want {
		t.Fatalf("expected identical cleaned config:\n--- a ---\n%s\n--- b ---\n%s", got, want)
	}
}

// A genuine config change must produce a different cleaned result.
func TestCleanDetectsRealChange(t *testing.T) {
	d, _ := Get("cisco_ios")
	host := pick(poolHostnames)
	a := "hostname " + host + "\ninterface Gi0/1\n ip address 10.0.0.1 255.255.255.0\n"
	b := "hostname " + host + "\ninterface Gi0/1\n ip address 10.0.0.2 255.255.255.0\n"
	if d.Clean(a) == d.Clean(b) {
		t.Fatal("real change should not normalise away")
	}
}

func TestGetFallsBackToGeneric(t *testing.T) {
	d, ok := Get("does-not-exist")
	if ok {
		t.Fatal("expected ok=false for unknown driver")
	}
	if d.Name != "generic" {
		t.Fatalf("expected generic fallback, got %q", d.Name)
	}
}

// FortiGate rewrites #conf_file_ver and re-encrypts every stored secret on
// each save; an unchanged FortiGate must still Clean() to identical bytes.
func TestFortinetCleanIgnoresVolatileFields(t *testing.T) {
	d, ok := Get("fortinet")
	if !ok {
		t.Fatal("expected the fortinet driver to exist")
	}
	user := pick(poolUsers)
	cluster := pick(poolClusters)
	hdr := "#config-version=FG120G-7.4.12-FW-build2902-260505:opmode=0:vdom=1:user=" + user + "\n"
	mk := func(ver, secret string) string {
		return hdr +
			"#conf_file_ver=" + ver + "\n" +
			"#buildno=2902\n" +
			"#global_vdom=1\n" +
			"config system ha\n" +
			"    set group-name \"" + cluster + "\"\n" +
			"    set password ENC " + secret + "\n" +
			"end\n"
	}
	a := mk(strconv.FormatUint(rand.Uint64(), 10), fakeSecret())
	b := mk(strconv.FormatUint(rand.Uint64(), 10), fakeSecret())
	if got, want := d.Clean(a), d.Clean(b); got != want {
		t.Fatalf("volatile fields should not produce a diff:\n--- a ---\n%s\n--- b ---\n%s", got, want)
	}
	c := strings.Replace(b, cluster, cluster+"-NEW", 1)
	if d.Clean(b) == d.Clean(c) {
		t.Fatal("real changes must still be detected")
	}
}

// --raw (CleanRaw) must keep every line, ignored fields included.
func TestCleanRawKeepsIgnoredFields(t *testing.T) {
	d, _ := Get("fortinet")
	ver := strconv.FormatUint(rand.Uint64(), 10)
	secret := fakeSecret()
	raw := "#conf_file_ver=" + ver + "\n" +
		"config system interface\n" +
		"    set password ENC " + secret + "\n" +
		"end\n"
	out := d.CleanRaw(raw)
	for _, want := range []string{"#conf_file_ver=" + ver, "set password ENC " + secret} {
		if !strings.Contains(out, want) {
			t.Fatalf("CleanRaw dropped %q:\n%s", want, out)
		}
	}
}

// FortiGate re-encrypts stored private keys (PKCS#8) with a fresh salt/IV on
// every save, so ENCRYPTED blocks must be skipped; plain certificates and
// public keys are deterministic and must be kept. --raw keeps everything.
func TestFortinetCleanIgnoresPEMBlocks(t *testing.T) {
	d, ok := Get("fortinet")
	if !ok {
		t.Fatal("expected the fortinet driver to exist")
	}
	cert := pick(poolCerts)
	certBody := fakePEMBody(2)
	pubKeyBody := fakePEMBody(1)
	mk := func(keyBody string) string {
		return "config vpn certificate local\n" +
			"    edit \"" + cert + "\"\n" +
			"        set comments ''\n" +
			"        set private-key \"-----BEGIN ENCRYPTED PRIVATE KEY-----\n" +
			keyBody +
			"-----END ENCRYPTED PRIVATE KEY-----\".\n" +
			"        set certificate \"-----BEGIN CERTIFICATE-----\n" +
			certBody +
			"-----END CERTIFICATE-----\".\n" +
			"        set public-key \"-----BEGIN PUBLIC KEY-----\n" +
			pubKeyBody +
			"-----END PUBLIC KEY-----\".\n" +
			"    next\n" +
			"end\n"
	}
	a := d.Clean(mk(fakePEMBody(4)))
	b := d.Clean(mk(fakePEMBody(4)))
	want := "config vpn certificate local\n" +
		"    edit \"" + cert + "\"\n" +
		"        set comments ''\n" +
		"        set certificate \"-----BEGIN CERTIFICATE-----\n" +
		certBody +
		"-----END CERTIFICATE-----\".\n" +
		"        set public-key \"-----BEGIN PUBLIC KEY-----\n" +
		pubKeyBody +
		"-----END PUBLIC KEY-----\".\n" +
		"    next\n" +
		"end\n"
	if a != want {
		t.Fatalf("cleaned output mismatch:\n--- got ---\n%s\n--- want ---\n%s", a, want)
	}
	if strings.Contains(a, "ENCRYPTED PRIVATE KEY") {
		t.Fatalf("encrypted private key block not removed:\n%s", a)
	}
	if a != b {
		t.Fatalf("volatile PEM re-encryption should not produce a diff:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	raw := d.CleanRaw(mk(fakePEMBody(4)))
	for _, part := range []string{"ENCRYPTED PRIVATE KEY", "BEGIN CERTIFICATE", "BEGIN PUBLIC KEY"} {
		if !strings.Contains(raw, part) {
			t.Fatalf("CleanRaw dropped %q:\n%s", part, raw)
		}
	}
}

// The openwrt driver must exist with intentionally empty ignore lists: nothing
// on a stock OpenWrt is volatile enough to strip today. It captures over
// ssh-exec byte-exactly and formats driver-side.
func TestOpenWRTDriverContract(t *testing.T) {
	d, ok := Get("openwrt")
	if !ok {
		t.Fatal("expected the openwrt driver to exist")
	}
	if len(d.Strip) != 0 || len(d.StripBlocks) != 0 {
		t.Fatalf("openwrt ignore lists must be empty for now (Strip=%d, StripBlocks=%d)",
			len(d.Strip), len(d.StripBlocks))
	}
	if d.Transport != "ssh-exec" {
		t.Fatalf("Transport = %q, want ssh-exec (byte-exact capture)", d.Transport)
	}
	if !d.BinarySafe {
		t.Fatal("BinarySafe must be set so transport normalisation is skipped")
	}
	if d.PostProcess == nil {
		t.Fatal("PostProcess must be set (driver-side formatting)")
	}
}

// Clean must be a stable pass-through for openwrt output: identical dumps
// clean identically, real changes are detected, and base64 blobs survive
// the generic normaliser untouched.
func TestOpenWRTCleanStable(t *testing.T) {
	d, _ := Get("openwrt")
	host := pick(poolHostnames)
	a := "## /etc/config/system\n" +
		"config system\n" +
		"\toption hostname '" + host + "'\n" +
		"## /etc/config/network\n" +
		"config interface 'lan'\n" +
		"\toption ipaddr '192.168.1.1'\n"
	b := strings.Replace(a, "192.168.1.1", "10.0.0.9", 1)
	if d.Clean(a) != d.Clean(a) { // sanity; guards against future statefulness
		t.Fatal("Clean is not deterministic")
	}
	if d.Clean(a) == d.Clean(b) {
		t.Fatal("real changes must still be detected")
	}
	blob := fakePEMBody(2)
	in := "## /usr/local/bin/agent\n## binary file, base64 follows\n" + blob
	out := d.Clean(in)
	for _, want := range append([]string{"## /usr/local/bin/agent", "## binary file, base64 follows"},
		strings.Split(strings.TrimSuffix(blob, "\n"), "\n")...) {
		if !strings.Contains(out, want) {
			t.Fatalf("base64 payload altered by Clean, missing %q:\n%s", want, out)
		}
	}
}

// postProcessOpenWRT must keep text sections verbatim and re-encode binary
// sections (NUL within the first 4 KiB) as base64 of the EXACT received bytes.
func TestOpenWRTPostProcess(t *testing.T) {
	host := pick(poolHostnames)
	text := "config system\n\toption hostname '" + host + "'\n"
	bin := string([]byte{0x00, 0x01, 0x02, 0xFF}) + strings.Repeat("AB\x00CD\r\n", 200)

	// NOTE: no trailing newline after bin — mirroring the engine, which only
	// appends '\n' when the capture lacks one; post-processing must strip that
	// framing byte without touching file content.
	in := "\n## /etc/config/system\n" + text + "\n## /root/scripts/tool\n" + bin
	out := postProcessOpenWRT(in)

	if !strings.Contains(out, "## /etc/config/system\n"+text) {
		t.Fatalf("text section not preserved verbatim:\n%s", out)
	}
	if !strings.Contains(out, "## binary file, base64 follows") {
		t.Fatalf("binary section not encoded:\n%s", out)
	}

	// Decode the base64 payload back and require byte-exact fidelity,
	// including NUL and CRLF bytes inside the binary content.
	start := strings.Index(out, "base64 follows\n")
	if start < 0 {
		t.Fatal("missing base64 marker")
	}
	payload := out[start+len("base64 follows\n"):]
	if i := strings.Index(payload, "\n\n## "); i >= 0 {
		payload = payload[:i]
	} else if strings.HasSuffix(payload, "\n") {
		payload = strings.TrimSuffix(payload, "\n")
	}
	dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload), ""))
	if err != nil {
		t.Fatalf("payload is not valid base64: %v\n%s", err, payload)
	}
	if string(dec) != bin {
		t.Fatalf("roundtrip mismatch: got %d bytes, want %d bytes", len(dec), len(bin))
	}

	// A text-only dump must come through unchanged (modulo section framing).
	textOnly := "\n## /etc/config/network\nconfig interface 'lan'\n\toption ipaddr '10.0.0.1'\n"
	if postProcessOpenWRT(textOnly) != textOnly {
		t.Fatalf("text-only dump was modified:\n%s", postProcessOpenWRT(textOnly))
	}

	// A binary file WITHOUT a final newline, followed by another section:
	// both must round-trip exactly.
	mixed := "\n## /opt/nolf\n\x00Xtail" + "\n## /etc/config/system\nhostname ok\n"
	out2 := postProcessOpenWRT(mixed)
	p2 := out2[strings.Index(out2, "base64 follows\n")+len("base64 follows\n"):]
	if i := strings.Index(p2, "\n\n## "); i >= 0 {
		p2 = p2[:i]
	}
	dec2, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(strings.TrimSuffix(p2, "\n")), ""))
	if err != nil {
		t.Fatalf("no-newline payload invalid: %v", err)
	}
	if string(dec2) != "\x00Xtail" {
		t.Fatalf("no-newline roundtrip mismatch: %q", dec2)
	}
	if !strings.Contains(out2, "## /etc/config/system\nhostname ok\n") {
		t.Fatalf("section after binary corrupted:\n%s", out2)
	}
}
