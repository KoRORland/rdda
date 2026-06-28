package cli

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/KoRORland/rdda/internal/health"
	"github.com/spf13/cobra"
)

func newHealthCmd() *cobra.Command {
	var to, token string
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Send one RU health beat to the EU controller (run by rdda-health.timer)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if to == "" {
				to = os.Getenv("RDDA_HEALTH_TO")
			}
			if token == "" {
				token = os.Getenv("RDDA_PULL_TOKEN")
			}
			if to == "" {
				return fmt.Errorf("no health target: pass --to or set $RDDA_HEALTH_TO")
			}
			rep := health.Gather(health.DefaultRunner, Version)
			return health.Send(&http.Client{Timeout: 15 * time.Second}, to, token, rep)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "EU /ru/health URL (default $RDDA_HEALTH_TO)")
	cmd.Flags().StringVar(&token, "token", "", "pull token (default $RDDA_PULL_TOKEN)")
	return cmd
}
