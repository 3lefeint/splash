package engine

import (
	"math"
	"slices"
	"sync"

	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	// localYellow is the value of an entry of sector_flags while that sector has a local yellow; the sectors without
	// one report 11. Seen in a capture in which a car stood still in a sector; the plugin does not document it.
	localYellow = 1
	// stoppedSpeed is the speed in m/s (about 11 km/h) below which a car in a yellow sector counts as a candidate for
	// the cause of the yellow.
	stoppedSpeed = 3.0
	// motionMaxAge is how long in game seconds the newest sample of a car may be old for its speed to still count.
	motionMaxAge = 3.0
)

// yellowInfo is what the sector flags say. The game sets only the sector flag; the per-car flag fields stay unset, so
// the cars are judged by their sector and speed.
type yellowInfo struct {
	// Sectors are the sectors (1 to 3) with a local yellow, never nil.
	Sectors []int
	// causes holds the keys of the cars that are candidates for the cause of the yellow: in a flagged sector, not in
	// the pits, slower than stoppedSpeed. It is a candidate, not a proof: other slow cars in the same sector look the
	// same. Our own car is never one.
	causes map[string]bool
}

// motionLog derives the speed of every car from its lap distance in the last two distinct scoring samples, so no
// extra field has to be sent for it.
type motionLog struct {
	mu sync.Mutex
	m  map[string]*motion
}

type motion struct {
	dist, et         float64
	prevDist, prevET float64
	hasPrev          bool
}

func (l *motionLog) observe(sc *wire.Scoring, et float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*motion{}
	}
	for _, v := range sc.Vehicles {
		if v.LapDist == nil {
			continue
		}
		k := carKey(v.VehicleName, v.CarID)
		m := l.m[k]
		switch {
		case m == nil || et < m.et-1: // new car, or the game time went back (a restarted session)
			l.m[k] = &motion{dist: *v.LapDist, et: et}
		case et-m.et >= 0.3: // a new sample, not the same one read again
			m.prevDist, m.prevET, m.hasPrev = m.dist, m.et, true
			m.dist, m.et = *v.LapDist, et
		}
	}
}

// speed returns the speed of a car in m/s, false when it cannot be told.
func (l *motionLog) speed(key string, length, now float64) (float64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.m[key]
	if m == nil || !m.hasPrev || now-m.et > motionMaxAge {
		return 0, false
	}
	dt := m.et - m.prevET
	if dt < 0.3 || dt > 5 {
		return 0, false
	}
	d := m.dist - m.prevDist
	if length > 0 {
		if d < -length/2 { // across the start line
			d += length
		} else if d > length/2 {
			d -= length
		}
	}
	return math.Abs(d) / dt, true
}

// yellow works out the local yellows of the scoring sample. It does not touch the full course yellow (isFCY).
func (e *Engine) yellow(sc *wire.Scoring, ourID int, ourName string, et float64) yellowInfo {
	e.motion.observe(sc, et)
	y := yellowInfo{Sectors: []int{}, causes: map[string]bool{}}
	for i, f := range sc.SectorFlags {
		if f == localYellow {
			y.Sectors = append(y.Sectors, i+1)
		}
	}
	for _, v := range sc.Vehicles {
		if v.Sector == nil || !slices.Contains(y.Sectors, *v.Sector) || v.InPits || (v.CarID == ourID && v.VehicleName == ourName) {
			continue
		}
		if sp, ok := e.motion.speed(carKey(v.VehicleName, v.CarID), sc.TrackLength, et); ok && sp < stoppedSpeed {
			y.causes[carKey(v.VehicleName, v.CarID)] = true
		}
	}
	return y
}
