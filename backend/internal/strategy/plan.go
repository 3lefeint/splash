package strategy

import "errors"

// ErrInvalidInput is returned for non-positive consumption, lap time or tank size.
var ErrInvalidInput = errors.New("strategy: input must be positive")

// LapsToStop is the number of full laps the remaining fuel allows while keeping the reserve.
func LapsToStop(fuel, reserve, fuelPerLap float64) (int, error) {
	if fuelPerLap <= 0 {
		return 0, ErrInvalidInput
	}
	n := floorInt((fuel - reserve) / fuelPerLap)
	if n < 0 {
		n = 0
	}
	return n, nil
}

// LapsLeft is the number of laps still to be driven when timeLeft seconds remain
// at the given lap time. The extra lap accounts for the finish: the last lap ends
// when the overall leader crosses the line after the time has run out.
func LapsLeft(timeLeft, lapTime float64) (int, error) {
	if lapTime <= 0 {
		return 0, ErrInvalidInput
	}
	if timeLeft < 0 {
		timeLeft = 0
	}
	return ceilInt(timeLeft/lapTime) + 1, nil
}

// StopsRemaining is the number of pit stops needed to reach the finish with the
// given fuel, consumption and tank size.
func StopsRemaining(lapsLeft int, fuelPerLap, fuel, tank float64) (int, error) {
	if fuelPerLap <= 0 || tank <= 0 {
		return 0, ErrInvalidInput
	}
	n := ceilInt((float64(lapsLeft)*fuelPerLap - fuel) / tank)
	if n < 0 {
		n = 0
	}
	return n, nil
}

// SaveTarget is the fuel per lap at which one stop less is needed. ok is false
// when no stop is left to save or no laps remain.
func SaveTarget(fuel, tank float64, stops, lapsLeft int) (target float64, ok bool) {
	if stops < 1 || lapsLeft < 1 {
		return 0, false
	}
	return (fuel + float64(stops-1)*tank) / float64(lapsLeft), true
}
