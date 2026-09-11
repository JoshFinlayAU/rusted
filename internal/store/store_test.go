package store

import (
	"errors"
	"testing"
	"time"
)

func TestRenameDevicePreservesHistory(t *testing.T) {
	st, err := Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cid, err := st.CreateCredential(&Credential{Name: "c", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateDevice(&Device{Name: "sw1", Host: "sw1", Driver: "generic", CredentialID: cid, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := st.RecordRun(&BackupRun{DeviceID: id, StartedAt: now, FinishedAt: now, Status: "success", Commit: "abc123"}); err != nil {
		t.Fatal(err)
	}

	if err := st.RenameDevice("sw1", "sw2"); err != nil {
		t.Fatalf("RenameDevice: %v", err)
	}
	dev, err := st.GetDevice("sw2")
	if err != nil {
		t.Fatalf("device not reachable under new name: %v", err)
	}
	if dev.ID != id {
		t.Fatalf("rename must keep the row id: got %d, want %d", dev.ID, id)
	}
	if dev.Host != "sw2" {
		t.Fatalf("default host must follow the new name, got %q", dev.Host)
	}

	// Run history stays attached to the renamed device.
	hist, err := st.History("sw2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Status != "success" || hist[0].Commit != "abc123" {
		t.Fatalf("history lost on rename: %+v", hist)
	}
	if _, err := st.GetDevice("sw1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old name must be gone, got %v", err)
	}
}

func TestRenameDeviceKeepsCustomHost(t *testing.T) {
	st, err := Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cid, _ := st.CreateCredential(&Credential{Name: "c", Username: "u", Password: "p"})
	if _, err := st.CreateDevice(&Device{Name: "rtr", Host: "10.0.0.1", Driver: "generic", CredentialID: cid, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.RenameDevice("rtr", "rtr2"); err != nil {
		t.Fatal(err)
	}
	dev, err := st.GetDevice("rtr2")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Host != "10.0.0.1" {
		t.Fatalf("explicit host must survive rename, got %q", dev.Host)
	}
}

func TestRenameDeviceErrors(t *testing.T) {
	st, err := Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cid, _ := st.CreateCredential(&Credential{Name: "c", Username: "u", Password: "p"})
	if _, err := st.CreateDevice(&Device{Name: "a", Host: "a", Driver: "generic", CredentialID: cid, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDevice(&Device{Name: "b", Host: "b", Driver: "generic", CredentialID: cid, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.RenameDevice("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown old name = %v, want ErrNotFound", err)
	}
	if err := st.RenameDevice("a", "b"); err == nil {
		t.Fatal("renaming onto an existing name must fail")
	}
	if _, err := st.GetDevice("a"); err != nil {
		t.Fatalf("failed rename must leave the device intact: %v", err)
	}
}
