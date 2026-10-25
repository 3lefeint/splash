package trackmap

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

const length = 2000.0

// circle puts a car on a circular track with the given lap length.
func at(dist float64) (x, z float64) {
	r := length / (2 * math.Pi)
	a := dist / length * 2 * math.Pi
	return r * math.Cos(a), r * math.Sin(a)
}

func scoring(laps int, dist float64, pits bool) wire.Scoring {
	x, z := at(dist)
	return wire.Scoring{Track: "t", TrackLength: length, Vehicles: []wire.ScoringVehicle{
		{CarID: 1, VehicleName: "car", Laps: laps, InPits: pits, LapDist: &dist, X: &x, Z: &z}}}
}

func drive(b *Builder, laps int, step float64, pitAt float64) *Map {
	var got *Map
	for d := 10.0; d < length; d += step {
		if m := b.Observe(scoring(laps, d, pitAt > 0 && d >= pitAt)); m != nil {
			got = m
		}
	}
	if m := b.Observe(scoring(laps+1, 5, false)); m != nil {
		got = m
	}
	return got
}

func TestCleanLapGivesAnOutlineThatFollowsTheTrack(t *testing.T) {
	m := drive(NewBuilder(), 3, 60, 0)
	if m == nil || len(m.Points) != Points || m.Length != length || m.Track != "t" {
		t.Fatalf("unexpected map %+v", m)
	}
	r := length / (2 * math.Pi)
	for i, p := range m.Points {
		if d := math.Abs(math.Hypot(p[0], p[1]) - r); d > 2 {
			t.Fatalf("point %d is %.1f m off the circle", i, d)
		}
	}
	x0, z0 := at(0)
	if math.Hypot(m.Points[0][0]-x0, m.Points[0][1]-z0) > 5 {
		t.Fatalf("point 0 is not at the start line: %v", m.Points[0])
	}
}

func TestLapWithPitVisitGivesNoMap(t *testing.T) {
	if m := drive(NewBuilder(), 3, 60, 800); m != nil {
		t.Fatal("a lap through the pits must not make a map")
	}
}

func TestSparseLapGivesNoMap(t *testing.T) {
	if m := drive(NewBuilder(), 3, 400, 0); m != nil {
		t.Fatal("a lap with gaps of 400 m must not make a map")
	}
}

func TestFirstLapStartedMidTrackIsSkipped(t *testing.T) {
	b := NewBuilder()
	var got *Map
	for d := 900.0; d < length; d += 60 {
		if m := b.Observe(scoring(3, d, false)); m != nil {
			got = m
		}
	}
	if m := b.Observe(scoring(4, 5, false)); m != nil {
		got = m
	}
	if got != nil {
		t.Fatal("a lap seen only from the middle must not make a map")
	}
}

func TestVehiclesWithoutPositionsAreIgnored(t *testing.T) {
	b := NewBuilder()
	if m := b.Observe(wire.Scoring{Track: "t", TrackLength: length, Vehicles: []wire.ScoringVehicle{{CarID: 1, VehicleName: "c"}}}); m != nil {
		t.Fatal("unexpected map")
	}
}

func TestPartialShowsTheLapSoFar(t *testing.T) {
	b := NewBuilder()
	if b.Partial("t", length) != nil {
		t.Fatal("progress before any sample")
	}
	for d := 10.0; d <= 1000; d += 10 {
		b.Observe(scoring(3, d, false))
	}
	p := b.Partial("t", length)
	if p == nil || p.Track != "t" || p.Covered < 0.49 || p.Covered > 0.51 || len(p.Points) < 50 || len(p.Points) > maxProgressPoints+1 {
		t.Fatalf("%+v", p)
	}
	// a pit visit spoils the lap: no progress is reported for it
	b.Observe(scoring(3, 1010, true))
	if b.Partial("t", length) != nil {
		t.Fatal("progress for a lap with a pit visit")
	}
	// the next lap starts again from the line
	b.Observe(scoring(4, 20, false))
	b.Observe(scoring(4, 40, false))
	if p := b.Partial("t", length); p == nil || p.Covered > 0.05 {
		t.Fatalf("%+v", p)
	}
}
