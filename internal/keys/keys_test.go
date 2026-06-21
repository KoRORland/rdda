package keys

import (
	"regexp"
	"testing"
)

func TestNewUUIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	u := NewUUID()
	if !re.MatchString(u) {
		t.Fatalf("uuid %q is not a valid v4 uuid", u)
	}
	if NewUUID() == u {
		t.Fatal("two NewUUID calls returned the same value")
	}
}

func TestNewShortID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}$`)
	s, err := NewShortID()
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString(s) {
		t.Fatalf("shortID %q must be 8 hex chars", s)
	}
}

func TestNewToken(t *testing.T) {
	tok, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 16 {
		t.Fatalf("token %q too short", tok)
	}
}
