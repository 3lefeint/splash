// Package wear attributes the tyre wear of the driven car to sections of the track.
package wear

import "sync"

// Segments is the number of equally long track sections a lap is divided into.
const Segments = 50

const (
	// maxJump is the largest wear change between two samples that still counts as driving. Anything bigger
	// is a tyre change or a glitch.
	maxJump = 0.02
	// maxStep is the largest distance between two samples, as a fraction of the lap, that is attributed.
	maxStep = 0.1
)

// LapWear is the wear lost in one lap, by wheel (fl, fr, rl, rr) and track section, as fractions of a tyre.
type LapWear struct {
	Lap int `json:"lap"`
	// Coverage is the fraction of the lap that was measured; a low value means samples were missing.
	Coverage float64              `json:"coverage"`
	Loss     [4][Segments]float64 `json:"loss"`
}

// Tracker collects the wear of consecutive telemetry samples and returns a lap when it is complete.
type Tracker struct {
	mu       sync.Mutex
	key      string
	has      bool
	lap      int
	prevDist float64
	prevWear [4]float64
	cur      LapWear
	covered  float64
}

// Observe feeds one telemetry sample of the driven car. key identifies session and car: a change starts over.
// It returns the lap that was just completed, or nil.
func (t *Tracker) Observe(key string, lap int, dist, length float64, wear []float64) *LapWear {
	if len(wear) != 4 || length <= 0 || dist < 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var w [4]float64
	copy(w[:], wear)
	if !t.has || key != t.key {
		t.start(key, lap, dist, w)
		return nil
	}
	var done *LapWear
	if lap != t.lap {
		d := t.cur
		d.Lap = t.lap
		d.Coverage = min(t.covered/length, 1)
		done = &d
		t.cur, t.covered, t.lap = LapWear{}, 0, lap
		t.prevDist, t.prevWear = dist, w
		return done
	}
	step := dist - t.prevDist
	if step > 0 && step <= maxStep*length {
		var delta [4]float64
		ok := true
		for i := range delta {
			delta[i] = t.prevWear[i] - w[i]
			if delta[i] > maxJump || delta[i] < -maxJump {
				ok = false
			}
		}
		if ok {
			seg := int((t.prevDist + step/2) / length * Segments)
			seg = min(max(seg, 0), Segments-1)
			for i := range delta {
				if delta[i] > 0 {
					t.cur.Loss[i][seg] += delta[i]
				}
			}
			t.covered += step
		}
	}
	t.prevDist, t.prevWear = dist, w
	return nil
}

func (t *Tracker) start(key string, lap int, dist float64, w [4]float64) {
	t.key, t.has, t.lap = key, true, lap
	t.prevDist, t.prevWear = dist, w
	t.cur, t.covered = LapWear{}, 0
}
