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

	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/store"
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
