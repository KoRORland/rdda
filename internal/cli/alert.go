package cli

import (
	"fmt"

	"github.com/KoRORland/rdda/internal/alert"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

func newAlertCmd(dir *string) *cobra.Command {
	var test bool
	var command string
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Evaluate alert conditions and email on transitions (EU; run by rdda-alert.timer)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			if command != "" {
				cfg.Alert.Command = command
			}
			e := alert.New(*dir, cfg)
			if test {
				if err := e.SendTest(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "test alert sent to "+cfg.Alert.Email)
				return nil
			}
			fired, resolved, err := e.Run()
			for _, k := range fired {
				fmt.Fprintln(cmd.OutOrStdout(), "ALERT "+k)
			}
			for _, k := range resolved {
				fmt.Fprintln(cmd.OutOrStdout(), "RESOLVED "+k)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&test, "test", false, "send one test email and exit")
	cmd.Flags().StringVar(&command, "command", "", "override the alert command (default config alert.command, else msmtp)")
	return cmd
}
