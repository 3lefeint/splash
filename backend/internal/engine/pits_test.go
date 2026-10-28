package engine

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func stop(entry, lane, stopped, fuel float64, tyres, swap bool) store.StopRecord {
	exit := entry + lane
	return store.StopRecord{CarID: 0, VehicleName: "me", EntryET: entry, ExitET: &exit, FuelAdded: &fuel,
		Stopped: stopped, TyresChanged: tyres, DriverSwap: swap}
}

func close1(a *float64, b float64) bool { return a != nil && math.Abs(*a-b) < 1e-9 }

// lane500 judges stops on a track of 5000 m with a pit lane stretch of 500 m and a pace of 100 s a lap: 10 s for the
// stretch at race pace.
func lane500() PitContext {
	d := 500.0
	return PitContext{LaneStretch: &d, TrackLength: 5000, LapTime: 100}
}

func TestPitStatsRowsAndThrough(t *testing.T) {
	st := pitStats([]store.StopRecord{stop(100, 70, 40, 60, false, false),
		{CarID: 5, VehicleName: "other", EntryET: 1}}, 0, "me", PitContext{})
	if st == nil || len(st.Rows) != 1 {
		t.Fatalf("%+v", st)
	}
	r := st.Rows[0]
	if !close1(r.Lane, 70) || !close1(r.Stopped, 40) || !close1(r.Through, 30) {
		t.Fatalf("%+v", r)
	}
}

func TestPitSuggestionsFromDifferentKindsOfStops(t *testing.T) {
	stops := []store.StopRecord{
		stop(100, 70, 30, 60, false, false), // refuel only: 2 l/s
		stop(500, 70, 30, 90, false, false), // refuel only: 3 l/s
		stop(900, 62, 22, 0, true, false),   // tyres only: the time is known, it is not suggested
		stop(1300, 62, 20, 0, true, false),
		stop(1700, 68, 18, 0, false, true),  // driver swap only: 18 s
		stop(2100, 70, 30, 60, true, false), // fuel 60 l (30 s at 2.5 l/s... see below) and tyres 21 s together
	}
	st := pitStats(stops, 0, "me", lane500())
	s := st.Suggest
	// the gross times are 40, 40, 40, 42, 50 and 40 s; 10 s of them is racing past, the median of the net is 30 s
	if s == nil || !close1(s.PitThrough, 30) || !close1(s.RefuelRate, 2.5) || !close1(s.DriverSwap, 18) {
		t.Fatalf("%+v", s)
	}
}

func TestThereIsNoParallelServiceSuggestionAnyMore(t *testing.T) {
	// a stop with fuel and new tyres together: the service runs in sequence, nothing is suggested about it
	stops := []store.StopRecord{stop(100, 70, 30, 60, false, false), stop(500, 76, 36, 60, true, false)}
	s := pitStats(stops, 0, "me", PitContext{}).Suggest
	if s == nil || !close1(s.RefuelRate, 2) {
		t.Fatalf("%+v", s)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "parallel") {
		t.Fatalf("%s", b)
	}
}

func TestStopsWithoutStandingTimeGiveNoSuggestion(t *testing.T) {
	st := pitStats([]store.StopRecord{stop(100, 70, 0, 60, false, false)}, 0, "me", PitContext{})
	if st == nil || st.Suggest != nil || st.Rows[0].Stopped != nil || !close1(st.Rows[0].Lane, 70) {
		t.Fatalf("%+v", st)
	}
	if pitStats(nil, 0, "me", PitContext{}) != nil {
		t.Fatal("expected nil")
	}
}

func TestMedian(t *testing.T) {
	if m := median([]float64{3, 1, 2}); !close1(m, 2) {
		t.Fatalf("%v", m)
	}
	if m := median([]float64{1, 2, 3, 10}); !close1(m, 2.5) {
		t.Fatalf("%v", m)
	}
	if median(nil) != nil {
		t.Fatal("expected nil")
	}
}

func TestMeasuredFuelFlowIsUsedEvenWithTyresAndWaiting(t *testing.T) {
	s := stop(100, 80, 46, 72, true, false)
	s.Work, s.FuelTime = 20, 18 // 72 l in 18 s; the driver waited until 46 s
	st := pitStats([]store.StopRecord{s}, 0, "me", PitContext{})
	if st == nil || st.Suggest == nil || !close1(st.Suggest.RefuelRate, 4) || !close1(st.Rows[0].Work, 20) {
		t.Fatalf("%+v", st)
	}
}

func TestTheTyreChangeIsNotSuggestedAnyMore(t *testing.T) {
	stops := []store.StopRecord{stop(900, 62, 22, 0, true, false), stop(1300, 62, 20, 0, true, false)}
	if st := pitStats(stops, 0, "me", lane500()); st.Suggest == nil || st.Suggest.RefuelRate != nil {
		t.Fatalf("%+v", st.Suggest)
	}
	b, _ := json.Marshal(PitSuggest{})
	if strings.Contains(string(b), "tyre_change") {
		t.Fatalf("%s", b)
	}
}

func TestTheLaneLossIsTheNetTimeAgainstRacingPast(t *testing.T) {
	st := pitStats([]store.StopRecord{stop(100, 70, 40, 60, false, false)}, 0, "me", lane500())
	r := st.Rows[0]
	if !close1(r.Through, 30) || !close1(r.RacePart, 10) || !close1(r.Net, 20) || !close1(st.Suggest.PitThrough, 20) {
		t.Fatalf("%+v %+v", r, st.Suggest)
	}
	// the stretch of 250 m on a 2 minute lap of 5 km is 6 s
	d := 250.0
	ctx := PitContext{LaneStretch: &d, TrackLength: 5000, LapTime: 120}
	if r := pitStats([]store.StopRecord{stop(100, 70, 40, 60, false, false)}, 0, "me", ctx).Rows[0]; !close1(r.RacePart, 6) || !close1(r.Net, 24) {
		t.Fatalf("%+v", r)
	}
}

func TestTheLaneLossNeedsTheLengthOfTheStretchAndThePace(t *testing.T) {
	d := 500.0
	stop1 := []store.StopRecord{stop(100, 70, 40, 60, false, false)}
	for name, ctx := range map[string]PitContext{
		"no stretch":      {TrackLength: 5000, LapTime: 100},
		"no track length": {LaneStretch: &d, LapTime: 100},
		"no pace":         {LaneStretch: &d, TrackLength: 5000},
	} {
		st := pitStats(stop1, 0, "me", ctx)
		r := st.Rows[0]
		if r.RacePart != nil || r.Net != nil || !close1(r.Through, 30) {
			t.Errorf("%s: the gross time stays, nothing else is made up: %+v", name, r)
		}
		if st.Suggest != nil && st.Suggest.PitThrough != nil {
			t.Errorf("%s: no suggestion without the distance: %+v", name, st.Suggest)
		}
	}
}

func TestANegativeOrZeroNetTimeGivesNoSuggestion(t *testing.T) {
	d := 5000.0 // a stretch as long as a lap: 100 s at race pace, longer than the 30 s lost
	ctx := PitContext{LaneStretch: &d, TrackLength: 5000, LapTime: 100}
	st := pitStats([]store.StopRecord{stop(100, 70, 40, 60, false, false)}, 0, "me", ctx)
	if !close1(st.Rows[0].Net, -70) {
		t.Fatalf("the row shows the number: %+v", st.Rows[0])
	}
	if st.Suggest != nil && st.Suggest.PitThrough != nil {
		t.Fatalf("%+v", st.Suggest)
	}
}

// fakeLane is a pit lane source that knows one lane.
type fakeLane struct{ l *pitlane.Lane }

func (f fakeLane) Lane(string) *pitlane.Lane          { return f.l }
func (fakeLane) StoppedAt(string, int) *pitlane.Point { return nil }

func TestTheEngineMeasuresTheStretchFromTheLearnedLane(t *testing.T) {
	// the start/finish line lies inside the pit lane: entry at 4900 m, exit at 120 m of a 5000 m track = 220 m
	r := newRig()
	entry, exit := 4900.0, 120.0
	r.eng.Pits = fakeLane{&pitlane.Lane{Track: "monza", EntryDist: &entry, ExitDist: &exit}}
	r.setLive(100, 50, 3, vehicle(0, "me", "GT3", 1, 0))
	r.lv.UpdateScoring(sess, 101, wire.Scoring{Track: "monza", SessionType: "race", EndET: 10000, TrackLength: 5000,
		Vehicles: []wire.ScoringVehicle{vehicle(0, "me", "GT3", 1, 0)}})
	r.laps(5) // 100 s laps
	exitET := 200.0
	ctx := context.Background()
	r.st.SaveEvent(ctx, sess, 100, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "me"})
	r.st.SaveEvent(ctx, sess, exitET, wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "me"})
	r.st.AddStopped(ctx, sess, 0, "me", store.Standing{From: 130, To: 170})
	st, err := r.eng.Compute(context.Background())
	if err != nil || st.Pits == nil || len(st.Pits.Rows) != 1 {
		t.Fatalf("%+v %v", st.Pits, err)
	}
	if got := st.Pits.Rows[0]; !close1(got.RacePart, 220*100/5000.0) {
		t.Fatalf("race part for 220 m at 100 s a lap of 5000 m is 4.4 s: %+v", got)
	}
}

func TestAStopSaysWhyThereIsNoNetValue(t *testing.T) {
	d := 500.0
	one := []store.StopRecord{stop(100, 70, 40, 60, false, false)}
	for name, c := range map[string]struct {
		ctx  PitContext
		want string
	}{
		"nothing learned":   {PitContext{TrackLength: 5000, LapTime: 100}, "lane_unknown"},
		"no entry distance": {PitContext{TrackLength: 5000, LapTime: 100, StretchMissing: "entry_dist"}, "entry_dist"},
		"no exit distance":  {PitContext{TrackLength: 5000, LapTime: 100, StretchMissing: "exit_dist"}, "exit_dist"},
		"bad stretch":       {PitContext{TrackLength: 5000, LapTime: 100, StretchMissing: "stretch"}, "stretch"},
		"no track length":   {PitContext{LaneStretch: &d, LapTime: 100}, "track_length"},
		"no lap time":       {PitContext{LaneStretch: &d, TrackLength: 5000}, "lap_time"},
	} {
		if got := pitStats(one, 0, "me", c.ctx).Rows[0].NetMissing; got != c.want {
			t.Errorf("%s: got %q want %q", name, got, c.want)
		}
	}
	// with everything known there is no reason; without a standing time or before the exit there is another one
	if r := pitStats(one, 0, "me", lane500()).Rows[0]; r.NetMissing != "" || r.Net == nil {
		t.Errorf("%+v", r)
	}
	if r := pitStats([]store.StopRecord{stop(100, 70, 0, 60, false, false)}, 0, "me", lane500()).Rows[0]; r.NetMissing != "stand_time" {
		t.Errorf("no standing time: %+v", r)
	}
	open := store.StopRecord{CarID: 0, VehicleName: "me", EntryET: 100}
	if r := pitStats([]store.StopRecord{open}, 0, "me", lane500()).Rows[0]; r.NetMissing != "in_progress" {
		t.Errorf("open stop: %+v", r)
	}
}

func TestTheEngineNamesTheMissingEntryDistance(t *testing.T) {
	r := newRig()
	exit := 319.0
	r.eng.Pits = fakeLane{&pitlane.Lane{Track: "monza", ExitDist: &exit}} // the row as it is on staging: no entry distance
	r.setLive(100, 50, 3, vehicle(0, "me", "GT3", 1, 0))
	r.lv.UpdateScoring(sess, 101, wire.Scoring{Track: "monza", SessionType: "race", EndET: 10000, TrackLength: 5000,
		Vehicles: []wire.ScoringVehicle{vehicle(0, "me", "GT3", 1, 0)}})
	r.laps(5)
	ctx := context.Background()
	r.st.SaveEvent(ctx, sess, 100, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "me"})
	r.st.SaveEvent(ctx, sess, 200, wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "me"})
	r.st.AddStopped(ctx, sess, 0, "me", store.Standing{From: 130, To: 170})
	st, err := r.eng.Compute(ctx)
	if err != nil || st.Pits == nil || len(st.Pits.Rows) != 1 || st.Pits.Rows[0].NetMissing != "entry_dist" {
		t.Fatalf("%+v %v", st.Pits, err)
	}
}
