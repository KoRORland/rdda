// Package doctor runs role-aware active health diagnostics for an RDDA node.
package doctor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/health"
	"github.com/KoRORland/rdda/internal/singboxconf"
	"github.com/KoRORland/rdda/internal/state"
)

type Status int

const (
	PASS Status = iota
	WARN
	FAIL
)

// Check is one diagnostic result.
type Check struct {
	Name   string
	Status Status
	Detail string
	Hint   string
}

// Doctor runs the checks. The probe fields are seams (real by default, faked in tests).
type Doctor struct {
	dir        string
	unitActive func(unit string) bool
	httpProbe  func(probeURL, bearer string) (code int, body []byte, notAfter time.Time, err error)
	dialDest   func(host string, port int) error
	egress     func(singboxConfig []byte, probeURL string) (ok bool, err error)
	cfInfo     func(tunnelID string) (connectors int, err error)
	svcUser    func() (uid, gid int, err error)
	statFile   func(path string) (uid, gid int, mode fs.FileMode, err error)
	dirFiles   func(path string) []string
	now        func() time.Time
}

// New returns a Doctor wired to the real probe implementations.
func New(dir string) *Doctor {
	return &Doctor{
		dir:        dir,
		unitActive: func(u string) bool { return health.UnitActive(health.DefaultRunner, u) },
		httpProbe:  realHTTPProbe,
		dialDest:   realDialDest,
		egress:     realEgress,
		cfInfo:     realCloudflaredInfo,
		svcUser:    realServiceUser,
		statFile:   realStatFile,
		dirFiles:   realDirFiles,
		now:        time.Now,
	}
}

// Run detects the role and runs the appropriate checks.
func (d *Doctor) Run(probeURL string) []Check {
	if _, err := os.Stat(filepath.Join(d.dir, "config.yaml")); err == nil {
		return d.runEU()
	}
	return d.runRU(probeURL)
}

func (d *Doctor) runRU(probeURL string) []Check {
	var cs []Check
	if d.unitActive("rdda-singbox") {
		cs = append(cs, Check{"units", PASS, "rdda-singbox active", ""})
	} else {
		cs = append(cs, Check{"units", FAIL, "rdda-singbox not active", "systemctl status rdda-singbox; journalctl -u rdda-singbox"})
	}

	sb, sbErr := os.ReadFile(filepath.Join(d.dir, "singbox.json"))

	switch host, port, ok := realityDestFromConfig(sb); {
	case sbErr != nil:
		cs = append(cs, Check{"REALITY dest", WARN, "no singbox.json to read the dest from", "render + install the RU config first"})
	case !ok:
		cs = append(cs, Check{"REALITY dest", WARN, "no REALITY inbound in singbox.json", ""})
	default:
		if err := d.dialDest(host, port); err != nil {
			cs = append(cs, Check{"REALITY dest", FAIL, fmt.Sprintf("%s:%d unreachable: %v", host, port, err), "pick a --client-sni reachable from this RU node, then re-render"})
		} else {
			cs = append(cs, Check{"REALITY dest", PASS, fmt.Sprintf("%s:%d reachable (TLS 1.3)", host, port), ""})
		}
	}

	from, token := readPullEnv(d.dir)
	if from == "" {
		cs = append(cs, Check{"control channel", WARN, "pull not configured (/etc/rdda/pull.env)", ""})
	} else {
		code, body, _, err := d.httpProbe(withToken(from, token), token)
		switch {
		case err != nil:
			cs = append(cs, Check{"control channel", FAIL, fmt.Sprintf("%s: %v", from, err), "check Cloudflare/cloudflared and the EU sub server"})
		case code != 200:
			cs = append(cs, Check{"control channel", FAIL, fmt.Sprintf("%s → %d", from, code), "token mismatch or sub-server error"})
		case !isConfigJSON(body):
			cs = append(cs, Check{"control channel", FAIL, fmt.Sprintf("%s → 200 but not the RU config", from), "hostname not routed to the tunnel (stale/conflicting DNS record?) — it's serving a wrong origin"})
		default:
			cs = append(cs, Check{"control channel", PASS, fmt.Sprintf("%s → 200 (config)", from), ""})
		}
	}

	cs = append(cs, d.lastPullCheck())
	cs = append(cs, d.permsCheck("singbox.json", "pull.env"))

	switch {
	case sbErr != nil:
		cs = append(cs, Check{"end-to-end egress", WARN, "no singbox.json", ""})
	default:
		ok, err := d.egress(sb, probeURL)
		switch {
		case err != nil:
			cs = append(cs, Check{"end-to-end egress", WARN, "inconclusive: " + err.Error(), "needs sing-box on PATH; run the probe manually to debug"})
		case ok:
			cs = append(cs, Check{"end-to-end egress", PASS, "fetched " + probeURL + " via the tunnel", ""})
		default:
			cs = append(cs, Check{"end-to-end egress", FAIL, "could not fetch " + probeURL + " via the tunnel", "the RU→EU tunnel or the EU exit is down"})
		}
	}
	return cs
}

func (d *Doctor) lastPullCheck() Check {
	fi, err := os.Stat(filepath.Join(d.dir, "singbox.json"))
	if err != nil {
		return Check{"last pull", WARN, "no singbox.json", ""}
	}
	age := d.now().Sub(fi.ModTime()).Round(time.Second)
	if age > 30*time.Minute {
		return Check{"last pull", WARN, fmt.Sprintf("%s ago", age), "is rdda-pull.timer running?"}
	}
	return Check{"last pull", PASS, fmt.Sprintf("%s ago", age), ""}
}

func (d *Doctor) runEU() []Check {
	var cs []Check

	var inactive []string
	for _, u := range []string{"rdda-singbox", "rdda-sub", "cloudflared"} {
		if !d.unitActive(u) {
			inactive = append(inactive, u)
		}
	}
	if len(inactive) == 0 {
		cs = append(cs, Check{"units", PASS, "rdda-singbox, rdda-sub, cloudflared active", ""})
	} else {
		cs = append(cs, Check{"units", FAIL, "not active: " + strings.Join(inactive, ", "), "systemctl status " + strings.Join(inactive, " ")})
	}

	store, err := state.Open(d.dir)
	if err != nil {
		cs = append(cs, Check{"config", FAIL, "cannot open state dir: " + err.Error(), ""})
		return cs
	}
	cfg, cerr := store.LoadConfig()
	if cerr != nil {
		cs = append(cs, Check{"config", FAIL, "config.yaml unreadable: " + cerr.Error(), "fix or restore /etc/rdda/config.yaml"})
		return cs
	}
	clients, _ := store.ListClients()
	_, e1 := singboxconf.RenderRU(cfg, clients)
	_, e2 := singboxconf.RenderEU(cfg)
	if e1 != nil || e2 != nil {
		cs = append(cs, Check{"config", FAIL, fmt.Sprintf("render failed (ru=%v eu=%v)", e1, e2), "check config.yaml fields"})
	} else {
		cs = append(cs, Check{"config", PASS, "render ru/eu OK", ""})
	}

	cs = append(cs, d.permsCheck("singbox.json", "config.yaml", "clients"))

	if cfg.Cloudflare.SubHostname == "" || cfg.PullToken == "" {
		cs = append(cs, Check{"sub endpoint", WARN, "Cloudflare sub endpoint not configured", ""})
	} else {
		u := "https://" + cfg.Cloudflare.SubHostname + "/ru/config"
		code, body, notAfter, err := d.httpProbe(withToken(u, cfg.PullToken), cfg.PullToken)
		switch {
		case err != nil:
			cs = append(cs, Check{"sub endpoint", FAIL, fmt.Sprintf("%s: %v", u, err), "check the Cloudflare tunnel + rdda-sub"})
		case code != 200:
			cs = append(cs, Check{"sub endpoint", FAIL, fmt.Sprintf("%s → %d", u, code), "token mismatch or sub-server error"})
		case !isConfigJSON(body):
			cs = append(cs, Check{"sub endpoint", FAIL, fmt.Sprintf("%s → 200 but not the RU config", u), "sub host isn't routed to this tunnel (stale/conflicting DNS record?) — cloudflared tunnel route dns, delete any old record first"})
		default:
			cs = append(cs, Check{"sub endpoint", PASS, fmt.Sprintf("%s → 200 (config)", u), ""})
		}
		if !notAfter.IsZero() && d.now().Add(30*24*time.Hour).After(notAfter) {
			cs = append(cs, Check{"cert expiry", WARN, "TLS cert expires " + notAfter.Format("2006-01-02"), "renew the public TLS cert"})
		}
	}

	if h, ok, _ := store.LoadRUHealth(); !ok {
		cs = append(cs, Check{"RU beat", WARN, "no health beat received yet", "check rdda-health.timer on the RU node"})
	} else if age := d.now().Sub(h.ReceivedAt).Round(time.Second); age > 20*time.Minute {
		cs = append(cs, Check{"RU beat", WARN, fmt.Sprintf("stale — %s ago", age), "the RU node may be down"})
	} else {
		cs = append(cs, Check{"RU beat", PASS, fmt.Sprintf("%s ago", age), ""})
	}

	if cfg.Cloudflare.TunnelID != "" {
		n, err := d.cfInfo(cfg.Cloudflare.TunnelID)
		switch {
		case err != nil:
			cs = append(cs, Check{"cloudflared", WARN, "could not query: " + err.Error(), ""})
		case n < 1:
			cs = append(cs, Check{"cloudflared", WARN, "no live tunnel connectors", "is cloudflared connected?"})
		default:
			cs = append(cs, Check{"cloudflared", PASS, fmt.Sprintf("%d connector(s)", n), ""})
		}
	}
	return cs
}

// --- helpers ---

// withToken appends the legacy ?token= query — the one-release bridge for a sub
// server not yet reading the Authorization header. The probe also sends the
// header (see realHTTPProbe); this keeps doctor green against an un-updated EU
// node. TODO(next release): drop this and probe with the header only.
func withToken(rawURL, token string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

// isConfigJSON reports whether body is a JSON object — the shape /ru/config
// serves. It's the guard against a hostname that answers 200 from the wrong
// origin (e.g. a stale "Hello World!" page) because its DNS route never reached
// the tunnel: status alone can't tell that apart, the body can.
func isConfigJSON(body []byte) bool {
	t := bytes.TrimSpace(body)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

// readPullEnv parses RDDA_PULL_FROM + RDDA_PULL_TOKEN from <dir>/pull.env.
func readPullEnv(dir string) (from, token string) {
	f, err := os.Open(filepath.Join(dir, "pull.env"))
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		switch k {
		case "RDDA_PULL_FROM":
			from = v
		case "RDDA_PULL_TOKEN":
			token = v
		}
	}
	return from, token
}

// Render formats checks; a non-PASS line shows its hint.
func Render(cs []Check) string {
	glyph := map[Status]string{PASS: "✓", WARN: "⚠", FAIL: "✗"}
	var b strings.Builder
	warns, fails := 0, 0
	for _, c := range cs {
		fmt.Fprintf(&b, "  %s %-22s %s\n", glyph[c.Status], c.Name, c.Detail)
		if c.Status != PASS && c.Hint != "" {
			fmt.Fprintf(&b, "      → %s\n", c.Hint)
		}
		switch c.Status {
		case WARN:
			warns++
		case FAIL:
			fails++
		}
	}
	fmt.Fprintf(&b, "%d warning(s), %d failure(s)\n", warns, fails)
	return b.String()
}

// AnyFail reports whether any check FAILed.
func AnyFail(cs []Check) bool {
	for _, c := range cs {
		if c.Status == FAIL {
			return true
		}
	}
	return false
}
