// Package subserver serves per-client subscription bodies over HTTP (EU node).
package subserver

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/subscription"
	"github.com/KoRORland/rdda/internal/xrayconf"
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
	mux.HandleFunc("/ru/config", func(w http.ResponseWriter, r *http.Request) {
		cfg, err := store.LoadConfig()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if cfg.PullToken == "" {
			http.NotFound(w, r) // pull-sync not enabled: do not advertise the endpoint
			return
		}
		token := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.PullToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clients, err := store.ListClients()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		b, err := xrayconf.RenderRU(cfg, clients)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	return mux
}
