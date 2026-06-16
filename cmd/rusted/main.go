// Command rusted is a network device configuration backup tool — a modern
// replacement for RANCID and Oxidized. It connects to devices over pluggable
// transports (SSH built in), captures their running configuration using
// per-platform drivers, and versions the results in a git repository.
package main

import (
	"fmt"
	"os"

	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/store"
	"github.com/spf13/cobra"
)

var (
	flagDB      string
	flagBackups string
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
	}
	root.PersistentFlags().StringVar(&flagDB, "db", env("RUSTED_DB", "rusted.db"), "path to the SQLite database")
	root.PersistentFlags().StringVar(&flagBackups, "backups", env("RUSTED_BACKUPS", "backups"), "path to the git backup repository")

	root.AddCommand(
		initCmd(),
		credCmd(),
		deviceCmd(),
		driverCmd(),
		backupCmd(),
		serveCmd(),
	)
	return root
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// openStore opens the SQLite store.
func openStore() (*store.Store, error) {
	return store.Open(flagDB)
}

// openGit opens the backup git repository.
func openGit() (*gitstore.Store, error) {
	return gitstore.Open(flagBackups)
}
