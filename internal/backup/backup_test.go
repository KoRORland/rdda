package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func seedState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "clients"), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "config.yaml"), "ru_host: ru\npull_token: secret\n")
	mustWrite(t, filepath.Join(dir, "clients", "alice.json"), `{"uuid":"u1"}`)
	mustWrite(t, filepath.Join(dir, "clients", "bob.json"), `{"uuid":"u2"}`)
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRestoreRoundTrip(t *testing.T) {
	src := seedState(t)
	arc, err := Create(src, "pw")
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "restored") // does not exist yet
	if err := Restore(arc, "pw", dst, false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"config.yaml", "clients/alice.json", "clients/bob.json"} {
		want, _ := os.ReadFile(filepath.Join(src, rel))
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s mismatch", rel)
		}
	}
}

func TestRestoreRefusesOverwrite(t *testing.T) {
	arc, _ := Create(seedState(t), "pw")
	dst := seedState(t) // already has config.yaml
	if err := Restore(arc, "pw", dst, false); err == nil {
		t.Fatal("restore must refuse to overwrite without force")
	}
	if err := Restore(arc, "pw", dst, true); err != nil {
		t.Fatalf("restore with force must succeed: %v", err)
	}
}

func TestRestoreWrongPassphraseWritesNothing(t *testing.T) {
	arc, _ := Create(seedState(t), "pw")
	dst := filepath.Join(t.TempDir(), "restored")
	if err := Restore(arc, "wrong", dst, true); err == nil {
		t.Fatal("wrong passphrase must fail")
	}
	if _, err := os.Stat(filepath.Join(dst, "config.yaml")); !os.IsNotExist(err) {
		t.Fatal("nothing should be written on a failed restore")
	}
}
