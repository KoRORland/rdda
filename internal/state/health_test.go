package state

import (
	"testing"
	"time"

	"github.com/KoRORland/rdda/internal/health"
)

func TestRUHealthRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.LoadRUHealth(); ok {
		t.Fatal("expected no beat initially")
	}
	in := RUHealth{Report: health.Report{SingboxActive: true, SingboxVersion: "1.13.14"}, ReceivedAt: time.Now().UTC()}
	if err := s.SaveRUHealth(in); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LoadRUHealth()
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if !got.SingboxActive || got.SingboxVersion != "1.13.14" || got.ReceivedAt.IsZero() {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
