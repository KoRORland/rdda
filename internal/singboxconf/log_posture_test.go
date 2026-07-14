package singboxconf

import (
	"encoding/json"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

// The no-logs posture (v0.5 #20, SECURITY.md): every node config must log at
// "warn", which is above the "info" level at which sing-box logs per-connection
// data. This guards against an accidental bump to info/debug that would start
// recording connections — logs that could deanonymize a user.
func TestNodeConfigsLogAtWarn(t *testing.T) {
	ruCfg := clientCfg()
	ruCfg.TunnelUUID = "tuuid"
	ruCfg.Cloudflare = state.Cloudflare{TunnelHostname: "tunnel.example.com"}
	ruCfg.TunnelReality = state.Reality{
		Target: "www.microsoft.com:443", ServerName: "www.microsoft.com",
		PublicKey: "V9J9A-cx7pe0AmSaVcYwBg39y6W3wIxv9nciaXO8AmI", ShortIDs: []string{"abcd1234"},
	}

	cases := map[string]func() ([]byte, error){
		"RU":     func() ([]byte, error) { return RenderRU(ruCfg, nil) },
		"EU":     func() ([]byte, error) { return RenderEU(ruCfg) },
		"client": func() ([]byte, error) { return RenderClient(clientCfg(), "uuid-1", 1080) },
	}
	for name, render := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := render()
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(b, &doc); err != nil {
				t.Fatal(err)
			}
			lg, ok := doc["log"].(map[string]any)
			if !ok {
				t.Fatalf("%s config has no log block", name)
			}
			if lg["level"] != "warn" {
				t.Fatalf("%s log level = %v, want warn (info would log connections)", name, lg["level"])
			}
		})
	}
}
