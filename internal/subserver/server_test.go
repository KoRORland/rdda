package subserver

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func TestSubHandler(t *testing.T) {
	s, _ := state.Open(t.TempDir())
	_ = s.SaveConfig(state.Config{
		RUHost: "ru.example.net", RUPort: 443, ClientPath: "/cl",
		ProfileTitle:  "Kitchen Wifi", // operator-configured, not the project name
		ClientReality: state.Reality{ServerName: "www.microsoft.com", PublicKey: "cpub"},
	})
	c, _ := s.AddClient("granny")

	srv := httptest.NewServer(Handler(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sub/" + c.Token)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}

	// The response must name the profile so Hiddify doesn't show it as UNKNOWN:
	// a Profile-Title header (for URL subscriptions) and a leading // comment
	// header (for file/clipboard imports).
	wantHdr := "base64:" + base64.StdEncoding.EncodeToString([]byte("Kitchen Wifi"))
	if got := resp.Header.Get("Profile-Title"); got != wantHdr {
		t.Errorf("Profile-Title header = %q, want %q", got, wantHdr)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "// profile-title: Kitchen Wifi") {
		t.Errorf("body must start with the configured // profile-title import header, got: %.40q", body)
	}
	if strings.Contains(string(body), "RDDA") {
		t.Errorf("sub body must not carry the project name, got: %.60q", body)
	}

	bad, _ := http.Get(srv.URL + "/sub/nope")
	if bad.StatusCode != 404 {
		t.Fatalf("unknown token status=%d want 404", bad.StatusCode)
	}
}
