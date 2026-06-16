package main

import (
	"fmt"
	"net/http"

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
				token = cfg.APIToken
			}
			if addr == "" {
				addr = cfg.APIAddr
			}
			if token == "" {
				return fmt.Errorf("an API token is required: set it in the config file, --token, or RUSTED_API_TOKEN")
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
	cmd.Flags().StringVar(&addr, "addr", "", "listen address (default from config or :8080)")
	cmd.Flags().StringVar(&token, "token", "", "bearer token (default from config or RUSTED_API_TOKEN)")
	return cmd
}
