package cfconfig

import (
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
	"gopkg.in/yaml.v3"
)

func TestRenderIngress(t *testing.T) {
	cfg := state.Config{
		EUPort: 8443,
		Cloudflare: state.Cloudflare{
			TunnelHostname:  "tunnel.example.com",
			SubHostname:     "sub.example.com",
			TunnelID:        "abc-123",
			CredentialsFile: "/etc/cloudflared/abc-123.json",
		},
	}
	b, err := Render(cfg, 8080)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	if doc["tunnel"] != "abc-123" {
		t.Fatalf("tunnel = %v, want abc-123", doc["tunnel"])
	}
	if doc["credentials-file"] != "/etc/cloudflared/abc-123.json" {
		t.Fatalf("credentials-file wrong: %v", doc["credentials-file"])
	}
	s := string(b)
	if !strings.Contains(s, "hostname: tunnel.example.com") ||
		!strings.Contains(s, "service: http://localhost:8443") {
		t.Fatalf("missing data-hop ingress rule:\n%s", s)
	}
	if !strings.Contains(s, "hostname: sub.example.com") ||
		!strings.Contains(s, "service: http://localhost:8080") {
		t.Fatalf("missing sub ingress rule:\n%s", s)
	}
	if !strings.Contains(s, "service: http_status:404") {
		t.Fatalf("missing catch-all 404 rule:\n%s", s)
	}
}

func TestRenderErrorsWhenDisabled(t *testing.T) {
	if _, err := Render(state.Config{}, 8080); err == nil {
		t.Fatal("Render must error when Cloudflare is not configured")
	}
}
