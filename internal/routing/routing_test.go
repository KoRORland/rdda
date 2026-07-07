package routing

import "testing"

// A rendered RU config with all three rule types + geoip rule-set.
const ruConfig = `{
  "route": {
    "rules": [
      {"ip_is_private": true, "outbound": "direct"},
      {"rule_set": "geoip-ru", "outbound": "direct"},
      {"domain_suffix": ["gov.ru", ".yandex.ru"], "outbound": "direct"}
    ],
    "final": "proxy",
    "rule_set": [{"type":"local","tag":"geoip-ru","format":"binary","path":"/etc/rdda/geoip-ru.srs"}]
  }
}`

// geoip fake: 77.88.* is "domestic", everything else not.
func fakeGeoIP(domestic bool) GeoIPMatcher {
	return func(_, ip string) (bool, bool) { return domestic, true }
}

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		geoip        GeoIPMatcher
		wantOutbound string
		wantRule     string
	}{
		{"private IP → direct", "192.168.1.5", fakeGeoIP(false), "direct", "ip_is_private"},
		{"loopback → direct", "127.0.0.1", fakeGeoIP(false), "direct", "ip_is_private"},
		{"domestic IP → direct via geoip", "77.88.55.88", fakeGeoIP(true), "direct", "rule_set:geoip-ru"},
		{"foreign IP → tunnel", "8.8.8.8", fakeGeoIP(false), "proxy", "final"},
		{"allow-listed domain → direct", "www.yandex.ru", nil, "direct", "domain_suffix"},
		{"exact suffix domain → direct", "gov.ru", nil, "direct", "domain_suffix"},
		{"foreign domain → tunnel", "youtube.com", nil, "proxy", "final"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Evaluate([]byte(ruConfig), c.input, c.geoip)
			if err != nil {
				t.Fatal(err)
			}
			if d.Outbound != c.wantOutbound || d.Rule != c.wantRule {
				t.Fatalf("got %s via %q, want %s via %q\ntrace: %v", d.Outbound, d.Rule, c.wantOutbound, c.wantRule, d.Trace)
			}
		})
	}
}

// A foreign IP whose geoip verdict is indeterminate must fall through to the
// tunnel (final) and say so, never silently claim "direct".
func TestEvaluate_GeoIPIndeterminate(t *testing.T) {
	indeterminate := GeoIPMatcher(func(_, _ string) (bool, bool) { return false, false })
	d, err := Evaluate([]byte(ruConfig), "8.8.8.8", indeterminate)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outbound != "proxy" {
		t.Fatalf("indeterminate geoip must not divert from final: got %s", d.Outbound)
	}
	var sawUnknown bool
	for _, s := range d.Trace {
		if contains(s, "UNKNOWN") {
			sawUnknown = true
		}
	}
	if !sawUnknown {
		t.Errorf("trace should flag the geoip step UNKNOWN: %v", d.Trace)
	}
}

func TestEvaluate_NoGeoRuleSet(t *testing.T) {
	// Config with geoip disabled (no rule_set): domestic IPs tunnel like any other.
	cfg := `{"route":{"rules":[{"ip_is_private":true,"outbound":"direct"}],"final":"proxy"}}`
	d, _ := Evaluate([]byte(cfg), "77.88.55.88", fakeGeoIP(true))
	if d.Outbound != "proxy" || d.Rule != "final" {
		t.Fatalf("without a geoip rule a public IP tunnels: got %s via %q", d.Outbound, d.Rule)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
