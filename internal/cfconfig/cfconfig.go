// Package cfconfig renders the cloudflared ingress config.yml from RDDA state.
package cfconfig

import (
	"fmt"

	"github.com/KoRORland/rdda/internal/state"
	"gopkg.in/yaml.v3"
)

// Render returns a cloudflared config.yml that routes the data-hop hostname to
// the local sing-box WebSocket inbound and the subscription hostname to the local
// subscription server. subPort is the loopback port the sub server listens on.
func Render(cfg state.Config, subPort int) ([]byte, error) {
	if !cfg.CFEnabled() || cfg.Cloudflare.TunnelID == "" {
		return nil, fmt.Errorf("cloudflare not configured (tunnel_hostname and tunnel_id required)")
	}
	doc := map[string]any{
		"tunnel":           cfg.Cloudflare.TunnelID,
		"credentials-file": cfg.Cloudflare.CredentialsFile,
		"ingress": []map[string]any{
			{"hostname": cfg.Cloudflare.TunnelHostname, "service": fmt.Sprintf("http://localhost:%d", cfg.EUPort)},
			{"hostname": cfg.Cloudflare.SubHostname, "service": fmt.Sprintf("http://localhost:%d", subPort)},
			{"service": "http_status:404"},
		},
	}
	return yaml.Marshal(doc)
}
