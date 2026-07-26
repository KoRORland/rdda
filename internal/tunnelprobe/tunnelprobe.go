// Package tunnelprobe runs an end-to-end reachability probe THROUGH the RU
// node's own tunnel: it spins a throwaway sing-box client from the node's
// singbox.json and fetches an external URL over that client's SOCKS proxy. A
// success proves the exit path actually reaches the open internet — distinct
// from "the local units are running." It is the shared sensor behind both
// `rdda doctor`'s end-to-end egress check and the RU health beat's reachability
// field (kept in its own package so neither doctor nor health depends on the
// other).
package tunnelprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/KoRORland/rdda/internal/keys"
	"golang.org/x/net/proxy"
)

// Status is the typed outcome of a through-tunnel reachability probe.
type Status string

const (
	// OK: an external URL was fetched through the tunnel — the exit path works.
	OK Status = "ok"
	// Blocked: sing-box ran but nothing got through — the exit path is down or
	// blocked (a provable failure, not an inconclusive one).
	Blocked Status = "blocked"
	// Unknown: the probe could not run conclusively (no sing-box on PATH, an
	// unusable config, or a local resource failure) — not evidence of a block.
	Unknown Status = "unknown"
)

// Result is a typed reachability reading. It is deliberately richer than a bool
// so a later automated responder (transport shift / rotation) and pool
// health-weighting can consume the same sensor without a rewrite.
type Result struct {
	Status  Status
	Latency time.Duration // time to the successful fetch (OK only)
	Detail  string        // failure reason (non-OK) or measured latency note
}

// Probe spins a throwaway sing-box client from singboxJSON and fetches probeURL
// through its SOCKS proxy, returning a typed reachability reading.
func Probe(singboxJSON []byte, probeURL string) Result {
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		return Result{Status: Unknown, Detail: "sing-box not on PATH"}
	}
	// Use an OS-assigned free port, not a fixed one: a bind collision would make
	// every fetch fail and falsely report the tunnel blocked. A failure to
	// allocate is inconclusive (Unknown), not a Blocked.
	socksPort, err := freePort()
	if err != nil {
		return Result{Status: Unknown, Detail: "could not allocate a probe port: " + err.Error()}
	}
	cfg, _, err := buildClientConfig(singboxJSON, socksPort)
	if err != nil {
		return Result{Status: Unknown, Detail: err.Error()}
	}
	tmp, err := os.MkdirTemp("", "rdda-tunnelprobe-*")
	if err != nil {
		return Result{Status: Unknown, Detail: err.Error()}
	}
	defer os.RemoveAll(tmp)
	cfgPath := filepath.Join(tmp, "client.json")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return Result{Status: Unknown, Detail: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	proc := exec.CommandContext(ctx, bin, "run", "-c", cfgPath)
	if err := proc.Start(); err != nil {
		return Result{Status: Unknown, Detail: "start sing-box: " + err.Error()}
	}
	defer func() { _ = proc.Process.Kill(); _ = proc.Wait() }()

	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, proxy.Direct)
	if err != nil {
		return Result{Status: Unknown, Detail: "socks dialer: " + err.Error()}
	}
	tr := &http.Transport{Dial: dialer.Dial} //nolint:staticcheck
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}

	// Give sing-box a moment to bind, retrying the fetch a few times.
	var lastErr error
	for i := 0; i < 12; i++ {
		time.Sleep(time.Second)
		start := time.Now()
		resp, err := client.Get(probeURL)
		if err != nil {
			lastErr = err
			continue
		}
		latency := time.Since(start)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 500 {
			return Result{Status: OK, Latency: latency, Detail: latency.Round(time.Millisecond).String()}
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
	}
	// sing-box ran but nothing got through ⇒ provable block.
	detail := "no response through the tunnel"
	if lastErr != nil {
		detail = lastErr.Error()
	}
	return Result{Status: Blocked, Detail: detail}
}

// freePort asks the OS for an unused loopback TCP port for the throwaway SOCKS
// proxy, so two probes (or anything on a fixed port) can't collide.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// buildClientConfig rebuilds a throwaway client config from the RU singbox.json:
// a SOCKS inbound on socksPort and a VLESS/REALITY outbound to the local RU
// inbound (127.0.0.1), with the public key derived from the inbound private key.
func buildClientConfig(singboxJSON []byte, socksPort int) (cfg []byte, uuid string, err error) {
	var doc struct {
		Inbounds []struct {
			ListenPort int `json:"listen_port"`
			Users      []struct {
				UUID string `json:"uuid"`
			} `json:"users"`
			TLS struct {
				ServerName string `json:"server_name"`
				Reality    struct {
					Enabled    bool     `json:"enabled"`
					PrivateKey string   `json:"private_key"`
					ShortID    []string `json:"short_id"`
				} `json:"reality"`
			} `json:"tls"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(singboxJSON, &doc); err != nil {
		return nil, "", err
	}
	if len(doc.Inbounds) == 0 || !doc.Inbounds[0].TLS.Reality.Enabled {
		return nil, "", fmt.Errorf("no REALITY inbound in singbox.json")
	}
	in := doc.Inbounds[0]
	if len(in.Users) == 0 || in.Users[0].UUID == "" {
		return nil, "", fmt.Errorf("no client uuid in singbox.json")
	}
	pub, err := keys.PublicFromPrivate(in.TLS.Reality.PrivateKey)
	if err != nil {
		return nil, "", fmt.Errorf("derive public key: %w", err)
	}
	sid := ""
	if len(in.TLS.Reality.ShortID) > 0 {
		sid = in.TLS.Reality.ShortID[0]
	}
	client := map[string]any{
		"log":      map[string]any{"level": "error"},
		"inbounds": []map[string]any{{"type": "socks", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []map[string]any{{
			"type": "vless", "tag": "proxy", "server": "127.0.0.1", "server_port": in.ListenPort,
			"uuid": in.Users[0].UUID,
			"tls": map[string]any{
				"enabled": true, "server_name": in.TLS.ServerName,
				"utls":    map[string]any{"enabled": true, "fingerprint": "firefox"},
				"reality": map[string]any{"enabled": true, "public_key": pub, "short_id": sid},
			},
			"multiplex": map[string]any{"enabled": true, "protocol": "h2mux", "max_streams": 8},
		}, {"type": "direct", "tag": "direct"}},
	}
	b, err := json.Marshal(client)
	return b, in.Users[0].UUID, err
}
