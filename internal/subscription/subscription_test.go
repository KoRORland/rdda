package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/xrayconf"
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
		"type": "xhttp", "security": "reality", "encryption": "none",
		"pbk": "cpub", "sni": "www.microsoft.com", "sid": "0011", "fp": "chrome", "path": "/cl",
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
	if !strings.HasPrefix(string(dec), "vless://") {
		t.Fatalf("decoded body not a vless URI: %s", dec)
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
	raw, err := xrayconf.RenderRU(c, []state.Client{client})
	if err != nil {
		t.Fatalf("RenderRU: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal RU config: %v", err)
	}

	// Navigate to the first inbound's streamSettings.realitySettings.
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
	streamAny, ok := inbound["streamSettings"]
	if !ok {
		t.Fatal("inbound missing 'streamSettings'")
	}
	stream, ok := streamAny.(map[string]any)
	if !ok {
		t.Fatal("streamSettings is not map[string]any")
	}
	realityAny, ok := stream["realitySettings"]
	if !ok {
		t.Fatal("streamSettings missing 'realitySettings'")
	}
	reality, ok := realityAny.(map[string]any)
	if !ok {
		t.Fatal("realitySettings is not map[string]any")
	}
	serverNamesAny, ok := reality["serverNames"]
	if !ok {
		t.Fatal("realitySettings missing 'serverNames'")
	}
	serverNames, ok := serverNamesAny.([]any)
	if !ok {
		t.Fatal("serverNames is not []any")
	}
	shortIdsAny, ok := reality["shortIds"]
	if !ok {
		t.Fatal("realitySettings missing 'shortIds'")
	}
	shortIds, ok := shortIdsAny.([]any)
	if !ok {
		t.Fatal("shortIds is not []any")
	}
	xhttpAny, ok := stream["xhttpSettings"]
	if !ok {
		t.Fatal("streamSettings missing 'xhttpSettings'")
	}
	xhttp, ok := xhttpAny.(map[string]any)
	if !ok {
		t.Fatal("xhttpSettings is not map[string]any")
	}
	ruPath, ok := xhttp["path"].(string)
	if !ok {
		t.Fatal("xhttpSettings.path is not a string")
	}

	// Parse the subscription URI.
	uri := ClientURI(c, client)
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse URI: %v", err)
	}
	q := u.Query()
	uriSID := q.Get("sid")
	uriPath := q.Get("path")
	uriSNI := q.Get("sni")

	// Assert sid is in the RU inbound's shortIds list.
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
		t.Errorf("URI sid=%q not found in RU inbound shortIds %v", uriSID, shortIds)
	}

	// Assert path matches.
	if uriPath != ruPath {
		t.Errorf("URI path=%q does not match RU inbound xhttpSettings.path=%q", uriPath, ruPath)
	}

	// Assert sni is in the RU inbound's serverNames list.
	sniFound := false
	for _, v := range serverNames {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s == uriSNI {
			sniFound = true
			break
		}
	}
	if !sniFound {
		t.Errorf("URI sni=%q not found in RU inbound serverNames %v", uriSNI, serverNames)
	}
}
