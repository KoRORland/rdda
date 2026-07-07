package geoipupdate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A big non-HTML blob passes the structural validator.
func srsBlob(b byte) []byte { return bytes.Repeat([]byte{b}, 2048) }

func newUpdater(t *testing.T, data []byte, fetchErr error) (*Updater, *int, *string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "geoip-ru.srs")
	reloads := 0
	status := ""
	u := &Updater{
		Path:     path,
		URL:      "https://example/geoip-ru.srs",
		Fetch:    func(string) ([]byte, error) { return data, fetchErr },
		Validate: ValidateSRS,
		Reload:   func() error { reloads++; return nil },
	}
	_ = status
	return u, &reloads, &status
}

func TestRun_WritesAndReloadsOnChange(t *testing.T) {
	u, reloads, _ := newUpdater(t, srsBlob('A'), nil)
	res, err := u.Run()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || *reloads != 1 {
		t.Fatalf("first run must write + reload: %+v reloads=%d", res, *reloads)
	}
	got, _ := os.ReadFile(u.Path)
	if len(got) != 2048 {
		t.Fatalf("file not written (%d bytes)", len(got))
	}
}

func TestRun_NoChangeNoReload(t *testing.T) {
	u, reloads, _ := newUpdater(t, srsBlob('A'), nil)
	if _, err := u.Run(); err != nil { // seed
		t.Fatal(err)
	}
	res, err := u.Run() // identical data again
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || *reloads != 1 {
		t.Fatalf("unchanged data must not reload: %+v reloads=%d", res, *reloads)
	}
}

// Fail-safe: a fetch error leaves an existing file untouched and does not reload.
func TestRun_FetchErrorKeepsOldFile(t *testing.T) {
	u, reloads, _ := newUpdater(t, srsBlob('A'), nil)
	_, _ = u.Run() // seed good data
	u.Fetch = func(string) ([]byte, error) { return nil, errors.New("network down") }
	res, err := u.Run()
	if err == nil || !res.Skipped {
		t.Fatalf("fetch failure must skip: %+v err=%v", res, err)
	}
	if *reloads != 1 { // still just the seed reload
		t.Fatalf("must not reload on fetch failure, reloads=%d", *reloads)
	}
	if got, _ := os.ReadFile(u.Path); len(got) != 2048 {
		t.Fatal("old geoip file must survive a failed fetch")
	}
}

// A truncated/HTML body must be rejected, not swapped in.
func TestRun_RejectsBadPayload(t *testing.T) {
	u, reloads, _ := newUpdater(t, srsBlob('A'), nil)
	_, _ = u.Run()
	u.Fetch = func(string) ([]byte, error) { return []byte("<html>rate limited</html>"), nil }
	res, err := u.Run()
	if err == nil || !res.Skipped || !strings.Contains(res.Reason, "validation") {
		t.Fatalf("bad payload must be rejected: %+v err=%v", res, err)
	}
	if got, _ := os.ReadFile(u.Path); got[0] != 'A' {
		t.Fatal("good data must not be overwritten by an error page")
	}
	if *reloads != 1 {
		t.Fatalf("no reload on rejected payload, reloads=%d", *reloads)
	}
}

func TestValidateSRS(t *testing.T) {
	if err := ValidateSRS(srsBlob('A')); err != nil {
		t.Errorf("valid blob rejected: %v", err)
	}
	for _, bad := range [][]byte{{}, []byte("tiny"), []byte("<html>x</html>"), append([]byte("  {json"), srsBlob('x')...)} {
		if err := ValidateSRS(bad); err == nil {
			t.Errorf("bad payload %q accepted", string(bad[:min(len(bad), 12)]))
		}
	}
}
