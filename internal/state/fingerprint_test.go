package state

import "testing"

func TestRandomClientFingerprint_InPool(t *testing.T) {
	// Every draw must be a supported pool value, and over many draws we should
	// see more than one distinct fingerprint (it's actually randomized).
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		fp := RandomClientFingerprint()
		if !IsClientFingerprint(fp) {
			t.Fatalf("drew unsupported fingerprint %q", fp)
		}
		seen[fp] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected variety across the pool, only saw %v", seen)
	}
}

func TestAddClient_RandomFingerprintPersisted(t *testing.T) {
	s, _ := Open(t.TempDir())
	c, err := s.AddClient("granny")
	if err != nil {
		t.Fatal(err)
	}
	if !IsClientFingerprint(c.Fingerprint) {
		t.Fatalf("new client got no pool fingerprint: %q", c.Fingerprint)
	}
	// It must be persisted (stable across reloads, not re-randomized).
	list, _ := s.ListClients()
	if len(list) != 1 || list[0].Fingerprint != c.Fingerprint {
		t.Fatalf("fingerprint not persisted: added %q, loaded %+v", c.Fingerprint, list)
	}
}

func TestAddClientWithFingerprint_PinAndValidate(t *testing.T) {
	s, _ := Open(t.TempDir())
	c, err := s.AddClientWithFingerprint("edgey", "edge")
	if err != nil {
		t.Fatal(err)
	}
	if c.Fingerprint != "edge" {
		t.Fatalf("pin ignored: %q", c.Fingerprint)
	}
	if _, err := s.AddClientWithFingerprint("bad", "chrome"); err == nil {
		t.Fatal("an out-of-pool fingerprint must be rejected")
	}
}

func TestFingerprintOr_Fallback(t *testing.T) {
	if got := (Client{}).FingerprintOr("firefox"); got != "firefox" {
		t.Errorf("empty client fingerprint should fall back, got %q", got)
	}
	if got := (Client{Fingerprint: "safari"}).FingerprintOr("firefox"); got != "safari" {
		t.Errorf("set fingerprint should win, got %q", got)
	}
}
