package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
# comment
db        = "/data/file.db"
backups   = "/data/backups"
api_addr  = ":9090"
api_token = "filetoken"
secret    = "filesecret"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	// File only.
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB != "/data/file.db" || cfg.APIAddr != ":9090" || cfg.APIToken != "filetoken" || cfg.Secret != "filesecret" {
		t.Fatalf("file values not loaded: %+v", cfg)
	}
	if cfg.Path != path {
		t.Fatalf("Path = %q, want %q", cfg.Path, path)
	}

	// Env overrides file.
	t.Setenv("RUSTED_API_TOKEN", "envtoken")
	t.Setenv("RUSTED_DB", "/env/file.db")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIToken != "envtoken" {
		t.Fatalf("env should override file token, got %q", cfg.APIToken)
	}
	if cfg.DB != "/env/file.db" {
		t.Fatalf("env should override file db, got %q", cfg.DB)
	}
	// File value with no env override survives.
	if cfg.Secret != "filesecret" {
		t.Fatalf("secret should remain from file, got %q", cfg.Secret)
	}
}

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	// Point candidates away from any real config.
	t.Setenv("RUSTED_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	d := Defaults()
	if cfg.DB != d.DB || cfg.Backups != d.Backups || cfg.APIAddr != d.APIAddr {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
	if cfg.Path != "" {
		t.Fatalf("expected no config path, got %q", cfg.Path)
	}
}

func TestLoadMissingExplicitFails(t *testing.T) {
	if _, err := Load("/no/such/config.toml"); err == nil {
		t.Fatal("expected error for missing explicit config")
	}
}

func TestRenderRoundTrip(t *testing.T) {
	in := Config{DB: "a.db", Backups: "b", APIAddr: ":1", APIToken: "tok", Secret: "sec"}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	if err := os.WriteFile(path, []byte(in.Render()), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.DB != in.DB || out.Backups != in.Backups || out.APIAddr != in.APIAddr || out.APIToken != in.APIToken || out.Secret != in.Secret {
		t.Fatalf("round trip mismatch:\n in: %+v\nout: %+v", in, out)
	}
}
