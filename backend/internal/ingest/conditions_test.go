package ingest

import (
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/wire"
)

func tel(lap int, surface float64, water float64) wire.Telemetry {
	row := []float64{surface - 1, surface, surface + 1}
	return wire.Telemetry{CarID: 0, VehicleName: "v", Lap: lap,
		TyreSurfaceTemp: [][]float64{row, row, row, row}, TyrePressure: []float64{150, 150, 160, 160},
		EngineWaterTemp: &water, CompoundFront: "Medium", CompoundRear: "Soft"}
}

func TestConditionsAreAveragedOverTheLapAndEmittedWhenItEnds(t *testing.T) {
	var l conditionLog
	ctl, amb := 1, 20.0
	sc := &wire.Scoring{Weather: &wire.Weather{AmbientTemp: &amb}, Vehicles: []wire.ScoringVehicle{{CarID: 0, VehicleName: "v", Control: &ctl}}}
	for i, temp := range []float64{80, 90, 100} {
		if c := l.observe("s", float64(10+i), tel(4, temp, 90+float64(i)), sc); c != nil {
			t.Fatalf("emitted during the lap: %+v", c)
		}
	}
	c := l.observe("s", 20, tel(5, 50, 50), sc)
	if c == nil || c.Lap != 5 || c.ET != 20 || c.Surface[0] != 90 || c.Pressure[3] != 160 || *c.Water != 91 ||
		c.CompoundFront != "Medium" || c.CompoundRear != "Soft" || *c.Control != 1 || *c.Ambient != 20 {
		t.Fatalf("%+v", c)
	}
	// the sample that started lap 5 belongs to lap 5, not to the record above
	for i := 0; i < 3; i++ {
		l.observe("s", 21+float64(i), tel(5, 50, 50), sc)
	}
	if c := l.observe("s", 30, tel(6, 0, 0), sc); c == nil || c.Surface[0] != 50 {
		t.Fatalf("%+v", c)
	}
}

func TestShortOrRestartedLapsGiveNoRecord(t *testing.T) {
	var l conditionLog
	l.observe("s", 1, tel(2, 80, 90), nil)
	if c := l.observe("s", 2, tel(3, 80, 90), nil); c != nil {
		t.Fatalf("a lap with one sample: %+v", c)
	}
	for i := 0; i < 4; i++ {
		l.observe("s", 3+float64(i), tel(3, 80, 90), nil)
	}
	if c := l.observe("s", 10, tel(0, 80, 90), nil); c != nil {
		t.Fatalf("a restart gives no record: %+v", c)
	}
}

func TestSessionIsWrittenOnChangeAndThenEveryFewSeconds(t *testing.T) {
	var s sessionLog
	t0 := time.Now()
	if s.update("a", t0, 1, "ring", "race", "", -1) == nil {
		t.Fatal("first sight must be written")
	}
	if s.update("a", t0.Add(time.Second), 2, "ring", "race", "", -1) != nil {
		t.Fatal("written again without a change")
	}
	if i := s.update("a", t0.Add(2*time.Second), 3, "", "", "M4", 7); i == nil || i.Car != "M4" || i.CarID != 7 || i.Track != "ring" {
		t.Fatalf("new car not written: %+v", i)
	}
	if i := s.update("a", t0.Add(30*time.Second), 40, "", "", "", -1); i == nil || i.LastET != 40 {
		t.Fatalf("sign of life not written: %+v", i)
	}
	if i := s.update("b", t0.Add(31*time.Second), 1, "x", "", "", -1); i == nil || i.SessionID != "b" || i.Track != "x" || i.Car != "" {
		t.Fatalf("new session: %+v", i)
	}
}
