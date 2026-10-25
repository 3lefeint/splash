package wear

import (
	"math"
	"testing"
)

const length = 2000.0

// drive feeds a lap in 20 m steps; loss(dist) is the wear lost per metre at that distance.
func drive(t *Tracker, lap int, start float64, loss func(d float64) float64) (end float64, done *LapWear) {
	w := start
	for d := 0.0; d < length; d += 20 {
		w -= loss(d) * 20
		if r := t.Observe("k", lap, d, length, []float64{w, w, w, w}); r != nil {
			done = r
		}
	}
	return w, done
}

func TestWearIsAttributedToTheSectionWhereItHappened(t *testing.T) {
	var tr Tracker
	// 0.0001 per metre only in the first quarter of the lap, none elsewhere.
	hard := func(d float64) float64 {
		if d < length/4 {
			return 0.0001
		}
		return 0
	}
	w, _ := drive(&tr, 1, 1.0, hard)
	_, done := drive(&tr, 2, w, hard)
	// The first sample of lap 3 completes lap 2.
	if r := tr.Observe("k", 3, 0, length, []float64{w, w, w, w}); r != nil {
		done = r
	}
	if done == nil || done.Lap != 2 {
		t.Fatalf("done=%+v", done)
	}
	var first, rest float64
	for s := 0; s < Segments; s++ {
		if s < Segments/4 {
			first += done.Loss[0][s]
		} else {
			rest += done.Loss[0][s]
		}
	}
	if math.Abs(first-0.05) > 0.002 || rest > 0.001 {
		t.Fatalf("first quarter %.4f, rest %.4f", first, rest)
	}
	if done.Coverage < 0.95 {
		t.Fatalf("coverage %.2f", done.Coverage)
	}
}

func TestATyreChangeJumpIsNotCountedAsWear(t *testing.T) {
	var tr Tracker
	tr.Observe("k", 1, 100, length, []float64{0.5, 0.5, 0.5, 0.5})
	tr.Observe("k", 1, 120, length, []float64{1, 1, 1, 1}) // new set: wear goes up by 0.5
	tr.Observe("k", 1, 140, length, []float64{0.9999, 1, 1, 1})
	r := tr.Observe("k", 2, 10, length, []float64{0.9999, 1, 1, 1})
	if r == nil {
		t.Fatal("lap not completed")
	}
	var total float64
	for s := 0; s < Segments; s++ {
		total += r.Loss[0][s]
	}
	if math.Abs(total-0.0001) > 1e-9 {
		t.Fatalf("total %.6f", total)
	}
}

func TestSamplesWithGapsLowerTheCoverage(t *testing.T) {
	var tr Tracker
	tr.Observe("k", 1, 0, length, []float64{1, 1, 1, 1})
	tr.Observe("k", 1, 1000, length, []float64{0.99, 0.99, 0.99, 0.99}) // a 1 km hole is not attributed
	tr.Observe("k", 1, 1020, length, []float64{0.9899, 0.9899, 0.9899, 0.9899})
	r := tr.Observe("k", 2, 0, length, []float64{0.9899, 0.9899, 0.9899, 0.9899})
	if r == nil || r.Coverage > 0.02 {
		t.Fatalf("%+v", r)
	}
}

func TestANewKeyStartsOver(t *testing.T) {
	var tr Tracker
	tr.Observe("a", 1, 0, length, []float64{1, 1, 1, 1})
	if r := tr.Observe("b", 2, 0, length, []float64{1, 1, 1, 1}); r != nil {
		t.Fatal("a different car must not complete a lap")
	}
}
