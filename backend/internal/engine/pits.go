package engine

import (
	"sort"

	"github.com/3lefeint/splash/backend/internal/store"
)

// fuelService is the amount of fuel in litres from which a stop counts as a refuelling stop.
const fuelService = 5.0

// StopRow is one pit stop of the driven car as measured.
type StopRow struct {
	No int `json:"no"`
	// Entry is the game time of the pit entry.
	Entry float64 `json:"entry"`
	// Lane is the time from pit entry to pit exit, Stopped the time standing in the box and Through the rest (the gross
	// time in the lane). RacePart is the time a car at race pace needs for the same stretch and Net = Through -
	// RacePart the time lost against racing past; both are nil while the length of the pit lane stretch is unknown.
	Lane    *float64 `json:"lane"`
	Stopped *float64 `json:"stopped"`
	// Work is the time until the last fuel or tyre change was done; Stopped may be longer (the driver waited).
	Work *float64 `json:"work"`
	// FuelTime is how long fuel was flowing.
	FuelTime *float64 `json:"fuel_time"`
	Through  *float64 `json:"through"`
	RacePart *float64 `json:"race_part"`
	Net      *float64 `json:"net"`
	// NetMissing says why there is no net value: in_progress, stand_time, track_length, lap_time, lane_unknown, entry_dist, exit_dist
	// or stretch (the learned distances do not make a pit lane). Empty when Net is set.
	NetMissing string   `json:"net_missing,omitempty"`
	Fuel       *float64 `json:"fuel"`
	Tyres      bool     `json:"tyres"`
	Swap       bool     `json:"swap"`
}

// PitSuggest holds parameter values derived from the measured stops. A value is nil when no stop allows it.
type PitSuggest struct {
	PitThrough *float64 `json:"pit_through"`
	RefuelRate *float64 `json:"refuel_rate"`
	DriverSwap *float64 `json:"driver_swap"`
}

// PitContext is what the stops are judged against: the parameters (for the known tyre change time), the length of the
// pit lane stretch along the lap (nil while unknown), the track length and the pace of the car.
type PitContext struct {
	Params      store.Params
	LaneStretch *float64
	// StretchMissing says why LaneStretch is nil: lane_unknown (no pit lane learned), entry_dist, exit_dist or
	// stretch. Empty means lane_unknown.
	StretchMissing string
	TrackLength    float64
	LapTime        float64
}

// racePart is the time in seconds a car at race pace needs for the pit lane stretch; false while it cannot be told.
func (c PitContext) racePart() (float64, bool) {
	if c.LaneStretch == nil || c.TrackLength <= 0 || c.LapTime <= 0 {
		return 0, false
	}
	return *c.LaneStretch * c.LapTime / c.TrackLength, true
}

// netMissing is the reason the lane loss cannot be worked out for a stop that has a standing time.
func (c PitContext) netMissing() string {
	switch {
	case c.TrackLength <= 0:
		return "track_length"
	case c.LapTime <= 0:
		return "lap_time"
	case c.LaneStretch == nil && c.StretchMissing != "":
		return c.StretchMissing
	case c.LaneStretch == nil:
		return "lane_unknown"
	}
	return ""
}

// PitStats are the measured stops of the session and what they suggest for the training parameters.
type PitStats struct {
	Rows    []StopRow   `json:"rows"`
	Suggest *PitSuggest `json:"suggest,omitempty"`
}

// pitStats works on the stops of the driven car. It returns nil while the car has not stopped.
func pitStats(stops []store.StopRecord, carID int, vehicle string, ctx PitContext) *PitStats {
	var rows []StopRow
	for _, s := range stops {
		if s.CarID != carID || s.VehicleName != vehicle {
			continue
		}
		r := StopRow{No: len(rows) + 1, Entry: s.EntryET, Fuel: s.FuelAdded, Tyres: s.TyresChanged, Swap: s.DriverSwap}
		if s.ExitET != nil {
			lane := *s.ExitET - s.EntryET
			r.Lane = &lane
			if s.Stopped > 0 && s.Stopped < lane {
				st, th := s.Stopped, lane-s.Stopped
				r.Stopped, r.Through = &st, &th
				if race, ok := ctx.racePart(); ok {
					net := th - race
					r.RacePart, r.Net = &race, &net
				} else {
					r.NetMissing = ctx.netMissing()
				}
				if s.Work > 0 {
					w := s.Work
					r.Work = &w
				}
				if s.FuelTime > 0 {
					f := s.FuelTime
					r.FuelTime = &f
				}
			}
		}
		if r.Net == nil && r.NetMissing == "" {
			r.NetMissing = "stand_time" // no standing time was measured, so there is no gross time either
			if r.Lane == nil {
				r.NetMissing = "in_progress" // the car has not left the pit lane yet
			}
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return nil
	}
	return &PitStats{Rows: rows, Suggest: suggest(rows, ctx)}
}

// served is the time the work in the box took: measured when known, else the time the car stood.
func served(r StopRow) float64 {
	if r.Work != nil {
		return *r.Work
	}
	return *r.Stopped
}

// suggest derives parameter values from the stops. The pit lane loss is the net time against racing past; a stop
// without it, or with a net time of 0 or less, gives no suggestion for it. The tyre change time is known and fixed, so
// it is not suggested.
func suggest(rows []StopRow, ctx PitContext) *PitSuggest {
	var through, refuel, swap []float64
	fuel := func(r StopRow) float64 {
		if r.Fuel == nil {
			return 0
		}
		return *r.Fuel
	}
	for _, r := range rows {
		if r.Stopped == nil {
			continue
		}
		if r.Net != nil && *r.Net > 0 {
			through = append(through, *r.Net)
		}
		f := fuel(r)
		switch {
		case f > fuelService && r.FuelTime != nil:
			refuel = append(refuel, f / *r.FuelTime) // fuel flow was measured, other work does not matter
		case f > fuelService && !r.Tyres && !r.Swap:
			refuel = append(refuel, f/served(r))
		case f <= fuelService && r.Swap && !r.Tyres:
			swap = append(swap, served(r))
		}
	}
	out := &PitSuggest{PitThrough: median(through), RefuelRate: median(refuel), DriverSwap: median(swap)}
	if *out == (PitSuggest{}) {
		return nil
	}
	return out
}

func median(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return &m
}
