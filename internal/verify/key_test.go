package verify

import "testing"

// A real release-signing key is embedded, so Maintainer must parse it and expose
// a usable public key. (While minisign.pub holds the PLACEHOLDER-UNSIGNED marker
// this instead returns an error, so the self-update path fails closed rather than
// skipping verification — see Maintainer.)
func TestMaintainer_ReturnsEmbeddedKey(t *testing.T) {
	pk, err := Maintainer()
	if err != nil {
		t.Fatalf("Maintainer must return the embedded key: %v", err)
	}
	if pk == nil || len(pk.key) == 0 {
		t.Fatal("Maintainer returned an empty key")
	}
}
