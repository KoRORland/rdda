package state

import (
	"testing"
)

func TestConfigCloudflareRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := Config{
		RUHost: "ru.example", RUPort: 443, EUHost: "eu.example", EUPort: 443,
		Cloudflare: Cloudflare{
			TunnelHostname:  "tunnel.example.com",
			SubHostname:     "sub.example.com",
			TunnelID:        "abc-123",
			CredentialsFile: "/etc/cloudflared/abc-123.json",
		},
		PullToken: "tok-xyz",
	}
	if err := s.SaveConfig(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Cloudflare.TunnelHostname != "tunnel.example.com" || got.PullToken != "tok-xyz" {
		t.Fatalf("cloudflare/pulltoken did not round-trip: %+v", got)
	}
	if !got.CFEnabled() {
		t.Fatal("CFEnabled() should be true when TunnelHostname is set")
	}
}

func TestConfigCFDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if err := s.SaveConfig(Config{RUHost: "ru.example"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.CFEnabled() {
		t.Fatal("CFEnabled() must be false for an empty cloudflare block")
	}
}
