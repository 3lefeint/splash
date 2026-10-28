package engine

import (
	"math"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
)

func stop(entry, lane, stopped, fuel float64, tyres, swap bool) store.StopRecord {
	exit := entry + lane
	return store.StopRecord{CarID: 0, VehicleName: "me", EntryET: entry, ExitET: &exit, FuelAdded: &fuel,
		Stopped: stopped, TyresChanged: tyres, DriverSwap: swap}
}

func close1(a *float64, b float64) bool { return a != nil && math.Abs(*a-b) < 1e-9 }

func TestPitStatsRowsAndThrough(t *testing.T) {
	st := pitStats([]store.StopRecord{stop(100, 70, 40, 60, false, false),
		{CarID: 5, VehicleName: "other", EntryET: 1}}, 0, "me")
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
		stop(900, 62, 22, 0, true, false),   // tyres only: 22 s
		stop(1300, 62, 20, 0, true, false),  // tyres only: 20 s
		stop(1700, 68, 18, 0, false, true),  // driver swap only: 18 s
		stop(2100, 70, 30, 60, true, false), // fuel 60 l (30 s at 2.5 l/s... see below) and tyres 21 s together
	}
	st := pitStats(stops, 0, "me")
	s := st.Suggest
	if s == nil || !close1(s.PitThrough, 40) || !close1(s.RefuelRate, 2.5) || !close1(s.TyreChange, 21) || !close1(s.DriverSwap, 18) {
		t.Fatalf("%+v", s)
	}
	// 60 l at 2.5 l/s = 24 s; the stop stood 30 s: serial would be 45 s, parallel 24 s
	if s.Parallel == nil || !*s.Parallel {
		t.Fatalf("parallel=%v", s.Parallel)
	}
}

func TestSerialServiceIsRecognised(t *testing.T) {
	stops := []store.StopRecord{
		stop(100, 70, 30, 60, false, false), // 2 l/s
		stop(500, 62, 20, 0, true, false),   // 20 s
		stop(900, 80, 50, 60, true, false),  // 30 + 20
	}
	if s := pitStats(stops, 0, "me").Suggest; s == nil || s.Parallel == nil || *s.Parallel {
		t.Fatalf("%+v", s)
	}
}

func TestStopsWithoutStandingTimeGiveNoSuggestion(t *testing.T) {
	st := pitStats([]store.StopRecord{stop(100, 70, 0, 60, false, false)}, 0, "me")
	if st == nil || st.Suggest != nil || st.Rows[0].Stopped != nil || !close1(st.Rows[0].Lane, 70) {
		t.Fatalf("%+v", st)
	}
	if pitStats(nil, 0, "me") != nil {
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
	st := pitStats([]store.StopRecord{s}, 0, "me")
	if st == nil || st.Suggest == nil || !close1(st.Suggest.RefuelRate, 4) || !close1(st.Rows[0].Work, 20) {
		t.Fatalf("%+v", st)
	}
}
