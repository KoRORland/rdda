// Package dest discovers a plausible, nearby REALITY handshake destination for
// a node.
//
// The REALITY `dest` (the borrowed site whose TLS identity the inspected
// client→RU hop mimics) is fleet-visible: shipping the same default on every
// deployment hands a censor a class signature — "REALITY nodes fronting host X
// that also carry tunnel-shaped traffic" — that generalizes from one
// characterized node to all of them. Instead of a shared default, each node
// discovers its own dest by probing a curated set of popular, CDN-fronted
// domains FROM THIS NODE and picking the nearest one that validates as a real
// TLS 1.3 + HTTP/2 site.
//
// Two properties drive the choice: the dest must be NEARBY (low RTT — so a
// censor correlating "how far is this website really" against the node's own
// latency finds nothing off) and PLAUSIBLE AS A TRAFFIC PATTERN (a target that
// legitimately receives large inbound TLS volume, so this node funneling many
// connections to it is unremarkable). Popular CDN-fronted hosts satisfy
// plausibility by construction and give "nearby" for free via anycast edges;
// probing from the node and ranking by RTT turns that into a concrete pick that
// differs naturally across nodes in different regions.
package dest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

//go:embed candidates.txt
var candidatesFile []byte

// Candidates returns the curated seed list of popular, CDN-fronted domains to
// probe. They are only candidates: correctness is enforced per-node by Probe,
// not by trusting the list, so a listed host that has become blocked or dropped
// TLS 1.3/h2 is simply skipped. Selection criteria (see candidates.txt): popular
// + CDN-fronted (anycast ⇒ a nearby edge, universal TLS 1.3/h2) + plausibly
// unblocked in RU + not the Cloudflare edge the RU→EU tunnel already fronts.
func Candidates() []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(candidatesFile))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Result is the outcome of probing one candidate host from this node.
type Result struct {
	Host   string
	OK     bool
	RTT    time.Duration
	Reason string // why it failed to validate (empty when OK)
}

// Prober validates one candidate as a REALITY dest from this node and measures
// its RTT. Tests inject a fake.
type Prober interface {
	Probe(ctx context.Context, host string) Result
}

// DefaultProber validates a candidate by opening a real TLS 1.3 + HTTP/2
// request to it and measuring the round trip. A candidate qualifies only if the
// borrowed handshake would be real: TLS 1.3 negotiated over X25519, ALPN h2, a
// valid CA-signed cert (verification is ON here — unlike the reachability-only
// probe in check-dest — because a REALITY dest needs a genuine cert), and a
// real page: a 2xx, or a 3xx that stays on the same registrable domain (a
// locale redirect like microsoft.com→microsoft.com/en-us/ is genuine site
// behavior and a fine borrowed identity — only a CROSS-domain redirect, or a
// 4xx/5xx parking/error page, disqualifies).
type DefaultProber struct {
	Timeout time.Duration // per-candidate; 0 ⇒ 8s
}

func (d DefaultProber) Probe(ctx context.Context, host string) Result {
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 8 * time.Second
	}
	tr := &http.Transport{
		// TLS 1.3 only, X25519 only: if the server can't do either, the
		// handshake fails and the candidate is dropped — exactly the REALITY
		// requirement. Cert verification stays ON (InsecureSkipVerify false).
		TLSClientConfig: &tls.Config{
			MinVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{tls.X25519},
		},
		// A custom TLSClientConfig otherwise disables h2; force the attempt so
		// net/http adds h2 to ALPN and we can observe whether the server takes it.
		ForceAttemptHTTP2:   true,
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport: tr,
		Timeout:   timeout,
		// Observe the dest's OWN status: a dest that 3xx-redirects elsewhere is a
		// poor borrowed identity, so don't follow — require a direct 200.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return Result{Host: host, Reason: "bad request: " + err.Error()}
	}
	start := time.Now()
	resp, err := client.Do(req)
	rtt := time.Since(start)
	if err != nil {
		return Result{Host: host, RTT: rtt, Reason: tlsFailReason(err)}
	}
	defer func() { _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); resp.Body.Close() }()

	if resp.TLS == nil || resp.TLS.Version != tls.VersionTLS13 {
		return Result{Host: host, RTT: rtt, Reason: "no TLS 1.3"}
	}
	if resp.TLS.NegotiatedProtocol != "h2" {
		return Result{Host: host, RTT: rtt, Reason: "no HTTP/2 (ALPN h2)"}
	}
	if reason := realPage(host, resp); reason != "" {
		return Result{Host: host, RTT: rtt, Reason: reason}
	}
	return Result{Host: host, OK: true, RTT: rtt}
}

// realPage returns "" if the response is an acceptable REALITY borrowed
// identity, else a short reason. 2xx passes; a 3xx passes only if its Location
// stays on host's registrable domain (a same-site locale redirect); everything
// else (cross-domain redirect, 4xx/5xx) fails.
func realPage(host string, resp *http.Response) string {
	switch resp.StatusCode / 100 {
	case 2:
		return ""
	case 3:
		if sameSite(host, resp.Header.Get("Location")) {
			return ""
		}
		return fmt.Sprintf("redirects off-domain (%d → %s)", resp.StatusCode, resp.Header.Get("Location"))
	default:
		return fmt.Sprintf("not a real page (got %d)", resp.StatusCode)
	}
}

// sameSite reports whether a redirect Location stays on host's registrable
// domain. A relative Location (no host) is same-site by definition.
func sameSite(host, location string) bool {
	if location == "" {
		return false
	}
	u, err := url.Parse(location)
	if err != nil {
		return false
	}
	lh := u.Hostname()
	if lh == "" {
		return true // relative redirect ⇒ same host
	}
	return registrable(host) == registrable(lh)
}

func registrable(host string) string {
	if d, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(host, ".")); err == nil {
		return d
	}
	return host
}

func tlsFailReason(err error) string {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "timeout"):
		return "unreachable/timeout"
	case strings.Contains(msg, "certificate"):
		return "invalid certificate"
	default:
		return "handshake failed: " + msg
	}
}

// Discover probes every host concurrently (bounded) and returns the results
// ranked best-first: validating hosts sorted by ascending RTT (nearest first),
// then the failures. Best(Discover(...)) is the auto-pick.
func Discover(ctx context.Context, hosts []string, p Prober) []Result {
	const maxConc = 8
	results := make([]Result, len(hosts))
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, h string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = p.Probe(ctx, h)
		}(i, h)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK // passing hosts first
		}
		if results[i].OK {
			return results[i].RTT < results[j].RTT // nearest first
		}
		return false // preserve input order among failures
	})
	return results
}

// Best returns the nearest validating result — the host to auto-pick. ok=false
// means no candidate validated from this node.
func Best(results []Result) (Result, bool) {
	for _, r := range results {
		if r.OK {
			return r, true
		}
	}
	return Result{}, false
}
