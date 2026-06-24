// Package xrayconf renders xray-core JSON configs from RDDA state.
package xrayconf

import (
	"encoding/json"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// RenderRU builds the RU node config: client entry + tunnel to EU + split routing.
func RenderRU(cfg state.Config, clients []state.Client) ([]byte, error) {
	xrayClients := make([]obj, 0, len(clients))
	for _, c := range clients {
		xrayClients = append(xrayClients, obj{"id": c.UUID, "flow": ""})
	}

	inbound := obj{
		"listen": "0.0.0.0", "port": cfg.RUPort, "protocol": "vless", "tag": "in",
		"settings": obj{"clients": xrayClients, "decryption": "none"},
		"streamSettings": obj{
			"network":      "xhttp",
			"xhttpSettings": obj{"path": cfg.ClientPath},
			"security":     "reality",
			"realitySettings": obj{
				"target":      cfg.ClientReality.Target,
				"serverNames": []string{cfg.ClientReality.ServerName},
				"privateKey":  cfg.ClientReality.PrivateKey,
				"shortIds":    cfg.ClientReality.ShortIDs,
			},
		},
		"sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
	}

	var proxyOut obj
	if cfg.CFEnabled() {
		proxyOut = obj{
			"protocol": "vless", "tag": "proxy",
			"settings": obj{"vnext": []obj{{
				"address": cfg.Cloudflare.TunnelHostname, "port": 443,
				"users": []obj{{"id": cfg.TunnelUUID, "encryption": "none", "flow": ""}},
			}}},
			"streamSettings": obj{
				"network":       "xhttp",
				"xhttpSettings": obj{"path": cfg.TunnelPath, "host": cfg.Cloudflare.TunnelHostname},
				"security":      "tls",
				"tlsSettings": obj{
					"serverName":  cfg.Cloudflare.TunnelHostname,
					"alpn":        []string{"h2", "http/1.1"},
					"fingerprint": "chrome",
				},
			},
		}
	} else {
		proxyOut = obj{
			"protocol": "vless", "tag": "proxy",
			"settings": obj{"vnext": []obj{{
				"address": cfg.EUHost, "port": cfg.EUPort,
				"users": []obj{{"id": cfg.TunnelUUID, "encryption": "none", "flow": ""}},
			}}},
			"streamSettings": obj{
				"network":       "xhttp",
				"xhttpSettings": obj{"path": cfg.TunnelPath},
				"security":      "reality",
				"realitySettings": obj{
					"serverName":  cfg.TunnelReality.ServerName,
					"publicKey":   cfg.TunnelReality.PublicKey,
					"shortId":     firstOrEmpty(cfg.TunnelReality.ShortIDs),
					"fingerprint": "chrome",
				},
			},
		}
	}

	rules := []obj{
		{"type": "field", "ip": []string{"geoip:private", "geoip:ru"}, "outboundTag": "direct"},
	}
	if len(cfg.IntlAllowDomains) > 0 {
		rules = append(rules, obj{"type": "field", "domain": cfg.IntlAllowDomains, "outboundTag": "direct"})
	}

	doc := obj{
		"log":      obj{"loglevel": "warning"},
		"inbounds": []obj{inbound},
		"outbounds": []obj{
			proxyOut,
			{"protocol": "freedom", "tag": "direct"},
			{"protocol": "blackhole", "tag": "block"},
		},
		"routing": obj{"domainStrategy": "IPIfNonMatch", "rules": rules},
	}
	return json.MarshalIndent(doc, "", "  ")
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// RenderClient builds a minimal client-side xray config:
// SOCKS5 inbound on socksPort → VLESS/xhttp/REALITY outbound to the RU node.
// clientUUID must match one of the UUIDs registered in the RU server config.
func RenderClient(cfg state.Config, clientUUID string, socksPort int) ([]byte, error) {
	inbound := obj{
		"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "tag": "socks-in",
		"settings": obj{"auth": "noauth", "udp": false},
	}
	outbound := obj{
		"protocol": "vless", "tag": "proxy",
		"settings": obj{"vnext": []obj{{
			"address": cfg.RUHost, "port": cfg.RUPort,
			"users": []obj{{"id": clientUUID, "encryption": "none", "flow": ""}},
		}}},
		"streamSettings": obj{
			"network":       "xhttp",
			"xhttpSettings": obj{"path": cfg.ClientPath},
			"security":      "reality",
			"realitySettings": obj{
				"serverName":  cfg.ClientReality.ServerName,
				"publicKey":   cfg.ClientReality.PublicKey,
				"shortId":     firstOrEmpty(cfg.ClientReality.ShortIDs),
				"fingerprint": "chrome",
			},
		},
	}
	doc := obj{
		"log":       obj{"loglevel": "warning"},
		"inbounds":  []obj{inbound},
		"outbounds": []obj{outbound},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// RenderEU builds the EU node config: terminate the RU tunnel, exit to the internet.
func RenderEU(cfg state.Config) ([]byte, error) {
	var inbound obj
	if cfg.CFEnabled() {
		inbound = obj{
			"listen": "127.0.0.1", "port": cfg.EUPort, "protocol": "vless", "tag": "in",
			"settings": obj{
				"clients":    []obj{{"id": cfg.TunnelUUID, "flow": ""}},
				"decryption": "none",
			},
			"streamSettings": obj{
				"network":       "xhttp",
				"xhttpSettings": obj{"path": cfg.TunnelPath},
				"security":      "none",
			},
			"sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
		}
	} else {
		inbound = obj{
			"listen": "0.0.0.0", "port": cfg.EUPort, "protocol": "vless", "tag": "in",
			"settings": obj{
				"clients":    []obj{{"id": cfg.TunnelUUID, "flow": ""}},
				"decryption": "none",
			},
			"streamSettings": obj{
				"network":       "xhttp",
				"xhttpSettings": obj{"path": cfg.TunnelPath},
				"security":      "reality",
				"realitySettings": obj{
					"target":      cfg.TunnelReality.Target,
					"serverNames": []string{cfg.TunnelReality.ServerName},
					"privateKey":  cfg.TunnelReality.PrivateKey,
					"shortIds":    cfg.TunnelReality.ShortIDs,
				},
			},
			"sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls", "quic"}},
		}
	}
	doc := obj{
		"log":      obj{"loglevel": "warning"},
		"inbounds": []obj{inbound},
		"outbounds": []obj{
			{"protocol": "freedom", "tag": "direct"},
			{"protocol": "blackhole", "tag": "block"},
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}
