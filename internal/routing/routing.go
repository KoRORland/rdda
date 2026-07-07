// Package routing evaluates a rendered RU sing-box config's route block against
// a single destination, so an operator can ask "where would traffic to X go —
// direct or through the EU tunnel, and which rule decided?" without reading
// JSON or inferring sing-box's matching by hand.
//
// It reads the *rendered* config (singbox.json), not config.yaml, so it reports
// what the node actually runs and can never drift from RenderRU.
package routing

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// GeoIPMatcher reports whether ip is in the rule-set at srsPath. determinate is
// false when the verdict couldn't be obtained (e.g. sing-box unavailable); the
// caller then labels the geoip step as unknown rather than guessing.
type GeoIPMatcher func(srsPath, ip string) (matched, determinate bool)

// Decision is the outcome of evaluating one destination.
type Decision struct {
	Input    string
	IsIP     bool
	Outbound string   // "direct" or "proxy" (the tunnel)
	Rule     string   // which rule decided
	Trace    []string // ordered, human-readable evaluation steps
}

// Tunneled reports whether the destination exits via the EU tunnel.
func (d Decision) Tunneled() bool { return d.Outbound == "proxy" }

type routeDoc struct {
	Route struct {
		Rules   []map[string]any `json:"rules"`
		Final   string           `json:"final"`
		RuleSet []struct {
			Tag  string `json:"tag"`
			Path string `json:"path"`
		} `json:"rule_set"`
	} `json:"route"`
}

// Evaluate parses a rendered sing-box config and returns the routing decision
// for input (an IP or a domain). It mirrors sing-box's first-match-wins rule
// order; geoip is the one rule needing external data, resolved via geoip.
func Evaluate(singboxJSON []byte, input string, geoip GeoIPMatcher) (Decision, error) {
	var doc routeDoc
	if err := json.Unmarshal(singboxJSON, &doc); err != nil {
		return Decision{}, fmt.Errorf("parse sing-box config: %w", err)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return Decision{}, fmt.Errorf("empty destination")
	}
	final := doc.Route.Final
	if final == "" {
		final = "proxy"
	}
	d := Decision{Input: input, IsIP: net.ParseIP(input) != nil}

	srsPath := func(tag string) string {
		for _, rs := range doc.Route.RuleSet {
			if rs.Tag == tag {
				return rs.Path
			}
		}
		return ""
	}

	for i, r := range doc.Route.Rules {
		out, _ := r["outbound"].(string)
		switch {
		case isTrue(r["ip_is_private"]):
			if d.IsIP && isPrivateIP(input) {
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] ip_is_private → match", i))
				return finish(d, out, "ip_is_private"), nil
			}
			d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] ip_is_private → no", i))

		case r["rule_set"] != nil:
			tag := ruleSetTag(r["rule_set"])
			if !d.IsIP {
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] rule_set %q → skipped (domain; resolved+geoip'd at runtime)", i, tag))
				continue
			}
			path := srsPath(tag)
			if geoip == nil || path == "" {
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] rule_set %q → UNKNOWN (no matcher/path)", i, tag))
				continue
			}
			matched, ok := geoip(path, input)
			switch {
			case !ok:
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] rule_set %q → UNKNOWN (sing-box unavailable)", i, tag))
			case matched:
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] rule_set %q → match", i, tag))
				return finish(d, out, "rule_set:"+tag), nil
			default:
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] rule_set %q → no", i, tag))
			}

		case r["domain_suffix"] != nil:
			suffixes := toStrings(r["domain_suffix"])
			if !d.IsIP && matchDomainSuffix(input, suffixes) {
				d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] domain_suffix → match", i))
				return finish(d, out, "domain_suffix"), nil
			}
			d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] domain_suffix → no", i))

		default:
			d.Trace = append(d.Trace, fmt.Sprintf("rule[%d] (unsupported rule type) → skipped", i))
		}
	}
	d.Trace = append(d.Trace, "final")
	return finish(d, final, "final"), nil
}

func finish(d Decision, outbound, rule string) Decision {
	d.Outbound, d.Rule = outbound, rule
	return d
}

func isTrue(v any) bool { b, ok := v.(bool); return ok && b }

// isPrivateIP mirrors sing-box's ip_is_private: RFC1918 + loopback + link-local.
func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// matchDomainSuffix mirrors sing-box domain_suffix: a suffix with a leading dot
// matches only sub-labels; without one it matches the exact domain and any
// sub-label of it.
func matchDomainSuffix(domain string, suffixes []string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	for _, s := range suffixes {
		s = strings.ToLower(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, ".") {
			if strings.HasSuffix(domain, s) {
				return true
			}
			continue
		}
		if domain == s || strings.HasSuffix(domain, "."+s) {
			return true
		}
	}
	return false
}

func ruleSetTag(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
