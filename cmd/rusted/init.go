package main

import (
	"fmt"

	"github.com/athenanetworks/rusted/internal/secret"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialise the database and backup git repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			gs, err := openGit()
			if err != nil {
				return err
			}
			fmt.Printf("database:  %s\n", cfg.DB)
			fmt.Printf("backups:   %s\n", gs.Dir)
			if cfg.Path != "" {
				fmt.Printf("config:    %s\n", cfg.Path)
			}
			if secret.Enabled() {
				fmt.Println("encryption: enabled")
			} else {
				fmt.Println("encryption: DISABLED — set a secret (config 'secret' or RUSTED_SECRET) to encrypt stored credentials")
			}
			fmt.Println("rusted initialised.")
			return nil
		},
	}
}
