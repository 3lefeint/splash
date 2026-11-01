package api

import (
	"net/http"
	"sort"
	"time"
)

// agentInfo is an agent of the team as the picker shows it. Addresses are left out.
type agentInfo struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Mine       bool       `json:"mine"`
	Active     bool       `json:"active"`
	LastSample *time.Time `json:"last_sample"`
	LastSeen   *time.Time `json:"last_seen"`
	SessionID  string     `json:"session_id,omitempty"`
}

// getAgents lists the agents of the team: the registered ones that are not revoked, and the shared dev token if it
// has connected. Mine are the agents of the requesting member's address.
func (a *API) getAgents(w http.ResponseWriter, r *http.Request) {
	rt, ok := a.team(w, r)
	if !ok {
		return
	}
	me, _ := IdentityOf(r.Context())
	list, err := a.Teams.Dir.Agents(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := []agentInfo{}
	seen := map[int64]bool{}
	for _, ag := range list {
		if ag.TeamID != rt.ID || ag.Revoked {
			continue
		}
		seen[ag.ID] = true
		info := agentInfo{ID: ag.ID, Name: ag.Name, Mine: me.Email != "" && ag.Email == me.Email, LastSeen: ag.LastSeen}
		if live, ok := rt.AgentByID(ag.ID); ok {
			info.fillLive(rt.Active(live), live.Live.Updated(), live.Live.Snapshot().SessionID)
		}
		out = append(out, info)
	}
	for _, live := range rt.Agents() {
		if !seen[live.ID] && live.ID == 0 { // the shared dev token has no registered agent
			info := agentInfo{ID: 0, Name: "shared"}
			info.fillLive(rt.Active(live), live.Live.Updated(), live.Live.Snapshot().SessionID)
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Mine != y.Mine {
			return x.Mine
		}
		if x.Active != y.Active {
			return x.Active
		}
		return x.Name < y.Name
	})
	writeJSON(w, http.StatusOK, out)
}

func (i *agentInfo) fillLive(active bool, updated time.Time, session string) {
	i.Active, i.SessionID = active, session
	if !updated.IsZero() {
		i.LastSample = &updated
	}
}
