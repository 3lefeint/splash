package ingest

import (
	"context"
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const trackLen = 2000.0

func scoringAt(laps int, dist float64) wire.Scoring {
	r := trackLen / (2 * math.Pi)
	a := dist / trackLen * 2 * math.Pi
	x, z := r*math.Cos(a), r*math.Sin(a)
	return wire.Scoring{Track: "ring", TrackLength: trackLen, Vehicles: []wire.ScoringVehicle{
		{CarID: 1, VehicleName: "car", Laps: laps, LapDist: &dist, X: &x, Z: &z}}}
}

func TestRecorderStoresTheMapAfterACleanLap(t *testing.T) {
	st := store.NewMemory()
	r := NewMapRecorder(st, nil)
	ctx := context.Background()
	for d := 10.0; d < trackLen; d += 60 {
		r.Observe(ctx, "s1", scoringAt(2, d))
	}
	if _, ok, _ := st.GetTrackMap(ctx, "ring"); ok {
		t.Fatal("map stored before the lap was complete")
	}
	r.Observe(ctx, "s1", scoringAt(3, 5))
	m, ok, _ := st.GetTrackMap(ctx, "ring")
	if !ok || m.Length != trackLen || len(m.Points) == 0 {
		t.Fatalf("map=%+v ok=%v", m, ok)
	}
}

func TestRecorderKeepsAnExistingMap(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	st.SaveTrackMap(ctx, trackmapOf("ring", 1))
	r := NewMapRecorder(st, nil)
	for d := 10.0; d < trackLen; d += 60 {
		r.Observe(ctx, "s1", scoringAt(2, d))
	}
	r.Observe(ctx, "s1", scoringAt(3, 5))
	if m, _, _ := st.GetTrackMap(ctx, "ring"); m.Length != 1 {
		t.Fatalf("the existing map was replaced: %+v", m)
	}
}

func trackmapOf(track string, length float64) trackmap.Map {
	return trackmap.Map{Track: track, Length: length, Points: [][2]float64{{0, 0}}}
}
