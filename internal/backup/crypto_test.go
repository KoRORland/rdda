package backup

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	pt := []byte("hello rdda state \x00\x01\x02")
	arc, err := encrypt("correct horse", pt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decrypt("correct horse", arc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatal("round-trip mismatch")
	}
}

func TestWrongPassphrase(t *testing.T) {
	arc, _ := encrypt("right", []byte("secret"))
	if _, err := decrypt("wrong", arc); err == nil {
		t.Fatal("wrong passphrase must fail")
	}
}

func TestTamperedCiphertext(t *testing.T) {
	arc, _ := encrypt("pw", []byte("secret data here"))
	arc[len(arc)-1] ^= 0xff
	if _, err := decrypt("pw", arc); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}
}

func TestTamperedHeaderAAD(t *testing.T) {
	arc, _ := encrypt("pw", []byte("secret"))
	arc[20] ^= 0xff // a salt byte, inside the header used as AAD
	if _, err := decrypt("pw", arc); err == nil {
		t.Fatal("tampered header (AAD) must fail")
	}
}

func TestEmptyPassphrase(t *testing.T) {
	if _, err := encrypt("", []byte("x")); err == nil {
		t.Fatal("empty passphrase must fail")
	}
}

func TestBadMagic(t *testing.T) {
	junk := make([]byte, headerLen+16)
	if _, err := decrypt("pw", junk); err == nil {
		t.Fatal("bad magic must fail")
	}
}
