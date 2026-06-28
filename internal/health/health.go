// Package health gathers a small RU-node health report and sends it to the EU
// controller over the existing sub channel. The report rides a deliberately
// randomized beat (random interval + random-length pad) so it does not become a
// DPI beacon.
package health

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Report is one RU health beat.
type Report struct {
	TS             time.Time `json:"ts"`
	SingboxActive  bool      `json:"singbox_active"`
	NfqwsActive    bool      `json:"nfqws_active"`
	SingboxVersion string    `json:"singbox_version"`
	RDDAVersion    string    `json:"rdda_version"`
	Pad            string    `json:"pad"`
}

// Runner runs a command and returns its trimmed combined output. Tests inject a fake.
type Runner func(name string, args ...string) (string, error)

// DefaultRunner executes the command for real.
func DefaultRunner(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// UnitActive reports whether `systemctl is-active <unit>` prints "active".
func UnitActive(run Runner, unit string) bool {
	out, _ := run("systemctl", "is-active", unit)
	return out == "active"
}

// randomPad returns base64 of a random number (16..1024) of random bytes, so no
// two beats share a size.
func randomPad() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1009)) // 0..1008
	b := make([]byte, n.Int64()+16)                 // 16..1024
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// Gather builds a Report from local node state.
func Gather(run Runner, rddaVersion string) Report {
	sbVer := ""
	if out, err := run("sing-box", "version"); err == nil {
		if f := strings.Fields(out); len(f) >= 3 { // "sing-box version 1.13.14"
			sbVer = f[2]
		}
	}
	return Report{
		TS:             time.Now().UTC(),
		SingboxActive:  UnitActive(run, "rdda-singbox"),
		NfqwsActive:    UnitActive(run, "rdda-nfqws"),
		SingboxVersion: sbVer,
		RDDAVersion:    rddaVersion,
		Pad:            randomPad(),
	}
}

// Send POSTs the report to endpoint?token=token (the EU /ru/health URL).
func Send(client *http.Client, endpoint, token string, r Report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health beat rejected: %s", resp.Status)
	}
	return nil
}
