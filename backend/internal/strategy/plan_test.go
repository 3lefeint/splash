package strategy

import "testing"

func TestLapsToStop(t *testing.T) {
	cases := []struct {
		fuel, reserve, fpl float64
		want               int
	}{
		{60, 3, 3, 19},   // 57 / 3 exactly
		{60, 3, 3.1, 18}, // 18.39
		{2, 3, 3, 0},     // below reserve
		{3, 3, 3, 0},
	}
	for _, c := range cases {
		got, err := LapsToStop(c.fuel, c.reserve, c.fpl)
		if err != nil || got != c.want {
			t.Errorf("LapsToStop(%v,%v,%v)=%d,%v want %d", c.fuel, c.reserve, c.fpl, got, err, c.want)
		}
	}
	if _, err := LapsToStop(60, 3, 0); err != ErrInvalidInput {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestLapsLeft(t *testing.T) {
	cases := []struct {
		timeLeft, lapTime float64
		want              int
	}{
		{1000, 100, 11}, // 10 + 1
		{1001, 100, 12}, // 11 + 1
		{0, 100, 1},     // only the finishing lap
		{-5, 100, 1},
	}
	for _, c := range cases {
		got, err := LapsLeft(c.timeLeft, c.lapTime)
		if err != nil || got != c.want {
			t.Errorf("LapsLeft(%v,%v)=%d,%v want %d", c.timeLeft, c.lapTime, got, err, c.want)
		}
	}
	if _, err := LapsLeft(100, 0); err != ErrInvalidInput {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestStopsRemaining(t *testing.T) {
	cases := []struct {
		lapsLeft        int
		fpl, fuel, tank float64
		want            int
	}{
		{20, 3, 60, 90, 0}, // 60 needed, 60 on board
		{21, 3, 60, 90, 1}, // 63 needed
		{50, 3, 30, 90, 2}, // 120 missing -> 1.33 -> 2
		{50, 3, 60, 90, 1}, // 90 missing, exactly one tank
		{10, 3, 60, 90, 0}, // surplus fuel
	}
	for _, c := range cases {
		got, err := StopsRemaining(c.lapsLeft, c.fpl, c.fuel, c.tank)
		if err != nil || got != c.want {
			t.Errorf("StopsRemaining(%d,%v,%v,%v)=%d,%v want %d", c.lapsLeft, c.fpl, c.fuel, c.tank, got, err, c.want)
		}
	}
	if _, err := StopsRemaining(10, 3, 60, 0); err != ErrInvalidInput {
		t.Errorf("want ErrInvalidInput, got %v", err)
	}
}

func TestSaveTarget(t *testing.T) {
	// 2 stops, 30 l on board, 90 l tank, 50 laps: one stop less means
	// (30 + 1*90) / 50 = 2.4 l per lap.
	got, ok := SaveTarget(30, 90, 2, 50)
	if !ok || !near(got, 2.4) {
		t.Errorf("got %v ok=%v, want 2.4", got, ok)
	}
	if _, ok := SaveTarget(60, 90, 0, 20); ok {
		t.Error("no stop left: nothing to save")
	}
	if _, ok := SaveTarget(60, 90, 1, 0); ok {
		t.Error("no laps left: no target")
	}
}

// A full race fed with synthetic laps: the model output drives the plan.
func TestPlanFromSyntheticLaps(t *testing.T) {
	c := NewConsumption(0.3)
	fuel := 90.0
	for i := 1; i <= 10; i++ {
		c.Add(lap(i, "a", 100, fuel, fuel-3))
		fuel -= 3
	}
	fpl, ok := c.FuelPerLap("a")
	if !ok || !near(fpl, 3) {
		t.Fatalf("fpl=%v ok=%v", fpl, ok)
	}
	toStop, _ := LapsToStop(fuel, 3, fpl) // 60 on board
	left, _ := LapsLeft(5000, 100)        // 50 + 1
	stops, _ := StopsRemaining(left, fpl, fuel, 90)
	target, can := SaveTarget(fuel, 90, stops, left)
	if toStop != 19 || left != 51 || stops != 2 || !can || !near(target, 150.0/51) {
		t.Fatalf("toStop=%d left=%d stops=%d target=%v can=%v", toStop, left, stops, target, can)
	}
}
