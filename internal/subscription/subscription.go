// Package subscription builds the sing-box client config a Hiddify user imports.
package subscription

import (
	"encoding/base64"
	"encoding/json"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// ProfileTitle is the name Hiddify shows for an RDDA profile. It replaces the
// "UNKNOWN" that Hiddify falls back to when a subscription carries no name: it is
// surfaced via the Profile-Title HTTP header (URL path) and the in-file
// ImportHeader comment (QR/file path).
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
	doc, err := buildDoc(cfg, c)
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	return string(b), err
}

// QRPayload returns the self-contained config to embed in a client's QR: the
// ImportHeader (which names the profile) followed by the *compact* hardened
// sing-box config. Unlike a subscription URL, this references only the RU entry
// node — never the EU exit — so scanning it exposes nothing about the exit and
// needs no network fetch; the client imports it offline. Compact (unindented)
// JSON keeps the QR as small/scannable as the ~1KB config allows.
func QRPayload(cfg state.Config, c state.Client) (string, error) {
	doc, err := buildDoc(cfg, c)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return ImportHeader() + string(b), nil
}

// buildDoc assembles the hardened sing-box config document shared by Build (human-
// readable, indented) and QRPayload (compact, QR-embedded).
func buildDoc(cfg state.Config, c state.Client) (obj, error) {
	ob, err := ClientOutbound(cfg, c)
	if err != nil {
		return nil, err
	}
	var out obj
	if err := json.Unmarshal(ob, &out); err != nil {
		return nil, err
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
	return doc, nil
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
