// Package engine combines the live state, stored laps and stops, parameters and
// overrides into the complete state shown to the team.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/segments"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/strategy"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	// Tolerances at which a live value counts as matching its override again.
	fuelOverrideTolerance       = 1.0 // litres
	fuelPerLapOverrideTolerance = 0.1 // litres per lap
	paceLaps                    = 5   // laps averaged for the lap time forecast
	trendLaps                   = 5   // laps used for the gap trend
	historyLaps                 = 10
)

// State is what the SSE stream and GET /api/state deliver.
type State struct {
	Ready     bool      `json:"ready"`
	SessionID string    `json:"session_id,omitempty"`
	Track     string    `json:"track,omitempty"`
	ET        float64   `json:"et"`
	TimeLeft  float64   `json:"time_left"`
	Car       *Car      `json:"car,omitempty"`
	Race      *Race     `json:"race,omitempty"`
	StintPlan *Plan     `json:"stint_plan,omitempty"`
	Opponents Opponents `json:"opponents"`
	// TrackLength and Field place every car on the track map.
	TrackLength float64                   `json:"track_length,omitempty"`
	Field       []FieldCar                `json:"field,omitempty"`
	Wear        *WearStats                `json:"wear_stats,omitempty"`
	Tyres       *Tyres                    `json:"tyres,omitempty"`
	PitMenu     *wire.PitMenu             `json:"pit_menu,omitempty"`
	Command     *commands.Status          `json:"command,omitempty"`
	Strategy    *Strategy                 `json:"strategy,omitempty"`
	Fit         *Fit                      `json:"fit,omitempty"`
	Fcy         bool                      `json:"fcy,omitempty"`
	Agents      []commands.AgentStatus    `json:"agents,omitempty"`
	Sections    *TrackStats               `json:"sections,omitempty"`
	Pits        *PitStats                 `json:"pits,omitempty"`
	Weather     *Weather                  `json:"weather,omitempty"`
	Params      store.Params              `json:"params"`
	Overrides   map[string]store.Override `json:"overrides"`
}

// FieldCar is the position of one car on the track.
type FieldCar struct {
	CarID       int     `json:"car_id"`
	VehicleName string  `json:"vehicle_name"`
	Driver      string  `json:"driver"`
	Class       string  `json:"class"`
	Place       int     `json:"place"`
	LapDist     float64 `json:"lap_dist"`
	InPits      bool    `json:"in_pits"`
	Ours        bool    `json:"ours"`
	OffTrack    bool    `json:"off_track,omitempty"`
}

type Car struct {
	CarID        int       `json:"car_id"`
	VehicleName  string    `json:"vehicle_name"`
	Driver       string    `json:"driver"`
	Class        string    `json:"class"`
	Place        int       `json:"place"`
	Lap          int       `json:"lap"`
	Fuel         float64   `json:"fuel"`
	FuelCapacity float64   `json:"fuel_capacity"`
	FuelPerLap   *float64  `json:"fuel_per_lap"`
	LapTime      *float64  `json:"lap_time"`
	Wear         []float64 `json:"wear"`
	// Control is who drives the car: 0 local player, 1 local AI, 2 remote; nil when unknown.
	Control *int `json:"control,omitempty"`
	// PitState is the pit state of the car: 0 none, 1 requested, 2 entering, 3 stopped, 4 exiting.
	PitState int `json:"pit_state"`
	// Penalties is the number of penalties, DentMax the largest body damage (0 none, 1 some, 2 heavy).
	Penalties   int  `json:"penalties,omitempty"`
	DentMax     int  `json:"dent_max,omitempty"`
	Overheating bool `json:"overheating,omitempty"`
	Detached    bool `json:"detached,omitempty"`
	// FuelOverridden and FuelPerLapOverridden tell the UI that a manual value is in use.
	FuelOverridden       bool `json:"fuel_overridden"`
	FuelPerLapOverridden bool `json:"fuel_per_lap_overridden"`
}

type Race struct {
	LapsToStop     int      `json:"laps_to_stop"`
	LapsLeft       int      `json:"laps_left"`
	StopsRemaining int      `json:"stops_remaining"`
	SaveTarget     *float64 `json:"save_target"`
	// StintEnd is the game time at which the fuel runs down to the reserve.
	StintEnd float64 `json:"stint_end"`
}

type Plan struct {
	Entries          []strategy.PlanEntry `json:"entries"`
	Shortfall        float64              `json:"shortfall"`
	CurrentDeviation float64              `json:"current_deviation"`
	PitLoss          float64              `json:"pit_loss"`
}

type Opponents struct {
	Ahead  *Opponent `json:"ahead"`
	Behind *Opponent `json:"behind"`
}

type Opponent struct {
	CarID       int      `json:"car_id"`
	VehicleName string   `json:"vehicle_name"`
	Driver      string   `json:"driver"`
	Place       int      `json:"place"`
	Gap         *float64 `json:"gap"`   // seconds
	Trend       *float64 `json:"trend"` // seconds per lap, negative = closing
}

// Engine computes State. It keeps the gap history of the class opponents.
type Engine struct {
	Store store.Store
	Live  *live.State
	// Commands is the hub whose newest command is part of the state. Nil leaves it out.
	Commands *commands.Hub
	// Log receives warnings about failed writes. New sets a logger that discards them.
	Log *slog.Logger
	// Field and Own measure the sections of every car and of the driven car, Pits knows the pit lane. Nil leaves them out.
	Field, Own *segments.Recorder
	Pits       PitSource

	mu      sync.Mutex
	session string
	lastLap int
	history map[string][]float64
	weather weatherLog
	wet     wetCache
	rivals  rivalLog
}

// New returns an Engine.
func New(st store.Store, lv *live.State) *Engine {
	return &Engine{Store: st, Live: lv, Log: slog.New(slog.DiscardHandler), history: map[string][]float64{}}
}

// Compute builds the current state. It is not Ready until telemetry and scoring have arrived.
func (e *Engine) Compute(ctx context.Context) (State, error) {
	snap := e.Live.Snapshot()
	ov, err := e.Store.GetOverrides(ctx)
	if err != nil {
		return State{}, err
	}
	st := State{SessionID: snap.SessionID, ET: snap.ScoringET, Overrides: ov, Opponents: Opponents{}}
	if snap.Telemetry == nil || snap.Scoring == nil {
		return st, nil
	}
	tel, sc := snap.Telemetry, snap.Scoring
	st.Track = sc.Track
	st.TimeLeft = math.Max(sc.EndET-snap.ScoringET, 0)

	var me *wire.ScoringVehicle
	for i := range sc.Vehicles {
		v := &sc.Vehicles[i]
		if v.CarID == tel.CarID && v.VehicleName == tel.VehicleName {
			me = v
		}
	}
	car := Car{CarID: tel.CarID, VehicleName: tel.VehicleName, Lap: tel.Lap, Fuel: tel.Fuel,
		FuelCapacity: tel.FuelCapacity, Wear: tel.Wear}
	if me != nil {
		car.Driver, car.Class, car.Place, car.Control = me.Driver, me.VehicleClass, me.Place, me.Control
		car.PitState, car.Penalties = me.PitState, me.Penalties
	}
	if tel.DentMax != nil {
		car.DentMax = *tel.DentMax
	}
	car.Overheating, car.Detached = tel.Overheating, tel.Detached

	params, ok, err := e.Store.GetParams(ctx, sc.Track, tel.VehicleName)
	if err != nil {
		return State{}, err
	}
	if !ok {
		params = store.Params{Track: sc.Track, Car: tel.VehicleName, Reserve: store.DefaultReserve}
	}
	st.Params = params

	laps, err := e.Store.LapsOfCar(ctx, snap.SessionID, tel.CarID, tel.VehicleName)
	if err != nil {
		return State{}, err
	}
	wears, err := e.Store.LapWears(ctx, snap.SessionID, tel.CarID, tel.VehicleName)
	if err != nil {
		return State{}, err
	}
	st.Wear = wearStats(laps, wears)
	st.Fit = fitModel(laps, setBoundaries(laps), e.wetLaps(ctx, snap.SessionID, tel.CarID, tel.VehicleName, len(laps)))
	allStops, err := e.Store.Stops(ctx, snap.SessionID)
	if err != nil {
		return State{}, err
	}
	st.Pits = pitStats(allStops, tel.CarID, tel.VehicleName)
	model, lapTime, lastDriver := fuelModel(laps)
	if car.Driver == "" {
		car.Driver = lastDriver
	}
	if lapTime == 0 && me != nil && me.LastLap != nil {
		lapTime = *me.LastLap
	}
	if lapTime > 0 {
		car.LapTime = &lapTime
	}

	// Overrides replace live values until the live value matches them again.
	if o, ok := ov[store.OverrideFuel]; ok {
		if math.Abs(tel.Fuel-o.Value) <= fuelOverrideTolerance {
			delete(ov, store.OverrideFuel)
			if err := e.Store.DeleteOverride(ctx, store.OverrideFuel); err != nil {
				return State{}, err
			}
		} else {
			car.Fuel, car.FuelOverridden = o.Value, true
		}
	}
	fpl, haveFPL := model.FuelPerLap(car.Driver)
	if !haveFPL {
		fpl, haveFPL = model.Mean()
	}
	if o, ok := ov[store.OverrideFuelPerLap]; ok {
		if haveFPL && math.Abs(fpl-o.Value) <= fuelPerLapOverrideTolerance {
			delete(ov, store.OverrideFuelPerLap)
			if err := e.Store.DeleteOverride(ctx, store.OverrideFuelPerLap); err != nil {
				return State{}, err
			}
		} else {
			fpl, haveFPL = o.Value, true
			car.FuelPerLapOverridden = true
		}
	}
	if haveFPL {
		car.FuelPerLap = &fpl
	}
	st.Car = &car
	st.Ready = true

	if haveFPL && lapTime > 0 {
		st.Race = race(car, params, fpl, lapTime, snap.ScoringET, st.TimeLeft)
	}
	if st.Race != nil {
		plan, err := e.stintPlan(ctx, snap, car, params, fpl, lapTime, sc.EndET, st.Race.StintEnd)
		if err != nil {
			return State{}, err
		}
		st.StintPlan = plan
	}
	st.Opponents = e.opponents(snap.SessionID, car, sc)
	st.TrackLength, st.Field = field(sc, tel)
	st.Tyres = tyreState(tel)
	if tel != nil {
		st.PitMenu = tel.PitMenu
	}
	if e.Commands != nil {
		st.Command = e.Commands.Status()
		st.Agents = e.Commands.Agents()
	}
	st.Weather = weatherState(sc.Weather, e.weatherHistory(ctx, snap.SessionID, snap.ScoringET, sc.Weather))
	st.Fcy = isFCY(sc)
	st.Sections = e.trackStats(snap.SessionID, sc.Track, car, sc.TrackLength, st.Opponents)
	st.Strategy, err = e.strategyState(ctx, snap, car, params, lapTime, st.StintPlan, st.Fcy)
	if err != nil {
		return State{}, err
	}
	return st, nil
}

// fuelModel feeds the stored laps of the car into the consumption model and
// returns it with the average of the last valid lap times and the last driver.
func fuelModel(laps []store.LapRecord) (m *strategy.Consumption, lapTime float64, lastDriver string) {
	m = strategy.NewConsumption(strategy.DefaultAlpha)
	var times []float64
	for _, r := range laps {
		l := r.Lap
		if l.Driver != "" {
			lastDriver = l.Driver
		}
		if l.LapTime == nil || l.FuelStart == nil || l.FuelEnd == nil {
			continue
		}
		used := m.Add(strategy.Lap{Number: l.Lap, Driver: l.Driver, Time: *l.LapTime,
			FuelStart: *l.FuelStart, FuelEnd: *l.FuelEnd, FCY: l.FCY, InLap: l.InLap, OutLap: l.OutLap})
		if used {
			times = append(times, *l.LapTime)
		}
	}
	if n := len(times); n > 0 {
		if n > paceLaps {
			times = times[n-paceLaps:]
		}
		var sum float64
		for _, t := range times {
			sum += t
		}
		lapTime = sum / float64(len(times))
	}
	return m, lapTime, lastDriver
}

func race(car Car, p store.Params, fpl, lapTime, et, timeLeft float64) *Race {
	r := &Race{}
	var err error
	if r.LapsToStop, err = strategy.LapsToStop(car.Fuel, p.Reserve, fpl); err != nil {
		return nil
	}
	if r.LapsLeft, err = strategy.LapsLeft(timeLeft, lapTime); err != nil {
		return nil
	}
	if car.FuelCapacity > 0 {
		if r.StopsRemaining, err = strategy.StopsRemaining(r.LapsLeft, fpl, car.Fuel, car.FuelCapacity); err != nil {
			return nil
		}
		if t, ok := strategy.SaveTarget(car.Fuel, car.FuelCapacity, r.StopsRemaining, r.LapsLeft); ok {
			r.SaveTarget = &t
		}
	}
	r.StintEnd = et + float64(r.LapsToStop)*lapTime
	return r
}

// pitLoss is the time between two stints used for planning.
func pitLoss(p store.Params, capacity float64) float64 {
	var refuel float64
	if p.RefuelRate > 0 {
		refuel = math.Max(capacity-p.Reserve, 0) / p.RefuelRate
	}
	other := p.TyreChange + p.DriverSwap
	if p.ParallelService {
		return p.PitThroughGreen + math.Max(refuel, other)
	}
	return p.PitThroughGreen + refuel + other
}

func (e *Engine) stintPlan(ctx context.Context, snap live.Snapshot, car Car, p store.Params, fpl, lapTime, raceEnd, stintEnd float64) (*Plan, error) {
	rows, err := e.Store.GetStintPlan(ctx)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	planned := make([]strategy.PlannedStint, len(rows))
	for i, r := range rows {
		planned[i] = strategy.PlannedStint{Driver: r.Driver, Duration: r.Duration}
	}
	stops, err := e.Store.Stops(ctx, snap.SessionID)
	if err != nil {
		return nil, err
	}
	var mine []store.StopRecord
	for _, s := range stops {
		if s.CarID == car.CarID && s.VehicleName == car.VehicleName {
			mine = append(mine, s)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].EntryET < mine[j].EntryET })

	// Every stop ends a stint, so n stops mean n completed stints.
	start := snap.FirstET
	if len(mine) > 0 && start > mine[0].EntryET {
		start = 0 // the backend restarted after the first stop; the true start is unknown, the session start is the bound
	}
	var done []strategy.ActualStint
	for i, s := range mine {
		if i >= len(planned) {
			break
		}
		done = append(done, strategy.ActualStint{Driver: planned[i].Driver, Start: start, End: s.EntryET})
		start = s.EntryET
		if s.ExitET != nil {
			start = *s.ExitET
		}
	}
	in := strategy.PlanInput{
		Planned: planned,
		Done:    done,
		Current: &strategy.CurrentStint{Start: start, ExpectedEnd: stintEnd},
		RaceEnd: raceEnd,
		PitLoss: pitLoss(p, car.FuelCapacity),
	}
	if fpl > 0 && lapTime > 0 && car.FuelCapacity > 0 {
		in.MaxStint = math.Max(car.FuelCapacity-p.Reserve, 0) / fpl * lapTime
	}
	res, err := strategy.Reschedule(in)
	if err != nil && !errors.Is(err, strategy.ErrNoCurrentStint) {
		return nil, err
	}
	return &Plan{Entries: res.Entries, Shortfall: res.Shortfall, CurrentDeviation: res.CurrentDeviation, PitLoss: in.PitLoss}, nil
}

// opponents finds the class opponents directly ahead and behind and keeps their
// gap history, one sample per lap of our car.
func (e *Engine) opponents(session string, car Car, sc *wire.Scoring) Opponents {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != session {
		e.session, e.lastLap, e.history = session, 0, map[string][]float64{}
	}
	byPlace := map[int]wire.ScoringVehicle{}
	var class []wire.ScoringVehicle
	for _, v := range sc.Vehicles {
		byPlace[v.Place] = v
		if v.VehicleClass == car.Class {
			class = append(class, v)
		}
	}
	sort.Slice(class, func(i, j int) bool { return class[i].Place < class[j].Place })
	idx := -1
	for i, v := range class {
		if v.CarID == car.CarID && v.VehicleName == car.VehicleName {
			idx = i
		}
	}
	var out Opponents
	if idx < 0 || car.Class == "" {
		return out
	}
	record := car.Lap != e.lastLap
	e.lastLap = car.Lap
	mk := func(o wire.ScoringVehicle, from, to int) *Opponent {
		op := &Opponent{CarID: o.CarID, VehicleName: o.VehicleName, Driver: o.Driver, Place: o.Place}
		// The time gap is the sum of the time-behind-next values of all cars between the two places.
		var gap float64
		known := true
		for pl := from; pl <= to; pl++ {
			v, ok := byPlace[pl]
			if !ok || v.TimeBehindNext == nil {
				known = false
				break
			}
			gap += *v.TimeBehindNext
		}
		if !known {
			return op
		}
		op.Gap = &gap
		key := fmt.Sprintf("%s|%d", o.VehicleName, o.CarID)
		if record {
			h := append(e.history[key], gap)
			if len(h) > historyLaps {
				h = h[len(h)-historyLaps:]
			}
			e.history[key] = h
		}
		if t, ok := strategy.GapTrend(e.history[key], trendLaps); ok {
			op.Trend = &t
		}
		return op
	}
	if idx > 0 {
		out.Ahead = mk(class[idx-1], class[idx-1].Place+1, car.Place)
	}
	if idx < len(class)-1 {
		out.Behind = mk(class[idx+1], car.Place+1, class[idx+1].Place)
	}
	return out
}

// field lists the cars that report a distance along the lap.
func field(sc *wire.Scoring, tel *wire.Telemetry) (float64, []FieldCar) {
	if sc.TrackLength <= 0 {
		return 0, nil
	}
	var out []FieldCar
	for _, v := range sc.Vehicles {
		if v.LapDist == nil {
			continue
		}
		out = append(out, FieldCar{CarID: v.CarID, VehicleName: v.VehicleName, Driver: v.Driver,
			Class: v.VehicleClass, Place: v.Place, LapDist: *v.LapDist, InPits: v.InPits,
			OffTrack: offTrack(v.PathLateral, v.TrackEdge, v.InPits),
			Ours:     v.CarID == tel.CarID && v.VehicleName == tel.VehicleName})
	}
	return sc.TrackLength, out
}

// isFCY is true during a full course yellow: game phase 6 or one of the full course yellow states.
func isFCY(sc *wire.Scoring) bool {
	if sc.GamePhase != nil && *sc.GamePhase == 6 {
		return true
	}
	return sc.YellowState != nil && *sc.YellowState >= 1 && *sc.YellowState <= 3
}

// strategyState analyses the other cars. The lap statistics of the field are reloaded every few seconds.
func (e *Engine) strategyState(ctx context.Context, snap live.Snapshot, car Car, p store.Params, lapTime float64, plan *Plan, fcy bool) (*Strategy, error) {
	r := &e.rivals
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reset(snap.SessionID)
	r.watchFuel(snap.Scoring.Vehicles)
	if r.laps == nil || time.Since(r.at) > rivalRefresh {
		laps, err := e.Store.Laps(ctx, snap.SessionID)
		if err != nil {
			return nil, err
		}
		r.laps, r.at = summarizeField(laps), time.Now()
	}
	swap := false
	if plan != nil {
		for i, en := range plan.Entries {
			if en.Current && i+1 < len(plan.Entries) {
				swap = plan.Entries[i+1].DriverSwap
			}
		}
	}
	return r.analyse(strategyInput{sc: snap.Scoring, car: car, params: p, lapTime: lapTime, fcy: fcy, swap: swap, stats: r.laps}), nil
}

// wetCache remembers which laps of the driven car were driven on a wet track.
type wetCache struct {
	mu      sync.Mutex
	session string
	car     string
	laps    int
	wet     map[int]bool
}

// wetLaps returns a test for laps driven on a wet track. The stored conditions are read again only when the car has
// completed another lap.
func (e *Engine) wetLaps(ctx context.Context, session string, carID int, vehicle string, laps int) func(int) bool {
	c := &e.wet
	c.mu.Lock()
	defer c.mu.Unlock()
	key := carKey(vehicle, carID)
	if c.session != session || c.car != key || c.laps != laps {
		c.session, c.car, c.laps, c.wet = session, key, laps, map[int]bool{}
		if list, err := e.Store.LapConditions(ctx, session); err == nil {
			for _, l := range list {
				if l.CarID == carID && l.VehicleName == vehicle && ((l.Wetness != nil && *l.Wetness > wetLap) || (l.Rain != nil && *l.Rain > wetLap)) {
					c.wet[l.Lap] = true
				}
			}
		}
	}
	wet := c.wet
	return func(lap int) bool { return wet[lap] }
}

// wetLap is the wetness or rain above which a lap does not tell about the dry lap time model.
const wetLap = 0.1
