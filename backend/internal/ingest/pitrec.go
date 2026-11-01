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
	r := &PitRecorder{Store: st, Log: log, loaded: map[string]bool{}}
	r.t.OnEvent = r.logEvent
	return r
}

// logEvent writes a line for every change of the pit flag the tracker saw: the ones it counted at INFO, the ones it
// ignored (a car put into its box, samples too far apart, an odd pit state) at DEBUG with the reason.
func (r *PitRecorder) logEvent(e pitlane.Event) {
	dist := -1.0
	if e.Dist != nil {
		dist = *e.Dist
	}
	args := []any{"kind", e.Kind, "track", e.Track, "car", e.Car, "lap_dist", dist, "pit_state", e.State,
		"step_m", e.Step, "gap_s", e.Gap, "source", e.Source}
	if e.Accepted {
		r.Log.Info("pit lane event counted", args...)
		return
	}
	r.Log.Debug("pit lane event ignored", append(args, "reason", e.Reason)...)
}

// Observe processes one scoring sample of a source (an agent in a session) taken at game time et.
func (r *PitRecorder) Observe(ctx context.Context, src string, et float64, sc wire.Scoring) {
	if sc.Track == "" {
		return
	}
	r.mu.Lock()
	first := !r.loaded[sc.Track]
	r.loaded[sc.Track] = true
	r.mu.Unlock()
	if first {
		if l, ok, err := r.Store.GetPitLane(ctx, sc.Track); err != nil {
			r.Log.Error("pit lane lookup", "err", err)
		} else if ok {
			r.t.Load(l)
		}
	}
	changed := r.t.Observe(src, et, sc)
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
				r.Log.Error("pit lane save", "err", err)
			}
		}
	}
}

// Reset forgets what was learned about the pit lane of a track, in memory and in the database, so that it is learned
// again from the next passes.
func (r *PitRecorder) Reset(ctx context.Context, track string) error {
	r.t.Reset(track)
	r.mu.Lock()
	r.loaded[track] = true // do not load the old lane again
	r.dirty = false
	r.mu.Unlock()
	return r.Store.DeletePitLane(ctx, track)
}

// Lane returns what is known about the pit lane of the track, or nil.
func (r *PitRecorder) Lane(track string) *pitlane.Lane { return r.t.Lane(track) }

// StoppedAt returns the box the car last stopped at, or nil.
func (r *PitRecorder) StoppedAt(vehicle string, id int) *pitlane.Point {
	return r.t.StoppedAt(vehicle, id)
}
