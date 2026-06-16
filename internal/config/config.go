// Package config loads rusted's settings from a config file, with environment
// variables and command-line flags layered on top.
//
// Precedence (lowest to highest):
//
//	built-in defaults  <  config file  <  environment variables  <  CLI flags
//
// The config file is a small TOML-style "key = value" format (a real TOML
// parser is overkill for a handful of flat keys, so we parse the subset we
// need with no external dependency). Example:
//
//	# /etc/rusted/config.toml
//	db        = "/var/lib/rusted/rusted.db"
//	backups   = "/var/lib/rusted/backups"
//	api_addr  = ":8080"
//	api_token = "…"
//	secret    = "…"
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all resolved settings.
type Config struct {
	DB       string
	Backups  string
	APIAddr  string
	APIToken string
	Secret   string

	// Path is the config file that was loaded, or "" if none was found.
	Path string
}

// Defaults returns the built-in defaults.
func Defaults() Config {
	return Config{DB: "rusted.db", Backups: "backups", APIAddr: ":8080"}
}

// UserConfigPath is the per-user config location (~/.config/rusted/config.toml,
// honouring XDG_CONFIG_HOME).
func UserConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "rusted", "config.toml")
}

// GlobalConfigPath is the system-wide config location.
const GlobalConfigPath = "/etc/rusted/config.toml"

// Candidates lists the config files to try, in order, when no explicit path is
// given.
func Candidates() []string {
	var c []string
	if p := os.Getenv("RUSTED_CONFIG"); p != "" {
		c = append(c, p)
	}
	if p := UserConfigPath(); p != "" {
		c = append(c, p)
	}
	c = append(c, GlobalConfigPath)
	return c
}

// Load resolves configuration. If explicit is non-empty it must exist and is
// used; otherwise the first existing candidate (if any) is loaded. Environment
// variables are then layered on top.
func Load(explicit string) (Config, error) {
	cfg := Defaults()

	path := explicit
	if path == "" {
		for _, cand := range Candidates() {
			if fileExists(cand) {
				path = cand
				break
			}
		}
	} else if !fileExists(path) {
		return cfg, fmt.Errorf("config file %q not found", path)
	}

	if path != "" {
		if err := parseFile(path, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %q: %w", path, err)
		}
		cfg.Path = path
	}

	applyEnv(&cfg)
	return cfg, nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("RUSTED_DB"); v != "" {
		c.DB = v
	}
	if v := os.Getenv("RUSTED_BACKUPS"); v != "" {
		c.Backups = v
	}
	if v := os.Getenv("RUSTED_API_ADDR"); v != "" {
		c.APIAddr = v
	}
	if v := os.Getenv("RUSTED_API_TOKEN"); v != "" {
		c.APIToken = v
	}
	if v := os.Getenv("RUSTED_SECRET"); v != "" {
		c.Secret = v
	}
}

func parseFile(path string, c *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.Trim(strings.TrimSpace(v), `"'`)
		switch key {
		case "db":
			c.DB = val
		case "backups":
			c.Backups = val
		case "api_addr":
			c.APIAddr = val
		case "api_token":
			c.APIToken = val
		case "secret":
			c.Secret = val
		}
	}
	return sc.Err()
}

// Render produces the file contents for a config.
func (c Config) Render() string {
	var b strings.Builder
	b.WriteString("# rusted configuration\n")
	b.WriteString("# Keep this file private: it contains the API token and encryption secret.\n\n")
	fmt.Fprintf(&b, "db        = %q\n", c.DB)
	fmt.Fprintf(&b, "backups   = %q\n", c.Backups)
	fmt.Fprintf(&b, "api_addr  = %q\n", c.APIAddr)
	fmt.Fprintf(&b, "api_token = %q\n", c.APIToken)
	fmt.Fprintf(&b, "secret    = %q\n", c.Secret)
	return b.String()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
