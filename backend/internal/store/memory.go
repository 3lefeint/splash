package store

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wear"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// Memory is an in-memory Store for tests and local runs without a database.
type Memory struct {
	mu       sync.Mutex
	laps     map[lapKey]wire.Lap
	stops    []StopRecord
	progress map[string]int64

	params    map[string]Params
	plan      []PlanRow
	overrides map[string]Override
	maps      map[string]trackmap.Map
	corners   map[string][]Corner
	setups    []PitSetup
	wears     map[lapKey]wear.LapWear
	sessions  map[string]SessionInfo
	conds     map[lapKey]LapConditions
	weather   map[string][]WeatherPoint
	lanes     map[string]pitlane.Lane
}

type lapKey struct {
	session string
	car     int
	vehicle string
	lap     int
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		laps:      map[lapKey]wire.Lap{},
		sessions:  map[string]SessionInfo{},
		conds:     map[lapKey]LapConditions{},
		weather:   map[string][]WeatherPoint{},
		lanes:     map[string]pitlane.Lane{},
		progress:  map[string]int64{},
		params:    map[string]Params{},
		overrides: map[string]Override{},
		maps:      map[string]trackmap.Map{},
		corners:   map[string][]Corner{},
		wears:     map[lapKey]wear.LapWear{},
	}
}

func (m *Memory) SaveLap(_ context.Context, sessionID string, l wire.Lap) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := lapKey{sessionID, l.CarID, l.VehicleName, l.Lap}
	if old, ok := m.laps[k]; ok {
		l = mergeLap(old, l)
	}
	m.laps[k] = l
	return nil
}

func (m *Memory) SaveEvent(_ context.Context, sessionID string, et float64, e wire.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch e.Kind {
	case wire.EventPitEntry:
		for _, s := range m.stops {
			if s.SessionID == sessionID && s.CarID == e.CarID && s.VehicleName == e.VehicleName && math.Abs(s.EntryET-et) <= PitEntryWindow {
				return nil
			}
		}
		m.stops = append(m.stops, StopRecord{SessionID: sessionID, CarID: e.CarID, VehicleName: e.VehicleName, EntryET: et})
	case wire.EventPitExit:
		if i := m.latest(sessionID, e, et, true); i >= 0 {
			s := &m.stops[i]
			s.ExitET = &et
			if e.FuelAdded != nil {
				s.FuelAdded = e.FuelAdded
			}
			s.TyresChanged = s.TyresChanged || (e.TyresChanged != nil && *e.TyresChanged)
		}
	case wire.EventDriverSwap:
		if i := m.latest(sessionID, e, et, false); i >= 0 {
			m.stops[i].DriverSwap = true
		}
	}
	return nil
}

// latest finds the newest stop of the car that started at or before et.
// openOnly restricts it to stops without an exit.
func (m *Memory) latest(sessionID string, e wire.Event, et float64, openOnly bool) int {
	best := -1
	for i, s := range m.stops {
		if s.SessionID != sessionID || s.CarID != e.CarID || s.VehicleName != e.VehicleName || s.EntryET > et {
			continue
		}
		if openOnly && s.ExitET != nil {
			continue
		}
		if best < 0 || s.EntryET > m.stops[best].EntryET {
			best = i
		}
	}
	return best
}

func (m *Memory) Progress(_ context.Context, agentID, sessionID string) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.progress[agentID+"|"+sessionID]
	return v, ok, nil
}

func (m *Memory) SetProgress(_ context.Context, agentID, sessionID string, seq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := agentID + "|" + sessionID
	if old, ok := m.progress[k]; !ok || seq > old {
		m.progress[k] = seq
	}
	return nil
}

func (m *Memory) Laps(_ context.Context, sessionID string) ([]LapRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []LapRecord
	for k, l := range m.laps {
		if k.session == sessionID {
			out = append(out, LapRecord{sessionID, l})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.CarID != b.CarID {
			return a.CarID < b.CarID
		}
		return a.Lap.Lap < b.Lap.Lap
	})
	return out, nil
}

func (m *Memory) Stops(_ context.Context, sessionID string) ([]StopRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []StopRecord
	for _, s := range m.stops {
		if s.SessionID == sessionID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EntryET < out[j].EntryET })
	return out, nil
}

func (m *Memory) LapsOfCar(_ context.Context, sessionID string, carID int, vehicleName string) ([]LapRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []LapRecord
	for k, l := range m.laps {
		if k.session == sessionID && k.car == carID && k.vehicle == vehicleName {
			out = append(out, LapRecord{sessionID, l})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lap.Lap < out[j].Lap.Lap })
	return out, nil
}

func (m *Memory) GetParams(_ context.Context, track, car string) (Params, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.params[track+"|"+car]
	return p, ok, nil
}

func (m *Memory) ListParams(_ context.Context) ([]Params, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Params, 0, len(m.params))
	for _, p := range m.params {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Track != out[j].Track {
			return out[i].Track < out[j].Track
		}
		return out[i].Car < out[j].Car
	})
	return out, nil
}

func (m *Memory) SetParams(_ context.Context, p Params) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.params[p.Track+"|"+p.Car] = p
	return nil
}

func (m *Memory) GetStintPlan(_ context.Context) ([]PlanRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PlanRow(nil), m.plan...), nil
}

func (m *Memory) SetStintPlan(_ context.Context, rows []PlanRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plan = append([]PlanRow(nil), rows...)
	return nil
}

func (m *Memory) GetOverrides(_ context.Context) (map[string]Override, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Override, len(m.overrides))
	for k, v := range m.overrides {
		out[k] = v
	}
	return out, nil
}

func (m *Memory) SetOverride(_ context.Context, key string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrides[key] = Override{Key: key, Value: value, SetAt: time.Now().UTC()}
	return nil
}

func (m *Memory) DeleteOverride(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.overrides, key)
	return nil
}

func (m *Memory) GetTrackMap(_ context.Context, track string) (trackmap.Map, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tm, ok := m.maps[track]
	return tm, ok, nil
}

func (m *Memory) SaveTrackMap(_ context.Context, tm trackmap.Map) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maps[tm.Track] = tm
	return nil
}

func (m *Memory) DeleteTrackMap(_ context.Context, track string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.maps, track)
	return nil
}

func (m *Memory) ListTracks(_ context.Context) ([]TrackInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TrackInfo, 0, len(m.maps))
	for _, tm := range m.maps {
		out = append(out, TrackInfo{tm.Track, tm.Length})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Track < out[j].Track })
	return out, nil
}

func (m *Memory) GetCorners(_ context.Context, track string) ([]Corner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Corner{}, m.corners[track]...), nil
}

func (m *Memory) SaveCorners(_ context.Context, track string, corners []Corner) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.corners[track] = append([]Corner{}, corners...)
	return nil
}

func (m *Memory) SaveLapWear(_ context.Context, sessionID string, carID int, vehicleName string, w wear.LapWear) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wears[lapKey{sessionID, carID, vehicleName, w.Lap}] = w
	return nil
}

func (m *Memory) LapWears(_ context.Context, sessionID string, carID int, vehicleName string) ([]wear.LapWear, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []wear.LapWear
	for k, w := range m.wears {
		if k.session == sessionID && k.car == carID && k.vehicle == vehicleName {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lap < out[j].Lap })
	return out, nil
}

func (m *Memory) AddStopped(_ context.Context, sessionID string, carID int, vehicleName string, st Standing) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	best := -1
	for i, s := range m.stops {
		if s.SessionID != sessionID || s.CarID != carID || s.VehicleName != vehicleName || s.EntryET > st.From {
			continue
		}
		if best < 0 || s.EntryET > m.stops[best].EntryET {
			best = i
		}
	}
	if best >= 0 && st.To > st.From {
		m.stops[best].Stopped += st.To - st.From
		m.stops[best].Work += st.Work
		m.stops[best].FuelTime += st.FuelTime
	}
	return nil
}

func (m *Memory) PitSetups(_ context.Context, car string) ([]PitSetup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []PitSetup{}
	for _, s := range m.setups {
		if s.Car == car {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *Memory) SavePitSetup(_ context.Context, s PitSetup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, old := range m.setups {
		if old.Car == s.Car && old.Name == s.Name {
			m.setups[i] = s
			return nil
		}
	}
	m.setups = append(m.setups, s)
	return nil
}

func (m *Memory) DeletePitSetup(_ context.Context, car, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, old := range m.setups {
		if old.Car == car && old.Name == name {
			m.setups = append(m.setups[:i], m.setups[i+1:]...)
			break
		}
	}
	return nil
}

func (m *Memory) TouchSession(_ context.Context, s SessionInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.sessions[s.SessionID]
	if !ok {
		cur = SessionInfo{SessionID: s.SessionID, StartedAt: s.LastSeen, CarID: -1}
	}
	cur.LastSeen, cur.LastET = s.LastSeen, s.LastET
	if s.CarID >= 0 {
		cur.CarID = s.CarID
	}
	for dst, v := range map[*string]string{&cur.Track: s.Track, &cur.SessionType: s.SessionType, &cur.Car: s.Car} {
		if v != "" {
			*dst = v
		}
	}
	m.sessions[s.SessionID] = cur
	return nil
}

func (m *Memory) Sessions(_ context.Context) ([]SessionSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	by := map[string]*SessionSummary{}
	for id, s := range m.sessions {
		by[id] = &SessionSummary{SessionInfo: s}
	}
	get := func(id string) *SessionSummary {
		if by[id] == nil {
			by[id] = &SessionSummary{SessionInfo: SessionInfo{SessionID: id, CarID: -1}}
		}
		return by[id]
	}
	for k := range m.laps {
		if s := get(k.session); k.vehicle == s.Car && (s.CarID < 0 || k.car == s.CarID) {
			s.Laps++
		}
	}
	for _, st := range m.stops {
		if s := get(st.SessionID); st.VehicleName == s.Car && (s.CarID < 0 || st.CarID == s.CarID) {
			s.Stops++
		}
	}
	for _, c := range m.conds {
		s := get(c.SessionID)
		s.Conditions++
		if c.Control != nil && *c.Control == 1 {
			s.AILaps++
		}
		if c.Control != nil && *c.Control == 0 {
			s.PlayerLaps++
		}
	}
	out := make([]SessionSummary, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

func (m *Memory) SaveLapConditions(_ context.Context, c LapConditions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conds[lapKey{c.SessionID, c.CarID, c.VehicleName, c.Lap}] = c
	return nil
}

func (m *Memory) LapConditions(_ context.Context, sessionID string) ([]LapConditions, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []LapConditions
	for k, c := range m.conds {
		if k.session == sessionID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CarID != out[j].CarID {
			return out[i].CarID < out[j].CarID
		}
		return out[i].Lap < out[j].Lap
	})
	return out, nil
}

func (m *Memory) SaveWeatherPoint(_ context.Context, sessionID string, p WeatherPoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pts := m.weather[sessionID]
	for i := range pts {
		if pts[i].ET == p.ET {
			pts[i] = p
			return nil
		}
	}
	m.weather[sessionID] = append(pts, p)
	return nil
}

func (m *Memory) WeatherPoints(_ context.Context, sessionID string, limit int) ([]WeatherPoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pts := append([]WeatherPoint(nil), m.weather[sessionID]...)
	sort.Slice(pts, func(i, j int) bool { return pts[i].ET < pts[j].ET })
	if limit > 0 && len(pts) > limit {
		pts = pts[len(pts)-limit:]
	}
	return pts, nil
}

func (m *Memory) GetPitLane(_ context.Context, track string) (pitlane.Lane, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.lanes[track]
	return l, ok, nil
}

func (m *Memory) SavePitLane(_ context.Context, l pitlane.Lane) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lanes[l.Track] = l
	return nil
}
