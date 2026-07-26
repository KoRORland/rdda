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

	"github.com/KoRORland/rdda/internal/tunnelprobe"
)

// Reachability status values carried in a beat (mirroring tunnelprobe).
const (
	ReachOK      = string(tunnelprobe.OK)      // reached the open internet through the tunnel
	ReachBlocked = string(tunnelprobe.Blocked) // units up but the exit path is down/blocked
	ReachUnknown = string(tunnelprobe.Unknown) // probe inconclusive (not evidence of a block)
)

// Reach is the end-to-end exit-path reachability carried in the beat: whether
// the RU node actually reaches the open internet THROUGH its own tunnel, as
// distinct from whether the local units are merely running (liveness ≠
// reachability). It is a typed reading, not a bool, so a later automated
// responder (transport shift / rotation) and pool health-weighting can consume
// the same sensor without a rewrite.
type Reach struct {
	Status    string `json:"status"`               // ReachOK | ReachBlocked | ReachUnknown
	LatencyMS int64  `json:"latency_ms,omitempty"` // round-trip to the successful fetch (OK)
	Detail    string `json:"detail,omitempty"`     // failure reason (non-OK) or latency note
}

// Report is one RU health beat.
type Report struct {
	TS             time.Time `json:"ts"`
	SingboxActive  bool      `json:"singbox_active"`
	NfqwsActive    bool      `json:"nfqws_active"`
	SingboxVersion string    `json:"singbox_version"`
	RDDAVersion    string    `json:"rdda_version"`
	Reach          Reach     `json:"reach"`
	Pad            string    `json:"pad"`
}

// Reachability runs the shared through-tunnel probe from the RU node and maps it
// to the beat's typed Reach field. singboxJSON is the node's rendered config;
// probeURL is fetched through the tunnel to prove the exit path works.
func Reachability(singboxJSON []byte, probeURL string) Reach {
	return reachFromResult(tunnelprobe.Probe(singboxJSON, probeURL))
}

func reachFromResult(r tunnelprobe.Result) Reach {
	out := Reach{Status: string(r.Status), Detail: r.Detail}
	if r.Latency > 0 {
		out.LatencyMS = r.Latency.Milliseconds()
	}
	return out
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

// Send POSTs the report to the EU /ru/health URL, authenticating with an
// Authorization: Bearer header. It also sets the legacy ?token= query as a
// one-release bridge for an EU node not yet reading the header (F-2).
// TODO(next release): drop the query param.
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
	req.Header.Set("Authorization", "Bearer "+token)
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
