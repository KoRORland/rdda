// Package subscription builds Hiddify/xray subscription bodies from RDDA state.
package subscription

import (
	"encoding/base64"
	"fmt"
	"net/url"

	"github.com/KoRORland/rdda/internal/state"
)

// ClientURI returns a VLESS share link pointing at the RU entry node.
func ClientURI(cfg state.Config, c state.Client) string {
	q := url.Values{}
	q.Set("type", "ws")
	q.Set("host", cfg.RUHost)
	q.Set("path", cfg.ClientPath)
	q.Set("security", "reality")
	q.Set("encryption", "none")
	q.Set("pbk", cfg.ClientReality.PublicKey)
	q.Set("sni", cfg.ClientReality.ServerName)
	sid := ""
	if len(cfg.ClientReality.ShortIDs) > 0 {
		sid = cfg.ClientReality.ShortIDs[0]
	}
	q.Set("sid", sid)
	q.Set("fp", cfg.FP())
	u := url.URL{
		Scheme:   "vless",
		User:     url.User(c.UUID),
		Host:     fmt.Sprintf("%s:%d", cfg.RUHost, cfg.RUPort),
		RawQuery: q.Encode(),
		Fragment: c.Name,
	}
	return u.String()
}

// Build returns the base64-encoded subscription body for a client.
// The decoded body is a v2ray-style subscription: one URI per line, with a
// leading comment reminding the operator to enable Mux/multiplex in Hiddify
// (the bare vless link cannot carry mux settings).
func Build(cfg state.Config, c state.Client) string {
	body := "# enable Mux/multiplex in Hiddify settings\n" + ClientURI(cfg, c)
	return base64.StdEncoding.EncodeToString([]byte(body))
}
