package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/athenanetworks/rusted/internal/store"
	"github.com/spf13/cobra"
)

func credCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "cred",
		Aliases: []string{"credential", "creds"},
		Short:   "Manage login credentials",
	}
	c.AddCommand(credAddCmd(), credListCmd(), credRemoveCmd())
	return c
}

func credAddCmd() *cobra.Command {
	var username, password, enable, keyFile string
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Add a credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			cred := &store.Credential{Name: args[0], Username: username, Password: password, Enable: enable}
			if keyFile != "" {
				b, err := os.ReadFile(keyFile)
				if err != nil {
					return fmt.Errorf("read key file: %w", err)
				}
				cred.PrivateKey = string(b)
			}
			if _, err := st.CreateCredential(cred); err != nil {
				return err
			}
			fmt.Printf("credential %q added\n", cred.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&username, "username", "u", "", "login username (required)")
	cmd.Flags().StringVarP(&password, "password", "p", "", "login password")
	cmd.Flags().StringVarP(&enable, "enable", "e", "", "enable/privileged password")
	cmd.Flags().StringVarP(&keyFile, "key", "k", "", "path to a PEM private key")
	cmd.MarkFlagRequired("username")
	return cmd
}

func credListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			creds, err := st.ListCredentials()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tUSERNAME\tPASSWORD\tKEY\tENABLE")
			for _, c := range creds {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.Name, c.Username,
					mask(c.Password), yesno(c.PrivateKey != ""), mask(c.Enable))
			}
			return tw.Flush()
		},
	}
}

func credRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a credential",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.DeleteCredential(args[0]); err != nil {
				return err
			}
			fmt.Printf("credential %q removed\n", args[0])
			return nil
		},
	}
}

func mask(s string) string {
	if s == "" {
		return "-"
	}
	return "********"
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
