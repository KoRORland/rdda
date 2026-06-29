// Package backup creates and restores an encrypted archive of the EU node's
// source-of-truth state (config.yaml + clients/).
package backup

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	magic     = "RDDABAK1"
	formatVer = 1
	saltLen   = 16
	nonceLen  = chacha20poly1305.NonceSizeX // 24
	keyLen    = 32
	headerLen = 8 + 1 + 4 + 4 + 1 + saltLen + nonceLen // 58

	defTime    = 2
	defMemory  = 64 * 1024 // KiB = 64 MiB
	defThreads = 4
)

type header struct {
	time    uint32
	memory  uint32
	threads uint8
	salt    [saltLen]byte
	nonce   [nonceLen]byte
}

func (h header) marshal() []byte {
	b := make([]byte, 0, headerLen)
	b = append(b, magic...)
	b = append(b, formatVer)
	b = binary.BigEndian.AppendUint32(b, h.time)
	b = binary.BigEndian.AppendUint32(b, h.memory)
	b = append(b, h.threads)
	b = append(b, h.salt[:]...)
	b = append(b, h.nonce[:]...)
	return b
}

func parseHeader(b []byte) (header, error) {
	var h header
	if len(b) < headerLen {
		return h, errors.New("backup: file too short / not an RDDA backup")
	}
	if string(b[:8]) != magic {
		return h, errors.New("backup: bad magic / not an RDDA backup")
	}
	if b[8] != formatVer {
		return h, fmt.Errorf("backup: unsupported format version %d", b[8])
	}
	h.time = binary.BigEndian.Uint32(b[9:13])
	h.memory = binary.BigEndian.Uint32(b[13:17])
	h.threads = b[17]
	copy(h.salt[:], b[18:18+saltLen])
	copy(h.nonce[:], b[18+saltLen:headerLen])
	return h, nil
}

func deriveKey(passphrase string, h header) []byte {
	return argon2.IDKey([]byte(passphrase), h.salt[:], h.time, h.memory, h.threads, keyLen)
}

// encrypt seals plaintext, returning header||ciphertext. The header is the AEAD
// associated data so any tampering with it is detected on open.
func encrypt(passphrase string, plaintext []byte) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("backup: empty passphrase")
	}
	h := header{time: defTime, memory: defMemory, threads: defThreads}
	if _, err := rand.Read(h.salt[:]); err != nil {
		return nil, err
	}
	if _, err := rand.Read(h.nonce[:]); err != nil {
		return nil, err
	}
	hdr := h.marshal()
	aead, err := chacha20poly1305.NewX(deriveKey(passphrase, h))
	if err != nil {
		return nil, err
	}
	return aead.Seal(hdr, h.nonce[:], plaintext, hdr), nil
}

// decrypt opens an archive (header||ciphertext).
func decrypt(passphrase string, archive []byte) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("backup: empty passphrase")
	}
	h, err := parseHeader(archive)
	if err != nil {
		return nil, err
	}
	hdr := archive[:headerLen]
	aead, err := chacha20poly1305.NewX(deriveKey(passphrase, h))
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, h.nonce[:], archive[headerLen:], hdr)
	if err != nil {
		return nil, errors.New("backup: wrong passphrase or corrupt archive")
	}
	return pt, nil
}
