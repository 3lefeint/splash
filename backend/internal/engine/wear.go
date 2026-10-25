package engine

import (
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wear"
)

const (
	// baseLaps and recentLaps are the number of clean laps averaged for the start of a tyre set and for now.
	baseLaps   = 3
	recentLaps = 3
	// minTrendLaps is the number of clean laps on a set from which a trend and the change are shown.
	minTrendLaps = 4
	// minCoverage is the share of a lap that must have been measured for it to count.
	minCoverage = 0.9
	// newSetJump is the rise of the average wear value between two laps that marks a new set of tyres.
	newSetJump = 0.05
)

// WearStats says where the tyres of the driven car lose their life and whether that gets better or worse.
// All wear values are percentage points of a tyre per lap.
type WearStats struct {
	Segments int `json:"segments"`
	// Laps is the number of clean laps on the set that went into the figures.
	Laps int `json:"laps"`
	// PreviousSet is true while the current set has no clean lap yet and the figures are those of the set before.
	PreviousSet bool `json:"previous_set,omitempty"`
	// Recent is the loss per lap and track section by wheel (fl, fr, rl, rr), over the latest clean laps.
	Recent [][]float64 `json:"recent"`
	// Base is the same for the first clean laps of the set. Empty until the set has enough laps.
	Base [][]float64 `json:"base,omitempty"`
	// PerLap is the average loss of the four wheels for every clean lap of the set.
	PerLap []LapLoss `json:"per_lap"`
	// PerWheel is the recent loss per lap of each wheel.
	PerWheel []float64 `json:"per_wheel"`
	// TrendPct is the recent loss per lap relative to the start of the set; positive means the tyres wear faster.
	TrendPct *float64 `json:"trend_pct"`
}

// LapLoss is the average wear lost by one lap.
type LapLoss struct {
	Lap  int     `json:"lap"`
	Loss float64 `json:"loss"`
}

// wearStats works on the stored laps of the driven car. It looks at the current set of tyres and, while that has
// no clean lap yet (right after a stop), at the set before. It returns nil while there is no clean lap at all.
func wearStats(laps []store.LapRecord, wears []wear.LapWear) *WearStats {
	byLap := map[int]store.LapRecord{}
	for _, l := range laps {
		byLap[l.Lap.Lap] = l
	}
	bounds := setBoundaries(laps) // the last lap before each set; the first set starts after lap 0
	for i := len(bounds) - 1; i >= 0; i-- {
		lo, hi := bounds[i], int(^uint(0)>>1)
		if i+1 < len(bounds) {
			hi = bounds[i+1]
		}
		var clean []wear.LapWear
		for _, w := range wears {
			l, ok := byLap[w.Lap]
			if !ok || w.Lap <= lo || w.Lap > hi || w.Coverage < minCoverage {
				continue
			}
			if l.InLap || l.OutLap || l.FCY {
				continue
			}
			clean = append(clean, w)
		}
		if len(clean) > 0 {
			st := summarize(clean)
			st.PreviousSet = i != len(bounds)-1
			return st
		}
	}
	return nil
}

func summarize(clean []wear.LapWear) *WearStats {
	st := &WearStats{Segments: wear.Segments, Laps: len(clean)}
	for _, w := range clean {
		st.PerLap = append(st.PerLap, LapLoss{Lap: w.Lap, Loss: mean4(w) * 100})
	}
	// With 4 or 5 clean laps the start of the set and now are the first and last two laps, from 6 on three each.
	k := recentLaps
	trend := len(clean) >= minTrendLaps
	if trend {
		k = min(baseLaps, len(clean)/2)
	}
	recent := clean[max(len(clean)-k, 0):]
	st.Recent = average(recent)
	st.PerWheel = make([]float64, 4)
	for i, row := range st.Recent {
		for _, v := range row {
			st.PerWheel[i] += v
		}
	}
	if trend {
		base := clean[:k]
		st.Base = average(base)
		var b, r float64
		for _, w := range base {
			b += mean4(w)
		}
		for _, w := range recent {
			r += mean4(w)
		}
		b /= float64(len(base))
		r /= float64(len(recent))
		if b > 0 {
			t := (r/b - 1) * 100
			st.TrendPct = &t
		}
	}
	return st
}

// setBoundaries returns, for every set of tyres, the last lap before it: 0 for the first set and the lap before
// each lap on which the average wear value rose.
func setBoundaries(laps []store.LapRecord) []int {
	bounds := []int{0}
	prev, havePrev := 0.0, false
	for _, l := range laps {
		if len(l.Wear) != 4 {
			continue
		}
		avg := (l.Wear[0] + l.Wear[1] + l.Wear[2] + l.Wear[3]) / 4
		if havePrev && avg > prev+newSetJump {
			bounds = append(bounds, l.Lap.Lap-1)
		}
		prev, havePrev = avg, true
	}
	return bounds
}

func mean4(w wear.LapWear) float64 {
	var sum float64
	for i := range w.Loss {
		for _, v := range w.Loss[i] {
			sum += v
		}
	}
	return sum / 4
}

// average returns the mean loss per wheel and section over the given laps in percentage points.
func average(ws []wear.LapWear) [][]float64 {
	out := make([][]float64, 4)
	for i := range out {
		out[i] = make([]float64, wear.Segments)
		for _, w := range ws {
			for s, v := range w.Loss[i] {
				out[i][s] += v * 100 / float64(len(ws))
			}
		}
	}
	return out
}
