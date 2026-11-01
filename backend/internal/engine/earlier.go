package engine

import (
	"context"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
)

const (
	// earlierTTL is how long the laps of earlier sessions are reused before they are read again. A lap that someone
	// leaves out of the models counts from the next read on.
	earlierTTL = 15 * time.Second
	// minDriverLaps is the number of valid laps a driver needs before their own fuel consumption is used and not the
	// team's average.
	minDriverLaps = 3
)

// earlierCache holds the laps of earlier sessions for one track, car and running session.
type earlierCache struct {
	mu   sync.Mutex
	key  string
	at   time.Time
	laps []store.HistoryLap
}

// earlier returns the laps of the driven car from earlier sessions on this track, oldest first. When they cannot be
// read the models go on with the running session alone.
func (e *Engine) earlier(ctx context.Context, track, vehicle, session string) []store.HistoryLap {
	c := &e.past
	c.mu.Lock()
	defer c.mu.Unlock()
	key := track + "|" + vehicle + "|" + session
	if c.key == key && time.Since(c.at) < earlierTTL {
		return c.laps
	}
	laps, err := e.Store.HistoryLaps(ctx, track, vehicle, session)
	if err != nil {
		e.Log.Warn("laps of earlier sessions could not be read, the models use the running session", "err", err)
		return nil
	}
	c.key, c.at, c.laps = key, time.Now(), laps
	return laps
}

func records(hist []store.HistoryLap) []store.LapRecord {
	out := make([]store.LapRecord, len(hist))
	for i, h := range hist {
		out[i] = h.LapRecord
	}
	return out
}

// onlyDriver keeps the laps a driver drove. The driver is the name the game reports in the scoring.
func onlyDriver(hist []store.HistoryLap, driver string) []store.HistoryLap {
	if driver == "" {
		return hist
	}
	var out []store.HistoryLap
	for _, h := range hist {
		if h.Driver == driver {
			out = append(out, h)
		}
	}
	return out
}

// historyGroups splits the laps of earlier sessions into one group per session and car: lap numbers and tyre sets only
// mean something inside one. withWet is whether wet laps are to be left out.
func historyGroups(hist []store.HistoryLap, withWet bool) []fitGroup {
	type key struct {
		session string
		car     int
		vehicle string
	}
	var order []key
	laps := map[key][]store.HistoryLap{}
	for _, h := range hist {
		k := key{h.SessionID, h.CarID, h.VehicleName}
		if _, ok := laps[k]; !ok {
			order = append(order, k)
		}
		laps[k] = append(laps[k], h)
	}
	out := make([]fitGroup, 0, len(order))
	for _, k := range order {
		g := fitGroup{history: true}
		wet := map[int]bool{}
		for _, h := range laps[k] {
			g.laps = append(g.laps, h.LapRecord)
			if (h.Wetness != nil && *h.Wetness > wetLap) || (h.Rain != nil && *h.Rain > wetLap) {
				wet[h.Lap.Lap] = true
			}
		}
		g.bounds = setBoundaries(g.laps)
		if withWet {
			g.wet = func(lap int) bool { return wet[lap] }
		}
		out = append(out, g)
	}
	return out
}

// fitWithHistory fits the lap time model on the running session plus the laps of earlier sessions. Those are the laps of
// the current driver; when the model cannot be fitted with them for lack of laps, the laps of all drivers of the team.
func fitWithHistory(current fitGroup, earlier []store.HistoryLap, driver string, opt FitOptions) *Fit {
	if len(earlier) == 0 {
		return fitGroups([]fitGroup{current}, opt)
	}
	with := func(h []store.HistoryLap) *Fit {
		return fitGroups(append(historyGroups(h, opt.SkipWet), current), opt)
	}
	own := onlyDriver(earlier, driver)
	fit, scope := with(own), "driver"
	if driver == "" {
		scope = "team"
	}
	if fit.Laps < opt.MinLaps && len(own) < len(earlier) {
		if all := with(earlier); all.Laps > fit.Laps {
			fit, scope = all, "team"
		}
	}
	if fit.HistoryLaps > 0 {
		fit.Scope = scope
	}
	return fit
}

// lastDriverOf is the driver of the newest lap that names one.
func lastDriverOf(laps []store.LapRecord) string {
	for i := len(laps) - 1; i >= 0; i-- {
		if laps[i].Driver != "" {
			return laps[i].Driver
		}
	}
	return ""
}
