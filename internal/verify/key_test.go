package verify

import "testing"

// The committed minisign.pub is a placeholder until a maintainer embeds a real
// key. Maintainer must fail closed on it, so the self-update path refuses to
// install rather than silently skipping signature verification.
func TestMaintainer_PlaceholderFailsClosed(t *testing.T) {
	if _, err := Maintainer(); err == nil {
		t.Fatal("Maintainer must return an error while minisign.pub is the placeholder")
	}
}
