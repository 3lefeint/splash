package ingest

import (
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

func veh(id, state int) []wire.ScoringVehicle {
	return []wire.ScoringVehicle{{CarID: id, VehicleName: "car", PitState: state}}
}

func TestStationaryReportsTheStandStillWhenItEnds(t *testing.T) {
	var s stationary
	var got []stood
	for _, step := range []struct {
		et    float64
		state int
	}{{10, 0}, {10.5, 2}, {11, 3}, {11.5, 3}, {40, 3}, {40.5, 4}, {41, 0}} {
		got = append(got, s.observe("s", step.et, veh(1, step.state))...)
	}
	if len(got) != 1 || got[0].From != 11 || got[0].To != 40.5 || got[0].CarID != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestStationaryIgnoresRepeatedAndOlderSamples(t *testing.T) {
	var s stationary
	s.observe("s", 10, veh(1, 3))
	s.observe("s", 10, veh(1, 0)) // the same sample from the second agent, possibly stale
	s.observe("s", 9, veh(1, 0))
	if got := s.observe("s", 12, veh(1, 0)); len(got) != 1 || got[0].From != 10 || got[0].To != 12 {
		t.Fatalf("%+v", got)
	}
}

func TestStationaryStartsOverInANewSession(t *testing.T) {
	var s stationary
	s.observe("a", 10, veh(1, 3))
	if got := s.observe("b", 11, veh(1, 0)); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestStationaryMeasuresTheWorkInTheBox(t *testing.T) {
	var s stationary
	tel := func(et, fuel, wear float64) {
		s.telemetry("s", et, wire.Telemetry{CarID: 1, VehicleName: "car", Fuel: fuel, Wear: []float64{wear, wear, wear, wear}})
	}
	s.observe("s", 10, veh(1, 2))
	s.observe("s", 11, veh(1, 3))
	tel(11.2, 20, 0.7)
	tel(12, 20, 1.0) // new tyres at 12
	tel(13, 20, 1.0)
	tel(14, 24, 1.0) // fuel flows from 13 to 18
	tel(16, 36, 1.0)
	tel(18, 50, 1.0)
	tel(19, 50, 1.0)
	got := s.observe("s", 30, veh(1, 0)) // the driver waited until 30
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	g := got[0]
	if g.From != 11 || g.To != 30 || g.Work != 7 || g.FuelTime != 5 {
		t.Fatalf("%+v", g)
	}
}

func TestStationaryWithoutWorkReportsOnlyTheStandingTime(t *testing.T) {
	var s stationary
	s.observe("s", 10, veh(1, 3))
	got := s.observe("s", 20, veh(1, 0))
	if len(got) != 1 || got[0].Work != 0 || got[0].FuelTime != 0 || got[0].To-got[0].From != 10 {
		t.Fatalf("%+v", got)
	}
}
