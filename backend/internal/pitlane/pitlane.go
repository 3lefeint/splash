// Package pitlane learns where the pit lane starts and ends and where the pit boxes are, from the positions of
// cars that drive through it.
package pitlane

import (
	"math"
	"strconv"
	"sync"
	"time"

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
	// pitStateEntering and pitStateStopped are the plugin's pit states of a car that drives into the lane and of one that
	// stands in its box; pitStateExiting is the state while it leaves the box.
	pitStateEntering = 2
	pitStateStopped  = 3
	pitStateExiting  = 4
	// sameBox is the distance in metres within which two stops count as the same box.
	sameBox = 6.0
	// maxBoxes bounds the list.
	maxBoxes = 80
	// meanOf is the number of observations a running mean averages before it starts to follow new ones.
	meanOf = 20
	// moved is the shift in metres of a point that counts as a change worth saving.
	moved = 1.0
	// maxSpeed and slack bound how far a car can have moved between two samples: maxSpeed * seconds + slack metres. A
	// car that "jumps" to its box (return to the garage, a new session, a start from the garage) moves farther.
	maxSpeed = 100.0
	slack    = 50.0
	// maxGap is the longest time in seconds between two samples of a car for which its movement can be judged.
	maxGap = 10.0
	// clusterWidth is the distance in metres within which observations of a distance along the lap belong together.
	clusterWidth = 60.0
	// maxClusters bounds the clusters kept for one distance.
	maxClusters = 6
	// pruneAfter forgets the state of a source that sent nothing for this long.
	pruneAfter = 10 * time.Minute
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

// wrap maps a difference of two lap distances into (-length/2, length/2]: the lap is a circle.
func wrap(delta, length float64) float64 {
	delta = math.Mod(delta, length)
	if delta > length/2 {
		delta -= length
	} else if delta < -length/2 {
		delta += length
	}
	return delta
}

// cluster is a group of observations of a distance along the lap that lie close together: their mean, taken as offsets
// from the first one so that values on both sides of the start line do not average to the middle of the lap.
type cluster struct {
	ref, off float64
	n, seq   int
	id       int
}

// dist is a distance along the lap learned from many observations. A single false observation (a car that was put into
// its box somewhere on the track) forms a cluster of its own and cannot move the value; the value is the centre of the
// cluster with the most observations, the newest winning a tie.
type dist struct {
	cs  []cluster
	seq int
}

func (d *dist) best() *cluster {
	var b *cluster
	for i := range d.cs {
		c := &d.cs[i]
		if b == nil || c.n > b.n || (c.n == b.n && c.seq > b.seq) {
			b = c
		}
	}
	return b
}

func (d *dist) value(length float64) float64 {
	b := d.best()
	if length <= 0 { // not seen on this run yet: the stored value
		return b.ref
	}
	v := math.Mod(b.ref+b.off, length)
	if v < 0 {
		v += length
	}
	return v
}

// add takes an observation in and reports whether the value changed by more than a metre or another cluster won.
func (d *dist) add(v, length float64) (changed bool) {
	d.seq++
	var oldID int
	var oldVal float64
	b := d.best()
	if b != nil {
		oldID, oldVal = b.id, d.value(length)
	}
	joined := false
	for i := range d.cs {
		c := &d.cs[i]
		if math.Abs(wrap(v-(c.ref+c.off), length)) <= clusterWidth {
			if c.n < meanOf {
				c.n++
			}
			c.off += (wrap(v-c.ref, length) - c.off) / float64(c.n)
			c.seq = d.seq
			joined = true
			break
		}
	}
	if !joined {
		d.cs = append(d.cs, cluster{ref: v, n: 1, seq: d.seq, id: d.seq})
		if len(d.cs) > maxClusters { // drop the weakest, the oldest first
			w := 0
			for i, c := range d.cs {
				if c.n < d.cs[w].n || (c.n == d.cs[w].n && c.seq < d.cs[w].seq) {
					w = i
				}
			}
			d.cs = append(d.cs[:w], d.cs[w+1:]...)
		}
	}
	nb := d.best()
	if b == nil || nb.id != oldID {
		return true
	}
	return math.Abs(wrap(d.value(length)-oldVal, length)) > moved
}

// demote lowers the weight of everything stored to one observation, so that a few new ones can outvote it.
func (d *dist) demote() {
	for i := range d.cs {
		d.cs[i].n = 1
	}
}

type sample struct {
	et, x, z float64
	dist     *float64
}

type carState struct {
	in, stopped bool
	box         *Point
	// exitPending is set when a car left the lane in a sample without a lap distance; the next sample outside the lane
	// that has one gives the exit, if it comes within exitBy (game time). A distance from much later is not the exit.
	exitPending bool
	exitBy      float64
	prev        sample
}

// laneData is what is learned about one track.
type laneData struct {
	entry, exit   *mean
	entryD, exitD *dist
	boxes         []mean
	// length is the track length the distances belong to. checked says the stored distances were compared with it.
	length  float64
	checked bool
}

// check compares distances that were stored earlier with the track length as soon as it is known. A pair that does not
// make a pit lane is demoted, so that the first genuine passes replace it.
func (l *laneData) check() {
	if l.checked || l.length <= 0 {
		return
	}
	l.checked = true
	if l.entryD == nil || l.exitD == nil {
		return
	}
	probe := Lane{}
	e, x := l.entryD.value(l.length), l.exitD.value(l.length)
	probe.EntryDist, probe.ExitDist = &e, &x
	if _, ok := probe.Stretch(l.length); !ok {
		l.entryD.demote()
		l.exitD.demote()
	}
}

// source is the stream of scoring samples of one agent in one session. Cars are followed per source: two agents see the
// same cars with different timing, and mixing their samples would look like cars that jump.
type source struct {
	track string
	cars  map[string]*carState
	seen  time.Time
}

// Event is one change of the pit flag of a car: counted as an entry or an exit, or ignored with the reason.
type Event struct {
	Kind   string // entry or exit
	Source string
	Car    string
	Track  string
	Dist   *float64 // lap distance of the sample, if it had one
	State  int      // pit state of the sample
	Step   float64  // metres moved since the previous sample, -1 when unknown
	Gap    float64  // seconds since the previous sample
	// Accepted is true when the event was counted. Reason says why not: gap (the previous sample is too old or too
	// new to judge), jump (the car moved farther than it can drive) or state (the pit state does not belong to an
	// entry or an exit).
	Accepted bool
	Reason   string
}

// Tracker follows the cars of the running sessions and learns the pit lane of every track it sees.
type Tracker struct {
	mu    sync.Mutex
	lanes map[string]*laneData
	srcs  map[string]*source
	prune time.Time
	// OnEvent, when set, is told about every change of the pit flag, counted or not. It is called without the lock.
	OnEvent func(Event)
}

func key(v wire.ScoringVehicle) string { return v.VehicleName + "|" + strconv.Itoa(v.CarID) }

func (t *Tracker) lane(track string) *laneData {
	if t.lanes == nil {
		t.lanes = map[string]*laneData{}
	}
	l := t.lanes[track]
	if l == nil {
		l = &laneData{}
		t.lanes[track] = l
	}
	return l
}

// Load starts from what was stored for the track.
func (t *Tracker) Load(l Lane) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lanes == nil {
		t.lanes = map[string]*laneData{}
	}
	ln := &laneData{}
	t.lanes[l.Track] = ln
	if l.Entry != nil {
		ln.entry = &mean{*l.Entry, meanOf}
	}
	if l.Exit != nil {
		ln.exit = &mean{*l.Exit, meanOf}
	}
	for _, b := range l.Boxes {
		ln.boxes = append(ln.boxes, mean{b, meanOf / 2})
	}
	if l.EntryDist != nil {
		ln.entryD = &dist{cs: []cluster{{ref: *l.EntryDist, n: meanOf, id: -1}}}
	}
	if l.ExitDist != nil {
		ln.exitD = &dist{cs: []cluster{{ref: *l.ExitDist, n: meanOf, id: -1}}}
	}
}

// Reset forgets what was learned about the track, so that it is learned again from the next passes.
func (t *Tracker) Reset(track string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.lanes, track)
}

// Observe processes one scoring sample of a source (an agent in a session) taken at game time et and reports whether the
// lane changed enough to be saved.
func (t *Tracker) Observe(src string, et float64, sc wire.Scoring) bool {
	t.mu.Lock()
	changed, events := t.observe(src, et, sc)
	t.mu.Unlock()
	if t.OnEvent != nil {
		for _, e := range events {
			t.OnEvent(e)
		}
	}
	return changed
}

func (t *Tracker) observe(srcKey string, et float64, sc wire.Scoring) (changed bool, events []Event) {
	if sc.Track == "" {
		return false, nil
	}
	if t.srcs == nil {
		t.srcs = map[string]*source{}
	}
	now := time.Now()
	if now.Sub(t.prune) > time.Minute {
		t.prune = now
		for k, s := range t.srcs {
			if now.Sub(s.seen) > pruneAfter {
				delete(t.srcs, k)
			}
		}
	}
	s := t.srcs[srcKey]
	if s == nil || s.track != sc.Track {
		s = &source{track: sc.Track, cars: map[string]*carState{}}
		t.srcs[srcKey] = s
	}
	s.seen = now
	ln := t.lane(sc.Track)
	if sc.TrackLength > 0 {
		ln.length = sc.TrackLength
		ln.check()
	}
	for _, v := range sc.Vehicles {
		if v.X == nil || v.Z == nil {
			continue
		}
		p := Point{*v.X, *v.Z}
		cur := sample{et: et, x: p[0], z: p[1], dist: v.LapDist}
		k := key(v)
		c := s.cars[k]
		if c == nil {
			// the first sight only tells the state, it must not count as an entry or an exit
			c = &carState{in: v.InPits, stopped: v.PitState == pitStateStopped, prev: cur}
			s.cars[k] = c
			continue
		}
		if v.InPits && !c.in {
			ev := judge("entry", srcKey, sc.Track, k, c, v, cur, sc.TrackLength, pitStateEntering)
			events = append(events, ev)
			if ev.Accepted {
				if ln.entry == nil {
					ln.entry = &mean{}
				}
				changed = ln.entry.add(p) || changed
				// The sample of the entry normally carries the lap distance; if it does not, the sample right before it,
				// taken outside the lane, is used when that one has it. An older distance would be wrong: the game gives
				// no distance for some stretches (the player on a shortcut), and the last one before such a gap can lie
				// kilometres back.
				d := v.LapDist
				if d == nil {
					d = c.prev.dist
				}
				if d != nil && sc.TrackLength > 0 {
					if ln.entryD == nil {
						ln.entryD = &dist{}
					}
					changed = ln.entryD.add(*d, sc.TrackLength) || changed
				}
			}
		}
		if !v.InPits && c.in {
			ev := judge("exit", srcKey, sc.Track, k, c, v, cur, sc.TrackLength, 0, pitStateExiting)
			events = append(events, ev)
			if ev.Accepted {
				if ln.exit == nil {
					ln.exit = &mean{}
				}
				changed = ln.exit.add(p) || changed
				if v.LapDist != nil && sc.TrackLength > 0 {
					changed = ln.addExitDist(*v.LapDist, sc.TrackLength) || changed
				} else {
					c.exitPending, c.exitBy = true, et+maxGap
				}
			}
		}
		if !v.InPits && c.exitPending {
			if et > c.exitBy {
				c.exitPending = false
			} else if v.LapDist != nil && sc.TrackLength > 0 {
				changed = ln.addExitDist(*v.LapDist, sc.TrackLength) || changed
				c.exitPending = false
			}
		}
		stopped := v.PitState == pitStateStopped
		if stopped && !c.stopped && v.InPits {
			var box *Point
			box, changed = ln.box(p, changed)
			c.box = box
		}
		c.in, c.stopped, c.prev = v.InPits, stopped, cur
	}
	return changed, events
}

// judge decides whether a change of the pit flag is a real entry or exit: the car must have moved the way a car moves
// since its previous sample, and its pit state must be one that belongs to the change.
func judge(kind, src, track, car string, c *carState, v wire.ScoringVehicle, cur sample, length float64, states ...int) Event {
	ev := Event{Kind: kind, Source: src, Car: car, Track: track, Dist: v.LapDist, State: v.PitState, Step: -1, Gap: cur.et - c.prev.et}
	if !(ev.Gap > 0 && ev.Gap <= maxGap) {
		ev.Reason = "gap"
		return ev
	}
	ev.Step = math.Hypot(cur.x-c.prev.x, cur.z-c.prev.z)
	if cur.dist != nil && c.prev.dist != nil && length > 0 {
		if d := math.Abs(wrap(*cur.dist-*c.prev.dist, length)); d > ev.Step {
			ev.Step = d
		}
	}
	if ev.Step > maxSpeed*ev.Gap+slack {
		ev.Reason = "jump"
		return ev
	}
	for _, s := range states {
		if v.PitState == s {
			ev.Accepted = true
			return ev
		}
	}
	ev.Reason = "state"
	return ev
}

func (l *laneData) addExitDist(d, length float64) bool {
	if l.exitD == nil {
		l.exitD = &dist{}
	}
	return l.exitD.add(d, length)
}

// box finds the known box a car stopped at or adds a new one.
func (l *laneData) box(p Point, changed bool) (*Point, bool) {
	for i := range l.boxes {
		if math.Hypot(l.boxes[i].p[0]-p[0], l.boxes[i].p[1]-p[1]) <= sameBox {
			if l.boxes[i].add(p) {
				changed = true
			}
			bp := l.boxes[i].p
			return &bp, changed
		}
	}
	if len(l.boxes) >= maxBoxes {
		return nil, changed
	}
	l.boxes = append(l.boxes, mean{p, 1})
	bp := p
	return &bp, true
}

// Lane returns what is known for the track; nil while nothing is.
func (t *Tracker) Lane(track string) *Lane {
	t.mu.Lock()
	defer t.mu.Unlock()
	ln := t.lanes[track]
	if ln == nil || (ln.entry == nil && ln.exit == nil && len(ln.boxes) == 0 && ln.entryD == nil && ln.exitD == nil) {
		return nil
	}
	l := &Lane{Track: track, Boxes: []Point{}}
	if ln.entry != nil {
		p := ln.entry.p
		l.Entry = &p
	}
	if ln.exit != nil {
		p := ln.exit.p
		l.Exit = &p
	}
	for _, b := range ln.boxes {
		l.Boxes = append(l.Boxes, b.p)
	}
	if ln.entryD != nil {
		v := ln.entryD.value(ln.length)
		l.EntryDist = &v
	}
	if ln.exitD != nil {
		v := ln.exitD.value(ln.length)
		l.ExitDist = &v
	}
	return l
}

// StoppedAt is the box the car with the given key last stopped at, or nil.
func (t *Tracker) StoppedAt(vehicle string, id int) *Point {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.srcs {
		if c := s.cars[vehicle+"|"+strconv.Itoa(id)]; c != nil && c.box != nil {
			p := *c.box
			return &p
		}
	}
	return nil
}
