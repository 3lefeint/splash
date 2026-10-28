package pitlane

import (
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

func car(id int, x, z float64, pits bool, state int) wire.ScoringVehicle {
	return wire.ScoringVehicle{CarID: id, VehicleName: "v", X: &x, Z: &z, InPits: pits, PitState: state}
}

func sc(vs ...wire.ScoringVehicle) wire.Scoring { return wire.Scoring{Track: "t", Vehicles: vs} }

func TestEntryExitAndBoxesAreLearnedFromACarDrivingThroughTheLane(t *testing.T) {
	var tr Tracker
	tr.Observe(sc(car(1, 0, 0, false, 0)))   // first sight: no entry
	tr.Observe(sc(car(1, 100, 0, true, 2)))  // enters the lane
	tr.Observe(sc(car(1, 150, 10, true, 3))) // stops in its box
	tr.Observe(sc(car(1, 150, 10, true, 3))) // still stopped: the box is counted once
	tr.Observe(sc(car(1, 200, 10, true, 4)))
	changed := tr.Observe(sc(car(1, 260, 5, false, 0))) // leaves the lane
	if !changed {
		t.Fatal("the exit was not reported as a change")
	}
	l := tr.Lane("t")
	if l == nil || l.Entry == nil || l.Entry[0] != 100 || l.Exit == nil || l.Exit[0] != 260 || len(l.Boxes) != 1 || l.Boxes[0] != (Point{150, 10}) {
		t.Fatalf("%+v", l)
	}
	if b := tr.StoppedAt("v", 1); b == nil || *b != (Point{150, 10}) {
		t.Fatalf("our box %v", b)
	}
	if tr.StoppedAt("v", 2) != nil {
		t.Fatal("a car that never stopped has no box")
	}
}

func TestTheSameBoxIsNotAddedTwiceAndAnotherOneIs(t *testing.T) {
	var tr Tracker
	tr.Observe(sc(car(1, 0, 0, false, 0), car(2, 0, 0, false, 0)))
	tr.Observe(sc(car(1, 150, 10, true, 3), car(2, 160, 10, true, 3))) // 10 m apart: two boxes
	tr.Observe(sc(car(1, 150, 10, true, 4), car(2, 160, 10, true, 4)))
	tr.Observe(sc(car(1, 152, 11, true, 3))) // car 1 returns to its box a little off
	if l := tr.Lane("t"); len(l.Boxes) != 2 {
		t.Fatalf("%+v", l)
	}
}

func TestLoadedLaneIsKeptAndAnotherTrackStartsOver(t *testing.T) {
	var tr Tracker
	tr.Load(Lane{Track: "t", Entry: &Point{1, 2}, Boxes: []Point{{5, 5}}})
	if l := tr.Lane("t"); l == nil || l.Entry[0] != 1 || len(l.Boxes) != 1 {
		t.Fatalf("%+v", l)
	}
	tr.Observe(wire.Scoring{Track: "other"})
	if tr.Lane("t") != nil || tr.Lane("other") != nil {
		t.Fatal("old lane survived a track change")
	}
}
