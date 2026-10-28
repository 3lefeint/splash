package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// saveEvery is the shortest time between two writes of the pit lane.
const saveEvery = 15 * time.Second

// PitRecorder learns the pit lane of the track from the scoring stream and stores it.
type PitRecorder struct {
	Store store.Store
	Log   *slog.Logger

	t      pitlane.Tracker
	mu     sync.Mutex
	loaded map[string]bool
	saved  time.Time
	dirty  bool
}

// NewPitRecorder returns a recorder; a nil logger discards logs.
func NewPitRecorder(st store.Store, log *slog.Logger) *PitRecorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &PitRecorder{Store: st, Log: log, loaded: map[string]bool{}}
}

// Observe processes one scoring sample.
func (r *PitRecorder) Observe(ctx context.Context, sc wire.Scoring) {
	if sc.Track == "" {
		return
	}
	r.mu.Lock()
	first := !r.loaded[sc.Track]
	r.loaded[sc.Track] = true
	r.mu.Unlock()
	if first {
		if l, ok, err := r.Store.GetPitLane(ctx, sc.Track); err != nil {
			r.Log.Warn("pit lane lookup", "err", err)
		} else if ok {
			r.t.Load(l)
		}
	}
	changed := r.t.Observe(sc)
	r.mu.Lock()
	r.dirty = r.dirty || changed
	due := r.dirty && time.Since(r.saved) >= saveEvery
	if due {
		r.dirty, r.saved = false, time.Now()
	}
	r.mu.Unlock()
	if due {
		if l := r.t.Lane(sc.Track); l != nil {
			if err := r.Store.SavePitLane(ctx, *l); err != nil {
				r.Log.Warn("pit lane save", "err", err)
			}
		}
	}
}

// Lane returns what is known about the pit lane of the track, or nil.
func (r *PitRecorder) Lane(track string) *pitlane.Lane { return r.t.Lane(track) }

// StoppedAt returns the box the car last stopped at, or nil.
func (r *PitRecorder) StoppedAt(vehicle string, id int) *pitlane.Point {
	return r.t.StoppedAt(vehicle, id)
}
