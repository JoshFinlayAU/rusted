package driver

import "testing"

func TestCambiumDraftDrivers(t *testing.T) {
	for _, name := range []string{"cambium_epmp", "cambium_cnmatrix"} {
		d, ok := Get(name)
		if !ok {
			t.Errorf("%s not registered", name)
		}
		if len(d.Config) == 0 {
			t.Errorf("%s has no config command", name)
		}
	}
	if d, _ := Get("cambium_epmp"); d.Config[0] != "config show json" {
		t.Errorf("cambium_epmp config = %q", d.Config[0])
	}
	// ePMP JSON export must set RawNormalize (skip normalisation, don't mask JSON values).
	if d, _ := Get("cambium_epmp"); !d.RawNormalize {
		t.Error("cambium_epmp should set RawNormalize")
	}
}
