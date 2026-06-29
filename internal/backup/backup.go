package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Create returns an encrypted archive of config.yaml + clients/ under stateDir.
func Create(stateDir, passphrase string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := addFile(tw, stateDir, "config.yaml"); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "clients"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := addFile(tw, stateDir, "clients/"+e.Name()); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return encrypt(passphrase, buf.Bytes())
}

func addFile(tw *tar.Writer, root, rel string) error {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = tw.Write(b)
	return err
}

// Restore decrypts archive and writes config.yaml + clients/ into destDir. It
// refuses to overwrite an existing config.yaml unless force. It unpacks to a
// temp dir first, so a failure leaves any existing state untouched.
func Restore(archive []byte, passphrase, destDir string, force bool) error {
	if _, err := os.Stat(filepath.Join(destDir, "config.yaml")); err == nil {
		if !force {
			return fmt.Errorf("backup: %s/config.yaml already exists; pass --force to overwrite", destDir)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	plain, err := decrypt(passphrase, archive)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return fmt.Errorf("backup: bad gzip stream: %w", err)
	}
	tr := tar.NewReader(gz)

	tmp, err := os.MkdirTemp(filepath.Dir(destDir), ".rdda-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "clients"), 0o700); err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(filepath.FromSlash(hdr.Name))
		clean = filepath.ToSlash(clean)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return fmt.Errorf("backup: unsafe archive entry %q", hdr.Name)
		}
		if clean != "config.yaml" && !strings.HasPrefix(clean, "clients/") {
			return fmt.Errorf("backup: unexpected archive entry %q", hdr.Name)
		}
		const maxEntry = 8 << 20 // 8 MiB; RDDA state files are tiny
		if hdr.Size > maxEntry {
			return fmt.Errorf("backup: archive entry %q too large (%d bytes)", hdr.Name, hdr.Size)
		}
		b, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, filepath.FromSlash(clean)), b, 0o600); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tmp, "config.yaml"), filepath.Join(destDir, "config.yaml")); err != nil {
		return fmt.Errorf("backup: archive missing config.yaml: %w", err)
	}
	dstClients := filepath.Join(destDir, "clients")
	_ = os.RemoveAll(dstClients)
	return os.Rename(filepath.Join(tmp, "clients"), dstClients)
}
