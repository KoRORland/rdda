package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// init writes a pull.env deriving both endpoint URLs from the sub host + token.
func TestControlChannelInitWritesPullEnv(t *testing.T) {
	dir := t.TempDir()
	out := run(t, "--dir", dir, "control-channel", "init",
		"--sub-host", "sub.example.com", "--token", "tok123")
	if !strings.Contains(out, "wrote "+filepath.Join(dir, "pull.env")) {
		t.Fatalf("unexpected output: %q", out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "pull.env"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"RDDA_PULL_FROM=https://sub.example.com/ru/config",
		"RDDA_HEALTH_TO=https://sub.example.com/ru/health",
		"RDDA_PULL_TOKEN=tok123",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pull.env missing %q\n---\n%s", want, got)
		}
	}
	fi, err := os.Stat(filepath.Join(dir, "pull.env"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("pull.env perms = %o, want 600 (holds the pull token)", perm)
	}
}

func TestControlChannelInitRequiresFlags(t *testing.T) {
	dir := t.TempDir()
	if err := runErr(t, "--dir", dir, "control-channel", "init", "--sub-host", "sub.example.com"); err == nil {
		t.Error("missing --token must error")
	}
}

// show (EU side) emits a ready-to-paste init command built from config.yaml.
func TestControlChannelShowFromConfig(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init",
		"--ru-host", "ru.example.net", "--eu-host", "eu.example.net",
		"--cf-tunnel-host", "cdn.example.com", "--cf-sub-host", "sub.example.com")
	out := run(t, "--dir", dir, "control-channel", "show")
	if !strings.Contains(out, "rdda control-channel init --sub-host sub.example.com --token ") {
		t.Fatalf("show did not emit an init command line:\n%s", out)
	}
	if !strings.Contains(out, "RDDA_PULL_FROM=https://sub.example.com/ru/config") {
		t.Fatalf("show did not include the env block:\n%s", out)
	}
}
