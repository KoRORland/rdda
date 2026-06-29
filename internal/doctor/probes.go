package doctor

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
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

// Heavy seams — real implementations land in Task 3. Stubs return an error so
// the corresponding checks degrade to WARN (inconclusive) until then.
func realEgress(_ []byte, _ string) (bool, error) {
	return false, errors.New("egress probe not implemented")
}

func realCloudflaredInfo(_ string) (int, error) {
	return 0, errors.New("cloudflared probe not implemented")
}
