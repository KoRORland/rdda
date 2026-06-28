package health

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
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
	var gotToken, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotToken = req.URL.Query().Get("token")
		gotMethod = req.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := Send(srv.Client(), srv.URL, "tok-123", Report{}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotToken != "tok-123" {
		t.Fatalf("method=%s token=%s", gotMethod, gotToken)
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
