package cli

import (
	"bytes"
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

func TestBackupPushStreamsEncryptedArchive(t *testing.T) {
	src := t.TempDir()
	run(t, "--dir", src, "init", "--ru-host", "ru", "--eu-host", "eu")
	run(t, "--dir", src, "client", "add", "bob")

	pf := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(pf, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "local.rdda")
	pushed := filepath.Join(t.TempDir(), "pushed.rdda")

	// `tee <file>` copies stdin to the file — a stand-in for scp/rclone/ssh.
	run(t, "--dir", src, "backup", "--out", out, "--passphrase-file", pf, "--push", "tee "+pushed)

	local, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := os.ReadFile(pushed)
	if err != nil {
		t.Fatalf("push did not write the archive: %v", err)
	}
	if !bytes.Equal(local, remote) {
		t.Fatal("pushed archive differs from the local backup")
	}
}

func TestBackupPushFailureAborts(t *testing.T) {
	src := t.TempDir()
	run(t, "--dir", src, "init", "--ru-host", "ru", "--eu-host", "eu")
	pf := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(pf, []byte("pw"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "should-not-exist.rdda")

	root := newRoot()
	root.SetArgs([]string{"--dir", src, "backup", "--out", out, "--passphrase-file", pf, "--push", "false"})
	if err := root.Execute(); err == nil {
		t.Fatal("a failing --push must make backup error")
	}
	// Push runs before the local write, so nothing is written on push failure.
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("local backup must not be written when the push fails")
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
