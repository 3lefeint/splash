package store

import (
	"context"
	"sort"
	"sync"
	"time"

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
		progress:  map[string]int64{},
		params:    map[string]Params{},
		overrides: map[string]Override{},
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
			if s.SessionID == sessionID && s.CarID == e.CarID && s.VehicleName == e.VehicleName && s.EntryET == et {
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
