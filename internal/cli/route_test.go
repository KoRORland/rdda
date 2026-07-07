package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const routeTestConfig = `{
  "route": {
    "rules": [
      {"ip_is_private": true, "outbound": "direct"},
      {"rule_set": "geoip-ru", "outbound": "direct"},
      {"domain_suffix": [".yandex.ru"], "outbound": "direct"}
    ],
    "final": "proxy",
    "rule_set": [{"type":"local","tag":"geoip-ru","format":"binary","path":"/etc/rdda/geoip-ru.srs"}]
  }
}`

func TestRouteTest_DomainVerdicts(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "singbox.json")
	if err := os.WriteFile(cfg, []byte(routeTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// A foreign domain tunnels; an allow-listed one goes direct. Neither needs
	// sing-box (geoip is only consulted for IPs), so this is deterministic.
	out := run(t, "route", "test", "--config", cfg, "youtube.com", "maps.yandex.ru")
	if !strings.Contains(out, "youtube.com") || !strings.Contains(out, "TUNNEL") {
		t.Errorf("foreign domain should TUNNEL:\n%s", out)
	}
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "maps.yandex.ru") {
			line = l
		}
	}
	if !strings.Contains(line, "DIRECT") || !strings.Contains(line, "domain_suffix") {
		t.Errorf("allow-listed domain should be DIRECT via domain_suffix, got: %q", line)
	}
}

func TestRouteTest_MissingConfig(t *testing.T) {
	if err := runErr(t, "route", "test", "--config", "/nonexistent/singbox.json", "example.com"); err == nil {
		t.Error("missing config must error")
	}
}
