package backup

import (
	"context"
	"strings"
	"testing"

	"github.com/athenanetworks/rusted/internal/driver"
	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/store"
	"github.com/athenanetworks/rusted/internal/transport"
)

// fakeTransport returns canned command output so the engine can be exercised
// without a real device. The configuration it returns changes only in a
// volatile timestamp line between calls, which must NOT produce a second commit.
type fakeTransport struct{ calls *int }

func (fakeTransport) Name() string { return "fake" }

func (f fakeTransport) Dial(_ context.Context, _ transport.Target) (transport.Session, error) {
	*f.calls++
	return &fakeSession{call: *f.calls}, nil
}

type fakeSession struct{ call int }

func (s *fakeSession) SendCommand(cmd string) (string, error) {
	if cmd == "show running-config" {
		// Same config, different "last change" timestamp each call.
		ts := "10:00:00 UTC Mon Jun 16 2026"
		if s.call > 1 {
			ts = "23:45:01 UTC Tue Jun 17 2026"
		}
		return "! Last configuration change at " + ts + "\nhostname r1\ninterface Gi0/1\n ip address 10.0.0.1 255.255.255.0\n", nil
	}
	return "", nil
}

func (s *fakeSession) Close() error { return nil }

func TestEngineEndToEnd(t *testing.T) {
	calls := 0
	transport.Register(fakeTransport{calls: &calls})

	st, err := store.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	gs, err := gitstore.Open(t.TempDir() + "/backups")
	if err != nil {
		t.Fatal(err)
	}

	cid, err := st.CreateCredential(&store.Credential{Name: "c", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDevice(&store.Device{Name: "r1", Host: "127.0.0.1", Driver: "cisco_ios", CredentialID: cid, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	eng := New(st, gs)
	eng.Transport = "fake"

	// First backup: should commit.
	r1, err := eng.BackupDevice(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != "success" {
		t.Fatalf("first backup status = %q (%s)", r1.Status, r1.Message)
	}
	if r1.Commit == "" {
		t.Fatal("first backup should have a commit hash")
	}

	// Second backup: only the timestamp changed, so it must be unchanged.
	r2, err := eng.BackupDevice(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != "unchanged" {
		t.Fatalf("second backup status = %q (%s); timestamp-only change should be unchanged", r2.Status, r2.Message)
	}

	// History should record both runs.
	hist, err := st.History("r1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(hist))
	}

	// Stored config must have the timestamp masked.
	cfg, err := gs.Latest("r1.cfg")
	if err != nil {
		t.Fatal(err)
	}
	// cisco_ios strips the volatile "Last configuration change" line entirely,
	// so it must be absent while the real config survives.
	if strings.Contains(cfg, "Last configuration change") {
		t.Fatalf("volatile line should have been stripped; got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "hostname r1") {
		t.Fatalf("stored config missing real content; got:\n%s", cfg)
	}
}

// scriptTransport answers each command from a map and remembers the Target it
// was dialled with.
type scriptTransport struct {
	name string
	out  map[string]string
	tgt  *transport.Target
}

func (s scriptTransport) Name() string { return s.name }

func (s scriptTransport) Dial(_ context.Context, t transport.Target) (transport.Session, error) {
	*s.tgt = t
	return scriptSession(s.out), nil
}

type scriptSession map[string]string

func (s scriptSession) SendCommand(cmd string) (string, error) { return s[cmd], nil }
func (s scriptSession) Close() error                           { return nil }

func newTestEngine(t *testing.T, drv string, tr scriptTransport) (*Engine, *store.Store, *gitstore.Store) {
	t.Helper()
	transport.Register(tr)
	st, err := store.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	gs, err := gitstore.Open(t.TempDir() + "/backups")
	if err != nil {
		t.Fatal(err)
	}
	cid, err := st.CreateCredential(&store.Credential{Name: "c", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDevice(&store.Device{Name: "r1", Host: "127.0.0.1", Driver: drv, CredentialID: cid, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	eng := New(st, gs)
	eng.Transport = tr.name
	return eng, st, gs
}

// CaptureRaw shows the unmasked capture but must never commit it or record a
// backup run: it can hold secrets the normal backup masks.
func TestCaptureRawCommitsNothing(t *testing.T) {
	var tgt transport.Target
	eng, st, gs := newTestEngine(t, "fortinet", scriptTransport{
		name: "script-raw",
		tgt:  &tgt,
		out: map[string]string{
			"show full-configuration": "#conf_file_ver=7\nconfig system ha\n    set password ENC 06xSECRET\nend\n",
		},
	})
	out, err := eng.CaptureRaw(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"#conf_file_ver=7", "ENC 06xSECRET"} {
		if !strings.Contains(out, s) {
			t.Errorf("raw capture missing %q:\n%s", s, out)
		}
	}
	if hist, _ := st.History("r1", 0); len(hist) != 0 {
		t.Errorf("CaptureRaw recorded %d run(s)", len(hist))
	}
	if cfg, err := gs.Latest("r1.cfg"); err == nil {
		t.Errorf("CaptureRaw committed a config:\n%s", cfg)
	}
}

// A BinarySafe driver gets RawOutput on its Target, no framing newline between
// commands, and its Prepare/PostProcess run before Clean.
func TestBinarySafeCapture(t *testing.T) {
	var seen string
	driver.Register(driver.Driver{
		Name:         "test_binary",
		BinarySafe:   true,
		RawNormalize: true,
		Prepare: func(d driver.Driver) driver.Driver {
			d.Config = []string{"one", "two"}
			d.PostProcess = func(raw string) (string, error) {
				seen = raw
				return "processed\n", nil
			}
			return d
		},
	})
	var tgt transport.Target
	eng, _, gs := newTestEngine(t, "test_binary", scriptTransport{
		name: "script-binary",
		tgt:  &tgt,
		out:  map[string]string{"one": "a\x00", "two": "b"},
	})
	r, err := eng.BackupDevice(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "success" {
		t.Fatalf("status = %q (%s)", r.Status, r.Message)
	}
	if !tgt.RawOutput {
		t.Error("Target.RawOutput not set for a BinarySafe driver")
	}
	if seen != "a\x00b" {
		t.Errorf("PostProcess got %q, want the bytes untouched", seen)
	}
	if cfg, _ := gs.Latest("r1.cfg"); cfg != "processed\n" {
		t.Errorf("stored %q", cfg)
	}
}
