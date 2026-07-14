package verify

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// testSigner independently constructs minisign-format public keys and signatures
// (i.e. not via this package's parser), so the tests exercise verify.go against
// the real on-wire format rather than against its own assumptions.
type testSigner struct {
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	keyID [8]byte
}

func newTestSigner(t *testing.T) testSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	s := testSigner{pub: pub, priv: priv}
	if _, err := rand.Read(s.keyID[:]); err != nil {
		t.Fatalf("key id: %v", err)
	}
	return s
}

// pubFile renders the two-line minisign public key file for this signer.
func (s testSigner) pubFile() string {
	blob := append([]byte(algLegacy), s.keyID[:]...)
	blob = append(blob, s.pub...)
	return "untrusted comment: minisign public key\n" + base64.StdEncoding.EncodeToString(blob) + "\n"
}

// sign builds a detached minisign signature over content in the given mode
// (algLegacy or algPrehashed), with an optional trusted-comment override to let
// tests tamper with it.
func (s testSigner) sign(content []byte, alg, trustedComment string) string {
	signed := content
	if alg == algPrehashed {
		h := blake2b.Sum512(content)
		signed = h[:]
	}
	sig := ed25519.Sign(s.priv, signed)

	sigBlob := append([]byte(alg), s.keyID[:]...)
	sigBlob = append(sigBlob, sig...)

	if trustedComment == "" {
		trustedComment = "timestamp:1 file:SHA256SUMS"
	}
	global := ed25519.Sign(s.priv, append(append([]byte(nil), sig...), []byte(trustedComment)...))

	return "untrusted comment: minisign signature\n" +
		base64.StdEncoding.EncodeToString(sigBlob) + "\n" +
		"trusted comment: " + trustedComment + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n"
}

func mustParse(t *testing.T, s testSigner) *PublicKey {
	t.Helper()
	pk, err := ParsePublicKey(s.pubFile())
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	return pk
}

func TestVerify_RoundTrip(t *testing.T) {
	content := []byte("f00d  rdda-linux-amd64\ncafe  rdda-linux-arm64\n")
	for _, alg := range []string{algLegacy, algPrehashed} {
		t.Run(alg, func(t *testing.T) {
			s := newTestSigner(t)
			pk := mustParse(t, s)
			if err := pk.Verify(content, s.sign(content, alg, "")); err != nil {
				t.Fatalf("valid %s signature rejected: %v", alg, err)
			}
		})
	}
}

func TestVerify_ParseBareKeyLine(t *testing.T) {
	s := newTestSigner(t)
	bare := strings.TrimSpace(strings.Split(strings.TrimSpace(s.pubFile()), "\n")[1])
	if _, err := ParsePublicKey(bare); err != nil {
		t.Fatalf("bare key line should parse: %v", err)
	}
}

func TestVerify_TamperedContent(t *testing.T) {
	s := newTestSigner(t)
	pk := mustParse(t, s)
	sig := s.sign([]byte("original"), algLegacy, "")
	if err := pk.Verify([]byte("tampered"), sig); err == nil {
		t.Fatal("verification passed on tampered content")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	content := []byte("payload")
	signer := newTestSigner(t)
	attacker := newTestSigner(t) // different key id and key
	pk := mustParse(t, signer)
	if err := pk.Verify(content, attacker.sign(content, algLegacy, "")); err == nil {
		t.Fatal("verification passed for a signature from a different key")
	}
}

func TestVerify_SameKeyIDDifferentKey(t *testing.T) {
	// An attacker who reuses the maintainer's key id but signs with their own
	// key must still be rejected — the signature check, not just the id, is load
	// bearing.
	content := []byte("payload")
	signer := newTestSigner(t)
	attacker := newTestSigner(t)
	attacker.keyID = signer.keyID
	pk := mustParse(t, signer)
	if err := pk.Verify(content, attacker.sign(content, algLegacy, "")); err == nil {
		t.Fatal("verification passed for forged key id with wrong signing key")
	}
}

func TestVerify_TamperedTrustedComment(t *testing.T) {
	// Swap the trusted comment for one the global signature does not cover.
	s := newTestSigner(t)
	pk := mustParse(t, s)
	sig := s.sign([]byte("payload"), algLegacy, "timestamp:1 file:SHA256SUMS")
	tampered := strings.Replace(sig, "trusted comment: timestamp:1 file:SHA256SUMS",
		"trusted comment: timestamp:1 file:evil", 1)
	if err := pk.Verify([]byte("payload"), tampered); err == nil {
		t.Fatal("verification passed with a tampered trusted comment")
	}
}

func TestVerify_TamperedSignatureBytes(t *testing.T) {
	s := newTestSigner(t)
	pk := mustParse(t, s)
	sig := s.sign([]byte("payload"), algLegacy, "")
	lines := strings.Split(strings.TrimSpace(sig), "\n")
	// Flip a byte in the base64 signature blob (line 2).
	blob, _ := base64.StdEncoding.DecodeString(lines[1])
	blob[20] ^= 0xff
	lines[1] = base64.StdEncoding.EncodeToString(blob)
	if err := pk.Verify([]byte("payload"), strings.Join(lines, "\n")); err == nil {
		t.Fatal("verification passed with a corrupted signature blob")
	}
}

func TestParsePublicKey_Malformed(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"bad base64":   "untrusted comment: x\n!!!!not base64!!!!\n",
		"wrong length": "untrusted comment: x\n" + base64.StdEncoding.EncodeToString([]byte("too short")) + "\n",
		"wrong alg":    "untrusted comment: x\n" + base64.StdEncoding.EncodeToString(append([]byte("XX"), make([]byte, 40)...)) + "\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePublicKey(in); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestVerify_MalformedSignatureFile(t *testing.T) {
	s := newTestSigner(t)
	pk := mustParse(t, s)
	cases := map[string]string{
		"too few lines":     "untrusted comment: x\nAAAA\n",
		"missing untrusted": "x\nAAAA\ntrusted comment: y\nAAAA\n",
		"missing trusted":   "untrusted comment: x\nAAAA\nnope\nAAAA\n",
		"sig not base64":    "untrusted comment: x\n!!!!\ntrusted comment: y\nAAAA\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if err := pk.Verify([]byte("payload"), in); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}
