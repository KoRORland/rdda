package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/KoRORland/rdda/internal/routing"
	"github.com/spf13/cobra"
)

func newRouteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Inspect RU traffic routing (direct vs EU tunnel)",
	}
	var configPath string
	var showTrace bool
	test := &cobra.Command{
		Use:   "test <ip-or-domain> [more...]",
		Short: "Show where a destination routes (direct or through the EU tunnel) and why",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("read %s: %w (run on the RU node, or pass --config)", configPath, err)
			}
			out := cmd.OutOrStdout()
			for _, input := range args {
				d, err := routing.Evaluate(data, input, realGeoIPMatch)
				if err != nil {
					return err
				}
				verdict := "DIRECT"
				if d.Tunneled() {
					verdict = "TUNNEL (EU exit)"
				}
				fmt.Fprintf(out, "%-40s → %-16s [rule: %s]\n", d.Input, verdict, d.Rule)
				if showTrace {
					for _, s := range d.Trace {
						fmt.Fprintf(out, "    %s\n", s)
					}
				}
			}
			return nil
		},
	}
	test.Flags().StringVar(&configPath, "config", "/etc/rdda/singbox.json", "rendered RU sing-box config to evaluate against")
	test.Flags().BoolVar(&showTrace, "trace", false, "print the per-rule evaluation trace")
	cmd.AddCommand(test)
	return cmd
}

// realGeoIPMatch asks sing-box whether ip is in the rule-set at srsPath. It is
// best-effort: sing-box's `rule-set match` output is parsed for a match marker,
// and any failure to run it yields determinate=false so the caller labels the
// geoip step UNKNOWN rather than guessing a verdict.
func realGeoIPMatch(srsPath, ip string) (matched, determinate bool) {
	out, err := exec.Command("sing-box", "rule-set", "match", srsPath, ip).CombinedOutput()
	low := strings.ToLower(string(out))
	if err != nil && len(strings.TrimSpace(low)) == 0 {
		return false, false // couldn't run / no output → unknown
	}
	if strings.Contains(low, "no match") || strings.Contains(low, "not match") {
		return false, true
	}
	if strings.Contains(low, "match") {
		return true, true
	}
	// Ran but produced no recognizable verdict: don't claim a match.
	return false, true
}
