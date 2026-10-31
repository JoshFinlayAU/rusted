package store

import (
	"errors"
	"testing"
	"time"
)

func openTest(t *testing.T) (*Store, int64) {
	t.Helper()
	st, err := Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cid, err := st.CreateCredential(&Credential{Name: "c", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	return st, cid
}

func TestValidateDeviceName(t *testing.T) {
	for _, ok := range []string{"core1", "core-1.example.com", "rtr_2", "r[1]", "edge rtr", ".hidden"} {
		if err := ValidateDeviceName(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "site/core1", `site\core1`, "..", ".", "../x", "-rf", "a\nb", "a\x00b", "a\x7fb", "a\u0085b"} {
		if err := ValidateDeviceName(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted (err %v)", bad, err)
		}
	}
}

func TestValidateGroup(t *testing.T) {
	for _, ok := range []string{"", "dc1", "site/core", "a b/c-d", ".x"} {
		if err := ValidateGroup(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"/abs", "a//b", "a/", "../x", "a/../b", "a/./b", ".", `a\b`, "-x", "a/-x", ".git", "a/.GIT", "a\tb"} {
		if err := ValidateGroup(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted (err %v)", bad, err)
		}
	}
}

func TestCreateDeviceValidates(t *testing.T) {
	st, cid := openTest(t)
	for _, d := range []*Device{
		{Name: "site/core1", Host: "h"},
		{Name: "r1", Host: "h", Group: "../x"},
		{Name: "r1", Host: "h", Port: 70000},
		{Name: "r1", Host: ""},
	} {
		d.CredentialID = cid
		if _, err := st.CreateDevice(d); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted (err %v)", d, err)
		}
	}
	if devs, _ := st.ListDevices(); len(devs) != 0 {
		t.Fatalf("invalid devices were stored: %v", devs)
	}
}

func TestListDevicesWithStatus(t *testing.T) {
	st, cid := openTest(t)
	id, err := st.CreateDevice(&Device{Name: "a", Host: "a", CredentialID: cid, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDevice(&Device{Name: "b", Host: "b", CredentialID: cid}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	t1 := t0.Add(30 * time.Minute)
	for _, r := range []*BackupRun{
		{DeviceID: id, StartedAt: t1, FinishedAt: t1, Status: "failed", Message: "connect: refused"},
		{DeviceID: id, StartedAt: t0, FinishedAt: t0, Status: "success", Message: "configuration updated"},
	} {
		if err := st.RecordRun(r); err != nil {
			t.Fatal(err)
		}
	}
	devs, err := st.ListDevicesWithStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("got %d devices", len(devs))
	}
	a, b := devs[0], devs[1]
	if a.LastBackup == nil || !a.LastBackup.Equal(t1) || a.LastStatus != "failed" || a.LastMessage != "connect: refused" {
		t.Fatalf("latest run not reported: %v %q %q", a.LastBackup, a.LastStatus, a.LastMessage)
	}
	if a.CredentialName != "c" {
		t.Fatalf("CredentialName = %q", a.CredentialName)
	}
	if b.LastBackup != nil || b.LastStatus != "" {
		t.Fatalf("device without runs has status %v %q", b.LastBackup, b.LastStatus)
	}
}

func TestUpdateDevice(t *testing.T) {
	st, cid := openTest(t)
	if _, err := st.CreateDevice(&Device{Name: "r1", Host: "10.0.0.1", Group: "dc", CredentialID: cid, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	d, _ := st.GetDevice("r1")
	d.Group, d.Transport, d.Port = "", "telnet", 2222
	if err := st.UpdateDevice(d, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetDevice("r1")
	if got.Group != "" || got.Transport != "telnet" || got.Port != 2222 || got.Host != "10.0.0.1" {
		t.Fatalf("update not applied: %+v", got)
	}

	// A failing hook rolls the row back.
	d.Group = "moved"
	if err := st.UpdateDevice(d, func() (func() error, error) { return nil, errors.New("git says no") }); err == nil {
		t.Fatal("hook error was swallowed")
	}
	if got, _ := st.GetDevice("r1"); got.Group != "" {
		t.Fatalf("group changed despite hook failure: %q", got.Group)
	}

	d.Group = "a//b"
	if err := st.UpdateDevice(d, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid group accepted: %v", err)
	}
	d.Group, d.Port = "", 0
	if err := st.UpdateDevice(d, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("port 0 accepted: %v", err)
	}
}

func TestRenameDevice(t *testing.T) {
	st, cid := openTest(t)
	id, err := st.CreateDevice(&Device{Name: "sw1", Host: "sw1", CredentialID: cid, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDevice(&Device{Name: "sw9", Host: "sw9", CredentialID: cid}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := st.RecordRun(&BackupRun{DeviceID: id, StartedAt: now, FinishedAt: now, Status: "success", Commit: "abc123"}); err != nil {
		t.Fatal(err)
	}

	if err := st.RenameDevice("sw1", "sw2", nil); err != nil {
		t.Fatal(err)
	}
	d, err := st.GetDevice("sw2")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != id || d.Host != "sw1" {
		t.Fatalf("rename must keep the row and leave host alone: %+v", d)
	}
	if hist, _ := st.History("sw2", 0); len(hist) != 1 || hist[0].Commit != "abc123" {
		t.Fatalf("run history lost on rename: %+v", hist)
	}
	if _, err := st.GetDevice("sw1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old name still resolves: %v", err)
	}

	if err := st.RenameDevice("nope", "x", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown device = %v, want ErrNotFound", err)
	}
	if err := st.RenameDevice("sw2", "sw9", nil); err == nil {
		t.Fatal("renaming onto an existing device must fail")
	}
	if err := st.RenameDevice("sw2", "a/b", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid new name accepted: %v", err)
	}

	// The DB rename is rolled back when the repository move fails.
	if err := st.RenameDevice("sw2", "sw3", func() (func() error, error) { return nil, errors.New("git mv failed") }); err == nil {
		t.Fatal("hook error was swallowed")
	}
	if _, err := st.GetDevice("sw2"); err != nil {
		t.Fatalf("device lost after failed rename: %v", err)
	}
}

func TestDeleteDeviceTx(t *testing.T) {
	st, cid := openTest(t)
	if _, err := st.CreateDevice(&Device{Name: "r1", Host: "h", CredentialID: cid}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteDeviceTx("r1", func() (func() error, error) { return nil, errors.New("git rm failed") }); err == nil {
		t.Fatal("hook error was swallowed")
	}
	if _, err := st.GetDevice("r1"); err != nil {
		t.Fatalf("device deleted despite hook failure: %v", err)
	}
	ran := false
	if err := st.DeleteDeviceTx("r1", func() (func() error, error) { ran = true; return nil, nil }); err != nil || !ran {
		t.Fatalf("delete = %v, hook ran %t", err, ran)
	}
	if err := st.DeleteDevice("r1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}
