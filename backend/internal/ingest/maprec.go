package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// mapCheckTTL is how long the answer "this track has a map" or "has none" is reused.
const mapCheckTTL = 5 * time.Second

// MapRecorder builds the outline of a track from the scoring stream and stores it. It stops looking at
// a track as soon as a map exists.
type MapRecorder struct {
	Store store.Store
	Log   *slog.Logger

	b       *trackmap.Builder
	mu      sync.Mutex
	session string
	track   string
	length  float64
	checked map[string]mapCheck
}

type mapCheck struct {
	at  time.Time
	has bool
}

// NewMapRecorder returns a recorder; a nil logger discards logs.
func NewMapRecorder(st store.Store, log *slog.Logger) *MapRecorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &MapRecorder{Store: st, Log: log, b: trackmap.NewBuilder(), checked: map[string]mapCheck{}}
}

// Observe processes one scoring sample.
func (r *MapRecorder) Observe(ctx context.Context, sessionID string, sc wire.Scoring) {
	if sc.Track == "" || sc.TrackLength <= 0 {
		return
	}
	r.mu.Lock()
	if r.session != sessionID {
		r.session = sessionID
		r.b.Reset()
	}
	r.track, r.length = sc.Track, sc.TrackLength
	c, seen := r.checked[sc.Track]
	r.mu.Unlock()

	if !seen || time.Since(c.at) > mapCheckTTL {
		_, has, err := r.Store.GetTrackMap(ctx, sc.Track)
		if err != nil {
			r.Log.Warn("track map lookup", "err", err)
			return
		}
		c = mapCheck{time.Now(), has}
		r.mu.Lock()
		r.checked[sc.Track] = c
		r.mu.Unlock()
	}
	if c.has {
		return
	}
	if m := r.b.Observe(sc); m != nil {
		if err := r.Store.SaveTrackMap(ctx, *m); err != nil {
			r.Log.Warn("track map save", "err", err)
			return
		}
		r.Log.Info("track map recorded", "track", m.Track, "length", m.Length)
		r.mu.Lock()
		r.checked[sc.Track] = mapCheck{time.Now(), true}
		r.mu.Unlock()
	}
}

// Progress returns the outline recorded so far for track, or nil when a map exists already or nothing was recorded.
func (r *MapRecorder) Progress(track string) *trackmap.Progress {
	r.mu.Lock()
	t, length, c := r.track, r.length, r.checked[track]
	r.mu.Unlock()
	if t != track || c.has {
		return nil
	}
	return r.b.Partial(track, length)
}
