package subserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func newStoreWithConfig(t *testing.T, c state.Config) *state.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRUConfig_ValidTokenReturnsRenderedConfig(t *testing.T) {
	s := newStoreWithConfig(t, state.Config{
		RUHost: "ru.example", RUPort: 443, EUPort: 8443,
		TunnelUUID: "uuid-1", TunnelPath: "/tpath", ClientPath: "/cpath",
		ClientReality: state.Reality{Target: "www.microsoft.com:443", ServerName: "www.microsoft.com", PrivateKey: "k", ShortIDs: []string{"aa"}},
		Cloudflare:    state.Cloudflare{TunnelHostname: "tunnel.example.com"},
		PullToken:     "secret-tok",
	})
	srv := httptest.NewServer(Handler(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ru/config?token=secret-tok")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 || body[0] != '{' {
		t.Fatalf("expected JSON body, got %q", string(body[:min(20, len(body))]))
	}
}

func TestRUConfig_BadTokenIs401(t *testing.T) {
	s := newStoreWithConfig(t, state.Config{RUHost: "ru.example", PullToken: "secret-tok"})
	srv := httptest.NewServer(Handler(s))
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/ru/config?token=wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRUConfig_NoTokenConfiguredIs404(t *testing.T) {
	s := newStoreWithConfig(t, state.Config{RUHost: "ru.example"}) // PullToken empty
	srv := httptest.NewServer(Handler(s))
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/ru/config?token=anything")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func min(a, b int) int { if a < b { return a }; return b }
