// Package subscription builds the sing-box client config a Hiddify user imports.
package subscription

import (
	"encoding/json"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// ClientOutbound returns one sing-box VLESS/REALITY/multiplex outbound object
// pointing at the RU entry node.
func ClientOutbound(cfg state.Config, c state.Client) ([]byte, error) {
	sid := ""
	if len(cfg.ClientReality.ShortIDs) > 0 {
		sid = cfg.ClientReality.ShortIDs[0]
	}
	out := obj{
		"type": "vless", "tag": "rdda",
		"server": cfg.RUHost, "server_port": cfg.RUPort,
		"uuid": c.UUID,
		"tls": obj{
			"enabled":     true,
			"server_name": cfg.ClientReality.ServerName,
			"utls":        obj{"enabled": true, "fingerprint": c.FingerprintOr(cfg.FP())},
			"reality":     obj{"enabled": true, "public_key": cfg.ClientReality.PublicKey, "short_id": sid},
		},
		"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
	}
	return json.MarshalIndent(out, "", "  ")
}

// Build returns the full sing-box client config (subscription body) for a client:
// a TUN/SOCKS-less minimal config with the RDDA outbound + a direct fallback.
func Build(cfg state.Config, c state.Client) (string, error) {
	ob, err := ClientOutbound(cfg, c)
	if err != nil {
		return "", err
	}
	var out obj
	if err := json.Unmarshal(ob, &out); err != nil {
		return "", err
	}
	doc := obj{
		"log":       obj{"level": "warn"},
		"outbounds": []obj{out, {"type": "direct", "tag": "direct"}},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	return string(b), err
}
