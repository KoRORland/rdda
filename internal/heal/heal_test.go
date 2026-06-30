package heal

import (
	"errors"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/health"
)

// fakeRunner reports the given units as failed; units in restartErr fail to restart.
// It records every systemctl invocation into calls.
func fakeRunner(failed map[string]bool, restartErr map[string]bool, calls *[]string) health.Runner {
	return func(name string, args ...string) (string, error) {
		*calls = append(*calls, strings.Join(append([]string{name}, args...), " "))
		if len(args) == 2 && args[0] == "is-failed" {
			if failed[args[1]] {
				return "failed", nil
			}
			return "active", errors.New("not failed") // is-failed exits non-zero when not failed
		}
		if len(args) == 2 && args[0] == "restart" && restartErr[args[1]] {
			return "", errors.New("restart boom")
		}
		return "", nil
	}
}

func TestRunHealsFailedUnit(t *testing.T) {
	var calls []string
	h := &Healer{
		run:   fakeRunner(map[string]bool{"rdda-singbox": true}, nil, &calls),
		units: []string{"rdda-singbox", "rdda-sub"},
	}
	healed, err := h.Run()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(healed) != 1 || healed[0] != "rdda-singbox" {
		t.Fatalf("expected rdda-singbox healed, got %v", healed)
	}
	joined := strings.Join(calls, "|")
	if !strings.Contains(joined, "systemctl reset-failed rdda-singbox") ||
		!strings.Contains(joined, "systemctl restart rdda-singbox") {
		t.Fatalf("expected reset-failed+restart, calls: %v", calls)
	}
	if strings.Contains(joined, "restart rdda-sub") {
		t.Fatal("active unit must not be restarted")
	}
}

func TestRunSkipsActiveAndAbsentUnits(t *testing.T) {
	var calls []string
	h := &Healer{
		run:   fakeRunner(map[string]bool{}, nil, &calls), // nothing failed
		units: []string{"rdda-sub", "cloudflared"},
	}
	healed, err := h.Run()
	if err != nil || len(healed) != 0 {
		t.Fatalf("expected no healing, got %v %v", healed, err)
	}
	for _, c := range calls {
		if strings.Contains(c, "restart") || strings.Contains(c, "reset-failed") {
			t.Fatalf("no unit should be touched: %v", calls)
		}
	}
}

func TestRunRestartErrorSurfacesButLoopContinues(t *testing.T) {
	var calls []string
	h := &Healer{
		run: fakeRunner(
			map[string]bool{"rdda-singbox": true, "rdda-sub": true},
			map[string]bool{"rdda-singbox": true}, // singbox restart fails
			&calls),
		units: []string{"rdda-singbox", "rdda-sub"},
	}
	healed, err := h.Run()
	if err == nil {
		t.Fatal("expected the restart error to surface")
	}
	if len(healed) != 1 || healed[0] != "rdda-sub" {
		t.Fatalf("loop must continue and heal rdda-sub, got %v", healed)
	}
}

func TestNewUsesRealDefaults(t *testing.T) {
	h := New()
	if h.run == nil || len(h.units) == 0 {
		t.Fatal("New must wire a runner and the default unit list")
	}
	want := map[string]bool{"rdda-singbox": true, "rdda-sub": true, "cloudflared": true, "rdda-nfqws": true}
	for _, u := range h.units {
		delete(want, u)
	}
	if len(want) != 0 {
		t.Fatalf("default units missing: %v", want)
	}
	_ = health.DefaultRunner // ensure import is used
}
