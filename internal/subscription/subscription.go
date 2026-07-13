// Package subscription builds the sing-box client config a Hiddify user imports.
package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// ProfileTitle is the name Hiddify shows for an RDDA profile. It replaces the
// "UNKNOWN" that Hiddify falls back to when a subscription carries no name: it is
// surfaced three ways — the Profile-Title HTTP header, the in-file ImportHeader
// comment, and the DeepLink fragment.
const ProfileTitle = "RDDA"

// ClientOutbound returns one sing-box VLESS/REALITY/multiplex outbound object
// pointing at the RU entry node. The tag "rdda" is referenced by Build's route
// (route.final and the DNS detour), so it must stay in sync with those.
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

// Build returns the full sing-box client config (subscription body) for a client.
// It is a complete, hardened config — not just outbounds — so the secure posture
// ships in the config itself and the user never has to open Hiddify's settings:
//   - DNS resolves via DoH tunnelled through the rdda outbound (no in-country
//     plaintext DNS leak);
//   - QUIC / UDP:443 and BitTorrent are dropped so nothing bypasses the TLS path;
//   - private/loopback traffic goes direct; everything else goes through rdda.
//
// It uses the same pre-1.11 sing-box schema (route.rules with "outbound",
// ip_is_private, block outbound) as internal/singboxconf so both sides stay on
// one sing-box version.
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
		"log": obj{"level": "warn"},
		"dns": obj{
			// DoH to a raw IP (no bootstrap needed), forced out through the rdda
			// tunnel so the resolver sees the EU exit, not the in-country client.
			"servers": []obj{
				{"tag": "remote", "address": "https://1.1.1.1/dns-query", "detour": "rdda", "strategy": "prefer_ipv4"},
			},
			"final":    "remote",
			"strategy": "prefer_ipv4",
		},
		"outbounds": []obj{
			out,
			{"type": "direct", "tag": "direct"},
			{"type": "block", "tag": "block"},
		},
		"route": obj{
			"rules": []obj{
				{"ip_is_private": true, "outbound": "direct"},
				{"protocol": "quic", "outbound": "block"},
				{"network": "udp", "port": 443, "outbound": "block"},
				{"protocol": "bittorrent", "outbound": "block"},
			},
			"final": "rdda",
		},
		"experimental": obj{
			"cache_file": obj{"enabled": true},
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	return string(b), err
}

// SubURL returns the public https subscription URL for a client, e.g.
// https://eu.example.net/sub/<token>. It reads cfg.SubBaseURL (set at init to the
// EU/CF-fronted host); an empty base yields "/sub/<token>", which callers should
// treat as a misconfiguration.
func SubURL(cfg state.Config, c state.Client) string {
	return strings.TrimRight(cfg.SubBaseURL, "/") + "/sub/" + c.Token
}

// DeepLink returns a hiddify://import/ deep link for a client. Scanning its QR (or
// opening the link) imports the subscription into Hiddify in one tap and names the
// profile via the URL fragment.
func DeepLink(cfg state.Config, c state.Client) string {
	return "hiddify://import/" + SubURL(cfg, c) + "#" + ProfileTitle
}

// ImportHeader returns the //-prefixed comment header Hiddify reads from the first
// lines of a config file (and strips before JSON-parsing). It names the profile and
// sets an update interval for users who import the config by file/clipboard rather
// than by subscription URL (where the Profile-Title HTTP header does the same job).
func ImportHeader() string {
	return "// profile-title: " + ProfileTitle + "\n" +
		"// profile-update-interval: 24\n"
}

// ProfileTitleHeader returns the value for the Hiddify "Profile-Title" HTTP
// response header, base64-encoded so non-ASCII titles survive.
func ProfileTitleHeader() string {
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(ProfileTitle))
}
