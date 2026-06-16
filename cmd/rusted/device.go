package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/athenanetworks/rusted/internal/store"
	"github.com/spf13/cobra"
)

func deviceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "device",
		Aliases: []string{"dev", "devices"},
		Short:   "Manage devices",
	}
	c.AddCommand(deviceAddCmd(), deviceListCmd(), deviceRemoveCmd(), deviceEnableCmd(true), deviceEnableCmd(false))
	return c
}

func deviceAddCmd() *cobra.Command {
	var host, driver, cred, group string
	var port int
	var disabled bool
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Add a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			c, err := st.GetCredential(cred)
			if err != nil {
				return fmt.Errorf("credential %q: %w", cred, err)
			}
			d := &store.Device{
				Name: args[0], Host: host, Port: port, Driver: driver,
				CredentialID: c.ID, Group: group, Enabled: !disabled,
			}
			if d.Host == "" {
				d.Host = args[0]
			}
			if _, err := st.CreateDevice(d); err != nil {
				return err
			}
			fmt.Printf("device %q added\n", d.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "hostname or IP (defaults to device name)")
	cmd.Flags().IntVarP(&port, "port", "P", 22, "SSH port")
	cmd.Flags().StringVarP(&driver, "driver", "d", "generic", "platform driver (see 'rusted driver list')")
	cmd.Flags().StringVarP(&cred, "credential", "c", "", "credential name (required)")
	cmd.Flags().StringVarP(&group, "group", "g", "", "sub-directory within the backup repo")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "add the device disabled")
	cmd.MarkFlagRequired("credential")
	return cmd
}

func deviceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List devices",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			devs, err := st.ListDevices()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tHOST\tPORT\tDRIVER\tGROUP\tENABLED")
			for _, d := range devs {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n", d.Name, d.Host, d.Port, d.Driver, dash(d.Group), yesno(d.Enabled))
			}
			return tw.Flush()
		},
	}
}

func deviceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a device and its history",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.DeleteDevice(args[0]); err != nil {
				return err
			}
			fmt.Printf("device %q removed\n", args[0])
			return nil
		},
	}
}

func deviceEnableCmd(enable bool) *cobra.Command {
	use, word := "enable NAME", "enabled"
	if !enable {
		use, word = "disable NAME", "disabled"
	}
	return &cobra.Command{
		Use:   use,
		Short: fmt.Sprintf("Mark a device %s", word),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.SetDeviceEnabled(args[0], enable); err != nil {
				return err
			}
			fmt.Printf("device %q %s\n", args[0], word)
			return nil
		},
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
