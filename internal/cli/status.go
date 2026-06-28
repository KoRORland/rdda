package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/KoRORland/rdda/internal/health"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

const staleAfter = 20 * time.Minute

type statusView struct {
	role     string // "EU" or "RU"
	units    map[string]string
	clients  int
	haveBeat bool
	beat     state.RUHealth
	beatAge  time.Duration
	destAddr string
	destOK   bool
	havePull bool
	pullAge  time.Duration
}

func unitOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func renderStatus(v statusView) string {
	if v.role == "EU" {
		s := "RDDA status — EU (controller)\n"
		s += fmt.Sprintf("  rdda-singbox  %s\n", unitOrUnknown(v.units["rdda-singbox"]))
		s += fmt.Sprintf("  rdda-sub      %s\n", unitOrUnknown(v.units["rdda-sub"]))
		s += fmt.Sprintf("  cloudflared   %s\n", unitOrUnknown(v.units["cloudflared"]))
		s += fmt.Sprintf("  clients       %d\n", v.clients)
		switch {
		case !v.haveBeat:
			s += "  RU node       no beat yet\n"
		case v.beatAge > staleAfter:
			s += fmt.Sprintf("  RU node       ⚠ STALE — no beat in %s\n", v.beatAge.Round(time.Second))
		default:
			sb := "inactive"
			if v.beat.SingboxActive {
				sb = "active"
			}
			s += fmt.Sprintf("  RU node       ✓ last beat %s ago — singbox %s (sb %s / rdda %s)\n",
				v.beatAge.Round(time.Second), sb, v.beat.SingboxVersion, v.beat.RDDAVersion)
		}
		return s
	}
	s := "RDDA status — RU (entry)\n"
	s += fmt.Sprintf("  rdda-singbox  %s\n", unitOrUnknown(v.units["rdda-singbox"]))
	s += fmt.Sprintf("  rdda-nfqws    %s\n", unitOrUnknown(v.units["rdda-nfqws"]))
	if v.destAddr != "" {
		if v.destOK {
			s += fmt.Sprintf("  REALITY dest  %s reachable (TLS 1.3)\n", v.destAddr)
		} else {
			s += fmt.Sprintf("  REALITY dest  %s UNREACHABLE\n", v.destAddr)
		}
	}
	if v.havePull {
		s += fmt.Sprintf("  last pull     %s ago\n", v.pullAge.Round(time.Second))
	}
	return s
}

func gatherStatus(dir string, store *state.Store, run health.Runner) statusView {
	v := statusView{units: map[string]string{}}
	unit := func(u string) { out, _ := run("systemctl", "is-active", u); v.units[u] = out }

	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err == nil {
		v.role = "EU"
		unit("rdda-singbox")
		unit("rdda-sub")
		unit("cloudflared")
		if cs, err := store.ListClients(); err == nil {
			v.clients = len(cs)
		}
		if h, ok, _ := store.LoadRUHealth(); ok {
			v.haveBeat, v.beat, v.beatAge = true, h, time.Since(h.ReceivedAt)
		}
		return v
	}

	v.role = "RU"
	unit("rdda-singbox")
	unit("rdda-nfqws")
	cfgPath := filepath.Join(dir, "singbox.json")
	if b, err := os.ReadFile(cfgPath); err == nil {
		if dest, ok := extractRealityDest(b); ok {
			v.destAddr = net.JoinHostPort(dest.host, strconv.Itoa(dest.port))
			v.destOK = dialReality(dest) == nil
		}
	}
	if fi, err := os.Stat(cfgPath); err == nil {
		v.havePull, v.pullAge = true, time.Since(fi.ModTime())
	}
	return v
}

func newStatusCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show RDDA node health (EU: incl. the RU node's last beat; RU: local)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := state.Open(*dir)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), renderStatus(gatherStatus(*dir, store, health.DefaultRunner)))
			return nil
		},
	}
}
