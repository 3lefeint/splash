// Package pitlane learns where the pit lane starts and ends and where the pit boxes are, from the positions of
// cars that drive through it.
package pitlane

import (
	"math"
	"strconv"
	"sync"

	"github.com/3lefeint/splash/backend/internal/wire"
)

// Point is a world position x, z in metres.
type Point [2]float64

// Lane is what is known about the pit lane of one track.
type Lane struct {
	Track string  `json:"track"`
	Entry *Point  `json:"entry,omitempty"`
	Exit  *Point  `json:"exit,omitempty"`
	Boxes []Point `json:"boxes"`
}

const (
	// pitStateStopped is the plugin's pit state of a car that stands in its box.
	pitStateStopped = 3
	// sameBox is the distance in metres within which two stops count as the same box.
	sameBox = 6.0
	// maxBoxes bounds the list.
	maxBoxes = 80
	// meanOf is the number of observations a running mean averages before it starts to follow new ones.
	meanOf = 20
	// moved is the shift in metres of a point that counts as a change worth saving.
	moved = 1.0
)

type mean struct {
	p Point
	n int
}

func (m *mean) add(p Point) (changed bool) {
	old := m.p
	if m.n < meanOf {
		m.n++
	}
	f := 1 / float64(m.n)
	m.p = Point{m.p[0] + (p[0]-m.p[0])*f, m.p[1] + (p[1]-m.p[1])*f}
	return m.n == 1 || math.Hypot(old[0]-m.p[0], old[1]-m.p[1]) > moved
}

type carState struct {
	in, stopped bool
	box         *Point
}

// Tracker follows the cars of the running session.
type Tracker struct {
	mu          sync.Mutex
	track       string
	entry, exit *mean
	boxes       []mean
	cars        map[string]*carState
}

func key(v wire.ScoringVehicle) string { return v.VehicleName + "|" + strconv.Itoa(v.CarID) }

// Load starts from what was stored for the track.
func (t *Tracker) Load(l Lane) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reset(l.Track)
	if l.Entry != nil {
		t.entry = &mean{*l.Entry, meanOf}
	}
	if l.Exit != nil {
		t.exit = &mean{*l.Exit, meanOf}
	}
	for _, b := range l.Boxes {
		t.boxes = append(t.boxes, mean{b, meanOf / 2})
	}
}

func (t *Tracker) reset(track string) {
	t.track, t.entry, t.exit, t.boxes, t.cars = track, nil, nil, nil, map[string]*carState{}
}

// Observe processes one scoring sample and reports whether the lane changed enough to be saved.
func (t *Tracker) Observe(sc wire.Scoring) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sc.Track == "" {
		return false
	}
	if t.track != sc.Track || t.cars == nil {
		t.reset(sc.Track)
	}
	changed := false
	for _, v := range sc.Vehicles {
		if v.X == nil || v.Z == nil {
			continue
		}
		p := Point{*v.X, *v.Z}
		c := t.cars[key(v)]
		if c == nil {
			// the first sight only tells the state, it must not count as an entry or an exit
			t.cars[key(v)] = &carState{in: v.InPits, stopped: v.PitState == pitStateStopped}
			continue
		}
		if v.InPits && !c.in {
			if t.entry == nil {
				t.entry = &mean{}
			}
			changed = t.entry.add(p) || changed
		}
		if !v.InPits && c.in {
			if t.exit == nil {
				t.exit = &mean{}
			}
			changed = t.exit.add(p) || changed
		}
		stopped := v.PitState == pitStateStopped
		if stopped && !c.stopped {
			var box *Point
			box, changed = t.box(p, changed)
			c.box = box
		}
		c.in, c.stopped = v.InPits, stopped
	}
	return changed
}

// box finds the known box a car stopped at or adds a new one.
func (t *Tracker) box(p Point, changed bool) (*Point, bool) {
	for i := range t.boxes {
		if math.Hypot(t.boxes[i].p[0]-p[0], t.boxes[i].p[1]-p[1]) <= sameBox {
			if t.boxes[i].add(p) {
				changed = true
			}
			bp := t.boxes[i].p
			return &bp, changed
		}
	}
	if len(t.boxes) >= maxBoxes {
		return nil, changed
	}
	t.boxes = append(t.boxes, mean{p, 1})
	bp := p
	return &bp, true
}

// Lane returns what is known for the track; nil while nothing is.
func (t *Tracker) Lane(track string) *Lane {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.track != track || (t.entry == nil && t.exit == nil && len(t.boxes) == 0) {
		return nil
	}
	l := &Lane{Track: track, Boxes: []Point{}}
	if t.entry != nil {
		p := t.entry.p
		l.Entry = &p
	}
	if t.exit != nil {
		p := t.exit.p
		l.Exit = &p
	}
	for _, b := range t.boxes {
		l.Boxes = append(l.Boxes, b.p)
	}
	return l
}

// StoppedAt is the box the car with the given key last stopped at, or nil.
func (t *Tracker) StoppedAt(vehicle string, id int) *Point {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.cars[vehicle+"|"+strconv.Itoa(id)]; c != nil && c.box != nil {
		p := *c.box
		return &p
	}
	return nil
}
