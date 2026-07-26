package dest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeProber returns canned results keyed by host.
type fakeProber map[string]Result

func (f fakeProber) Probe(_ context.Context, host string) Result {
	if r, ok := f[host]; ok {
		r.Host = host
		return r
	}
	return Result{Host: host, Reason: "no canned result"}
}

func TestDiscover_RanksPassingByRTTNearestFirst(t *testing.T) {
	hosts := []string{"far.example", "near.example", "broken.example", "mid.example"}
	p := fakeProber{
		"far.example":    {OK: true, RTT: 300 * time.Millisecond},
		"near.example":   {OK: true, RTT: 20 * time.Millisecond},
		"broken.example": {OK: false, RTT: 5 * time.Millisecond, Reason: "no HTTP/2 (ALPN h2)"},
		"mid.example":    {OK: true, RTT: 120 * time.Millisecond},
	}
	got := Discover(context.Background(), hosts, p)

	// Passing hosts come first, nearest RTT first; failures last.
	wantOrder := []string{"near.example", "mid.example", "far.example", "broken.example"}
	for i, w := range wantOrder {
		if got[i].Host != w {
			t.Fatalf("rank[%d] = %q, want %q (full order: %v)", i, got[i].Host, w, hostsOf(got))
		}
	}

	best, ok := Best(got)
	if !ok || best.Host != "near.example" {
		t.Fatalf("Best = %q ok=%v, want near.example true", best.Host, ok)
	}
}

func TestDiscover_AllFail_NoBest(t *testing.T) {
	hosts := []string{"a.example", "b.example"}
	p := fakeProber{
		"a.example": {OK: false, Reason: "invalid certificate"},
		"b.example": {OK: false, Reason: "unreachable/timeout"},
	}
	got := Discover(context.Background(), hosts, p)
	if _, ok := Best(got); ok {
		t.Fatalf("Best returned ok on an all-fail set: %v", hostsOf(got))
	}
}

// A candidate that fails ANY REALITY requirement must never become the pick,
// even if it is the fastest host probed — a bad dest is a broken/detectable
// REALITY handshake.
func TestDiscover_FastestButInvalidNotPicked(t *testing.T) {
	hosts := []string{"fast-but-h1.example", "slow-but-valid.example"}
	p := fakeProber{
		"fast-but-h1.example":    {OK: false, RTT: 1 * time.Millisecond, Reason: "no HTTP/2 (ALPN h2)"},
		"slow-but-valid.example": {OK: true, RTT: 500 * time.Millisecond},
	}
	best, ok := Best(Discover(context.Background(), hosts, p))
	if !ok || best.Host != "slow-but-valid.example" {
		t.Fatalf("Best = %q ok=%v, want slow-but-valid.example true", best.Host, ok)
	}
}

func TestCandidates_ParsesNonEmptyNonComment(t *testing.T) {
	c := Candidates()
	if len(c) == 0 {
		t.Fatal("Candidates() is empty")
	}
	for _, h := range c {
		if h == "" || strings.HasPrefix(h, "#") || strings.ContainsAny(h, " \t") {
			t.Fatalf("candidate %q is blank, a comment, or has whitespace", h)
		}
	}
	// The list is deliberately non-Cloudflare (avoid sharing the tunnel's edge).
	for _, h := range c {
		if strings.Contains(h, "cloudflare") {
			t.Errorf("candidate %q is Cloudflare; the seed list must stay non-CF", h)
		}
	}
}

func TestSameSite(t *testing.T) {
	cases := []struct {
		host, loc string
		want      bool
	}{
		{"www.microsoft.com", "https://www.microsoft.com/en-us/", true}, // same host
		{"www.microsoft.com", "/en-us/", true},                         // relative
		{"apple.com", "https://www.apple.com/", true},                  // same registrable domain
		{"www.samsung.com", "https://www.samsung.com/us/", true},
		{"www.example.com", "https://tracker.ads.net/r?u=x", false}, // cross-domain
		{"www.example.com", "", false},                             // no Location
	}
	for _, c := range cases {
		if got := sameSite(c.host, c.loc); got != c.want {
			t.Errorf("sameSite(%q, %q) = %v, want %v", c.host, c.loc, got, c.want)
		}
	}
}

func hostsOf(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Host
	}
	return out
}
