// Package trackmap builds the outline of a track from the positions of cars on a clean lap.
package trackmap

import (
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/3lefeint/splash/backend/internal/wire"
)

// Points is the number of points of a stored outline.
const Points = 500

// Map is the outline of one track. Point i lies at i*Length/len(Points) metres from the start line;
// the outline is closed (the last point connects to the first).
type Map struct {
	Track  string       `json:"track"`
	Length float64      `json:"length"`
	Points [][2]float64 `json:"points"` // x, z in world metres
}

type sample struct{ dist, x, z float64 }

type carLap struct {
	laps    int
	started bool
	tainted bool
	samples []sample
}

// Builder watches scoring samples and returns a Map once a car has completed a clean lap.
type Builder struct {
	mu   sync.Mutex
	cars map[string]*carLap
}

func NewBuilder() *Builder { return &Builder{cars: map[string]*carLap{}} }

// Reset forgets the samples collected so far, e.g. on a new session.
func (b *Builder) Reset() {
	b.mu.Lock()
	b.cars = map[string]*carLap{}
	b.mu.Unlock()
}

// Observe feeds one scoring sample. It returns a map when a vehicle just completed a clean lap.
func (b *Builder) Observe(sc wire.Scoring) *Map {
	if sc.TrackLength <= 0 || sc.Track == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out *Map
	for _, v := range sc.Vehicles {
		if v.LapDist == nil || v.X == nil || v.Z == nil {
			continue
		}
		key := v.VehicleName + "|" + strconv.Itoa(v.CarID)
		c := b.cars[key]
		if c == nil {
			c = &carLap{laps: v.Laps}
			b.cars[key] = c
		}
		if v.Laps != c.laps {
			if out == nil && v.Laps == c.laps+1 && c.started && !c.tainted {
				out = build(sc.Track, sc.TrackLength, c.samples)
			}
			*c = carLap{laps: v.Laps}
		}
		if v.InPits {
			c.tainted = true
			continue
		}
		d := *v.LapDist
		if d < 0 || d > sc.TrackLength*1.01 {
			continue
		}
		if n := len(c.samples); n > 0 && d < c.samples[n-1].dist-50 {
			c.tainted = true // went backwards: spin, reset or a lap-distance wrap
		}
		c.started = c.started || d <= math.Max(150, sc.TrackLength*0.05)
		if len(c.samples) > 0 || c.started {
			c.samples = append(c.samples, sample{d, *v.X, *v.Z})
		}
	}
	return out
}

// build turns the samples of one lap into a closed, evenly spaced outline. It returns nil when the lap
// does not cover the track well enough.
func build(track string, length float64, in []sample) *Map {
	if len(in) < 20 {
		return nil
	}
	s := append([]sample(nil), in...)
	sort.Slice(s, func(i, j int) bool { return s[i].dist < s[j].dist })
	maxGap := math.Max(120, length*0.04)
	if s[0].dist > maxGap || length-s[len(s)-1].dist > maxGap {
		return nil
	}
	for i := 1; i < len(s); i++ {
		if s[i].dist-s[i-1].dist > maxGap {
			return nil
		}
	}
	// Close the loop with the first sample one lap later.
	first := s[0]
	s = append(s, sample{first.dist + length, first.x, first.z})
	pts := make([][2]float64, Points)
	for i := range pts {
		d := float64(i) * length / Points
		if d < s[0].dist {
			d += length
		}
		j := sort.Search(len(s), func(k int) bool { return s[k].dist >= d }) - 1
		j = min(max(j, 0), len(s)-2)
		a, b := s[j], s[j+1]
		t := 0.0
		if b.dist > a.dist {
			t = math.Min(math.Max((d-a.dist)/(b.dist-a.dist), 0), 1)
		}
		pts[i] = [2]float64{a.x + (b.x-a.x)*t, a.z + (b.z-a.z)*t}
	}
	return &Map{Track: track, Length: length, Points: pts}
}

// Progress is the part of the outline recorded so far.
type Progress struct {
	Track   string       `json:"track"`
	Covered float64      `json:"covered"` // 0 to 1: how much of the lap the best car has driven
	Points  [][2]float64 `json:"points"`  // x, z in world metres, in driving order
}

// maxProgressPoints bounds the size of a Progress.
const maxProgressPoints = 300

// Partial returns the lap in progress of the car that has driven furthest without a pit stop or a spin,
// or nil while no car has started a clean lap.
func (b *Builder) Partial(track string, length float64) *Progress {
	if length <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var best *carLap
	for _, c := range b.cars {
		if c.started && !c.tainted && len(c.samples) >= 2 && (best == nil || last(c).dist > last(best).dist) {
			best = c
		}
	}
	if best == nil {
		return nil
	}
	step := 1
	if n := len(best.samples); n > maxProgressPoints {
		step = (n + maxProgressPoints - 1) / maxProgressPoints
	}
	var pts [][2]float64
	for i := 0; i < len(best.samples); i += step {
		pts = append(pts, [2]float64{best.samples[i].x, best.samples[i].z})
	}
	if l := last(best); step > 1 {
		pts = append(pts, [2]float64{l.x, l.z})
	}
	return &Progress{Track: track, Covered: math.Min(last(best).dist/length, 1), Points: pts}
}

func last(c *carLap) sample { return c.samples[len(c.samples)-1] }
