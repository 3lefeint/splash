// Package api serves the REST endpoints and the SSE stream used by the web frontend.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/logx"
	"github.com/3lefeint/splash/backend/internal/ratelimit"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/teams"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	maxBody          = 64 << 10
	maxPlanRows      = 200
	defaultHeartbeat = 15 * time.Second
	defaultTick      = time.Second
	subscriberCh     = 1
)

// API holds the handlers. Every request is identified first (see Identify) and then served from the runtime of the
// team of the identity.
type API struct {
	Teams *teams.Registry
	// Verifier checks the Cloudflare Access token of browser requests. Nil switches authentication off: every request
	// then belongs to the team "default" and nobody is an administrator.
	Verifier *auth.Verifier
	// Identify says who a browser request comes from and which team it belongs to. It is the only place that decides
	// this; the default checks the Access token with Verifier and maps the e-mail address to a team.
	Identify func(*http.Request) (Identity, error)
	// WriteLimit limits write requests per identity, ConnLimit new SSE streams. Nil means unlimited.
	WriteLimit, ConnLimit *ratelimit.Limiter
	// Interval is how often the state is pushed to the SSE clients; zero means every second.
	Interval time.Duration
	// Heartbeat is how often an idle SSE stream gets a keep-alive message, so idle timeouts on the way (the tunnel,
	// Cloudflare) do not cut it; zero means 15 seconds.
	Heartbeat time.Duration
	Log       *slog.Logger
	// Throttle keeps floods of the same INFO line (failed logins, rate limit hits) to one line per interval.
	Throttle *logx.Throttle

	mu   sync.Mutex
	ctx  context.Context
	hubs map[hubKey]*Hub
	now  func() time.Time
}

// New returns an API for the teams of reg. Until Start is called no state is pushed to SSE clients.
func New(reg *teams.Registry) *API {
	a := &API{Teams: reg, Interval: defaultTick, hubs: map[hubKey]*Hub{}, now: time.Now, Log: logx.Discard(), Throttle: logx.NewThrottle(time.Minute)}
	a.Identify = a.identify
	return a
}

// team returns the runtime of the requesting team, or writes the error response and returns false.
func (a *API) team(w http.ResponseWriter, r *http.Request) (*teams.Runtime, bool) {
	id, ok := IdentityOf(r.Context())
	if !ok { // a handler that was not registered through guard
		fail(w, http.StatusInternalServerError, "request was not identified")
		return nil, false
	}
	return a.Teams.Get(id.TeamID), true
}

// Start lets the SSE hubs run until ctx ends. A hub of a team is started when its first client connects.
func (a *API) Start(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

// Agent selectors of a request. A request names an agent with ?agent=<id>. Without it a member sees their own agent,
// the one of their address that sent the newest sample; a request without an address (authentication switched off)
// follows one active agent of the team, see teams.Runtime.Followed.
const (
	followAgent int64 = -1
	mineAgent   int64 = -2
)

type hubKey struct {
	team, agent int64
	email       string // only for mineAgent
}

// agentSel reads which agent a request is about.
func agentSel(r *http.Request) (sel int64, email string, err error) {
	if v := r.URL.Query().Get("agent"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			return 0, "", errors.New("agent must be a number")
		}
		return id, "", nil
	}
	if me, _ := IdentityOf(r.Context()); me.Email != "" {
		return mineAgent, me.Email, nil
	}
	return followAgent, "", nil
}

// engineOf returns the engine and agent a selector stands for. ag is nil when there is nothing to show: the engine
// without live data is returned then (a team without a connected agent, a member without an agent of their own). ok is
// false for an explicit agent that has not connected since the backend started.
func engineOf(rt *teams.Runtime, sel int64, email string) (eng *engine.Engine, ag *teams.AgentRT, ok bool) {
	switch sel {
	case followAgent:
		ag = rt.Followed()
	case mineAgent:
		ag = rt.AgentOf(email)
	default:
		if ag, ok = rt.AgentByID(sel); !ok {
			return nil, nil, false
		}
	}
	if ag == nil {
		return rt.Idle(), nil, true
	}
	return ag.Engine, ag, true
}

// engine resolves the agent of a request, or writes the error response and returns false.
func (a *API) engine(w http.ResponseWriter, r *http.Request, rt *teams.Runtime) (sel int64, email string, eng *engine.Engine, ag *teams.AgentRT, ok bool) {
	sel, email, err := agentSel(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return 0, "", nil, nil, false
	}
	eng, ag, ok = engineOf(rt, sel, email)
	if !ok {
		fail(w, http.StatusNotFound, "unknown agent")
		return 0, "", nil, nil, false
	}
	return sel, email, eng, ag, true
}

// hub returns the SSE hub of a team and agent selector and starts it on first use.
func (a *API) hub(rt *teams.Runtime, sel int64, email string) *Hub {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := hubKey{team: rt.ID, agent: sel, email: email}
	h, ok := a.hubs[key]
	if !ok {
		h = &Hub{Compute: func(ctx context.Context) (engine.State, error) {
			eng, _, ok := engineOf(rt, sel, email)
			if !ok {
				return engine.State{}, errors.New("unknown agent")
			}
			return eng.Compute(ctx)
		}, Interval: a.Interval, Log: a.Log, Team: rt.ID}
		a.hubs[key] = h
		if a.ctx != nil {
			go h.Run(a.ctx)
		}
	}
	return h
}

// Register adds the routes to mux. Every route goes through guard.
func (a *API) Register(mux *http.ServeMux) {
	h := func(pattern string, fn http.HandlerFunc) { mux.HandleFunc(pattern, a.guard(pattern, fn)) }
	h("GET /api/me", a.getMe)
	h("GET /api/state", a.getState)
	h("GET /api/agents", a.getAgents)
	h("GET /api/stream", a.stream)
	h("GET /api/params", a.getParams)
	h("PUT /api/params", a.putParams)
	h("GET /api/stint-plan", a.getPlan)
	h("PUT /api/stint-plan", a.putPlan)
	h("GET /api/track-map", a.getTrackMap)
	h("DELETE /api/track-map", a.deleteTrackMap)
	h("GET /api/tracks", a.listTracks)
	h("GET /api/track-corners", a.getCorners)
	h("PUT /api/track-corners", a.putCorners)
	h("GET /api/track-map/progress", a.getMapProgress)
	h("GET /api/pit-setups", a.getPitSetups)
	h("PUT /api/pit-setups", a.putPitSetup)
	h("DELETE /api/pit-setups", a.deletePitSetup)
	h("POST /api/pit-setups/apply", a.applyPitSetup)
	h("POST /api/pit-request", a.pitRequest)
	h("GET /api/sessions", a.getSessions)
	h("GET /api/sessions/{id}/export", a.exportSession)
	h("GET /api/sessions/{id}/laps", a.getSessionLaps)
	h("PUT /api/sessions/{id}/excluded", a.putSessionExcluded)
	h("PUT /api/laps/excluded", a.putLapExcluded)
	h("GET /api/overrides", a.getOverrides)
	h("PUT /api/overrides/{key}", a.putOverride)
	h("DELETE /api/overrides/{key}", a.deleteOverride)
	a.registerAdmin(h)
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) { writeJSON(w, status, apiError{msg}) }

func serverError(w http.ResponseWriter, err error) {
	logx.ReportError(w, err) // the request middleware logs it, with the route and the team
	fail(w, http.StatusInternalServerError, err.Error())
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func finiteNonNegative(vals ...float64) bool {
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	return true
}

func (a *API) getState(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	_, _, eng, _, ok := a.engine(w, r, rt)
	if !ok {
		return
	}
	st, err := eng.Compute(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) getParams(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	track, car := r.URL.Query().Get("track"), r.URL.Query().Get("car")
	if track == "" && car == "" {
		list, err := rt.Store.ListParams(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		if list == nil {
			list = []store.Params{}
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	p, ok, err := rt.Store.GetParams(r.Context(), track, car)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "no parameters for this track and car")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) putParams(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	var p store.Params
	if !decode(w, r, &p) {
		return
	}
	if q := r.URL.Query(); q.Get("track") != "" || q.Get("car") != "" {
		p.Track, p.Car = q.Get("track"), q.Get("car")
	}
	if p.Track == "" || p.Car == "" {
		fail(w, http.StatusBadRequest, "track and car are required")
		return
	}
	if !finiteNonNegative(p.PitThroughGreen, p.PitThroughFCY, p.RefuelRate, p.TyreChange, p.DriverSwap,
		p.FuelEffect, p.DegPerLap, p.SaveCost, p.Reserve, p.FitSlowPercent) {
		fail(w, http.StatusBadRequest, "values must be finite and not negative")
		return
	}
	if err := p.ValidateFit(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ParallelService = false // no longer used; an older client may still send it
	p = p.WithDefaults()
	if err := rt.Store.SetParams(r.Context(), p); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	rows, err := rt.Store.GetStintPlan(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if rows == nil {
		rows = []store.PlanRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *API) putPlan(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	var rows []store.PlanRow
	if !decode(w, r, &rows) {
		return
	}
	if len(rows) > maxPlanRows {
		fail(w, http.StatusBadRequest, fmt.Sprintf("at most %d stints", maxPlanRows))
		return
	}
	for i, row := range rows {
		if row.Driver == "" || !finiteNonNegative(row.Duration) || row.Duration == 0 {
			fail(w, http.StatusBadRequest, fmt.Sprintf("stint %d: driver and a duration above 0 are required", i+1))
			return
		}
	}
	if err := rt.Store.SetStintPlan(r.Context(), rows); err != nil {
		serverError(w, err)
		return
	}
	if rows == nil {
		rows = []store.PlanRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// overrideAgent is the agent whose overrides a request reads or writes, see agentSel. Without an agent there is
// nothing to read and nothing to write.
func (a *API) overrideAgent(w http.ResponseWriter, r *http.Request, rt *teams.Runtime) (int64, bool) {
	_, _, eng, ag, ok := a.engine(w, r, rt)
	if !ok {
		return 0, false
	}
	if ag == nil && isWrite(r.Method) {
		fail(w, http.StatusConflict, "there is no agent to set an override for")
		return 0, false
	}
	return eng.AgentID, true
}

func (a *API) getOverrides(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	agent, ok := a.overrideAgent(w, r, rt)
	if !ok {
		return
	}
	ov, err := rt.Store.GetOverrides(r.Context(), agent)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

func (a *API) putOverride(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	agent, ok := a.overrideAgent(w, r, rt)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if !store.ValidOverrideKey(key) {
		fail(w, http.StatusNotFound, "unknown override key")
		return
	}
	var body struct {
		Value float64 `json:"value"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !finiteNonNegative(body.Value) {
		fail(w, http.StatusBadRequest, "value must be finite and not negative")
		return
	}
	if err := rt.Store.SetOverride(r.Context(), agent, key, body.Value); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": body.Value})
}

func (a *API) deleteOverride(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	agent, ok := a.overrideAgent(w, r, rt)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if !store.ValidOverrideKey(key) {
		fail(w, http.StatusNotFound, "unknown override key")
		return
	}
	if err := rt.Store.DeleteOverride(r.Context(), agent, key); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stream sends the complete state as server-sent events.
func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	sel, email, eng, _, ok := a.engine(w, r, rt)
	if !ok {
		return
	}
	ch, cancel := a.hub(rt, sel, email).Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// A new client gets the current state at once instead of waiting for the next tick.
	if st, err := eng.Compute(r.Context()); err == nil {
		if data, err := json.Marshal(st); err == nil {
			writeEvent(w, data)
		}
	}
	fl.Flush()
	start := time.Now()
	a.Log.Debug("stream connected", "team", rt.ID)
	defer func() { a.Log.Debug("stream closed", "team", rt.ID, "seconds", int(time.Since(start).Seconds())) }()

	beat := a.Heartbeat
	if beat <= 0 {
		beat = defaultHeartbeat
	}
	tick := time.NewTicker(beat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			if err := writeEvent(w, data); err != nil {
				a.Log.Warn("stream client gone with unsent data", "team", rt.ID, "err", err)
				return
			}
		case <-tick.C:
			a.Log.Debug("stream heartbeat", "team", rt.ID)
			// One message with a comment, which every proxy and client accepts, and a named event with the interval in
			// milliseconds, which the page can see: a connection that went silent without an error is detected by the
			// absence of both.
			if _, err := fmt.Fprintf(w, ": ping\nevent: ping\ndata: %d\n\n", beat.Milliseconds()); err != nil {
				a.Log.Warn("stream client gone with unsent data", "team", rt.ID, "err", err)
				return
			}
		}
		fl.Flush()
	}
}

func writeEvent(w http.ResponseWriter, data []byte) error {
	_, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
	return err
}

// Hub computes the state once per interval and hands it to all SSE clients.
type Hub struct {
	// Compute builds the state to publish.
	Compute  func(context.Context) (engine.State, error)
	Interval time.Duration
	// Log and Team are for the lines about a state that could not be computed.
	Log  *slog.Logger
	Team int64

	warnedAt time.Time // when the last compute failure was logged; a failing database would otherwise log every second

	mu   sync.Mutex
	subs map[chan []byte]struct{}
	last []byte
}

// Subscribe returns a channel with the newest state; a slow client misses
// intermediate states but always gets the latest one.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, subscriberCh)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[chan []byte]struct{}{}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Run publishes until ctx ends. It computes nothing while nobody is listening
// and sends only when the state changed.
func (h *Hub) Run(ctx context.Context) {
	iv := h.Interval
	if iv <= 0 {
		iv = defaultTick
	}
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if h.subscribers() == 0 {
			continue
		}
		st, err := h.Compute(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				if h.Log != nil && time.Since(h.warnedAt) > time.Minute {
					h.warnedAt = time.Now()
					h.Log.Error("state could not be computed", "team", h.Team, "err", err)
				}
				continue
			}
			return
		}
		data, err := json.Marshal(st)
		if err != nil {
			continue
		}
		h.mu.Lock()
		if bytes.Equal(data, h.last) {
			h.mu.Unlock()
			continue
		}
		h.last = data
		for ch := range h.subs {
			select {
			case ch <- data:
			default:
				select { // replace the stale state with the newest
				case <-ch:
				default:
				}
				select {
				case ch <- data:
				default:
				}
			}
		}
		h.mu.Unlock()
	}
}

func (a *API) getTrackMap(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	m, ok, err := rt.Store.GetTrackMap(r.Context(), track)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "no map for this track yet")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// deleteTrackMap removes the stored outline so the next clean lap records a new one.
func (a *API) deleteTrackMap(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	if err := rt.Store.DeleteTrackMap(r.Context(), track); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listTracks names the tracks that have a stored outline, for the track editor.
func (a *API) listTracks(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	list, err := rt.Store.ListTracks(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) getCorners(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	list, err := rt.Store.GetCorners(r.Context(), track)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// putCorners replaces the corner names of a track. The list is kept in the order it was sent.
func (a *API) putCorners(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	var list []store.Corner
	if !decode(w, r, &list) {
		return
	}
	if len(list) > maxCorners {
		fail(w, http.StatusBadRequest, "too many corners")
		return
	}
	for _, c := range list {
		if c.Name == "" || len(c.Name) > maxNameLen {
			fail(w, http.StatusBadRequest, "every corner needs a name of at most 40 characters")
			return
		}
		for _, d := range []*float64{c.From, c.To} {
			if d != nil && !(*d >= 0 && *d <= maxLapDist) {
				fail(w, http.StatusBadRequest, "a distance must be between 0 and 100000 metres")
				return
			}
		}
		if c.Label != nil && !(math.Abs(c.Label[0]) <= maxWorld && math.Abs(c.Label[1]) <= maxWorld) {
			fail(w, http.StatusBadRequest, "the label position is out of range")
			return
		}
	}
	if list == nil {
		list = []store.Corner{}
	}
	if err := rt.Store.SaveCorners(r.Context(), track, list); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

const (
	maxCorners    = 200
	maxLapDist    = 100000
	maxWorld      = 1e6
	maxSetupSteps = 16
	maxNameLen    = 40
)

func validSteps(steps []wire.Step) string {
	if len(steps) == 0 || len(steps) > maxSetupSteps {
		return fmt.Sprintf("1 to %d steps are required", maxSetupSteps)
	}
	for i, st := range steps {
		if st.Category == "" || st.Choice == "" || len(st.Category) > maxNameLen || len(st.Choice) > maxNameLen {
			return fmt.Sprintf("step %d: category and choice are required (at most %d characters)", i+1, maxNameLen)
		}
	}
	return ""
}

func (a *API) getPitSetups(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	list, err := rt.Store.PitSetups(r.Context(), r.URL.Query().Get("car"))
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) putPitSetup(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	var s store.PitSetup
	if !decode(w, r, &s) {
		return
	}
	if s.Car == "" || s.Name == "" || len(s.Name) > maxNameLen {
		fail(w, http.StatusBadRequest, "car and a name of at most 40 characters are required")
		return
	}
	if msg := validSteps(s.Steps); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	if err := rt.Store.SavePitSetup(r.Context(), s); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) deletePitSetup(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if err := rt.Store.DeletePitSetup(r.Context(), q.Get("car"), q.Get("name")); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) applyPitSetup(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	var req struct {
		Name  string      `json:"name"`
		Steps []wire.Step `json:"steps"`
	}
	if !decode(w, r, &req) {
		return
	}
	if msg := validSteps(req.Steps); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	agent, ok := a.commandAgent(w, r, rt)
	if !ok {
		return
	}
	id, err := rt.Cmds.Send(r.Context(), agent, req.Name, req.Steps)
	switch {
	case errors.Is(err, commands.ErrNotConnected), errors.Is(err, commands.ErrBusy):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		serverError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
	}
}

func (a *API) getMapProgress(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	if p := rt.Ingest.Maps.Progress(r.URL.Query().Get("track")); p != nil {
		writeJSON(w, http.StatusOK, p)
		return
	}
	fail(w, http.StatusNotFound, "no outline is being recorded")
}

// commandAgent is the agent a command goes to: the requesting member's own, or one named with ?agent= that belongs to
// them (an administrator may name any). Without authentication any agent may be named.
func (a *API) commandAgent(w http.ResponseWriter, r *http.Request, rt *teams.Runtime) (int64, bool) {
	_, _, _, ag, ok := a.engine(w, r, rt)
	if !ok {
		return 0, false
	}
	if ag == nil {
		fail(w, http.StatusConflict, "there is no agent to send the command to")
		return 0, false
	}
	if me, _ := IdentityOf(r.Context()); me.Email != "" && !me.IsAdmin && ag.Email() != me.Email {
		fail(w, http.StatusForbidden, "the agent belongs to somebody else")
		return 0, false
	}
	return ag.ID, true
}

// pitRequest toggles the pit request of the driven car through the agent of the requesting member.
func (a *API) pitRequest(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || len(name) > maxNameLen {
		name = "pit request"
	}
	agent, ok := a.commandAgent(w, r, rt)
	if !ok {
		return
	}
	id, err := rt.Cmds.PitRequest(r.Context(), agent, name)
	switch {
	case errors.Is(err, commands.ErrNotConnected), errors.Is(err, commands.ErrBusy):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		serverError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
	}
}
