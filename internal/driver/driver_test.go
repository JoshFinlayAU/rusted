package driver

import "testing"

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
