package store

import (
	"fmt"
	"math"
	"time"
)

// Params are the measured or manually entered training parameters for one track and car.
// Times are seconds, fuel is litres.
type Params struct {
	Track string `json:"track"`
	Car   string `json:"car"`
	// PitThroughGreen and PitThroughFCY are the time lost by driving through the
	// pit lane without standing still.
	PitThroughGreen float64 `json:"pit_through_green"`
	PitThroughFCY   float64 `json:"pit_through_fcy"`
	RefuelRate      float64 `json:"refuel_rate"` // litres per second
	TyreChange      float64 `json:"tyre_change"` // 0 means unset: DefaultTyreChange applies
	DriverSwap      float64 `json:"driver_swap"`
	// ParallelService is not used any more: the pit service always runs in sequence. It is only still accepted when
	// an older client sends it, and the database column stays in place unused.
	ParallelService bool    `json:"parallel_service,omitempty"`
	FuelEffect      float64 `json:"fuel_effect"` // seconds per litre
	DegPerLap       float64 `json:"deg_per_lap"` // seconds per lap of tyre age
	SaveCost        float64 `json:"save_cost"`   // seconds lost per litre saved per lap
	Reserve         float64 `json:"reserve"`     // litres kept in the tank at the stop
	// The lap time model settings: which laps count for the fit. A zero or absent value means the default, so
	// parameter sets stored before these existed keep working. See WithDefaults.
	FitMinLaps     int     `json:"fit_min_laps"`     // clean laps needed before the model is fitted
	FitSlowPercent float64 `json:"fit_slow_percent"` // laps this many percent slower than the median are left out
	FitSkipFCY     *bool   `json:"fit_skip_fcy"`     // leave out laps under full course yellow
	FitSkipWet     *bool   `json:"fit_skip_wet"`     // leave out laps on a wet track
}

// DefaultReserve is the fuel reserve used when no parameter set exists.
const DefaultReserve = 3.0

// DefaultTyreChange is the time in seconds a tyre change takes (a known fixed value), used where no parameter set
// exists or tyre_change was never set. The storage cannot tell an unset value from a 0 entered on purpose, so 0
// counts as unset.
const DefaultTyreChange = 17.0

// Defaults and allowed ranges of the lap time model settings.
const (
	DefaultFitMinLaps     = 5
	MinFitMinLaps         = 3
	MaxFitMinLaps         = 20
	DefaultFitSlowPercent = 8.0
	MinFitSlowPercent     = 2.0
	MaxFitSlowPercent     = 30.0
)

// WithDefaults returns the parameters with the unset values (the tyre change time and the lap time model settings)
// replaced by their defaults.
func (p Params) WithDefaults() Params {
	p.TyreChange = p.TyreChangeTime()
	if p.FitMinLaps == 0 {
		p.FitMinLaps = DefaultFitMinLaps
	}
	if p.FitSlowPercent == 0 {
		p.FitSlowPercent = DefaultFitSlowPercent
	}
	on := func(b *bool) *bool {
		v := b == nil || *b
		return &v
	}
	p.FitSkipFCY, p.FitSkipWet = on(p.FitSkipFCY), on(p.FitSkipWet)
	return p
}

// TyreChangeTime is the tyre change time in seconds: the stored value, the default while it is unset (0).
func (p Params) TyreChangeTime() float64 {
	if p.TyreChange > 0 {
		return p.TyreChange
	}
	return DefaultTyreChange
}

// ValidateFit rejects lap time model settings outside their range; unset (zero) values are fine.
func (p Params) ValidateFit() error {
	if p.FitMinLaps != 0 && (p.FitMinLaps < MinFitMinLaps || p.FitMinLaps > MaxFitMinLaps) {
		return fmt.Errorf("fit_min_laps must be %d to %d", MinFitMinLaps, MaxFitMinLaps)
	}
	if p.FitSlowPercent != 0 && (math.IsNaN(p.FitSlowPercent) || p.FitSlowPercent < MinFitSlowPercent || p.FitSlowPercent > MaxFitSlowPercent) {
		return fmt.Errorf("fit_slow_percent must be %g to %g", MinFitSlowPercent, MaxFitSlowPercent)
	}
	return nil
}

// PlanRow is one stint of the global stint plan.
type PlanRow struct {
	Driver   string  `json:"driver"`
	Duration float64 `json:"duration"` // seconds
}

// Override replaces a live value until it is cleared.
type Override struct {
	Key   string    `json:"key"`
	Value float64   `json:"value"`
	SetAt time.Time `json:"set_at"`
}

// Override keys.
const (
	OverrideFuel       = "fuel"
	OverrideFuelPerLap = "fuel_per_lap"
)

// ValidOverrideKey reports whether key may be overridden.
func ValidOverrideKey(key string) bool {
	return key == OverrideFuel || key == OverrideFuelPerLap
}
