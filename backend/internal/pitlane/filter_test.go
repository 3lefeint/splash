package pitlane

import (
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

// The 24h Nürburgring as it was recorded: 25321 m long, the lane starts at 25193 m and ends at 319 m, 447 m in all.
const nord = 25321.0

func nsample(dist, x float64, pits bool, state int) wire.Scoring {
	z := 0.0
	return wire.Scoring{Track: "nord", TrackLength: nord, Vehicles: []wire.ScoringVehicle{
		{CarID: 1, VehicleName: "v", X: &x, Z: &z, LapDist: &dist, InPits: pits, PitState: state}}}
}

// pass is a car that drives through the lane the way the recording shows it.
func pass(tr *Tracker, src string) {
	observeFrom(tr, src, nsample(25150, 25150, false, 0))
	observeFrom(tr, src, nsample(25193, 25193, true, 2)) // enters
	observeFrom(tr, src, nsample(80, 80, true, 4))       // across the line, still in the lane
	observeFrom(tr, src, nsample(319, 319, false, 0))    // exits
	observeFrom(tr, src, nsample(400, 400, false, 0))
}

func wantLane(t *testing.T, tr *Tracker, entry, exit float64) {
	t.Helper()
	l := tr.Lane("nord")
	if l == nil || l.EntryDist == nil || l.ExitDist == nil {
		t.Fatalf("both distances must be known: %+v", l)
	}
	if *l.EntryDist < entry-3 || *l.EntryDist > entry+3 || *l.ExitDist < exit-3 || *l.ExitDist > exit+3 {
		t.Fatalf("entry %.1f exit %.1f, want about %.0f and %.0f", *l.EntryDist, *l.ExitDist, entry, exit)
	}
	if d, ok := l.Stretch(nord); !ok || d < 440 || d > 455 {
		t.Fatalf("stretch %v %v", d, ok)
	}
}

func events(tr *Tracker) *[]Event {
	var got []Event
	tr.OnEvent = func(e Event) { got = append(got, e) }
	return &got
}

func TestAPassLearnsTheLaneOfTheRing(t *testing.T) {
	var tr Tracker
	got := events(&tr)
	pass(&tr, "a")
	wantLane(t, &tr, 25193, 319)
	if len(*got) != 2 || !(*got)[0].Accepted || (*got)[0].Kind != "entry" || !(*got)[1].Accepted || (*got)[1].Kind != "exit" {
		t.Fatalf("events %+v", *got)
	}
}

func TestACarThatJumpsIntoItsBoxIsNotAnEntry(t *testing.T) {
	var tr Tracker
	got := events(&tr)
	observeFrom(&tr, "a", nsample(12500, 12500, false, 0))
	observeFrom(&tr, "a", nsample(60, 60, true, 4)) // put into its box, as "return to the garage" does
	if l := tr.Lane("nord"); l != nil && l.EntryDist != nil {
		t.Fatalf("a jump to the box was counted: %+v", l)
	}
	if len(*got) != 1 || (*got)[0].Accepted || (*got)[0].Reason != "jump" {
		t.Fatalf("events %+v", *got) // the jump is the first reason named, the pit state 4 would not do either
	}
}

func TestOnlyPitStateTwoCountsAsAnEntry(t *testing.T) {
	for state, want := range map[int]bool{0: false, 1: false, 2: true, 3: false, 4: false} {
		var tr Tracker
		observeFrom(&tr, "a", nsample(25150, 25150, false, 0))
		observeFrom(&tr, "a", nsample(25193, 25193, true, state))
		l := tr.Lane("nord")
		if got := l != nil && l.EntryDist != nil; got != want {
			t.Errorf("pit state %d: entry counted = %v, want %v", state, got, want)
		}
	}
}

func TestAnEntryWithTheRightStateButAJumpIsNotCounted(t *testing.T) {
	var tr Tracker
	got := events(&tr)
	observeFrom(&tr, "a", nsample(12500, 12500, false, 0))
	observeFrom(&tr, "a", nsample(25193, 25193, true, 2)) // 12 km in one sample
	if (*got)[0].Accepted || (*got)[0].Reason != "jump" {
		t.Fatalf("events %+v", *got)
	}
}

func TestAStaleLapDistanceDoesNotHideAJumpOfThePosition(t *testing.T) {
	var tr Tracker
	got := events(&tr)
	observeFrom(&tr, "a", nsample(25150, 25150, false, 0))
	// the lap distance has not moved yet, the position has: the car was put somewhere else
	observeFrom(&tr, "a", wire.Scoring{Track: "nord", TrackLength: nord, Vehicles: []wire.ScoringVehicle{
		{CarID: 1, VehicleName: "v", X: ptr(9000), Z: ptr(500), LapDist: ptr(25150), InPits: true, PitState: 2}}})
	if (*got)[0].Accepted || (*got)[0].Reason != "jump" {
		t.Fatalf("events %+v", *got)
	}
}

func ptr(v float64) *float64 { return &v }

func TestSamplesFarApartInTimeCannotBeJudged(t *testing.T) {
	var tr Tracker
	got := events(&tr)
	tr.Observe("a", 100, nsample(25150, 25150, false, 0))
	tr.Observe("a", 160, nsample(25193, 25193, true, 2)) // a minute later
	if (*got)[0].Accepted || (*got)[0].Reason != "gap" {
		t.Fatalf("events %+v", *got)
	}
	tr.Observe("a", 160, nsample(25193, 25193, false, 0)) // not later at all
	tr.Observe("a", 160, nsample(25193, 25193, true, 2))
	if last := (*got)[len(*got)-1]; last.Accepted || last.Reason != "gap" {
		t.Fatalf("the same time twice: %+v", last)
	}
}

func TestOnlyAnExitInAFittingStateCounts(t *testing.T) {
	for state, want := range map[int]bool{0: true, 1: false, 2: false, 3: false, 4: true} {
		var tr Tracker
		observeFrom(&tr, "a", nsample(80, 80, true, 4)) // first sight in the lane
		observeFrom(&tr, "a", nsample(319, 319, false, state))
		l := tr.Lane("nord")
		if got := l != nil && l.ExitDist != nil; got != want {
			t.Errorf("pit state %d: exit counted = %v, want %v", state, got, want)
		}
	}
}

func TestAWrongFirstObservationIsOutvotedByTheNextPasses(t *testing.T) {
	var tr Tracker
	// a car that moves plausibly and has the right state, but at the wrong place: a false first entry
	observeFrom(&tr, "a", nsample(12400, 12400, false, 0))
	observeFrom(&tr, "a", nsample(12500, 12500, true, 2))
	pass(&tr, "b")
	pass(&tr, "b")
	wantLane(t, &tr, 25193, 319)
}

func TestSingleFalseObservationsDoNotMoveAStoredValue(t *testing.T) {
	var tr Tracker
	e, x := 25193.0, 319.0
	tr.Load(Lane{Track: "nord", EntryDist: &e, ExitDist: &x})
	observeFrom(&tr, "a", nsample(12400, 12400, false, 0))
	observeFrom(&tr, "a", nsample(12500, 12500, true, 2)) // one false entry half a lap away
	wantLane(t, &tr, 25193, 319)
	pass(&tr, "b")
	wantLane(t, &tr, 25193, 319)
}

func TestStoredDistancesThatDoNotFitAreReplacedByTheFirstPasses(t *testing.T) {
	var tr Tracker
	e, x := 886.7, 319.0 // what was stored: the entry is wrong
	tr.Load(Lane{Track: "nord", EntryDist: &e, ExitDist: &x})
	pass(&tr, "a")
	if l := tr.Lane("nord"); l.EntryDist == nil || *l.EntryDist < 25190 || *l.EntryDist > 25196 {
		t.Fatalf("a wrong stored entry must give way to the first pass: %+v", l.EntryDist)
	}
	wantLane(t, &tr, 25193, 319)
}

func TestCarsAreFollowedPerSource(t *testing.T) {
	var tr Tracker
	got := events(&tr)
	// two agents see a car with the same name and number at different places and times; together they must not look
	// like one car that jumps in and out of the lane
	observeFrom(&tr, "a", nsample(12000, 12000, false, 0))
	observeFrom(&tr, "b", nsample(60, 60, true, 2))
	observeFrom(&tr, "a", nsample(12050, 12050, false, 0))
	observeFrom(&tr, "b", nsample(60, 60, true, 2))
	observeFrom(&tr, "a", nsample(12100, 12100, false, 0))
	if len(*got) != 0 {
		t.Fatalf("flips between sources: %+v", *got)
	}
}

func TestResetForgetsTheLaneOfATrackOnly(t *testing.T) {
	var tr Tracker
	pass(&tr, "a")
	tr.Load(Lane{Track: "other", Entry: &Point{1, 1}})
	tr.Reset("nord")
	if tr.Lane("nord") != nil {
		t.Fatal("the lane must be forgotten")
	}
	if tr.Lane("other") == nil {
		t.Fatal("another track must stay")
	}
	pass(&tr, "a")
	wantLane(t, &tr, 25193, 319)
}

func TestBoxesAreOnlyCountedInsideTheLane(t *testing.T) {
	var tr Tracker
	observeFrom(&tr, "a", nsample(12000, 12000, false, 0))
	observeFrom(&tr, "a", nsample(12050, 12050, false, 3)) // "stopped" on the track: no box
	if l := tr.Lane("nord"); l != nil && len(l.Boxes) != 0 {
		t.Fatalf("%+v", l.Boxes)
	}
}

// nodist is a sample without a lap distance, as the game gives the player on the shortcut before the start line.
func nodist(x float64, pits bool, state int) wire.Scoring {
	z := 0.0
	return wire.Scoring{Track: "nord", TrackLength: nord, Vehicles: []wire.ScoringVehicle{
		{CarID: 1, VehicleName: "v", X: &x, Z: &z, InPits: pits, PitState: state}}}
}

func TestAnOldDistanceIsNotUsedForAnEntryWithoutOne(t *testing.T) {
	var tr Tracker
	observeFrom(&tr, "a", nsample(4261, 4261, false, 0)) // the last distance before the gap, 16 s and kilometres earlier
	observeFrom(&tr, "a", nodist(4300, false, 1))
	observeFrom(&tr, "a", nodist(4400, false, 1))
	observeFrom(&tr, "a", nodist(4500, true, 2)) // enters without a distance, and the sample before has none either
	l := tr.Lane("nord")
	if l == nil || l.Entry == nil {
		t.Fatalf("the entry point is still learned: %+v", l)
	}
	if l.EntryDist != nil {
		t.Fatalf("an entry distance of %.0f m was taken from the old sample", *l.EntryDist)
	}
}

func TestTheSampleRightBeforeAnEntryStandsInForItsMissingDistance(t *testing.T) {
	var tr Tracker
	observeFrom(&tr, "a", nsample(25150, 25150, false, 0))
	observeFrom(&tr, "a", nodist(25190, true, 2))
	l := tr.Lane("nord")
	if l == nil || l.EntryDist == nil || *l.EntryDist != 25150 {
		t.Fatalf("%+v", l)
	}
}

func TestTheExitWaitsForADistanceOnlyForAFewSeconds(t *testing.T) {
	var tr Tracker
	tr.Observe("a", 100, nsample(300, 300, true, 4))
	tr.Observe("a", 101, nodist(319, false, 0)) // exits without a distance
	tr.Observe("a", 140, nsample(900, 319, false, 0))
	l := tr.Lane("nord")
	if l == nil || l.Exit == nil || l.ExitDist != nil {
		t.Fatalf("a distance from 40 s later is not the exit: %+v", l)
	}

	var tr2 Tracker
	tr2.Observe("a", 100, nsample(300, 300, true, 4))
	tr2.Observe("a", 101, nodist(319, false, 0))
	tr2.Observe("a", 102, nsample(350, 350, false, 0))
	if l := tr2.Lane("nord"); l == nil || l.ExitDist == nil || *l.ExitDist != 350 {
		t.Fatalf("the next sample gives the exit: %+v", l)
	}
}
