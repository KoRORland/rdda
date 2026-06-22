// Package subserver serves per-client subscription bodies over HTTP (EU node).
package subserver

import (
	"net/http"
	"strings"

	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/subscription"
)

// Handler serves GET /sub/<token>.
func Handler(store *state.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sub/", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/sub/")
		if token == "" {
			http.NotFound(w, r)
			return
		}
		c, ok, err := store.ClientByToken(token)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		cfg, err := store.LoadConfig()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(subscription.Build(cfg, c)))
	})
	return mux
}
