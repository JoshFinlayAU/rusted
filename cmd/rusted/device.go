package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/athenanetworks/rusted/internal/gitstore"
	"github.com/athenanetworks/rusted/internal/store"
	"github.com/spf13/cobra"
)

func deviceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "device",
		Aliases: []string{"dev", "devices"},
		Short:   "Manage devices",
	}
	c.AddCommand(deviceAddCmd(), deviceListCmd(), deviceUpdateCmd(), deviceRemoveCmd(), deviceEnableCmd(true), deviceEnableCmd(false))
	return c
}

func deviceAddCmd() *cobra.Command {
	var host, driver, transport, cred, group string
	var port, cmdTimeout, idleTimeout int
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
				Transport:    transport,
				CmdTimeout:   cmdTimeout,
				IdleTimeout:  idleTimeout,
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
	cmd.Flags().StringVarP(&transport, "transport", "t", "", "transport name (defaults to engine default: ssh)")
	cmd.Flags().IntVar(&cmdTimeout, "cmd-timeout", 0, "per-command timeout in seconds (default: 60)")
	cmd.Flags().IntVar(&idleTimeout, "idle-timeout", 0, "idle ms before output is considered done (default: 700)")
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
			fmt.Fprintln(tw, "NAME\tHOST\tPORT\tDRIVER\tTRANSPORT\tCMD_TIMEOUT\tIDLE_MS\tGROUP\tENABLED\tLAST_BACKUP\tLAST_STATUS")
			for _, d := range devs {
				cmdT, idleT := dashInt(d.CmdTimeout), dashInt(d.IdleTimeout)
				lastBackup := "-"
				if d.LastBackup != nil {
					lastBackup = d.LastBackup.Format("2006-01-02 15:04:05")
				}
				lastStatus := dash(d.LastStatus)
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					d.Name, d.Host, d.Port, d.Driver, dash(d.Transport), cmdT, idleT, dash(d.Group), yesno(d.Enabled), lastBackup, lastStatus)
			}
			return tw.Flush()
		},
	}
}

func deviceUpdateCmd() *cobra.Command {
	var host, driver, transport, cred, group string
	var port, cmdTimeout, idleTimeout int
	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update a device's fields (transport, driver, host, port, credential, group, timeouts)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			changed := false
			if cmd.Flags().Changed("host") && host != "" {
				if err := st.UpdateDeviceHost(args[0], host); err != nil {
					return err
				}
				changed = true
			}
			if cmd.Flags().Changed("port") {
				if err := st.UpdateDevicePort(args[0], port); err != nil {
					return err
				}
				changed = true
			}
			if driver != "" {
				if err := st.UpdateDeviceDriver(args[0], driver); err != nil {
					return err
				}
				changed = true
			}
			if transport != "" {
				if err := st.UpdateDeviceTransport(args[0], transport); err != nil {
					return err
				}
				changed = true
			}
			if cred != "" {
				c, err := st.GetCredential(cred)
				if err != nil {
					return fmt.Errorf("credential %q: %w", cred, err)
				}
				if err := st.UpdateDeviceCredential(args[0], c.ID); err != nil {
					return err
				}
				changed = true
			}
			if group != "" {
				if err := st.UpdateDeviceGroup(args[0], group); err != nil {
					return err
				}
				changed = true
			}
			if cmd.Flags().Changed("cmd-timeout") {
				if err := st.UpdateDeviceCmdTimeout(args[0], cmdTimeout); err != nil {
					return err
				}
				changed = true
			}
			if cmd.Flags().Changed("idle-timeout") {
				if err := st.UpdateDeviceIdleTimeout(args[0], idleTimeout); err != nil {
					return err
				}
				changed = true
			}
			if !changed {
				return fmt.Errorf("no fields changed; use flags --host, --port, --driver, --transport, --credential, --group, --cmd-timeout, --idle-timeout")
			}
			fmt.Printf("device %q updated\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&host, "host", "H", "", "hostname or IP")
	cmd.Flags().IntVarP(&port, "port", "P", 0, "port (default: leave unchanged)")
	cmd.Flags().StringVarP(&driver, "driver", "d", "", "platform driver (see 'rusted driver list')")
	cmd.Flags().StringVarP(&transport, "transport", "t", "", "transport name (e.g. ssh, ssh-exec, telnet)")
	cmd.Flags().StringVarP(&cred, "credential", "c", "", "credential name")
	cmd.Flags().StringVarP(&group, "group", "g", "", "sub-directory within the backup repo")
	cmd.Flags().IntVar(&cmdTimeout, "cmd-timeout", 0, "per-command timeout in seconds (default: 60)")
	cmd.Flags().IntVar(&idleTimeout, "idle-timeout", 0, "idle ms before output is considered done (default: 700)")
	return cmd
}

// lookupDevice finds a device by NAME, tolerating the repository-relative
// config path the operator sees on disk ("[group/]NAME.cfg"), and wraps
// misses in an error that names what was not found.
func lookupDevice(st *store.Store, arg string) (*store.Device, error) {
	dev, err := st.GetDevice(arg)
	if err == nil {
		return dev, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if strings.HasSuffix(arg, ".cfg") {
		// Same device, spelled as its backup file ("group/name.cfg").
		if dev, err := st.GetDevice(filepath.Base(strings.TrimSuffix(arg, ".cfg"))); err == nil {
			return dev, nil
		}
	}
	return nil, fmt.Errorf("device %q not found (use the device name, e.g. %q)", arg, strings.TrimSuffix(filepath.Base(arg), ".cfg"))
}

func deviceRemoveCmd() *cobra.Command {
	var purgeHistory bool
	cmd := &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a device and its history",
		Long: "Remove a device, its run history and its configuration file from the\n" +
			"backup git repository. NAME is the device name; the config file path as\n" +
			"shown in ./backups ([group/]NAME.cfg) is accepted too.\n" +
			"With --purge-history the configurations are also erased from the\n" +
			"repository's history itself (irreversible; requires the git-filter-repo\n" +
			"command and rewrites every commit hash).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			// Resolve before touching anything: unknown names fail cleanly and
			// the repo-relative path (group/name.cfg) is needed for git.
			dev, err := lookupDevice(st, args[0])
			if err != nil {
				return err
			}
			rel := dev.Name + ".cfg"
			if dev.Group != "" {
				rel = dev.Group + "/" + rel
			}

			if purgeHistory && !gitstore.FilterRepoAvailable() {
				return fmt.Errorf("--purge-history requires git-filter-repo, which is not installed on this host")
			}

			gs, err := openGit()
			if err != nil {
				return err
			}

			// Git first (loud failure leaves the device registered), DB after.
			if err := gs.RemoveDevice(rel); err != nil {
				return fmt.Errorf("device still registered; could not remove %s: %w", rel, err)
			}
			if purgeHistory {
				if err := gs.PurgeHistory(rel); err != nil {
					return fmt.Errorf("device still registered; could not rewrite backup history for %s: %w", rel, err)
				}
				// Keep other devices' recorded run hashes valid after the rewrite.
				if m, err := gs.CommitMap(); err == nil && len(m) > 0 {
					_ = st.RemapRunCommits(m)
				}
				fmt.Printf("history of %s irreversibly purged from the backup repository\n", rel)
			}
			if err := st.DeleteDevice(dev.Name); err != nil {
				return err
			}
			fmt.Printf("device %q removed\n", dev.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&purgeHistory, "purge-history", false,
		"also erase this device's configs from the backup repository history (irreversible, needs git-filter-repo)")
	return cmd
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

func dashInt(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
