package pull

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_WritesAtomicallyAndReloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"inbounds":[]}`))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "singbox.json")
	reloaded := false
	err := Run(Options{URL: srv.URL + "/ru/config", Token: "tok", Dest: dest,
		Reload: func() error { reloaded = true; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != `{"inbounds":[]}` {
		t.Fatalf("dest content = %q", string(b))
	}
	if !reloaded {
		t.Fatal("Reload was not called")
	}
}

func TestRun_FetchFailureLeavesDestUntouched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "singbox.json")
	if err := os.WriteFile(dest, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{URL: srv.URL + "/ru/config", Token: "wrong", Dest: dest,
		Reload: func() error { t.Fatal("Reload must not run on fetch failure"); return nil }})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "OLD" {
		t.Fatalf("dest must be untouched on failure, got %q", string(b))
	}
}

func TestRun_RejectsNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "singbox.json")
	if err := Run(Options{URL: srv.URL, Token: "x", Dest: dest}); err == nil {
		t.Fatal("expected error on non-JSON body")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("dest must not be created on invalid body")
	}
}
