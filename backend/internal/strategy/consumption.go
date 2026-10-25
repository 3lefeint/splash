package strategy

import (
	"math"
	"sort"
)

const (
	// DefaultAlpha is the EWMA smoothing factor for fuel per lap.
	DefaultAlpha = 0.3
	// paceWindow is the number of recent lap times used for the pace check.
	paceWindow = 10
	// paceLimit is the factor over the median lap time above which a lap is invalid.
	paceLimit = 1.07
)

// Consumption keeps an EWMA of fuel per lap for each driver, fed only with valid laps.
type Consumption struct {
	alpha   float64
	drivers map[string]*driverState
}

type driverState struct {
	fuelPerLap float64
	laps       int
	times      []float64 // last lap times that passed the basic checks, oldest first
}

// NewConsumption returns a model with the given EWMA alpha (0 < alpha <= 1).
// Any other value falls back to DefaultAlpha.
func NewConsumption(alpha float64) *Consumption {
	if alpha <= 0 || alpha > 1 {
		alpha = DefaultAlpha
	}
	return &Consumption{alpha: alpha, drivers: map[string]*driverState{}}
}

// Add feeds a completed lap into the model and reports whether it was used.
// Invalid are in/out laps, FCY/yellow laps, laps with a driver change, laps
// without fuel burn (refuelled) and laps slower than 107 % of the median of the
// driver's last 10 laps that passed the other checks.
func (c *Consumption) Add(l Lap) bool {
	if l.InLap || l.OutLap || l.FCY || l.DriverSwap {
		return false
	}
	used := l.FuelStart - l.FuelEnd
	if used <= 0 || l.Time <= 0 {
		return false
	}
	s := c.drivers[l.Driver]
	if s == nil {
		s = &driverState{}
		c.drivers[l.Driver] = s
	}
	slow := len(s.times) > 0 && l.Time > paceLimit*median(s.times)
	// Slow laps enter the window too, so the median follows a lasting pace
	// change (rain, new tyres) while a single outlier barely moves it.
	s.times = append(s.times, l.Time)
	if len(s.times) > paceWindow {
		s.times = s.times[1:]
	}
	if slow {
		return false
	}
	if s.laps == 0 {
		s.fuelPerLap = used
	} else {
		s.fuelPerLap = c.alpha*used + (1-c.alpha)*s.fuelPerLap
	}
	s.laps++
	return true
}

// FuelPerLap returns the smoothed consumption of a driver. ok is false until
// the driver has at least one valid lap.
func (c *Consumption) FuelPerLap(driver string) (fpl float64, ok bool) {
	s := c.drivers[driver]
	if s == nil || s.laps == 0 {
		return 0, false
	}
	return s.fuelPerLap, true
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// epsilon guards ceil/floor against float noise (e.g. 2.0000000001 -> 3).
const epsilon = 1e-9

func ceilInt(x float64) int  { return int(math.Ceil(x - epsilon)) }
func floorInt(x float64) int { return int(math.Floor(x + epsilon)) }
