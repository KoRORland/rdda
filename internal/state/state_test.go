package state

import (
	"testing"
)

func sampleConfig() Config {
	return Config{
		RUHost: "ru.example.net", RUPort: 443,
		EUHost: "eu.example.net", EUPort: 443,
		ClientPath: "/cl", TunnelPath: "/tn",
		TunnelUUID: "11111111-1111-4111-8111-111111111111",
		SubBaseURL: "https://eu.example.net",
		IntlAllowDomains: []string{"wikipedia.org"},
		ClientReality: Reality{Target: "www.microsoft.com:443", ServerName: "www.microsoft.com", PrivateKey: "priv1", PublicKey: "pub1", ShortIDs: []string{"0011"}},
		TunnelReality: Reality{Target: "www.apple.com:443", ServerName: "www.apple.com", PrivateKey: "priv2", PublicKey: "pub2", ShortIDs: []string{"0022"}},
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := sampleConfig()
	if err := s.SaveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.RUHost != want.RUHost || got.TunnelUUID != want.TunnelUUID ||
		got.ClientReality.PublicKey != "pub1" || got.TunnelReality.ServerName != "www.apple.com" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.LoadConfig(); err == nil {
		t.Fatal("expected error loading missing config")
	}
}
