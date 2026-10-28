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

// at is a car at a lap distance on a track of 5000 m.
func at(id int, dist float64, pits bool) wire.ScoringVehicle {
	x, z := dist, 0.0
	return wire.ScoringVehicle{CarID: id, VehicleName: "v", X: &x, Z: &z, InPits: pits, LapDist: &dist}
}

func lap(vs ...wire.ScoringVehicle) wire.Scoring {
	return wire.Scoring{Track: "t", TrackLength: 5000, Vehicles: vs}
}

func TestTheLapDistanceOfEntryAndExitIsLearned(t *testing.T) {
	var tr Tracker
	tr.Observe(lap(at(1, 4000, false)))
	tr.Observe(lap(at(1, 4400, true))) // enters
	tr.Observe(lap(at(1, 4700, true)))
	tr.Observe(lap(at(1, 4900, false))) // leaves
	l := tr.Lane("t")
	if l == nil || l.EntryDist == nil || *l.EntryDist != 4400 || l.ExitDist == nil || *l.ExitDist != 4900 {
		t.Fatalf("%+v", l)
	}
	if d, ok := l.Stretch(5000); !ok || d != 500 {
		t.Fatalf("stretch %v %v", d, ok)
	}
}

func TestTheStretchWrapsWhenTheStartLineIsInsideThePitLane(t *testing.T) {
	var tr Tracker
	tr.Observe(lap(at(1, 4500, false)))
	tr.Observe(lap(at(1, 4800, true))) // enters before the line
	tr.Observe(lap(at(1, 150, false))) // leaves after it
	l := tr.Lane("t")
	if d, ok := l.Stretch(5000); !ok || d != 350 {
		t.Fatalf("stretch %v %v: %+v", d, ok, l)
	}
}

func TestEntriesOnBothSidesOfTheLineAverageAcrossIt(t *testing.T) {
	var tr Tracker
	tr.Observe(lap(at(1, 4000, false), at(2, 4000, false)))
	tr.Observe(lap(at(1, 4990, true))) // enters 10 m before the line
	tr.Observe(lap(at(2, 10, true)))   // another car enters 10 m after it
	l := tr.Lane("t")
	if l.EntryDist == nil || !(*l.EntryDist > 4990 || *l.EntryDist < 10) {
		t.Fatalf("the mean of 4990 and 10 must stay near the line: %+v", l.EntryDist)
	}
}

func TestLaneStretchNeedsBothDistancesAndASaneLength(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := map[string]Lane{
		"no distances":   {},
		"only the entry": {EntryDist: f(100)},
		"too long":       {EntryDist: f(100), ExitDist: f(3000)},
		"no length":      {EntryDist: f(100), ExitDist: f(100)},
	}
	for name, l := range cases {
		if d, ok := l.Stretch(5000); ok {
			t.Errorf("%s: %v", name, d)
		}
	}
	if _, ok := (Lane{EntryDist: f(1), ExitDist: f(2)}).Stretch(0); ok {
		t.Error("a track length of 0 gives no stretch")
	}
}

func TestStoredDistancesSurviveALoad(t *testing.T) {
	var tr Tracker
	e, x := 4800.0, 150.0
	tr.Load(Lane{Track: "t", EntryDist: &e, ExitDist: &x})
	l := tr.Lane("t")
	if l == nil || l.EntryDist == nil || *l.EntryDist != 4800 || *l.ExitDist != 150 {
		t.Fatalf("%+v", l)
	}
}

// A pass through the pit lane of the Nordschleife (20832 m): the entry is on the last stretch of the lap, the line lies
// inside the lane and the exit is just after it.
const ring = 20832.0

type pos struct {
	dist   *float64
	x, z   float64
	inPits bool
	state  int
}

func d(v float64) *float64 { return &v }

func ringSample(p pos) wire.Scoring {
	x, z := p.x, p.z
	return wire.Scoring{Track: "ring", TrackLength: ring, Vehicles: []wire.ScoringVehicle{
		{CarID: 1, VehicleName: "v", X: &x, Z: &z, LapDist: p.dist, InPits: p.inPits, PitState: p.state}}}
}

// withStop is a pass that stops in its box; drive is one that does not.
func withStop() []pos {
	return []pos{
		{d(20300), -1300, -1900, false, 0}, // before the lane
		{d(20400), -1174, -1842, true, 2},  // enters
		{d(20550), -1100, -1780, true, 2},
		{d(20700), -1000, -1700, true, 3}, // stopped in the box
		{d(20700), -1000, -1700, true, 3},
		{d(20810), -900, -1600, true, 4}, // leaves the box
		{d(80), -800, -1500, true, 4},    // across the line, still in the lane
		{d(319), -588, -1362, false, 0},  // exits
		{d(600), -400, -1200, false, 0},
	}
}

func drive() []pos {
	p := withStop()
	for i := range p {
		if p[i].state == 3 {
			p[i].state = 2
		}
	}
	return p
}

func replay(tr *Tracker, ps []pos) {
	for _, p := range ps {
		tr.Observe(ringSample(p))
	}
}

func check(t *testing.T, l *Lane, entry, exit, stretch float64) {
	t.Helper()
	if l == nil || l.EntryDist == nil || l.ExitDist == nil {
		t.Fatalf("both distances must be learned: %+v", l)
	}
	if *l.EntryDist < entry-1 || *l.EntryDist > entry+1 || *l.ExitDist < exit-1 || *l.ExitDist > exit+1 {
		t.Fatalf("entry %v exit %v, want %v and %v", *l.EntryDist, *l.ExitDist, entry, exit)
	}
	got, ok := l.Stretch(ring)
	if !ok || got < stretch-2 || got > stretch+2 {
		t.Fatalf("stretch %v %v, want %v", got, ok, stretch)
	}
}

func TestAPassWithAStopLearnsBothDistancesAcrossTheLine(t *testing.T) {
	var tr Tracker
	replay(&tr, withStop())
	l := tr.Lane("ring")
	check(t, l, 20400, 319, 751) // 20832 - 20400 + 319
	if len(l.Boxes) != 1 {
		t.Fatalf("%+v", l.Boxes)
	}
}

func TestADriveThroughLearnsBothDistancesToo(t *testing.T) {
	var tr Tracker
	replay(&tr, drive())
	check(t, tr.Lane("ring"), 20400, 319, 751)
}

func TestSeveralPassesKeepTheDistancesNearTheLine(t *testing.T) {
	var tr Tracker
	replay(&tr, withStop())
	second := withStop()
	second[1].dist, second[7].dist = d(20410), d(305) // the next pass is a few metres off
	replay(&tr, second)
	check(t, tr.Lane("ring"), 20405, 312, 739)
}

func TestTheEntryUsesTheSampleBeforeItWhenTheLaneSampleHasNoLapDistance(t *testing.T) {
	var tr Tracker
	ps := withStop()
	ps[1].dist = nil // the lap distance of the entry sample is missing
	replay(&tr, ps)
	check(t, tr.Lane("ring"), 20300, 319, 851) // the last distance seen outside the lane
}

func TestTheExitUsesTheNextSampleWhenTheExitSampleHasNoLapDistance(t *testing.T) {
	var tr Tracker
	ps := withStop()
	ps[7].dist = nil // exit without a lap distance
	replay(&tr, ps)
	check(t, tr.Lane("ring"), 20400, 600, 1032) // the next sample outside the lane gives it
}

func TestNoDistanceIsMadeUpWhenTheLapDistanceNeverComes(t *testing.T) {
	var tr Tracker
	ps := withStop()
	for i := range ps {
		ps[i].dist = nil
	}
	replay(&tr, ps)
	l := tr.Lane("ring")
	if l == nil || l.Entry == nil || l.Exit == nil || l.EntryDist != nil || l.ExitDist != nil {
		t.Fatalf("points without distances: %+v", l)
	}
}

func TestAStoredLaneWithoutAnEntryDistanceGetsOneOnTheNextPass(t *testing.T) {
	// the row as it is today: points and boxes and an exit distance, but no entry distance
	entry, exit := Point{-1174.3, -1841.9}, Point{-588.3, -1361.9}
	boxes := []Point{{-1000, -1700}, {-990, -1690}}
	exitDist := 319.14
	var tr Tracker
	tr.Load(Lane{Track: "ring", Entry: &entry, Exit: &exit, Boxes: boxes, ExitDist: &exitDist})
	if l := tr.Lane("ring"); l.EntryDist != nil || l.ExitDist == nil {
		t.Fatalf("%+v", l)
	}
	replay(&tr, withStop())
	l := tr.Lane("ring")
	if l.EntryDist == nil || *l.EntryDist < 20399 || *l.EntryDist > 20401 {
		t.Fatalf("entry distance after the pass: %+v", l)
	}
	if got, ok := l.Stretch(ring); !ok || got < 740 || got > 760 {
		t.Fatalf("stretch %v %v", got, ok)
	}
	// the stored points and boxes are kept (the new pass moves the points only a little, the same box is not added again)
	if l.Entry == nil || l.Exit == nil || len(l.Boxes) != 2 {
		t.Fatalf("lost data: %+v", l)
	}
	if l.Exit[0] < -600 || l.Exit[0] > -580 || l.Entry[0] > -1160 || l.Entry[0] < -1180 {
		t.Fatalf("points: %+v %+v", l.Entry, l.Exit)
	}
}
