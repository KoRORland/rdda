package qr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTerminal(t *testing.T) {
	s, err := Terminal("hiddify://import/https://eu.example.net/sub/tok#RDDA")
	if err != nil {
		t.Fatal(err)
	}
	if len(s) == 0 {
		t.Fatal("Terminal returned empty QR")
	}
}

func TestPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "granny.png")
	if err := PNG("hiddify://import/https://eu.example.net/sub/tok#RDDA", path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("PNG not written: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("PNG file is empty")
	}
}
