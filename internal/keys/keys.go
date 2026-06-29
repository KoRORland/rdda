// Package keys generates the cryptographic identities RDDA hands to sing-box.
package keys

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// NewUUID returns a random RFC-4122 v4 UUID string (sing-box client id).
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewShortID returns an 8-hex-char REALITY shortId (4 random bytes).
func NewShortID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// NewToken returns a URL-safe random token for subscription URLs (16 bytes).
func NewToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// X25519Keypair holds base64 raw-url-encoded REALITY keys for sing-box.
type X25519Keypair struct {
	PrivateKey string
	PublicKey  string
}

// NewX25519Keypair generates a clamped Curve25519 keypair for REALITY.
func NewX25519Keypair() (X25519Keypair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return X25519Keypair{}, err
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return X25519Keypair{}, err
	}
	return X25519Keypair{
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv[:]),
		PublicKey:  base64.RawURLEncoding.EncodeToString(pub),
	}, nil
}

// PublicFromPrivate derives the base64-raw-url X25519 public key from a
// base64-raw-url private key (the REALITY keypair encoding).
func PublicFromPrivate(privB64 string) (string, error) {
	priv, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		return "", fmt.Errorf("decode private key: %w", err)
	}
	if len(priv) != 32 {
		return "", fmt.Errorf("private key must be 32 bytes, got %d", len(priv))
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(pub), nil
}
