package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/athenanetworks/rusted/internal/driver"
	"github.com/spf13/cobra"
)

func driverCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "driver",
		Aliases: []string{"drivers"},
		Short:   "Inspect platform drivers",
	}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available platform drivers",
		RunE: func(cmd *cobra.Command, args []string) error {
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tDESCRIPTION\tCONFIG COMMANDS")
			for _, d := range driver.List() {
				cmds := ""
				for i, c := range d.Config {
					if i > 0 {
						cmds += "; "
					}
					cmds += c
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", d.Name, d.Description, cmds)
			}
			return tw.Flush()
		},
	})
	return c
}
