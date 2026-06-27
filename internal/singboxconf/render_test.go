package singboxconf

import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func clientCfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 8443,
		ClientReality: state.Reality{
			Target: "www.microsoft.com:443", ServerName: "www.microsoft.com",
			// Valid Curve25519 public key required by sing-box check (not just schema).
			PublicKey: "V9J9A-cx7pe0AmSaVcYwBg39y6W3wIxv9nciaXO8AmI", ShortIDs: []string{"abcd1234"},
		},
		Fingerprint: "firefox",
	}
}

func TestRenderClient(t *testing.T) {
	b, err := RenderClient(clientCfg(), "uuid-1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	if out["type"] != "vless" || out["uuid"] != "uuid-1" {
		t.Fatalf("outbound: %v", out)
	}
	if _, ok := out["flow"]; ok {
		t.Fatal("client outbound must NOT set flow (no Vision; multiplex chosen)")
	}
	tls := out["tls"].(map[string]any)
	if tls["server_name"] != "www.microsoft.com" {
		t.Fatalf("server_name: %v", tls)
	}
	if tls["reality"].(map[string]any)["public_key"] != "V9J9A-cx7pe0AmSaVcYwBg39y6W3wIxv9nciaXO8AmI" {
		t.Fatalf("reality: %v", tls)
	}
	if tls["utls"].(map[string]any)["fingerprint"] != "firefox" {
		t.Fatalf("utls: %v", tls)
	}
	if out["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("multiplex must be enabled: %v", out)
	}
	mustCheck(t, b)
}

func TestSplitHostPort(t *testing.T) {
	h, p := splitHostPort("www.microsoft.com:8443", 443)
	if h != "www.microsoft.com" || p != 8443 {
		t.Fatalf("got %s:%d", h, p)
	}
	h, p = splitHostPort("example.com", 443)
	if h != "example.com" || p != 443 {
		t.Fatalf("default port not applied: %s:%d", h, p)
	}
}
