// Package selfupdate replaces the running rdda binary with the latest published
// release, verified by checksum, with an automatic rollback if the new binary is
// broken. Used by `rdda update` and the opt-in rdda-update.timer (root).
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/verify"
)

const (
	repo           = "KoRORland/rdda"
	binPathDefault = "/usr/local/bin/rdda"
	restartUnit    = "rdda-sub"
)

// Updater self-updates the rdda binary. The function fields are seams: New wires
// them to real implementations; tests inject fakes.
type Updater struct {
	arch       string
	current    string
	binPath    string
	resolveTag func() (string, error)
	fetch      func(tag, arch string) (bin []byte, sums, sig string, err error)
	loadKey    func() (*verify.PublicKey, error)
	restart    func(unit string) error
	isActive   func(unit string) bool
	unitExists func(unit string) bool
	runVersion func(path string) (string, error)
	sleep      func(time.Duration)
}

// New wires an Updater to real GitHub/systemd/exec implementations.
func New(current string) *Updater {
	u := &Updater{arch: goarch(), current: current, binPath: binPathDefault, sleep: time.Sleep}
	u.resolveTag = resolveLatestTag
	u.fetch = fetchRelease
	u.loadKey = verify.Maintainer
	u.restart = func(unit string) error { return exec.Command("systemctl", "restart", unit).Run() }
	u.isActive = func(unit string) bool {
		out, _ := exec.Command("systemctl", "is-active", unit).CombinedOutput()
		return strings.TrimSpace(string(out)) == "active"
	}
	// unitExists reports whether systemd knows the unit at all. `restartUnit`
	// (rdda-sub) is EU-only; on an RU node it does not exist, so the post-swap
	// restart must be skipped rather than treated as a failure that rolls back a
	// perfectly good binary. `systemctl cat` exits non-zero for an unknown unit.
	u.unitExists = func(unit string) bool {
		return exec.Command("systemctl", "cat", unit).Run() == nil
	}
	// The post-swap self-check compares this output against the target release
	// tag (v == to). That contract holds because `rdda version` prints exactly
	// the ldflags-injected Version and nothing else (see internal/cli/cli.go).
	// If `rdda version` ever prints more than the bare tag, this check would
	// roll back every valid update — keep them in sync.
	u.runVersion = func(path string) (string, error) {
		out, err := exec.Command(path, "version").CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	return u
}

func (u *Updater) prevPath() string { return u.binPath + ".prev" }

// Check resolves the latest tag and reports whether it differs from current.
func (u *Updater) Check() (current, latest string, newer bool, err error) {
	latest, err = u.resolveTag()
	if err != nil {
		return u.current, "", false, err
	}
	return u.current, latest, latest != u.current, nil
}

// Update downloads+verifies the latest release, atomically swaps it in, restarts
// rdda-sub, and rolls back if the new binary fails to run or rdda-sub stays down.
// from == to means already up to date (no-op).
func (u *Updater) Update() (from, to string, err error) {
	to, err = u.resolveTag()
	if err != nil {
		return u.current, "", err
	}
	if to == u.current {
		return u.current, u.current, nil
	}
	// Load the embedded signing key first: a build without a real key must fail
	// closed here, before any download, rather than fall back to no verification.
	key, err := u.loadKey()
	if err != nil {
		return u.current, to, err
	}
	bin, sums, sig, err := u.fetch(to, u.arch)
	if err != nil {
		return u.current, to, err
	}
	// The signature over SHA256SUMS is the trust gate. A digest only proves the
	// binary matches what the release serves; the signature proves the maintainer
	// signed that SHA256SUMS. Verify it before extracting the digest or touching
	// any file, so a substituted binary + attacker-generated checksum is rejected.
	if err := key.Verify([]byte(sums), sig); err != nil {
		return u.current, to, fmt.Errorf("release signature verification failed: %w", err)
	}
	sum := sumFor(sums, "rdda-linux-"+u.arch)
	if sum == "" {
		return u.current, to, fmt.Errorf("no checksum for rdda-linux-%s in SHA256SUMS", u.arch)
	}
	if got := sha256hex(bin); !strings.EqualFold(got, sum) {
		return u.current, to, fmt.Errorf("checksum mismatch: got %s want %s", got, sum)
	}
	cur, err := os.ReadFile(u.binPath)
	if err != nil {
		return u.current, to, fmt.Errorf("read current binary: %w", err)
	}
	if err := os.WriteFile(u.prevPath(), cur, 0o755); err != nil {
		return u.current, to, fmt.Errorf("write backup: %w", err)
	}
	if err := u.writeBin(bin); err != nil {
		return u.current, to, fmt.Errorf("swap binary: %w", err)
	}
	if v, verr := u.runVersion(u.binPath); verr != nil || v != to {
		return u.current, to, u.revert(fmt.Sprintf("new binary self-check failed (got %q want %q)", v, to))
	}
	// The `rdda version` self-check above is the role-independent proof the new
	// binary works. Only restart the long-running service that execs rdda when it
	// actually exists on this node — on an RU node there is none (rdda-sub is
	// EU-only; pull/health/geoip are oneshot timers that pick up the new binary
	// on their next fire), so skip the restart instead of failing the update.
	if u.unitExists(restartUnit) {
		if rerr := u.restart(restartUnit); rerr != nil {
			return u.current, to, u.revert(fmt.Sprintf("restart %s failed: %v", restartUnit, rerr))
		}
		if !u.waitActive(restartUnit) {
			return u.current, to, u.revert(fmt.Sprintf("%s not active after update", restartUnit))
		}
	}
	return u.current, to, nil
}

// revert rolls back after a failed update and returns an error describing both
// the original cause and whether the rollback itself succeeded. A silent
// rollback failure reported as success is the worst outcome for a no-brick
// feature, so a failed restore is surfaced loudly.
func (u *Updater) revert(cause string) error {
	if rerr := u.rollback(); rerr != nil {
		return fmt.Errorf("%s: ROLLBACK FAILED, binary may be broken: %v", cause, rerr)
	}
	return fmt.Errorf("%s: rolled back", cause)
}

// Rollback restores the previous binary (rdda.prev) and restarts rdda-sub.
func (u *Updater) Rollback() error {
	if _, err := os.Stat(u.prevPath()); err != nil {
		return fmt.Errorf("no previous binary to roll back to")
	}
	return u.rollback()
}

// rollback restores the previous binary and restarts rdda-sub, used by Update on
// a failed update. It returns the restore error (if any) so the caller can report
// a failed rollback rather than falsely claiming the node reverted.
func (u *Updater) rollback() error {
	if err := u.restoreBin(); err != nil {
		return err
	}
	if u.unitExists(restartUnit) {
		return u.restart(restartUnit)
	}
	return nil
}

func (u *Updater) writeBin(b []byte) error {
	np := u.binPath + ".new"
	if err := os.WriteFile(np, b, 0o755); err != nil {
		_ = os.Remove(np) // don't leave a partial .new behind on a failed write
		return err
	}
	if err := os.Rename(np, u.binPath); err != nil {
		_ = os.Remove(np) // ...or a stale .new behind on a failed swap
		return err
	}
	// os.WriteFile's mode is masked by umask; the live binary must keep its
	// world-exec bit so rdda-sub (User=rdda) can exec it. Force the mode
	// explicitly, matching install.sh's `install -m 0755`. Without this, a
	// hardened umask (027/077) would strip o+x and brick rdda-sub — and a
	// rollback routes through here too, so it would not recover.
	return os.Chmod(u.binPath, 0o755)
}

func (u *Updater) restoreBin() error {
	b, err := os.ReadFile(u.prevPath())
	if err != nil {
		return err
	}
	return u.writeBin(b)
}

func (u *Updater) waitActive(unit string) bool {
	for i := 0; i < 5; i++ {
		if u.isActive(unit) {
			return true
		}
		u.sleep(time.Second)
	}
	return false
}

func goarch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func resolveLatestTag() (string, error) {
	body, err := httpGetBytes("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	var doc struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	if doc.TagName == "" {
		return "", fmt.Errorf("no tag_name in latest release (has a release been published?)")
	}
	return doc.TagName, nil
}

func fetchRelease(tag, arch string) (bin []byte, sums, sig string, err error) {
	base := "https://github.com/" + repo + "/releases/download/" + tag
	bin, err = httpGetBytes(base + "/rdda-linux-" + arch)
	if err != nil {
		return nil, "", "", err
	}
	sumsBytes, err := httpGetBytes(base + "/SHA256SUMS")
	if err != nil {
		return nil, "", "", err
	}
	// The detached minisign signature over SHA256SUMS. Its absence is a hard
	// failure: an unsigned release must not be installable by a signing-aware
	// client (fail closed), and a stable asset name keeps this resolvable.
	sigBytes, err := httpGetBytes(base + "/SHA256SUMS.minisig")
	if err != nil {
		return nil, "", "", fmt.Errorf("fetch release signature: %w", err)
	}
	return bin, string(sumsBytes), string(sigBytes), nil
}

// httpGetBytes fetches a URL with a per-attempt timeout and a few retries, so a
// single flaky TLS handshake to the GitHub release CDN (routine from inside
// Russia — the whole reason this project exists) doesn't abort a self-update.
func httpGetBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
		} else {
			body, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			switch {
			case rerr != nil:
				lastErr = rerr
			case resp.StatusCode != http.StatusOK:
				lastErr = fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
			default:
				return body, nil
			}
		}
		if attempt < 4 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
	}
	return nil, fmt.Errorf("GET %s failed after retries: %w", url, lastErr)
}

// sumFor returns the hex checksum whose line ends with the given filename.
// SHA256SUMS lines look like: "<hex>  rdda-linux-amd64".
func sumFor(sums, filename string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == filename {
			return f[0]
		}
	}
	return ""
}
