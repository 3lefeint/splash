package store

import (
	"bytes"
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

// Memory is an in-memory Store for tests and local runs without a database. NewMemory returns the root, which holds
// the data shared by all teams (track data, teams, agents); ForTeam returns the Store of one team. Like the Postgres
// root, the root itself is a team of its own with id 0 that no agent or request ever maps to.
type Memory struct {
	*memTeam
	*memShared
	team int64
}

// memShared is what all teams have in common.
type memShared struct {
	smu     sync.Mutex
	maps    map[string]trackmap.Map
	corners map[string][]Corner
	lanes   map[string]pitlane.Lane

	teams   map[int64]Team
	agents  map[int64]*memAgent
	members map[string]Member
	audit   []AuditEntry
	views   map[int64]*Memory
}

type memAgent struct {
	Agent
	hash []byte
}

// memTeam is the data of one team.
type memTeam struct {
	mu       sync.Mutex
	laps     map[lapKey]wire.Lap
	lapAgent map[lapKey]int64 // agent that delivered the lap's fuel figures, else the first one
	lapFlag  map[lapKey]bool  // own exclusion flag of a lap; absent follows the session
	sessOut  map[string]bool  // sessions left out of the models
	stops    []StopRecord
	progress map[string]int64

	params    map[string]Params
	plan      []PlanRow
	overrides map[overrideKey]Override
	setups    []PitSetup
	wears     map[lapKey]wear.LapWear
	sessions  map[string]SessionInfo
	cars      map[int64]*TeamCar
	carNames  map[string]int64 // vehicle name -> team car id
	nextCar   int64
	conds     map[lapKey]LapConditions
	weather   map[string][]WeatherPoint
}

type lapKey struct {
	session string
	car     int
	vehicle string
	lap     int
}

func newMemTeam() *memTeam {
	return &memTeam{
		laps:      map[lapKey]wire.Lap{},
		lapAgent:  map[lapKey]int64{},
		lapFlag:   map[lapKey]bool{},
		sessOut:   map[string]bool{},
		sessions:  map[string]SessionInfo{},
		cars:      map[int64]*TeamCar{},
		carNames:  map[string]int64{},
		conds:     map[lapKey]LapConditions{},
		weather:   map[string][]WeatherPoint{},
		progress:  map[string]int64{},
		params:    map[string]Params{},
		overrides: map[overrideKey]Override{},
		wears:     map[lapKey]wear.LapWear{},
	}
}

// DefaultTeamName is the team that holds the data that existed before teams were introduced.
const DefaultTeamName = "default"

// NewMemory returns an empty in-memory store with the team "default" (id 1, as the migration creates it).
func NewMemory() *Memory {
	sh := &memShared{
		maps:    map[string]trackmap.Map{},
		corners: map[string][]Corner{},
		lanes:   map[string]pitlane.Lane{},
		teams:   map[int64]Team{1: {1, DefaultTeamName}},
		agents:  map[int64]*memAgent{},
		members: map[string]Member{},
		views:   map[int64]*Memory{},
	}
	root := &Memory{newMemTeam(), sh, 0}
	sh.views[0] = root
	return root
}

// ForTeam returns the Store of one team; the same id always gives the same data.
func (m *Memory) ForTeam(id int64) Store {
	m.smu.Lock()
	defer m.smu.Unlock()
	v, ok := m.views[id]
	if !ok {
		v = &Memory{newMemTeam(), m.memShared, id}
		m.views[id] = v
	}
	return v
}

func (m *Memory) SaveLap(_ context.Context, sessionID string, l wire.Lap, agentID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := lapKey{sessionID, l.CarID, l.VehicleName, l.Lap}
	hasFuel := l.FuelStart != nil || l.FuelEnd != nil
	if old, ok := m.laps[k]; ok {
		l = mergeLap(old, l)
	}
	m.laps[k] = l
	if agentID != 0 && (m.lapAgent[k] == 0 || hasFuel) {
		m.lapAgent[k] = agentID
	}
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

// record is a stored lap with its agent and exclusion; the caller holds the lock.
func (m *Memory) record(sessionID string, k lapKey, l wire.Lap) LapRecord {
	rec := LapRecord{SessionID: sessionID, AgentID: m.lapAgent[k], Lap: l, Excluded: m.sessOut[sessionID]}
	if v, ok := m.lapFlag[k]; ok {
		rec.ExcludedFlag, rec.Excluded = &v, v
	}
	return rec
}

func (m *Memory) Laps(_ context.Context, sessionID string) ([]LapRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []LapRecord
	for k, l := range m.laps {
		if k.session == sessionID {
			out = append(out, m.record(sessionID, k, l))
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
			out = append(out, m.record(sessionID, k, l))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lap.Lap < out[j].Lap.Lap })
	return out, nil
}

func (m *Memory) GetParams(_ context.Context, track, car string) (Params, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	canon := m.canonical(car)
	if p, ok := m.params[track+"|"+canon]; ok {
		return p, true, nil
	}
	p, ok := m.params[track+"|"+car] // a set stored before the car had a team car
	return p, ok, nil
}

// canonical is CanonicalCar for a caller that holds the lock.
func (m *Memory) canonical(vehicleName string) string {
	if id, ok := m.carNames[vehicleName]; ok {
		return m.cars[id].Name
	}
	return vehicleName
}

func (m *Memory) CanonicalCar(_ context.Context, vehicleName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.canonical(vehicleName), nil
}

func (m *Memory) Cars(_ context.Context) ([]TeamCar, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TeamCar, 0, len(m.cars))
	for _, c := range m.cars {
		car := TeamCar{ID: c.ID, Name: c.Name, Names: []string{}}
		for name, id := range m.carNames {
			if id == c.ID {
				car.Names = append(car.Names, name)
			}
		}
		sort.Strings(car.Names)
		out = append(out, car)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) CreateCar(_ context.Context, name string) (TeamCar, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cars {
		if c.Name == name {
			return TeamCar{}, ErrExists
		}
	}
	m.nextCar++
	c := &TeamCar{ID: m.nextCar, Name: name, Names: []string{}}
	m.cars[c.ID] = c
	return *c, nil
}

func (m *Memory) HistoryLaps(_ context.Context, track, vehicleName, exceptSession string) ([]HistoryLap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// the vehicle names of the team car the vehicle name belongs to, and the name itself
	names := map[string]bool{vehicleName: true}
	if id, ok := m.carNames[vehicleName]; ok {
		for n, cid := range m.carNames {
			if cid == id {
				names[n] = true
			}
		}
	}
	var out []HistoryLap
	for k, l := range m.laps {
		sess, ok := m.sessions[k.session]
		if !ok || sess.Track != track || k.session == exceptSession || !names[k.vehicle] || l.FuelStart == nil || l.FuelEnd == nil {
			continue
		}
		rec := m.record(k.session, k, l)
		if rec.Excluded {
			continue
		}
		h := HistoryLap{LapRecord: rec, StartedAt: sess.StartedAt}
		if c, ok := m.conds[k]; ok {
			h.Rain, h.Wetness = c.Rain, c.Wetness
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case !a.StartedAt.Equal(b.StartedAt):
			return a.StartedAt.Before(b.StartedAt)
		case a.SessionID != b.SessionID:
			return a.SessionID < b.SessionID
		case a.CarID != b.CarID:
			return a.CarID < b.CarID
		}
		return a.Lap.Lap < b.Lap.Lap
	})
	return out, nil
}

func (m *Memory) SetLapExcluded(_ context.Context, sessionID string, carID int, vehicleName string, lap int, excluded *bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := lapKey{sessionID, carID, vehicleName, lap}
	if _, ok := m.laps[k]; !ok {
		return ErrNoLap
	}
	if excluded == nil {
		delete(m.lapFlag, k)
	} else {
		m.lapFlag[k] = *excluded
	}
	return nil
}

func (m *Memory) SetSessionExcluded(_ context.Context, sessionID string, excluded bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return ErrNoSession
	}
	m.sessOut[sessionID] = excluded
	return nil
}

func (m *Memory) DeleteCar(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cars[id]; !ok {
		return ErrNoCar
	}
	delete(m.cars, id)
	for name, cid := range m.carNames {
		if cid == id {
			delete(m.carNames, name)
		}
	}
	return nil
}

func (m *Memory) MapCarName(_ context.Context, id int64, vehicleName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cars[id]; !ok {
		return ErrNoCar
	}
	m.carNames[vehicleName] = id
	return nil
}

func (m *Memory) UnmapCarName(_ context.Context, vehicleName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.carNames, vehicleName)
	return nil
}

func (m *Memory) UnmappedCarNames(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	out := []string{}
	for _, s := range m.sessions {
		if _, mapped := m.carNames[s.Car]; s.Car != "" && !mapped && !seen[s.Car] {
			seen[s.Car] = true
			out = append(out, s.Car)
		}
	}
	sort.Strings(out)
	return out, nil
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
	p.Car = m.canonical(p.Car)
	m.params[p.Track+"|"+p.Car] = p.WithDefaults()
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

type overrideKey struct {
	agent int64
	key   string
}

func (m *Memory) GetOverrides(_ context.Context, agentID int64) (map[string]Override, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]Override{}
	for k, v := range m.overrides {
		if k.agent == agentID {
			out[k.key] = v
		}
	}
	return out, nil
}

func (m *Memory) SetOverride(_ context.Context, agentID int64, key string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrides[overrideKey{agentID, key}] = Override{Key: key, Value: value, SetAt: time.Now().UTC()}
	return nil
}

func (m *Memory) DeleteOverride(_ context.Context, agentID int64, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.overrides, overrideKey{agentID, key})
	return nil
}

func (m *Memory) GetTrackMap(_ context.Context, track string) (trackmap.Map, bool, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	tm, ok := m.maps[track]
	return tm, ok, nil
}

func (m *Memory) SaveTrackMap(_ context.Context, tm trackmap.Map) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	m.maps[tm.Track] = tm
	return nil
}

func (m *Memory) DeleteTrackMap(_ context.Context, track string) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	delete(m.maps, track)
	return nil
}

func (m *Memory) ListTracks(_ context.Context) ([]TrackInfo, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	out := make([]TrackInfo, 0, len(m.maps))
	for _, tm := range m.maps {
		out = append(out, TrackInfo{tm.Track, tm.Length})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Track < out[j].Track })
	return out, nil
}

func (m *Memory) GetCorners(_ context.Context, track string) ([]Corner, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	return append([]Corner{}, m.corners[track]...), nil
}

func (m *Memory) SaveCorners(_ context.Context, track string, corners []Corner) error {
	m.smu.Lock()
	defer m.smu.Unlock()
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
	canon := m.canonical(car)
	out := []PitSetup{}
	seen := map[string]bool{}
	// Set-ups stored before the car had a team car are still under the vehicle name; a set-up of the team car wins.
	for _, name := range []string{canon, car} {
		for _, s := range m.setups {
			if s.Car == name && !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s)
			}
		}
		if canon == car {
			break
		}
	}
	return out, nil
}

func (m *Memory) SavePitSetup(_ context.Context, s PitSetup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.Car = m.canonical(s.Car)
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
	canon := m.canonical(car)
	kept := m.setups[:0]
	for _, old := range m.setups {
		if (old.Car == canon || old.Car == car) && old.Name == name {
			continue
		}
		kept = append(kept, old)
	}
	m.setups = kept
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
		by[id] = &SessionSummary{SessionInfo: s, Excluded: m.sessOut[id]}
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
	m.smu.Lock()
	defer m.smu.Unlock()
	l, ok := m.lanes[track]
	return l, ok, nil
}

func (m *Memory) SavePitLane(_ context.Context, l pitlane.Lane) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	m.lanes[l.Track] = l
	return nil
}

func (m *Memory) TeamByName(_ context.Context, name string) (Team, bool, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	for _, t := range m.teams {
		if t.Name == name {
			return t, true, nil
		}
	}
	return Team{}, false, nil
}

func (m *Memory) CreateTeam(_ context.Context, name string) (Team, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	var next int64 = 1
	for id, t := range m.teams {
		if t.Name == name {
			return Team{}, ErrExists
		}
		if id >= next {
			next = id + 1
		}
	}
	t := Team{next, name}
	m.teams[next] = t
	return t, nil
}

func (m *Memory) CreateAgent(_ context.Context, name string, teamID int64, tokenHash []byte) (Agent, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	if _, ok := m.teams[teamID]; !ok {
		return Agent{}, ErrNoTeam
	}
	var next int64 = 1
	for id, a := range m.agents {
		if bytes.Equal(a.hash, tokenHash) {
			return Agent{}, ErrExists
		}
		if id >= next {
			next = id + 1
		}
	}
	a := &memAgent{Agent: Agent{ID: next, Name: name, TeamID: teamID}, hash: tokenHash}
	m.agents[next] = a
	return a.Agent, nil
}

func (m *Memory) SetAgentEmail(_ context.Context, id int64, email string) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return ErrNoAgent
	}
	a.Email = email
	return nil
}

func (m *Memory) RevokeAgent(_ context.Context, id int64) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	if a, ok := m.agents[id]; ok {
		a.Revoked = true
	}
	return nil
}

func (m *Memory) AgentByTokenHash(_ context.Context, hash []byte) (Agent, bool, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	for _, a := range m.agents {
		if bytes.Equal(a.hash, hash) {
			return a.Agent, true, nil
		}
	}
	return Agent{}, false, nil
}

func (m *Memory) TouchAgent(_ context.Context, id int64) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	if a, ok := m.agents[id]; ok {
		now := time.Now()
		a.LastSeen = &now
	}
	return nil
}

func (m *Memory) Teams(_ context.Context) ([]Team, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	out := make([]Team, 0, len(m.teams))
	for _, t := range m.teams {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// member returns the member with the name of its team filled in.
func (m *Memory) member(v Member) Member {
	v.Team = m.teams[v.TeamID].Name
	return v
}

func (m *Memory) MemberByEmail(_ context.Context, email string) (Member, bool, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	v, ok := m.members[email]
	return m.member(v), ok, nil
}

func (m *Memory) Members(_ context.Context) ([]Member, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	out := make([]Member, 0, len(m.members))
	for _, v := range m.members {
		out = append(out, m.member(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}

func (m *Memory) SetMember(_ context.Context, v Member) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	if _, ok := m.teams[v.TeamID]; !ok {
		return ErrNoTeam
	}
	v.Team = ""
	m.members[v.Email] = v
	return nil
}

func (m *Memory) DeleteMember(_ context.Context, email string) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	delete(m.members, email)
	return nil
}

func (m *Memory) Agents(_ context.Context) ([]Agent, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	out := make([]Agent, 0, len(m.agents))
	for _, a := range m.agents {
		out = append(out, a.Agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) AddAudit(_ context.Context, e AuditEntry) error {
	m.smu.Lock()
	defer m.smu.Unlock()
	m.audit = append(m.audit, e)
	return nil
}

func (m *Memory) Audit(_ context.Context, limit int) ([]AuditEntry, error) {
	m.smu.Lock()
	defer m.smu.Unlock()
	var out []AuditEntry
	for i := len(m.audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.audit[i])
	}
	return out, nil
}
