package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func run(t *testing.T, args ...string) string {
	t.Helper()
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("cmd %v failed: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestInitWritesConfig(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")
	s, _ := state.Open(dir)
	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RUHost != "ru.example.net" || cfg.EUHost != "eu.example.net" {
		t.Fatalf("hosts not set: %+v", cfg)
	}
	if cfg.ClientReality.PrivateKey == "" || cfg.TunnelReality.PublicKey == "" || cfg.TunnelUUID == "" {
		t.Fatal("init did not generate keys/uuid")
	}
	if !filepath.IsAbs(dir) { // sanity
		t.Skip()
	}
}

func TestClientAddPrintsSingboxConfig(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")
	out := run(t, "--dir", dir, "client", "add", "granny")
	if !strings.Contains(out, "\"outbounds\"") || !strings.Contains(out, "reality") {
		t.Fatalf("expected a sing-box config, got: %s", out)
	}
}

func TestRenderRUEU(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")
	run(t, "--dir", dir, "client", "add", "granny")

	ru := run(t, "--dir", dir, "render", "ru")
	var doc map[string]any
	if err := json.Unmarshal([]byte(ru), &doc); err != nil {
		t.Fatalf("render ru not JSON: %v", err)
	}
	if !strings.Contains(ru, "geoip-ru") {
		t.Error("RU render missing routing")
	}
	eu := run(t, "--dir", dir, "render", "eu")
	if err := json.Unmarshal([]byte(eu), &doc); err != nil {
		t.Fatalf("render eu not JSON: %v", err)
	}
}

func TestRenderClient(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")

	const uuid = "11111111-2222-3333-4444-555555555555"
	out := run(t, "--dir", dir, "render", "client", "--uuid", uuid, "--socks-port", "19080")

	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("render client not JSON: %v\n%s", err, out)
	}
	inbounds, ok := doc["inbounds"].([]any)
	if !ok || len(inbounds) == 0 {
		t.Fatalf("render client missing inbounds: %s", out)
	}
	in := inbounds[0].(map[string]any)
	if in["type"] != "socks" {
		t.Errorf("expected socks inbound, got %v", in["type"])
	}
	if port, _ := in["listen_port"].(float64); int(port) != 19080 {
		t.Errorf("expected socks inbound on port 19080, got %v", in["listen_port"])
	}
	if !strings.Contains(out, uuid) {
		t.Errorf("render client output should embed the client UUID, got: %s", out)
	}
}

func TestInitSetsFingerprint(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "r", "--eu-host", "e", "--fingerprint", "safari")
	s, _ := state.Open(dir)
	cfg, _ := s.LoadConfig()
	if cfg.Fingerprint != "safari" {
		t.Fatalf("fingerprint = %q, want safari", cfg.Fingerprint)
	}
}

func TestRenderNfqws(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru", "--eu-host", "eu")
	// enable desync in config, then render
	out := run(t, "--dir", dir, "render", "nfqws")
	if !strings.Contains(out, "dpi-desync") {
		t.Fatalf("render nfqws must emit desync flags: %s", out)
	}
}

func TestVersionIsOverridable(t *testing.T) {
	// Version must be a var (ldflags-injectable), defaulting to a non-empty string.
	if Version == "" {
		t.Fatal("Version must default to a non-empty value")
	}
	orig := Version
	t.Cleanup(func() { Version = orig })
	Version = "v9.9.9"
	out := run(t, "version")
	if !strings.Contains(out, "v9.9.9") {
		t.Fatalf("version command should print injected Version, got %q", out)
	}
}
