package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPullCommand_WritesDest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"inbounds":[]}`))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "singbox.json")
	root := newRoot()
	root.SetArgs([]string{"pull", "--from", srv.URL + "/ru/config", "--token", "tok", "--dest", dest, "--reload-cmd", "true"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != `{"inbounds":[]}` {
		t.Fatalf("dest = %q", string(b))
	}
}
