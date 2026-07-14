package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/KoRORland/rdda/internal/backup"
	"github.com/KoRORland/rdda/internal/shellword"
	"github.com/spf13/cobra"
)

func newBackupCmd(dir *string) *cobra.Command {
	var out, passFile, push string
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
			// Push the (already-encrypted) archive to an operator-controlled
			// off-node destination first, so a lost EU node isn't a lost source of
			// truth (#13). Push before the local write so a push failure surfaces
			// loudly rather than being masked by a successful local file.
			if push != "" {
				if err := pushBackup(cmd, push, arc); err != nil {
					return err
				}
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
	cmd.Flags().StringVar(&push, "push", "", "also stream the encrypted archive to a command's stdin, e.g. --push 'rclone rcat remote:rdda/backup.rdda' or --push \"ssh host 'cat > /backups/rdda.rdda'\"")
	return cmd
}

// pushBackup runs the operator's --push command with the encrypted archive on its
// stdin. The command is parsed shell-style (quotes/escapes) but not run through a
// shell, so there is no injection surface beyond what the operator already typed.
func pushBackup(cmd *cobra.Command, push string, arc []byte) error {
	parts, err := shellword.Split(push)
	if err != nil {
		return fmt.Errorf("bad --push: %w", err)
	}
	if len(parts) == 0 {
		return fmt.Errorf("--push is empty after parsing")
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdin = bytes.NewReader(arc)
	c.Stdout = cmd.ErrOrStderr()
	c.Stderr = cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "pushed encrypted backup via: %s\n", parts[0])
	return nil
}
