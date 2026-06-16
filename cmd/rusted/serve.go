package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/athenanetworks/rusted/internal/api"
	"github.com/athenanetworks/rusted/internal/backup"
	"github.com/spf13/cobra"
)

func serveCmd() *cobra.Command {
	var addr, token string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP API used by the LibreNMS integration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("RUSTED_API_TOKEN")
			}
			if token == "" {
				return fmt.Errorf("an API token is required: set --token or RUSTED_API_TOKEN")
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			gs, err := openGit()
			if err != nil {
				return err
			}
			srv := &api.Server{
				Store:  st,
				Git:    gs,
				Engine: backup.New(st, gs),
				Token:  token,
			}
			fmt.Printf("rusted API listening on %s\n", addr)
			return http.ListenAndServe(addr, srv.Handler())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", env("RUSTED_API_ADDR", ":8080"), "listen address")
	cmd.Flags().StringVar(&token, "token", "", "bearer token (or set RUSTED_API_TOKEN)")
	return cmd
}
