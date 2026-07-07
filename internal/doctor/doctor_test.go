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
	d.httpProbe = func(string) (int, []byte, time.Time, error) { return 200, []byte(`{"outbounds":[]}`), time.Time{}, nil }
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
	d.httpProbe = func(string) (int, []byte, time.Time, error) { return 200, []byte("Hello World!"), time.Time{}, nil }
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

func names(cs []Check) map[string]string {
	m := map[string]string{}
	for _, c := range cs {
		m[c.Name] = c.Detail
	}
	return m
}
