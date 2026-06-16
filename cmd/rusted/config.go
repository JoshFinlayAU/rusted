package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/athenanetworks/rusted/internal/config"
	"github.com/spf13/cobra"
)

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Manage the rusted config file",
	}
	c.AddCommand(configInitCmd(), configShowCmd())
	return c
}

func configInitCmd() *cobra.Command {
	var global, force bool
	var dataDir string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a config file with a generated API token and encryption secret",
		Long: "Creates a config file (user-level by default, or system-wide with --global) " +
			"containing a randomly generated API token and encryption secret, plus database " +
			"and backup paths. Existing files are not overwritten unless --force is given.",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.UserConfigPath()
			if dataDir == "" {
				dataDir = defaultDataDir(global)
			}
			if global {
				path = config.GlobalConfigPath
			}
			if path == "" {
				return fmt.Errorf("could not determine config path")
			}

			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
			}

			// Preserve existing token/secret if a config is being regenerated,
			// so encrypted credentials remain readable.
			existing, _ := config.Load(path)
			token := existing.APIToken
			if token == "" {
				token = randToken(32)
			}
			sec := existing.Secret
			if sec == "" {
				sec = randToken(32)
			}

			out := config.Config{
				DB:       filepath.Join(dataDir, "rusted.db"),
				Backups:  filepath.Join(dataDir, "backups"),
				APIAddr:  ":8080",
				APIToken: token,
				Secret:   sec,
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return err
			}
			// 0600: the file holds the API token and encryption secret.
			if err := os.WriteFile(path, []byte(out.Render()), 0o600); err != nil {
				return err
			}
			fmt.Printf("wrote %s (mode 0600)\n", path)
			fmt.Printf("  db:      %s\n", out.DB)
			fmt.Printf("  backups: %s\n", out.Backups)
			fmt.Println("Keep this file private — it contains your API token and encryption secret.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "write the system-wide config (/etc/rusted/config.toml)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "directory for the database and backups")
	return cmd
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the resolved configuration (secrets masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			src := cfg.Path
			if src == "" {
				src = "(no config file; using defaults/env/flags)"
			}
			fmt.Printf("source:    %s\n", src)
			fmt.Printf("db:        %s\n", cfg.DB)
			fmt.Printf("backups:   %s\n", cfg.Backups)
			fmt.Printf("api_addr:  %s\n", cfg.APIAddr)
			fmt.Printf("api_token: %s\n", maskSecret(cfg.APIToken))
			fmt.Printf("secret:    %s\n", maskSecret(cfg.Secret))
			return nil
		},
	}
}

func defaultDataDir(global bool) string {
	if global {
		return "/var/lib/rusted"
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "rusted")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "rusted-data"
	}
	return filepath.Join(home, ".local", "share", "rusted")
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal: refuse to emit a weak token.
		panic("rusted: could not read secure random: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 8 {
		return "********"
	}
	return s[:4] + "…" + s[len(s)-4:]
}
