package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/athenanetworks/rusted/internal/backup"
	"github.com/spf13/cobra"
)

func backupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup",
		Short: "Run backups and view history",
	}
	c.AddCommand(backupRunCmd(), backupHistoryCmd())
	return c
}

func backupRunCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "run [NAME]",
		Short: "Back up a device (or --all enabled devices)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) != 1 {
				return fmt.Errorf("specify a device NAME or use --all")
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
			eng := backup.New(st, gs)
			ctx := context.Background()

			var results []*backup.Result
			if all {
				results, err = eng.BackupAll(ctx)
				if err != nil {
					return err
				}
			} else {
				r, err := eng.BackupDevice(ctx, args[0])
				if err != nil {
					return err
				}
				results = []*backup.Result{r}
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "DEVICE\tSTATUS\tCOMMIT\tDETAIL")
			failed := 0
			for _, r := range results {
				commit := r.Commit
				if len(commit) > 8 {
					commit = commit[:8]
				}
				if r.Status == "failed" {
					failed++
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Device, r.Status, dash(commit), r.Message)
			}
			tw.Flush()
			if failed > 0 {
				return fmt.Errorf("%d device(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "back up all enabled devices")
	return cmd
}

func backupHistoryCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history NAME",
		Short: "Show backup history for a device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			runs, err := st.History(args[0], limit)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "STARTED\tSTATUS\tBYTES\tCOMMIT\tDETAIL")
			for _, r := range runs {
				commit := r.Commit
				if len(commit) > 8 {
					commit = commit[:8]
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					r.StartedAt.Format("2006-01-02 15:04:05"), r.Status, r.Bytes, dash(commit), r.Message)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "max rows to show (0 = all)")
	return cmd
}
