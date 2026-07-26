package doctor

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderAndAnyFail(t *testing.T) {
	cs := []Check{
		{Name: "units", Status: PASS, Detail: "ok"},
		{Name: "dest", Status: WARN, Detail: "skipped", Hint: "configure it"},
		{Name: "tunnel", Status: FAIL, Detail: "down", Hint: "restart"},
	}
	out := Render(cs)
	if !strings.Contains(out, "✓ units") || !strings.Contains(out, "⚠ dest") || !strings.Contains(out, "✗ tunnel") {
		t.Fatalf("glyphs:\n%s", out)
	}
	if !strings.Contains(out, "configure it") || !strings.Contains(out, "restart") {
		t.Fatalf("hints missing:\n%s", out)
	}
	if !AnyFail(cs) {
		t.Fatal("AnyFail must be true with a FAIL")
	}
	if AnyFail([]Check{{Status: PASS}, {Status: WARN}}) {
		t.Fatal("WARN-only must not be AnyFail")
	}
}

// fakeDoctor builds a Doctor with deterministic seams.
func fakeDoctor(dir string) *Doctor {
	d := New(dir)
	d.unitActive = func(string) bool { return true }
	d.dialDest = func(string, int) error { return nil }
	d.validDest = func(string, int) (bool, string) { return true, "" }
	d.httpProbe = func(string, string) (int, []byte, time.Time, error) {
		return 200, []byte(`{"outbounds":[]}`), time.Time{}, nil
	}
	d.egress = func([]byte, string) (bool, error) { return true, nil }
	d.cfInfo = func(string) (int, error) { return 2, nil }
	d.svcUser = func() (int, int, error) { return 1000, 1000, nil }
	d.statFile = func(string) (int, int, fs.FileMode, error) { return 1000, 1000, 0o600, nil }
	d.dirFiles = func(string) []string { return nil }
	d.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return d
}

func TestRoleDetectRU(t *testing.T) {
	dir := t.TempDir() // no config.yaml ⇒ RU
	cs := fakeDoctor(dir).Run("http://probe")
	if names(cs)["end-to-end egress"] == "" {
		t.Fatalf("RU run should include the egress check: %v", cs)
	}
}

func TestIsConfigJSON(t *testing.T) {
	if !isConfigJSON([]byte("  {\"a\":1}\n")) {
		t.Fatal("a JSON object (with surrounding whitespace) must pass")
	}
	for _, bad := range []string{"Hello World!", "[1,2]", "", "  ", "{oops"} {
		if isConfigJSON([]byte(bad)) {
			t.Fatalf("non-config body %q must fail", bad)
		}
	}
}

// A hostname mis-routed to a wrong origin answers 200 with a non-JSON body
// (the "Hello World!" trap); the control-channel check must FAIL, not pass.
func TestControlChannel_WrongOrigin200Fails(t *testing.T) {
	dir := t.TempDir() // no config.yaml ⇒ RU role
	if err := os.WriteFile(filepath.Join(dir, "pull.env"),
		[]byte("RDDA_PULL_FROM=https://sub.example/ru/config\nRDDA_PULL_TOKEN=t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := fakeDoctor(dir)
	d.httpProbe = func(string, string) (int, []byte, time.Time, error) {
		return 200, []byte("Hello World!"), time.Time{}, nil
	}
	for _, c := range d.Run("http://probe") {
		if c.Name == "control channel" {
			if c.Status != FAIL {
				t.Fatalf("wrong-origin 200 must FAIL, got status=%v detail=%q", c.Status, c.Detail)
			}
			return
		}
	}
	t.Fatal("no control channel check produced")
}

// writeRUSingbox writes a minimal RU singbox.json with a REALITY inbound whose
// handshake dest is destHost:443, so the RU "REALITY dest" check reaches the
// probe path.
func writeRUSingbox(t *testing.T, dir, destHost string) {
	t.Helper()
	cfg := `{"inbounds":[{"type":"vless","tls":{"enabled":true,"server_name":"` + destHost + `",
		"reality":{"enabled":true,"handshake":{"server":"` + destHost + `","server_port":443}}}}]}`
	if err := os.WriteFile(filepath.Join(dir, "singbox.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func destCheck(t *testing.T, cs []Check) Check {
	t.Helper()
	for _, c := range cs {
		if c.Name == "REALITY dest" {
			return c
		}
	}
	t.Fatal("no REALITY dest check produced")
	return Check{}
}

// A reachable dest that no longer validates as a genuine borrowed identity is a
// weak, more-detectable camouflage — WARN (not FAIL: the tunnel still works),
// pointing at discovery.
func TestRealityDest_ReachableButWeakWarns(t *testing.T) {
	dir := t.TempDir()
	writeRUSingbox(t, dir, "some-host.example")
	d := fakeDoctor(dir)
	d.validDest = func(string, int) (bool, string) { return false, "no HTTP/2 (ALPN h2)" }
	c := destCheck(t, d.Run("http://probe"))
	if c.Status != WARN || !strings.Contains(c.Detail, "weak as a REALITY dest") {
		t.Fatalf("expected WARN weak-dest, got status=%v detail=%q", c.Status, c.Detail)
	}
}

// The pre-discovery shared default is a fleet-wide signature; flag it even when
// it still validates.
func TestRealityDest_LegacySharedDefaultWarns(t *testing.T) {
	dir := t.TempDir()
	writeRUSingbox(t, dir, "addons.mozilla.org")
	d := fakeDoctor(dir) // validDest stubbed to ok
	c := destCheck(t, d.Run("http://probe"))
	if c.Status != WARN || !strings.Contains(c.Detail, "legacy shared default") {
		t.Fatalf("expected WARN legacy-default, got status=%v detail=%q", c.Status, c.Detail)
	}
}

func TestRealityDest_ValidatingPasses(t *testing.T) {
	dir := t.TempDir()
	writeRUSingbox(t, dir, "good-host.example")
	c := destCheck(t, fakeDoctor(dir).Run("http://probe")) // dialDest+validDest stubbed ok
	if c.Status != PASS || !strings.Contains(c.Detail, "validates") {
		t.Fatalf("expected PASS validates, got status=%v detail=%q", c.Status, c.Detail)
	}
}

func names(cs []Check) map[string]string {
	m := map[string]string{}
	for _, c := range cs {
		m[c.Name] = c.Detail
	}
	return m
}
