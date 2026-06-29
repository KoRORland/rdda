package keys

import (
	"encoding/base64"
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

func TestPublicFromPrivate(t *testing.T) {
	kp, err := NewX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	got, err := PublicFromPrivate(kp.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != kp.PublicKey {
		t.Fatalf("derived %s, want %s", got, kp.PublicKey)
	}
	if _, err := PublicFromPrivate("!!! not base64 !!!"); err == nil {
		t.Fatal("invalid private key must error")
	}
	if _, err := PublicFromPrivate("YWJj"); err == nil {
		t.Fatal("wrong-length key must error")
	}
}

func TestNewX25519Keypair(t *testing.T) {
	kp, err := NewX25519Keypair()
	if err != nil {
		t.Fatal(err)
	}
	if kp.PrivateKey == "" || kp.PublicKey == "" {
		t.Fatal("empty key")
	}
	// sing-box uses base64 raw-url encoding of 32-byte keys.
	priv, err := base64.RawURLEncoding.DecodeString(kp.PrivateKey)
	if err != nil || len(priv) != 32 {
		t.Fatalf("private key not 32 raw-url bytes: %v len=%d", err, len(priv))
	}
	pub, err := base64.RawURLEncoding.DecodeString(kp.PublicKey)
	if err != nil || len(pub) != 32 {
		t.Fatalf("public key not 32 raw-url bytes: %v len=%d", err, len(pub))
	}
}
