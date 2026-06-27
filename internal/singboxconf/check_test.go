package singboxconf

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mustCheck writes cfgJSON to a temp file and runs `sing-box check`. If sing-box
// is not installed it skips, so unit CI without sing-box still runs field asserts;
// the integration job (which installs sing-box) enforces full schema validity.
func mustCheck(t *testing.T, cfgJSON []byte) {
	t.Helper()
	bin, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box not installed; skipping schema check")
	}
	f := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(f, cfgJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", "-c", f).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check failed: %v\n%s", err, out)
	}
}
