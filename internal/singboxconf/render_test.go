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

func ruCfg(cf bool) state.Config {
	c := clientCfg()
	// OVERRIDE 2: clientCfg() does not set ClientReality.PrivateKey; RenderRU uses it
	// for the REALITY inbound. sing-box check validates X25519 key format.
	c.ClientReality.PrivateKey = "4CU9TV7yDehWUFosUk7WsmBIkJ3OoKhCISr8-t9PJ24"
	c.RUPort = 8443
	c.TunnelUUID = "tunnel-uuid"
	c.TunnelPath = "/tnl"
	c.EUHost = "eu.example.net"
	c.EUPort = 9443
	// OVERRIDE 2: replace placeholder "TPUB" with real Curve25519 public key.
	c.TunnelReality = state.Reality{
		Target:     "www.apple.com:443",
		ServerName: "www.apple.com",
		PublicKey:  "-YPePsJFV9QtMmrdg-0WRlzz4UNS6GQZGJkIACRIiwQ",
		PrivateKey: "cIDFmqtsquCJlcsNsSyEodQum-cf2mit658JrnBHIkU",
		ShortIDs:   []string{"ee11"},
	}
	c.IntlAllowDomains = []string{"example.org"}
	if cf {
		c.Cloudflare = state.Cloudflare{TunnelHostname: "tunnel.rdda.test"}
	}
	return c
}

func TestRenderRU_CF(t *testing.T) {
	b, err := RenderRU(ruCfg(true), []state.Client{{UUID: "uuid-1", Name: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	if in["type"] != "vless" || in["tls"].(map[string]any)["reality"].(map[string]any)["enabled"] != true {
		t.Fatalf("RU inbound must be VLESS+REALITY: %v", in)
	}
	if in["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("RU inbound must accept multiplex: %v", in)
	}
	proxy := doc["outbounds"].([]any)[0].(map[string]any)
	tr := proxy["transport"].(map[string]any)
	// OVERRIDE 1: CF transport is ws (not httpupgrade — CF rewrites HTTPUpgrade to WS).
	if tr["type"] != "ws" || tr["path"] != "/tnl" {
		t.Fatalf("CF outbound must be ws: %v", tr)
	}
	if proxy["tls"].(map[string]any)["server_name"] != "tunnel.rdda.test" {
		t.Fatalf("CF outbound TLS SNI: %v", proxy["tls"])
	}
	if _, hasReality := proxy["tls"].(map[string]any)["reality"]; hasReality {
		t.Fatal("CF outbound must NOT carry reality (CF terminates TLS)")
	}
	mustCheck(t, b)
}

func TestRenderRU_NonCF(t *testing.T) {
	b, _ := RenderRU(ruCfg(false), []state.Client{{UUID: "uuid-1"}})
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	proxy := doc["outbounds"].([]any)[0].(map[string]any)
	// OVERRIDE 2: assert against real key, not placeholder "TPUB".
	if proxy["tls"].(map[string]any)["reality"].(map[string]any)["public_key"] != "-YPePsJFV9QtMmrdg-0WRlzz4UNS6GQZGJkIACRIiwQ" {
		t.Fatalf("non-CF outbound must use tunnel REALITY: %v", proxy["tls"])
	}
	mustCheck(t, b)
}

func TestRenderEU_CF(t *testing.T) {
	b, _ := RenderEU(ruCfg(true))
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	tr := in["transport"].(map[string]any)
	// OVERRIDE 1: CF transport is ws (not httpupgrade).
	if tr["type"] != "ws" || tr["path"] != "/tnl" {
		t.Fatalf("EU inbound transport must be ws: %v", tr)
	}
	if _, hasTLS := in["tls"]; hasTLS {
		t.Fatal("EU inbound under CF must NOT enable TLS (Cloudflare terminates it)")
	}
	if in["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("EU inbound must accept multiplex: %v", in)
	}
	mustCheck(t, b)
}

func TestRenderEU_NonCF(t *testing.T) {
	b, _ := RenderEU(ruCfg(false))
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	in := doc["inbounds"].([]any)[0].(map[string]any)
	if in["tls"].(map[string]any)["reality"].(map[string]any)["enabled"] != true {
		t.Fatalf("non-CF EU inbound must be REALITY: %v", in)
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
