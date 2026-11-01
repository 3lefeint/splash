package store

import (
	"context"
	"reflect"
	"strconv"
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
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, Driver: "a", LapTime: f(98), FuelStart: f(60), FuelEnd: f(57), Wear: []float64{1, 1, 1, 1}}, 0)
		// Spotter agent: sector times, no fuel, flags a yellow.
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, LapTime: f(98), S1: f(30), S2: f(32), S3: f(36), FCY: true}, 0)
		// A resend of the first message changes nothing.
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, Driver: "a", LapTime: f(98), FuelStart: f(60), FuelEnd: f(57)}, 0)
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
		s.SaveLap(ctx, sess, wire.Lap{CarID: 1, VehicleName: "x", Lap: 1}, 0)
		s.SaveLap(ctx, sess, wire.Lap{CarID: 1, VehicleName: "y", Lap: 1}, 0) // reused slot id
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
			s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: l}, 0)
		}
		s.SaveLap(ctx, sess, wire.Lap{CarID: 1, VehicleName: "other", Lap: 1}, 0)
		laps, err := s.LapsOfCar(ctx, sess, 0, "v")
		if err != nil || len(laps) != 3 || laps[0].Lap.Lap != 1 || laps[2].Lap.Lap != 3 {
			t.Fatalf("laps=%v err=%v", laps, err)
		}
	})

	t.Run("params per track and car", func(t *testing.T) {
		if _, ok, _ := s.GetParams(ctx, "monza", "gt3"); ok {
			t.Fatal("unexpected params")
		}
		p := Params{Track: "monza", Car: "gt3", PitThroughGreen: 22.5, RefuelRate: 2.8, Reserve: 2}
		s.SetParams(ctx, p)
		s.SetParams(ctx, Params{Track: "monza", Car: "hyper"})
		got, ok, err := s.GetParams(ctx, "monza", "gt3")
		if err != nil || !ok || !reflect.DeepEqual(got, p.WithDefaults()) {
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

	t.Run("lap time model settings default and round trip", func(t *testing.T) {
		off := false
		// unset values come back as the defaults
		s.SetParams(ctx, Params{Track: "spa", Car: "gt3"})
		got, ok, err := s.GetParams(ctx, "spa", "gt3")
		if err != nil || !ok || got.FitMinLaps != 5 || got.FitSlowPercent != 8 || got.FitSkipFCY == nil || !*got.FitSkipFCY || got.FitSkipWet == nil || !*got.FitSkipWet {
			t.Fatalf("defaults: %+v ok=%v err=%v", got, ok, err)
		}
		// explicit values, also switched off, are stored and listed
		s.SetParams(ctx, Params{Track: "spa", Car: "gt3", FitMinLaps: 12, FitSlowPercent: 4.5, FitSkipFCY: &off, FitSkipWet: &off})
		got, _, _ = s.GetParams(ctx, "spa", "gt3")
		if got.FitMinLaps != 12 || got.FitSlowPercent != 4.5 || *got.FitSkipFCY || *got.FitSkipWet {
			t.Fatalf("round trip: %+v", got)
		}
		list, _ := s.ListParams(ctx)
		for _, p := range list {
			if p.Track == "spa" && (p.FitMinLaps != 12 || *p.FitSkipFCY) {
				t.Fatalf("list: %+v", p)
			}
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
		s.SetOverride(ctx, 0, OverrideFuel, 40)
		s.SetOverride(ctx, 0, OverrideFuel, 41)
		s.SetOverride(ctx, 0, OverrideFuelPerLap, 3.2)
		got, err := s.GetOverrides(ctx, 0)
		if err != nil || len(got) != 2 || got[OverrideFuel].Value != 41 {
			t.Fatalf("got=%v err=%v", got, err)
		}
		s.DeleteOverride(ctx, 0, OverrideFuel)
		s.DeleteOverride(ctx, 0, "missing") // not an error
		if got, _ := s.GetOverrides(ctx, 0); len(got) != 1 {
			t.Fatalf("got=%v", got)
		}
		s.DeleteOverride(ctx, 0, OverrideFuelPerLap)
	})

	t.Run("overrides are kept per agent", func(t *testing.T) {
		s.SetOverride(ctx, 1, OverrideFuel, 10)
		s.SetOverride(ctx, 2, OverrideFuel, 20)
		s.SetOverride(ctx, 2, OverrideFuelPerLap, 3)
		o1, _ := s.GetOverrides(ctx, 1)
		o2, _ := s.GetOverrides(ctx, 2)
		if len(o1) != 1 || o1[OverrideFuel].Value != 10 || len(o2) != 2 || o2[OverrideFuel].Value != 20 {
			t.Fatalf("agent 1 %+v, agent 2 %+v", o1, o2)
		}
		s.DeleteOverride(ctx, 1, OverrideFuel)
		if o2, _ = s.GetOverrides(ctx, 2); len(o2) != 2 {
			t.Fatalf("deleting the override of agent 1 changed agent 2: %+v", o2)
		}
		s.DeleteOverride(ctx, 2, OverrideFuel)
		s.DeleteOverride(ctx, 2, OverrideFuelPerLap)
	})

	t.Run("team cars: parameters and set-ups follow the car, not the livery", func(t *testing.T) {
		const track = "cars-track"
		// parameters stored under a livery name before there was a team car
		s.SetParams(ctx, Params{Track: track + "-old", Car: "BMW #3 old", TyreChange: 11})
		s.SetParams(ctx, Params{Track: track + "-old", Car: "BMW", TyreChange: 12})

		bmw, err := s.CreateCar(ctx, "BMW")
		if err != nil || bmw.ID == 0 || len(bmw.Names) != 0 {
			t.Fatalf("create %+v err=%v", bmw, err)
		}
		if _, err := s.CreateCar(ctx, "BMW"); err != ErrExists {
			t.Fatalf("duplicate: %v", err)
		}
		porsche, _ := s.CreateCar(ctx, "Porsche")
		for _, n := range []string{"BMW #2 blue", "BMW #1 red"} {
			if err := s.MapCarName(ctx, bmw.ID, n); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.MapCarName(ctx, 987654, "x"); err != ErrNoCar {
			t.Fatalf("unknown car: %v", err)
		}
		cars, _ := s.Cars(ctx)
		if len(cars) != 2 || cars[0].Name != "BMW" || !reflect.DeepEqual(cars[0].Names, []string{"BMW #1 red", "BMW #2 blue"}) ||
			cars[1].Name != "Porsche" || len(cars[1].Names) != 0 {
			t.Fatalf("cars %+v", cars)
		}
		if got, _ := s.CanonicalCar(ctx, "BMW #1 red"); got != "BMW" {
			t.Fatalf("canonical %q", got)
		}
		if got, _ := s.CanonicalCar(ctx, "Ford #9"); got != "Ford #9" {
			t.Fatalf("a name without a team car stays: %q", got)
		}

		// written through one livery, found through another and through the team car
		s.SetParams(ctx, Params{Track: track, Car: "BMW #1 red", TyreChange: 21})
		for _, car := range []string{"BMW #2 blue", "BMW"} {
			got, ok, _ := s.GetParams(ctx, track, car)
			if !ok || got.TyreChange != 21 || got.Car != "BMW" {
				t.Fatalf("params through %q: %+v ok=%v", car, got, ok)
			}
		}
		// the set of the old livery name is still found, but the team car's own set wins
		if got, ok, _ := s.GetParams(ctx, track+"-old", "BMW #1 red"); !ok || got.TyreChange != 12 {
			t.Fatalf("team car set wins: %+v ok=%v", got, ok)
		}
		s.MapCarName(ctx, bmw.ID, "BMW #3 old")
		if got, ok, _ := s.GetParams(ctx, track+"-old", "BMW #3 old"); !ok || got.TyreChange != 12 {
			t.Fatalf("after mapping: %+v ok=%v", got, ok)
		}
		s.SetParams(ctx, Params{Track: track + "-legacy", Car: "Legacy livery", TyreChange: 13})
		cl, _ := s.CreateCar(ctx, "Legacy car")
		s.MapCarName(ctx, cl.ID, "Legacy livery")
		if got, ok, _ := s.GetParams(ctx, track+"-legacy", "Legacy livery"); !ok || got.TyreChange != 13 {
			t.Fatalf("a set stored under the livery name is still found: %+v ok=%v", got, ok)
		}

		// pit set-ups the same way
		s.SavePitSetup(ctx, PitSetup{Car: "BMW #1 red", Name: "Max", Steps: []wire.Step{{Category: "FUEL:", Choice: "max"}}})
		got, _ := s.PitSetups(ctx, "BMW #2 blue")
		if len(got) != 1 || got[0].Name != "Max" || got[0].Car != "BMW" {
			t.Fatalf("set-ups %+v", got)
		}
		s.DeletePitSetup(ctx, "BMW #2 blue", "Max")
		if got, _ := s.PitSetups(ctx, "BMW"); len(got) != 0 {
			t.Fatalf("after delete %+v", got)
		}

		// a name moves to another car; unmapping and deleting a car release it
		s.MapCarName(ctx, porsche.ID, "BMW #2 blue")
		if got, _ := s.CanonicalCar(ctx, "BMW #2 blue"); got != "Porsche" {
			t.Fatalf("moved name: %q", got)
		}
		s.UnmapCarName(ctx, "BMW #2 blue")
		if got, _ := s.CanonicalCar(ctx, "BMW #2 blue"); got != "BMW #2 blue" {
			t.Fatalf("unmapped name: %q", got)
		}
		if err := s.DeleteCar(ctx, bmw.ID); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.CanonicalCar(ctx, "BMW #1 red"); got != "BMW #1 red" {
			t.Fatalf("name of a deleted car: %q", got)
		}
		if err := s.DeleteCar(ctx, bmw.ID); err != ErrNoCar {
			t.Fatalf("delete twice: %v", err)
		}
		s.DeleteCar(ctx, porsche.ID)
		s.DeleteCar(ctx, cl.ID)
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
		s.SaveLap(ctx, "sess", wire.Lap{CarID: 0, VehicleName: "M4", Lap: 1}, 0)
		s.SaveLap(ctx, "sess", wire.Lap{CarID: 1, VehicleName: "other", Lap: 1}, 0)
		s.SaveLap(ctx, "sess", wire.Lap{CarID: 5, VehicleName: "M4", Lap: 1}, 0)                           // a rival with the same vehicle name
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
		ed, xd := 4900.5, 120.0
		l.EntryDist, l.ExitDist = &ed, &xd
		s.SavePitLane(ctx, l) // replaces
		got, ok, err := s.GetPitLane(ctx, "ring")
		if err != nil || !ok || got.Entry == nil || got.Entry[1] != 2 || got.Exit == nil || got.Exit[0] != 9 || len(got.Boxes) != 2 || got.Boxes[1][1] != -6 {
			t.Fatalf("got=%+v ok=%v err=%v", got, ok, err)
		}
		if got.EntryDist == nil || *got.EntryDist != 4900.5 || got.ExitDist == nil || *got.ExitDist != 120 {
			t.Fatalf("lap distances: %+v", got)
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

func TestMemory(t *testing.T) {
	db := NewMemory()
	runSuite(t, db.ForTeam(1))
	runTeamSuite(t, db)
}

// runTeamSuite checks that teams share nothing but track data, and the agent directory.
func runTeamSuite(t *testing.T, db DB) {
	ctx := context.Background()
	def, ok, err := db.TeamByName(ctx, DefaultTeamName)
	if err != nil || !ok {
		t.Fatalf("default team: %v %v", ok, err)
	}
	// Two new teams, so the data the shared suite left in the default team does not matter.
	first, err := db.CreateTeam(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateTeam(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateTeam(ctx, "other"); err != ErrExists {
		t.Fatalf("duplicate team: %v", err)
	}
	a, b := db.ForTeam(first.ID), db.ForTeam(other.ID)
	const sess = "same-session" // both teams race in the same game session, so the id is the same

	t.Run("laps, stops, weather, conditions, wear and progress are per team", func(t *testing.T) {
		a.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 1, LapTime: f(90)}, 0)
		b.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 1, LapTime: f(100)}, 0)
		b.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 2, LapTime: f(101)}, 0)
		la, _ := a.Laps(ctx, sess)
		lb, _ := b.Laps(ctx, sess)
		if len(la) != 1 || *la[0].LapTime != 90 || len(lb) != 2 || *lb[0].LapTime != 100 {
			t.Fatalf("laps a=%+v b=%+v", la, lb)
		}
		if l, _ := a.LapsOfCar(ctx, sess, 0, "v"); len(l) != 1 {
			t.Fatalf("laps of car: %+v", l)
		}

		entry := wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "v"}
		exit := wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "v", FuelAdded: f(40)}
		a.SaveEvent(ctx, sess, 1000, entry)
		b.SaveEvent(ctx, sess, 1000, entry) // same entry time: must not count as a duplicate of team a's stop
		b.SaveEvent(ctx, sess, 1050, exit)
		sa, _ := a.Stops(ctx, sess)
		sb, _ := b.Stops(ctx, sess)
		if len(sa) != 1 || sa[0].ExitET != nil || len(sb) != 1 || sb[0].ExitET == nil {
			t.Fatalf("stops a=%+v b=%+v", sa, sb)
		}
		a.AddStopped(ctx, sess, 0, "v", Standing{From: 1010, To: 1030, Work: 15})
		if sb, _ = b.Stops(ctx, sess); sb[0].Stopped != 0 {
			t.Fatalf("stand-still leaked into the other team: %+v", sb)
		}

		a.SaveWeatherPoint(ctx, sess, WeatherPoint{ET: 5, Ambient: f(20)})
		b.SaveWeatherPoint(ctx, sess, WeatherPoint{ET: 5, Ambient: f(30)})
		wa, _ := a.WeatherPoints(ctx, sess, 10)
		wb, _ := b.WeatherPoints(ctx, sess, 10)
		if len(wa) != 1 || *wa[0].Ambient != 20 || len(wb) != 1 || *wb[0].Ambient != 30 {
			t.Fatalf("weather a=%+v b=%+v", wa, wb)
		}

		a.SaveLapConditions(ctx, LapConditions{SessionID: sess, CarID: 0, VehicleName: "v", Lap: 1})
		if c, _ := b.LapConditions(ctx, sess); len(c) != 0 {
			t.Fatalf("conditions leaked: %+v", c)
		}
		a.SaveLapWear(ctx, sess, 0, "v", wear.LapWear{Lap: 1, Coverage: 1})
		if w, _ := b.LapWears(ctx, sess, 0, "v"); len(w) != 0 {
			t.Fatalf("wear leaked: %+v", w)
		}

		a.SetProgress(ctx, "agent", sess, 9)
		if _, known, _ := b.Progress(ctx, "agent", sess); known {
			t.Fatal("progress leaked")
		}
		if v, known, _ := a.Progress(ctx, "agent", sess); !known || v != 9 {
			t.Fatalf("progress %d %v", v, known)
		}
	})

	t.Run("sessions are per team", func(t *testing.T) {
		now := time.Now()
		a.TouchSession(ctx, SessionInfo{SessionID: sess, Track: "monza", Car: "v", CarID: 0, StartedAt: now, LastSeen: now})
		b.TouchSession(ctx, SessionInfo{SessionID: sess, Track: "spa", Car: "v", CarID: 0, StartedAt: now, LastSeen: now})
		b.TouchSession(ctx, SessionInfo{SessionID: "only-b", Track: "spa", StartedAt: now, LastSeen: now})
		la, _ := a.Sessions(ctx)
		lb, _ := b.Sessions(ctx)
		if len(la) != 1 || la[0].Track != "monza" || la[0].Laps != 1 || la[0].Stops != 1 {
			t.Fatalf("a=%+v", la)
		}
		if len(lb) != 2 {
			t.Fatalf("b=%+v", lb)
		}
		for _, s := range lb {
			if s.SessionID == sess && (s.Track != "spa" || s.Laps != 2) {
				t.Fatalf("b=%+v", lb)
			}
		}
	})

	t.Run("params, stint plan, overrides and pit setups are per team", func(t *testing.T) {
		a.SetParams(ctx, Params{Track: "monza", Car: "v", Reserve: 2})
		b.SetParams(ctx, Params{Track: "monza", Car: "v", Reserve: 5})
		pa, _, _ := a.GetParams(ctx, "monza", "v")
		pb, _, _ := b.GetParams(ctx, "monza", "v")
		if pa.Reserve != 2 || pb.Reserve != 5 {
			t.Fatalf("params a=%+v b=%+v", pa, pb)
		}
		b.SetParams(ctx, Params{Track: "spa", Car: "v"})
		if l, _ := a.ListParams(ctx); len(l) != 1 {
			t.Fatalf("list params a=%+v", l)
		}

		a.SetStintPlan(ctx, []PlanRow{{"x", 3600}})
		b.SetStintPlan(ctx, []PlanRow{{"y", 1800}, {"z", 1800}})
		ra, _ := a.GetStintPlan(ctx)
		rb, _ := b.GetStintPlan(ctx)
		if len(ra) != 1 || ra[0].Driver != "x" || len(rb) != 2 {
			t.Fatalf("plan a=%+v b=%+v", ra, rb)
		}
		a.SetStintPlan(ctx, nil) // replacing the plan of a must not clear b
		if rb, _ = b.GetStintPlan(ctx); len(rb) != 2 {
			t.Fatalf("plan of b after clearing a: %+v", rb)
		}

		a.SetOverride(ctx, 0, OverrideFuel, 10)
		b.SetOverride(ctx, 0, OverrideFuel, 20)
		oa, _ := a.GetOverrides(ctx, 0)
		ob, _ := b.GetOverrides(ctx, 0)
		if oa[OverrideFuel].Value != 10 || ob[OverrideFuel].Value != 20 {
			t.Fatalf("overrides a=%+v b=%+v", oa, ob)
		}
		a.DeleteOverride(ctx, 0, OverrideFuel)
		if ob, _ = b.GetOverrides(ctx, 0); len(ob) != 1 {
			t.Fatalf("override of b after deleting a's: %+v", ob)
		}

		a.SavePitSetup(ctx, PitSetup{Car: "v", Name: "Max", Steps: []wire.Step{{Category: "FUEL:", Choice: "max"}}})
		b.SavePitSetup(ctx, PitSetup{Car: "v", Name: "Max", Steps: []wire.Step{{Category: "FUEL:", Choice: "half"}}})
		sa, _ := a.PitSetups(ctx, "v")
		sb, _ := b.PitSetups(ctx, "v")
		if len(sa) != 1 || sa[0].Steps[0].Choice != "max" || len(sb) != 1 || sb[0].Steps[0].Choice != "half" {
			t.Fatalf("setups a=%+v b=%+v", sa, sb)
		}
		a.DeletePitSetup(ctx, "v", "Max")
		if sb, _ = b.PitSetups(ctx, "v"); len(sb) != 1 {
			t.Fatalf("setup of b after deleting a's: %+v", sb)
		}
	})

	t.Run("track data is shared", func(t *testing.T) {
		a.SaveTrackMap(ctx, trackmap.Map{Track: "shared-ring", Length: 1000, Points: [][2]float64{{1, 2}, {3, 4}}})
		if _, ok, _ := b.GetTrackMap(ctx, "shared-ring"); !ok {
			t.Fatal("outline not visible to the other team")
		}
		b.SaveCorners(ctx, "shared-ring", []Corner{{Name: "T1"}})
		if c, _ := a.GetCorners(ctx, "shared-ring"); len(c) != 1 {
			t.Fatalf("corners %+v", c)
		}
		a.SavePitLane(ctx, pitlane.Lane{Track: "shared-ring"})
		if _, ok, _ := b.GetPitLane(ctx, "shared-ring"); !ok {
			t.Fatal("pit lane not visible to the other team")
		}
	})

	t.Run("members, agents list and audit log", func(t *testing.T) {
		if err := db.SetMember(ctx, Member{Email: "m@example.com", TeamID: first.ID, IsAdmin: true}); err != nil {
			t.Fatal(err)
		}
		if err := db.SetMember(ctx, Member{Email: "m@example.com", TeamID: other.ID}); err != nil { // moves, stays one row
			t.Fatal(err)
		}
		m, ok, err := db.MemberByEmail(ctx, "m@example.com")
		if err != nil || !ok || m.TeamID != other.ID || m.Team != "other" || m.IsAdmin {
			t.Fatalf("member %+v %v %v", m, ok, err)
		}
		if err := db.SetMember(ctx, Member{Email: "x@example.com", TeamID: 99999}); err != ErrNoTeam {
			t.Fatalf("unknown team: %v", err)
		}
		if all, _ := db.Members(ctx); len(all) != 1 {
			t.Fatalf("members %+v", all)
		}
		db.DeleteMember(ctx, "m@example.com")
		if _, ok, _ := db.MemberByEmail(ctx, "m@example.com"); ok {
			t.Fatal("not deleted")
		}
		if _, err := db.CreateAgent(ctx, "x", 99999, HashToken("t-unknown-team")); err != ErrNoTeam {
			t.Fatalf("agent of an unknown team: %v", err)
		}
		if ts, _ := db.Teams(ctx); len(ts) < 3 || ts[0].Name != DefaultTeamName {
			t.Fatalf("teams %+v", ts)
		}
		base := time.Now().Add(-time.Hour)
		db.AddAudit(ctx, AuditEntry{TS: base, Email: "a@example.com", Team: "first", Method: "PUT", Path: "/p1"})
		db.AddAudit(ctx, AuditEntry{TS: base.Add(time.Second), Email: "b@example.com", Team: "other", Method: "DELETE", Path: "/p2"})
		es, _ := db.Audit(ctx, 1)
		if len(es) != 1 || es[0].Path != "/p2" || es[0].Email != "b@example.com" || es[0].Team != "other" || es[0].Method != "DELETE" {
			t.Fatalf("audit %+v", es)
		}
	})

	t.Run("agent tokens", func(t *testing.T) {
		h := HashToken("secret-one")
		ag, err := db.CreateAgent(ctx, "driver", other.ID, h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateAgent(ctx, "again", def.ID, h); err != ErrExists {
			t.Fatalf("duplicate hash: %v", err)
		}
		got, ok, err := db.AgentByTokenHash(ctx, HashToken("secret-one"))
		if err != nil || !ok || got.ID != ag.ID || got.TeamID != other.ID || got.Revoked || got.LastSeen != nil {
			t.Fatalf("lookup %+v %v %v", got, ok, err)
		}
		if _, ok, _ := db.AgentByTokenHash(ctx, HashToken("secret-two")); ok {
			t.Fatal("unknown token found")
		}
		if err := db.TouchAgent(ctx, ag.ID); err != nil {
			t.Fatal(err)
		}
		if err := db.RevokeAgent(ctx, ag.ID); err != nil {
			t.Fatal(err)
		}
		got, _, _ = db.AgentByTokenHash(ctx, h)
		if !got.Revoked || got.LastSeen == nil {
			t.Fatalf("after revoke %+v", got)
		}
		list, _ := db.Agents(ctx)
		found := false
		for _, a := range list {
			found = found || (a.ID == ag.ID && a.Revoked)
		}
		if !found {
			t.Fatalf("agents %+v", list)
		}
	})

	t.Run("team cars are per team", func(t *testing.T) {
		ca, err := a.CreateCar(ctx, "Same name")
		if err != nil {
			t.Fatal(err)
		}
		a.MapCarName(ctx, ca.ID, "livery")
		if got, _ := b.CanonicalCar(ctx, "livery"); got != "livery" {
			t.Fatalf("team b sees the mapping of team a: %q", got)
		}
		// team b has no car yet, so it cannot put a name on the car of team a or delete it
		if err := b.MapCarName(ctx, ca.ID, "other livery"); err != ErrNoCar {
			t.Fatalf("map on the car of another team: %v", err)
		}
		if err := b.DeleteCar(ctx, ca.ID); err != ErrNoCar {
			t.Fatalf("delete the car of another team: %v", err)
		}
		if _, err := b.CreateCar(ctx, "Same name"); err != nil {
			t.Fatalf("another team may use the name: %v", err)
		}

		// names seen in sessions that belong to no team car
		now := time.Now()
		a.TouchSession(ctx, SessionInfo{SessionID: "cars-s1", Track: "x", Car: "livery", StartedAt: now, LastSeen: now})
		a.TouchSession(ctx, SessionInfo{SessionID: "cars-s2", Track: "x", Car: "Ford #9", StartedAt: now, LastSeen: now})
		a.TouchSession(ctx, SessionInfo{SessionID: "cars-s3", Track: "x", Car: "Ford #9", StartedAt: now, LastSeen: now})
		un, _ := a.UnmappedCarNames(ctx)
		has := func(list []string, n string) bool {
			for _, x := range list {
				if x == n {
					return true
				}
			}
			return false
		}
		if !has(un, "Ford #9") || has(un, "livery") {
			t.Fatalf("unmapped %v: want the name without a team car once, not the mapped one", un)
		}
		for i := 1; i < len(un); i++ {
			if un[i] == un[i-1] {
				t.Fatalf("unmapped %v lists a name twice", un)
			}
		}
		if unb, _ := b.UnmappedCarNames(ctx); has(unb, "Ford #9") {
			t.Fatalf("team b sees the sessions of team a: %v", unb)
		}
	})

	t.Run("laps and sessions can be left out of the models", func(t *testing.T) {
		s := db.ForTeam(first.ID)
		const sess = "excl"
		s.TouchSession(ctx, SessionInfo{SessionID: sess, Track: "x", Car: "v", StartedAt: time.Now(), LastSeen: time.Now()})
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 1, LapTime: f(90)}, 0)
		s.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 2, LapTime: f(91)}, 0)
		flags := func() (a, b bool, fa, fb *bool) {
			laps, _ := s.LapsOfCar(ctx, sess, 0, "v")
			if len(laps) != 2 {
				t.Fatalf("laps %+v", laps)
			}
			return laps[0].Excluded, laps[1].Excluded, laps[0].ExcludedFlag, laps[1].ExcludedFlag
		}
		if a, b, _, _ := flags(); a || b {
			t.Fatal("laps start out taken in")
		}
		if err := s.SetSessionExcluded(ctx, sess, true); err != nil {
			t.Fatal(err)
		}
		if a, b, fa, fb := flags(); !a || !b || fa != nil || fb != nil {
			t.Fatalf("session left out: %v %v %v %v", a, b, fa, fb)
		}
		// a lap with a flag of its own keeps it when the session changes
		no, yes := false, true
		if err := s.SetLapExcluded(ctx, sess, 0, "v", 1, &no); err != nil {
			t.Fatal(err)
		}
		if a, b, fa, _ := flags(); a || !b || fa == nil || *fa {
			t.Fatalf("lap taken in inside a left-out session: %v %v %v", a, b, fa)
		}
		s.SetSessionExcluded(ctx, sess, false)
		s.SetLapExcluded(ctx, sess, 0, "v", 2, &yes)
		if a, b, _, fb := flags(); a || !b || fb == nil || !*fb {
			t.Fatalf("lap left out in a taken-in session: %v %v %v", a, b, fb)
		}
		all, _ := s.Laps(ctx, sess)
		if len(all) != 2 || all[1].Excluded != true {
			t.Fatalf("Laps %+v", all)
		}
		// a flag can be removed again
		s.SetLapExcluded(ctx, sess, 0, "v", 1, nil)
		s.SetLapExcluded(ctx, sess, 0, "v", 2, nil)
		if a, b, fa, fb := flags(); a || b || fa != nil || fb != nil {
			t.Fatalf("flags removed: %v %v %v %v", a, b, fa, fb)
		}
		if err := s.SetLapExcluded(ctx, sess, 0, "v", 99, &yes); err != ErrNoLap {
			t.Fatalf("unknown lap: %v", err)
		}
		if err := s.SetSessionExcluded(ctx, "no such session", true); err != ErrNoSession {
			t.Fatalf("unknown session: %v", err)
		}
		s.SetSessionExcluded(ctx, sess, true)
		list, _ := s.Sessions(ctx)
		found := false
		for _, x := range list {
			if x.SessionID == sess {
				found = true
				if !x.Excluded {
					t.Fatalf("summary %+v", x)
				}
			}
		}
		if !found {
			t.Fatal("session missing in the summary")
		}
	})

	t.Run("history laps: same track and team car, earlier sessions, not left out", func(t *testing.T) {
		st := db.ForTeam(first.ID)
		car, _ := st.CreateCar(ctx, "Hist car")
		st.MapCarName(ctx, car.ID, "hist red")
		st.MapCarName(ctx, car.ID, "hist blue")
		t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
		session := func(id, track, vehicle string, hours int) {
			st.TouchSession(ctx, SessionInfo{SessionID: id, Track: track, Car: vehicle, StartedAt: t0.Add(time.Duration(hours) * time.Hour), LastSeen: t0.Add(time.Duration(hours) * time.Hour)})
		}
		lap := func(id, vehicle string, n int, fuel bool) {
			l := wire.Lap{CarID: 0, VehicleName: vehicle, Lap: n, Driver: "d", LapTime: f(90)}
			if fuel {
				l.FuelStart, l.FuelEnd = f(60), f(57)
			}
			st.SaveLap(ctx, id, l, 0)
		}
		session("h-new", "hist-track", "hist blue", 5)
		session("h-old", "hist-track", "hist red", 1)
		session("h-cur", "hist-track", "hist red", 9)
		session("h-elsewhere", "hist-other-track", "hist red", 2)
		session("h-rival", "hist-track", "somebody else", 3)
		lap("h-old", "hist red", 1, true)
		lap("h-old", "hist red", 2, true)
		lap("h-old", "hist red", 3, false) // no fuel figures: a rival or a lap without telemetry
		lap("h-new", "hist blue", 1, true) // another livery of the same team car
		lap("h-cur", "hist red", 1, true)  // the running session is left out
		lap("h-elsewhere", "hist red", 1, true)
		lap("h-rival", "somebody else", 1, true)
		st.SaveLapConditions(ctx, LapConditions{SessionID: "h-old", CarID: 0, VehicleName: "hist red", Lap: 2, Rain: f(0.4), Wetness: f(0.5)})

		got, err := st.HistoryLaps(ctx, "hist-track", "hist red", "h-cur")
		if err != nil {
			t.Fatal(err)
		}
		ids := ""
		for _, h := range got {
			ids += h.SessionID + ":" + strconv.Itoa(h.Lap.Lap) + " "
		}
		if ids != "h-old:1 h-old:2 h-new:1 " {
			t.Fatalf("history %q: want the old session first, laps with fuel figures only, both liveries of the team car", ids)
		}
		if got[1].Rain == nil || *got[1].Rain != 0.4 || got[1].Wetness == nil || got[0].Rain != nil || !got[0].StartedAt.Before(got[2].StartedAt) {
			t.Fatalf("conditions and start time: %+v", got)
		}
		// asked through the other livery it is the same history
		if again, _ := st.HistoryLaps(ctx, "hist-track", "hist blue", "h-cur"); len(again) != 3 {
			t.Fatalf("through the other livery: %d laps", len(again))
		}
		// a name without a team car sees only its own laps
		if solo, _ := st.HistoryLaps(ctx, "hist-track", "somebody else", ""); len(solo) != 1 || solo[0].SessionID != "h-rival" {
			t.Fatalf("unmapped name: %+v", solo)
		}

		// left-out laps and sessions are not part of it; a lap taken in inside a left-out session is
		yes, no := true, false
		st.SetLapExcluded(ctx, "h-old", 0, "hist red", 1, &yes)
		st.SetSessionExcluded(ctx, "h-new", true)
		if got, _ = st.HistoryLaps(ctx, "hist-track", "hist red", "h-cur"); len(got) != 1 || got[0].Lap.Lap != 2 {
			t.Fatalf("after leaving laps out: %+v", got)
		}
		st.SetLapExcluded(ctx, "h-new", 0, "hist blue", 1, &no)
		if got, _ = st.HistoryLaps(ctx, "hist-track", "hist red", "h-cur"); len(got) != 2 {
			t.Fatalf("a lap taken in inside a left-out session: %+v", got)
		}
	})

	t.Run("an agent can be tied to a member and released again", func(t *testing.T) {
		ag, err := db.CreateAgent(ctx, "owned", first.ID, HashToken("owned"))
		if err != nil || ag.Email != "" {
			t.Fatalf("new agent %+v err=%v", ag, err)
		}
		if err := db.SetAgentEmail(ctx, ag.ID, "driver@example.com"); err != nil {
			t.Fatal(err)
		}
		got, ok, _ := db.AgentByTokenHash(ctx, HashToken("owned"))
		if !ok || got.Email != "driver@example.com" {
			t.Fatalf("by token %+v", got)
		}
		list, _ := db.Agents(ctx)
		found := false
		for _, a := range list {
			found = found || (a.ID == ag.ID && a.Email == "driver@example.com")
		}
		if !found {
			t.Fatalf("agents %+v", list)
		}
		if err := db.SetAgentEmail(ctx, ag.ID, ""); err != nil {
			t.Fatal(err)
		}
		if got, _, _ = db.AgentByTokenHash(ctx, HashToken("owned")); got.Email != "" {
			t.Fatalf("after release %+v", got)
		}
		if err := db.SetAgentEmail(ctx, 99999, "x@example.com"); err != ErrNoAgent {
			t.Fatalf("unknown agent: %v", err)
		}
	})

	t.Run("a lap keeps the agent that delivered it first", func(t *testing.T) {
		st := db.ForTeam(first.ID)
		one, err := db.CreateAgent(ctx, "lap-one", first.ID, HashToken("lap-one"))
		if err != nil {
			t.Fatal(err)
		}
		two, err := db.CreateAgent(ctx, "lap-two", first.ID, HashToken("lap-two"))
		if err != nil {
			t.Fatal(err)
		}
		const sess = "lap-agent"
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 1, LapTime: f(90)}, one.ID)
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 1, S1: f(30)}, two.ID) // same lap, other agent
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 2}, 0)                 // shared dev token
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 2}, two.ID)            // a later delivery fills it in
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 3}, 0)
		laps, err := st.Laps(ctx, sess)
		if err != nil || len(laps) != 3 {
			t.Fatalf("laps=%+v err=%v", laps, err)
		}
		if laps[0].AgentID != one.ID || laps[1].AgentID != two.ID || laps[2].AgentID != 0 {
			t.Fatalf("agents %d %d %d, want %d %d 0", laps[0].AgentID, laps[1].AgentID, laps[2].AgentID, one.ID, two.ID)
		}
		if laps[0].S1 == nil || *laps[0].S1 != 30 {
			t.Fatalf("the fields of both deliveries are merged: %+v", laps[0])
		}
		own, err := st.LapsOfCar(ctx, sess, 0, "v")
		if err != nil || len(own) != 3 || own[0].AgentID != one.ID {
			t.Fatalf("laps of car %+v err=%v", own, err)
		}
		// every agent derives the laps of all cars, but only the driven car has fuel figures: that agent is the driver
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 4, LapTime: f(90)}, one.ID)
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 4, LapTime: f(90), FuelStart: f(60), FuelEnd: f(57)}, two.ID)
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, LapTime: f(90), FuelStart: f(60), FuelEnd: f(57)}, two.ID)
		st.SaveLap(ctx, sess, wire.Lap{CarID: 0, VehicleName: "v", Lap: 5, LapTime: f(90)}, one.ID)
		laps4, _ := st.LapsOfCar(ctx, sess, 0, "v")
		if len(laps4) != 5 || laps4[3].AgentID != two.ID || laps4[4].AgentID != two.ID {
			t.Fatalf("the agent with the fuel figures wins: %+v", laps4[3:])
		}
	})
}
