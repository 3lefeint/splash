// Package api serves the REST endpoints and the SSE stream used by the web frontend.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	maxBody      = 64 << 10
	maxPlanRows  = 200
	heartbeat    = 15 * time.Second
	defaultTick  = time.Second
	subscriberCh = 1
)

// API holds the handlers. Writes have no authentication; the VPN is trusted.
type API struct {
	Store  store.Store
	Engine *engine.Engine
	Hub    *Hub
	// Cmds sends pit setups to the driver agent. Nil answers with 503.
	Cmds *commands.Hub
	// MapProgress returns the track outline recorded so far. Nil answers with 404.
	MapProgress func(track string) *trackmap.Progress
}

// New returns an API with a Hub that pushes the state every second.
func New(st store.Store, eng *engine.Engine) *API {
	return &API{Store: st, Engine: eng, Hub: &Hub{Engine: eng, Interval: defaultTick}}
}

// Register adds the routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", a.getState)
	mux.HandleFunc("GET /api/stream", a.stream)
	mux.HandleFunc("GET /api/params", a.getParams)
	mux.HandleFunc("PUT /api/params", a.putParams)
	mux.HandleFunc("GET /api/stint-plan", a.getPlan)
	mux.HandleFunc("PUT /api/stint-plan", a.putPlan)
	mux.HandleFunc("GET /api/track-map", a.getTrackMap)
	mux.HandleFunc("DELETE /api/track-map", a.deleteTrackMap)
	mux.HandleFunc("GET /api/tracks", a.listTracks)
	mux.HandleFunc("GET /api/track-corners", a.getCorners)
	mux.HandleFunc("PUT /api/track-corners", a.putCorners)
	mux.HandleFunc("GET /api/track-map/progress", a.getMapProgress)
	mux.HandleFunc("GET /api/pit-setups", a.getPitSetups)
	mux.HandleFunc("PUT /api/pit-setups", a.putPitSetup)
	mux.HandleFunc("DELETE /api/pit-setups", a.deletePitSetup)
	mux.HandleFunc("POST /api/pit-setups/apply", a.applyPitSetup)
	mux.HandleFunc("POST /api/pit-request", a.pitRequest)
	mux.HandleFunc("GET /api/sessions", a.getSessions)
	mux.HandleFunc("GET /api/sessions/{id}/export", a.exportSession)
	mux.HandleFunc("GET /api/overrides", a.getOverrides)
	mux.HandleFunc("PUT /api/overrides/{key}", a.putOverride)
	mux.HandleFunc("DELETE /api/overrides/{key}", a.deleteOverride)
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
	st, err := a.Engine.Compute(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) getParams(w http.ResponseWriter, r *http.Request) {
	track, car := r.URL.Query().Get("track"), r.URL.Query().Get("car")
	if track == "" && car == "" {
		list, err := a.Store.ListParams(r.Context())
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
	p, ok, err := a.Store.GetParams(r.Context(), track, car)
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
		p.FuelEffect, p.DegPerLap, p.SaveCost, p.Reserve) {
		fail(w, http.StatusBadRequest, "values must be finite and not negative")
		return
	}
	if err := a.Store.SetParams(r.Context(), p); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.GetStintPlan(r.Context())
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
	if err := a.Store.SetStintPlan(r.Context(), rows); err != nil {
		serverError(w, err)
		return
	}
	if rows == nil {
		rows = []store.PlanRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *API) getOverrides(w http.ResponseWriter, r *http.Request) {
	ov, err := a.Store.GetOverrides(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

func (a *API) putOverride(w http.ResponseWriter, r *http.Request) {
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
	if err := a.Store.SetOverride(r.Context(), key, body.Value); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": body.Value})
}

func (a *API) deleteOverride(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !store.ValidOverrideKey(key) {
		fail(w, http.StatusNotFound, "unknown override key")
		return
	}
	if err := a.Store.DeleteOverride(r.Context(), key); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stream sends the complete state as server-sent events.
func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	ch, cancel := a.Hub.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// A new client gets the current state at once instead of waiting for the next tick.
	if st, err := a.Engine.Compute(r.Context()); err == nil {
		if data, err := json.Marshal(st); err == nil {
			writeEvent(w, data)
		}
	}
	fl.Flush()

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			writeEvent(w, data)
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}

func writeEvent(w http.ResponseWriter, data []byte) {
	fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
}

// Hub computes the state once per interval and hands it to all SSE clients.
type Hub struct {
	Engine   *engine.Engine
	Interval time.Duration

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
		st, err := h.Engine.Compute(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
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
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	m, ok, err := a.Store.GetTrackMap(r.Context(), track)
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
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	if err := a.Store.DeleteTrackMap(r.Context(), track); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listTracks names the tracks that have a stored outline, for the track editor.
func (a *API) listTracks(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ListTracks(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) getCorners(w http.ResponseWriter, r *http.Request) {
	track := r.URL.Query().Get("track")
	if track == "" {
		fail(w, http.StatusBadRequest, "track is required")
		return
	}
	list, err := a.Store.GetCorners(r.Context(), track)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// putCorners replaces the corner names of a track. The list is kept in the order it was sent.
func (a *API) putCorners(w http.ResponseWriter, r *http.Request) {
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
	if err := a.Store.SaveCorners(r.Context(), track, list); err != nil {
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
	list, err := a.Store.PitSetups(r.Context(), r.URL.Query().Get("car"))
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) putPitSetup(w http.ResponseWriter, r *http.Request) {
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
	if err := a.Store.SavePitSetup(r.Context(), s); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *API) deletePitSetup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := a.Store.DeletePitSetup(r.Context(), q.Get("car"), q.Get("name")); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) applyPitSetup(w http.ResponseWriter, r *http.Request) {
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
	if a.Cmds == nil {
		fail(w, http.StatusServiceUnavailable, "commands are not available")
		return
	}
	id, err := a.Cmds.Send(r.Context(), req.Name, req.Steps)
	switch {
	case errors.Is(err, commands.ErrNoDriver), errors.Is(err, commands.ErrBusy):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		serverError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
	}
}

func (a *API) getMapProgress(w http.ResponseWriter, r *http.Request) {
	if a.MapProgress != nil {
		if p := a.MapProgress(r.URL.Query().Get("track")); p != nil {
			writeJSON(w, http.StatusOK, p)
			return
		}
	}
	fail(w, http.StatusNotFound, "no outline is being recorded")
}

// pitRequest toggles the pit request of the driven car through the driver agent.
func (a *API) pitRequest(w http.ResponseWriter, r *http.Request) {
	if a.Cmds == nil {
		fail(w, http.StatusServiceUnavailable, "commands are not available")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || len(name) > maxNameLen {
		name = "pit request"
	}
	id, err := a.Cmds.PitRequest(r.Context(), name)
	switch {
	case errors.Is(err, commands.ErrNoDriver), errors.Is(err, commands.ErrBusy):
		fail(w, http.StatusConflict, err.Error())
	case err != nil:
		serverError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
	}
}
