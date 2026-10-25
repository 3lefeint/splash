package segments

import (
	"math"
	"testing"
)

const length = 2000.0

// drive feeds laps of a car driving at the given speed profile: speed(d) in m/s. Samples every dt seconds.
func drive(r *Recorder, key string, laps int, dt float64, speed func(d float64) float64, fuelRate float64, startET float64) float64 {
	et, _ := driveFrom(r, key, laps, dt, speed, fuelRate, startET, 100)
	return et
}

// driveFrom drives for the given number of laps starting at distance d0 and returns the end time and distance.
func driveFrom(r *Recorder, key string, laps int, dt float64, speed func(d float64) float64, fuelRate, startET, d0 float64) (float64, float64) {
	et, d, fuel := startET, d0, 80.0
	end := float64(laps) * length
	total := 0.0
	for total < end {
		r.Observe("s", key, length, Sample{Dist: d, ET: et, Fuel: fuel, Brake: -1})
		step := speed(d) * dt
		d += step
		total += step
		fuel -= fuelRate * step
		et += dt
		if d >= length {
			d -= length
		}
	}
	return et, d
}

func constant(v float64) func(float64) float64 { return func(float64) float64 { return v } }

func TestCompleteLapsGiveSectionTimesThatAddUpToTheLapTime(t *testing.T) {
	var r Recorder
	drive(&r, "a", 5, 0.5, constant(50), 0.001, 0)
	laps := r.Laps("a")
	if len(laps) != 4 { // five laps from mid-lap: the first partial lap and the last unfinished one do not count
		t.Fatalf("laps: %d", len(laps))
	}
	for _, l := range laps {
		if math.Abs(l.Total-40) > 0.01 { // 2000 m at 50 m/s
			t.Fatalf("total %v", l.Total)
		}
		for i, v := range l.Time {
			if math.Abs(v-0.8) > 0.01 {
				t.Fatalf("section %d: %v", i, v)
			}
		}
		if l.Fuel == nil || math.Abs((*l.Fuel)[10]-0.001*length/N) > 1e-6 {
			t.Fatalf("fuel %+v", l.Fuel)
		}
	}
}

func TestAFasterSectionShowsInTheDifferenceBetweenTwoCars(t *testing.T) {
	var r Recorder
	slowFirstHalf := func(d float64) float64 {
		if d < 1000 {
			return 40
		}
		return 50
	}
	drive(&r, "slow", 5, 0.5, slowFirstHalf, 0, 0)
	drive(&r, "fast", 5, 0.5, constant(50), 0, 0)
	diff := Diff(MeanTime(r.Laps("slow"), 3), MeanTime(r.Laps("fast"), 3))
	if diff[5] == nil || math.Abs(*diff[5]-(40.0/40-40.0/50)) > 0.03 { // 40 m section: 1.0 s against 0.8 s
		t.Fatalf("slow half: %v", diff[5])
	}
	if diff[40] == nil || math.Abs(*diff[40]) > 0.03 {
		t.Fatalf("same speed: %v", diff[40])
	}
	sp := Speeds(MeanTime(r.Laps("slow"), 3), length)
	if sp[5] == nil || math.Abs(*sp[5]-40) > 1 || math.Abs(*sp[40]-50) > 1 {
		t.Fatalf("speeds %v %v", *sp[5], *sp[40])
	}
}

func TestLapsWithAPitVisitAGapOrASpinAreDropped(t *testing.T) {
	var r Recorder
	et := 0.0
	step := func(d float64, pit bool) {
		r.Observe("s", "c", length, Sample{Dist: d, ET: et, InPits: pit, Fuel: -1, Brake: -1})
		et += 0.5
	}
	lap := func(pitAt, jumpAt float64, spin bool) {
		for d := 0.0; d < length; d += 25 {
			if jumpAt > 0 && d >= jumpAt && d < jumpAt+300 {
				continue // 12 samples missing
			}
			step(d, pitAt > 0 && d >= pitAt && d < pitAt+50)
			if spin && d == 1000 {
				step(900, false)
			}
		}
	}
	lap(0, 0, false) // starts the first lap
	lap(0, 0, false) // clean
	lap(500, 0, false)
	lap(0, 800, false)
	lap(0, 0, true)
	step(0, false)
	if n := len(r.Laps("c")); n != 1 {
		t.Fatalf("valid laps: %d", n)
	}
}

func TestBestLapIsKeptWhileOldLapsFallOut(t *testing.T) {
	var r Recorder
	et, d := driveFrom(&r, "b", 3, 0.5, constant(45), 0, 0, 100)
	et, d = driveFrom(&r, "b", 3, 0.5, constant(50), 0, et, d)
	best := r.Best("b")
	if best == nil {
		t.Fatal("no best lap")
	}
	if math.Abs(best.Total-40) > 0.01 {
		t.Fatalf("best %v, want the 50 m/s laps", best.Total)
	}
	for i := 0; i < 8; i++ {
		et, d = driveFrom(&r, "b", 2, 0.5, constant(48), 0, et, d)
	}
	if n := len(r.Laps("b")); n != keepLaps {
		t.Fatalf("kept %d", n)
	}
}

func TestBrakingIsRecordedPerSection(t *testing.T) {
	var r Recorder
	et := 0.0
	for lap := 0; lap < 3; lap++ {
		for d := 0.0; d < length; d += 20 {
			brake := 0.0
			if d >= 400 && d < 480 {
				brake = 0.9
			}
			r.Observe("s", "d", length, Sample{Dist: d, ET: et, Fuel: -1, Brake: brake})
			et += 0.4
		}
	}
	laps := r.Laps("d")
	if len(laps) == 0 || laps[0].Brake == nil {
		t.Fatalf("%+v", laps)
	}
	b := MaxBrake(laps, 3)
	if b[10] == nil || *b[10] < 0.85 || *b[30] != 0 { // section 10 covers 400-440 m
		t.Fatalf("braking %v %v", b[10], b[30])
	}
}

func TestOtherSessionOrTrackLengthStartsOver(t *testing.T) {
	var r Recorder
	drive(&r, "a", 3, 0.5, constant(50), 0, 0)
	r.Observe("other", "a", length, Sample{Dist: 10, ET: 1, Fuel: -1, Brake: -1})
	if len(r.Laps("a")) != 0 {
		t.Fatal("laps of the old session survived")
	}
}
