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
	// EntryDist and ExitDist are the distances in metres along the lap at which cars enter and leave the pit lane, as
	// seen in the scoring samples. Absent for lanes stored before they were learned.
	EntryDist *float64 `json:"entry_dist,omitempty"`
	ExitDist  *float64 `json:"exit_dist,omitempty"`
}

// Stretch returns the length in metres of the pit lane along the lap, from the entry to the exit, for a track of the
// given length. The start/finish line may lie inside the pit lane: the stretch then wraps around the lap. It returns
// false while a distance is unknown, or when the stretch would be longer than half a lap (not a pit lane).
func (l Lane) Stretch(length float64) (float64, bool) {
	if l.EntryDist == nil || l.ExitDist == nil || length <= 0 {
		return 0, false
	}
	d := math.Mod(*l.ExitDist-*l.EntryDist, length)
	if d < 0 {
		d += length
	}
	if d <= 0 || d > length/2 {
		return 0, false
	}
	return d, true
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

// dist is a running mean of a distance along the lap. The lap is a circle, so the mean is taken of the offsets from the
// first value, and an entry just before the line and one just after it do not average to the middle of the lap.
type dist struct {
	ref, off float64
	n        int
}

func (d *dist) add(v, length float64) (changed bool) {
	if d.n == 0 {
		d.ref = v
	}
	delta := math.Mod(v-d.ref, length)
	if delta > length/2 {
		delta -= length
	} else if delta < -length/2 {
		delta += length
	}
	old := d.off
	if d.n < meanOf {
		d.n++
	}
	d.off += (delta - d.off) / float64(d.n)
	return d.n == 1 || math.Abs(d.off-old) > moved
}

func (d *dist) value(length float64) float64 {
	if length <= 0 { // not seen on this run yet: the stored value
		return d.ref
	}
	v := math.Mod(d.ref+d.off, length)
	if v < 0 {
		v += length
	}
	return v
}

type carState struct {
	in, stopped bool
	box         *Point
	// last is the last lap distance seen outside the pit lane. When a sample has no lap distance at the moment the car
	// enters the lane, it stands in for the entry: the car was at most one sample before it.
	last *float64
	// exitPending is set when a car left the lane in a sample without a lap distance; the next sample outside the lane
	// that has one gives the exit.
	exitPending bool
}

// Tracker follows the cars of the running session.
type Tracker struct {
	mu          sync.Mutex
	track       string
	entry, exit *mean
	// entryD and exitD are the distances along the lap at the entry and the exit; length is the track length they
	// belong to.
	entryD, exitD *dist
	length        float64
	boxes         []mean
	cars          map[string]*carState
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
	if l.EntryDist != nil {
		t.entryD = &dist{ref: *l.EntryDist, n: meanOf}
	}
	if l.ExitDist != nil {
		t.exitD = &dist{ref: *l.ExitDist, n: meanOf}
	}
}

func (t *Tracker) reset(track string) {
	t.track, t.entry, t.exit, t.boxes, t.cars = track, nil, nil, nil, map[string]*carState{}
	t.entryD, t.exitD = nil, nil
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
	if sc.TrackLength > 0 {
		t.length = sc.TrackLength
	}
	for _, v := range sc.Vehicles {
		if v.X == nil || v.Z == nil {
			continue
		}
		p := Point{*v.X, *v.Z}
		c := t.cars[key(v)]
		if c == nil {
			// the first sight only tells the state, it must not count as an entry or an exit
			c = &carState{in: v.InPits, stopped: v.PitState == pitStateStopped}
			if !v.InPits && v.LapDist != nil {
				d := *v.LapDist
				c.last = &d
			}
			t.cars[key(v)] = c
			continue
		}
		if v.InPits && !c.in {
			if t.entry == nil {
				t.entry = &mean{}
			}
			changed = t.entry.add(p) || changed
			// The sample of the entry normally carries the lap distance; if it does not, the one before it, taken
			// outside the lane, is used.
			d := v.LapDist
			if d == nil {
				d = c.last
			}
			if d != nil && sc.TrackLength > 0 {
				if t.entryD == nil {
					t.entryD = &dist{}
				}
				changed = t.entryD.add(*d, sc.TrackLength) || changed
			}
		}
		if !v.InPits && c.in {
			if t.exit == nil {
				t.exit = &mean{}
			}
			changed = t.exit.add(p) || changed
			if v.LapDist != nil && sc.TrackLength > 0 {
				changed = t.addExitDist(*v.LapDist, sc.TrackLength) || changed
			} else {
				c.exitPending = true
			}
		}
		if !v.InPits {
			if c.exitPending && v.LapDist != nil && sc.TrackLength > 0 {
				changed = t.addExitDist(*v.LapDist, sc.TrackLength) || changed
				c.exitPending = false
			}
			if v.LapDist != nil {
				d := *v.LapDist
				c.last = &d
			}
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

func (t *Tracker) addExitDist(d, length float64) bool {
	if t.exitD == nil {
		t.exitD = &dist{}
	}
	return t.exitD.add(d, length)
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
	if t.track != track || (t.entry == nil && t.exit == nil && len(t.boxes) == 0 && t.entryD == nil && t.exitD == nil) {
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
	if t.entryD != nil {
		v := t.entryD.value(t.length)
		l.EntryDist = &v
	}
	if t.exitD != nil {
		v := t.exitD.value(t.length)
		l.ExitDist = &v
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
