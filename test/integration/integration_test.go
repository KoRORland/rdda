//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRealDeployTunnel runs the multi-host nspawn integration harness.
// Requires root and systemd-nspawn, debootstrap, nft, and machinectl on the host.
func TestRealDeployTunnel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("multi-host integration test requires root (nspawn/nft/debootstrap)")
	}
	for _, bin := range []string{"systemd-nspawn", "debootstrap", "nft", "machinectl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
	cmd := exec.Command("bash", filepath.Join(".", "multihost", "run-multihost.sh"))
	out, err := cmd.CombinedOutput()
	t.Logf("run-multihost.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("multi-host harness failed: %v", err)
	}
}
