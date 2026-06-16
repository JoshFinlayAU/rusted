// Command rusted is a network device configuration backup tool — a modern
// replacement for RANCID and Oxidized. It connects to devices over pluggable
// transports (SSH built in), captures their running configuration using
// per-platform drivers, and versions the results in a git repository.
package main

import (
	"fmt"
	"os"

	"github.com/athenanetworks/rusted/internal/config"
	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/secret"
	"github.com/athenanetworks/rusted/internal/store"
	"github.com/spf13/cobra"
)

var (
	flagDB      string
	flagBackups string
	flagConfig  string

	// cfg holds the resolved configuration (file + env + flags). It is
	// populated by the root command's PersistentPreRunE before any subcommand
	// RunE executes.
	cfg config.Config
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rusted",
		Short: "Network device configuration backup tool (RANCID/Oxidized replacement)",
		Long: "rusted backs up network device configurations over SSH and versions " +
			"them in a git repository. Credentials and devices are stored in SQLite.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			loaded, err := config.Load(flagConfig)
			if err != nil {
				return err
			}
			// CLI flags win over everything when explicitly provided.
			if cmd.Flags().Changed("db") {
				loaded.DB = flagDB
			}
			if cmd.Flags().Changed("backups") {
				loaded.Backups = flagBackups
			}
			cfg = loaded
			// Expose the resolved secret to the secret package, which reads it
			// from the environment. This lets the config file (not just an env
			// var) drive encryption-at-rest.
			if cfg.Secret != "" {
				_ = os.Setenv(secret.EnvKey, cfg.Secret)
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&flagConfig, "config", "", "path to config file (default: $RUSTED_CONFIG, ~/.config/rusted/config.toml, /etc/rusted/config.toml)")
	root.PersistentFlags().StringVar(&flagDB, "db", "", "path to the SQLite database (default: rusted.db or config)")
	root.PersistentFlags().StringVar(&flagBackups, "backups", "", "path to the git backup repository (default: backups or config)")

	root.AddCommand(
		initCmd(),
		configCmd(),
		credCmd(),
		deviceCmd(),
		driverCmd(),
		backupCmd(),
		serveCmd(),
	)
	return root
}

// openStore opens the SQLite store using the resolved config.
func openStore() (*store.Store, error) {
	return store.Open(cfg.DB)
}

// openGit opens the backup git repository using the resolved config.
func openGit() (*gitstore.Store, error) {
	return gitstore.Open(cfg.Backups)
}
