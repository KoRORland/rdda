package xrayconf

import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func cfConfig() state.Config {
	return state.Config{
		RUHost: "ru.example", RUPort: 443, EUHost: "eu.example", EUPort: 8443,
		ClientPath: "/cpath", TunnelPath: "/tpath", TunnelUUID: "uuid-1",
		ClientReality: state.Reality{Target: "www.microsoft.com:443", ServerName: "www.microsoft.com", PrivateKey: "ckey", ShortIDs: []string{"aa"}},
		TunnelReality: state.Reality{ServerName: "www.apple.com", PublicKey: "tpub", ShortIDs: []string{"bb"}},
		Cloudflare:    state.Cloudflare{TunnelHostname: "tunnel.example.com", SubHostname: "sub.example.com"},
	}
}

func TestRenderRU_CFTunnelOutboundUsesTLSNotReality(t *testing.T) {
	b, err := RenderRU(cfConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	ss := out["streamSettings"].(map[string]any)
	if ss["security"] != "tls" {
		t.Fatalf("tunnel outbound security = %v, want tls", ss["security"])
	}
	if _, hasReality := ss["realitySettings"]; hasReality {
		t.Fatal("tunnel outbound must not carry realitySettings when CF enabled")
	}
	vnext := out["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
	if vnext["address"] != "tunnel.example.com" || vnext["port"].(float64) != 443 {
		t.Fatalf("tunnel outbound must dial CF hostname:443, got %v:%v", vnext["address"], vnext["port"])
	}
	tls := ss["tlsSettings"].(map[string]any)
	if tls["serverName"] != "tunnel.example.com" {
		t.Fatalf("tls serverName = %v, want tunnel.example.com", tls["serverName"])
	}
	xh := ss["xhttpSettings"].(map[string]any)
	if xh["host"] != "tunnel.example.com" || xh["path"] != "/tpath" {
		t.Fatalf("xhttp host/path wrong: %v %v", xh["host"], xh["path"])
	}
}

func TestRenderRU_ClientInboundKeepsREALITY(t *testing.T) {
	b, _ := RenderRU(cfConfig(), nil)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	ss := in["streamSettings"].(map[string]any)
	if ss["security"] != "reality" {
		t.Fatalf("client->RU inbound must stay REALITY, got %v", ss["security"])
	}
}

func TestRenderEU_CFInboundIsLoopbackPlaintext(t *testing.T) {
	b, err := RenderEU(cfConfig())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	if in["listen"] != "127.0.0.1" {
		t.Fatalf("EU inbound must listen on loopback under CF, got %v", in["listen"])
	}
	ss := in["streamSettings"].(map[string]any)
	if ss["security"] != "none" {
		t.Fatalf("EU inbound security = %v, want none (cloudflared terminates TLS)", ss["security"])
	}
	if _, hasReality := ss["realitySettings"]; hasReality {
		t.Fatal("EU inbound must not carry realitySettings when CF enabled")
	}
}

func TestRenderRU_NonCFKeepsREALITYOutbound(t *testing.T) {
	cfg := cfConfig()
	cfg.Cloudflare = state.Cloudflare{} // disabled
	b, _ := RenderRU(cfg, nil)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	out := doc["outbounds"].([]any)[0].(map[string]any)
	ss := out["streamSettings"].(map[string]any)
	if ss["security"] != "reality" {
		t.Fatalf("non-CF tunnel outbound must stay REALITY, got %v", ss["security"])
	}
}
