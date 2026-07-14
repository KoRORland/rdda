package ratelimit

import (
	"testing"
	"time"
)

func TestAllow_BurstThenBlock(t *testing.T) {
	l := New(3, 1)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip1") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.Allow("ip1") {
		t.Fatal("4th request over the burst must be blocked")
	}
}

func TestAllow_KeysAreIndependent(t *testing.T) {
	l := New(1, 1)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("distinct keys must not share a bucket")
	}
	if l.Allow("a") {
		t.Fatal("key a is now out of tokens")
	}
}

func TestAllow_RefillsOverTime(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(2, 1) // 1 token/sec
	l.now = func() time.Time { return now }

	if !l.Allow("ip") || !l.Allow("ip") {
		t.Fatal("burst of 2 should pass")
	}
	if l.Allow("ip") {
		t.Fatal("should be empty after the burst")
	}
	now = now.Add(1500 * time.Millisecond) // ~1.5 tokens back
	if !l.Allow("ip") {
		t.Fatal("should refill after time passes")
	}
	if l.Allow("ip") {
		t.Fatal("only ~0.5 token left, must block")
	}
}

func TestSweep_EvictsIdleBuckets(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(2, 1)
	l.now = func() time.Time { return now }

	l.Allow("ip") // creates a bucket
	if len(l.buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(l.buckets))
	}
	// Advance past the sweep interval and long enough for the bucket to fully
	// refill; the next Allow (a different key) should reclaim the idle one.
	now = now.Add(2 * time.Minute)
	l.Allow("other")
	if _, stale := l.buckets["ip"]; stale {
		t.Fatal("idle, fully-refilled bucket should have been evicted")
	}
}

func TestNew_ClampsBurst(t *testing.T) {
	l := New(0, 1)
	if !l.Allow("x") {
		t.Fatal("burst < 1 must be clamped to at least 1")
	}
}
