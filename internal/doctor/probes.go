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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/keys"
	"golang.org/x/net/proxy"
)

// realDialDest opens a TLS 1.3 handshake (REALITY-dest reachability).
func realDialDest(host string, port int) error {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 8 * time.Second}, "tcp", net.JoinHostPort(host, strconv.Itoa(port)),
		&tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}, //nolint:gosec
	)
	if err != nil {
		return err
	}
	return conn.Close()
}

// realHTTPProbe GETs probeURL and returns the status code + the served leaf
// cert's NotAfter (zero for plaintext).
func realHTTPProbe(probeURL string) (int, time.Time, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Get(probeURL)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer resp.Body.Close()
	var notAfter time.Time
	if resp.TLS != nil {
		for _, c := range resp.TLS.PeerCertificates {
			if !c.IsCA {
				notAfter = leafNotAfter(c)
				break
			}
		}
	}
	return resp.StatusCode, notAfter, nil
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

// realEgress spins a throwaway sing-box from the RU config and fetches probeURL
// through its SOCKS proxy. ok=false,err=nil means the fetch failed (tunnel down);
// err!=nil means the probe was inconclusive (caller WARNs).
func realEgress(singboxJSON []byte, probeURL string) (bool, error) {
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		return false, fmt.Errorf("sing-box not on PATH")
	}
	const socksPort = 12080
	cfg, _, err := buildClientConfig(singboxJSON, socksPort)
	if err != nil {
		return false, err
	}
	tmp, err := os.MkdirTemp("", "rdda-doctor-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	cfgPath := filepath.Join(tmp, "client.json")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	proc := exec.CommandContext(ctx, bin, "run", "-c", cfgPath)
	if err := proc.Start(); err != nil {
		return false, err
	}
	defer func() { _ = proc.Process.Kill(); _ = proc.Wait() }()

	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, proxy.Direct)
	if err != nil {
		return false, err
	}
	tr := &http.Transport{Dial: dialer.Dial} //nolint:staticcheck
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}

	// Give sing-box a moment to bind, retrying the fetch a few times.
	var lastErr error
	for i := 0; i < 12; i++ {
		time.Sleep(time.Second)
		resp, err := client.Get(probeURL)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 500 {
			return true, nil // reached the destination through the tunnel
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
	}
	_ = lastErr
	return false, nil // sing-box ran but nothing got through ⇒ provable FAIL
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
