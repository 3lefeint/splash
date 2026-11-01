// Package teams holds the runtime of every team: live state, engine, command hub and agent ingest. A runtime is
// created when a team is first needed, so teams never share anything in memory.
package teams

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/segments"
	"github.com/3lefeint/splash/backend/internal/store"
)

// Runtime is everything one team needs while the backend runs. The live state and the engine exist once per agent,
// not once per team: the members train independently, in the same or in different sessions.
type Runtime struct {
	ID int64
	// Store sees only the data of this team (and the track data all teams share).
	Store  store.Store
	Cmds   *commands.Hub
	Ingest *ingest.Server

	// ActiveWindow is how recent an agent's last sample must be to count as active in Followed; zero means 30 s.
	ActiveWindow time.Duration

	log     *slog.Logger
	mu      sync.Mutex
	agents  map[int64]*AgentRT
	idle    *engine.Engine
	engines func(ref ingest.AgentRef, lv *live.State) *engine.Engine
}

// AgentRT is the live state of one agent and the engine that computes from it.
type AgentRT struct {
	ID     int64
	Name   string
	Live   *live.State
	Engine *engine.Engine

	mu    sync.Mutex
	email string
}

// Email is the member the agent belongs to; empty for none.
func (a *AgentRT) Email() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.email
}

func (a *AgentRT) setEmail(email string) {
	a.mu.Lock()
	a.email = email
	a.mu.Unlock()
}

// Agent returns the runtime of an agent and creates it on first use. Agent 0 is the shared dev token.
func (rt *Runtime) Agent(ref ingest.AgentRef) *AgentRT {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if a, ok := rt.agents[ref.ID]; ok {
		a.setEmail(ref.Email) // the address may have changed since the agent connected last
		return a
	}
	lv := &live.State{}
	a := &AgentRT{ID: ref.ID, Name: ref.Name, Live: lv, Engine: rt.engines(ref, lv), email: ref.Email}
	rt.agents[ref.ID] = a
	return a
}

// SetAgentEmail passes a changed owner on to the runtime of the agent, if it has one.
func (rt *Runtime) SetAgentEmail(id int64, email string) {
	if a, ok := rt.AgentByID(id); ok {
		a.setEmail(email)
	}
}

// AgentOf is the agent of a member that sent the newest live sample; nil when none of the member's agents has
// connected since the backend started. The address is compared as given, so pass it in lower case.
func (rt *Runtime) AgentOf(email string) *AgentRT {
	if email == "" {
		return nil
	}
	for _, a := range rt.Agents() { // newest sample first
		if a.Email() == email {
			return a
		}
	}
	return nil
}

// AgentByID returns the runtime of an agent that has connected since the backend started.
func (rt *Runtime) AgentByID(id int64) (*AgentRT, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	a, ok := rt.agents[id]
	return a, ok
}

// Agents lists the agent runtimes, the one with the newest live sample first.
func (rt *Runtime) Agents() []*AgentRT {
	rt.mu.Lock()
	out := make([]*AgentRT, 0, len(rt.agents))
	for _, a := range rt.agents {
		out = append(out, a)
	}
	rt.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].Live.Updated(), out[j].Live.Updated()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// defaultActiveWindow is how recent the last live sample of an agent must be for it to count as active.
const defaultActiveWindow = 30 * time.Second

// Active says whether the agent sent a live sample recently.
func (rt *Runtime) Active(a *AgentRT) bool {
	window := rt.ActiveWindow
	if window <= 0 {
		window = defaultActiveWindow
	}
	return time.Since(a.Live.Updated()) <= window
}

// Followed is the agent a request without an explicit agent shows: among the agents with a live sample in the last
// 30 seconds the one with the lowest id, so that two agents driving at the same time do not make the view jump;
// without an active agent the one with the newest sample. Nil when no agent has connected. It stands in until a
// browser is tied to its own agent.
func (rt *Runtime) Followed() *AgentRT {
	list := rt.Agents() // newest sample first
	if len(list) == 0 {
		return nil
	}
	var best *AgentRT
	for _, a := range list {
		if rt.Active(a) && (best == nil || a.ID < best.ID) {
			best = a
		}
	}
	if best == nil {
		best = list[0]
	}
	return best
}

// NoAgent is the id of the engine without live data. It owns no overrides and no laps.
const NoAgent int64 = -1

// Idle returns an engine without live data, for a request that has no agent to show: a team none of whose agents has
// connected yet, or a member without an agent. Its state has agent_id -1.
func (rt *Runtime) Idle() *engine.Engine { return rt.idle }

// Registry creates the runtimes on demand and is safe for concurrent use.
type Registry struct {
	Dir store.Directory
	Log *slog.Logger
	// PingInterval and DeadAfter configure the keep-alive of the agent connections, see ingest.Server.
	PingInterval, DeadAfter time.Duration
	// MinAgentVersion is the oldest agent version that counts as up to date; empty marks no agent as outdated.
	MinAgentVersion string

	defaultID int64
	mu        sync.Mutex
	runtimes  map[int64]*Runtime
}

// NewRegistry looks up the team "default", which owns the data that existed before teams were introduced.
func NewRegistry(ctx context.Context, dir store.Directory, log *slog.Logger) (*Registry, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	t, ok, err := dir.TeamByName(ctx, store.DefaultTeamName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("team %q not found: run the migrations", store.DefaultTeamName)
	}
	return &Registry{Dir: dir, Log: log, defaultID: t.ID, runtimes: map[int64]*Runtime{}}, nil
}

// DefaultID is the id of the team "default".
func (r *Registry) DefaultID() int64 { return r.defaultID }

// Get returns the runtime of a team and creates it on first use.
func (r *Registry) Get(id int64) *Runtime {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt, ok := r.runtimes[id]
	if !ok {
		rt = r.build(id)
		r.runtimes[id] = rt
	}
	return rt
}

func (r *Registry) build(id int64) *Runtime {
	st := r.Dir.ForTeam(id)
	cmds := commands.New()
	cmds.MinVersion = r.MinAgentVersion
	ing := ingest.New(st, nil, r.Log)
	ing.PingInterval, ing.DeadAfter, ing.TeamID = r.PingInterval, r.DeadAfter, id
	ing.Maps = ingest.NewMapRecorder(st, r.Log)
	ing.Cmds = cmds
	ing.Pits = ingest.NewPitRecorder(st, r.Log)
	ing.Field, ing.Own = &segments.Recorder{}, &segments.Recorder{}
	// The recorders and the command hub are shared by the agents of the team; they are keyed by session and car.
	newEngine := func(ref ingest.AgentRef, lv *live.State) *engine.Engine {
		eng := engine.New(st, lv)
		eng.AgentID, eng.AgentName = ref.ID, ref.Name
		eng.Commands = cmds
		eng.Log = r.Log
		eng.Field, eng.Own, eng.Pits = ing.Field, ing.Own, ing.Pits
		return eng
	}
	rt := &Runtime{ID: id, Store: st, Cmds: cmds, Ingest: ing, log: r.Log, agents: map[int64]*AgentRT{},
		idle: newEngine(ingest.AgentRef{ID: NoAgent}, &live.State{}), engines: newEngine}
	ing.LiveFor = func(ref ingest.AgentRef) *live.State { return rt.Agent(ref).Live }
	return rt
}
