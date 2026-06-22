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
	q.Set("type", "xhttp")
	q.Set("path", cfg.ClientPath)
	q.Set("security", "reality")
	q.Set("encryption", "none")
	q.Set("pbk", cfg.ClientReality.PublicKey)
	q.Set("sni", cfg.ClientReality.ServerName)
	q.Set("sid", c.ShortID)
	q.Set("fp", "chrome")
	u := url.URL{
		Scheme:   "vless",
		User:     url.User(c.UUID),
		Host:     fmt.Sprintf("%s:%d", cfg.RUHost, cfg.RUPort),
		RawQuery: q.Encode(),
		Fragment: c.Name,
	}
	return u.String()
}

// Build returns the base64-encoded subscription body (one URI) for a client.
func Build(cfg state.Config, c state.Client) string {
	return base64.StdEncoding.EncodeToString([]byte(ClientURI(cfg, c)))
}
