package cli

import "testing"

func TestExtractRealityDest_Inbound(t *testing.T) {
	cfg := []byte(`{"inbounds":[{"type":"vless","tls":{"enabled":true,"server_name":"addons.mozilla.org",
		"reality":{"enabled":true,"handshake":{"server":"addons.mozilla.org","server_port":443}}}}]}`)
	d, ok := extractRealityDest(cfg)
	if !ok {
		t.Fatal("expected a REALITY dest")
	}
	if d.host != "addons.mozilla.org" || d.port != 443 {
		t.Fatalf("got %s:%d", d.host, d.port)
	}
}

func TestExtractRealityDest_DefaultPort(t *testing.T) {
	cfg := []byte(`{"inbounds":[{"tls":{"reality":{"enabled":true,"handshake":{"server":"example.com"}}}}]}`)
	d, ok := extractRealityDest(cfg)
	if !ok || d.host != "example.com" || d.port != 443 {
		t.Fatalf("default port not applied: %+v ok=%v", d, ok)
	}
}

func TestExtractRealityDest_NoReality(t *testing.T) {
	// EU-under-Cloudflare: a plain WebSocket inbound, no REALITY -> nothing to verify.
	cfg := []byte(`{"inbounds":[{"type":"vless","transport":{"type":"ws","path":"/tn"}}]}`)
	if _, ok := extractRealityDest(cfg); ok {
		t.Fatal("ws inbound has no REALITY dest to verify")
	}
}

func TestExtractRealityDest_Malformed(t *testing.T) {
	if _, ok := extractRealityDest([]byte("not json")); ok {
		t.Fatal("malformed config must not yield a dest")
	}
}
