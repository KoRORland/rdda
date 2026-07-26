package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KoRORland/rdda/internal/dest"
	"github.com/KoRORland/rdda/internal/state"
)

// fakeProber returns canned results keyed by host; unknown hosts fail.
type fakeProber map[string]dest.Result

func (f fakeProber) Probe(_ context.Context, host string) dest.Result {
	if r, ok := f[host]; ok {
		r.Host = host
		return r
	}
	return dest.Result{Host: host, Reason: "unreachable/timeout"}
}

// allPass makes every curated candidate pass, with the given host fastest so it
// is the deterministic pick.
func allPass(fastest string) fakeProber {
	f := fakeProber{}
	for i, h := range dest.Candidates() {
		f[h] = dest.Result{OK: true, RTT: time.Duration(i+2) * 10 * time.Millisecond}
	}
	f[fastest] = dest.Result{OK: true, RTT: 1 * time.Millisecond}
	return f
}

func newStore(t *testing.T) *state.Store {
	t.Helper()
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(state.Config{
		RUHost: "ru.example", RUPort: 443, EUHost: "eu.example", EUPort: 443,
		ClientReality: state.Reality{ServerName: "old-client.example", Target: "old-client.example:443", PrivateKey: "CK", PublicKey: "CPUB", ShortIDs: []string{"aa"}},
		TunnelReality: state.Reality{ServerName: "old-tunnel.example", Target: "old-tunnel.example:443", PrivateKey: "TK", PublicKey: "TPUB", ShortIDs: []string{"bb"}},
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunDiscover_WriteClientDestPreservesKeys(t *testing.T) {
	s := newStore(t)
	pick := dest.Candidates()[3]
	var out bytes.Buffer
	if err := runDiscover(context.Background(), &out, s, discoverOpts{which: "client", write: true}, allPass(pick)); err != nil {
		t.Fatal(err)
	}

	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientReality.ServerName != pick || cfg.ClientReality.Target != pick+":443" {
		t.Errorf("client dest not written: server_name=%q target=%q", cfg.ClientReality.ServerName, cfg.ClientReality.Target)
	}
	// Keys/short-ids preserved; only the borrowed host changes.
	if cfg.ClientReality.PrivateKey != "CK" || cfg.ClientReality.PublicKey != "CPUB" || cfg.ClientReality.ShortIDs[0] != "aa" {
		t.Errorf("client REALITY keys not preserved: %+v", cfg.ClientReality)
	}
	// --which client must not touch the tunnel hop.
	if cfg.TunnelReality.ServerName != "old-tunnel.example" {
		t.Errorf("tunnel dest changed under --which client: %q", cfg.TunnelReality.ServerName)
	}
	if !strings.Contains(out.String(), "pick: "+pick) {
		t.Errorf("pick not surfaced in output:\n%s", out.String())
	}
}

func TestRunDiscover_ReportOnlyDoesNotWrite(t *testing.T) {
	s := newStore(t)
	var out bytes.Buffer
	if err := runDiscover(context.Background(), &out, s, discoverOpts{which: "client", write: false}, allPass(dest.Candidates()[0])); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.LoadConfig()
	if cfg.ClientReality.ServerName != "old-client.example" {
		t.Errorf("report-only run wrote the dest: %q", cfg.ClientReality.ServerName)
	}
	if !strings.Contains(out.String(), "report only") {
		t.Errorf("report-only hint missing:\n%s", out.String())
	}
}

func TestRunDiscover_AllFailIsError(t *testing.T) {
	s := newStore(t)
	var out bytes.Buffer
	err := runDiscover(context.Background(), &out, s, discoverOpts{which: "client", write: true}, fakeProber{})
	if err == nil {
		t.Fatal("expected error when no candidate validates")
	}
	if !strings.Contains(err.Error(), "no candidate validated") {
		t.Errorf("unexpected error: %v", err)
	}
	cfg, _ := s.LoadConfig()
	if cfg.ClientReality.ServerName != "old-client.example" {
		t.Error("config mutated despite all-fail discovery")
	}
}

func TestRunDiscover_BothSetsBothHops(t *testing.T) {
	s := newStore(t)
	pick := dest.Candidates()[2]
	var out bytes.Buffer
	if err := runDiscover(context.Background(), &out, s, discoverOpts{which: "both", write: true}, allPass(pick)); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.LoadConfig()
	if cfg.ClientReality.ServerName != pick || cfg.TunnelReality.ServerName != pick {
		t.Errorf("both hops not set: client=%q tunnel=%q", cfg.ClientReality.ServerName, cfg.TunnelReality.ServerName)
	}
}

func TestRunDiscover_BadWhichRejected(t *testing.T) {
	s := newStore(t)
	var out bytes.Buffer
	if err := runDiscover(context.Background(), &out, s, discoverOpts{which: "sideways"}, allPass("x")); err == nil {
		t.Fatal("expected error for invalid --which")
	}
}
