package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/3lefeint/splash/backend/internal/logx"
	"github.com/3lefeint/splash/backend/internal/store"
)

// The admin routes all live under /api/admin/, so a proxy in front can route that prefix separately.
const (
	maxEmail    = 254
	maxTeamName = 60
	tokenBytes  = 32
)

func (a *API) registerAdmin(h func(string, http.HandlerFunc)) {
	h("GET /api/admin/teams", a.adminOnly(a.adminTeams))
	h("POST /api/admin/teams", a.adminOnly(a.adminCreateTeam))
	h("GET /api/admin/members", a.adminOnly(a.adminMembers))
	h("PUT /api/admin/members", a.adminOnly(a.adminSetMember))
	h("DELETE /api/admin/members", a.adminOnly(a.adminDeleteMember))
	h("GET /api/admin/agents", a.adminOnly(a.adminAgents))
	h("POST /api/admin/agents", a.adminOnly(a.adminCreateAgent))
	h("PUT /api/admin/agents/{id}", a.adminOnly(a.adminSetAgentEmail))
	h("POST /api/admin/agents/{id}/revoke", a.adminOnly(a.adminRevokeAgent))
}

// adminOnly lets only members with the admin flag through. Without authentication nobody is an administrator.
func (a *API) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id, _ := IdentityOf(r.Context()); !id.IsAdmin {
			fail(w, http.StatusForbidden, "administrators only")
			return
		}
		next(w, r)
	}
}

// adminLog writes the INFO line for an administrative change: what kind, which ids, who (as a short hash). Addresses
// and tokens are never in it.
func (a *API) adminLog(r *http.Request, action string, args ...any) {
	me, _ := IdentityOf(r.Context())
	a.Log.Info("admin action", append([]any{"action", action, "admin", logx.ShortID(me.Email), "admin_team", me.TeamID}, args...)...)
}

type teamJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type memberJSON struct {
	Email   string `json:"email"`
	TeamID  int64  `json:"team_id"`
	Team    string `json:"team,omitempty"`
	IsAdmin bool   `json:"is_admin"`
}

type agentJSON struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	TeamID   int64      `json:"team_id"`
	Email    string     `json:"email"`
	Revoked  bool       `json:"revoked"`
	LastSeen *time.Time `json:"last_seen"`
}

func agentOf(ag store.Agent) agentJSON {
	return agentJSON{ag.ID, ag.Name, ag.TeamID, ag.Email, ag.Revoked, ag.LastSeen}
}

// ownerOf checks the address an agent is to belong to: empty for none, otherwise a member of the agent's team.
func (a *API) ownerOf(w http.ResponseWriter, r *http.Request, raw string, teamID int64) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	email, ok := normalizeEmail(raw)
	if !ok {
		fail(w, http.StatusBadRequest, "a valid e-mail address is required")
		return "", false
	}
	m, found, err := a.Teams.Dir.MemberByEmail(r.Context(), email)
	if err != nil {
		serverError(w, err)
		return "", false
	}
	if !found || m.TeamID != teamID {
		fail(w, http.StatusBadRequest, "the address is not a member of the team of the agent")
		return "", false
	}
	return email, true
}

func (a *API) adminTeams(w http.ResponseWriter, r *http.Request) {
	list, err := a.Teams.Dir.Teams(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]teamJSON, 0, len(list))
	for _, t := range list {
		out = append(out, teamJSON{t.ID, t.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) adminCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > maxTeamName {
		fail(w, http.StatusBadRequest, "a team name of at most 60 characters is required")
		return
	}
	t, err := a.Teams.Dir.CreateTeam(r.Context(), req.Name)
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "a team with this name exists")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "team_created", "team_id", t.ID)
	writeJSON(w, http.StatusCreated, teamJSON{t.ID, t.Name})
}

func (a *API) adminMembers(w http.ResponseWriter, r *http.Request) {
	list, err := a.Teams.Dir.Members(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]memberJSON, 0, len(list))
	for _, m := range list {
		out = append(out, memberJSON{m.Email, m.TeamID, m.Team, m.IsAdmin})
	}
	writeJSON(w, http.StatusOK, out)
}

// normalizeEmail lower-cases an address the way the token's address is compared; ok is false for something that
// cannot be an address.
func normalizeEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	at := strings.IndexByte(s, '@')
	return s, at > 0 && at < len(s)-1 && len(s) <= maxEmail && !strings.ContainsAny(s, " \t\r\n,;<>")
}

// adminSetMember adds an address to a team or moves it, and sets its admin flag. An administrator cannot lock
// themselves out by removing their own admin flag.
func (a *API) adminSetMember(w http.ResponseWriter, r *http.Request) {
	var req memberJSON
	if !decode(w, r, &req) {
		return
	}
	email, ok := normalizeEmail(req.Email)
	if !ok {
		fail(w, http.StatusBadRequest, "a valid e-mail address is required")
		return
	}
	if me, _ := IdentityOf(r.Context()); me.Email == email && !req.IsAdmin {
		fail(w, http.StatusConflict, "you cannot remove your own admin flag")
		return
	}
	err := a.Teams.Dir.SetMember(r.Context(), store.Member{Email: email, TeamID: req.TeamID, IsAdmin: req.IsAdmin})
	if errors.Is(err, store.ErrNoTeam) {
		fail(w, http.StatusBadRequest, "unknown team")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "member_set", "member", logx.ShortID(email), "team_id", req.TeamID, "is_admin", req.IsAdmin)
	writeJSON(w, http.StatusOK, memberJSON{Email: email, TeamID: req.TeamID, IsAdmin: req.IsAdmin})
}

func (a *API) adminDeleteMember(w http.ResponseWriter, r *http.Request) {
	email, ok := normalizeEmail(r.URL.Query().Get("email"))
	if !ok {
		fail(w, http.StatusBadRequest, "a valid e-mail address is required")
		return
	}
	if me, _ := IdentityOf(r.Context()); me.Email == email {
		fail(w, http.StatusConflict, "you cannot remove yourself")
		return
	}
	if err := a.Teams.Dir.DeleteMember(r.Context(), email); err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "member_removed", "member", logx.ShortID(email))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) adminAgents(w http.ResponseWriter, r *http.Request) {
	list, err := a.Teams.Dir.Agents(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]agentJSON, 0, len(list))
	for _, ag := range list {
		out = append(out, agentOf(ag))
	}
	writeJSON(w, http.StatusOK, out)
}

// adminCreateAgent registers an agent and returns its token. This is the only time the token exists in plain text:
// only its hash is stored.
func (a *API) adminCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		TeamID int64  `json:"team_id"`
		Email  string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > maxTeamName {
		fail(w, http.StatusBadRequest, "an agent name of at most 60 characters is required")
		return
	}
	owner, ok := a.ownerOf(w, r, req.Email, req.TeamID)
	if !ok {
		return
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		serverError(w, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	ag, err := a.Teams.Dir.CreateAgent(r.Context(), req.Name, req.TeamID, store.HashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNoTeam) {
			fail(w, http.StatusBadRequest, "unknown team")
			return
		}
		serverError(w, err)
		return
	}
	if owner != "" {
		if err := a.Teams.Dir.SetAgentEmail(r.Context(), ag.ID, owner); err != nil {
			serverError(w, err)
			return
		}
		ag.Email = owner
		a.Teams.Get(ag.TeamID).SetAgentEmail(ag.ID, owner)
	}
	a.adminLog(r, "agent_created", "agent_id", ag.ID, "team_id", ag.TeamID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, struct {
		agentJSON
		Token string `json:"token"`
	}{agentOf(ag), token})
}

// adminSetAgentEmail ties an agent to a member of its team, or releases it with an empty address.
func (a *API) adminSetAgentEmail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid agent id")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	list, err := a.Teams.Dir.Agents(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var ag store.Agent
	found := false
	for _, x := range list {
		if x.ID == id {
			ag, found = x, true
		}
	}
	if !found {
		fail(w, http.StatusNotFound, "unknown agent")
		return
	}
	owner, ok := a.ownerOf(w, r, req.Email, ag.TeamID)
	if !ok {
		return
	}
	if err := a.Teams.Dir.SetAgentEmail(r.Context(), id, owner); err != nil {
		serverError(w, err)
		return
	}
	a.Teams.Get(ag.TeamID).SetAgentEmail(id, owner)
	a.adminLog(r, "agent_owner_set", "agent_id", id, "team_id", ag.TeamID, "owned", owner != "")
	ag.Email = owner
	writeJSON(w, http.StatusOK, agentOf(ag))
}

func (a *API) adminRevokeAgent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid agent id")
		return
	}
	if err := a.Teams.Dir.RevokeAgent(r.Context(), id); err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "agent_revoked", "agent_id", id)
	w.WriteHeader(http.StatusNoContent)
}
