package engine

import (
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	// rivalPaceLaps is the number of clean laps averaged for a rival's pace.
	rivalPaceLaps = 5
	// rivalRefresh is how long the lap statistics of the field are reused; they come from all stored laps.
	rivalRefresh = 10 * time.Second
	// fuelSamples is the number of laps the fuel use of a rival is judged from.
	fuelSamples = 6
	// maxRivals bounds the list in the state: the cars closest in place.
	maxRivals = 16
	// stopSoonLaps: a rival that stops within this many laps is not counted as passing us when we stop.
	stopSoonLaps = 2
	// windowLaps is how many laps ahead the best moment to stop is searched.
	windowLaps = 15
	// outlierFactor drops laps slower than this multiple of the median lap time from a pace.
	outlierFactor = 1.12
)

// Rival is another car with what we can tell about its race. Gap is the time in seconds the rival is ahead of us,
// negative when it is behind. Rate is the time in seconds per lap that we gain on the rival: positive when it is slower.
type Rival struct {
	CarID        int       `json:"car_id"`
	VehicleName  string    `json:"vehicle_name"`
	Driver       string    `json:"driver"`
	Place        int       `json:"place"`
	Control      *int      `json:"control,omitempty"`
	InPits       bool      `json:"in_pits"`
	Gap          *float64  `json:"gap"`
	Pace         *float64  `json:"pace"`
	Rate         *float64  `json:"rate"`
	Stops        int       `json:"stops"`
	StintLaps    int       `json:"stint_laps"`
	Fuel         *float64  `json:"fuel"`
	FuelLapsLeft *float64  `json:"fuel_laps_left"`
	Wear         []float64 `json:"wear"`
	// StopIn is the forecast number of laps until the rival stops. StopBasis says what it rests on: fuel (its own use),
	// assumed (our consumption applied to its tank) or stint (the length of its earlier stints).
	StopIn    *float64 `json:"stop_in"`
	StopBasis string   `json:"stop_basis,omitempty"`
}

// WindowPoint is the outcome of stopping InLaps laps from now: how many cars that are behind now would be ahead after the stop.
type WindowPoint struct {
	InLaps int `json:"in_laps"`
	Lost   int `json:"lost"`
}

// Rejoin is what happens to our position when we stop, with the stop time estimated from the parameters.
type Rejoin struct {
	// Loss is the time in seconds the stop costs.
	Loss float64 `json:"loss"`
	// Position is the place we would have after stopping now, Lost the number of places lost.
	Position int `json:"position"`
	Lost     int `json:"lost"`
	// PassedBy are the cars that would be ahead of us after stopping now; AlsoStop are cars behind us that stop soon anyway.
	PassedBy []string `json:"passed_by"`
	AlsoStop []string `json:"also_stop"`
	// Window lists the outcome of stopping later; BestIn is the number of laps with the fewest places lost.
	Window []WindowPoint `json:"window"`
	BestIn int           `json:"best_in"`
	// Incomplete is true when pit parameters are missing and Loss is only part of the real time.
	Incomplete bool `json:"incomplete"`
}

// Strategy is the race against the other cars.
type Strategy struct {
	Rivals []Rival `json:"rivals"`
	Rejoin *Rejoin `json:"rejoin,omitempty"`
}

// carLaps summarises the stored laps of one car.
type carLaps struct {
	pace      float64
	stintLaps int
	stints    []int // laps of earlier, complete stints
}

// rivalLog keeps what is needed between two computations of the state: the lap statistics of the field, refreshed
// now and then, and the fuel use of the cars, seen from the fuel level at the end of every lap.
type rivalLog struct {
	mu      sync.Mutex
	session string
	at      time.Time
	laps    map[string]carLaps
	fuel    map[string]*fuelWatch
}

type fuelWatch struct {
	laps int
	fuel float64
	used []float64
}

func carKey(vehicle string, id int) string { return vehicle + "|" + strconv.Itoa(id) }

// reset forgets everything when the session changes.
func (r *rivalLog) reset(session string) {
	if r.session != session {
		r.session, r.at, r.laps, r.fuel = session, time.Time{}, nil, map[string]*fuelWatch{}
	}
}

// watchFuel notes the fuel of every car whenever it has completed a lap.
func (r *rivalLog) watchFuel(vs []wire.ScoringVehicle) {
	for _, v := range vs {
		if v.Fuel == nil {
			continue
		}
		k := carKey(v.VehicleName, v.CarID)
		w := r.fuel[k]
		if w == nil {
			r.fuel[k] = &fuelWatch{laps: v.Laps, fuel: *v.Fuel}
			continue
		}
		if v.Laps == w.laps {
			if *v.Fuel > w.fuel { // refuelled: the next lap starts from this level
				w.fuel = *v.Fuel
			}
			continue
		}
		if v.Laps == w.laps+1 {
			if used := w.fuel - *v.Fuel; used > 0.05 && used < 30 {
				w.used = append(w.used, used)
				if len(w.used) > fuelSamples {
					w.used = w.used[len(w.used)-fuelSamples:]
				}
			}
		}
		w.laps, w.fuel = v.Laps, *v.Fuel
	}
}

// perLap is the median fuel use per lap of a car.
func (r *rivalLog) perLap(k string) (float64, bool) {
	w := r.fuel[k]
	if w == nil || len(w.used) < 2 {
		return 0, false
	}
	m := median(w.used)
	return *m, true
}

// summarizeField turns the stored laps of all cars into pace and stint lengths.
func summarizeField(laps []store.LapRecord) map[string]carLaps {
	by := map[string][]store.LapRecord{}
	for _, l := range laps {
		k := carKey(l.VehicleName, l.CarID)
		by[k] = append(by[k], l)
	}
	out := make(map[string]carLaps, len(by))
	for k, ls := range by {
		sort.Slice(ls, func(i, j int) bool { return ls[i].Lap.Lap < ls[j].Lap.Lap })
		var c carLaps
		var times []float64
		stint := 0
		for _, l := range ls {
			if l.OutLap && stint > 0 {
				c.stints = append(c.stints, stint)
				stint = 0
			}
			stint++
			if l.LapTime != nil && !l.InLap && !l.OutLap && !l.FCY {
				times = append(times, *l.LapTime)
			}
		}
		c.stintLaps = stint
		c.pace = paceOf(times)
		out[k] = c
	}
	return out
}

// paceOf averages the last clean lap times without the ones that are much slower than the typical lap.
func paceOf(times []float64) float64 {
	if len(times) == 0 {
		return 0
	}
	if len(times) > 3*rivalPaceLaps {
		times = times[len(times)-3*rivalPaceLaps:]
	}
	med := *median(times)
	var good []float64
	for _, t := range times {
		if t <= med*outlierFactor {
			good = append(good, t)
		}
	}
	if len(good) > rivalPaceLaps {
		good = good[len(good)-rivalPaceLaps:]
	}
	var sum float64
	for _, t := range good {
		sum += t
	}
	return sum / float64(len(good))
}

// serviceTime is how long our next stop takes in the pit box.
func serviceTime(p store.Params, fuelNeeded float64, swap bool) float64 {
	var refuel float64
	if p.RefuelRate > 0 && fuelNeeded > 0 {
		refuel = fuelNeeded / p.RefuelRate
	}
	other := p.TyreChange
	if swap {
		other += p.DriverSwap
	}
	if p.ParallelService {
		return math.Max(refuel, other)
	}
	return refuel + other
}

// strategyInput is what the rivals analysis needs from the state computation.
type strategyInput struct {
	sc      *wire.Scoring
	car     Car
	params  store.Params
	lapTime float64
	fcy     bool
	swap    bool // the next stop changes the driver
	stats   map[string]carLaps
}

// analyse lists the rivals and works out where we would rejoin.
func (r *rivalLog) analyse(in strategyInput) *Strategy {
	sc := in.sc
	vs := append([]wire.ScoringVehicle(nil), sc.Vehicles...)
	sort.Slice(vs, func(i, j int) bool { return vs[i].Place < vs[j].Place })
	me := -1
	for i, v := range vs {
		if v.CarID == in.car.CarID && v.VehicleName == in.car.VehicleName {
			me = i
		}
	}
	if me < 0 {
		return nil
	}
	var ourFPL float64
	if in.car.FuelPerLap != nil {
		ourFPL = *in.car.FuelPerLap
	}
	reserve := in.params.Reserve
	var rivals []Rival
	for i, v := range vs {
		if i == me {
			continue
		}
		k := carKey(v.VehicleName, v.CarID)
		rv := Rival{CarID: v.CarID, VehicleName: v.VehicleName, Driver: v.Driver, Place: v.Place, Control: v.Control,
			InPits: v.InPits, Stops: v.NumPitstops, Fuel: v.Fuel, Wear: v.Wear}
		rv.Gap = gapTo(vs, me, i)
		st := in.stats[k]
		rv.StintLaps = st.stintLaps
		if st.pace > 0 {
			p := st.pace
			rv.Pace = &p
			if in.lapTime > 0 {
				rate := p - in.lapTime
				rv.Rate = &rate
			}
		}
		switch {
		case v.Fuel != nil:
			if fpl, ok := r.perLap(k); ok {
				left := math.Max(*v.Fuel-reserve, 0) / fpl
				rv.FuelLapsLeft, rv.StopIn, rv.StopBasis = &left, &left, "fuel"
			} else if ourFPL > 0 {
				left := math.Max(*v.Fuel-reserve, 0) / ourFPL
				rv.FuelLapsLeft, rv.StopIn, rv.StopBasis = &left, &left, "assumed"
			}
		}
		if rv.StopIn == nil && len(st.stints) > 0 {
			typical := float64(*median(intsToFloats(st.stints)))
			left := math.Max(typical-float64(st.stintLaps), 0)
			rv.StopIn, rv.StopBasis = &left, "stint"
		}
		rivals = append(rivals, rv)
	}
	out := &Strategy{Rivals: nearest(rivals, in.car.Place)}
	out.Rejoin = rejoin(in, rivals)
	return out
}

func intsToFloats(v []int) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}

// gapTo is the time the car at index j is ahead of the car at index me (negative when behind), summing the
// time-behind-next values of the cars between them; nil when one of them is unknown.
func gapTo(vs []wire.ScoringVehicle, me, j int) *float64 {
	var sum float64
	if j < me {
		for k := j + 1; k <= me; k++ {
			if vs[k].TimeBehindNext == nil {
				return nil
			}
			sum += *vs[k].TimeBehindNext
		}
	} else {
		for k := me + 1; k <= j; k++ {
			if vs[k].TimeBehindNext == nil {
				return nil
			}
			sum -= *vs[k].TimeBehindNext
		}
	}
	return &sum
}

// nearest keeps the cars closest to our place.
func nearest(rs []Rival, place int) []Rival {
	if len(rs) <= maxRivals {
		return rs
	}
	sort.Slice(rs, func(i, j int) bool {
		return math.Abs(float64(rs[i].Place-place)) < math.Abs(float64(rs[j].Place-place))
	})
	rs = rs[:maxRivals]
	sort.Slice(rs, func(i, j int) bool { return rs[i].Place < rs[j].Place })
	return rs
}

// rejoin works out the places lost by a stop now and by a stop in the next laps.
func rejoin(in strategyInput, rivals []Rival) *Rejoin {
	p := in.params
	through := p.PitThroughGreen
	if in.fcy && p.PitThroughFCY > 0 {
		through = p.PitThroughFCY
	}
	need := math.Max(in.car.FuelCapacity-in.car.Fuel, 0)
	loss := through + serviceTime(p, need, in.swap)
	if loss <= 0 {
		return nil
	}
	rj := &Rejoin{Loss: loss, Incomplete: through <= 0 || (p.RefuelRate <= 0 && need > 0)}
	maxIn := windowLaps
	for in0 := 0; in0 <= maxIn; in0++ {
		lost, passed, also := lostAt(in, rivals, loss, float64(in0))
		if in0 == 0 {
			rj.Lost, rj.Position, rj.PassedBy, rj.AlsoStop = lost, in.car.Place+lost, passed, also
		}
		rj.Window = append(rj.Window, WindowPoint{InLaps: in0, Lost: lost})
	}
	best := 0
	for _, w := range rj.Window {
		if w.Lost < rj.Window[best].Lost {
			best = w.InLaps
		}
	}
	rj.BestIn = best
	return rj
}

// lostAt counts the cars behind us now that would be ahead after we stop in k laps, assuming constant paces and
// that every rival that stops before then loses the same time we do.
func lostAt(in strategyInput, rivals []Rival, loss, k float64) (int, []string, []string) {
	var passed, also []string
	for _, r := range rivals {
		if r.Gap == nil || *r.Gap >= 0 || r.InPits {
			continue
		}
		behind := -*r.Gap
		if r.Rate != nil {
			behind += k * *r.Rate // a slower rival (rate > 0) falls further back
		}
		name := r.Driver
		if name == "" {
			name = r.VehicleName
		}
		if r.StopIn != nil && *r.StopIn <= k {
			continue // it stops before us and drops back by its own stop
		}
		if r.StopIn != nil && *r.StopIn <= k+stopSoonLaps {
			if behind < loss {
				also = append(also, name)
			}
			continue
		}
		if behind < loss {
			passed = append(passed, name)
		}
	}
	return len(passed), passed, also
}
