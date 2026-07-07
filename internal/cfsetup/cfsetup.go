// Package cfsetup parses cloudflared CLI output for the zero-touch Cloudflare
// tunnel bring-up (rdda cf setup). The parsing is isolated here because it is
// the error-prone, order-sensitive core of the bring-up — the part that in the
// field silently shipped a dead endpoint when `tunnel route dns` no-op'd on a
// pre-existing record. Keeping it pure makes it testable without a live tunnel.
package cfsetup

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// "Created tunnel NAME with id <uuid>"
	reCreatedID = regexp.MustCompile(`Created tunnel .* with id ([0-9a-fA-F-]{36})`)
	// "Tunnel credentials written to /path/<uuid>.json."
	reCredsPath = regexp.MustCompile(`credentials written to (\S+?\.json)`)
	uuidRe      = regexp.MustCompile(`[0-9a-fA-F-]{36}`)
)

// ParseCreate extracts the tunnel ID and credentials-file path from the output
// of `cloudflared tunnel create <name>`.
func ParseCreate(out string) (id, creds string, err error) {
	if m := reCreatedID.FindStringSubmatch(out); m != nil {
		id = m[1]
	}
	if m := reCredsPath.FindStringSubmatch(out); m != nil {
		creds = strings.TrimRight(m[1], ".")
	}
	if id == "" {
		return "", "", fmt.Errorf("could not find tunnel id in create output")
	}
	return id, creds, nil
}

// FindTunnelID scans `cloudflared tunnel list` output for a tunnel named name
// and returns its ID. Used for idempotency: a re-run reuses the existing tunnel
// instead of failing on "tunnel with name already exists".
func FindTunnelID(listOut, name string) (string, error) {
	for _, line := range strings.Split(listOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Table rows are: ID NAME CREATED CONNECTIONS. Match the NAME column
		// exactly, guarding against a header row or the info banner.
		if fields[1] == name && uuidRe.MatchString(fields[0]) {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no tunnel named %q in list output", name)
}

// RouteResult classifies the outcome of `cloudflared tunnel route dns`.
type RouteResult struct {
	OK       bool   // the hostname now routes to this tunnel
	Conflict bool   // a pre-existing DNS record blocks the route (the silent-no-op trap)
	Msg      string // human-readable summary
}

var reRouteOK = regexp.MustCompile(`(?i)(added|updated) (cname|record).*route to this tunnel`)

// ClassifyRouteDNS interprets route-dns output + the command error. A conflict
// (an existing A/AAAA/CNAME on the hostname) is the field failure that left the
// sub host serving a stale origin: it must be surfaced, never treated as done.
func ClassifyRouteDNS(out string, cmdErr error) RouteResult {
	text := out
	if cmdErr != nil {
		text += " " + cmdErr.Error()
	}
	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "already exists"):
		return RouteResult{Conflict: true, Msg: "a DNS record already exists for this hostname — delete it (or repoint it) so the route reaches this tunnel"}
	case reRouteOK.MatchString(out):
		return RouteResult{OK: true, Msg: "routed to this tunnel"}
	case cmdErr != nil:
		return RouteResult{Msg: "route dns failed: " + cmdErr.Error()}
	default:
		// No explicit confirmation and no error: treat as unverified, not OK.
		return RouteResult{Msg: "route dns returned no confirmation: " + strings.TrimSpace(out)}
	}
}
