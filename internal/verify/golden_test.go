package verify

import (
	"strings"
	"testing"
)

// Golden vectors produced by the real `minisign` 0.12 CLI (not this package), so
// the pure-Go verifier is checked against genuine minisign output — including its
// default prehashed (BLAKE2b-512, "ED") mode and tab-separated trusted comment:
//
//	minisign -G -W -p mk.pub -s mk.key
//	printf 'f00d  rdda-linux-amd64\ncafe  rdda-linux-arm64\n' > SHA256SUMS
//	minisign -S -s mk.key -m SHA256SUMS
const (
	goldenPubKey = "untrusted comment: minisign public key 41092A1BC025DEC0\n" +
		"RWTA3iXAGyoJQb4epFKfpRl0tYuC8qL+hok0V8URsQBea45Ii/cUyp45\n"

	// Exact signed bytes: two-space separators and a trailing newline, as sha256sum writes.
	goldenSHA256SUMS = "f00d  rdda-linux-amd64\ncafe  rdda-linux-arm64\n"

	goldenMinisig = "untrusted comment: signature from minisign secret key\n" +
		"RUTA3iXAGyoJQQatt6w4PeG7xJDTko0wtu3cTVazbquCtJfUNU2e/9l0RFY1k2WL0PpZ/hMfSoOHnWAMABESLED6ixNGi0Zqngg=\n" +
		"trusted comment: timestamp:1784062239\tfile:SHA256SUMS\thashed\n" +
		"Q6zRaiRq4PnQ2i6RIt8ZDrnLkWh6N1GknxT73AyR42fOSAdy99QzdMC1WfOU8fhvS5KGem0M8ASltqgvz+4gAA==\n"
)

func TestVerify_GoldenMinisignVector(t *testing.T) {
	pk, err := ParsePublicKey(goldenPubKey)
	if err != nil {
		t.Fatalf("parse real minisign public key: %v", err)
	}
	if err := pk.Verify([]byte(goldenSHA256SUMS), goldenMinisig); err != nil {
		t.Fatalf("real minisign signature rejected by pure-Go verifier: %v", err)
	}
}

func TestVerify_GoldenRejectsTamper(t *testing.T) {
	pk, err := ParsePublicKey(goldenPubKey)
	if err != nil {
		t.Fatal(err)
	}
	// A single flipped byte in the signed content must be caught.
	tampered := strings.Replace(goldenSHA256SUMS, "f00d", "f00e", 1)
	if err := pk.Verify([]byte(tampered), goldenMinisig); err == nil {
		t.Fatal("verifier accepted tampered content against a real minisign signature")
	}
}
