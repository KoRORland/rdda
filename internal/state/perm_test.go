package state

import (
	"os"
	"path/filepath"
	"testing"
)

// On a host without the rdda user (CI/dev), ChownTree and the client-add chown
// must be graceful no-ops, not errors — the feature is a production hygiene step.
func TestChownTree_NoServiceUserIsNoop(t *testing.T) {
	if _, _, ok := lookupServiceUser(); ok {
		t.Skip("rdda user exists on this host; no-op path not exercised")
	}
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddClient("granny"); err != nil {
		t.Fatalf("AddClient must succeed even when the rdda user is absent: %v", err)
	}
	if err := s.ChownTree(); err != nil {
		t.Fatalf("ChownTree must be a no-op (nil) without the rdda user, got %v", err)
	}
	// The client file is still written and readable by the current user.
	if _, err := os.Stat(filepath.Join(dir, "clients", "granny.json")); err != nil {
		t.Fatalf("client file not written: %v", err)
	}
}
