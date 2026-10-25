package engine

import (
	"context"
	"testing"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func TestWeatherHistorySurvivesARestartOfTheEngine(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	w := func(track float64) *wire.Weather { return &wire.Weather{TrackTemp: &track} }
	e := New(st, &live.State{})
	for i, temp := range []float64{20, 21, 22} {
		e.weatherHistory(ctx, "s", float64(i)*weatherEvery, w(temp))
	}
	e.weatherHistory(ctx, "s", 2*weatherEvery+10, w(99)) // too soon after the last point: not stored
	if pts, _ := st.WeatherPoints(ctx, "s", 100); len(pts) != 3 {
		t.Fatalf("stored %d points", len(pts))
	}

	// a new engine, as after a restart of the backend, continues with the stored history
	e2 := New(st, &live.State{})
	h := e2.weatherHistory(ctx, "s", 3*weatherEvery, w(23))
	if len(h) != 4 || *h[0].Track != 20 || *h[3].Track != 23 {
		t.Fatalf("%+v", h)
	}
	// another session starts its own history
	if h := e2.weatherHistory(ctx, "other", 5, w(10)); len(h) != 1 {
		t.Fatalf("%+v", h)
	}
}
