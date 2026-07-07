package cli

import (
	"fmt"
	"os"

	"github.com/KoRORland/rdda/internal/selfupdate"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

// updater is the selfupdate seam (faked in tests). *selfupdate.Updater satisfies it.
type updater interface {
	Check() (current, latest string, newer bool, err error)
	Update() (from, to string, err error)
	Rollback() error
}

var newUpdater = func(current string) updater { return selfupdate.New(current) }

func newUpdateCmd(dir *string) *cobra.Command {
	var check, rollback bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Self-update the rdda binary to the latest release (rolls back on failure)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := newUpdater(Version)
			if check {
				cur, latest, newer, err := u.Check()
				if err != nil {
					return err
				}
				if newer {
					fmt.Fprintf(cmd.OutOrStdout(), "%s installed, %s available\n", cur, latest)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "up to date (%s)\n", cur)
				}
				return nil
			}
			if os.Geteuid() != 0 {
				return fmt.Errorf("rdda update must run as root")
			}
			if rollback {
				if err := u.Rollback(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "rolled back to previous binary")
				return nil
			}
			from, to, err := u.Update()
			if err != nil {
				return err
			}
			// Normalize state-dir ownership on every update: a root-run
			// `rdda client add` can leave a root-owned client file the rdda
			// service user can't read (an opaque sub-server 500). Best-effort.
			normalizeStateOwnership(cmd, *dir)
			if from == to {
				fmt.Fprintf(cmd.OutOrStdout(), "already at %s\n", to)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "updated %s -> %s\n", from, to)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report installed vs latest version; make no changes")
	cmd.Flags().BoolVar(&rollback, "rollback", false, "restore the previous binary (rdda.prev)")
	return cmd
}

// normalizeStateOwnership best-effort chowns the state dir back to the rdda
// service user. Failures are warned, not fatal — the binary update already
// succeeded and this is a hygiene step.
func normalizeStateOwnership(cmd *cobra.Command, dir string) {
	s, err := state.Open(dir)
	if err != nil {
		return
	}
	if err := s.ChownTree(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not normalize %s ownership to rdda: %v\n", dir, err)
	}
}
