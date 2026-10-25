package store

import "time"

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
	TyreChange      float64 `json:"tyre_change"`
	DriverSwap      float64 `json:"driver_swap"`
	// ParallelService is true when refuelling runs in parallel to the tyre
	// change and the driver swap.
	ParallelService bool    `json:"parallel_service"`
	FuelEffect      float64 `json:"fuel_effect"` // seconds per litre
	DegPerLap       float64 `json:"deg_per_lap"` // seconds per lap of tyre age
	SaveCost        float64 `json:"save_cost"`   // seconds lost per litre saved per lap
	Reserve         float64 `json:"reserve"`     // litres kept in the tank at the stop
}

// DefaultReserve is the fuel reserve used when no parameter set exists.
const DefaultReserve = 3.0

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
