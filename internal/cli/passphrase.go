package cli

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/term"
)

// readPassphrase resolves the backup passphrase: $RDDA_BACKUP_PASSPHRASE, else
// --passphrase-file, else a no-echo TTY prompt. confirm=true prompts twice.
func readPassphrase(passFile string, confirm bool) (string, error) {
	if p := os.Getenv("RDDA_BACKUP_PASSPHRASE"); p != "" {
		return p, nil
	}
	if passFile != "" {
		b, err := os.ReadFile(passFile)
		if err != nil {
			return "", err
		}
		p := string(bytes.TrimRight(b, "\r\n"))
		if p == "" {
			return "", fmt.Errorf("passphrase file %s is empty", passFile)
		}
		return p, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("no passphrase: set $RDDA_BACKUP_PASSPHRASE or --passphrase-file (stdin is not a terminal)")
	}
	fmt.Fprint(os.Stderr, "Passphrase: ")
	b1, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	p1 := string(b1)
	if p1 == "" {
		return "", fmt.Errorf("empty passphrase")
	}
	if confirm {
		fmt.Fprint(os.Stderr, "Confirm passphrase: ")
		b2, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(b2) != p1 {
			return "", fmt.Errorf("passphrases do not match")
		}
	}
	return p1, nil
}
