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

func TestConfigFingerprintDefault(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if err := s.SaveConfig(Config{RUHost: "ru.example"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.FP() != "firefox" {
		t.Fatalf("FP() default = %q, want firefox", got.FP())
	}
	got.Fingerprint = "safari"
	if got.FP() != "safari" {
		t.Fatalf("FP() = %q, want safari", got.FP())
	}
}

func TestDesyncRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{RUHost: "ru", Desync: Desync{Enabled: true, Profile: "fake,split2", Ports: []int{443}}}
	if err := s.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Desync.Enabled || got.Desync.Profile != "fake,split2" || got.Desync.Ports[0] != 443 {
		t.Fatalf("desync round-trip: %+v", got.Desync)
	}
}

func TestAlertRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := Config{RUHost: "ru", Alert: Alert{Enabled: true, Email: "ops@x", Command: "msmtp", CertWarnDays: 7}}
	if err := s.SaveConfig(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Alert.Enabled || got.Alert.Email != "ops@x" || got.Alert.Command != "msmtp" || got.Alert.CertWarnDays != 7 {
		t.Fatalf("alert round-trip: %+v", got.Alert)
	}
}
