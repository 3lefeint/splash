package engine

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func f(v float64) *float64 { return &v }

// field: places 1..5, we are third. Gaps behind next: P2 4 s behind P1, we 6 s behind P2, P4 5 s behind us, P5 7 s behind P4.
func field5() []wire.ScoringVehicle {
	mk := func(id, place int, tb float64) wire.ScoringVehicle {
		return wire.ScoringVehicle{CarID: id, VehicleName: "car" + string(rune('0'+id)), Driver: "d" + string(rune('0'+id)),
			Place: place, TimeBehindNext: f(tb), Fuel: f(50), FuelCapacity: f(100)}
	}
	return []wire.ScoringVehicle{mk(1, 1, 0), mk(2, 2, 4), mk(3, 3, 6), mk(4, 4, 5), mk(5, 5, 7)}
}

func input(vs []wire.ScoringVehicle, p store.Params) strategyInput {
	sc := &wire.Scoring{Vehicles: vs}
	car := Car{CarID: 3, VehicleName: "car3", Place: 3, Fuel: 20, FuelCapacity: 100, FuelPerLap: f(2)}
	return strategyInput{sc: sc, car: car, params: p, lapTime: 100}
}

func TestGapsAreSummedAlongThePlaces(t *testing.T) {
	vs := field5()
	if g := gapTo(vs, 2, 0); g == nil || *g != 10 { // P1 is 4+6 s ahead
		t.Fatalf("%v", g)
	}
	if g := gapTo(vs, 2, 4); g == nil || *g != -12 { // P5 is 5+7 s behind
		t.Fatalf("%v", g)
	}
	vs[3].TimeBehindNext = nil
	if g := gapTo(vs, 2, 4); g != nil {
		t.Fatalf("an unknown gap in between gives no gap: %v", *g)
	}
}

func TestRejoinCountsTheCarsThatWouldPassUs(t *testing.T) {
	var r rivalLog
	r.reset("s")
	p := store.Params{PitThroughGreen: 20, RefuelRate: 4, Reserve: 3} // stop: 20 s + 80 l / 4 = 40 s
	s := r.analyse(input(field5(), p))
	if s == nil || s.Rejoin == nil {
		t.Fatalf("%+v", s)
	}
	rj := s.Rejoin
	if rj.Loss != 40 || rj.Lost != 2 || rj.Position != 5 || len(rj.PassedBy) != 2 || rj.Incomplete {
		t.Fatalf("%+v", rj) // P4 at 5 s and P5 at 12 s are both inside 40 s
	}
	rj = r.analyse(input(field5(), store.Params{PitThroughGreen: 8, RefuelRate: 100})).Rejoin
	if rj.Loss != 8.8 || rj.Lost != 1 || rj.PassedBy[0] != "d4" { // only P4 (5 s) is inside 8.8 s
		t.Fatalf("%+v", rj)
	}
}

func TestRivalsThatStopSoonAreNotCountedAsPassing(t *testing.T) {
	var r rivalLog
	r.reset("s")
	vs := field5()
	vs[3].Fuel = f(6) // P4 has 3 l above the reserve: with our 2 l per lap it stops in 1.5 laps
	s := r.analyse(input(vs, store.Params{PitThroughGreen: 20, RefuelRate: 4, Reserve: 3}))
	rj := s.Rejoin
	if rj.Lost != 1 || len(rj.AlsoStop) != 1 || rj.AlsoStop[0] != "d4" {
		t.Fatalf("%+v", rj)
	}
	var p4 Rival
	for _, rv := range s.Rivals {
		if rv.CarID == 4 {
			p4 = rv
		}
	}
	if p4.StopIn == nil || math.Abs(*p4.StopIn-1.5) > 1e-9 || p4.StopBasis != "assumed" {
		t.Fatalf("%+v", p4)
	}
}

func TestBestWindowIsFoundWhenARivalStopsFirst(t *testing.T) {
	var r rivalLog
	r.reset("s")
	vs := field5()
	vs[3].Fuel = f(12) // P4: 9 l above the reserve = 4.5 laps
	vs[4].Fuel = f(60)
	s := r.analyse(input(vs, store.Params{PitThroughGreen: 20, RefuelRate: 4, Reserve: 3}))
	rj := s.Rejoin
	if rj.Window[0].Lost != 2 || rj.Window[2].Lost != 2 || rj.Window[3].Lost != 1 || rj.BestIn != 3 {
		t.Fatalf("%+v", rj.Window)
	}
}

func TestPaceRateIsPositiveForSlowerRivals(t *testing.T) {
	var r rivalLog
	r.reset("s")
	in := input(field5(), store.Params{PitThroughGreen: 10})
	var laps []store.LapRecord
	for l := 1; l <= 6; l++ {
		laps = append(laps, store.LapRecord{Lap: wire.Lap{CarID: 4, VehicleName: "car4", Lap: l, LapTime: f(101.5)}})
	}
	laps = append(laps, store.LapRecord{Lap: wire.Lap{CarID: 4, VehicleName: "car4", Lap: 7, LapTime: f(140), InLap: true}})
	in.stats = summarizeField(laps)
	s := r.analyse(in)
	for _, rv := range s.Rivals {
		if rv.CarID == 4 && (rv.Rate == nil || math.Abs(*rv.Rate-1.5) > 1e-9 || rv.StintLaps != 7) {
			t.Fatalf("%+v", rv)
		}
	}
}

func TestFuelUseOfARivalIsLearnedFromItsLapsAndIgnoresRefuelling(t *testing.T) {
	var r rivalLog
	r.reset("s")
	lap := func(n int, fuel float64) []wire.ScoringVehicle {
		return []wire.ScoringVehicle{{CarID: 9, VehicleName: "x", Laps: n, Fuel: f(fuel)}}
	}
	for n, fuel := range []float64{80, 77.5, 75, 72.4, 100} { // the last lap is a stop
		r.watchFuel(lap(n, fuel))
	}
	fpl, ok := r.perLap(carKey("x", 9))
	if !ok || math.Abs(fpl-2.5) > 0.1 {
		t.Fatalf("%v %v", fpl, ok)
	}
}

func TestStopForecastFallsBackToEarlierStints(t *testing.T) {
	var r rivalLog
	r.reset("s")
	vs := field5()
	for i := range vs {
		vs[i].Fuel = nil
	}
	in := input(vs, store.Params{PitThroughGreen: 10})
	var laps []store.LapRecord
	for l := 1; l <= 30; l++ {
		laps = append(laps, store.LapRecord{Lap: wire.Lap{CarID: 4, VehicleName: "car4", Lap: l, LapTime: f(100), OutLap: l == 11 || l == 21}})
	}
	in.stats = summarizeField(laps) // stints of 10 and 10 laps complete, 10 laps into the third
	for _, rv := range r.analyse(in).Rivals {
		if rv.CarID == 4 && (rv.StopIn == nil || *rv.StopIn != 0 || rv.StopBasis != "stint") {
			t.Fatalf("%+v", rv)
		}
	}
}

func TestPaceIgnoresSlowOutlierLaps(t *testing.T) {
	if p := paceOf([]float64{100, 100, 100, 100, 100, 190}); math.Abs(p-100) > 1e-9 {
		t.Fatalf("%v", p)
	}
	if paceOf(nil) != 0 {
		t.Fatal("no laps")
	}
}

func TestNoLossGivesNoRejoin(t *testing.T) {
	var r rivalLog
	r.reset("s")
	if s := r.analyse(input(field5(), store.Params{})); s == nil || s.Rejoin != nil {
		t.Fatalf("%+v", s)
	}
}
