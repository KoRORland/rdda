package subscription

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/singboxconf"
	"github.com/KoRORland/rdda/internal/state"
)

func cfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 8443,
		ClientReality: state.Reality{ServerName: "www.microsoft.com", PublicKey: "PUB", ShortIDs: []string{"ab12"}},
		Fingerprint:   "firefox",
	}
}

func TestClientOutbound(t *testing.T) {
	b, err := ClientOutbound(cfg(), state.Client{UUID: "uuid-9", Name: "granny"})
	if err != nil {
		t.Fatal(err)
	}
	var o map[string]any
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	if o["type"] != "vless" || o["uuid"] != "uuid-9" || o["server"] != "ru.example.net" {
		t.Fatalf("outbound: %v", o)
	}
	if o["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatal("subscription outbound must carry multiplex (the whole point of sing-box JSON)")
	}
	if o["tls"].(map[string]any)["reality"].(map[string]any)["public_key"] != "PUB" {
		t.Fatalf("reality: %v", o["tls"])
	}
}

// A client's own fingerprint must drive the client→RU uTLS, overriding the node
// default — that's what makes the fleet fingerprint-diverse.
func TestClientOutbound_UsesClientFingerprint(t *testing.T) {
	b, err := ClientOutbound(cfg(), state.Client{UUID: "u", Name: "granny", Fingerprint: "safari"})
	if err != nil {
		t.Fatal(err)
	}
	var o map[string]any
	_ = json.Unmarshal(b, &o)
	got := o["tls"].(map[string]any)["utls"].(map[string]any)["fingerprint"]
	if got != "safari" {
		t.Fatalf("expected client fingerprint safari, got %v (node default was firefox)", got)
	}
}

func TestBuildIsFullConfig(t *testing.T) {
	s, err := Build(cfg(), state.Client{UUID: "uuid-9", Name: "granny"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "\"outbounds\"") || !strings.Contains(s, "uuid-9") {
		t.Fatalf("Build must be a full sing-box config: %s", s)
	}
}

// Build must ship the hardened posture — DNS, QUIC/UDP-443 kill, and a route
// final pointing back at the rdda outbound — so the user never opens settings.
func TestBuildIsHardened(t *testing.T) {
	s, err := Build(cfg(), state.Client{UUID: "u", Name: "granny"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("Build output is not valid JSON: %v", err)
	}
	if _, ok := doc["dns"]; !ok {
		t.Error("hardened config must carry a dns block")
	}
	route, ok := doc["route"].(map[string]any)
	if !ok {
		t.Fatal("config missing route block")
	}
	if route["final"] != "rdda" {
		t.Errorf("route.final = %v, want rdda (the outbound tag)", route["final"])
	}
	// A block outbound must exist for the QUIC/bittorrent drop rules to target.
	obs, _ := doc["outbounds"].([]any)
	var hasBlock, hasRdda bool
	for _, o := range obs {
		m, _ := o.(map[string]any)
		if m["type"] == "block" {
			hasBlock = true
		}
		if m["tag"] == "rdda" {
			hasRdda = true
		}
	}
	if !hasBlock {
		t.Error("hardened config must include a block outbound")
	}
	if !hasRdda {
		t.Error("outbound tag must stay \"rdda\" — route.final and the dns detour reference it")
	}
	if !strings.Contains(s, "quic") {
		t.Error("hardened config must drop QUIC")
	}
}

func TestSubURLAndDeepLink(t *testing.T) {
	c := cfg()
	c.SubBaseURL = "https://eu.example.net/"
	cl := state.Client{UUID: "u", Name: "granny", Token: "AbC123"}
	if got, want := SubURL(c, cl), "https://eu.example.net/sub/AbC123"; got != want {
		t.Errorf("SubURL = %q, want %q (trailing slash must be trimmed)", got, want)
	}
	if got, want := DeepLink(c, cl), "hiddify://import/https://eu.example.net/sub/AbC123#RDDA"; got != want {
		t.Errorf("DeepLink = %q, want %q", got, want)
	}
}

func TestProfileTitleHeaderIsBase64(t *testing.T) {
	// base64("RDDA") == "UkREQQ==" — the exact value a client will match on.
	if got := ProfileTitleHeader(); got != "base64:UkREQQ==" {
		t.Errorf("ProfileTitleHeader = %q, want base64:UkREQQ==", got)
	}
}

// TestSubscriptionMatchesRUInbound is a cross-component regression test ensuring
// that the subscription outbound parameters match what RenderRU puts in the RU inbound.
// Specifically: the subscription's REALITY short_id must appear in RenderRU's inbound
// short_id list, and the SNI must match. Guards the v0.1 "shortId-is-shared" bug.
func TestSubscriptionMatchesRUInbound(t *testing.T) {
	c := state.Config{
		RUHost: "ru.example.net", RUPort: 443, ClientPath: "/cl",
		EUHost: "eu.example.net", EUPort: 443,
		TunnelPath: "/tun",
		ClientReality: state.Reality{
			ServerName: "www.microsoft.com",
			PublicKey:  "cpub",
			PrivateKey: "cpriv",
			Target:     "www.microsoft.com:443",
			ShortIDs:   []string{"aabb1122"},
		},
		TunnelReality: state.Reality{
			ServerName: "www.google.com",
			PublicKey:  "tpub",
			PrivateKey: "tpriv",
			Target:     "www.google.com:443",
			ShortIDs:   []string{"ccdd3344"},
		},
		TunnelUUID: "tunnel-uuid",
	}
	client := state.Client{Name: "alice", UUID: "client-uuid-1", ShortID: "deadbeef"}

	// Render RU config and unmarshal as generic map.
	raw, err := singboxconf.RenderRU(c, []state.Client{client})
	if err != nil {
		t.Fatalf("RenderRU: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal RU config: %v", err)
	}

	// Navigate to the first inbound's tls.reality (sing-box structure).
	inboundsAny, ok := doc["inbounds"]
	if !ok {
		t.Fatal("RU config missing 'inbounds'")
	}
	inbounds, ok := inboundsAny.([]any)
	if !ok || len(inbounds) == 0 {
		t.Fatal("RU config 'inbounds' is not a non-empty []any")
	}
	inbound, ok := inbounds[0].(map[string]any)
	if !ok {
		t.Fatal("inbounds[0] is not map[string]any")
	}
	tls, ok := inbound["tls"].(map[string]any)
	if !ok {
		t.Fatal("inbound missing 'tls'")
	}
	reality, ok := tls["reality"].(map[string]any)
	if !ok {
		t.Fatal("tls missing 'reality'")
	}
	ruSNI, ok := tls["server_name"].(string)
	if !ok {
		t.Fatal("tls.server_name is not a string")
	}
	shortIdsAny, ok := reality["short_id"]
	if !ok {
		t.Fatal("reality missing 'short_id'")
	}
	shortIds, ok := shortIdsAny.([]any)
	if !ok {
		t.Fatal("reality.short_id is not []any")
	}

	// Get subscription outbound and extract short_id + server_name from it.
	ob, err := ClientOutbound(c, client)
	if err != nil {
		t.Fatalf("ClientOutbound: %v", err)
	}
	var outbound map[string]any
	if err := json.Unmarshal(ob, &outbound); err != nil {
		t.Fatalf("unmarshal outbound: %v", err)
	}
	obTLS, ok := outbound["tls"].(map[string]any)
	if !ok {
		t.Fatal("outbound missing 'tls'")
	}
	obReality, ok := obTLS["reality"].(map[string]any)
	if !ok {
		t.Fatal("outbound tls missing 'reality'")
	}
	obSID, ok := obReality["short_id"].(string)
	if !ok {
		t.Fatal("outbound tls.reality.short_id is not a string")
	}
	obSNI, ok := obTLS["server_name"].(string)
	if !ok {
		t.Fatal("outbound tls.server_name is not a string")
	}

	// Assert short_id is in the RU inbound's short_id list.
	sidFound := false
	for _, v := range shortIds {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s == obSID {
			sidFound = true
			break
		}
	}
	if !sidFound {
		t.Errorf("outbound short_id=%q not found in RU inbound short_id %v", obSID, shortIds)
	}

	// Assert SNI matches the RU inbound's server_name.
	if obSNI != ruSNI {
		t.Errorf("outbound tls.server_name=%q does not match RU inbound tls.server_name=%q", obSNI, ruSNI)
	}
}
