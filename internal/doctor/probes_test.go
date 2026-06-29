package doctor

import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/keys"
)

func TestBuildClientConfig(t *testing.T) {
	kp, _ := keys.NewX25519Keypair()
	ru := map[string]any{
		"inbounds": []map[string]any{{
			"type": "vless", "listen_port": 8443,
			"users": []map[string]any{{"uuid": "uuid-xyz"}},
			"tls": map[string]any{
				"server_name": "addons.mozilla.org",
				"reality": map[string]any{
					"enabled": true, "private_key": kp.PrivateKey, "short_id": []string{"ab12"},
					"handshake": map[string]any{"server": "addons.mozilla.org", "server_port": 443},
				},
			},
		}},
	}
	b, _ := json.Marshal(ru)
	cfg, uuid, err := buildClientConfig(b, 11099)
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "uuid-xyz" {
		t.Fatalf("uuid: %s", uuid)
	}
	var doc map[string]any
	if err := json.Unmarshal(cfg, &doc); err != nil {
		t.Fatal(err)
	}
	out := doc["outbounds"].([]any)[0].(map[string]any)
	tls := out["tls"].(map[string]any)
	r := tls["reality"].(map[string]any)
	if r["public_key"] != kp.PublicKey {
		t.Fatalf("public key not derived from the inbound private key")
	}
	if out["server"] != "127.0.0.1" || tls["server_name"] != "addons.mozilla.org" {
		t.Fatalf("client config server/sni wrong: %v / %v", out["server"], tls["server_name"])
	}
	if doc["inbounds"].([]any)[0].(map[string]any)["listen_port"].(float64) != 11099 {
		t.Fatal("socks port not applied")
	}
}

func TestBuildClientConfigNoUUID(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"inbounds": []map[string]any{{"tls": map[string]any{"reality": map[string]any{"enabled": true, "private_key": "x"}}}}})
	if _, _, err := buildClientConfig(b, 1080); err == nil {
		t.Fatal("missing uuid must error (so egress WARNs)")
	}
}
