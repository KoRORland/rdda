package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func TestAlertTestSends(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(state.Config{RUHost: "ru", Alert: state.Alert{Enabled: true, Email: "ops@x"}}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "fake-msmtp")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := run(t, "--dir", dir, "alert", "--test", "--command", script)
	if !strings.Contains(out, "test alert sent") {
		t.Fatalf("expected confirmation, got: %s", out)
	}
}

func TestAlertTestNoEmailErrors(t *testing.T) {
	dir := t.TempDir()
	s, _ := state.Open(dir)
	_ = s.SaveConfig(state.Config{RUHost: "ru", Alert: state.Alert{Enabled: true}}) // no email
	root := newRoot()
	root.SetArgs([]string{"--dir", dir, "alert", "--test"})
	if err := root.Execute(); err == nil {
		t.Fatal("alert --test with no email must error")
	}
}
