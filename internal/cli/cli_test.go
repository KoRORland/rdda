package cli

import (
	"bytes"
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

func TestClientAddPrintsSubURL(t *testing.T) {
	dir := t.TempDir()
	run(t, "--dir", dir, "init", "--ru-host", "ru.example.net", "--eu-host", "eu.example.net")
	out := run(t, "--dir", dir, "client", "add", "granny")
	if !strings.Contains(out, "https://eu.example.net/sub/") {
		t.Fatalf("expected subscription URL, got: %s", out)
	}
	list := run(t, "--dir", dir, "client", "list")
	if !strings.Contains(list, "granny") {
		t.Fatalf("list missing granny: %s", list)
	}
	run(t, "--dir", dir, "client", "rm", "granny")
	list = run(t, "--dir", dir, "client", "list")
	if strings.Contains(list, "granny") {
		t.Fatalf("granny still listed after rm: %s", list)
	}
}
