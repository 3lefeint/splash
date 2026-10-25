package strategy

import (
	"errors"
	"sort"
)

// ErrNoCurrentStint is returned when stints are left to schedule but the running stint is unknown.
var ErrNoCurrentStint = errors.New("strategy: current stint required while stints remain")

// PlannedStint is one row of the manually entered stint plan.
type PlannedStint struct {
	Driver   string
	Duration float64 // seconds
}

// ActualStint is a completed stint; times are game elapsed time in seconds.
type ActualStint struct {
	Driver     string
	Start, End float64
}

// CurrentStint is the running stint. ExpectedEnd comes from the fuel forecast
// (laps to stop times lap time).
type CurrentStint struct {
	Start, ExpectedEnd float64
}

// PlanInput is everything needed to recompute the plan. Stint number n (0-based)
// belongs to Planned[n]: the first len(Done) are completed, the next one is the
// current stint, the rest are still to be scheduled.
type PlanInput struct {
	Planned  []PlannedStint
	Done     []ActualStint
	Current  *CurrentStint
	RaceEnd  float64 // ET at which the race ends
	PitLoss  float64 // seconds between two stints
	MaxStint float64 // longest possible stint (fuel limit); 0 = no limit
}

// PlanEntry is one stint of the recomputed plan.
type PlanEntry struct {
	No         int     `json:"no"` // 1-based
	Driver     string  `json:"driver"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Done       bool    `json:"done"`
	Current    bool    `json:"current"`
	DriverSwap bool    `json:"driver_swap"` // driver differs from the previous stint
}

// PlanResult is the recomputed plan.
type PlanResult struct {
	Entries []PlanEntry
	// Shortfall is the driving time the remaining stints cannot cover because
	// of MaxStint; the plan is short of this many seconds at the finish.
	Shortfall float64
	// CurrentDeviation is the expected end of the current stint minus its
	// planned end (start plus planned duration); positive means later than planned.
	CurrentDeviation float64
}

// Reschedule recomputes the plan from the real stint times. Completed stints keep
// their actual times, the current stint ends at its fuel forecast, and the
// remaining stints are scaled proportionally to their planned duration so that
// they fill the time up to RaceEnd, capped at MaxStint.
func Reschedule(in PlanInput) (PlanResult, error) {
	var res PlanResult
	nDone := len(in.Done)
	if nDone > len(in.Planned) {
		nDone = len(in.Planned)
	}
	add := func(e PlanEntry) {
		if n := len(res.Entries); n > 0 {
			e.DriverSwap = res.Entries[n-1].Driver != e.Driver
		}
		res.Entries = append(res.Entries, e)
	}
	for i := 0; i < nDone; i++ {
		a := in.Done[i]
		add(PlanEntry{No: i + 1, Driver: in.Planned[i].Driver, Start: a.Start, End: a.End, Done: true})
	}
	if nDone == len(in.Planned) {
		return res, nil
	}
	if in.Current == nil {
		return res, ErrNoCurrentStint
	}
	cur := in.Planned[nDone]
	add(PlanEntry{No: nDone + 1, Driver: cur.Driver, Start: in.Current.Start, End: in.Current.ExpectedEnd, Current: true})
	res.CurrentDeviation = in.Current.ExpectedEnd - (in.Current.Start + cur.Duration)

	rest := in.Planned[nDone+1:]
	if len(rest) == 0 {
		return res, nil
	}
	start := in.Current.ExpectedEnd + in.PitLoss
	available := in.RaceEnd - start - in.PitLoss*float64(len(rest)-1)
	durations, shortfall := scale(rest, available, in.MaxStint)
	res.Shortfall = shortfall
	for i, p := range rest {
		add(PlanEntry{No: nDone + 2 + i, Driver: p.Driver, Start: start, End: start + durations[i]})
		start += durations[i] + in.PitLoss
	}
	return res, nil
}

// scale distributes available seconds over the stints proportionally to their
// planned duration. Stints that would exceed max are capped and the rest is
// rescaled; shortfall is what remains when every stint is capped.
func scale(stints []PlannedStint, available, max float64) (durations []float64, shortfall float64) {
	durations = make([]float64, len(stints))
	if available < 0 {
		available = 0
	}
	free := make([]int, len(stints))
	for i := range free {
		free[i] = i
	}
	for len(free) > 0 {
		var sum float64
		for _, i := range free {
			sum += stints[i].Duration
		}
		if sum <= 0 {
			break
		}
		f := available / sum
		var over []int
		for _, i := range free {
			if max > 0 && stints[i].Duration*f > max {
				over = append(over, i)
			}
		}
		if len(over) == 0 {
			for _, i := range free {
				durations[i] = stints[i].Duration * f
			}
			return durations, 0
		}
		for _, i := range over {
			durations[i] = max
			available -= max
		}
		free = without(free, over)
	}
	return durations, available
}

func without(all, drop []int) []int {
	sort.Ints(drop)
	var out []int
	for _, v := range all {
		if k := sort.SearchInts(drop, v); k < len(drop) && drop[k] == v {
			continue
		}
		out = append(out, v)
	}
	return out
}
