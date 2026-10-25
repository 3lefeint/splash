package engine

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// stints builds laps with t = 90 + 0.03*fuel + 0.05*age; every stint starts with a full tank and new tyres.
func stints(n, per int, tank, use float64) ([]store.LapRecord, []int) {
	var laps []store.LapRecord
	bounds := []int{0}
	lap := 0
	for s := 0; s < n; s++ {
		if s > 0 {
			bounds = append(bounds, lap)
		}
		fuel := tank
		for i := 1; i <= per; i++ {
			lap++
			start, end := fuel, fuel-use
			t := 90 + 0.03*(start+end)/2 + 0.05*float64(i)
			laps = append(laps, store.LapRecord{Lap: wire.Lap{Lap: lap, LapTime: f(t), FuelStart: f(start), FuelEnd: f(end)}})
			fuel = end
		}
	}
	return laps, bounds
}

func TestFitRecoversFuelEffectAndTyreDegradation(t *testing.T) {
	// different stint lengths decorrelate fuel and tyre age
	laps, bounds := stints(2, 12, 60, 2)
	more, _ := stints(1, 6, 30, 2)
	offset := len(laps)
	for _, l := range more { // a short stint on a smaller fuel load
		l.Lap.Lap += offset
		laps = append(laps, l)
	}
	bounds = append(bounds, 24)
	fit := fitModel(laps, bounds, nil)
	if fit.Reason != "" || fit.FuelEffect == nil || fit.DegPerLap == nil {
		t.Fatalf("%+v", fit)
	}
	if math.Abs(*fit.FuelEffect-0.03) > 0.002 || math.Abs(*fit.DegPerLap-0.05) > 0.01 || fit.R2 < 0.99 {
		t.Fatalf("fuel %v deg %v r2 %v", *fit.FuelEffect, *fit.DegPerLap, fit.R2)
	}
}

func TestFitNeedsEnoughCleanLaps(t *testing.T) {
	laps, bounds := stints(1, 5, 60, 2)
	if fit := fitModel(laps, bounds, nil); fit.Reason != "few_laps" || fit.FuelEffect != nil {
		t.Fatalf("%+v", fit)
	}
}

func TestFitSaysWhenFuelAndTyreAgeCannotBeTold(t *testing.T) {
	laps, bounds := stints(1, 20, 60, 2) // one stint: fuel falls exactly as the tyres age
	if fit := fitModel(laps, bounds, nil); fit.Reason != "collinear" {
		t.Fatalf("%+v", fit)
	}
}

func TestFitIgnoresPitLapsAndSlowLaps(t *testing.T) {
	laps, bounds := stints(3, 8, 60, 2)
	laps[3].Lap.InLap = true
	laps[4].Lap.LapTime = f(200)
	laps[5].Lap.FCY = true
	fit := fitModel(laps, bounds, nil)
	if fit.Laps != 21 {
		t.Fatalf("laps used: %d", fit.Laps)
	}
}

func TestFitLeavesOutTheLapsItIsToldToSkip(t *testing.T) {
	laps, bounds := stints(3, 8, 60, 2)
	for i := range laps { // the rain made every fifth lap much slower
		if i%5 == 0 {
			laps[i].Lap.LapTime = f(*laps[i].Lap.LapTime + 6)
		}
	}
	skip := func(lap int) bool { return (lap-1)%5 == 0 }
	if fit := fitModel(laps, bounds, skip); fit.Laps != 19 {
		t.Fatalf("laps used: %d", fit.Laps)
	}
}
