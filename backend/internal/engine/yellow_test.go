package engine

import (
	"context"
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

func ip(v int) *int { return &v }

// yvehicle is a car at a lap distance in a sector (0 = the agent did not say).
func yvehicle(id int, name string, place int, dist float64, sector int) wire.ScoringVehicle {
	v := vehicle(id, name, "GT3", place, 1)
	v.LapDist = fp(dist)
	if sector != 0 {
		v.Sector = ip(sector)
	}
	return v
}

// setYellow sends a scoring sample with sector flags and a telemetry sample of car 0 at game time et.
func (r *rig) setYellow(et float64, flags []int, vehicles ...wire.ScoringVehicle) {
	r.lv.UpdateScoring(sess, et, wire.Scoring{Track: "monza", SessionType: "race", EndET: 10000, TrackLength: 5000,
		SectorFlags: flags, Vehicles: vehicles})
	r.lv.UpdateTelemetry(sess, et, wire.Telemetry{CarID: 0, VehicleName: "me", Lap: 2, Fuel: 50, FuelCapacity: 90,
		Wear: []float64{1, 1, 1, 1}})
}

func (r *rig) compute(t *testing.T) State {
	t.Helper()
	st, err := r.eng.Compute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestNoYellowWhileAllSectorsAreClear(t *testing.T) {
	r := newRig()
	r.setYellow(100, []int{11, 11, 11}, yvehicle(0, "me", 1, 390, 1))
	st := r.compute(t)
	if len(st.YellowSectors) != 0 || st.YellowSectors == nil || st.Fcy {
		t.Fatalf("sectors=%v fcy=%v", st.YellowSectors, st.Fcy)
	}
}

func TestSectorFlagsBecomeYellowSectors(t *testing.T) {
	r := newRig()
	r.setYellow(100, []int{11, 1, 1}, yvehicle(0, "me", 1, 390, 1), yvehicle(1, "x", 2, 2500, 2))
	st := r.compute(t)
	if len(st.YellowSectors) != 2 || st.YellowSectors[0] != 2 || st.YellowSectors[1] != 3 || st.Fcy {
		t.Fatalf("%+v", st)
	}
}

func causes(st State) map[int]bool {
	out := map[int]bool{}
	for _, f := range st.Field {
		out[f.CarID] = f.Cause
	}
	return out
}

func TestOnlyACarStandingStillInTheFlaggedSectorIsACause(t *testing.T) {
	r := newRig()
	still := func(id int, name string, place int, dist float64, sector int, pits bool) wire.ScoringVehicle {
		v := yvehicle(id, name, place, dist, sector)
		v.InPits = pits
		return v
	}
	// two samples half a second apart: car 3 does not move, car 1 moves 20 m (40 m/s), car 2 stands in the pits, car 4
	// stands in a sector without a flag, car 5 is slower than the limit (1 m/s)
	sample := func(et, moving, slow float64) []wire.ScoringVehicle {
		return []wire.ScoringVehicle{still(0, "me", 1, 390, 1, false), still(1, "moving", 2, moving, 1, false),
			still(2, "pitcar", 3, 100, 1, true), still(3, "spun", 4, 300, 1, false), still(4, "elsewhere", 5, 2000, 2, false),
			still(5, "slow", 6, slow, 1, false)}
	}
	r.setYellow(100, []int{1, 11, 11}, sample(100, 500, 600)...)
	if c := causes(r.compute(t)); c[3] || c[5] { // the first sample: no speed yet, so no cause either
		t.Fatalf("causes without a speed: %v", c)
	}
	r.setYellow(100.5, []int{1, 11, 11}, sample(100.5, 520, 600.5)...)
	st := r.compute(t)
	c := causes(st)
	if !c[3] || !c[5] || c[0] || c[1] || c[2] || c[4] {
		t.Fatalf("causes: %v", c)
	}
	rivals := map[int]bool{}
	for _, rv := range st.Strategy.Rivals {
		rivals[rv.CarID] = rv.Cause
	}
	if !rivals[3] || !rivals[5] || rivals[1] || rivals[4] {
		t.Fatalf("rivals: %v", rivals)
	}
}

func TestOurOwnCarIsNeverACause(t *testing.T) {
	r := newRig()
	r.setYellow(100, []int{1, 11, 11}, yvehicle(0, "me", 1, 390, 1))
	r.compute(t)
	r.setYellow(100.5, []int{1, 11, 11}, yvehicle(0, "me", 1, 390, 1))
	if c := causes(r.compute(t)); c[0] {
		t.Fatalf("%v", c)
	}
}

func TestACarUnderYellowByTheGameFieldsAloneIsNotACause(t *testing.T) {
	// the game leaves flag and under_yellow unset, and they are not used
	r := newRig()
	v := yvehicle(1, "x", 2, 800, 0)
	v.UnderYellow = true
	r.setYellow(100, []int{11, 11, 11}, yvehicle(0, "me", 1, 390, 1), v)
	r.compute(t)
	r.setYellow(100.5, []int{11, 11, 11}, yvehicle(0, "me", 1, 390, 1), v)
	if c := causes(r.compute(t)); c[1] {
		t.Fatalf("%v", c)
	}
}

func TestSpeedAcrossTheStartLineIsNotMistakenForStanding(t *testing.T) {
	r := newRig()
	r.setYellow(100, []int{1, 11, 11}, yvehicle(0, "me", 1, 100, 1), yvehicle(1, "x", 2, 4990, 1))
	r.compute(t)
	r.setYellow(100.5, []int{1, 11, 11}, yvehicle(0, "me", 1, 100, 1), yvehicle(1, "x", 2, 10, 1)) // 20 m across the line in half a second
	if c := causes(r.compute(t)); c[1] {
		t.Fatal("a car that crossed the line at 40 m/s is not standing still")
	}
}

func TestOldAgentsWithoutTheNewFieldsStillWork(t *testing.T) {
	r := newRig()
	// no sector on any car, no sector flags at all
	r.setYellow(100, nil, yvehicle(0, "me", 1, 390, 0), yvehicle(1, "x", 2, 800, 0))
	st := r.compute(t)
	if st.YellowSectors == nil || len(st.YellowSectors) != 0 {
		t.Fatalf("%+v", st)
	}
	// flags but no sectors on the cars: the sectors are listed, no car can be placed in them
	r.setYellow(101, []int{1, 11, 11}, yvehicle(0, "me", 1, 390, 0), yvehicle(1, "x", 2, 800, 0))
	st = r.compute(t)
	if len(st.YellowSectors) != 1 {
		t.Fatalf("%+v", st)
	}
	for _, f := range st.Field {
		if f.Cause {
			t.Fatalf("%+v", f)
		}
	}
}

func TestFullCourseYellowIsIndependentOfTheSectorFlags(t *testing.T) {
	r := newRig()
	r.lv.UpdateScoring(sess, 100, wire.Scoring{Track: "monza", SessionType: "race", EndET: 10000, TrackLength: 5000,
		GamePhase: ip(6), SectorFlags: []int{11, 11, 11}, Vehicles: []wire.ScoringVehicle{yvehicle(0, "me", 1, 390, 1)}})
	r.lv.UpdateTelemetry(sess, 100, wire.Telemetry{CarID: 0, VehicleName: "me", Lap: 2, Fuel: 50, FuelCapacity: 90, Wear: []float64{1, 1, 1, 1}})
	st := r.compute(t)
	if !st.Fcy || len(st.YellowSectors) != 0 {
		t.Fatalf("fcy=%v sectors=%v", st.Fcy, st.YellowSectors)
	}
}

func TestStateWithYellowHasNoNullLists(t *testing.T) {
	r := newRig()
	r.setYellow(100, []int{1, 11, 11}, yvehicle(0, "me", 1, 390, 1))
	assertNoNilLists(t, "yellow", r.compute(t))
}
