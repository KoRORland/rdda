package subscription

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func cfg() state.Config {
	return state.Config{
		RUHost: "ru.example.net", RUPort: 443, ClientPath: "/cl",
		ClientReality: state.Reality{ServerName: "www.microsoft.com", PublicKey: "cpub", ShortIDs: []string{"0011"}},
	}
}

func TestClientURI(t *testing.T) {
	c := state.Client{Name: "granny", UUID: "uuid-1", ShortID: "abcd1234"}
	uri := ClientURI(cfg(), c)
	if !strings.HasPrefix(uri, "vless://uuid-1@ru.example.net:443?") {
		t.Fatalf("bad prefix: %s", uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	checks := map[string]string{
		"type": "xhttp", "security": "reality", "encryption": "none",
		"pbk": "cpub", "sni": "www.microsoft.com", "sid": "abcd1234", "fp": "chrome", "path": "/cl",
	}
	for k, want := range checks {
		if q.Get(k) != want {
			t.Errorf("query %s=%q want %q", k, q.Get(k), want)
		}
	}
	if u.Fragment != "granny" {
		t.Errorf("fragment=%q want granny", u.Fragment)
	}
}

func TestBuildBase64(t *testing.T) {
	c := state.Client{Name: "granny", UUID: "uuid-1", ShortID: "abcd1234"}
	body := Build(cfg(), c)
	dec, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(dec), "vless://") {
		t.Fatalf("decoded body not a vless URI: %s", dec)
	}
}
