// Package heal recovers units stuck in systemd 'failed' state — the gap left by
// Restart=on-failure once a unit exhausts its start-limit. It never touches an
// active unit. Run by rdda-heal.timer (root) on both nodes.
package heal

import (
	"fmt"

	"github.com/KoRORland/rdda/internal/health"
)

// defaultUnits is the fixed candidate list. It is role-agnostic: a unit that is
// active, inactive, or not installed is not 'failed', so it is skipped — EU has
// singbox/sub/cloudflared, RU has singbox/nfqws, and absent units are no-ops.
var defaultUnits = []string{"rdda-singbox", "rdda-sub", "cloudflared", "rdda-nfqws"}

// Healer restarts failed units. run/units are seams (real via New; faked in tests).
type Healer struct {
	run   health.Runner
	units []string
}

// New wires a Healer to real systemctl and the default unit list.
func New() *Healer {
	return &Healer{run: health.DefaultRunner, units: defaultUnits}
}

// Run reset-fails and restarts every candidate unit currently in 'failed' state.
// A restart failure is surfaced in err but does not stop the loop.
func (h *Healer) Run() (healed []string, err error) {
	for _, u := range h.units {
		out, _ := h.run("systemctl", "is-failed", u)
		if out != "failed" {
			continue
		}
		_, _ = h.run("systemctl", "reset-failed", u)
		if _, rerr := h.run("systemctl", "restart", u); rerr != nil {
			if err == nil {
				err = fmt.Errorf("restart %s: %w", u, rerr)
			}
			continue
		}
		healed = append(healed, u)
	}
	return healed, err
}
