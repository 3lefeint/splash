package engine

import (
	"math"
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
	// Lane is the time from pit entry to pit exit, Stopped the time standing in the box and Through the rest.
	Lane    *float64 `json:"lane"`
	Stopped *float64 `json:"stopped"`
	// Work is the time until the last fuel or tyre change was done; Stopped may be longer (the driver waited).
	Work *float64 `json:"work"`
	// FuelTime is how long fuel was flowing.
	FuelTime *float64 `json:"fuel_time"`
	Through  *float64 `json:"through"`
	Fuel     *float64 `json:"fuel"`
	Tyres    bool     `json:"tyres"`
	Swap     bool     `json:"swap"`
}

// PitSuggest holds parameter values derived from the measured stops. A value is nil when no stop allows it.
type PitSuggest struct {
	PitThrough *float64 `json:"pit_through"`
	RefuelRate *float64 `json:"refuel_rate"`
	TyreChange *float64 `json:"tyre_change"`
	DriverSwap *float64 `json:"driver_swap"`
	Parallel   *bool    `json:"parallel_service"`
}

// PitStats are the measured stops of the session and what they suggest for the training parameters.
type PitStats struct {
	Rows    []StopRow   `json:"rows"`
	Suggest *PitSuggest `json:"suggest,omitempty"`
}

// pitStats works on the stops of the driven car. It returns nil while the car has not stopped.
func pitStats(stops []store.StopRecord, carID int, vehicle string) *PitStats {
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
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		return nil
	}
	return &PitStats{Rows: rows, Suggest: suggest(rows)}
}

// served is the time the work in the box took: measured when known, else the time the car stood.
func served(r StopRow) float64 {
	if r.Work != nil {
		return *r.Work
	}
	return *r.Stopped
}

func suggest(rows []StopRow) *PitSuggest {
	var through, refuel, tyre, swap []float64
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
		through = append(through, *r.Through)
		f := fuel(r)
		switch {
		case f > fuelService && r.FuelTime != nil:
			refuel = append(refuel, f / *r.FuelTime) // fuel flow was measured, other work does not matter
		case f > fuelService && !r.Tyres && !r.Swap:
			refuel = append(refuel, f/served(r))
		case f <= fuelService && r.Tyres && !r.Swap:
			tyre = append(tyre, served(r))
		case f <= fuelService && r.Swap && !r.Tyres:
			swap = append(swap, served(r))
		}
	}
	out := &PitSuggest{PitThrough: median(through), RefuelRate: median(refuel), TyreChange: median(tyre), DriverSwap: median(swap)}
	if out.RefuelRate != nil && out.TyreChange != nil {
		out.Parallel = parallel(rows, *out.RefuelRate, *out.TyreChange, fuel)
	}
	if *out == (PitSuggest{}) {
		return nil
	}
	return out
}

// parallel decides from stops with fuel and new tyres together whether refuelling runs next to the tyre change:
// the time standing is then the longer of the two, otherwise their sum. It returns nil without such stops.
func parallel(rows []StopRow, rate, tyre float64, fuel func(StopRow) float64) *bool {
	var par, ser float64
	n := 0
	for _, r := range rows {
		if r.Stopped == nil || !r.Tyres || r.Swap || fuel(r) <= fuelService {
			continue
		}
		f := fuel(r) / rate
		par += math.Abs(served(r) - math.Max(f, tyre))
		ser += math.Abs(served(r) - (f + tyre))
		n++
	}
	if n == 0 {
		return nil
	}
	v := par < ser
	return &v
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
