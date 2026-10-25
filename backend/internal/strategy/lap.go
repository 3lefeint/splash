// Package strategy holds the pure strategy logic: fuel model, stops left and
// saving target. It has no I/O so it can be tested with synthetic lap data.
package strategy

// Lap is a completed lap of the driven car as far as the fuel model needs it.
type Lap struct {
	Number     int
	Driver     string
	Time       float64 // seconds
	FuelStart  float64 // litres at the start line
	FuelEnd    float64 // litres at the finish line
	FCY        bool    // full course yellow or local yellow during the lap
	InLap      bool
	OutLap     bool
	DriverSwap bool // driver change happened in this lap
}
