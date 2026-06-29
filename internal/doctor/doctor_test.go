package doctor

import (
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
	d.httpProbe = func(string) (int, time.Time, error) { return 200, time.Time{}, nil }
	d.egress = func([]byte, string) (bool, error) { return true, nil }
	d.cfInfo = func(string) (int, error) { return 2, nil }
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

func names(cs []Check) map[string]string {
	m := map[string]string{}
	for _, c := range cs {
		m[c.Name] = c.Detail
	}
	return m
}
