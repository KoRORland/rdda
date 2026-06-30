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
	fetch      func(tag, arch string) (bin []byte, sum string, err error)
	restart    func(unit string) error
	isActive   func(unit string) bool
	runVersion func(path string) (string, error)
	sleep      func(time.Duration)
}

// New wires an Updater to real GitHub/systemd/exec implementations.
func New(current string) *Updater {
	u := &Updater{arch: goarch(), current: current, binPath: binPathDefault, sleep: time.Sleep}
	u.resolveTag = resolveLatestTag
	u.fetch = fetchRelease
	u.restart = func(unit string) error { return exec.Command("systemctl", "restart", unit).Run() }
	u.isActive = func(unit string) bool {
		out, _ := exec.Command("systemctl", "is-active", unit).CombinedOutput()
		return strings.TrimSpace(string(out)) == "active"
	}
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
	bin, sum, err := u.fetch(to, u.arch)
	if err != nil {
		return u.current, to, err
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
		u.rollback()
		return u.current, to, fmt.Errorf("new binary self-check failed (got %q want %q): rolled back", v, to)
	}
	if rerr := u.restart(restartUnit); rerr != nil {
		u.rollback()
		return u.current, to, fmt.Errorf("restart %s failed: %v: rolled back", restartUnit, rerr)
	}
	if !u.waitActive(restartUnit) {
		u.rollback()
		return u.current, to, fmt.Errorf("%s not active after update: rolled back", restartUnit)
	}
	return u.current, to, nil
}

// Rollback restores the previous binary (rdda.prev) and restarts rdda-sub.
func (u *Updater) Rollback() error {
	if _, err := os.Stat(u.prevPath()); err != nil {
		return fmt.Errorf("no previous binary to roll back to")
	}
	if err := u.restoreBin(); err != nil {
		return err
	}
	return u.restart(restartUnit)
}

// rollback is the best-effort internal revert used by Update on a failed update.
func (u *Updater) rollback() {
	_ = u.restoreBin()
	_ = u.restart(restartUnit)
}

func (u *Updater) writeBin(b []byte) error {
	np := u.binPath + ".new"
	if err := os.WriteFile(np, b, 0o755); err != nil {
		return err
	}
	return os.Rename(np, u.binPath)
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
	resp, err := http.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github release lookup: HTTP %d", resp.StatusCode)
	}
	var doc struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	if doc.TagName == "" {
		return "", fmt.Errorf("no tag_name in latest release (has a release been published?)")
	}
	return doc.TagName, nil
}

func fetchRelease(tag, arch string) ([]byte, string, error) {
	base := "https://github.com/" + repo + "/releases/download/" + tag
	bin, err := httpGetBytes(base + "/rdda-linux-" + arch)
	if err != nil {
		return nil, "", err
	}
	sums, err := httpGetBytes(base + "/SHA256SUMS")
	if err != nil {
		return nil, "", err
	}
	sum := sumFor(string(sums), "rdda-linux-"+arch)
	if sum == "" {
		return nil, "", fmt.Errorf("no checksum for rdda-linux-%s in SHA256SUMS", arch)
	}
	return bin, sum, nil
}

func httpGetBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
