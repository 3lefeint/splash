// Package teams holds the runtime of every team: live state, engine, command hub and agent ingest. A runtime is
// created when a team is first needed, so teams never share anything in memory.
package teams

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/segments"
	"github.com/3lefeint/splash/backend/internal/store"
)

// Runtime is everything one team needs while the backend runs.
type Runtime struct {
	ID int64
	// Store sees only the data of this team (and the track data all teams share).
	Store  store.Store
	Live   *live.State
	Cmds   *commands.Hub
	Engine *engine.Engine
	Ingest *ingest.Server
}

// Registry creates the runtimes on demand and is safe for concurrent use.
type Registry struct {
	Dir store.Directory
	Log *slog.Logger
	// PingInterval and DeadAfter configure the keep-alive of the agent connections, see ingest.Server.
	PingInterval, DeadAfter time.Duration

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
	lv := &live.State{}
	cmds := commands.New()
	eng := engine.New(st, lv)
	eng.Commands = cmds
	eng.Log = r.Log
	ing := ingest.New(st, lv, r.Log)
	ing.PingInterval, ing.DeadAfter, ing.TeamID = r.PingInterval, r.DeadAfter, id
	ing.Maps = ingest.NewMapRecorder(st, r.Log)
	ing.Cmds = cmds
	ing.Pits = ingest.NewPitRecorder(st, r.Log)
	ing.Field, ing.Own = &segments.Recorder{}, &segments.Recorder{}
	eng.Field, eng.Own, eng.Pits = ing.Field, ing.Own, ing.Pits
	return &Runtime{ID: id, Store: st, Live: lv, Cmds: cmds, Engine: eng, Ingest: ing}
}
