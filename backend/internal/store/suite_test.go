package store

import (
	"context"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wear"
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
		s.SaveEvent(ctx, sess, 1000.4, entry)                                                          // the other agent saw the same entry 0.4 s later
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

	t.Run("track map round trip", func(t *testing.T) {
		if _, ok, _ := s.GetTrackMap(ctx, "ring"); ok {
			t.Fatal("unexpected map")
		}
		m := trackmap.Map{Track: "ring", Length: 1000, Points: [][2]float64{{0, 0}, {1.5, -2}, {3, 4}}}
		if err := s.SaveTrackMap(ctx, m); err != nil {
			t.Fatal(err)
		}
		m.Length = 1200
		s.SaveTrackMap(ctx, m) // replaces
		got, ok, err := s.GetTrackMap(ctx, "ring")
		if err != nil || !ok || got.Length != 1200 || len(got.Points) != 3 || got.Points[1] != [2]float64{1.5, -2} {
			t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
		}
		s.DeleteTrackMap(ctx, "ring")
		if _, ok, _ := s.GetTrackMap(ctx, "ring"); ok {
			t.Fatal("map still there")
		}
	})

	t.Run("tracks with an outline are listed by name", func(t *testing.T) {
		before, err := s.ListTracks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		s.SaveTrackMap(ctx, trackmap.Map{Track: "zz-list", Length: 500, Points: [][2]float64{{0, 0}, {1, 1}}})
		defer s.DeleteTrackMap(ctx, "zz-list")
		after, _ := s.ListTracks(ctx)
		if len(after) != len(before)+1 || after[len(after)-1].Track != "zz-list" || after[len(after)-1].Length != 500 {
			t.Fatalf("before=%v after=%v", before, after)
		}
	})

	t.Run("corners round trip", func(t *testing.T) {
		if got, err := s.GetCorners(ctx, "ring"); err != nil || len(got) != 0 {
			t.Fatalf("got=%v err=%v", got, err)
		}
		from, to := 120.5, 260.0
		in := []Corner{{Name: "Hatzenbach", From: &from, To: &to, Label: &[2]float64{-10.5, 20}}, {Name: "Flugplatz"}}
		if err := s.SaveCorners(ctx, "ring", in); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetCorners(ctx, "ring")
		if err != nil || len(got) != 2 || got[0].Name != "Hatzenbach" || got[0].From == nil || *got[0].From != 120.5 || got[0].To == nil || *got[0].To != 260 || got[0].Label == nil || got[0].Label[0] != -10.5 || got[1].From != nil || got[1].Label != nil {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		s.SaveCorners(ctx, "ring", nil)
		if got, _ := s.GetCorners(ctx, "ring"); len(got) != 0 {
			t.Fatalf("corners still there: %+v", got)
		}
	})

	t.Run("pit setups keep their order and are replaced by name", func(t *testing.T) {
		a := PitSetup{Car: "gt3", Name: "Max", Steps: []wire.Step{{Category: "FUEL:", Choice: "max"}}}
		b := PitSetup{Car: "gt3", Name: "Medium", Steps: []wire.Step{{Category: "TIRES:", Choice: "medium"}}}
		for _, x := range []PitSetup{a, b, {Car: "other", Name: "x", Steps: a.Steps}} {
			if err := s.SavePitSetup(ctx, x); err != nil {
				t.Fatal(err)
			}
		}
		a.Steps = []wire.Step{{Category: "FUEL:", Choice: "60"}}
		s.SavePitSetup(ctx, a)
		got, err := s.PitSetups(ctx, "gt3")
		if err != nil || len(got) != 2 || got[0].Name != "Max" || got[0].Steps[0].Choice != "60" || got[1].Name != "Medium" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		s.DeletePitSetup(ctx, "gt3", "Max")
		if got, _ := s.PitSetups(ctx, "gt3"); len(got) != 1 || got[0].Name != "Medium" {
			t.Fatalf("got=%+v", got)
		}
	})

	t.Run("sessions keep their start and count what is stored", func(t *testing.T) {
		t0 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
		s.TouchSession(ctx, SessionInfo{SessionID: "sess", Track: "ring", SessionType: "race", LastSeen: t0, LastET: 10})
		s.TouchSession(ctx, SessionInfo{SessionID: "sess", Car: "M4", CarID: 0, LastSeen: t0.Add(time.Minute), LastET: 70})
		s.TouchSession(ctx, SessionInfo{SessionID: "sess", CarID: -1, LastSeen: t0.Add(time.Minute), LastET: 70}) // unknown keeps the known id
		s.TouchSession(ctx, SessionInfo{SessionID: "older", Track: "x", LastSeen: t0.Add(-time.Hour)})
		s.SaveLap(ctx, "sess", wire.Lap{CarID: 0, VehicleName: "M4", Lap: 1})
		s.SaveLap(ctx, "sess", wire.Lap{CarID: 1, VehicleName: "other", Lap: 1})
		s.SaveLap(ctx, "sess", wire.Lap{CarID: 5, VehicleName: "M4", Lap: 1})                              // a rival with the same vehicle name
		s.SaveEvent(ctx, "sess", 50, wire.Event{Kind: wire.EventPitEntry, CarID: 1, VehicleName: "other"}) // a rival's stop is not counted
		one, zero := 1, 0
		amb := 21.5
		c := LapConditions{SessionID: "sess", CarID: 0, VehicleName: "M4", Lap: 1, ET: 90, Control: &one, Ambient: &amb,
			Surface: []float64{80, 81, 82, 83}, CompoundFront: "Medium"}
		s.SaveLapConditions(ctx, c)
		c.Lap, c.Control = 2, &zero
		s.SaveLapConditions(ctx, c)
		c.Lap, c.Control, c.Surface = 3, nil, nil
		s.SaveLapConditions(ctx, c)
		list, err := s.Sessions(ctx)
		if err != nil || len(list) < 2 || list[0].SessionID != "sess" {
			t.Fatalf("list=%+v err=%v", list, err)
		}
		g := list[0]
		if g.Track != "ring" || g.Car != "M4" || g.SessionType != "race" || !g.StartedAt.Equal(t0) || !g.LastSeen.Equal(t0.Add(time.Minute)) ||
			g.LastET != 70 || g.Laps != 1 || g.Stops != 0 || g.Conditions != 3 || g.AILaps != 1 || g.PlayerLaps != 1 {
			t.Fatalf("summary=%+v", g)
		}
		got, err := s.LapConditions(ctx, "sess")
		if err != nil || len(got) != 3 || got[0].Lap != 1 || got[0].Surface[3] != 83 || *got[0].Ambient != 21.5 ||
			*got[0].Control != 1 || got[2].Control != nil || got[2].Surface != nil || got[0].CompoundFront != "Medium" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("weather points come back oldest first, limited to the newest", func(t *testing.T) {
		r := 0.2
		for _, et := range []float64{180, 60, 120, 240} {
			a := et / 10
			s.SaveWeatherPoint(ctx, "wx", WeatherPoint{ET: et, Ambient: &a, Rain: &r})
		}
		a := 99.0
		s.SaveWeatherPoint(ctx, "wx", WeatherPoint{ET: 120, Ambient: &a}) // replaces the point at 120
		s.SaveWeatherPoint(ctx, "other", WeatherPoint{ET: 5})
		got, err := s.WeatherPoints(ctx, "wx", 3)
		if err != nil || len(got) != 3 || got[0].ET != 120 || got[2].ET != 240 || *got[0].Ambient != 99 || got[0].Rain != nil || *got[1].Rain != 0.2 {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("pit lane round trip", func(t *testing.T) {
		if _, ok, _ := s.GetPitLane(ctx, "ring"); ok {
			t.Fatal("unexpected lane")
		}
		l := pitlane.Lane{Track: "ring", Entry: &pitlane.Point{1, 2}, Boxes: []pitlane.Point{{3, 4}, {5.5, -6}}}
		if err := s.SavePitLane(ctx, l); err != nil {
			t.Fatal(err)
		}
		l.Exit = &pitlane.Point{9, 9}
		s.SavePitLane(ctx, l) // replaces
		got, ok, err := s.GetPitLane(ctx, "ring")
		if err != nil || !ok || got.Entry == nil || got.Entry[1] != 2 || got.Exit == nil || got.Exit[0] != 9 || len(got.Boxes) != 2 || got.Boxes[1][1] != -6 {
			t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
		}
	})

	t.Run("lap wear round trip", func(t *testing.T) {
		var w wear.LapWear
		w.Lap, w.Coverage = 4, 0.97
		w.Loss[2][7] = 0.00031
		s.SaveLapWear(ctx, "ws", 0, "v", w)
		w.Lap = 3
		s.SaveLapWear(ctx, "ws", 0, "v", w)
		w.Lap, w.Coverage = 4, 0.5
		s.SaveLapWear(ctx, "ws", 0, "v", w) // replaces lap 4
		s.SaveLapWear(ctx, "ws", 1, "other", w)
		got, err := s.LapWears(ctx, "ws", 0, "v")
		if err != nil || len(got) != 2 || got[0].Lap != 3 || got[1].Lap != 4 || got[1].Coverage != 0.5 || got[1].Loss[2][7] != 0.00031 {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("stationary time is added to the stop it belongs to", func(t *testing.T) {
		sess := "stat"
		s.SaveEvent(ctx, sess, 100, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "v"})
		s.SaveEvent(ctx, sess, 190, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "v"}) // a different, later stop
		s.AddStopped(ctx, sess, 0, "v", Standing{From: 120, To: 150, Work: 20, FuelTime: 15})
		s.AddStopped(ctx, sess, 0, "v", Standing{From: 160, To: 165, Work: 2}) // same stop, a second interval
		s.AddStopped(ctx, sess, 0, "v", Standing{From: 200, To: 230})
		s.AddStopped(ctx, sess, 0, "v", Standing{From: 50, To: 60, Work: 9}) // before any stop: ignored
		got, _ := s.Stops(ctx, sess)
		if len(got) != 2 || got[0].Stopped != 35 || got[0].Work != 22 || got[0].FuelTime != 15 || got[1].Stopped != 30 {
			t.Fatalf("%+v", got)
		}
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
