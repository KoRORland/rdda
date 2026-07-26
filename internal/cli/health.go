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
	var to, token, singboxPath, probeURL string
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
			// End-to-end reachability sensor: does the exit path actually work,
			// not just "is sing-box active" (liveness ≠ reachability). A missing
			// config is inconclusive (Unknown), never a false "blocked".
			if sb, err := os.ReadFile(singboxPath); err != nil {
				rep.Reach = health.Reach{Status: health.ReachUnknown, Detail: "no singbox.json: " + err.Error()}
			} else {
				rep.Reach = health.Reachability(sb, probeURL)
			}
			return health.Send(&http.Client{Timeout: 15 * time.Second}, to, token, rep)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "EU /ru/health URL (default $RDDA_HEALTH_TO)")
	cmd.Flags().StringVar(&token, "token", "", "pull token (default $RDDA_PULL_TOKEN)")
	cmd.Flags().StringVar(&singboxPath, "singbox", "/etc/rdda/singbox.json", "RU sing-box config to build the through-tunnel reachability probe from")
	cmd.Flags().StringVar(&probeURL, "probe-url", "https://www.cloudflare.com/cdn-cgi/trace", "URL fetched through the tunnel to verify end-to-end reachability")
	return cmd
}
