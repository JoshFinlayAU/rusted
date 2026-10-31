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
	// Point candidates away from any real config (incl. a global one on the build host).
	t.Setenv("RUSTED_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	orig := GlobalConfigPath
	GlobalConfigPath = filepath.Join(t.TempDir(), "none.toml")
	t.Cleanup(func() { GlobalConfigPath = orig })
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

func TestAPIExecFromFileAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("api_exec = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || !cfg.APIExec {
		t.Fatalf("api_exec = true not loaded: %+v, %v", cfg, err)
	}
	t.Setenv("RUSTED_API_EXEC", "false")
	if cfg, _ = Load(path); cfg.APIExec {
		t.Fatal("RUSTED_API_EXEC=false should override the file")
	}

	t.Setenv("RUSTED_API_EXEC", "")
	if round, _ := Load(writeConfig(t, Config{APIExec: true}.Render())); !round.APIExec {
		t.Fatal("Render dropped api_exec")
	}
}

func writeConfig(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
