package health

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KoRORland/rdda/internal/tunnelprobe"
)

func fakeRunner(states map[string]string) Runner {
	return func(name string, args ...string) (string, error) {
		if name == "systemctl" && len(args) == 2 && args[0] == "is-active" {
			return states[args[1]], nil
		}
		if name == "sing-box" && len(args) == 1 && args[0] == "version" {
			return "sing-box version 1.13.14", nil
		}
		return "", nil
	}
}

func TestGather(t *testing.T) {
	r := Gather(fakeRunner(map[string]string{"rdda-singbox": "active", "rdda-nfqws": "inactive"}), "v0.3.0")
	if !r.SingboxActive || r.NfqwsActive {
		t.Fatalf("unit states wrong: %+v", r)
	}
	if r.SingboxVersion != "1.13.14" || r.RDDAVersion != "v0.3.0" || r.TS.IsZero() {
		t.Fatalf("meta wrong: %+v", r)
	}
}

func TestReachFromResult(t *testing.T) {
	ok := reachFromResult(tunnelprobe.Result{Status: tunnelprobe.OK, Latency: 123 * time.Millisecond, Detail: "123ms"})
	if ok.Status != ReachOK || ok.LatencyMS != 123 || ok.Detail != "123ms" {
		t.Errorf("ok mapping wrong: %+v", ok)
	}
	// A block carries no latency but keeps the failure detail.
	bl := reachFromResult(tunnelprobe.Result{Status: tunnelprobe.Blocked, Detail: "status 502"})
	if bl.Status != ReachBlocked || bl.LatencyMS != 0 || bl.Detail != "status 502" {
		t.Errorf("blocked mapping wrong: %+v", bl)
	}
	un := reachFromResult(tunnelprobe.Result{Status: tunnelprobe.Unknown, Detail: "no sing-box"})
	if un.Status != ReachUnknown {
		t.Errorf("unknown mapping wrong: %+v", un)
	}
}

// The beat serializes the typed sensor so the EU node persists it.
func TestReportCarriesReach(t *testing.T) {
	r := Gather(fakeRunner(map[string]string{"rdda-singbox": "active"}), "v0")
	r.Reach = Reach{Status: ReachBlocked, Detail: "no response"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"reach":{"status":"blocked"`) {
		t.Fatalf("reach not serialized into the beat: %s", b)
	}
}

func TestRandomPadVaries(t *testing.T) {
	a, b := randomPad(), randomPad()
	if a == b {
		t.Fatal("two pads must differ")
	}
	if _, err := base64.StdEncoding.DecodeString(a); err != nil {
		t.Fatalf("pad not base64: %v", err)
	}
}

func TestSend(t *testing.T) {
	var gotHeader, gotQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotHeader = req.Header.Get("Authorization")
		gotQuery = req.URL.Query().Get("token")
		gotMethod = req.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := Send(srv.Client(), srv.URL, "tok-123", Report{}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method=%s", gotMethod)
	}
	// The token travels in the Authorization header ONLY (F-2) — never the URL,
	// where it would leak into logs/Referer/history.
	if gotHeader != "Bearer tok-123" {
		t.Fatalf("Authorization header = %q, want %q", gotHeader, "Bearer tok-123")
	}
	if gotQuery != "" {
		t.Fatalf("token must not ride the URL, got ?token=%q", gotQuery)
	}
}

func TestSendNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if err := Send(srv.Client(), srv.URL, "bad", Report{}); err == nil {
		t.Fatal("expected error on non-2xx")
	}
}
