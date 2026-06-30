package cli

import (
	"os"
	"strings"
	"testing"
)

type fakeUpdater struct {
	cur, latest  string
	newer        bool
	updFrom, updTo string
}

func (f fakeUpdater) Check() (string, string, bool, error) { return f.cur, f.latest, f.newer, nil }
func (f fakeUpdater) Update() (string, string, error)      { return f.updFrom, f.updTo, nil }
func (f fakeUpdater) Rollback() error                      { return nil }

type fakeHealer struct{ healed []string }

func (f fakeHealer) Run() ([]string, error) { return f.healed, nil }

func TestUpdateCheckReportsNewer(t *testing.T) {
	orig := newUpdater
	t.Cleanup(func() { newUpdater = orig })
	newUpdater = func(string) updater { return fakeUpdater{cur: "v0.2.0", latest: "v0.3.0", newer: true} }
	out := run(t, "update", "--check")
	if !strings.Contains(out, "v0.2.0 installed, v0.3.0 available") {
		t.Fatalf("got %q", out)
	}
}

func TestUpdateCheckUpToDate(t *testing.T) {
	orig := newUpdater
	t.Cleanup(func() { newUpdater = orig })
	newUpdater = func(string) updater { return fakeUpdater{cur: "v0.3.0", latest: "v0.3.0", newer: false} }
	out := run(t, "update", "--check")
	if !strings.Contains(out, "up to date (v0.3.0)") {
		t.Fatalf("got %q", out)
	}
}

func TestUpdateRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	err := runErr(t, "update")
	if err == nil || !strings.Contains(err.Error(), "must run as root") {
		t.Fatalf("expected root error, got %v", err)
	}
}

func TestHealPrintsHealed(t *testing.T) {
	orig := newHealer
	t.Cleanup(func() { newHealer = orig })
	newHealer = func() healer { return fakeHealer{healed: []string{"rdda-singbox"}} }
	out := run(t, "heal")
	if !strings.Contains(out, "HEALED rdda-singbox") {
		t.Fatalf("got %q", out)
	}
}
