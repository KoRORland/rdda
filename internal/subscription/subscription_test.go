package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/singboxconf"
)

func cfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 443, ClientPath: "/cl",
		ClientReality: state.Reality{ServerName: "www.microsoft.com", PublicKey: "cpub", ShortIDs: []string{"0011"}},
	}
}

func TestClientURI(t *testing.T) {
	c := state.Client{Name: "granny", UUID: "uuid-1", ShortID: "abcd1234"}
	uri := ClientURI(cfg(), c)
	if !strings.HasPrefix(uri, "vless://uuid-1@ru.example.net:443?") {
		t.Fatalf("bad prefix: %s", uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	checks := map[string]string{
		"type": "ws", "security": "reality", "encryption": "none",
		"pbk": "cpub", "sni": "www.microsoft.com", "sid": "0011", "fp": "firefox", "path": "/cl",
		"host": "ru.example.net",
	}
	for k, want := range checks {
		if q.Get(k) != want {
			t.Errorf("query %s=%q want %q", k, q.Get(k), want)
		}
	}
	if u.Fragment != "granny" {
		t.Errorf("fragment=%q want granny", u.Fragment)
	}
}

func TestBuildBase64(t *testing.T) {
	c := state.Client{Name: "granny", UUID: "uuid-1", ShortID: "abcd1234"}
	body := Build(cfg(), c)
	dec, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(dec), "\n")
	if len(lines) < 2 {
		t.Fatalf("decoded body has %d lines, want >=2: %q", len(lines), dec)
	}
	if !strings.HasPrefix(lines[0], "# ") {
		t.Errorf("line[0]=%q want a comment line starting with %q", lines[0], "# ")
	}
	if !strings.Contains(lines[0], "Mux") {
		t.Errorf("line[0]=%q want it to mention %q (mux note)", lines[0], "Mux")
	}
	if !strings.HasPrefix(lines[1], "vless://") {
		t.Errorf("line[1]=%q want prefix %q", lines[1], "vless://")
	}
}

// TestSubscriptionMatchesRUInbound is a cross-component regression test ensuring
// that the subscription URI parameters match what RenderRU puts in the RU inbound.
// If subscription and RU config drift apart, this test will catch it.
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

	// Parse the subscription URI.
	uri := ClientURI(c, client)
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse URI: %v", err)
	}
	q := u.Query()
	uriSID := q.Get("sid")
	uriSNI := q.Get("sni")

	// Assert sid is in the RU inbound's short_id list.
	sidFound := false
	for _, v := range shortIds {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s == uriSID {
			sidFound = true
			break
		}
	}
	if !sidFound {
		t.Errorf("URI sid=%q not found in RU inbound short_id %v", uriSID, shortIds)
	}

	// Assert sni matches the RU inbound's server_name.
	if uriSNI != ruSNI {
		t.Errorf("URI sni=%q does not match RU inbound tls.server_name=%q", uriSNI, ruSNI)
	}
}
