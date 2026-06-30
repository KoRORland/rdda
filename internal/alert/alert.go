// Package alert evaluates EU-side health conditions and emails the operator on
// state transitions (good→bad and bad→good). It is fail-soft and EU-only.
package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/health"
	"github.com/KoRORland/rdda/internal/state"
)

const staleAfter = 20 * time.Minute

// Condition is one active alert condition.
type Condition struct{ Key, Message string }

// Engine evaluates conditions and sends emails. The fields are seams (real by
// default via New; faked in tests).
type Engine struct {
	dir          string
	cfg          state.Config
	beatAge      func() (age time.Duration, haveBeat bool)
	unitActive   func(unit string) bool
	certNotAfter func() (time.Time, error)
	now          func() time.Time
	send         func(subject, body string) error
}

// New wires an Engine to real implementations and applies config defaults.
func New(dir string, cfg state.Config) *Engine {
	if cfg.Alert.Command == "" {
		cfg.Alert.Command = "msmtp"
	}
	if cfg.Alert.CertWarnDays <= 0 {
		cfg.Alert.CertWarnDays = 14
	}
	e := &Engine{dir: dir, cfg: cfg, now: time.Now}
	e.beatAge = func() (time.Duration, bool) {
		s, err := state.Open(dir)
		if err != nil {
			return 0, false
		}
		h, ok, err := s.LoadRUHealth()
		if err != nil || !ok {
			return 0, false
		}
		return e.now().Sub(h.ReceivedAt), true
	}
	e.unitActive = func(u string) bool { return health.UnitActive(health.DefaultRunner, u) }
	e.certNotAfter = func() (time.Time, error) { return certExpiry(cfg) }
	e.send = func(subject, body string) error { return sendEmail(cfg.Alert.Command, cfg.Alert.Email, subject, body) }
	return e
}

func (e *Engine) evaluate() []Condition {
	var cs []Condition
	if age, ok := e.beatAge(); !ok {
		cs = append(cs, Condition{"ru-down", "RU node down — no health beat received yet"})
	} else if age > staleAfter {
		cs = append(cs, Condition{"ru-down", fmt.Sprintf("RU node down — no health beat in %s", age.Round(time.Second))})
	}
	for _, u := range []string{"rdda-singbox", "rdda-sub", "cloudflared"} {
		if !e.unitActive(u) {
			cs = append(cs, Condition{"unit-" + u, "EU unit " + u + " is not active"})
		}
	}
	if na, err := e.certNotAfter(); err == nil && !na.IsZero() {
		if e.now().Add(time.Duration(e.cfg.Alert.CertWarnDays) * 24 * time.Hour).After(na) {
			cs = append(cs, Condition{"cert-expiry", fmt.Sprintf("TLS cert expires %s (< %d days)", na.Format("2006-01-02"), e.cfg.Alert.CertWarnDays)})
		}
	}
	return cs
}

func (e *Engine) statePath() string { return filepath.Join(e.dir, "alert-state.json") }

func (e *Engine) loadFiring() map[string]string {
	b, err := os.ReadFile(e.statePath())
	if err != nil {
		return map[string]string{}
	}
	var doc struct {
		Firing map[string]string `json:"firing"`
	}
	if json.Unmarshal(b, &doc) != nil || doc.Firing == nil {
		return map[string]string{}
	}
	return doc.Firing
}

func (e *Engine) saveFiring(firing map[string]string) error {
	b, err := json.MarshalIndent(struct {
		Firing map[string]string `json:"firing"`
	}{firing}, "", "  ")
	if err != nil {
		return err
	}
	tmp := e.statePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, e.statePath())
}

// Run evaluates conditions, emails transitions, and persists the firing set.
func (e *Engine) Run() (fired, resolved []string, err error) {
	if !e.cfg.Alert.Enabled {
		return nil, nil, nil
	}
	last := e.loadFiring()
	current := map[string]string{}
	for _, c := range e.evaluate() {
		current[c.Key] = c.Message
	}
	newSet := map[string]string{}
	var firstErr error

	for _, k := range sortedKeys(current) {
		if _, was := last[k]; was {
			newSet[k] = current[k] // still firing; already notified
			continue
		}
		if serr := e.send("[RDDA ALERT] "+current[k], current[k]); serr != nil {
			if firstErr == nil {
				firstErr = serr
			}
			continue // not persisted ⇒ re-fires next run
		}
		newSet[k] = current[k]
		fired = append(fired, k)
	}
	for _, k := range sortedKeys(last) {
		if _, still := current[k]; still {
			continue
		}
		if serr := e.send("[RDDA RESOLVED] "+last[k], last[k]); serr != nil {
			if firstErr == nil {
				firstErr = serr
			}
			newSet[k] = last[k] // keep ⇒ retry the resolved notice next run
			continue
		}
		resolved = append(resolved, k)
	}

	if serr := e.saveFiring(newSet); serr != nil && firstErr == nil {
		firstErr = serr
	}
	return fired, resolved, firstErr
}

// SendTest sends one test email so the operator can verify msmtp setup.
func (e *Engine) SendTest() error {
	if e.cfg.Alert.Email == "" {
		return fmt.Errorf("alert.email is not set")
	}
	return e.send("[RDDA TEST] alert delivery works",
		"This is a test alert from rdda. If you received it, your msmtp config is working.")
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// sendEmail pipes a minimal RFC-822 message to `<command> <to>` (msmtp by default).
func sendEmail(command, to, subject, body string) error {
	if to == "" {
		return fmt.Errorf("alert.email is not set")
	}
	host, _ := os.Hostname()
	msg := fmt.Sprintf("From: rdda@%s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n%s\r\n",
		host, to, subject, time.Now().Format(time.RFC1123Z), body)
	cmd := exec.Command(command, to)
	cmd.Stdin = bytes.NewReader([]byte(msg))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", command, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// certExpiry GETs the public sub endpoint and returns its leaf cert NotAfter.
func certExpiry(cfg state.Config) (time.Time, error) {
	if cfg.Cloudflare.SubHostname == "" {
		return time.Time{}, fmt.Errorf("no sub hostname configured")
	}
	u := "https://" + cfg.Cloudflare.SubHostname + "/ru/config"
	if cfg.PullToken != "" {
		if uu, err := url.Parse(u); err == nil {
			q := uu.Query()
			q.Set("token", cfg.PullToken)
			uu.RawQuery = q.Encode()
			u = uu.String()
		}
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Get(u)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.TLS == nil {
		return time.Time{}, fmt.Errorf("no TLS on sub endpoint")
	}
	for _, c := range resp.TLS.PeerCertificates {
		if !c.IsCA {
			return c.NotAfter, nil
		}
	}
	return time.Time{}, fmt.Errorf("no leaf certificate")
}
