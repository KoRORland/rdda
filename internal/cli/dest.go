package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/KoRORland/rdda/internal/dest"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

// discoverDestFn resolves the nearest validating REALITY dest by probing the
// curated set from this node. It is a package var so tests (and any offline
// path) can stub out the network.
var discoverDestFn = func(ctx context.Context, timeout time.Duration) (string, error) {
	best, ok := dest.Best(dest.Discover(ctx, dest.Candidates(), dest.DefaultProber{Timeout: timeout}))
	if !ok {
		return "", fmt.Errorf("no candidate validated as a REALITY dest from this node")
	}
	return best.Host, nil
}

// discoverOpts are the knobs for `rdda dest discover`.
type discoverOpts struct {
	which   string // "client", "tunnel", or "both"
	write   bool
	timeout time.Duration
}

func newDestCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{Use: "dest", Short: "Discover and manage the REALITY handshake destination"}
	cmd.AddCommand(newDestDiscoverCmd(dir))
	return cmd
}

func newDestDiscoverCmd(dir *string) *cobra.Command {
	var opts discoverOpts
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Auto-pick a nearby, plausible REALITY dest by probing a curated set from THIS node",
		Long: "Probes a curated set of popular, CDN-fronted domains from this node, validating each\n" +
			"as a real TLS 1.3 + HTTP/2 site with a valid cert and a direct 200, and auto-picks the\n" +
			"nearest one that passes. Removes the fleet-wide signature of a shared default dest: each\n" +
			"node discovers its own, so no two deployments front the same host by default.\n\n" +
			"Prints the ranked candidates and the pick. With --write, updates config.yaml (the REALITY\n" +
			"keys are preserved; only the borrowed host changes). Run --write on the EU source of truth\n" +
			"BEFORE handing out client configs — the dest is baked into every client's subscription.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			return runDiscover(cmd.Context(), cmd.OutOrStdout(), s, opts, dest.DefaultProber{Timeout: opts.timeout})
		},
	}
	cmd.Flags().StringVar(&opts.which, "which", "client", "which REALITY dest to set: client, tunnel, or both")
	cmd.Flags().BoolVar(&opts.write, "write", false, "persist the pick into config.yaml (default: report only)")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 8*time.Second, "per-candidate probe timeout")
	return cmd
}

// runDiscover is the testable core: it probes with the given Prober, reports the
// ranking, and (when opts.write) persists the pick. Tests inject a fake Prober
// and an in-memory store.
func runDiscover(ctx context.Context, w io.Writer, s *state.Store, opts discoverOpts, prober dest.Prober) error {
	if opts.which != "client" && opts.which != "tunnel" && opts.which != "both" {
		return fmt.Errorf("--which must be client, tunnel, or both (got %q)", opts.which)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	results := dest.Discover(ctx, dest.Candidates(), prober)
	printRanking(w, results)

	best, ok := dest.Best(results)
	if !ok {
		return fmt.Errorf("no candidate validated as a REALITY dest from this node — " +
			"check egress, or pin one explicitly with --client-sni/--tunnel-sni")
	}
	fmt.Fprintf(w, "\npick: %s (%s, nearest validating)\n", best.Host, best.RTT.Round(time.Millisecond))

	if !opts.write {
		fmt.Fprintf(w, "(report only; re-run with --write --which %s to persist)\n", opts.which)
		return nil
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config to write the dest: %w", err)
	}
	applyDest(&cfg, opts.which, best.Host)
	if err := s.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Fprintf(w, "wrote %s dest = %s to config.yaml (REALITY keys preserved; re-render the config)\n", opts.which, best.Host)
	return nil
}

// applyDest sets the borrowed host on the selected REALITY hop(s), preserving
// keys/short-ids. Target carries the ":443" the renderer splits back out.
func applyDest(cfg *state.Config, which, host string) {
	target := host + ":443"
	if which == "client" || which == "both" {
		cfg.ClientReality.ServerName = host
		cfg.ClientReality.Target = target
	}
	if which == "tunnel" || which == "both" {
		cfg.TunnelReality.ServerName = host
		cfg.TunnelReality.Target = target
	}
}

func printRanking(w io.Writer, results []dest.Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HOST\tRTT\tRESULT")
	for _, r := range results {
		if r.OK {
			fmt.Fprintf(tw, "%s\t%s\tok\n", r.Host, r.RTT.Round(time.Millisecond))
		} else {
			rtt := "-"
			if r.RTT > 0 {
				rtt = r.RTT.Round(time.Millisecond).String()
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Host, rtt, r.Reason)
		}
	}
	_ = tw.Flush()
}
