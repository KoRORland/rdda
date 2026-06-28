package cli

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// realityDest is the handshake target a REALITY inbound relays to — the borrowed
// site whose TLS identity it mimics. It is what appears as the SNI on the
// inspected client→RU hop, and the address this node must be able to reach.
type realityDest struct {
	host string
	port int
}

// extractRealityDest reads a rendered sing-box config and returns the first
// REALITY inbound's handshake dest, if any. ok=false means there is no REALITY
// inbound to verify (e.g. an EU node fronted by Cloudflare runs a plain
// WebSocket inbound with no REALITY).
func extractRealityDest(cfgJSON []byte) (realityDest, bool) {
	var doc struct {
		Inbounds []struct {
			TLS struct {
				Reality struct {
					Enabled   bool `json:"enabled"`
					Handshake struct {
						Server     string `json:"server"`
						ServerPort int    `json:"server_port"`
					} `json:"handshake"`
				} `json:"reality"`
			} `json:"tls"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfgJSON, &doc); err != nil {
		return realityDest{}, false
	}
	for _, in := range doc.Inbounds {
		r := in.TLS.Reality
		if r.Enabled && r.Handshake.Server != "" {
			p := r.Handshake.ServerPort
			if p == 0 {
				p = 443
			}
			return realityDest{host: r.Handshake.Server, port: p}, true
		}
	}
	return realityDest{}, false
}

// dialReality opens a TLS 1.3 handshake to a REALITY dest; nil means reachable.
func dialReality(dest realityDest) error {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 8 * time.Second}, "tcp", net.JoinHostPort(dest.host, strconv.Itoa(dest.port)),
		&tls.Config{ServerName: dest.host, MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}, //nolint:gosec
	)
	if err != nil {
		return err
	}
	// A completed TLS 1.3 handshake means the dest is reachable; a Close() error
	// must not flip that to "unreachable" (preserves the pre-refactor semantics).
	_ = conn.Close()
	return nil
}

func newCheckDestCmd() *cobra.Command {
	var cfgPath string
	var warn bool
	cmd := &cobra.Command{
		Use:   "check-dest",
		Short: "Verify this node can reach the REALITY handshake dest (TLS 1.3) in its sing-box config",
		Long: "Reads the rendered sing-box config and, if it has a REALITY inbound, opens a TLS 1.3\n" +
			"handshake to the borrowed dest. On the RU node this is the SNI the inspected\n" +
			"client→RU hop carries AND the site the node relays the handshake to — if it is\n" +
			"blocked or unreachable from here, no client can connect, so this exits non-zero\n" +
			"(it runs as an ExecStartPre of rdda-singbox, so a bad dest fails the install).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := os.ReadFile(cfgPath)
			if err != nil {
				// Nothing rendered yet / unreadable: don't block — sing-box validates its own config.
				fmt.Fprintf(cmd.OutOrStdout(), "check-dest: no config at %s, skipping\n", cfgPath)
				return nil
			}
			dest, ok := extractRealityDest(b)
			if !ok {
				fmt.Fprintln(cmd.OutOrStdout(), "check-dest: no REALITY inbound to verify (skipping)")
				return nil
			}
			if err := dialReality(dest); err != nil {
				failErr := fmt.Errorf("REALITY dest %s is not reachable via TLS 1.3 from this node: %w\n"+
					"  the inspected hop sends SNI %q and this node relays the handshake there;\n"+
					"  choose a --client-sni reachable AND unblocked from here, then re-render the config",
					net.JoinHostPort(dest.host, strconv.Itoa(dest.port)), err, dest.host)
				if warn {
					fmt.Fprintf(cmd.ErrOrStderr(), "check-dest WARNING: %v\n", failErr)
					return nil
				}
				return failErr
			}
			fmt.Fprintf(cmd.OutOrStdout(), "check-dest: REALITY dest %s reachable (TLS 1.3) OK\n",
				net.JoinHostPort(dest.host, strconv.Itoa(dest.port)))
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "/etc/rdda/singbox.json", "sing-box config to read the REALITY dest from")
	cmd.Flags().BoolVar(&warn, "warn", false, "soft mode: log a warning and exit 0 instead of failing when the dest is unreachable")
	return cmd
}
