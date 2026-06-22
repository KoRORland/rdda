package subserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KoRORland/rdda/internal/state"
)

func TestSubHandler(t *testing.T) {
	s, _ := state.Open(t.TempDir())
	_ = s.SaveConfig(state.Config{
		RUHost: "ru.example.net", RUPort: 443, ClientPath: "/cl",
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

	bad, _ := http.Get(srv.URL + "/sub/nope")
	if bad.StatusCode != 404 {
		t.Fatalf("unknown token status=%d want 404", bad.StatusCode)
	}
}
