package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

// initEUWithCF writes an EU config that already carries the CF hostnames.
func initEUWithCF(t *testing.T, dir string) {
	t.Helper()
	run(t, "--dir", dir, "init",
		"--ru-host", "ru.example.net", "--eu-host", "eu.example.net",
		"--cf-tunnel-host", "cdn.example.com", "--cf-sub-host", "sub.example.com")
}

// fakeCFEnv returns a cfEnv whose cloudflared/system calls are faked. routeOut
// maps a hostname to the route-dns output the fake returns.
func fakeCFEnv(t *testing.T, dir string, out *bytes.Buffer, routeOut map[string]string) (cfEnv, *[]string) {
	t.Helper()
	credSrc := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(credSrc, []byte(`{"AccountTag":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	env := cfEnv{
		dir: dir, out: out,
		hasCloudflared: func() bool { return true },
		exists:         func(string) bool { return true }, // cert.pem present ⇒ skip login
		writeFile:      func(string, []byte, os.FileMode) error { return nil },
		mkdirAll:       func(string, os.FileMode) error { return nil },
		runAttached:    func(string, ...string) error { return nil },
		run: func(name string, args ...string) (string, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch {
			case name == "cloudflared" && len(args) >= 2 && args[0] == "tunnel" && args[1] == "create":
				return fmt.Sprintf("Tunnel credentials written to %s.\n\nCreated tunnel rdda with id 6ff42ae2-765d-4adf-8112-31c55c1551ef", credSrc), nil
			case name == "cloudflared" && len(args) >= 3 && args[1] == "route" && args[2] == "dns":
				host := args[len(args)-1]
				return routeOut[host], nil
			default:
				return "", nil
			}
		},
	}
	return env, &calls
}

func TestCFSetup_HappyPath(t *testing.T) {
	dir := t.TempDir()
	initEUWithCF(t, dir)
	var out bytes.Buffer
	route := map[string]string{
		"cdn.example.com": "INF Added CNAME cdn.example.com which will route to this tunnel tunnelID=6ff4",
		"sub.example.com": "INF Added CNAME sub.example.com which will route to this tunnel tunnelID=6ff4",
	}
	env, calls := fakeCFEnv(t, dir, &out, route)
	if err := runCFSetup(env, cfOptions{TunnelName: "rdda"}); err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out.String())
	}
	// The CF block must be persisted with the parsed tunnel id.
	s, _ := state.Open(dir)
	cfg, _ := s.LoadConfig()
	if cfg.Cloudflare.TunnelID != "6ff42ae2-765d-4adf-8112-31c55c1551ef" {
		t.Errorf("tunnel id not saved: %+v", cfg.Cloudflare)
	}
	if cfg.Cloudflare.CredentialsFile != "/etc/cloudflared/6ff42ae2-765d-4adf-8112-31c55c1551ef.json" {
		t.Errorf("creds path not saved: %q", cfg.Cloudflare.CredentialsFile)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "systemctl enable --now cloudflared") {
		t.Errorf("service not enabled; calls:\n%s", joined)
	}
}

// A pre-existing DNS record (the silent-no-op trap) must abort the setup with
// the tunnel service left disabled.
func TestCFSetup_DNSConflictAborts(t *testing.T) {
	dir := t.TempDir()
	initEUWithCF(t, dir)
	var out bytes.Buffer
	route := map[string]string{
		"cdn.example.com": "INF Added CNAME cdn.example.com which will route to this tunnel tunnelID=6ff4",
		"sub.example.com": "Failed to add route: An A, AAAA, or CNAME record with that host already exists.",
	}
	env, calls := fakeCFEnv(t, dir, &out, route)
	err := runCFSetup(env, cfOptions{TunnelName: "rdda"})
	if err == nil {
		t.Fatal("expected a conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "sub.example.com") {
		t.Errorf("error should name the conflicting host: %v", err)
	}
	if strings.Contains(strings.Join(*calls, "\n"), "enable --now cloudflared") {
		t.Error("service must NOT be enabled when a hostname is unrouted")
	}
}

func TestCFSetup_DryRunNoMutation(t *testing.T) {
	dir := t.TempDir()
	initEUWithCF(t, dir)
	var out bytes.Buffer
	env, calls := fakeCFEnv(t, dir, &out, nil)
	env.dryRun = true
	env.hasCloudflared = func() bool { return false } // dry-run must not require cloudflared
	if err := runCFSetup(env, cfOptions{TunnelName: "rdda"}); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("dry-run must not run commands, got %v", *calls)
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("dry-run should print a plan:\n%s", out.String())
	}
}
