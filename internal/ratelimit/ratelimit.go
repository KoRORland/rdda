// Package ratelimit is a small keyed token-bucket rate limiter used to put basic
// abuse controls on the token-checked RU control-channel endpoints (F-4). Each
// key (a client IP) gets its own bucket that refills over time; idle buckets are
// evicted so a flood of distinct keys cannot grow the map without bound. It is
// safe for concurrent use.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter hands out tokens per key. A key starts with `burst` tokens and refills
// at `perSec` tokens per second, capped at `burst`.
type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	burst      float64
	perSec     float64
	sweepEvery time.Duration
	lastSweep  time.Time
	now        func() time.Time // seam for tests
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a limiter allowing bursts of `burst` requests per key and a
// sustained rate of `perSec` per second. perSec <= 0 disables refill (pure
// burst). burst < 1 is treated as 1.
func New(burst int, perSec float64) *Limiter {
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		buckets:    make(map[string]*bucket),
		burst:      float64(burst),
		perSec:     perSec,
		sweepEvery: time.Minute,
		now:        time.Now,
	}
}

// Allow consumes one token for key and reports whether the request may proceed.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 && l.perSec > 0 {
		if b.tokens += elapsed * l.perSec; b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}

	l.sweepLocked(now)

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepLocked periodically drops buckets that have sat idle long enough to fully
// refill — such a bucket is indistinguishable from a fresh one, so removing it is
// lossless and bounds memory under many distinct keys. Caller holds l.mu.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < l.sweepEvery {
		return
	}
	l.lastSweep = now
	if l.perSec <= 0 {
		return // no refill: buckets never become "full again", so nothing to reclaim
	}
	for k, b := range l.buckets {
		if now.Sub(b.last).Seconds()*l.perSec >= l.burst {
			delete(l.buckets, k)
		}
	}
}
