// Package subserver serves per-client subscription bodies over HTTP (EU node).
package subserver

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/health"
	"github.com/KoRORland/rdda/internal/ratelimit"
	"github.com/KoRORland/rdda/internal/singboxconf"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/subscription"
)

// fail logs the real cause (so a 500 is diagnosable from `journalctl -u
// rdda-sub`) and returns an opaque message to the client. The swallowed error
// here previously made a root-owned client file — unreadable by the rdda service
// user — look like an unexplained 500.
func fail(w http.ResponseWriter, route string, err error) {
	log.Printf("subserver: %s: %v", route, err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// clientIP identifies the caller for rate limiting. Behind cloudflared every
// request arrives from loopback, so the real client is in CF-Connecting-IP
// (set by Cloudflare); fall back to the socket peer when that is absent.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// limited wraps a handler with a per-client-IP token bucket, returning 429 when
// a caller exceeds it — a basic abuse control on the token-checked control
// channel (F-4). Scoped to /ru/* (hit only by the single RU node); /sub/ is left
// unlimited so a real Hiddify client is never throttled.
func limited(l *ratelimit.Limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// controlToken extracts the RU control-channel token, preferring an
// `Authorization: Bearer <token>` header and falling back to the legacy
// `?token=` query string. Query strings leak into access logs / Referer /
// history, so the header is the intended transport (F-2); the query fallback is
// a one-release bridge so an RU node that has not yet updated keeps
// authenticating. TODO(next release): drop the query fallback.
func controlToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

// Handler serves GET /sub/<token>.
func Handler(store *state.Store) http.Handler {
	mux := http.NewServeMux()
	// The control channel is polled by exactly one RU node at multi-minute
	// intervals, so a generous burst never touches legit traffic while still
	// capping abuse: 30 requests burst, ~1/sec sustained, per client IP.
	ctl := ratelimit.New(30, 1)
	mux.HandleFunc("/sub/", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/sub/")
		if token == "" {
			http.NotFound(w, r)
			return
		}
		c, ok, err := store.ClientByToken(token)
		if err != nil {
			fail(w, "/sub ClientByToken", err)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		cfg, err := store.LoadConfig()
		if err != nil {
			fail(w, "/sub LoadConfig", err)
			return
		}
		body, err := subscription.Build(cfg, c)
		if err != nil {
			fail(w, "/sub Build", err)
			return
		}
		// Name the profile so Hiddify doesn't show it as "UNKNOWN": the HTTP header
		// names URL subscriptions, the ImportHeader comment names file/clipboard
		// imports (Hiddify strips the //-lines before JSON-parsing).
		w.Header().Set("Profile-Title", subscription.ProfileTitleHeader())
		w.Header().Set("Profile-Update-Interval", "24")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(subscription.ImportHeader() + body))
	})
	mux.HandleFunc("/ru/config", limited(ctl, func(w http.ResponseWriter, r *http.Request) {
		cfg, err := store.LoadConfig()
		if err != nil {
			fail(w, "/ru/config LoadConfig", err)
			return
		}
		if cfg.PullToken == "" {
			http.NotFound(w, r) // pull-sync not enabled: do not advertise the endpoint
			return
		}
		token := controlToken(r)
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.PullToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clients, err := store.ListClients()
		if err != nil {
			fail(w, "/ru/config ListClients", err) // e.g. a root-owned client file the rdda user can't read
			return
		}
		b, err := singboxconf.RenderRU(cfg, clients)
		if err != nil {
			fail(w, "/ru/config RenderRU", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	mux.HandleFunc("/ru/health", limited(ctl, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cfg, err := store.LoadConfig()
		if err != nil {
			fail(w, "/ru/health LoadConfig", err)
			return
		}
		if cfg.PullToken == "" {
			http.NotFound(w, r) // pull-sync not enabled: do not advertise the endpoint
			return
		}
		token := controlToken(r)
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.PullToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var rep health.Report
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&rep); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := store.SaveRUHealth(state.RUHealth{Report: rep, ReceivedAt: time.Now().UTC()}); err != nil {
			fail(w, "/ru/health SaveRUHealth", err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return mux
}
