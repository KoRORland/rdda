package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/KoRORland/rdda/internal/backup"
	"github.com/spf13/cobra"
)

func newRestoreCmd(dir *string) *cobra.Command {
	var force bool
	var passFile string
	cmd := &cobra.Command{
		Use:   "restore <file>",
		Short: "Restore EU state from an encrypted backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arc, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			pass, err := readPassphrase(passFile, false)
			if err != nil {
				return err
			}
			if err := backup.Restore(arc, pass, *dir, force); err != nil {
				return err
			}
			chownToRdda(*dir) // best-effort; no-op where the rdda user is absent
			fmt.Fprintf(cmd.OutOrStdout(), "restored EU state to %s\n", *dir)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing state in --dir")
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read passphrase from file instead of prompting")
	return cmd
}

// chownToRdda recursively chowns dir to the rdda user so the EU services can
// read the restored state. Best-effort: silently does nothing if rdda is absent.
func chownToRdda(dir string) {
	u, err := user.Lookup("rdda")
	if err != nil {
		return
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return
	}
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil {
			_ = os.Chown(p, uid, gid)
		}
		return nil
	})
}
