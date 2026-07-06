package controlchannel

import (
	"strings"
	"testing"
)

func TestRenderEnv_DerivesBothURLsFromSubHost(t *testing.T) {
	out, err := RenderEnv("sub.example.com", "tok123")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"RDDA_PULL_FROM=https://sub.example.com/ru/config\n",
		"RDDA_HEALTH_TO=https://sub.example.com/ru/health\n",
		"RDDA_PULL_TOKEN=tok123\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderEnv_NormalizesHost(t *testing.T) {
	for _, in := range []string{
		"https://sub.example.com/",
		"sub.example.com/ru/config",
		"  sub.example.com  ",
	} {
		out, err := RenderEnv(in, "t")
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !strings.Contains(out, "RDDA_PULL_FROM=https://sub.example.com/ru/config\n") {
			t.Errorf("%q did not normalize to bare host:\n%s", in, out)
		}
	}
}

func TestRenderEnv_RequiresHostAndToken(t *testing.T) {
	if _, err := RenderEnv("", "t"); err == nil {
		t.Error("empty sub host must error")
	}
	if _, err := RenderEnv("sub.example.com", "  "); err == nil {
		t.Error("blank token must error")
	}
}
