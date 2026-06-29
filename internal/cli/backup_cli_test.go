package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	run(t, "--dir", src, "init", "--ru-host", "ru", "--eu-host", "eu")
	run(t, "--dir", src, "client", "add", "alice")

	pf := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(pf, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "b.rdda")
	run(t, "--dir", src, "backup", "--out", out, "--passphrase-file", pf)

	dst := filepath.Join(t.TempDir(), "restored")
	run(t, "--dir", dst, "restore", out, "--passphrase-file", pf)

	if _, err := os.Stat(filepath.Join(dst, "config.yaml")); err != nil {
		t.Fatal("config.yaml not restored")
	}
	if _, err := os.Stat(filepath.Join(dst, "clients", "alice.json")); err != nil {
		t.Fatal("client not restored")
	}
}

func TestRestoreWrongPassphraseFails(t *testing.T) {
	src := t.TempDir()
	run(t, "--dir", src, "init", "--ru-host", "ru", "--eu-host", "eu")
	good := filepath.Join(t.TempDir(), "good")
	_ = os.WriteFile(good, []byte("right"), 0o600)
	out := filepath.Join(t.TempDir(), "b.rdda")
	run(t, "--dir", src, "backup", "--out", out, "--passphrase-file", good)

	bad := filepath.Join(t.TempDir(), "bad")
	_ = os.WriteFile(bad, []byte("wrong"), 0o600)
	root := newRoot()
	root.SetArgs([]string{"--dir", filepath.Join(t.TempDir(), "r"), "restore", out, "--passphrase-file", bad})
	if err := root.Execute(); err == nil {
		t.Fatal("restore with wrong passphrase must error")
	}
}
