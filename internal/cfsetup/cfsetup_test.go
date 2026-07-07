package cfsetup

import (
	"errors"
	"testing"
)

func TestParseCreate(t *testing.T) {
	out := `Tunnel credentials written to /root/.cloudflared/6ff42ae2-765d-4adf-8112-31c55c1551ef.json. Keep this file secret.

Created tunnel rdda with id 6ff42ae2-765d-4adf-8112-31c55c1551ef`
	id, creds, err := ParseCreate(out)
	if err != nil {
		t.Fatal(err)
	}
	if id != "6ff42ae2-765d-4adf-8112-31c55c1551ef" {
		t.Errorf("id = %q", id)
	}
	if creds != "/root/.cloudflared/6ff42ae2-765d-4adf-8112-31c55c1551ef.json" {
		t.Errorf("creds = %q", creds)
	}
}

func TestParseCreate_NoID(t *testing.T) {
	if _, _, err := ParseCreate("some unrelated error"); err == nil {
		t.Error("missing id must error")
	}
}

func TestFindTunnelID(t *testing.T) {
	list := `You can obtain more detailed information for each tunnel with ` + "`cloudflared tunnel info <name/uuid>`" + `.
ID                                   NAME   CREATED              CONNECTIONS
6ff42ae2-765d-4adf-8112-31c55c1551ef rdda   2026-07-01T00:00:00Z 2xLHR
aaaaaaaa-0000-0000-0000-000000000000 other  2026-07-02T00:00:00Z`
	id, err := FindTunnelID(list, "rdda")
	if err != nil {
		t.Fatal(err)
	}
	if id != "6ff42ae2-765d-4adf-8112-31c55c1551ef" {
		t.Errorf("id = %q", id)
	}
	if _, err := FindTunnelID(list, "nope"); err == nil {
		t.Error("absent name must error")
	}
}

func TestClassifyRouteDNS(t *testing.T) {
	ok := ClassifyRouteDNS("2026-07-01 INF Added CNAME sub.example.com which will route to this tunnel tunnelID=6ff4", nil)
	if !ok.OK || ok.Conflict {
		t.Errorf("success misclassified: %+v", ok)
	}

	// The silent-no-op trap: an existing record must be a Conflict, not OK.
	conflict := ClassifyRouteDNS("", errors.New("Failed to add route: code: 1003, reason: ... An A, AAAA, or CNAME record with that host already exists."))
	if conflict.OK || !conflict.Conflict {
		t.Errorf("conflict misclassified: %+v", conflict)
	}

	// No confirmation, no error → unverified (must NOT be OK).
	unk := ClassifyRouteDNS("", nil)
	if unk.OK || unk.Conflict {
		t.Errorf("empty output must be unverified, got %+v", unk)
	}
}
