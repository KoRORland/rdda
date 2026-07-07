// Package geoipupdate refreshes the RU node's local geoip-ru rule-set (.srs)
// from upstream, fail-safe. It is designed for an anti-censorship entry node:
// it never leaves the node without geoip data and never crash-loops a timer —
// any fetch/validate failure keeps the existing file and is reported as a
// non-fatal skip. sing-box only reloads when the data actually changed.
package geoipupdate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultURL is the upstream geoip-ru rule-set (the same source install.sh uses).
const DefaultURL = "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs"

// Updater refreshes one .srs file. The function fields are seams: real
// implementations do HTTP / sing-box / systemctl; tests fake them.
type Updater struct {
	Path     string                           // destination .srs (e.g. /etc/rdda/geoip-ru.srs)
	URL      string                           // upstream URL
	Fetch    func(url string) ([]byte, error) // download (retry/timeout in the real impl)
	Validate func(data []byte) error          // structural + best-effort sing-box check
	Reload   func() error                     // reload sing-box (only called on change)
	Chown    func(path string) error          // best-effort owner fix after write
}

// Result reports what happened, for logging.
type Result struct {
	Changed bool
	Skipped bool   // fetch/validate failed; existing file left in place
	Reason  string // why skipped, or a short status
}

// Run fetches, validates, and atomically swaps the .srs iff its bytes changed,
// then reloads sing-box. On fetch/validate failure it returns a Skipped result
// with the error, having touched nothing — the caller logs and exits 0.
func (u Updater) Run() (Result, error) {
	data, err := u.Fetch(u.URL)
	if err != nil {
		return Result{Skipped: true, Reason: "fetch failed: " + err.Error()}, err
	}
	if u.Validate != nil {
		if err := u.Validate(data); err != nil {
			return Result{Skipped: true, Reason: "validation failed: " + err.Error()}, err
		}
	}
	if cur, err := os.ReadFile(u.Path); err == nil && bytesEqual(cur, data) {
		return Result{Changed: false, Reason: "already current (" + shortSum(data) + ")"}, nil
	}
	if err := u.atomicWrite(data); err != nil {
		return Result{}, err
	}
	if u.Chown != nil {
		_ = u.Chown(u.Path)
	}
	if u.Reload != nil {
		if err := u.Reload(); err != nil {
			return Result{Changed: true, Reason: "written but sing-box reload failed: " + err.Error()}, err
		}
	}
	return Result{Changed: true, Reason: "updated to " + shortSum(data)}, nil
}

func (u Updater) atomicWrite(data []byte) error {
	if dir := filepath.Dir(u.Path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := u.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, u.Path)
}

func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }

func shortSum(data []byte) string {
	s := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", s[:4])
}

// ValidateSRS is the structural half of validation: reject an empty/tiny body
// or an HTML/JSON error page masquerading as a rule-set (a truncated or
// rate-limited GitHub response), so bad data never replaces good data.
func ValidateSRS(data []byte) error {
	if len(data) < 512 {
		return fmt.Errorf("suspiciously small (%d bytes) — not a real geoip rule-set", len(data))
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && (trimmed[0] == '<' || trimmed[0] == '{') {
		return fmt.Errorf("looks like an HTML/JSON error page, not a binary rule-set")
	}
	return nil
}
