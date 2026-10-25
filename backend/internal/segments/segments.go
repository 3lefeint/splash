// Package segments measures how a lap is spent along the track: the time, the fuel and the braking in each of
// N equal sections. Cars are followed by their distance along the lap, interpolated between the samples.
package segments

import (
	"math"
	"sync"
)

// N is the number of sections of a lap; section i covers the distance from i*L/N to (i+1)*L/N.
const N = 50

const (
	// keepLaps is the number of valid laps kept per car.
	keepLaps = 5
	// maxStep is the largest jump of the lap distance between two samples, as a share of the lap, that still
	// counts as driving; more means samples are missing and the lap is dropped.
	maxStep = 0.15
	// maxGap is the largest game time in seconds between two samples of a valid lap.
	maxGap = 10.0
)

// Lap is one complete valid lap of a car.
type Lap struct {
	Time  [N]float64 // seconds spent in each section
	Total float64
	// Fuel is the litres used in each section and Brake the strongest brake pedal (0..1) seen in it; both only
	// for laps recorded from telemetry (Fuel is nil for the others).
	Fuel  *[N]float64
	Brake *[N]float64
}

// Sample is one observation of a car: where it is along the lap and when.
type Sample struct {
	Dist, ET float64
	InPits   bool
	// Fuel and Brake are optional; a negative value means not available.
	Fuel, Brake float64
}

// run is the lap in progress of one car.
type run struct {
	active   bool
	dirty    bool
	prev     *Sample
	t        [N + 1]float64
	have     [N + 1]bool
	fuel     [N + 1]float64
	haveFuel bool
	brake    [N]float64
	haveBr   bool
	laps     []Lap
	best     *Lap
}

func lerp(a, b, f float64) float64 { return a + (b-a)*f }

// Recorder follows many cars; keys identify them.
type Recorder struct {
	mu      sync.Mutex
	session string
	length  float64
	cars    map[string]*run
}

// Observe adds a sample of the car with the given key. Samples must come in time order per car; a sample that is
// not newer than the last is ignored (the same sample from a second agent).
func (r *Recorder) Observe(session, key string, length float64, s Sample) {
	if length <= 0 || s.Dist < 0 || s.Dist > length*1.02 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != session || r.cars == nil || r.length != length {
		r.session, r.length, r.cars = session, length, map[string]*run{}
	}
	c := r.cars[key]
	if c == nil {
		c = &run{}
		r.cars[key] = c
	}
	c.add(s, length)
}

func (c *run) add(s Sample, length float64) {
	if c.prev != nil && s.ET <= c.prev.ET {
		return
	}
	defer func() { c.prev = &s }()
	if s.InPits {
		c.dirty = true
	}
	if s.Brake >= 0 && c.active {
		i := min(int(s.Dist/length*N), N-1)
		c.brake[i], c.haveBr = math.Max(c.brake[i], s.Brake), true
	}
	p := c.prev
	if p == nil {
		return
	}
	if s.Dist < p.Dist-length/2 { // crossed the line
		c.cross(p, &s, length)
		return
	}
	if s.Dist < p.Dist { // went backwards: a spin or a reset
		c.dirty = true
		return
	}
	if (s.Dist-p.Dist) > length*maxStep || s.ET-p.ET > maxGap {
		c.dirty = true
	}
	if c.active {
		c.boundaries(p, s.Dist, s, length)
	}
}

// boundaries records the sections boundaries passed between p and a sample at the distance d (which may exceed the lap
// length for the sample after the line).
func (c *run) boundaries(p *Sample, d float64, s Sample, length float64) {
	for k := 1; k <= N; k++ {
		b := float64(k) * length / N
		if b <= p.Dist || b > d {
			continue
		}
		f := (b - p.Dist) / (d - p.Dist)
		c.t[k], c.have[k] = lerp(p.ET, s.ET, f), true
		if p.Fuel >= 0 && s.Fuel >= 0 {
			c.fuel[k], c.haveFuel = lerp(p.Fuel, s.Fuel, f), true
		}
	}
}

// cross finishes the lap in progress at the line and starts the next one.
func (c *run) cross(p, s *Sample, length float64) {
	d := s.Dist + length
	if c.active {
		c.boundaries(p, d, *s, length)
		c.finish()
	}
	f := (length - p.Dist) / (d - p.Dist)
	*c = run{active: true, prev: c.prev, laps: c.laps, best: c.best}
	c.t[0], c.have[0] = lerp(p.ET, s.ET, f), true
	if p.Fuel >= 0 && s.Fuel >= 0 {
		c.fuel[0], c.haveFuel = lerp(p.Fuel, s.Fuel, f), true
	}
	if s.ET-p.ET > maxGap || d-p.Dist > length*maxStep*2 {
		c.dirty = true
	}
	// the sample after the line may already be past the next boundaries
	c.boundaries(&Sample{Dist: 0, ET: c.t[0], Fuel: c.fuel[0]}, s.Dist, *s, length)
}

func (c *run) finish() {
	if c.dirty {
		return
	}
	for k := 0; k <= N; k++ {
		if !c.have[k] {
			return
		}
	}
	var lap Lap
	for i := 0; i < N; i++ {
		d := c.t[i+1] - c.t[i]
		if d <= 0 {
			return
		}
		lap.Time[i] = d
		lap.Total += d
	}
	if c.haveFuel {
		var f [N]float64
		for i := 0; i < N; i++ {
			f[i] = c.fuel[i] - c.fuel[i+1]
		}
		lap.Fuel = &f
	}
	if c.haveBr {
		b := c.brake
		lap.Brake = &b
	}
	c.laps = append(c.laps, lap)
	if len(c.laps) > keepLaps {
		c.laps = c.laps[len(c.laps)-keepLaps:]
	}
	if c.best == nil || lap.Total < c.best.Total {
		l := lap
		c.best = &l
	}
}

// Laps returns the valid laps of a car, oldest first.
func (r *Recorder) Laps(key string) []Lap {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.cars[key]; c != nil {
		return append([]Lap(nil), c.laps...)
	}
	return nil
}

// Best returns the fastest valid lap of a car since the session started, or nil.
func (r *Recorder) Best(key string) *Lap {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.cars[key]; c != nil && c.best != nil {
		b := *c.best
		return &b
	}
	return nil
}

// Profile is the mean of a quantity over the given laps for each section; a section without a value is nil.
type Profile [N]*float64

// MeanTime is the mean time per section over the last n of the laps.
func MeanTime(laps []Lap, n int) Profile {
	return mean(laps, n, func(l Lap) *[N]float64 { return &l.Time })
}

// MeanFuel is the mean fuel use per section; only laps recorded with fuel count.
func MeanFuel(laps []Lap, n int) Profile {
	return mean(laps, n, func(l Lap) *[N]float64 { return l.Fuel })
}

// MaxBrake is the mean over the laps of the strongest brake pedal in each section.
func MaxBrake(laps []Lap, n int) Profile {
	return mean(laps, n, func(l Lap) *[N]float64 { return l.Brake })
}

func mean(laps []Lap, n int, get func(Lap) *[N]float64) Profile {
	var p Profile
	if n > 0 && len(laps) > n {
		laps = laps[len(laps)-n:]
	}
	var sum [N]float64
	var cnt int
	for _, l := range laps {
		v := get(l)
		if v == nil {
			continue
		}
		cnt++
		for i := range sum {
			sum[i] += v[i]
		}
	}
	if cnt == 0 {
		return p
	}
	for i := range p {
		m := sum[i] / float64(cnt)
		p[i] = &m
	}
	return p
}

// Diff is a minus b for each section; nil where either is missing.
func Diff(a, b Profile) Profile {
	var out Profile
	for i := range out {
		if a[i] != nil && b[i] != nil {
			d := *a[i] - *b[i]
			out[i] = &d
		}
	}
	return out
}

// Of converts a single lap to a profile.
func Of(l *Lap) Profile {
	var p Profile
	if l == nil {
		return p
	}
	for i := range p {
		v := l.Time[i]
		p[i] = &v
	}
	return p
}

// Speeds converts a time profile to the mean speed in m/s in each section of a lap of the given length.
func Speeds(t Profile, length float64) Profile {
	var out Profile
	for i := range out {
		if t[i] != nil && *t[i] > 0 {
			v := length / N / *t[i]
			out[i] = &v
		}
	}
	return out
}
