package engine

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/segments"
)

const tl = 2000.0

// run feeds laps of one car into a recorder: speed(d) in m/s, samples every dt seconds, starting on the line.
func run(r *segments.Recorder, key string, laps int, speed func(float64) float64, fuel, brakeAt float64) {
	et, d, f := 0.0, 0.0, 80.0
	for total := 0.0; total < float64(laps)*tl; {
		brake := -1.0
		if brakeAt >= 0 {
			brake = 0
			if d >= brakeAt && d < brakeAt+60 {
				brake = 0.8
			}
		}
		r.Observe("s", key, tl, segments.Sample{Dist: d, ET: et, Fuel: f, Brake: brake})
		step := speed(d) * 0.4
		d += step
		total += step
		f -= fuel * step
		et += 0.4
		if d >= tl {
			d -= tl
		}
	}
}

func TestTrackStatsShowWhereWeGainOnTheCarAheadAndHowTheLapIsSpent(t *testing.T) {
	field, own := &segments.Recorder{}, &segments.Recorder{}
	me := carKey("me", 0)
	mine := func(d float64) float64 { // a slow corner at 400-600 m
		if d >= 400 && d < 600 {
			return 30
		}
		return 50
	}
	run(field, me, 6, mine, 0, -1)
	run(own, me, 6, mine, 0.001, 400)
	run(field, carKey("front", 1), 6, func(float64) float64 { return 45 }, 0, -1) // slower on the straights, faster in the corner

	e := New(nil, nil)
	e.Field, e.Own = field, own
	opp := Opponents{Ahead: &Opponent{CarID: 1, VehicleName: "front", Driver: "F. Ront"}}
	ts := e.trackStats("s", "t", Car{CarID: 0, VehicleName: "me"}, tl, opp)
	if ts == nil || ts.Segments != segments.N || ts.Laps != statLaps || ts.Ahead == nil || ts.Ahead.Name != "F. Ront" {
		t.Fatalf("%+v", ts)
	}
	straight, corner := ts.Ahead.Delta[2], ts.Ahead.Delta[12] // 80-120 m and 480-520 m
	if straight == nil || corner == nil || *straight > -0.05 || *corner < 0.3 {
		t.Fatalf("straight %v corner %v: we are faster on the straight and lose in the corner", straight, corner)
	}
	if ts.Behind != nil {
		t.Fatal("a car that does not exist was compared")
	}
	if ts.Speed == nil || math.Abs(*ts.Speed[12]-30) > 1 || math.Abs(*ts.Speed[2]-50) > 1 {
		t.Fatalf("speed %v", ts.Speed)
	}
	if ts.Fuel == nil || math.Abs(*ts.Fuel[3]-0.001*tl/segments.N) > 1e-5 {
		t.Fatalf("fuel %v", ts.Fuel[3])
	}
	if ts.Brake == nil || *ts.Brake[10] < 0.7 || *ts.Brake[30] != 0 { // 400-440 m is braking
		t.Fatalf("brake %v %v", ts.Brake[10], ts.Brake[30])
	}
	if ts.Best == nil {
		t.Fatal("no comparison with our best lap")
	}
}

type fakePits struct{}

func (fakePits) Lane(track string) *pitlane.Lane {
	return &pitlane.Lane{Track: track, Entry: &pitlane.Point{1, 2}, Boxes: []pitlane.Point{{3, 4}}}
}

func (fakePits) StoppedAt(vehicle string, id int) *pitlane.Point { return &pitlane.Point{3, 4} }

func TestTrackStatsCarryThePitLaneAndAreNilWithoutData(t *testing.T) {
	e := New(nil, nil)
	if ts := e.trackStats("s", "t", Car{VehicleName: "me"}, tl, Opponents{}); ts != nil {
		t.Fatalf("%+v", ts)
	}
	e.Pits = fakePits{}
	ts := e.trackStats("s", "t", Car{VehicleName: "me"}, tl, Opponents{})
	if ts == nil || ts.Pits == nil || ts.Pits.Entry[0] != 1 || len(ts.Pits.Boxes) != 1 || ts.Pits.OurBox[0] != 3 {
		t.Fatalf("%+v", ts)
	}
}

func TestOffTrackNeedsALateralPositionBeyondTheEdgePlusMargin(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	if !offTrack(f(-9), f(6), false) || offTrack(f(7), f(6), false) || offTrack(f(9), f(6), true) || offTrack(nil, f(6), false) || offTrack(f(9), f(0), false) {
		t.Fatal("off-track rule")
	}
}
