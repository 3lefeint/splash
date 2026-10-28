package ratelimit

import (
	"testing"
	"time"
)

func TestBurstThenRefill(t *testing.T) {
	now := time.Now()
	l := New(60, 3) // one a second, bursts of 3
	l.SetClock(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("burst %d refused", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 || wait > time.Second+time.Millisecond {
		t.Fatalf("ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("another key must have its own bucket")
	}
	now = now.Add(1100 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("not refilled")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("only one token should have come back")
	}
}

func TestDisabledAndNilLimiterAllowEverything(t *testing.T) {
	for _, l := range []*Limiter{nil, New(0, 0)} {
		for i := 0; i < 1000; i++ {
			if ok, _ := l.Allow("a"); !ok {
				t.Fatal("refused")
			}
		}
	}
}

func TestIdleBucketsAreForgotten(t *testing.T) {
	now := time.Now()
	l := New(60, 1)
	l.SetClock(func() time.Time { return now })
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	now = now.Add(10 * time.Minute)
	l.Allow("d")
	if n := len(l.buckets); n != 1 {
		t.Fatalf("%d buckets kept", n)
	}
}
