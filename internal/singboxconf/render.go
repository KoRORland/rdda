// Package singboxconf renders sing-box JSON configs from RDDA state.
package singboxconf

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// splitHostPort splits "host:port" into (host, port), applying defPort when no
// port is present. A REALITY handshake target is typically "host:443".
func splitHostPort(target string, defPort int) (string, int) {
	h, p, ok := strings.Cut(target, ":")
	if !ok {
		return target, defPort
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, defPort
	}
	return h, n
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// RenderClient builds a sing-box client config: SOCKS inbound -> VLESS/REALITY/
// multiplex outbound to the RU node. clientUUID must match a UUID the RU serves.
func RenderClient(cfg state.Config, clientUUID string, socksPort int) ([]byte, error) {
	out := obj{
		"type": "vless", "tag": "proxy",
		"server": cfg.RUHost, "server_port": cfg.RUPort,
		"uuid": clientUUID,
		"tls": obj{
			"enabled":     true,
			"server_name": cfg.ClientReality.ServerName,
			"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
			"reality": obj{
				"enabled":    true,
				"public_key": cfg.ClientReality.PublicKey,
				"short_id":   firstOrEmpty(cfg.ClientReality.ShortIDs),
			},
		},
		"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
	}
	doc := obj{
		"log":       obj{"level": "warn"},
		"inbounds":  []obj{{"type": "socks", "tag": "socks-in", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []obj{out, {"type": "direct", "tag": "direct"}},
	}
	return json.MarshalIndent(doc, "", "  ")
}
