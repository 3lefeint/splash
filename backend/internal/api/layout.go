package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// getLayout returns the dashboard default of the member's team: whatever an administrator saved (the frontend reads and
// validates it), or an empty object when the team has none.
func (a *API) getLayout(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	doc, found, err := rt.Store.TeamLayout(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if !found {
		doc = []byte("{}")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(doc)
}

// adminSetLayout saves the body, a JSON object, as the dashboard default of the administrator's own team.
func (a *API) adminSetLayout(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		fail(w, http.StatusBadRequest, "the layout is too large or unreadable")
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		fail(w, http.StatusBadRequest, "a JSON object is required")
		return
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := rt.Store.SetTeamLayout(r.Context(), compact.Bytes()); err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "layout_saved")
	w.WriteHeader(http.StatusNoContent)
}
