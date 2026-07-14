package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"

	"github.com/KoRORland/rdda/internal/verify"
)

// testKey is an ephemeral minisign signer used to produce valid release
// signatures for the fake fetch seam. It mirrors the on-wire minisign format
// (prehashed "ED" mode) so the real verify.PublicKey.Verify path is exercised.
type testKey struct {
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	keyID [8]byte
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k := testKey{pub: pub, priv: priv}
	if _, err := rand.Read(k.keyID[:]); err != nil {
		t.Fatal(err)
	}
	return k
}

func (k testKey) public(t *testing.T) *verify.PublicKey {
	t.Helper()
	blob := append(append([]byte("Ed"), k.keyID[:]...), k.pub...)
	pk, err := verify.ParsePublicKey(base64.StdEncoding.EncodeToString(blob))
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// signSums returns a detached minisign signature (prehashed mode) over sums.
func (k testKey) signSums(sums string) string {
	h := blake2b.Sum512([]byte(sums))
	sig := ed25519.Sign(k.priv, h[:])
	sigBlob := append(append([]byte("ED"), k.keyID[:]...), sig...)
	tc := "timestamp:1\tfile:SHA256SUMS\thashed"
	global := ed25519.Sign(k.priv, append(append([]byte(nil), sig...), []byte(tc)...))
	return "untrusted comment: sig\n" +
		base64.StdEncoding.EncodeToString(sigBlob) + "\n" +
		"trusted comment: " + tc + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n"
}

// sumsFor builds a SHA256SUMS body whose amd64 line matches bin's digest.
func sumsFor(bin string) string {
	return sha256hex([]byte(bin)) + "  rdda-linux-amd64\n" +
		sha256hex([]byte("other")) + "  rdda-linux-arm64\n"
}

// newTestUpdater builds an Updater whose binPath is a temp file pre-seeded with
// the "old" binary, and whose seams are fakes. The fetched "new" binary bytes are
// []byte(toTag) so a successful swap is observable by reading binPath back.
func newTestUpdater(t *testing.T, current, latest string) (*Updater, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "rdda")
	if err := os.WriteFile(bin, []byte(current), 0o755); err != nil {
		t.Fatal(err)
	}
	key := newTestKey(t)
	u := &Updater{
		arch:       "amd64",
		current:    current,
		binPath:    bin,
		resolveTag: func() (string, error) { return latest, nil },
		// The fetched "new binary" is []byte(tag); SHA256SUMS matches it and is
		// signed by the injected key, so the real verify path runs in every test.
		fetch: func(tag, arch string) ([]byte, string, string, error) {
			sums := sumsFor(tag)
			return []byte(tag), sums, key.signSums(sums), nil
		},
		loadKey:    func() (*verify.PublicKey, error) { return key.public(t), nil },
		restart:    func(string) error { return nil },
		isActive:   func(string) bool { return true },
		unitExists: func(string) bool { return true }, // EU-like: rdda-sub present
		runVersion: func(string) (string, error) { return latest, nil },
		sleep:      func(time.Duration) {},
	}
	return u, bin
}

func TestCheckReportsNewer(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.2.0", "v0.3.0")
	cur, latest, newer, err := u.Check()
	if err != nil || cur != "v0.2.0" || latest != "v0.3.0" || !newer {
		t.Fatalf("got %s %s %v %v", cur, latest, newer, err)
	}
}

func TestCheckSameVersionNotNewer(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.3.0", "v0.3.0")
	if _, _, newer, _ := u.Check(); newer {
		t.Fatal("same version must not be newer")
	}
}

func TestUpdateUpToDateNoop(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.3.0", "v0.3.0")
	from, to, err := u.Update()
	if err != nil || from != to {
		t.Fatalf("expected no-op, got %s %s %v", from, to, err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.3.0" {
		t.Fatalf("binary must be unchanged, got %q", b)
	}
	if _, err := os.Stat(bin + ".prev"); err == nil {
		t.Fatal("no backup should be written on a no-op")
	}
}

func TestUpdateHappyPath(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	from, to, err := u.Update()
	if err != nil || from != "v0.2.0" || to != "v0.3.0" {
		t.Fatalf("got %s %s %v", from, to, err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.3.0" {
		t.Fatalf("binary not swapped, got %q", b)
	}
	if b, _ := os.ReadFile(bin + ".prev"); string(b) != "v0.2.0" {
		t.Fatalf("backup not kept, got %q", b)
	}
}

// Regression: on an RU node rdda-sub does not exist. The update must succeed on
// the strength of the binary self-check alone — never restart (and fail on) a
// nonexistent unit, which previously rolled back a perfectly good update and
// printed a bogus "ROLLBACK FAILED".
func TestUpdateSkipsRestartWhenUnitAbsent(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.4.0", "v0.4.1")
	u.unitExists = func(string) bool { return false } // RU: no rdda-sub
	restartCalled := false
	u.restart = func(string) error { restartCalled = true; return errors.New("Unit rdda-sub.service not loaded") }
	from, to, err := u.Update()
	if err != nil {
		t.Fatalf("RU update must succeed without rdda-sub, got %v", err)
	}
	if from != "v0.4.0" || to != "v0.4.1" {
		t.Fatalf("got %s -> %s", from, to)
	}
	if restartCalled {
		t.Fatal("must not restart a unit that does not exist on this node")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.4.1" {
		t.Fatalf("new binary not in place: %q", b)
	}
}

// The swapped-in binary must keep its world-exec bit even under a hardened
// umask — rdda-sub runs as User=rdda and must be able to exec it. os.WriteFile's
// mode is umask-masked, so this guards the explicit chmod in writeBin.
func TestUpdatePreservesExecModeUnderHardenedUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	if _, _, err := u.Update(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o755 {
		t.Fatalf("binary must stay 0755 under umask 077, got %o", perm)
	}
}

func TestUpdateRestartErrorRollsBack(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	u.restart = func(string) error { return errors.New("restart boom") }
	if _, _, err := u.Update(); err == nil {
		t.Fatal("a restart failure must error")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("must roll back the binary after a restart failure, got %q", b)
	}
}

func TestUpdateChecksumMismatchAborts(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	key := newTestKey(t)
	u.loadKey = func() (*verify.PublicKey, error) { return key.public(t), nil }
	// Correctly signed SHA256SUMS, but its amd64 digest does not match the binary.
	u.fetch = func(string, string) ([]byte, string, string, error) {
		sums := sumsFor("a-different-binary")
		return []byte("v0.3.0"), sums, key.signSums(sums), nil
	}
	if _, _, err := u.Update(); err == nil {
		t.Fatal("checksum mismatch must error")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("binary must be untouched on mismatch, got %q", b)
	}
	if _, err := os.Stat(bin + ".prev"); err == nil {
		t.Fatal("no backup on a pre-swap abort")
	}
}

// A validly-checksummed binary whose SHA256SUMS is signed by the WRONG key (or
// unsigned) must be rejected before any file is touched — this is the whole
// point of F-1. Guards against a substituted binary + attacker-generated sums.
func TestUpdateBadSignatureAborts(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	attacker := newTestKey(t)
	// fetch signs with the attacker key; loadKey (unchanged) returns the real key.
	u.fetch = func(tag, arch string) ([]byte, string, string, error) {
		sums := sumsFor(tag)
		return []byte(tag), sums, attacker.signSums(sums), nil
	}
	_, _, err := u.Update()
	if err == nil {
		t.Fatal("a release signed by the wrong key must be rejected")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("binary must be untouched on a bad signature, got %q", b)
	}
	if _, err := os.Stat(bin + ".prev"); err == nil {
		t.Fatal("no backup on a pre-swap signature abort")
	}
}

// A build with no real signing key embedded (the committed placeholder) must
// fail closed and never install — even before reaching the network.
func TestUpdateUnconfiguredKeyFailsClosed(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	fetched := false
	u.loadKey = func() (*verify.PublicKey, error) { return nil, errors.New("release signing not configured") }
	u.fetch = func(string, string) ([]byte, string, string, error) {
		fetched = true
		return []byte("x"), "", "", nil
	}
	if _, _, err := u.Update(); err == nil {
		t.Fatal("must fail closed when no signing key is embedded")
	}
	if fetched {
		t.Fatal("must not download a release before the signing key is available")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("binary must be untouched, got %q", b)
	}
}

func TestUpdateSelfCheckFailRollsBack(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	u.runVersion = func(string) (string, error) { return "garbage", nil } // new binary reports wrong tag
	if _, _, err := u.Update(); err == nil {
		t.Fatal("self-check failure must error")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("must roll back to old binary, got %q", b)
	}
}

func TestUpdateUnitNotActiveRollsBack(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	u.isActive = func(string) bool { return false } // rdda-sub never comes back
	if _, _, err := u.Update(); err == nil {
		t.Fatal("inactive rdda-sub must error")
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("must roll back, got %q", b)
	}
}

func TestRollbackRestoresPrev(t *testing.T) {
	u, bin := newTestUpdater(t, "v0.2.0", "v0.3.0")
	if _, _, err := u.Update(); err != nil { // creates .prev = v0.2.0, bin = v0.3.0
		t.Fatal(err)
	}
	if err := u.Rollback(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "v0.2.0" {
		t.Fatalf("rollback must restore prev, got %q", b)
	}
}

func TestRollbackNoPrevErrors(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.2.0", "v0.3.0")
	if err := u.Rollback(); err == nil {
		t.Fatal("rollback with no .prev must error")
	}
}
