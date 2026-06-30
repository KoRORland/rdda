package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

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
	u := &Updater{
		arch:       "amd64",
		current:    current,
		binPath:    bin,
		resolveTag: func() (string, error) { return latest, nil },
		fetch:      func(tag, arch string) ([]byte, string, error) { return []byte(tag), sha256hex([]byte(tag)), nil },
		restart:    func(string) error { return nil },
		isActive:   func(string) bool { return true },
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
	u.fetch = func(string, string) ([]byte, string, error) { return []byte("v0.3.0"), "deadbeef", nil }
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
