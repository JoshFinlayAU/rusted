package main

import (
	"errors"
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
	c.AddCommand(deviceAddCmd(), deviceListCmd(), deviceUpdateCmd(), deviceRenameCmd(), deviceRemoveCmd(),
		deviceEnableCmd(true), deviceEnableCmd(false))
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
			devs, err := st.ListDevicesWithStatus()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tHOST\tPORT\tDRIVER\tTRANSPORT\tGROUP\tENABLED\tLAST_BACKUP\tLAST_STATUS")
			for _, d := range devs {
				last := "-"
				if d.LastBackup != nil {
					last = d.LastBackup.Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", d.Name, d.Host, d.Port, d.Driver, dash(d.Transport),
					dash(d.Group), yesno(d.Enabled), last, dash(d.LastStatus))
			}
			return tw.Flush()
		},
	}
}

func deviceUpdateCmd() *cobra.Command {
	var host, driver, transport, cred, group string
	var port int
	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Change a device's host, port, driver, transport, credential or group",
		Long: "Change the given fields of a device; flags left out keep their value.\n" +
			"--transport \"\" and --driver \"\" go back to the defaults, --group \"\"\n" +
			"to the repository root. Changing the group moves the stored config\n" +
			"(and its history) in the backup repository.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			old, err := st.GetDevice(args[0])
			if err != nil {
				return fmt.Errorf("device %q: %w", args[0], err)
			}
			d := *old
			f := cmd.Flags()
			if !f.Changed("host") && !f.Changed("port") && !f.Changed("driver") && !f.Changed("transport") &&
				!f.Changed("credential") && !f.Changed("group") {
				return fmt.Errorf("nothing to change; use --host, --port, --driver, --transport, --credential or --group")
			}
			if f.Changed("host") {
				d.Host = host
			}
			if f.Changed("port") {
				d.Port = port
			}
			if f.Changed("driver") {
				d.Driver = driver
			}
			if f.Changed("transport") {
				d.Transport = transport
			}
			if f.Changed("credential") {
				c, err := st.GetCredential(cred)
				if err != nil {
					return fmt.Errorf("credential %q: %w", cred, err)
				}
				d.CredentialID = c.ID
			}
			if f.Changed("group") {
				d.Group = group
			}

			var hook store.Hook
			if d.Group != old.Group {
				if err := store.ValidateGroup(d.Group); err != nil {
					return err
				}
				if hook, err = moveHook(old, &d); err != nil {
					return err
				}
			}
			if err := st.UpdateDevice(&d, hook); err != nil {
				return fmt.Errorf("device %q not updated: %w", old.Name, err)
			}
			fmt.Printf("device %q updated\n", d.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "hostname or IP")
	cmd.Flags().IntVarP(&port, "port", "P", 22, "port (1-65535)")
	cmd.Flags().StringVarP(&driver, "driver", "d", "", "platform driver (see 'rusted driver list'); \"\" = generic")
	cmd.Flags().StringVarP(&transport, "transport", "t", "", "transport name; \"\" = driver/engine default")
	cmd.Flags().StringVarP(&cred, "credential", "c", "", "credential name")
	cmd.Flags().StringVarP(&group, "group", "g", "", "sub-directory within the backup repo; \"\" = root")
	return cmd
}

func deviceRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename OLD NEW",
		Short: "Rename a device, keeping its backup history",
		Long: "Rename a device in place: its backup runs stay attached, and its stored\n" +
			"config is moved in the backup repository (git mv), so every earlier\n" +
			"version can still be viewed and diffed under the new name. The host is\n" +
			"not changed; use 'device update --host' for that.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			old, err := st.GetDevice(args[0])
			if err != nil {
				return fmt.Errorf("device %q: %w", args[0], err)
			}
			d := *old
			d.Name = args[1]
			if err := store.ValidateDeviceName(d.Name); err != nil {
				return err
			}
			if _, err := st.GetDevice(d.Name); err == nil {
				return fmt.Errorf("device %q already exists", d.Name)
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			hook, err := moveHook(old, &d)
			if err != nil {
				return err
			}
			if err := st.RenameDevice(old.Name, d.Name, hook); err != nil {
				return fmt.Errorf("device %q not renamed: %w", old.Name, err)
			}
			fmt.Printf("device %q renamed to %q\n", old.Name, d.Name)
			return nil
		},
	}
}

func deviceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a device, its run history and its stored config",
		Long: "Remove a device and its run history, and delete its config file from\n" +
			"the backup repository in a commit. Earlier versions remain in the\n" +
			"repository's git history.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			d, err := st.GetDevice(args[0])
			if err != nil {
				return fmt.Errorf("device %q: %w", args[0], err)
			}
			var hook store.Hook
			if pathErr := errors.Join(store.ValidateDeviceName(d.Name), store.ValidateGroup(d.Group)); pathErr != nil {
				// Stored before names were validated: don't derive a repository
				// path from it, just drop the row.
				fmt.Fprintf(os.Stderr, "warning: config file left in the backup repository: %v\n", pathErr)
			} else {
				gs, err := openGit()
				if err != nil {
					return err
				}
				rel := d.ConfigPath()
				hook = func() (func() error, error) {
					return gs.RemoveFile(rel, "remove "+d.Name+" (device deleted)")
				}
			}
			if err := st.DeleteDeviceTx(d.Name, hook); err != nil {
				return fmt.Errorf("device %q not removed: %w", d.Name, err)
			}
			fmt.Printf("device %q removed\n", d.Name)
			return nil
		},
	}
}

// moveHook returns the hook that moves old's stored config to where d's
// belongs, or nil when the path doesn't change. A row stored before names were
// validated has no trustworthy path, so its file is left where it is.
func moveHook(old, d *store.Device) (store.Hook, error) {
	from, to := old.ConfigPath(), d.ConfigPath()
	if from == to {
		return nil, nil
	}
	if err := errors.Join(store.ValidateDeviceName(old.Name), store.ValidateGroup(old.Group)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: not moving the stored config: %v\n", err)
		return nil, nil
	}
	gs, err := openGit()
	if err != nil {
		return nil, err
	}
	return func() (func() error, error) {
		return gs.MoveFile(from, to, "move "+from+" to "+to)
	}, nil
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
