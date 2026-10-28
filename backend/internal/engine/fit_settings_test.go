package engine

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// lapAt is a clean lap of the car "me" with the given time and fuel at the start (2 l are burnt).
func lapAt(lap int, t, fuelStart float64) wire.Lap {
	return wire.Lap{CarID: 0, VehicleName: "me", Lap: lap, Driver: "dme", LapTime: f(t), FuelStart: f(fuelStart), FuelEnd: f(fuelStart - 2)}
}

func reasons(fit *Fit) map[int]string {
	out := map[int]string{}
	for _, e := range fit.Excluded {
		out[e.Lap] = e.Reason
	}
	return out
}

func TestOldSettingsGiveTheSameFitAsBefore(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 40; round++ {
		laps, bounds := stints(3, 8+rng.Intn(6), 60, 2)
		var wetLaps = map[int]bool{}
		for i := range laps {
			l := &laps[i].Lap
			l.LapTime = f(*l.LapTime + rng.NormFloat64()*0.3)
			switch rng.Intn(14) {
			case 0:
				l.InLap = true
			case 1:
				l.OutLap = true
			case 2:
				l.FCY = true
			case 3:
				wetLaps[l.Lap] = true
			case 4:
				l.LapTime = f(*l.LapTime * 1.12) // slower than 8 percent over the median
			case 5:
				l.LapTime = nil
			case 6:
				l.FuelEnd = nil
			}
		}
		wet := func(lap int) bool { return wetLaps[lap] }
		want := legacyFitModel(laps, bounds, wet)
		got := fitModel(laps, bounds, wet, oldFit)
		same := func(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
		if got.Laps != want.Laps || got.Reason != want.Reason || !same(got.FuelEffect, want.FuelEffect) || !same(got.DegPerLap, want.DegPerLap) ||
			got.Base != want.Base || got.R2 != want.R2 {
			t.Fatalf("round %d: got %+v want %+v", round, got, want)
		}
	}
}

func TestEveryExclusionReasonIsNamed(t *testing.T) {
	laps, bounds := stints(1, 10, 60, 2)
	laps[0].Lap.LapTime = nil
	laps[1].Lap.InLap = true
	laps[2].Lap.OutLap = true
	laps[3].Lap.FCY = true
	laps[4].Lap.LapTime = f(*laps[4].Lap.LapTime * 1.5) // far over the median
	wet := func(lap int) bool { return lap == 6 }
	fit := fitModel(laps, bounds, wet, oldFit) // five laps are left, the slow one is judged by their median
	want := map[int]string{1: "missing_data", 2: "in_lap", 3: "out_lap", 4: "fcy", 6: "wet", 5: "too_slow"}
	got := reasons(fit)
	if fit.Reason != "few_laps" || len(got) != len(want) {
		t.Fatalf("%+v", fit)
	}
	for lap, r := range want {
		if got[lap] != r {
			t.Errorf("lap %d: got %q want %q", lap, got[lap], r)
		}
	}
	for i := 1; i < len(fit.Excluded); i++ {
		if fit.Excluded[i-1].Lap > fit.Excluded[i].Lap {
			t.Fatalf("not in lap order: %v", fit.Excluded)
		}
	}
	if fit.Laps != 4 || fit.MinLaps != 8 {
		t.Fatalf("laps=%d min=%d", fit.Laps, fit.MinLaps)
	}
}

func TestTheSlowLimitIsAdjustable(t *testing.T) {
	laps, bounds := stints(3, 8, 60, 2)
	laps[5].Lap.LapTime = f(*laps[5].Lap.LapTime * 1.05) // 5 percent over
	loose := fitModel(laps, bounds, nil, FitOptions{MinLaps: 5, SlowPercent: 8, SkipFCY: true, SkipWet: true})
	tight := fitModel(laps, bounds, nil, FitOptions{MinLaps: 5, SlowPercent: 4, SkipFCY: true, SkipWet: true})
	if loose.Laps != 24 || tight.Laps != 23 {
		t.Fatalf("loose=%d tight=%d", loose.Laps, tight.Laps)
	}
	if got := reasons(tight); len(got) != 0 {
		t.Fatalf("a fitted model lists no exclusions: %v", got)
	}
	few := fitModel(laps[:8], bounds[:1], nil, FitOptions{MinLaps: 9, SlowPercent: 2, SkipFCY: true, SkipWet: true})
	if reasons(few)[6] != "too_slow" {
		t.Fatalf("%v", few.Excluded)
	}
}

func TestTheMinimumOfLapsIsAdjustable(t *testing.T) {
	// two long stints and a short one on less fuel tell fuel and tyre age apart: 30 clean laps
	laps, bounds := stints(2, 12, 60, 2)
	more, _ := stints(1, 6, 30, 2)
	for _, l := range more {
		l.Lap.Lap += 24
		laps = append(laps, l)
	}
	bounds = append(bounds, 24)
	for _, c := range []struct {
		min    int
		reason string
	}{{31, "few_laps"}, {30, ""}, {3, ""}} {
		fit := fitModel(laps, bounds, nil, FitOptions{MinLaps: c.min, SlowPercent: 8, SkipFCY: true, SkipWet: true})
		if fit.Reason != c.reason || fit.MinLaps != c.min {
			t.Errorf("min %d: %+v", c.min, fit)
		}
	}
}

func TestFCYAndWetLapsCountWhenTheExclusionIsOff(t *testing.T) {
	laps, bounds := stints(3, 8, 60, 2)
	laps[2].Lap.FCY = true
	wet := func(lap int) bool { return lap == 10 }
	on := fitModel(laps, bounds, wet, FitOptions{MinLaps: 5, SlowPercent: 8, SkipFCY: true, SkipWet: true})
	off := fitModel(laps, bounds, wet, FitOptions{MinLaps: 5, SlowPercent: 8, SkipFCY: false, SkipWet: false})
	if on.Laps != 22 || off.Laps != 24 {
		t.Fatalf("on=%d off=%d", on.Laps, off.Laps)
	}
	// the wet test is only consulted with the exclusion on: the engine does not even read the conditions otherwise
	skipFCYOnly := fitModel(laps, bounds, wet, FitOptions{MinLaps: 5, SlowPercent: 8, SkipFCY: true, SkipWet: false})
	if skipFCYOnly.Laps != 23 {
		t.Fatalf("fcy only: %d", skipFCYOnly.Laps)
	}
}

func TestTheListOfExcludedLapsIsNeverNull(t *testing.T) {
	laps, bounds := stints(3, 8, 60, 2)
	for _, fit := range []*Fit{
		fitModel(laps, bounds, nil, oldFit),         // fitted
		fitModel(nil, nil, nil, oldFit),             // no laps at all
		fitModel(laps[:3], bounds[:1], nil, oldFit), // few laps, nothing excluded
	} {
		b, err := json.Marshal(fit)
		if err != nil || !strings.Contains(string(b), `"excluded":[]`) {
			t.Fatalf("%s %v", b, err)
		}
	}
}

func TestFitOptionsComeFromTheParametersWithDefaults(t *testing.T) {
	if o := fitOptions(store.Params{}); o != (FitOptions{MinLaps: 5, SlowPercent: 8, SkipFCY: true, SkipWet: true}) {
		t.Fatalf("%+v", o)
	}
	off := false
	o := fitOptions(store.Params{FitMinLaps: 12, FitSlowPercent: 3, FitSkipFCY: &off})
	if o != (FitOptions{MinLaps: 12, SlowPercent: 3, SkipFCY: false, SkipWet: true}) {
		t.Fatalf("%+v", o)
	}
}

func TestTheEngineReadsTheSettingsAtEachCalculation(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.setLive(100, 50, 7, vehicle(0, "me", "GT3", 1, 0))
	// 7 laps with fuel loads and tyre ages that can be told apart
	for i := 1; i <= 7; i++ {
		start := 90 - float64(i)*2 - float64((i%3)*7)
		r.st.SaveLap(ctx, sess, lapAt(i, 90+0.03*start+0.05*float64(i), start))
	}
	st, err := r.eng.Compute(ctx)
	if err != nil || st.Fit == nil || st.Fit.Reason != "" || st.Fit.MinLaps != 5 {
		t.Fatalf("default of 5 laps: %+v err=%v", st.Fit, err)
	}
	r.st.SetParams(ctx, store.Params{Track: "monza", Car: "me", FitMinLaps: 9})
	st, _ = r.eng.Compute(ctx)
	if st.Fit.Reason != "few_laps" || st.Fit.MinLaps != 9 || st.Fit.Laps != 7 || st.Params.FitMinLaps != 9 {
		t.Fatalf("9 laps: %+v params=%+v", st.Fit, st.Params)
	}
	if math.IsNaN(st.Fit.R2) {
		t.Fatal("nan")
	}
}

func TestTheTyreChangeDefaultReachesPitLossAndServiceTime(t *testing.T) {
	none := store.Params{}
	if got := pitLoss(none, 100); got != store.DefaultTyreChange {
		t.Fatalf("pitLoss without parameters: %v", got)
	}
	if got := serviceTime(none, 0, false); got != store.DefaultTyreChange {
		t.Fatalf("serviceTime without parameters: %v", got)
	}
	// the default counts next to the driver swap
	p := store.Params{PitThroughGreen: 20, RefuelRate: 4, Reserve: 3, DriverSwap: 10}
	if got := pitLoss(p, 83); got != 20+20+17+10 {
		t.Fatalf("serial: %v", got)
	}
	// an old stored value that says parallel has no effect: tyres, then fuel
	p.ParallelService = true
	if got := pitLoss(p, 83); got != 20+20+17+10 {
		t.Fatalf("pitLoss with an old parallel value: %v", got)
	}
	if got := serviceTime(p, 80, true); got != 20+17+10 { // refuelling 20 s + tyres 17 s + swap 10 s
		t.Fatalf("serviceTime with swap: %v", got)
	}
	if got := serviceTime(p, 80, false); got != 20+17 {
		t.Fatalf("serviceTime without swap: %v", got)
	}
	// an explicit value overrides it
	p.TyreChange = 12
	if got := pitLoss(p, 83); got != 20+20+12+10 {
		t.Fatalf("explicit: %v", got)
	}
}

func TestTheEngineStateCarriesTheDefaultTyreChange(t *testing.T) {
	r := newRig()
	r.setLive(100, 50, 3, vehicle(0, "me", "GT3", 1, 0))
	st, err := r.eng.Compute(context.Background())
	if err != nil || st.Params.TyreChange != store.DefaultTyreChange {
		t.Fatalf("no parameter set: %+v err=%v", st.Params, err)
	}
	r.st.SetParams(context.Background(), store.Params{Track: "monza", Car: "me"}) // stored but never set
	if st, _ = r.eng.Compute(context.Background()); st.Params.TyreChange != store.DefaultTyreChange {
		t.Fatalf("never set: %+v", st.Params)
	}
	r.st.SetParams(context.Background(), store.Params{Track: "monza", Car: "me", TyreChange: 22})
	if st, _ = r.eng.Compute(context.Background()); st.Params.TyreChange != 22 {
		t.Fatalf("explicit: %+v", st.Params)
	}
}
