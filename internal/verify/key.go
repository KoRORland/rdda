package verify

import (
	_ "embed"
	"errors"
	"strings"
)

// embeddedPublicKey is the maintainer's minisign release-signing public key,
// baked into the binary at build time. Shipping it in the binary (rather than
// fetching it) is the whole point: the update path trusts this key and nothing
// the network hands it.
//
//go:embed minisign.pub
var embeddedPublicKey string

// placeholderMarker identifies the committed placeholder key. A tree that has
// not had a real key embedded must fail closed, never silently skip verification.
const placeholderMarker = "PLACEHOLDER-UNSIGNED"

// Maintainer returns the embedded release-signing public key. It returns an
// error while the placeholder key is still in place, so callers (self-update)
// refuse to install an unverified release rather than degrading to no
// verification.
func Maintainer() (*PublicKey, error) {
	if strings.Contains(embeddedPublicKey, placeholderMarker) {
		return nil, errors.New("release signing not configured: embedded minisign public key is a placeholder (see docs/RELEASING.md)")
	}
	return ParsePublicKey(embeddedPublicKey)
}
