package subserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func TestRUHealthEndpoint(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.Open(dir)
	if err := store.SaveConfig(state.Config{RUHost: "ru", PullToken: "secret-tok"}); err != nil {
		t.Fatal(err)
	}
	h := Handler(store)
	body := `{"singbox_active":true,"singbox_version":"1.13.14"}`

	// valid POST
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ru/health?token=secret-tok", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid beat: got %d", rec.Code)
	}
	got, ok, _ := store.LoadRUHealth()
	if !ok || !got.SingboxActive || got.ReceivedAt.IsZero() {
		t.Fatalf("beat not stored: %+v ok=%v", got, ok)
	}

	// bad token
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ru/health?token=wrong", strings.NewReader(body)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: got %d", rec.Code)
	}

	// wrong method
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ru/health?token=secret-tok", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: got %d", rec.Code)
	}
}
