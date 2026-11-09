package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
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
	h("GET /api/admin/cars", a.adminOnly(a.adminCars))
	h("POST /api/admin/cars", a.adminOnly(a.adminCreateCar))
	h("PUT /api/admin/layout", a.adminOnly(a.adminSetLayout))
	h("DELETE /api/admin/cars/{id}", a.adminOnly(a.adminDeleteCar))
	h("PUT /api/admin/cars/{id}/names", a.adminOnly(a.adminMapCarName))
	h("DELETE /api/admin/car-names", a.adminOnly(a.adminUnmapCarName))
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
	Email      string `json:"email"`
	TeamID     int64  `json:"team_id"`
	Team       string `json:"team,omitempty"`
	IsAdmin    bool   `json:"is_admin"`
	Name       string `json:"name"`
	Short      string `json:"short"`
	CFClientID string `json:"cf_client_id"`
	// HasCFSecret says there is a Cloudflare secret; it is never sent back.
	HasCFSecret bool `json:"has_cf_secret"`
}

func memberOf(m store.Member) memberJSON {
	return memberJSON{m.Email, m.TeamID, m.Team, m.IsAdmin, m.Name, m.Short, m.CFClientID, m.HasCFSecret}
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
		out = append(out, memberOf(m))
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

// printable is true for text of printable ASCII only, which can go into a configuration file as it is.
func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

var shortPattern = regexp.MustCompile(`^[a-z0-9]{2,8}$`)

const (
	maxMemberName = 60
	maxCFID       = 200
	maxCFSecret   = 500
)

// adminSetMember adds an address to a team or changes it: team, admin flag, name, short name and the Cloudflare service
// token of the member's agent. Fields left out of the request keep their value; the secret is only ever written, never
// read back. An administrator cannot lock themselves out by removing their own admin flag.
func (a *API) adminSetMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string  `json:"email"`
		TeamID     int64   `json:"team_id"`
		IsAdmin    bool    `json:"is_admin"`
		Name       *string `json:"name"`
		Short      *string `json:"short"`
		CFClientID *string `json:"cf_client_id"`
		CFSecret   *string `json:"cf_secret"`
		ClearCF    bool    `json:"clear_cf"`
	}
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
	ctx := r.Context()
	old, _, err := a.Teams.Dir.MemberByEmail(ctx, email)
	if err != nil {
		serverError(w, err)
		return
	}
	m := store.Member{Email: email, TeamID: req.TeamID, IsAdmin: req.IsAdmin, Name: old.Name, Short: old.Short, CFClientID: old.CFClientID}
	if req.Name != nil {
		m.Name = strings.TrimSpace(*req.Name)
		if len(m.Name) > maxMemberName {
			fail(w, http.StatusBadRequest, "a name of at most 60 characters")
			return
		}
	}
	if req.Short != nil {
		m.Short = strings.ToLower(strings.TrimSpace(*req.Short))
		if m.Short != "" && !shortPattern.MatchString(m.Short) {
			fail(w, http.StatusBadRequest, "the short name has 2 to 8 letters or digits")
			return
		}
	}
	if req.CFClientID != nil {
		m.CFClientID = strings.TrimSpace(*req.CFClientID)
		if len(m.CFClientID) > maxCFID {
			fail(w, http.StatusBadRequest, "the Cloudflare client id is too long")
			return
		}
	}
	secret := ""
	if req.CFSecret != nil {
		secret = strings.TrimSpace(*req.CFSecret)
	}
	if !printable(m.CFClientID) || !printable(secret) {
		fail(w, http.StatusBadRequest, "the Cloudflare values must be plain printable characters")
		return
	}
	if secret != "" {
		if a.Secrets == nil {
			fail(w, http.StatusConflict, "this backend has no key to store Cloudflare secrets (SPLASH_SECRET_KEY)")
			return
		}
		if len(secret) > maxCFSecret || m.CFClientID == "" {
			fail(w, http.StatusBadRequest, "a Cloudflare secret needs its client id and is at most 500 characters")
			return
		}
	}
	// A secret belongs to its client id: a changed id without a new secret, or a removed id, drops the old secret.
	dropSecret := req.ClearCF || m.CFClientID == "" || (m.CFClientID != old.CFClientID && secret == "")
	if req.ClearCF {
		m.CFClientID = ""
	}
	switch err := a.Teams.Dir.SetMember(ctx, m); {
	case errors.Is(err, store.ErrNoTeam):
		fail(w, http.StatusBadRequest, "unknown team")
		return
	case errors.Is(err, store.ErrShortTaken):
		fail(w, http.StatusConflict, "another member of the team has this short name")
		return
	case err != nil:
		serverError(w, err)
		return
	}
	switch {
	case secret != "":
		sealed, err := a.Secrets.Seal(secret, email)
		if err == nil {
			err = a.Teams.Dir.SetMemberSecret(ctx, email, sealed)
		}
		if err != nil {
			serverError(w, err)
			return
		}
	case dropSecret && old.HasCFSecret:
		if err := a.Teams.Dir.SetMemberSecret(ctx, email, nil); err != nil {
			serverError(w, err)
			return
		}
	}
	// The agent of a member is named after the short name.
	if m.Short != "" && m.Short != old.Short {
		a.renameAgentsOf(r, email, m.TeamID, m.Short)
	}
	a.adminLog(r, "member_set", "member", logx.ShortID(email), "team_id", req.TeamID, "is_admin", req.IsAdmin,
		"cf_secret_set", secret != "", "cf_cleared", dropSecret)
	saved, _, _ := a.Teams.Dir.MemberByEmail(ctx, email)
	writeJSON(w, http.StatusOK, memberOf(saved))
}

// renameAgentsOf gives the agents of a member the new short name.
func (a *API) renameAgentsOf(r *http.Request, email string, teamID int64, name string) {
	list, err := a.Teams.Dir.Agents(r.Context())
	if err != nil {
		a.Log.Warn("agents could not be renamed", "err", err)
		return
	}
	for _, ag := range list {
		if ag.Email == email && ag.TeamID == teamID && !ag.Revoked && ag.Name != name {
			if err := a.Teams.Dir.RenameAgent(r.Context(), ag.ID, name); err != nil {
				a.Log.Warn("agent could not be renamed", "agent_id", ag.ID, "err", err)
			}
		}
	}
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

const maxCarName = 120

// carsOfTeam resolves ?team_id= (or a team_id in the body) to the store of that team, or writes the error response.
func (a *API) carsOfTeam(w http.ResponseWriter, r *http.Request, raw string) (store.Store, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, "team_id is required")
		return nil, false
	}
	teams, err := a.Teams.Dir.Teams(r.Context())
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	for _, t := range teams {
		if t.ID == id {
			return a.Teams.Get(id).Store, true
		}
	}
	fail(w, http.StatusBadRequest, "unknown team")
	return nil, false
}

// adminCars lists the team cars of a team and the vehicle names seen in its sessions that belong to none.
func (a *API) adminCars(w http.ResponseWriter, r *http.Request) {
	st, ok := a.carsOfTeam(w, r, r.URL.Query().Get("team_id"))
	if !ok {
		return
	}
	cars, err := st.Cars(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	unmapped, err := st.UnmappedCarNames(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Cars     []store.TeamCar `json:"cars"`
		Unmapped []string        `json:"unmapped"`
	}{cars, unmapped})
}

func (a *API) adminCreateCar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID int64  `json:"team_id"`
		Name   string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, ok := a.carsOfTeam(w, r, strconv.FormatInt(req.TeamID, 10))
	if !ok {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > maxCarName {
		fail(w, http.StatusBadRequest, "a car name of at most 120 characters is required")
		return
	}
	c, err := st.CreateCar(r.Context(), req.Name)
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "the team has a car with this name")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "car_created", "team_id", req.TeamID, "car_id", c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) adminDeleteCar(w http.ResponseWriter, r *http.Request) {
	st, ok := a.carsOfTeam(w, r, r.URL.Query().Get("team_id"))
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid car id")
		return
	}
	switch err := st.DeleteCar(r.Context(), id); {
	case errors.Is(err, store.ErrNoCar):
		fail(w, http.StatusNotFound, "unknown car")
	case err != nil:
		serverError(w, err)
	default:
		a.adminLog(r, "car_deleted", "car_id", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// adminMapCarName assigns a vehicle name of the game to a team car, moving it from another one.
func (a *API) adminMapCarName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID      int64  `json:"team_id"`
		VehicleName string `json:"vehicle_name"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, ok := a.carsOfTeam(w, r, strconv.FormatInt(req.TeamID, 10))
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid car id")
		return
	}
	req.VehicleName = strings.TrimSpace(req.VehicleName)
	if req.VehicleName == "" || len(req.VehicleName) > maxCarName {
		fail(w, http.StatusBadRequest, "a vehicle name of at most 120 characters is required")
		return
	}
	switch err := st.MapCarName(r.Context(), id, req.VehicleName); {
	case errors.Is(err, store.ErrNoCar):
		fail(w, http.StatusNotFound, "unknown car")
	case err != nil:
		serverError(w, err)
	default:
		a.adminLog(r, "car_name_mapped", "team_id", req.TeamID, "car_id", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) adminUnmapCarName(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, ok := a.carsOfTeam(w, r, q.Get("team_id"))
	if !ok {
		return
	}
	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		fail(w, http.StatusBadRequest, "a vehicle name is required")
		return
	}
	if err := st.UnmapCarName(r.Context(), name); err != nil {
		serverError(w, err)
		return
	}
	a.adminLog(r, "car_name_unmapped", "team_id", q.Get("team_id"))
	w.WriteHeader(http.StatusNoContent)
}
