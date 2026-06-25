//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestRealDeployTunnel runs the single-host CF-fronted harness to prove the
// XHTTP-over-TLS transport through a reverse proxy. This is a temporary
// transport-proof gate; the multi-host nspawn harness (multihost/) replaces it
// once complete (see docs/superpowers/plans/2026-06-25-multihost-integration-harness.md).
func TestRealDeployTunnel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real-deploy integration test requires root (systemd/useradd/chown)")
	}
	for _, bin := range []string{"xray", "rdda", "jq", "systemctl", "nginx", "openssl", "ss", "curl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
	const euPort, ruPort = 18443, 18444
	cmd := exec.Command("bash", filepath.Join(".", "run.sh"), strconv.Itoa(euPort), strconv.Itoa(ruPort))
	out, err := cmd.CombinedOutput()
	t.Logf("run.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("single-host harness failed: %v", err)
	}
}
