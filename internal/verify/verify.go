// Package verify checks minisign signatures against an embedded maintainer
// public key. It is the trust root for the self-update path (internal/selfupdate)
// and the offline counterpart to install.sh's `minisign -V`: a released
// SHA256SUMS is only honoured once its detached .minisig verifies here, so a
// substituted binary with an attacker-generated checksum is rejected before any
// swap. Verification is pure Go with no external `minisign` process, because the
// node that most needs to self-update reliably (inside Russia) is exactly where
// shelling out to an extra binary is least dependable.
//
// minisign format reference: https://jedisct1.github.io/minisign/
package verify

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// minisign signature-algorithm identifiers (the first two bytes of the decoded
// signature blob). "Ed" is a plain Ed25519 signature over the file; "ED" is
// Ed25519 over BLAKE2b-512(file) (minisign's prehashed mode, its default for
// large files). We accept both so verification does not depend on which mode the
// signer used.
const (
	algLegacy    = "Ed"
	algPrehashed = "ED"
)

// PublicKey is a parsed minisign public key (algorithm is always Ed25519).
type PublicKey struct {
	keyID [8]byte
	key   ed25519.PublicKey
}

// ParsePublicKey decodes a minisign public key. It accepts either the bare
// base64 key line or the full two-line "untrusted comment: ...\n<base64>" file
// that `minisign -G` writes.
func ParsePublicKey(s string) (*PublicKey, error) {
	line := lastPayloadLine(s)
	if line == "" {
		return nil, errors.New("minisign public key: empty")
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		return nil, fmt.Errorf("minisign public key: base64: %w", err)
	}
	// 2 (algorithm) + 8 (key id) + 32 (Ed25519 public key).
	if len(raw) != 42 {
		return nil, fmt.Errorf("minisign public key: want 42 bytes, got %d", len(raw))
	}
	if string(raw[0:2]) != algLegacy {
		return nil, fmt.Errorf("minisign public key: unsupported algorithm %q", string(raw[0:2]))
	}
	pk := &PublicKey{key: ed25519.PublicKey(append([]byte(nil), raw[10:42]...))}
	copy(pk.keyID[:], raw[2:10])
	return pk, nil
}

// Verify reports whether minisig is a valid detached minisign signature over
// content, made by pub. It checks the key id matches, the content signature
// verifies (legacy or prehashed), and the global "trusted comment" signature
// verifies — matching what `minisign -V` enforces. Any failure returns an error;
// nil means the content is authentic.
func (pub *PublicKey) Verify(content []byte, minisig string) error {
	sigLine, trustedComment, globalLine, err := parseMinisig(minisig)
	if err != nil {
		return err
	}

	sigBlob, err := base64.StdEncoding.DecodeString(sigLine)
	if err != nil {
		return fmt.Errorf("minisign signature: base64: %w", err)
	}
	// 2 (algorithm) + 8 (key id) + 64 (Ed25519 signature).
	if len(sigBlob) != 74 {
		return fmt.Errorf("minisign signature: want 74 bytes, got %d", len(sigBlob))
	}
	alg := string(sigBlob[0:2])
	// Constant-time to avoid leaking which byte of the key id mismatched; a wrong
	// key id means the signature was made by a different (possibly attacker) key.
	if subtle.ConstantTimeCompare(sigBlob[2:10], pub.keyID[:]) != 1 {
		return errors.New("minisign signature: key id does not match the embedded public key")
	}
	sig := sigBlob[10:74]

	var signed []byte
	switch alg {
	case algLegacy:
		signed = content
	case algPrehashed:
		h := blake2b.Sum512(content)
		signed = h[:]
	default:
		return fmt.Errorf("minisign signature: unsupported algorithm %q", alg)
	}
	if !ed25519.Verify(pub.key, signed, sig) {
		return errors.New("minisign signature: verification failed (content does not match signature)")
	}

	// The global signature binds the trusted comment to the content signature, so
	// a valid .minisig cannot have its trusted comment swapped. We do not consume
	// the comment, but verifying it keeps parity with `minisign -V` and rejects a
	// mangled signature file.
	globalSig, err := base64.StdEncoding.DecodeString(globalLine)
	if err != nil {
		return fmt.Errorf("minisign global signature: base64: %w", err)
	}
	if len(globalSig) != ed25519.SignatureSize {
		return fmt.Errorf("minisign global signature: want %d bytes, got %d", ed25519.SignatureSize, len(globalSig))
	}
	if !ed25519.Verify(pub.key, append(append([]byte(nil), sig...), []byte(trustedComment)...), globalSig) {
		return errors.New("minisign global signature: verification failed (trusted comment tampered)")
	}
	return nil
}

// parseMinisig splits a minisign signature file into its base64 signature line,
// trusted-comment text, and base64 global-signature line. A minisign file is:
//
//	untrusted comment: <text>
//	<base64 signature blob>
//	trusted comment: <text>
//	<base64 global signature>
func parseMinisig(s string) (sigLine, trustedComment, globalLine string, err error) {
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimRight(ln, "\r"))
	}
	// Trim any trailing blank lines (a final newline is conventional).
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != 4 {
		return "", "", "", fmt.Errorf("minisign signature: want 4 lines, got %d", len(lines))
	}
	const tcPrefix = "trusted comment: "
	if !strings.HasPrefix(lines[0], "untrusted comment:") {
		return "", "", "", errors.New("minisign signature: missing untrusted comment line")
	}
	if !strings.HasPrefix(lines[2], tcPrefix) {
		return "", "", "", errors.New("minisign signature: missing trusted comment line")
	}
	return strings.TrimSpace(lines[1]), lines[2][len(tcPrefix):], strings.TrimSpace(lines[3]), nil
}

// lastPayloadLine returns the last non-empty, non-comment line of a minisign key
// file — the base64 payload — so both the bare key and the full commented file
// parse.
func lastPayloadLine(s string) string {
	var out string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if ln == "" || strings.HasPrefix(ln, "untrusted comment:") {
			continue
		}
		out = ln
	}
	return out
}
