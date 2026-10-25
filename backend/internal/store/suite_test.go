package store

import (
	"context"
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

func f(v float64) *float64 { return &v }

// runSuite checks the behaviour every Store implementation must share.
func runSuite(t *testing.T, s Store) {
	ctx := context.Background()

	t.Run("lap merge keeps non-null fields from both agents", func(t *testing.T) {
		sess := "merge"
		// Driver agent: fuel data, no sector times.
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, Driver: "a", LapTime: f(98), FuelStart: f(60), FuelEnd: f(57), Wear: []float64{1, 1, 1, 1}})
		// Spotter agent: sector times, no fuel, flags a yellow.
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, LapTime: f(98), S1: f(30), S2: f(32), S3: f(36), FCY: true})
		// A resend of the first message changes nothing.
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, Driver: "a", LapTime: f(98), FuelStart: f(60), FuelEnd: f(57)})
		laps, err := s.Laps(ctx, sess)
		if err != nil || len(laps) != 1 {
			t.Fatalf("laps=%v err=%v", laps, err)
		}
		l := laps[0]
		if l.Driver != "a" || l.FuelStart == nil || *l.FuelStart != 60 || l.S3 == nil || *l.S3 != 36 || !l.FCY || len(l.Wear) != 4 {
			t.Fatalf("%+v", l)
		}
	})

	t.Run("same lap number of different vehicles stays separate", func(t *testing.T) {
		sess := "sep"
		s.SaveLap(ctx, sess, wire.Lap{CarID: 1, VehicleName: "x", Lap: 1})
		s.SaveLap(ctx, sess, wire.Lap{CarID: 1, VehicleName: "y", Lap: 1}) // reused slot id
		laps, _ := s.Laps(ctx, sess)
		if len(laps) != 2 {
			t.Fatalf("laps=%d", len(laps))
		}
	})

	t.Run("stop lifecycle is idempotent", func(t *testing.T) {
		sess := "stops"
		yes := true
		entry := wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "v"}
		exit := wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "v", FuelAdded: f(55), TyresChanged: &yes}
		swap := wire.Event{Kind: wire.EventDriverSwap, CarID: 0, VehicleName: "v"}
		for i := 0; i < 2; i++ { // both agents send the same events
			s.SaveEvent(ctx, sess, 1000, entry)
			s.SaveEvent(ctx, sess, 1030, swap)
			s.SaveEvent(ctx, sess, 1045, exit)
		}
		s.SaveEvent(ctx, sess, 1100, wire.Event{Kind: wire.EventFCYStart, CarID: 0, VehicleName: "v"}) // ignored
		stops, err := s.Stops(ctx, sess)
		if err != nil || len(stops) != 1 {
			t.Fatalf("stops=%v err=%v", stops, err)
		}
		st := stops[0]
		if st.EntryET != 1000 || st.ExitET == nil || *st.ExitET != 1045 || *st.FuelAdded != 55 || !st.TyresChanged || !st.DriverSwap {
			t.Fatalf("%+v", st)
		}
	})

	t.Run("exit without entry is ignored", func(t *testing.T) {
		sess := "orphan"
		s.SaveEvent(ctx, sess, 50, wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "v"})
		if stops, _ := s.Stops(ctx, sess); len(stops) != 0 {
			t.Fatalf("stops=%v", stops)
		}
	})

	t.Run("laps of one car", func(t *testing.T) {
		sess := "car"
		for _, l := range []int{3, 1, 2} {
			s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: l})
		}
		s.SaveLap(ctx, sess, wire.Lap{CarID: 1, VehicleName: "other", Lap: 1})
		laps, err := s.LapsOfCar(ctx, sess, 0, "v")
		if err != nil || len(laps) != 3 || laps[0].Lap.Lap != 1 || laps[2].Lap.Lap != 3 {
			t.Fatalf("laps=%v err=%v", laps, err)
		}
	})

	t.Run("params per track and car", func(t *testing.T) {
		if _, ok, _ := s.GetParams(ctx, "monza", "gt3"); ok {
			t.Fatal("unexpected params")
		}
		p := Params{Track: "monza", Car: "gt3", PitThroughGreen: 22.5, RefuelRate: 2.8, ParallelService: true, Reserve: 2}
		s.SetParams(ctx, p)
		s.SetParams(ctx, Params{Track: "monza", Car: "hyper"})
		got, ok, err := s.GetParams(ctx, "monza", "gt3")
		if err != nil || !ok || got != p {
			t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
		}
		p.PitThroughGreen = 25
		s.SetParams(ctx, p)
		if got, _, _ := s.GetParams(ctx, "monza", "gt3"); got.PitThroughGreen != 25 {
			t.Fatalf("update lost: %+v", got)
		}
		if list, _ := s.ListParams(ctx); len(list) != 2 {
			t.Fatalf("list=%v", list)
		}
	})

	t.Run("stint plan is replaced as a whole", func(t *testing.T) {
		s.SetStintPlan(ctx, []PlanRow{{"a", 3600}, {"b", 3600}, {"c", 1800}})
		s.SetStintPlan(ctx, []PlanRow{{"b", 100}, {"a", 200}})
		got, err := s.GetStintPlan(ctx)
		if err != nil || len(got) != 2 || got[0] != (PlanRow{"b", 100}) || got[1] != (PlanRow{"a", 200}) {
			t.Fatalf("got=%v err=%v", got, err)
		}
		s.SetStintPlan(ctx, nil)
		if got, _ := s.GetStintPlan(ctx); len(got) != 0 {
			t.Fatalf("got=%v", got)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		s.SetOverride(ctx, OverrideFuel, 40)
		s.SetOverride(ctx, OverrideFuel, 41)
		s.SetOverride(ctx, OverrideFuelPerLap, 3.2)
		got, err := s.GetOverrides(ctx)
		if err != nil || len(got) != 2 || got[OverrideFuel].Value != 41 {
			t.Fatalf("got=%v err=%v", got, err)
		}
		s.DeleteOverride(ctx, OverrideFuel)
		s.DeleteOverride(ctx, "missing") // not an error
		if got, _ := s.GetOverrides(ctx); len(got) != 1 {
			t.Fatalf("got=%v", got)
		}
		s.DeleteOverride(ctx, OverrideFuelPerLap)
	})

	t.Run("progress never moves backwards", func(t *testing.T) {
		if _, ok, _ := s.Progress(ctx, "agent", "p"); ok {
			t.Fatal("unexpected progress")
		}
		s.SetProgress(ctx, "agent", "p", 10)
		s.SetProgress(ctx, "agent", "p", 4)
		if seq, ok, _ := s.Progress(ctx, "agent", "p"); !ok || seq != 10 {
			t.Fatalf("seq=%d ok=%v", seq, ok)
		}
	})
}

func TestMemory(t *testing.T) { runSuite(t, NewMemory()) }
