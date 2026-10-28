package ingest

import (
	"strconv"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// minCondSamples is the number of telemetry samples a lap needs to produce a record.
const minCondSamples = 3

// sessionFlush is how often the sign of life of a session is written.
const sessionFlush = 20 * time.Second

// condAcc sums the telemetry of the lap in progress of one car.
type condAcc struct {
	lap                           int
	n                             int
	surface, inner, pressure, brk [4]float64
	nSurface, nInner, nPressure   int
	nBrake                        int
	water, oil                    float64
	nWater, nOil                  int
	frontCompound, rearCompound   string
}

func (a *condAcc) add(t wire.Telemetry) {
	a.n++
	if len(t.TyreSurfaceTemp) == 4 {
		a.nSurface++
		for i, row := range t.TyreSurfaceTemp {
			a.surface[i] += mean(row)
		}
	}
	if len(t.TyreInnerTemp) == 4 {
		a.nInner++
		for i, row := range t.TyreInnerTemp {
			a.inner[i] += mean(row)
		}
	}
	if len(t.TyrePressure) == 4 {
		a.nPressure++
		for i, v := range t.TyrePressure {
			a.pressure[i] += v
		}
	}
	if len(t.BrakeTemp) == 4 {
		a.nBrake++
		for i, v := range t.BrakeTemp {
			a.brk[i] += v
		}
	}
	if t.EngineWaterTemp != nil {
		a.nWater++
		a.water += *t.EngineWaterTemp
	}
	if t.EngineOilTemp != nil {
		a.nOil++
		a.oil += *t.EngineOilTemp
	}
	if t.CompoundFront != "" {
		a.frontCompound = t.CompoundFront
	}
	if t.CompoundRear != "" {
		a.rearCompound = t.CompoundRear
	}
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func avg(sum [4]float64, n int) []float64 {
	if n == 0 {
		return nil
	}
	out := make([]float64, 4)
	for i := range out {
		out[i] = sum[i] / float64(n)
	}
	return out
}

// conditionLog turns the telemetry of the driven car into one record per completed lap.
type conditionLog struct {
	mu   sync.Mutex
	cars map[string]*condAcc
}

// observe adds a telemetry sample. It returns the record of the lap that this sample just completed, or nil.
// The plugin counts the lap in progress from 0, the stored laps by the number of completed laps.
func (l *conditionLog) observe(session string, et float64, t wire.Telemetry, sc *wire.Scoring) *store.LapConditions {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cars == nil {
		l.cars = map[string]*condAcc{}
	}
	key := session + "|" + strconv.Itoa(t.CarID) + "|" + t.VehicleName
	a := l.cars[key]
	if a == nil {
		a = &condAcc{lap: t.Lap}
		l.cars[key] = a
	}
	var out *store.LapConditions
	if t.Lap != a.lap {
		if t.Lap == a.lap+1 && a.n >= minCondSamples {
			out = a.record(session, et, t, sc)
		}
		*a = condAcc{lap: t.Lap}
	}
	a.add(t)
	return out
}

func (a *condAcc) record(session string, et float64, t wire.Telemetry, sc *wire.Scoring) *store.LapConditions {
	c := &store.LapConditions{SessionID: session, CarID: t.CarID, VehicleName: t.VehicleName, Lap: a.lap + 1, ET: et,
		Surface: avg(a.surface, a.nSurface), Inner: avg(a.inner, a.nInner), Pressure: avg(a.pressure, a.nPressure),
		Brake: avg(a.brk, a.nBrake), CompoundFront: a.frontCompound, CompoundRear: a.rearCompound}
	if a.nWater > 0 {
		v := a.water / float64(a.nWater)
		c.Water = &v
	}
	if a.nOil > 0 {
		v := a.oil / float64(a.nOil)
		c.Oil = &v
	}
	if sc != nil {
		if w := sc.Weather; w != nil {
			c.Ambient, c.Track, c.Rain, c.Wetness = w.AmbientTemp, w.TrackTemp, w.Rain, w.Wetness
		}
		for _, v := range sc.Vehicles {
			if v.CarID == t.CarID && v.VehicleName == t.VehicleName {
				c.Control = v.Control
				break
			}
		}
	}
	return c
}

// sessionLog writes the sign of life of the running session now and then.
type sessionLog struct {
	mu      sync.Mutex
	info    store.SessionInfo
	flushed time.Time
	dirty   bool
}

// update merges what a message tells about the session and returns the record to write, or nil when it is not due.
func (s *sessionLog) update(id string, now time.Time, et float64, track, sessionType, car string, carID int) *store.SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.SessionID != id {
		s.info, s.flushed = store.SessionInfo{SessionID: id, CarID: -1}, time.Time{}
	}
	set := func(dst *string, v string) {
		if v != "" && *dst != v {
			*dst, s.dirty = v, true
		}
	}
	set(&s.info.Track, track)
	set(&s.info.SessionType, sessionType)
	set(&s.info.Car, car)
	if carID >= 0 && carID != s.info.CarID {
		s.info.CarID, s.dirty = carID, true
	}
	s.info.LastET, s.info.LastSeen = et, now
	if !s.flushed.IsZero() && !s.dirty && now.Sub(s.flushed) < sessionFlush {
		return nil
	}
	s.flushed, s.dirty = now, false
	out := s.info
	return &out
}
