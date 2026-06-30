package cli

import (
	"fmt"

	"github.com/KoRORland/rdda/internal/heal"
	"github.com/spf13/cobra"
)

// healer is the heal seam (faked in tests). *heal.Healer satisfies it.
type healer interface {
	Run() (healed []string, err error)
}

var newHealer = func() healer { return heal.New() }

func newHealCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "heal",
		Short: "Restart units stuck in systemd 'failed' state (run by rdda-heal.timer)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			healed, err := newHealer().Run()
			for _, u := range healed {
				fmt.Fprintln(cmd.OutOrStdout(), "HEALED "+u)
			}
			return err
		},
	}
}
