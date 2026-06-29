package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoctorRunsAndPrints(t *testing.T) {
	dir := t.TempDir() // no config.yaml ⇒ RU mode; no singbox.json ⇒ checks WARN; units may FAIL in CI
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--dir", dir, "doctor"})
	_ = root.Execute() // units FAIL in CI (no systemd); ignore exit code — we only care about the header
	if !strings.Contains(out.String(), "RDDA doctor") {
		t.Fatalf("expected a doctor report, got:\n%s", out.String())
	}
}
