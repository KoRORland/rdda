package xrayconf

import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func wsCfg() state.Config {
	return state.Config{
		RUHost: "ru.example", RUPort: 443, EUHost: "eu.example", EUPort: 8443,
		ClientPath: "/cl", TunnelPath: "/tn", TunnelUUID: "uuid-1",
		ClientReality: state.Reality{Target: "www.microsoft.com:443", ServerName: "www.microsoft.com", PrivateKey: "k", PublicKey: "pub", ShortIDs: []string{"aa"}},
		TunnelReality: state.Reality{ServerName: "www.apple.com", PublicKey: "tpub", ShortIDs: []string{"bb"}},
		Cloudflare:    state.Cloudflare{TunnelHostname: "tunnel.example.com"},
		Fingerprint:   "firefox",
	}
}

func TestRenderClient_WSWithMuxAndFirefox(t *testing.T) {
	b, err := RenderClient(wsCfg(), "uuid-1", 1080)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	ss := out["streamSettings"].(map[string]any)
	if ss["network"] != "ws" {
		t.Fatalf("network = %v, want ws", ss["network"])
	}
	if _, ok := ss["wsSettings"]; !ok {
		t.Fatal("missing wsSettings")
	}
	if ss["wsSettings"].(map[string]any)["path"] != "/cl" {
		t.Fatalf("ws path wrong: %v", ss["wsSettings"])
	}
	if ss["security"] != "reality" {
		t.Fatalf("client outbound must stay reality, got %v", ss["security"])
	}
	if ss["realitySettings"].(map[string]any)["fingerprint"] != "firefox" {
		t.Fatalf("fingerprint must be firefox, got %v", ss["realitySettings"])
	}
	mux, ok := out["mux"].(map[string]any)
	if !ok || mux["enabled"] != true {
		t.Fatalf("client outbound must enable mux, got %v", out["mux"])
	}
}

func TestRenderRU_TunnelOutboundWSMux(t *testing.T) {
	b, _ := RenderRU(wsCfg(), nil)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	ss := out["streamSettings"].(map[string]any)
	if ss["network"] != "ws" || ss["security"] != "tls" {
		t.Fatalf("tunnel outbound must be ws+tls, got %v/%v", ss["network"], ss["security"])
	}
	hdr := ss["wsSettings"].(map[string]any)["headers"].(map[string]any)
	if hdr["Host"] != "tunnel.example.com" {
		t.Fatalf("ws Host header must be the CF hostname, got %v", hdr["Host"])
	}
	if out["mux"].(map[string]any)["enabled"] != true {
		t.Fatal("tunnel outbound must enable mux")
	}
}

func TestRenderRU_ClientInboundWSReality(t *testing.T) {
	b, _ := RenderRU(wsCfg(), nil)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	ss := in["streamSettings"].(map[string]any)
	if ss["network"] != "ws" || ss["security"] != "reality" {
		t.Fatalf("client inbound must be ws+reality, got %v/%v", ss["network"], ss["security"])
	}
}

func TestRenderEU_CFInboundWSPlaintext(t *testing.T) {
	b, _ := RenderEU(wsCfg())
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	ss := in["streamSettings"].(map[string]any)
	if ss["network"] != "ws" || ss["security"] != "none" || in["listen"] != "127.0.0.1" {
		t.Fatalf("EU CF inbound must be loopback ws+none, got %v/%v/%v", in["listen"], ss["network"], ss["security"])
	}
}
