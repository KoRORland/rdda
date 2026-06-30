package alert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KoRORland/rdda/internal/state"
)

func newTestEngine(t *testing.T) (*Engine, *[]string) {
	t.Helper()
	var sent []string
	e := &Engine{
		dir: t.TempDir(),
		cfg: state.Config{Alert: state.Alert{Enabled: true, Email: "ops@x", Command: "msmtp", CertWarnDays: 14}},
		beatAge:      func() (time.Duration, bool) { return time.Minute, true },
		unitActive:   func(string) bool { return true },
		certNotAfter: func() (time.Time, error) { return time.Now().Add(365 * 24 * time.Hour), nil },
		now:          time.Now,
	}
	e.send = func(subject, _ string) error { sent = append(sent, subject); return nil }
	return e, &sent
}

func TestEvaluateHealthy(t *testing.T) {
	e, _ := newTestEngine(t)
	if cs := e.evaluate(); len(cs) != 0 {
		t.Fatalf("healthy node has no conditions: %v", cs)
	}
}

func TestEvaluateConditions(t *testing.T) {
	e, _ := newTestEngine(t)
	e.beatAge = func() (time.Duration, bool) { return 25 * time.Minute, true }
	e.unitActive = func(u string) bool { return u != "rdda-sub" }
	e.certNotAfter = func() (time.Time, error) { return time.Now().Add(3 * 24 * time.Hour), nil }
	keys := map[string]bool{}
	for _, c := range e.evaluate() {
		keys[c.Key] = true
	}
	for _, want := range []string{"ru-down", "unit-rdda-sub", "cert-expiry"} {
		if !keys[want] {
			t.Fatalf("missing %s: %v", want, keys)
		}
	}
	if keys["unit-rdda-singbox"] {
		t.Fatal("singbox is up; must not fire")
	}
}

func TestEvaluateCertProbeErrorNoAlert(t *testing.T) {
	e, _ := newTestEngine(t)
	e.certNotAfter = func() (time.Time, error) { return time.Time{}, os.ErrDeadlineExceeded }
	for _, c := range e.evaluate() {
		if c.Key == "cert-expiry" {
			t.Fatal("cert probe error must not alert")
		}
	}
}

func TestRunTransitionsAndDedup(t *testing.T) {
	e, sent := newTestEngine(t)
	if f, r, err := e.Run(); err != nil || len(f) != 0 || len(r) != 0 {
		t.Fatalf("healthy: %v %v %v", f, r, err)
	}
	if len(*sent) != 0 {
		t.Fatalf("no email expected: %v", *sent)
	}
	e.beatAge = func() (time.Duration, bool) { return 0, false } // RU down
	if f, _, _ := e.Run(); len(f) != 1 || f[0] != "ru-down" {
		t.Fatalf("expected ru-down fire: %v", f)
	}
	*sent = nil
	_, _, _ = e.Run() // still down
	if len(*sent) != 0 {
		t.Fatalf("steady must not re-alert: %v", *sent)
	}
	e.beatAge = func() (time.Duration, bool) { return time.Minute, true } // recovered
	if _, r, _ := e.Run(); len(r) != 1 || r[0] != "ru-down" {
		t.Fatalf("expected ru-down resolved: %v", r)
	}
}

func TestRunFailedSendRetries(t *testing.T) {
	e, _ := newTestEngine(t)
	e.beatAge = func() (time.Duration, bool) { return 0, false }
	e.send = func(string, string) error { return os.ErrPermission }
	if _, _, err := e.Run(); err == nil {
		t.Fatal("send error should surface")
	}
	var sent []string
	e.send = func(s, _ string) error { sent = append(sent, s); return nil }
	if f, _, _ := e.Run(); len(f) != 1 || f[0] != "ru-down" {
		t.Fatalf("a failed send must re-fire next run: %v", f)
	}
}

func TestRunDisabledNoop(t *testing.T) {
	e, sent := newTestEngine(t)
	e.cfg.Alert.Enabled = false
	e.beatAge = func() (time.Duration, bool) { return 0, false }
	_, _, _ = e.Run()
	if len(*sent) != 0 {
		t.Fatal("disabled alerting must not send")
	}
}

func TestSendEmailViaFakeCommand(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec.txt")
	script := filepath.Join(dir, "fake-msmtp")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'to=%s\\n' \"$1\" > "+rec+"\ncat >> "+rec+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sendEmail(script, "ops@example.com", "Subj", "Body"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(rec)
	s := string(b)
	if !strings.Contains(s, "to=ops@example.com") || !strings.Contains(s, "Subject: Subj") || !strings.Contains(s, "Body") {
		t.Fatalf("recorded:\n%s", s)
	}
}
