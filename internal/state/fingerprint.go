package state

import (
	"crypto/rand"
	"math/big"
	"sort"
	"strings"
)

// ClientFingerprints is the uTLS fingerprint pool a new client's client→RU hop
// is drawn from. A whole fleet sharing one fingerprint is itself a correlation
// signal, so each client mimics a different common browser's TLS ClientHello.
var ClientFingerprints = []string{"firefox", "edge", "safari", "android"}

// RandomClientFingerprint returns an unbiased random pick from the pool. It
// falls back to the first entry only if the system RNG fails.
func RandomClientFingerprint() string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(ClientFingerprints))))
	if err != nil {
		return ClientFingerprints[0]
	}
	return ClientFingerprints[n.Int64()]
}

// IsClientFingerprint reports whether fp is one of the supported pool values.
func IsClientFingerprint(fp string) bool {
	for _, v := range ClientFingerprints {
		if v == fp {
			return true
		}
	}
	return false
}

// FingerprintList is a human-readable, comma-joined pool (for flag help/errors).
func FingerprintList() string {
	c := append([]string(nil), ClientFingerprints...)
	sort.Strings(c)
	return strings.Join(c, ", ")
}
