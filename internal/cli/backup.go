package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/KoRORland/rdda/internal/backup"
	"github.com/spf13/cobra"
)

func newBackupCmd(dir *string) *cobra.Command {
	var out, passFile string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write an encrypted backup of the EU state (config.yaml + clients/)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pass, err := readPassphrase(passFile, true)
			if err != nil {
				return err
			}
			arc, err := backup.Create(*dir, pass)
			if err != nil {
				return err
			}
			if out == "-" {
				_, err = cmd.OutOrStdout().Write(arc)
				return err
			}
			if out == "" {
				out = "rdda-backup-" + time.Now().UTC().Format("20060102T150405Z") + ".rdda"
			}
			if err := os.WriteFile(out, arc, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote encrypted backup to %s (keep your passphrase — it cannot be recovered)\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output file (default rdda-backup-<UTC>.rdda; '-' for stdout)")
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read passphrase from file instead of prompting")
	return cmd
}
