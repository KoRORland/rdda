package doctor

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceUserCanRead(t *testing.T) {
	const svcUID, svcGID = 1000, 1000
	cases := []struct {
		name       string
		fUID, fGID int
		mode       fs.FileMode
		want       bool
	}{
		{"owner readable", 1000, 1000, 0o600, true},
		{"owner no read bit", 1000, 1000, 0o200, false},
		{"root-owned 0600 (the crash-loop)", 0, 0, 0o600, false},
		{"root-owned but world-readable", 0, 0, 0o644, true},
		{"group readable", 0, 1000, 0o640, true},
		{"group no read bit", 0, 1000, 0o600, false},
	}
	for _, c := range cases {
		if got := serviceUserCanRead(c.fUID, c.fGID, c.mode, svcUID, svcGID); got != c.want {
			t.Errorf("%s: serviceUserCanRead = %v, want %v", c.name, got, c.want)
		}
	}
}

// A root-owned 0600 singbox.json must FAIL the permissions check — that's the
// exact ownership foot-gun that crash-looped rdda-singbox in the field.
func TestPermsCheck_RootOwnedConfigFails(t *testing.T) {
	dir := t.TempDir()
	// pretend singbox.json exists and is root-owned 0600, config.yaml fine.
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("ru_host: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := fakeDoctor(dir)
	d.statFile = func(path string) (int, int, fs.FileMode, error) {
		if filepath.Base(path) == "singbox.json" {
			return 0, 0, 0o600, nil // root:root 0600 → rdda (1000) can't read
		}
		return 1000, 1000, 0o600, nil
	}
	c := d.permsCheck("singbox.json", "config.yaml")
	if c.Status != FAIL {
		t.Fatalf("root-owned singbox.json must FAIL, got %v (%q)", c.Status, c.Detail)
	}
	if !strings.Contains(c.Hint, "chown rdda:rdda") {
		t.Errorf("hint should point at chown, got %q", c.Hint)
	}
}

func TestPermsCheck_NoServiceUserWarns(t *testing.T) {
	d := fakeDoctor(t.TempDir())
	d.svcUser = func() (int, int, error) { return 0, 0, os.ErrNotExist }
	if c := d.permsCheck("singbox.json"); c.Status != WARN {
		t.Fatalf("missing rdda user must WARN, got %v", c.Status)
	}
}
