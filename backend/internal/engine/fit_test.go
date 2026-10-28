package engine

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// oldFit are the settings that reproduce the behaviour from before they were adjustable.
var oldFit = FitOptions{MinLaps: 8, SlowPercent: 8, SkipFCY: true, SkipWet: true}

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
	fit := fitModel(laps, bounds, nil, oldFit)
	if fit.Reason != "" || fit.FuelEffect == nil || fit.DegPerLap == nil {
		t.Fatalf("%+v", fit)
	}
	if math.Abs(*fit.FuelEffect-0.03) > 0.002 || math.Abs(*fit.DegPerLap-0.05) > 0.01 || fit.R2 < 0.99 {
		t.Fatalf("fuel %v deg %v r2 %v", *fit.FuelEffect, *fit.DegPerLap, fit.R2)
	}
}

func TestFitNeedsEnoughCleanLaps(t *testing.T) {
	laps, bounds := stints(1, 7, 60, 2)
	if fit := fitModel(laps, bounds, nil, oldFit); fit.Reason != "few_laps" || fit.FuelEffect != nil {
		t.Fatalf("%+v", fit)
	}
}

func TestFitSaysWhenFuelAndTyreAgeCannotBeTold(t *testing.T) {
	laps, bounds := stints(1, 20, 60, 2) // one stint: fuel falls exactly as the tyres age
	if fit := fitModel(laps, bounds, nil, oldFit); fit.Reason != "collinear" {
		t.Fatalf("%+v", fit)
	}
}

func TestFitIgnoresPitLapsAndSlowLaps(t *testing.T) {
	laps, bounds := stints(3, 8, 60, 2)
	laps[3].Lap.InLap = true
	laps[4].Lap.LapTime = f(200)
	laps[5].Lap.FCY = true
	fit := fitModel(laps, bounds, nil, oldFit)
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
	if fit := fitModel(laps, bounds, skip, oldFit); fit.Laps != 19 {
		t.Fatalf("laps used: %d", fit.Laps)
	}
}

const (
	legacyMinLaps    = 8
	legacySlowFactor = 1.08
)

// legacyFitModel is the fit as it was before the lap time model settings became adjustable, kept to show that the
// old values give the same result.
func legacyFitModel(laps []store.LapRecord, bounds []int, skip func(lap int) bool) *Fit {
	type row struct{ fuel, age, t float64 }
	var rows []row
	for _, r := range laps {
		l := r.Lap
		if l.LapTime == nil || l.FuelStart == nil || l.FuelEnd == nil || l.InLap || l.OutLap || l.FCY || (skip != nil && skip(l.Lap)) {
			continue
		}
		start := 0
		for _, b := range bounds {
			if b < l.Lap && b > start {
				start = b
			}
		}
		rows = append(rows, row{(*l.FuelStart + *l.FuelEnd) / 2, float64(l.Lap - start), *l.LapTime})
	}
	if len(rows) >= 3 {
		ts := make([]float64, len(rows))
		for i, r := range rows {
			ts[i] = r.t
		}
		med := *median(ts)
		kept := rows[:0]
		for _, r := range rows {
			if r.t <= med*legacySlowFactor {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	out := &Fit{Laps: len(rows)}
	if len(rows) < legacyMinLaps {
		out.Reason = "few_laps"
		return out
	}
	n := float64(len(rows))
	var mf, ma, mt float64
	for _, r := range rows {
		mf, ma, mt = mf+r.fuel, ma+r.age, mt+r.t
	}
	mf, ma, mt = mf/n, ma/n, mt/n
	var sff, saa, sfa, sft, sat, stt float64
	for _, r := range rows {
		df, da, dt := r.fuel-mf, r.age-ma, r.t-mt
		sff, saa, sfa = sff+df*df, saa+da*da, sfa+df*da
		sft, sat, stt = sft+df*dt, sat+da*dt, stt+dt*dt
	}
	var b, c float64
	haveF, haveA := sff > 1e-6, saa > 1e-6
	switch {
	case haveF && haveA:
		if math.Abs(sfa)/math.Sqrt(sff*saa) > fitCollinear {
			out.Reason = "collinear"
			return out
		}
		det := sff*saa - sfa*sfa
		b = (sft*saa - sat*sfa) / det
		c = (sat*sff - sft*sfa) / det
	case haveF:
		b = sft / sff
	case haveA:
		c = sat / saa
	default:
		out.Reason = "few_laps"
		return out
	}
	out.Base = mt - b*mf - c*ma
	// Stay at zero for what the data could not tell, so a missing effect is not reported as "no effect".
	if haveF {
		out.FuelEffect = &b
	}
	if haveA {
		out.DegPerLap = &c
	}
	if stt > 0 {
		out.R2 = math.Min(math.Max((b*sft+c*sat)/stt, 0), 1)
	}
	return out
}
