package xrayconf

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func cfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 443, EUHost: "eu.example.net", EUPort: 443,
		ClientPath: "/cl", TunnelPath: "/tn",
		TunnelUUID:       "tunnel-uuid",
		IntlAllowDomains: []string{"wikipedia.org"},
		ClientReality:    state.Reality{Target: "www.microsoft.com:443", ServerName: "www.microsoft.com", PrivateKey: "cpriv", PublicKey: "cpub", ShortIDs: []string{"0011"}},
		TunnelReality:    state.Reality{Target: "www.apple.com:443", ServerName: "www.apple.com", PrivateKey: "tpriv", PublicKey: "tpub", ShortIDs: []string{"0022"}},
	}
}

func TestRenderRUIsValidJSONWithClientsAndRouting(t *testing.T) {
	clients := []state.Client{{Name: "granny", UUID: "uuid-1", ShortID: "abcd1234"}}
	b, err := RenderRU(cfg(), clients)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	s := string(b)
	for _, want := range []string{"uuid-1", "ws", "reality", "cpriv", "tpub", "geoip:ru", "wikipedia.org", "eu.example.net"} {
		if !strings.Contains(s, want) {
			t.Errorf("RU config missing %q", want)
		}
	}
}

func TestRenderEUAcceptsTunnelAndExits(t *testing.T) {
	b, err := RenderEU(cfg())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	s := string(b)
	for _, want := range []string{"tunnel-uuid", "freedom", "tpriv", "/tn", "reality"} {
		if !strings.Contains(s, want) {
			t.Errorf("EU config missing %q", want)
		}
	}
	if strings.Contains(s, "uuid-1") {
		t.Error("EU config must not contain per-user client UUIDs")
	}
}

func TestRenderClient(t *testing.T) {
	b, err := RenderClient(cfg(), "client-uuid-9", 10808)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	s := string(b)
	// Must contain client UUID, socks inbound, ws+reality outbound with CLIENT reality params.
	for _, want := range []string{"client-uuid-9", "socks", "ws", "reality", "cpub", "www.microsoft.com", "/cl", `"shortId"`, `"serverName"`, `"publicKey"`} {
		if !strings.Contains(s, want) {
			t.Errorf("client config missing %q", want)
		}
	}
	// Must use CLIENT reality params, not tunnel ones.
	if strings.Contains(s, "tpub") {
		t.Error("client config must not contain tunnel public key \"tpub\"")
	}
	// Client-side REALITY uses singular forms (not server-side plural shortIds/serverNames).
	for _, absent := range []string{`"shortIds"`, `"serverNames"`, `"privateKey"`} {
		if strings.Contains(s, absent) {
			t.Errorf("client config must not contain server-side key %q", absent)
		}
	}
}
