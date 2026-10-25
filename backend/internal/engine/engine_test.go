package engine

import (
	"context"
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const sess = "s1"

func fp(v float64) *float64  { return &v }
func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

type rig struct {
	st  *store.Memory
	lv  *live.State
	eng *Engine
}

func newRig() *rig {
	st := store.NewMemory()
	lv := &live.State{}
	return &rig{st, lv, New(st, lv)}
}

func vehicle(id int, name, class string, place int, behindNext float64) wire.ScoringVehicle {
	return wire.ScoringVehicle{CarID: id, VehicleName: name, Driver: "d" + name, VehicleClass: class,
		Place: place, TimeBehindNext: fp(behindNext)}
}

func (r *rig) setLive(et, fuel float64, lap int, vehicles ...wire.ScoringVehicle) {
	r.lv.UpdateScoring(sess, et, wire.Scoring{Track: "monza", SessionType: "race", EndET: 10000, Vehicles: vehicles})
	r.lv.UpdateTelemetry(sess, et, wire.Telemetry{CarID: 0, VehicleName: "me", Lap: lap, Fuel: fuel,
		FuelCapacity: 90, Wear: []float64{1, 1, 1, 1}})
}

// laps stores n valid laps of 100 s burning 3 l each for the driver "dme".
func (r *rig) laps(n int) {
	fuel := 90.0
	for i := 1; i <= n; i++ {
		r.st.SaveLap(context.Background(), sess, wire.Lap{CarID: 0, VehicleName: "me", Lap: i, Driver: "dme",
			LapTime: fp(100), FuelStart: fp(fuel), FuelEnd: fp(fuel - 3)})
		fuel -= 3
	}
}

func TestNotReadyWithoutLiveData(t *testing.T) {
	r := newRig()
	st, err := r.eng.Compute(context.Background())
	if err != nil || st.Ready || st.Car != nil {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}

func TestRaceNumbers(t *testing.T) {
	r := newRig()
	r.laps(10)
	r.setLive(5000, 60, 11, vehicle(0, "me", "GT3", 1, 0))
	st, err := r.eng.Compute(context.Background())
	if err != nil || !st.Ready {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if st.Car.Driver != "dme" || !near(*st.Car.FuelPerLap, 3) || !near(*st.Car.LapTime, 100) {
		t.Fatalf("car=%+v", st.Car)
	}
	rc := st.Race
	if rc == nil || rc.LapsToStop != 19 || rc.LapsLeft != 51 || rc.StopsRemaining != 2 ||
		rc.SaveTarget == nil || !near(*rc.SaveTarget, 150.0/51) || !near(rc.StintEnd, 5000+19*100) {
		t.Fatalf("race=%+v", rc)
	}
	if !near(st.TimeLeft, 5000) || st.Params.Reserve != store.DefaultReserve {
		t.Fatalf("timeLeft=%v params=%+v", st.TimeLeft, st.Params)
	}
}

func TestNoRaceNumbersWithoutLaps(t *testing.T) {
	r := newRig()
	r.setLive(5000, 60, 1, vehicle(0, "me", "GT3", 1, 0))
	st, _ := r.eng.Compute(context.Background())
	if !st.Ready || st.Race != nil || st.Car.FuelPerLap != nil {
		t.Fatalf("st=%+v", st)
	}
}

func TestFuelOverrideAppliesAndClears(t *testing.T) {
	ctx := context.Background()
	r := newRig()
	r.laps(10)
	r.setLive(5000, 0, 11, vehicle(0, "me", "GT3", 1, 0)) // broken sensor reads 0
	r.st.SetOverride(ctx, store.OverrideFuel, 60)
	st, _ := r.eng.Compute(ctx)
	if !st.Car.FuelOverridden || st.Car.Fuel != 60 || st.Race.LapsToStop != 19 {
		t.Fatalf("car=%+v race=%+v", st.Car, st.Race)
	}
	r.setLive(5100, 59.5, 11, vehicle(0, "me", "GT3", 1, 0)) // sensor is back
	st, _ = r.eng.Compute(ctx)
	if st.Car.FuelOverridden || st.Car.Fuel != 59.5 {
		t.Fatalf("override must be cleared: %+v", st.Car)
	}
	if ov, _ := r.st.GetOverrides(ctx); len(ov) != 0 {
		t.Fatalf("override still stored: %v", ov)
	}
}

func TestFuelPerLapOverride(t *testing.T) {
	ctx := context.Background()
	r := newRig()
	r.laps(10)
	r.setLive(5000, 60, 11, vehicle(0, "me", "GT3", 1, 0))
	r.st.SetOverride(ctx, store.OverrideFuelPerLap, 2.5) // saving target in use
	st, _ := r.eng.Compute(ctx)
	if !st.Car.FuelPerLapOverridden || !near(*st.Car.FuelPerLap, 2.5) || st.Race.LapsToStop != 22 {
		t.Fatalf("car=%+v race=%+v", st.Car, st.Race)
	}
}

func TestFuelPerLapFallsBackToOtherDrivers(t *testing.T) {
	r := newRig()
	r.laps(10)
	v := vehicle(0, "me", "GT3", 1, 0)
	v.Driver = "new driver" // no valid lap yet
	r.setLive(5000, 60, 11, v)
	st, _ := r.eng.Compute(context.Background())
	if st.Car.FuelPerLap == nil || !near(*st.Car.FuelPerLap, 3) {
		t.Fatalf("car=%+v", st.Car)
	}
}

func TestStintPlanFromStops(t *testing.T) {
	ctx := context.Background()
	r := newRig()
	r.laps(10)
	r.st.SetParams(ctx, store.Params{Track: "monza", Car: "me", PitThroughGreen: 20, RefuelRate: 3, TyreChange: 10,
		DriverSwap: 5, ParallelService: true, Reserve: 3})
	r.st.SetStintPlan(ctx, []store.PlanRow{
		{Driver: "a", Duration: 3000}, {Driver: "b", Duration: 3000}, {Driver: "b", Duration: 3000},
	})
	r.setLive(100, 60, 1, vehicle(0, "me", "GT3", 1, 0)) // first sample: start of stint 1
	// One stop done: stint 1 is complete, stint 2 runs.
	r.st.SaveEvent(ctx, sess, 3000, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "me"})
	r.st.SaveEvent(ctx, sess, 3040, wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "me"})
	r.setLive(3500, 60, 11, vehicle(0, "me", "GT3", 1, 0))
	st, err := r.eng.Compute(ctx)
	if err != nil || st.StintPlan == nil {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	p := st.StintPlan
	// Refuelling (90-3)/3 = 29 s runs parallel to tyres + swap = 15 s: 20 + 29.
	if !near(p.PitLoss, 49) || len(p.Entries) != 3 {
		t.Fatalf("plan=%+v", p)
	}
	e := p.Entries
	if !e[0].Done || e[0].Start != 100 || e[0].End != 3000 || e[0].Driver != "a" {
		t.Fatalf("stint 1: %+v", e[0])
	}
	if !e[1].Current || e[1].Start != 3040 || !near(e[1].End, 3500+19*100) || !e[1].DriverSwap {
		t.Fatalf("stint 2: %+v", e[1])
	}
	// Same driver b: no swap. The time left (4551 s) exceeds one tank (87 l / 3 l * 100 s = 2900 s),
	// so the stint is capped and the rest shows up as shortfall.
	if e[2].DriverSwap || !near(e[2].Start, e[1].End+49) || !near(e[2].End-e[2].Start, 2900) || !near(p.Shortfall, 10000-e[2].Start-2900) {
		t.Fatalf("stint 3: %+v shortfall=%v", e[2], p.Shortfall)
	}
	if !near(p.CurrentDeviation, e[1].End-(3040+3000)) {
		t.Fatalf("deviation=%v", p.CurrentDeviation)
	}
}

func TestOpponentGapsAndTrend(t *testing.T) {
	ctx := context.Background()
	r := newRig()
	r.laps(10)
	// Overall order: 1 other class, 2 class rival A, 3 other class, 4 us, 5 other class, 6 class rival B.
	field := func(a, b, c, d float64) []wire.ScoringVehicle {
		return []wire.ScoringVehicle{
			vehicle(10, "p1", "HYPER", 1, 0),
			vehicle(11, "rivalA", "GT3", 2, a),
			vehicle(12, "p3", "HYPER", 3, b),
			vehicle(0, "me", "GT3", 4, c),
			vehicle(13, "p5", "HYPER", 5, d),
			vehicle(14, "rivalB", "GT3", 6, 7),
		}
	}
	// Rival A is 3+4 s ahead (cars at places 3 and 4 sum up), rival B is 2+7 s behind.
	for i, shrink := range []float64{0, 1, 2} {
		r.setLive(5000+float64(i)*100, 60, 11+i, field(5, 3, 4-shrink, 2)...)
		st, err := r.eng.Compute(ctx)
		if err != nil {
			t.Fatal(err)
		}
		a, b := st.Opponents.Ahead, st.Opponents.Behind
		if a == nil || a.VehicleName != "rivalA" || b == nil || b.VehicleName != "rivalB" {
			t.Fatalf("opponents=%+v", st.Opponents)
		}
		if *a.Gap != 3+4-shrink || *b.Gap != 2+7 {
			t.Fatalf("lap %d: ahead %v behind %v", i, *a.Gap, *b.Gap)
		}
		if i == 0 && a.Trend != nil {
			t.Fatal("no trend from a single lap")
		}
		if i == 2 && (a.Trend == nil || !near(*a.Trend, -1) || !near(*b.Trend, 0)) {
			t.Fatalf("trend ahead=%v behind=%v", a.Trend, b.Trend)
		}
	}
	// Repeated computes within the same lap must not add samples.
	r.eng.Compute(ctx)
	r.eng.Compute(ctx)
	st, _ := r.eng.Compute(ctx)
	if !near(*st.Opponents.Ahead.Trend, -1) {
		t.Fatalf("trend changed without a new lap: %v", *st.Opponents.Ahead.Trend)
	}
}

func TestFieldPlacesCarsOnTheTrack(t *testing.T) {
	r := newRig()
	d1, d2 := 120.5, 900.0
	me, other := vehicle(0, "me", "GT3", 1, 0), vehicle(1, "rival", "GT3", 2, 3)
	me.LapDist, other.LapDist = &d1, &d2
	r.setLive(100, 50, 3, me, other, vehicle(2, "nopos", "GT3", 3, 5))
	snap := r.eng.Live.Snapshot()
	snap.Scoring.TrackLength = 2000
	r.eng.Live.UpdateScoring("s1", 101, *snap.Scoring)
	st, err := r.eng.Compute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.TrackLength != 2000 || len(st.Field) != 2 {
		t.Fatalf("length=%v field=%+v", st.TrackLength, st.Field)
	}
	if !st.Field[0].Ours || st.Field[1].Ours || st.Field[1].LapDist != 900 {
		t.Fatalf("%+v", st.Field)
	}
}

func TestWeatherHistoryAndTyres(t *testing.T) {
	r := newRig()
	w := &wire.Weather{AmbientTemp: fp(20), TrackTemp: fp(30), Rain: fp(0), Wetness: fp(0)}
	for i, et := range []float64{100, 130, 170, 230} { // 130 is too soon after 100
		w.TrackTemp = fp(30 + float64(i))
		r.lv.UpdateScoring(sess, et, wire.Scoring{Track: "monza", EndET: 10000, Weather: w,
			Vehicles: []wire.ScoringVehicle{vehicle(0, "me", "GT3", 1, 0)}})
		r.lv.UpdateTelemetry(sess, et, wire.Telemetry{CarID: 0, VehicleName: "me", Lap: 1, Fuel: 50, FuelCapacity: 90,
			Wear: []float64{1, 1, 1, 1}, TyrePressure: []float64{180, 181, 182, 183},
			TyreSurfaceTemp: [][]float64{{80, 85, 82}, {80, 85, 82}, {80, 85, 82}, {80, 85, 82}}})
		st, err := r.eng.Compute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if et == 230 {
			if st.Weather == nil || len(st.Weather.History) != 3 || *st.Weather.Track != 33 {
				t.Fatalf("%+v", st.Weather)
			}
			if st.Weather.History[1].ET != 170 || *st.Weather.History[2].Track != 33 {
				t.Fatalf("%+v", st.Weather.History)
			}
			if st.Tyres == nil || st.Tyres.Pressure[3] != 183 || st.Tyres.Surface[0][1] != 85 {
				t.Fatalf("%+v", st.Tyres)
			}
		}
	}
}

func TestNoTyreBlockWithoutTemperatures(t *testing.T) {
	r := newRig()
	r.setLive(100, 50, 3, vehicle(0, "me", "GT3", 1, 0))
	st, _ := r.eng.Compute(context.Background())
	if st.Tyres != nil {
		t.Fatal("unexpected tyre block")
	}
}

func TestStintPlanStartsAtZeroWhenTheBackendMissedTheFirstStint(t *testing.T) {
	ctx := context.Background()
	r := newRig()
	r.laps(10)
	r.st.SetStintPlan(ctx, []store.PlanRow{{Driver: "a", Duration: 3000}, {Driver: "b", Duration: 3000}})
	// The backend saw its first telemetry only at 5000, after the first stop at 3000.
	r.st.SaveEvent(ctx, sess, 3000, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "me"})
	r.st.SaveEvent(ctx, sess, 3040, wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "me"})
	r.setLive(5000, 60, 11, vehicle(0, "me", "GT3", 1, 0))
	st, err := r.eng.Compute(ctx)
	if err != nil || st.StintPlan == nil {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if e := st.StintPlan.Entries[0]; e.Start != 0 || e.End != 3000 {
		t.Fatalf("stint 1: %+v", e)
	}
}
