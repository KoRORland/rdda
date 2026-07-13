// Package qr renders QR codes for subscription links: a half-block string for the
// operator's terminal and a PNG file to hand to the user.
package qr

import (
	qrcode "github.com/skip2/go-qrcode"
)

// Terminal returns a compact half-block ("▀"/"▄") QR of s, sized to render in a
// terminal. The false argument keeps the code on a light background (readable in
// both light and dark terminals) with a quiet-zone border.
func Terminal(s string) (string, error) {
	q, err := qrcode.New(s, qrcode.Medium)
	if err != nil {
		return "", err
	}
	return q.ToSmallString(false), nil
}

// PNG writes a QR of s to path (256px, medium error correction). The operator
// sends this image to the user, who scans it with Hiddify.
func PNG(s, path string) error {
	return qrcode.WriteFile(s, qrcode.Medium, 256, path)
}
