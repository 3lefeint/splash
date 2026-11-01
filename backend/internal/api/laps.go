package api

import (
	"errors"
	"net/http"

	"github.com/3lefeint/splash/backend/internal/store"
)

// lapRow is a lap of a driven car as the lap table shows it: the lap, the conditions it was driven in, who drove it and
// whether it counts for the models.
type lapRow struct {
	CarID       int       `json:"car_id"`
	VehicleName string    `json:"vehicle_name"`
	Lap         int       `json:"lap"`
	Driver      string    `json:"driver"`
	AgentID     int64     `json:"agent_id"`
	Agent       string    `json:"agent,omitempty"`
	LapTime     *float64  `json:"lap_time"`
	S1          *float64  `json:"s1"`
	S2          *float64  `json:"s2"`
	S3          *float64  `json:"s3"`
	FuelUsed    *float64  `json:"fuel_used"`
	TyreAge     *int      `json:"tyre_age"`
	Wear        []float64 `json:"wear,omitempty"`
	FCY         bool      `json:"fcy"`
	InLap       bool      `json:"in_lap"`
	OutLap      bool      `json:"out_lap"`
	Control     *int      `json:"control"`
	Ambient     *float64  `json:"ambient"`
	Track       *float64  `json:"track"`
	Rain        *float64  `json:"rain"`
	Wetness     *float64  `json:"wetness"`
	Pressure    []float64 `json:"pressure,omitempty"`
	Compound    string    `json:"compound,omitempty"`
	// Excluded is whether the lap is left out of the models; ExcludedFlag is the lap's own choice (null follows the
	// session). Mine says the lap was driven with an agent of the requesting member, CanEdit whether they may change it.
	Excluded     bool  `json:"excluded"`
	ExcludedFlag *bool `json:"excluded_flag"`
	Mine         bool  `json:"mine"`
	CanEdit      bool  `json:"can_edit"`
}

type sessionLaps struct {
	SessionID      string   `json:"session_id"`
	Excluded       bool     `json:"excluded"`
	CanEditSession bool     `json:"can_edit_session"`
	Laps           []lapRow `json:"laps"`
}

// myAgents is the set of registered agents of the requesting member. Without an address (authentication off) it is nil.
func (a *API) myAgents(r *http.Request, teamID int64) (map[int64]bool, error) {
	me, _ := IdentityOf(r.Context())
	if me.Email == "" {
		return nil, nil
	}
	list, err := a.Teams.Dir.Agents(r.Context())
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, ag := range list {
		if ag.TeamID == teamID && ag.Email == me.Email {
			out[ag.ID] = true
		}
	}
	return out, nil
}

// mayChange says whether the requesting member may change a lap delivered by agent: the member's own laps and, for an
// administrator, all of them. Without authentication everybody may.
func mayChange(r *http.Request, mine map[int64]bool, agent int64) bool {
	me, _ := IdentityOf(r.Context())
	return me.Email == "" || me.IsAdmin || (agent != 0 && mine[agent])
}

// getSessionLaps lists the laps of the driven cars of a session with their conditions, for the lap table.
func (a *API) getSessionLaps(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	ctx := r.Context()
	laps, err := rt.Store.Laps(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	conds, err := rt.Store.LapConditions(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	mine, err := a.myAgents(r, rt.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	agents, err := a.Teams.Dir.Agents(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	names := map[int64]string{}
	for _, ag := range agents {
		if ag.TeamID == rt.ID {
			names[ag.ID] = ag.Name
		}
	}
	type key struct {
		car     int
		vehicle string
		lap     int
	}
	byLap := map[key]store.LapConditions{}
	for _, c := range conds {
		byLap[key{c.CarID, c.VehicleName, c.Lap}] = c
	}
	out := sessionLaps{SessionID: id, Laps: []lapRow{}, CanEditSession: true}
	if sums, err := rt.Store.Sessions(ctx); err == nil {
		for _, s := range sums {
			if s.SessionID == id {
				out.Excluded = s.Excluded
			}
		}
	}
	for _, l := range laps {
		c, hasConds := byLap[key{l.CarID, l.VehicleName, l.Lap.Lap}]
		if l.FuelStart == nil && !hasConds {
			continue // a lap of another car: the table is about the cars the team drives
		}
		row := lapRow{
			CarID: l.CarID, VehicleName: l.VehicleName, Lap: l.Lap.Lap, Driver: l.Driver, AgentID: l.AgentID, Agent: names[l.AgentID],
			LapTime: l.LapTime, S1: l.S1, S2: l.S2, S3: l.S3, TyreAge: l.TyreAge, Wear: l.Wear,
			FCY: l.FCY, InLap: l.InLap, OutLap: l.OutLap, Excluded: l.Excluded, ExcludedFlag: l.ExcludedFlag,
			Mine: l.AgentID != 0 && mine[l.AgentID], CanEdit: mayChange(r, mine, l.AgentID),
		}
		if l.FuelStart != nil && l.FuelEnd != nil {
			used := *l.FuelStart - *l.FuelEnd
			row.FuelUsed = &used
		}
		if hasConds {
			row.Control, row.Ambient, row.Track, row.Rain, row.Wetness = c.Control, c.Ambient, c.Track, c.Rain, c.Wetness
			row.Pressure = c.Pressure
			row.Compound = c.CompoundFront
			if c.CompoundRear != "" && c.CompoundRear != c.CompoundFront {
				row.Compound += "/" + c.CompoundRear
			}
		}
		if !row.CanEdit {
			out.CanEditSession = false
		}
		out.Laps = append(out.Laps, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// putLapExcluded leaves a lap out of the models, takes it in, or lets it follow its session again (null).
func (a *API) putLapExcluded(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	var req struct {
		SessionID   string `json:"session_id"`
		CarID       int    `json:"car_id"`
		VehicleName string `json:"vehicle_name"`
		Lap         int    `json:"lap"`
		Excluded    *bool  `json:"excluded"`
	}
	if !decode(w, r, &req) {
		return
	}
	laps, err := rt.Store.Laps(r.Context(), req.SessionID)
	if err != nil {
		serverError(w, err)
		return
	}
	var agent int64
	found := false
	for _, l := range laps {
		if l.CarID == req.CarID && l.VehicleName == req.VehicleName && l.Lap.Lap == req.Lap {
			agent, found = l.AgentID, true
		}
	}
	if !found {
		fail(w, http.StatusNotFound, "unknown lap")
		return
	}
	mine, err := a.myAgents(r, rt.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if !mayChange(r, mine, agent) {
		fail(w, http.StatusForbidden, "only the driver of the lap or an administrator may change it")
		return
	}
	err = rt.Store.SetLapExcluded(r.Context(), req.SessionID, req.CarID, req.VehicleName, req.Lap, req.Excluded)
	if errors.Is(err, store.ErrNoLap) {
		fail(w, http.StatusNotFound, "unknown lap")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putSessionExcluded leaves a whole session out of the models, or takes it in again. Laps with a choice of their own keep
// it. Only an administrator or a member whose agents delivered all the laps of the session may do it.
func (a *API) putSessionExcluded(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	var req struct {
		Excluded bool `json:"excluded"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	laps, err := rt.Store.Laps(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	mine, err := a.myAgents(r, rt.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	conds, err := rt.Store.LapConditions(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	driven := map[[3]any]bool{}
	for _, c := range conds {
		driven[[3]any{c.CarID, c.VehicleName, c.Lap}] = true
	}
	for _, l := range laps {
		if l.FuelStart == nil && !driven[[3]any{l.CarID, l.VehicleName, l.Lap.Lap}] {
			continue // a lap of another car
		}
		if !mayChange(r, mine, l.AgentID) {
			fail(w, http.StatusForbidden, "the session has laps of other drivers: leave out your own laps, or ask an administrator")
			return
		}
	}
	err = rt.Store.SetSessionExcluded(r.Context(), id, req.Excluded)
	if errors.Is(err, store.ErrNoSession) {
		fail(w, http.StatusNotFound, "unknown session")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
