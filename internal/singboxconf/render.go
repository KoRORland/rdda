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

// RenderRU builds the RU node config: REALITY inbound for clients + an outbound
// to EU (WebSocket behind Cloudflare, else REALITY direct) + split routing.
func RenderRU(cfg state.Config, clients []state.Client) ([]byte, error) {
	users := make([]obj, 0, len(clients))
	for _, c := range clients {
		users = append(users, obj{"uuid": c.UUID})
	}
	hsHost, hsPort := splitHostPort(cfg.ClientReality.Target, 443)
	inbound := obj{
		"type": "vless", "tag": "in",
		"listen": "0.0.0.0", "listen_port": cfg.RUPort,
		"users": users,
		"tls": obj{
			"enabled":     true,
			"server_name": cfg.ClientReality.ServerName,
			"reality": obj{
				"enabled":     true,
				"handshake":   obj{"server": hsHost, "server_port": hsPort},
				"private_key": cfg.ClientReality.PrivateKey,
				"short_id":    cfg.ClientReality.ShortIDs,
			},
		},
		"multiplex": obj{"enabled": true},
	}

	var proxy obj
	if cfg.CFEnabled() {
		// OVERRIDE 1: CF rewrites HTTPUpgrade to WebSocket; use ws transport.
		// ws puts the Host override in a "headers" sub-object (not top-level "host").
		proxy = obj{
			"type": "vless", "tag": "proxy",
			"server": cfg.Cloudflare.TunnelHostname, "server_port": 443,
			"uuid": cfg.TunnelUUID,
			"tls": obj{
				"enabled":     true,
				"server_name": cfg.Cloudflare.TunnelHostname,
				"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
			},
			"transport": obj{"type": "ws", "path": cfg.TunnelPath, "headers": obj{"Host": cfg.Cloudflare.TunnelHostname}},
			"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
		}
	} else {
		proxy = obj{
			"type": "vless", "tag": "proxy",
			"server": cfg.EUHost, "server_port": cfg.EUPort,
			"uuid": cfg.TunnelUUID,
			"tls": obj{
				"enabled":     true,
				"server_name": cfg.TunnelReality.ServerName,
				"utls":        obj{"enabled": true, "fingerprint": cfg.FP()},
				"reality": obj{
					"enabled":    true,
					"public_key": cfg.TunnelReality.PublicKey,
					"short_id":   firstOrEmpty(cfg.TunnelReality.ShortIDs),
				},
			},
			"multiplex": obj{"enabled": true, "protocol": "h2mux", "max_streams": 8},
		}
	}

	rules := []obj{
		{"ip_is_private": true, "outbound": "direct"},
	}
	// geoip-ru split routing is OFF unless a LOCAL rule-set path is configured. A
	// local .srs (shipped at install time) means the RU data plane never blocks
	// startup on a remote download — sing-box fetches a remote rule_set (blocking)
	// at router init and FATALs if it cannot reach it, a poor dependency for an
	// anti-censorship entry node. Empty path = tunnel everything (safe, less efficient).
	if cfg.GeoIPPath != "" {
		rules = append(rules, obj{"rule_set": "geoip-ru", "outbound": "direct"})
	}
	if len(cfg.IntlAllowDomains) > 0 {
		rules = append(rules, obj{"domain_suffix": cfg.IntlAllowDomains, "outbound": "direct"})
	}
	route := obj{"rules": rules, "final": "proxy"}
	if cfg.GeoIPPath != "" {
		route["rule_set"] = []obj{{
			"type": "local", "tag": "geoip-ru", "format": "binary", "path": cfg.GeoIPPath,
		}}
	}
	doc := obj{
		"log":      obj{"level": "warn"},
		"inbounds": []obj{inbound},
		"outbounds": []obj{
			proxy,
			{"type": "direct", "tag": "direct"},
		},
		"route": route,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// RenderEU builds the EU node config: terminate the RU tunnel, exit to internet.
// Under Cloudflare the public TLS is terminated by CF, so the inbound runs plain
// WebSocket (no TLS). Without CF it terminates REALITY directly.
func RenderEU(cfg state.Config) ([]byte, error) {
	var inbound obj
	if cfg.CFEnabled() {
		inbound = obj{
			"type": "vless", "tag": "in",
			"listen": "127.0.0.1", "listen_port": cfg.EUPort,
			"users":     []obj{{"uuid": cfg.TunnelUUID}},
			"transport": obj{"type": "ws", "path": cfg.TunnelPath},
			"multiplex": obj{"enabled": true},
		}
	} else {
		hsHost, hsPort := splitHostPort(cfg.TunnelReality.Target, 443)
		inbound = obj{
			"type": "vless", "tag": "in",
			"listen": "0.0.0.0", "listen_port": cfg.EUPort,
			"users": []obj{{"uuid": cfg.TunnelUUID}},
			"tls": obj{
				"enabled":     true,
				"server_name": cfg.TunnelReality.ServerName,
				"reality": obj{
					"enabled":     true,
					"handshake":   obj{"server": hsHost, "server_port": hsPort},
					"private_key": cfg.TunnelReality.PrivateKey,
					"short_id":    cfg.TunnelReality.ShortIDs,
				},
			},
			"multiplex": obj{"enabled": true},
		}
	}
	doc := obj{
		"log":       obj{"level": "warn"},
		"inbounds":  []obj{inbound},
		"outbounds": []obj{{"type": "direct", "tag": "direct"}},
	}
	return json.MarshalIndent(doc, "", "  ")
}
