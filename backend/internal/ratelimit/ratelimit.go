// Package ratelimit limits how often one identity may do something. It is in memory and per process.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter is a token bucket per key: it holds up to Burst tokens and gains PerMinute tokens a minute.
type Limiter struct {
	PerMinute float64
	Burst     float64

	now     func() time.Time
	mu      sync.Mutex
	buckets map[string]*bucket
	sweep   time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// New returns a Limiter that allows perMinute events a minute with bursts of up to burst. A perMinute of 0 or less
// disables the limit.
func New(perMinute, burst int) *Limiter {
	return &Limiter{PerMinute: float64(perMinute), Burst: float64(max(burst, 1)), now: time.Now, buckets: map[string]*bucket{}}
}

// SetClock replaces the clock, for tests.
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

// Allow takes a token for key. When none is left it returns false and how long to wait for the next one.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	if l == nil || l.PerMinute <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.forget(now)
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: l.Burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.Burst, b.tokens+now.Sub(b.at).Seconds()*l.PerMinute/60)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.PerMinute * float64(time.Minute))
	return false, wait
}

// forget drops the buckets that have been full for a while, so the map does not grow with every identity ever seen.
func (l *Limiter) forget(now time.Time) {
	if now.Sub(l.sweep) < time.Minute {
		return
	}
	l.sweep = now
	idle := time.Duration(l.Burst/l.PerMinute*float64(time.Minute)) + time.Minute
	for k, b := range l.buckets {
		if now.Sub(b.at) > idle {
			delete(l.buckets, k)
		}
	}
}
