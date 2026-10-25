package strategy

import (
	"math"
	"testing"
)

func lap(n int, driver string, time, start, end float64) Lap {
	return Lap{Number: n, Driver: driver, Time: time, FuelStart: start, FuelEnd: end}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestConsumptionEWMA(t *testing.T) {
	c := NewConsumption(0.3)
	if _, ok := c.FuelPerLap("a"); ok {
		t.Fatal("expected no value before the first lap")
	}
	c.Add(lap(1, "a", 100, 90, 87))   // 3.0
	c.Add(lap(2, "a", 100, 87, 83.5)) // 3.5
	got, ok := c.FuelPerLap("a")
	if !ok || !near(got, 0.3*3.5+0.7*3.0) {
		t.Fatalf("got %v ok=%v, want 3.15", got, ok)
	}
}

func TestConsumptionDriversAreSeparate(t *testing.T) {
	c := NewConsumption(0.3)
	c.Add(lap(1, "a", 100, 90, 87))
	c.Add(lap(2, "b", 100, 90, 86))
	a, _ := c.FuelPerLap("a")
	b, _ := c.FuelPerLap("b")
	if !near(a, 3) || !near(b, 4) {
		t.Fatalf("a=%v b=%v", a, b)
	}
}

func TestConsumptionInvalidLaps(t *testing.T) {
	cases := map[string]Lap{
		"in lap":      {Driver: "a", Time: 100, FuelStart: 90, FuelEnd: 87, InLap: true},
		"out lap":     {Driver: "a", Time: 100, FuelStart: 90, FuelEnd: 87, OutLap: true},
		"fcy":         {Driver: "a", Time: 100, FuelStart: 90, FuelEnd: 87, FCY: true},
		"driver swap": {Driver: "a", Time: 100, FuelStart: 90, FuelEnd: 87, DriverSwap: true},
		"refuelled":   {Driver: "a", Time: 100, FuelStart: 10, FuelEnd: 80},
		"no time":     {Driver: "a", FuelStart: 90, FuelEnd: 87},
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewConsumption(0.3)
			if c.Add(l) {
				t.Fatal("lap must be rejected")
			}
			if _, ok := c.FuelPerLap("a"); ok {
				t.Fatal("rejected lap must not create a value")
			}
		})
	}
}

func TestConsumptionSlowLapRejected(t *testing.T) {
	c := NewConsumption(0.3)
	for i := 1; i <= 5; i++ {
		c.Add(lap(i, "a", 100, 90, 87))
	}
	if c.Add(lap(6, "a", 107.5, 90, 80)) { // > 107 % of median 100
		t.Fatal("slow lap must be rejected")
	}
	if !c.Add(lap(7, "a", 107, 90, 87)) { // exactly at the limit stays valid
		t.Fatal("lap at 107 % must be accepted")
	}
	if got, _ := c.FuelPerLap("a"); !near(got, 3) {
		t.Fatalf("slow lap leaked into the model: %v", got)
	}
}

func TestConsumptionPaceWindowIsLastTen(t *testing.T) {
	c := NewConsumption(0.3)
	// Ten slow valid laps push the early fast laps out of the window, so the
	// median follows the new pace and a 120 s lap becomes acceptable.
	for i := 1; i <= 3; i++ {
		c.Add(lap(i, "a", 100, 90, 87))
	}
	for i := 4; i <= 13; i++ {
		c.Add(lap(i, "a", 115, 90, 87))
	}
	if !c.Add(lap(14, "a", 120, 90, 87)) {
		t.Fatal("window must follow the recent pace")
	}
}

func TestDefaultAlphaFallback(t *testing.T) {
	for _, a := range []float64{0, -1, 1.5} {
		if NewConsumption(a).alpha != DefaultAlpha {
			t.Fatalf("alpha %v must fall back", a)
		}
	}
}
