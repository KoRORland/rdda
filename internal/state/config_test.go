package state

import (
	"fmt"
	"strings"
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

func TestNfqwsArgs_AlwaysHasFooling(t *testing.T) {
	// Defaults: a fooling method must always be present (the G2 fix).
	got := Desync{}.NfqwsArgs()
	if !strings.Contains(got, "--dpi-desync-fooling=badsum") {
		t.Errorf("default args missing fooling method: %q", got)
	}
	if !strings.Contains(got, "--dpi-desync=fake,split2") {
		t.Errorf("default args missing profile: %q", got)
	}
	// Custom profile + fooling are honoured; fooling is never dropped.
	custom := Desync{Profile: "fakedsplit", Fooling: "md5sig"}.NfqwsArgs()
	if !strings.Contains(custom, "--dpi-desync=fakedsplit") || !strings.Contains(custom, "--dpi-desync-fooling=md5sig") {
		t.Errorf("custom args wrong: %q", custom)
	}
}

func TestStructVariance_DefaultsWhenNoUUID(t *testing.T) {
	c := Config{} // no TunnelUUID
	if c.MuxProtocol() != "h2mux" || c.MuxStreams() != 8 {
		t.Errorf("empty-UUID defaults wrong: proto=%q streams=%d", c.MuxProtocol(), c.MuxStreams())
	}
	if order := c.RuleOrder(3); order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Errorf("empty-UUID RuleOrder must be identity, got %v", order)
	}
}

func TestStructVariance_DeterministicInRangeAndVaries(t *testing.T) {
	c1 := Config{TunnelUUID: "11111111-1111-1111-1111-111111111111"}
	c2 := Config{TunnelUUID: "22222222-2222-2222-2222-222222222222"}

	// Deterministic: same UUID ⇒ same choice across calls/renders.
	if c1.MuxProtocol() != c1.MuxProtocol() || c1.MuxStreams() != c1.MuxStreams() {
		t.Fatal("derivation must be deterministic")
	}
	// Every derived value stays end-to-end valid.
	validProto := map[string]bool{"h2mux": true, "smux": true, "yamux": true}
	for _, c := range []Config{c1, c2} {
		if !validProto[c.MuxProtocol()] {
			t.Errorf("invalid mux protocol %q", c.MuxProtocol())
		}
		if s := c.MuxStreams(); s < 4 || s > 16 {
			t.Errorf("max_streams %d out of the 4..16 range", s)
		}
		order := c.RuleOrder(3)
		seen := map[int]bool{}
		for _, i := range order {
			seen[i] = true
		}
		if len(order) != 3 || !seen[0] || !seen[1] || !seen[2] {
			t.Errorf("RuleOrder(3) not a permutation of 0..2: %v", order)
		}
	}

	// Varies across deployments: at least one structural axis differs between
	// two distinct UUIDs (the whole point — no fleet-wide monoculture).
	diff := c1.MuxProtocol() != c2.MuxProtocol() ||
		c1.MuxStreams() != c2.MuxStreams() ||
		fmt.Sprint(c1.RuleOrder(3)) != fmt.Sprint(c2.RuleOrder(3))
	if !diff {
		t.Error("two distinct deployments produced identical structural params")
	}
}
