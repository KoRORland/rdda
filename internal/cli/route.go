package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/geoipupdate"
	"github.com/KoRORland/rdda/internal/routing"
	"github.com/spf13/cobra"
)

func newRouteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Inspect and refresh RU traffic routing (direct vs EU tunnel)",
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
	cmd.AddCommand(test, newUpdateGeoIPCmd())
	return cmd
}

// newUpdateGeoIPCmd refreshes the RU node's geoip-ru rule-set. Run by
// rdda-geoip.timer; safe to run by hand. Fail-safe: a failed fetch/validate
// keeps the current file and exits 0 so the timer never enters a failed state.
func newUpdateGeoIPCmd() *cobra.Command {
	var path, url string
	cmd := &cobra.Command{
		Use:   "update-geoip",
		Short: "Refresh the RU geoip-ru rule-set from upstream (reloads sing-box only on change)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := geoipupdate.Updater{
				Path:     path,
				URL:      url,
				Fetch:    fetchWithRetry,
				Validate: validateGeoIP,
				Reload:   reloadSingbox,
				Chown:    chownToRDDA,
			}
			res, err := u.Run()
			out := cmd.OutOrStdout()
			switch {
			case res.Skipped:
				// Non-fatal: keep the old data, report, exit 0 (don't fail the timer).
				fmt.Fprintf(cmd.ErrOrStderr(), "geoip update skipped: %s\n", res.Reason)
				return nil
			case err != nil:
				// Data may already be written (e.g. reload failed); surface why.
				if res.Reason != "" {
					return errors.New(res.Reason)
				}
				return err
			default:
				fmt.Fprintf(out, "geoip-ru: %s\n", res.Reason)
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&path, "path", "/etc/rdda/geoip-ru.srs", "local geoip-ru rule-set path")
	cmd.Flags().StringVar(&url, "url", geoipupdate.DefaultURL, "upstream rule-set URL")
	return cmd
}

// fetchWithRetry downloads url with a connect/overall timeout and a few retries,
// mirroring install.sh's resilient fetch (GitHub-from-RU can be flaky).
func fetchWithRetry(url string) ([]byte, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
		} else {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			resp.Body.Close()
			switch {
			case rerr != nil:
				lastErr = rerr
			case resp.StatusCode != 200:
				lastErr = fmt.Errorf("%s → HTTP %d", url, resp.StatusCode)
			default:
				return body, nil
			}
		}
		if attempt < 4 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
	}
	return nil, lastErr
}

// validateGeoIP runs the structural check, then — if sing-box is present — a
// real load test so a well-formed-but-not-a-rule-set file is still rejected.
func validateGeoIP(data []byte) error {
	if err := geoipupdate.ValidateSRS(data); err != nil {
		return err
	}
	sb, err := exec.LookPath("sing-box")
	if err != nil {
		return nil // sing-box absent (e.g. EU/dev): structural check is all we can do
	}
	tmp, err := os.CreateTemp("", "geoip-*.srs")
	if err != nil {
		return nil // can't verify; don't block on it
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		return nil
	}
	tmp.Close()
	// A load error ("failed to read rule-set") means it's not a valid .srs.
	if out, err := exec.Command(sb, "rule-set", "match", tmp.Name(), "127.0.0.1").CombinedOutput(); err != nil &&
		strings.Contains(strings.ToLower(string(out)), "rule-set") {
		return fmt.Errorf("sing-box rejected the rule-set: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// reloadSingbox reloads the data plane via sudo — the rdda service user is
// granted exactly `systemctl reload-or-restart rdda-singbox` via
// /etc/sudoers.d/rdda-reload (the same grant rdda-pull uses). As root, sudo is
// a harmless passthrough.
func reloadSingbox() error {
	return exec.Command("sudo", "systemctl", "reload-or-restart", "rdda-singbox").Run()
}

func chownToRDDA(path string) error {
	u, err := user.Lookup("rdda")
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(path, uid, gid)
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
