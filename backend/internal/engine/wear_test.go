package engine

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wear"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func lapRec(n int, w float64, flags ...string) store.LapRecord {
	l := wire.Lap{Lap: n, Wear: []float64{w, w, w, w}}
	for _, f := range flags {
		switch f {
		case "in":
			l.InLap = true
		case "out":
			l.OutLap = true
		case "fcy":
			l.FCY = true
		}
	}
	return store.LapRecord{Lap: l}
}

// lost returns a lap that lost the given wear fraction in the first section on every wheel.
func lost(n int, v float64) wear.LapWear {
	w := wear.LapWear{Lap: n, Coverage: 1}
	for i := range w.Loss {
		w.Loss[i][0] = v
	}
	return w
}

func TestWearStatsTrendAndLocation(t *testing.T) {
	var laps []store.LapRecord
	var wears []wear.LapWear
	// six clean laps; the loss per lap grows from 0.2 % to 0.3 %
	for i, v := range []float64{0.002, 0.002, 0.002, 0.003, 0.003, 0.003} {
		laps = append(laps, lapRec(i+1, 1-0.003*float64(i+1)))
		wears = append(wears, lost(i+1, v))
	}
	st := wearStats(laps, wears)
	if st == nil || st.Laps != 6 || len(st.PerLap) != 6 {
		t.Fatalf("%+v", st)
	}
	if math.Abs(st.Recent[0][0]-0.3) > 1e-9 || st.Recent[0][1] != 0 || math.Abs(st.Base[0][0]-0.2) > 1e-9 {
		t.Fatalf("recent=%v base=%v", st.Recent[0][:2], st.Base[0][:2])
	}
	if st.TrendPct == nil || math.Abs(*st.TrendPct-50) > 1e-6 {
		t.Fatalf("trend=%v", st.TrendPct)
	}
	if math.Abs(st.PerWheel[2]-0.3) > 1e-9 {
		t.Fatalf("per wheel %v", st.PerWheel)
	}
}

func TestWearStatsTrendFromFourLaps(t *testing.T) {
	var laps []store.LapRecord
	var wears []wear.LapWear
	for i, v := range []float64{0.002, 0.002, 0.003, 0.003} {
		laps = append(laps, lapRec(i+1, 1-0.003*float64(i+1)))
		wears = append(wears, lost(i+1, v))
	}
	st := wearStats(laps, wears)
	if st == nil || st.Base == nil || st.TrendPct == nil || math.Abs(*st.TrendPct-50) > 1e-6 {
		t.Fatalf("%+v", st)
	}
	if st3 := wearStats(laps[:3], wears[:3]); st3.Base != nil || st3.TrendPct != nil {
		t.Fatal("three laps are too few for a trend")
	}
}

func TestWearStatsSkipsPitLapsYellowAndGappyLaps(t *testing.T) {
	laps := []store.LapRecord{lapRec(1, 0.99, "out"), lapRec(2, 0.98), lapRec(3, 0.97, "fcy"), lapRec(4, 0.96), lapRec(5, 0.95, "in")}
	wears := []wear.LapWear{lost(1, 0.01), lost(2, 0.01), lost(3, 0.01), lost(4, 0.01), lost(5, 0.01)}
	wears[3].Coverage = 0.5
	st := wearStats(laps, wears)
	if st == nil || st.Laps != 1 || st.PerLap[0].Lap != 2 {
		t.Fatalf("%+v", st)
	}
	if st.TrendPct != nil || st.Base != nil {
		t.Fatal("a trend needs more laps")
	}
}

func TestWearStatsRestartsAfterATyreChange(t *testing.T) {
	var laps []store.LapRecord
	var wears []wear.LapWear
	for i := 1; i <= 4; i++ {
		laps = append(laps, lapRec(i, 1-0.1*float64(i)))
		wears = append(wears, lost(i, 0.01))
	}
	laps = append(laps, lapRec(5, 0.6, "in"), lapRec(6, 1.0, "out"), lapRec(7, 0.99), lapRec(8, 0.98))
	wears = append(wears, lost(5, 0.01), lost(6, 0.01), lost(7, 0.004), lost(8, 0.004))
	st := wearStats(laps, wears)
	if st == nil || st.Laps != 2 || st.PerLap[0].Lap != 7 {
		t.Fatalf("%+v", st)
	}
}

func TestWearStatsShowTheSetBeforeUntilTheNewOneHasACleanLap(t *testing.T) {
	var laps []store.LapRecord
	var wears []wear.LapWear
	for i := 1; i <= 4; i++ {
		laps = append(laps, lapRec(i, 1-0.1*float64(i)))
		wears = append(wears, lost(i, 0.01))
	}
	laps = append(laps, lapRec(5, 1.0, "in"), lapRec(6, 1.0, "out"))
	wears = append(wears, lost(5, 0.01), lost(6, 0.01))
	st := wearStats(laps, wears)
	if st == nil || !st.PreviousSet || st.Laps != 4 {
		t.Fatalf("%+v", st)
	}
	laps = append(laps, lapRec(7, 0.99))
	wears = append(wears, lost(7, 0.004))
	if st = wearStats(laps, wears); st == nil || st.PreviousSet || st.Laps != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestNoWearStatsWithoutCleanLaps(t *testing.T) {
	if st := wearStats([]store.LapRecord{lapRec(1, 1, "out")}, []wear.LapWear{lost(1, 0.01)}); st != nil {
		t.Fatal("expected nil")
	}
}
