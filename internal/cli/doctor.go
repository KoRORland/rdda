package cli

import (
	"fmt"

	"github.com/KoRORland/rdda/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd(dir *string) *cobra.Command {
	var probeURL string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run active diagnostics (units, REALITY dest, control channel, end-to-end tunnel)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			role := "RU (entry)"
			cs := doctor.New(*dir).Run(probeURL)
			// Header role mirrors doctor's own detection.
			if anyEU(cs) {
				role = "EU (controller)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "RDDA doctor — %s\n%s", role, doctor.Render(cs))
			if doctor.AnyFail(cs) {
				return fmt.Errorf("doctor: one or more checks failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&probeURL, "probe-url", "https://www.cloudflare.com/cdn-cgi/trace", "URL fetched through the tunnel by the RU end-to-end probe")
	return cmd
}

// anyEU reports whether the check set is an EU run (has an EU-only check name).
func anyEU(cs []doctor.Check) bool {
	for _, c := range cs {
		if c.Name == "sub endpoint" || c.Name == "RU beat" || c.Name == "cloudflared" || c.Name == "config" {
			return true
		}
	}
	return false
}
