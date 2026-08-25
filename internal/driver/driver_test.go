package driver

import (
	"strings"
	"testing"
)

// Two captures of an IOS config that differ only in the volatile header and an
// inline timestamp must Clean() to the same bytes, so no spurious commit
// occurs.
func TestCleanStableAcrossBackups(t *testing.T) {
	d, _ := Get("cisco_ios")
	a := "Building configuration...\n" +
		"Current configuration : 1520 bytes\n" +
		"! Last configuration change at 10:02:11 UTC Tue Jun 16 2026 by admin\n" +
		"hostname core-sw1\n!\nntp clock-period 17179862\n"
	b := "Building configuration...\n" +
		"Current configuration : 1640 bytes\n" +
		"! Last configuration change at 23:11:54 UTC Wed Jun 17 2026 by admin\n" +
		"hostname core-sw1\n!\nntp clock-period 17179999\n"
	if got, want := d.Clean(a), d.Clean(b); got != want {
		t.Fatalf("expected identical cleaned config:\n--- a ---\n%s\n--- b ---\n%s", got, want)
	}
}

// A genuine config change must produce a different cleaned result.
func TestCleanDetectsRealChange(t *testing.T) {
	d, _ := Get("cisco_ios")
	a := "hostname core-sw1\ninterface Gi0/1\n ip address 10.0.0.1 255.255.255.0\n"
	b := "hostname core-sw1\ninterface Gi0/1\n ip address 10.0.0.2 255.255.255.0\n"
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
	a := "#config-version=FG120G-7.4.12-FW-build2902-260505:opmode=0:vdom=1:user=ssisnet\n" +
		"#conf_file_ver=11718951413801515\n" +
		"#buildno=2902\n" +
		"#global_vdom=1\n" +
		"config system ha\n" +
		"    set group-name \"Cluster-CARISP\"\n" +
		"    set password ENC 06xLtcgR6zBhOxpoerYsnx7OqJbHBIcDctklV/GlJXqrPH\n" +
		"end\n"
	b := "#config-version=FG120G-7.4.12-FW-build2902-260505:opmode=0:vdom=1:user=ssisnet\n" +
		"#conf_file_ver=11720360163074603\n" +
		"#buildno=2902\n" +
		"#global_vdom=1\n" +
		"config system ha\n" +
		"    set group-name \"Cluster-CARISP\"\n" +
		"    set password ENC kNImfeew7AEHFOBEVyUH71sFvuDctZeh6i+BOmdigmnQ\n" +
		"end\n"
	if got, want := d.Clean(a), d.Clean(b); got != want {
		t.Fatalf("volatile fields should not produce a diff:\n--- a ---\n%s\n--- b ---\n%s", got, want)
	}
	c := strings.Replace(b, "Cluster-CARISP", "Cluster-NEW", 1)
	if d.Clean(b) == d.Clean(c) {
		t.Fatal("real changes must still be detected")
	}
}

// --raw (CleanRaw) must keep every line, ignored fields included.
func TestCleanRawKeepsIgnoredFields(t *testing.T) {
	d, _ := Get("fortinet")
	raw := "#conf_file_ver=11718951413801515\n" +
		"config system interface\n" +
		"    set password ENC IFaYiusT463nTnX8Ii0zjX0FOu+84pDX\n" +
		"end\n"
	out := d.CleanRaw(raw)
	for _, want := range []string{"#conf_file_ver=11718951413801515", "set password ENC IFaYiusT463nTnX8Ii0zjX0FOu+84pDX"} {
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
	mk := func(salt string) string {
		return "config vpn certificate local\n" +
			"    edit \"*.carisp.lan\"\n" +
			"        set comments ''\n" +
			"        set private-key \"-----BEGIN ENCRYPTED PRIVATE KEY-----\n" +
			"MIIJpDBWBgkqhkiG9w0BBQ0wSTAxBgkqhkiG9w0BBQwwJAQQ" + salt + "\n" +
			"qLoLuwICCAAwDAYIKoZIhvcNAgkFADAUBggqhkiG9w0DBwQIErGAazsrkuIEgglI\n" +
			"iFP60mMljRBaE0gXeccpk6C/pLIz/nkQAoa47/q85DsiEu9PX1YREz6RmICoo3uK\n" +
			"-----END ENCRYPTED PRIVATE KEY-----\".\n" +
			"        set certificate \"-----BEGIN CERTIFICATE-----\n" +
			"MIIDTTCCAjUCFB1xv1hd0P9E0bzOPVknOZeFYpa1MA0GCSqGSIb3DQEBCwUAMGMx\n" +
			"-----END CERTIFICATE-----\".\n" +
			"        set public-key \"-----BEGIN PUBLIC KEY-----\n" +
			"MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv1x2y3z4\n" +
			"-----END PUBLIC KEY-----\".\n" +
			"    next\n" +
			"end\n"
	}
	a := d.Clean(mk("n1zwNWASLnZZcr1b"))
	b := d.Clean(mk("4RNFuuoWjBcY+i10"))
	want := "config vpn certificate local\n" +
		"    edit \"*.carisp.lan\"\n" +
		"        set comments ''\n" +
		"        set certificate \"-----BEGIN CERTIFICATE-----\n" +
		"MIIDTTCCAjUCFB1xv1hd0P9E0bzOPVknOZeFYpa1MA0GCSqGSIb3DQEBCwUAMGMx\n" +
		"-----END CERTIFICATE-----\".\n" +
		"        set public-key \"-----BEGIN PUBLIC KEY-----\n" +
		"MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAv1x2y3z4\n" +
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
	raw := d.CleanRaw(mk("n1zwNWASLnZZcr1b"))
	for _, part := range []string{"ENCRYPTED PRIVATE KEY", "BEGIN CERTIFICATE", "BEGIN PUBLIC KEY"} {
		if !strings.Contains(raw, part) {
			t.Fatalf("CleanRaw dropped %q:\n%s", part, raw)
		}
	}
}
