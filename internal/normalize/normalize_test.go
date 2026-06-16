package normalize

import "testing"

func TestApplyMasksDynamicStrings(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"cisco last change", "! Last configuration change at 10:02:11 UTC Tue Jun 16 2026 by admin",
			"! Last configuration change at <TIMESTAMP> by admin"},
		{"iso datetime", "# 2026-06-16 10:02:11 by RouterOS", "# <TIMESTAMP> by RouterOS"},
		{"iso date only", "set date 2026-06-16", "set date <DATE>"},
		{"verbose date", "created Jun 16, 2026", "created <DATE>"},
		{"uptime", "System uptime is 4 days, 2 hours, 11 minutes", "System <UPTIME>"},
		{"tz time", "clock set 23:59:59 GMT", "clock set <TIME>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Apply(c.in); got != c.want {
				t.Errorf("Apply(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

// A config that differs only in volatile content must normalise to an
// identical string, so change detection treats it as unchanged.
func TestApplyStableAcrossTimestamps(t *testing.T) {
	a := "hostname r1\n! Last configuration change at 10:02:11 UTC Tue Jun 16 2026\ninterface eth0\n"
	b := "hostname r1\n! Last configuration change at 23:59:00 UTC Wed Jun 17 2026\ninterface eth0\n"
	if Apply(a) != Apply(b) {
		t.Fatalf("timestamp-only difference should normalise equal:\n%q\n%q", Apply(a), Apply(b))
	}
}

// Real configuration values must survive normalisation untouched.
func TestApplyPreservesConfig(t *testing.T) {
	cfg := "interface GigabitEthernet0/1\n ip address 192.168.1.1 255.255.255.0\n description uplink-to-core\n"
	if got := Apply(cfg); got != cfg {
		t.Errorf("normalisation altered real config:\n%q", got)
	}
}
