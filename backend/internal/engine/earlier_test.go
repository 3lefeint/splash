package engine

import (
	"context"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// history stores a finished session on monza with the car "me": n laps of the driver, each 100 s burning use litres.
func (r *rig) history(id, driver string, n int, use float64, hours int) {
	ctx := context.Background()
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC).Add(time.Duration(hours) * time.Hour)
	r.st.TouchSession(ctx, store.SessionInfo{SessionID: id, Track: "monza", Car: "me", StartedAt: at, LastSeen: at})
	fuel := 90.0
	for i := 1; i <= n; i++ {
		r.st.SaveLap(ctx, id, wire.Lap{CarID: 0, VehicleName: "me", Lap: i, Driver: driver,
			LapTime: fp(100 + 0.1*float64(i%3)), FuelStart: fp(fuel), FuelEnd: fp(fuel - use)}, 0)
		fuel -= use
		if fuel < use {
			fuel = 90
		}
	}
}

// fresh forgets the cached laps of earlier sessions, as the passing of time does.
func (r *rig) fresh() { r.eng.past = earlierCache{} }

func TestEarlierSessionsGiveTheModelsAStartBeforeTheFirstLap(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.setLive(1000, 80, 1, vehicle(0, "me", "GT3", 1, 0))
	st, _ := r.eng.Compute(ctx)
	if st.Car.FuelPerLap != nil || st.Fit.HistoryLaps != 0 || st.Fit.Scope != "" {
		t.Fatalf("without history: %+v %+v", st.Car, st.Fit)
	}

	r.history("old", "dme", 10, 3, 0)
	r.fresh()
	st, _ = r.eng.Compute(ctx)
	if st.Car.FuelPerLap == nil || !near(*st.Car.FuelPerLap, 3) || st.Car.LapTime == nil || *st.Car.LapTime < 99 || *st.Car.LapTime > 101 {
		t.Fatalf("fuel and pace known from the first lap: %+v", st.Car)
	}
	if st.Race == nil {
		t.Fatal("the race numbers need the fuel per lap and the lap time")
	}
	if st.Fit.Laps != 10 || st.Fit.HistoryLaps != 10 || st.Fit.Scope != "driver" {
		t.Fatalf("fit %+v", st.Fit)
	}

	// the running session counts on top, and its newest laps weigh most
	r.laps(4)
	r.fresh()
	st, _ = r.eng.Compute(ctx)
	if st.Fit.Laps != 14 || st.Fit.HistoryLaps != 10 {
		t.Fatalf("fit with the running session %+v", st.Fit)
	}
}

func TestLapsAndSessionsLeftOutDoNotFeedTheModels(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.setLive(1000, 80, 1, vehicle(0, "me", "GT3", 1, 0))
	r.history("old", "dme", 10, 3, 0)
	r.history("hot", "dme", 6, 9, 1) // a session with odd consumption
	r.fresh()
	st, _ := r.eng.Compute(ctx)
	if st.Fit.HistoryLaps != 16 {
		t.Fatalf("both sessions count at first: %+v", st.Fit)
	}
	r.st.SetSessionExcluded(ctx, "hot", true)
	r.fresh()
	st, _ = r.eng.Compute(ctx)
	if st.Fit.HistoryLaps != 10 || !near(*st.Car.FuelPerLap, 3) {
		t.Fatalf("after leaving the session out: %+v fuel %v", st.Fit, st.Car.FuelPerLap)
	}
	yes := true
	r.st.SetLapExcluded(ctx, "old", 0, "me", 1, &yes)
	r.fresh()
	if st, _ = r.eng.Compute(ctx); st.Fit.HistoryLaps != 9 {
		t.Fatalf("after leaving a lap out: %+v", st.Fit)
	}
}

func TestADriverWithTooFewLapsFallsBackToTheTeam(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.setLive(1000, 80, 1, vehicle(0, "me", "GT3", 1, 0)) // the driver is "dme"
	r.history("theirs", "dother", 10, 4, 0)
	r.history("mine", "dme", 2, 3, 1)
	r.fresh()
	st, _ := r.eng.Compute(ctx)
	if st.Fit.Scope != "team" || st.Fit.HistoryLaps != 12 {
		t.Fatalf("two own laps are too few: %+v", st.Fit)
	}
	// enough laps of their own: only those count
	r.history("mine too", "dme", 6, 3, 2)
	r.fresh()
	st, _ = r.eng.Compute(ctx)
	if st.Fit.Scope != "driver" || st.Fit.HistoryLaps != 8 {
		t.Fatalf("own laps are enough: %+v", st.Fit)
	}
}

func TestADriversOwnFuelCountsAfterThreeLaps(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.history("theirs", "dother", 10, 4, 0)
	r.setLive(1000, 80, 3, vehicle(0, "me", "GT3", 1, 0))
	r.laps(2) // two laps of "dme", 3 l each
	r.fresh()
	st, _ := r.eng.Compute(ctx)
	if !near(*st.Car.FuelPerLap, 3.5) { // the average of dme (3) and dother (4)
		t.Fatalf("with two laps the team's average counts: %v", *st.Car.FuelPerLap)
	}
	r.laps(3)
	r.fresh()
	st, _ = r.eng.Compute(ctx)
	if !near(*st.Car.FuelPerLap, 3) {
		t.Fatalf("with three laps the driver's own value counts: %v", *st.Car.FuelPerLap)
	}
}

func TestWetLapsOfEarlierSessionsAreLeftOutOfTheFit(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	r.setLive(1000, 80, 1, vehicle(0, "me", "GT3", 1, 0))
	r.history("old", "dme", 10, 3, 0)
	for lap := 1; lap <= 4; lap++ {
		r.st.SaveLapConditions(ctx, store.LapConditions{SessionID: "old", CarID: 0, VehicleName: "me", Lap: lap, Wetness: fp(0.8)})
	}
	r.fresh()
	if st, _ := r.eng.Compute(ctx); st.Fit.HistoryLaps != 6 {
		t.Fatalf("four wet laps are left out: %+v", st.Fit)
	}
}
