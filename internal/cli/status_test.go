package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/KoRORland/rdda/internal/health"
	"github.com/KoRORland/rdda/internal/state"
)

func TestRenderStatusEU_Healthy(t *testing.T) {
	v := statusView{role: "EU",
		units:   map[string]string{"rdda-singbox": "active", "rdda-sub": "active", "cloudflared": "active"},
		clients: 3, haveBeat: true, beatAge: 90 * time.Second,
		beat: state.RUHealth{Report: health.Report{SingboxActive: true, SingboxVersion: "1.13.14", RDDAVersion: "v0.3.0"}}}
	out := renderStatus(v)
	if !strings.Contains(out, "EU (controller)") || !strings.Contains(out, "clients       3") || !strings.Contains(out, "last beat") {
		t.Fatalf("EU healthy render:\n%s", out)
	}
}

func TestRenderStatusEU_Stale(t *testing.T) {
	v := statusView{role: "EU", units: map[string]string{}, haveBeat: true, beatAge: 25 * time.Minute}
	if !strings.Contains(renderStatus(v), "STALE") {
		t.Fatalf("expected STALE:\n%s", renderStatus(v))
	}
}

func TestRenderStatusEU_NoBeat(t *testing.T) {
	v := statusView{role: "EU", units: map[string]string{}}
	if !strings.Contains(renderStatus(v), "no beat yet") {
		t.Fatalf("expected no beat yet:\n%s", renderStatus(v))
	}
}

func TestRenderStatusRU(t *testing.T) {
	v := statusView{role: "RU", units: map[string]string{"rdda-singbox": "active", "rdda-nfqws": "active"},
		destAddr: "addons.mozilla.org:443", destOK: true, havePull: true, pullAge: 2 * time.Minute}
	out := renderStatus(v)
	if !strings.Contains(out, "RU (entry)") || !strings.Contains(out, "addons.mozilla.org:443 reachable") {
		t.Fatalf("RU render:\n%s", out)
	}
}

func TestGatherStatusRoleDetect(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.Open(dir)
	active := func(string, ...string) (string, error) { return "active", nil }
	if gatherStatus(dir, store, active).role != "RU" {
		t.Fatal("expected RU without config.yaml")
	}
	if err := store.SaveConfig(state.Config{RUHost: "ru"}); err != nil {
		t.Fatal(err)
	}
	if gatherStatus(dir, store, active).role != "EU" {
		t.Fatal("expected EU with config.yaml")
	}
}
