package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/dest"
	"github.com/KoRORland/rdda/internal/tunnelprobe"
)

// realDialDest opens a TLS 1.3 handshake (REALITY-dest reachability).
//
// InsecureSkipVerify is deliberate and safe ONLY here: this is a reachability
// probe of a REALITY handshake destination, where certificate identity is not
// the question and no user traffic flows over this connection. Do NOT copy this
// tls.Config onto any path that carries real data — there it would disable the
// authentication that keeps the tunnel secure.
func realDialDest(host string, port int) error {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 8 * time.Second}, "tcp", net.JoinHostPort(host, strconv.Itoa(port)),
		&tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}, //nolint:gosec // probe-only; never on a data path
	)
	if err != nil {
		return err
	}
	return conn.Close()
}

// realValidateDest re-validates a REALITY dest as a genuine borrowed identity
// (TLS 1.3 + X25519 + ALPN h2 + a valid cert + a real page), reusing the
// discovery prober — a reachable dest that no longer validates is a weak,
// more-detectable camouflage even though the tunnel may still function. Only the
// standard :443 dest is validated; a non-standard port reports ok (reachability
// was already checked separately).
func realValidateDest(host string, port int) (ok bool, reason string) {
	if port != 443 {
		return true, ""
	}
	r := dest.DefaultProber{}.Probe(context.Background(), host)
	return r.OK, r.Reason
}

// legacyShared reports whether host is a known shared default dest from before
// per-node discovery. Every deployment that took the default shipped it, so it
// is a fleet-wide signature — worth flagging even when it still validates.
func legacyShared(host string) bool {
	return host == "addons.mozilla.org"
}

// realHTTPProbe GETs probeURL and returns the status code, the (capped) response
// body, and the served leaf cert's NotAfter (zero for plaintext). The body lets
// callers confirm they reached the intended origin, not just that *something*
// answered 200.
func realHTTPProbe(probeURL, bearer string) (int, []byte, time.Time, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest(http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, nil, time.Time{}, err
	}
	// Authenticate with the Authorization: Bearer header (F-2), matching the RU
	// pull/health clients. The URL may also carry ?token= as the migration bridge.
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, time.Time{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var notAfter time.Time
	if resp.TLS != nil {
		for _, c := range resp.TLS.PeerCertificates {
			if !c.IsCA {
				notAfter = leafNotAfter(c)
				break
			}
		}
	}
	return resp.StatusCode, body, notAfter, nil
}

func leafNotAfter(c *x509.Certificate) time.Time { return c.NotAfter }

// realityDestFromConfig extracts the first REALITY inbound's handshake dest.
func realityDestFromConfig(cfgJSON []byte) (host string, port int, ok bool) {
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
		return "", 0, false
	}
	for _, in := range doc.Inbounds {
		r := in.TLS.Reality
		if r.Enabled && r.Handshake.Server != "" {
			p := r.Handshake.ServerPort
			if p == 0 {
				p = 443
			}
			return r.Handshake.Server, p, true
		}
	}
	return "", 0, false
}

// realEgress runs the shared through-tunnel reachability probe and adapts its
// typed result to the doctor egress seam: OK ⇒ (true,nil) PASS; Blocked ⇒
// (false,nil) provable FAIL; Unknown ⇒ (false,err) inconclusive WARN.
func realEgress(singboxJSON []byte, probeURL string) (bool, error) {
	r := tunnelprobe.Probe(singboxJSON, probeURL)
	switch r.Status {
	case tunnelprobe.OK:
		return true, nil
	case tunnelprobe.Blocked:
		return false, nil
	default:
		return false, fmt.Errorf("%s", r.Detail)
	}
}

// realCloudflaredInfo counts live connectors via `cloudflared tunnel info`.
func realCloudflaredInfo(tunnelID string) (int, error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return 0, fmt.Errorf("cloudflared not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "tunnel", "info", tunnelID).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("cloudflared tunnel info: %w", err)
	}
	// Count CONNECTOR lines / connection rows; a connected tunnel lists ≥1.
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "connector") || strings.Contains(l, "connections:") {
			n++
		}
	}
	return n, nil
}
