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
	for _, want := range []string{"uuid-1", "xhttp", "reality", "cpriv", "tpub", "geoip:ru", "wikipedia.org", "eu.example.net"} {
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
