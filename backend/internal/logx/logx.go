// Package logx sets up the backend log: the level from the environment, a scrubbing handler as the last line of
// defence against secrets, a throttle for lines that an attacker or a broken client could produce in floods, and the
// HTTP middleware that writes the request lines.
package logx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LevelVar is the environment variable that sets the log level.
const LevelVar = "SPLASH_LOG_LEVEL"

// ParseLevel turns "debug", "info", "warn" or "error" (any case) into a level; an empty string means info.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("%s must be debug, info, warn or error, got %q", LevelVar, s)
}

var (
	emailRe  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(\.[A-Za-z0-9\-]+)+`)
	bearerRe = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=\-]+`)
	// jwtRe matches a JWT (three base64url parts, the first two starting like JSON) wherever it shows up.
	jwtRe = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]*`)
	// urlAuthRe matches the credentials of a URL such as postgres://user:password@host.
	urlAuthRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://)[^/\s:@]*:[^/\s@]*@`)
)

// Scrub removes what must never be logged from a text: e-mail addresses, bearer tokens, JWTs and the password part of
// URLs. Call sites do not log such values on purpose; this catches the ones that arrive inside an error text.
func Scrub(s string) string {
	s = urlAuthRe.ReplaceAllString(s, "${1}<redacted>@")
	s = bearerRe.ReplaceAllString(s, "Bearer <redacted>")
	s = jwtRe.ReplaceAllString(s, "<jwt>")
	return emailRe.ReplaceAllString(s, "<email>")
}

func scrubAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(Scrub(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			a.Value = slog.StringValue(Scrub(err.Error()))
		}
	}
	return a
}

// New returns the logger of the backend: text lines on w at the given level, scrubbed.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: scrubAttr}))
}

// Discard is a logger that writes nothing.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// ShortID is a short hash of an identity such as an e-mail address, so lines about the same identity can be matched
// without the address being in the log.
func ShortID(identity string) string {
	h := sha256.Sum256([]byte(strings.ToLower(identity)))
	return hex.EncodeToString(h[:4])
}

// Throttle lets a kind of line through once per interval and counts what it held back; the next line that gets
// through reports the count as "suppressed". A flood of the same failure becomes one line per interval.
type Throttle struct {
	Interval time.Duration

	mu   sync.Mutex
	now  func() time.Time
	seen map[string]*throttled
}

type throttled struct {
	at         time.Time
	suppressed int
}

// NewThrottle returns a Throttle with the given interval.
func NewThrottle(interval time.Duration) *Throttle {
	return &Throttle{Interval: interval, now: time.Now, seen: map[string]*throttled{}}
}

// SetClock replaces the clock, for tests.
func (t *Throttle) SetClock(now func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = now
}

// Allow reports whether a line of this key may be written now, and how many were held back since the last one.
func (t *Throttle) Allow(key string) (ok bool, suppressed int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	e := t.seen[key]
	if e == nil {
		if len(t.seen) > 1000 { // keys are bounded by the call sites, but never let the map grow without limit
			t.seen = map[string]*throttled{}
		}
		t.seen[key] = &throttled{at: now}
		return true, 0
	}
	if now.Sub(e.at) < t.Interval {
		e.suppressed++
		return false, 0
	}
	n := e.suppressed
	e.at, e.suppressed = now, 0
	return true, n
}

// Log writes the line through the throttle; key says which flood it belongs to.
func (t *Throttle) Log(log *slog.Logger, level slog.Level, key, msg string, args ...any) {
	if ok, n := t.Allow(key); ok {
		if n > 0 {
			args = append(args, "suppressed", n)
		}
		log.Log(context.Background(), level, msg, args...)
	}
}
