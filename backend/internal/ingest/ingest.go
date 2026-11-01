// Package ingest receives agent messages over WebSocket, stores the durable ones
// and acknowledges them.
package ingest

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/segments"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wear"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	// Path is the WebSocket endpoint for agents.
	Path = "/v1/agent/ws"
	// readLimit bounds one frame; a scoring message with a full field stays far below it.
	readLimit = 1 << 20
)

// Server handles agent connections.
type Server struct {
	Store store.Store
	// Live is the live state of agents that LiveFor does not serve; tests and the replay tool use it alone.
	Live *live.State
	// LiveFor returns the live state of an agent, so that every agent has its own session, scoring and telemetry.
	// Nil means all agents share Live.
	LiveFor func(AgentRef) *live.State
	Log     *slog.Logger
	// PingInterval is how often the server pings an agent connection; DeadAfter is how long a ping may stay
	// unanswered before the connection counts as dead and is closed. Zero values mean 20 s and 30 s.
	PingInterval, DeadAfter time.Duration
	// Maps records track outlines from the scoring stream. Nil disables it.
	Maps *MapRecorder
	// Pits learns the pit lane from the scoring stream. Nil disables it.
	Pits *PitRecorder
	// Field and Own measure how every car and the driven car spend a lap, section by section. Nil disables them.
	Field, Own *segments.Recorder
	// Cmds sends commands to the driver agent and takes its answers. Nil disables them.
	Cmds *commands.Hub

	// TeamID is the team this Server belongs to, for the log lines.
	TeamID int64
	// CountEvery is how often dropped and deduplicated samples are reported; zero means a minute.
	CountEvery time.Duration

	wear wear.Tracker

	agentsMu sync.Mutex
	agents   map[int64]*agentIngest

	mu       sync.Mutex
	received map[string]int64 // highest seq processed per agent and session

	countMu    sync.Mutex
	stale, dup int       // live samples older than the newest one, resent durable messages, since the last report
	countedAt  time.Time // when they were last reported
}

// agentIngest is what the ingest remembers about one agent between messages: its live state and the logs that follow
// the session the agent is in. Agents are independent, so one in another session does not disturb the others.
type agentIngest struct {
	live  *live.State
	stood stationary
	cond  conditionLog
	sess  sessionLog
}

// agent returns the ingest state of an agent and creates it on its first connection.
func (s *Server) agent(ref AgentRef) *agentIngest {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()
	if a, ok := s.agents[ref.ID]; ok {
		return a
	}
	lv := s.Live
	if s.LiveFor != nil {
		lv = s.LiveFor(ref)
	}
	if s.agents == nil {
		s.agents = map[int64]*agentIngest{}
	}
	a := &agentIngest{live: lv}
	s.agents[ref.ID] = a
	return a
}

// connStats is what the log lines about one agent connection need.
type connStats struct {
	start    time.Time
	agent    string
	ref      AgentRef // the agent the token belongs to; the zero value for the shared dev token
	st       *agentIngest
	msgs     int
	stored   int // durable messages stored on this connection
	dups     int // durable messages that were resends of ones already stored
	helloAt  time.Time
	resent   bool // the backlog line was written
	dead     atomic.Bool
	lastSeen string
}

// New returns the Server of one team. A nil logger discards logs.
func New(st store.Store, lv *live.State, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{Store: st, Live: lv, Log: log, received: map[string]int64{}}
}

// ServeHTTP upgrades the request and processes messages until the agent disconnects. The caller has already
// authenticated the agent and chosen the team this Server belongs to.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.Log.Debug("websocket handshake failed", "team", s.TeamID, "err", err)
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(readLimit)
	ctx := r.Context()
	cs := &connStats{start: time.Now(), ref: AgentFrom(r.Context())}
	cs.st = s.agent(cs.ref)
	reason := "connection closed"
	go s.keepAlive(ctx, c, cs)
	var conn *commands.Conn
	defer func() {
		if conn != nil {
			s.Cmds.Remove(conn)
		}
		if cs.dead.Load() {
			reason = "ping timeout"
		}
		if cs.agent == "" {
			s.Log.Debug("agent connection closed before any message", "team", s.TeamID, "reason", reason)
			return
		}
		s.Log.Info("agent disconnected", "team", s.TeamID, "agent", cs.agent, "reason", reason,
			"seconds", int(time.Since(cs.start).Seconds()), "messages", cs.msgs, "stored", cs.stored, "resent_duplicates", cs.dups)
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			switch cs := websocket.CloseStatus(err); {
			case ctx.Err() != nil:
				reason = "server shutdown or request canceled"
			case cs == websocket.StatusNormalClosure || cs == websocket.StatusGoingAway:
				reason = "closed by agent"
			default:
				reason = "connection lost: " + err.Error()
			}
			return
		}
		msg, err := wire.Decode(data)
		if err != nil {
			s.Log.Warn("invalid message", "team", s.TeamID, "err", err)
			reason = "invalid message"
			c.Close(websocket.StatusUnsupportedData, "invalid message")
			return
		}
		cs.msgs++
		s.Log.Debug("frame", "team", s.TeamID, "type", msg.Type, "bytes", len(data), "seq", msg.Seq)
		if cs.agent == "" {
			cs.agent = clip(msg.AgentID)
			s.Log.Info("agent connected", "team", s.TeamID, "agent", cs.agent, "agent_id", cs.ref.ID)
		}
		if conn == nil && s.Cmds != nil {
			conn = s.Cmds.Add(c, msg.AgentID, cs.ref.ID)
		}
		if conn != nil {
			s.Cmds.Seen(conn, msg.SessionID)
			if h, ok := msg.Body.(wire.Hello); ok {
				s.Cmds.SetVersion(conn, h.AgentVersion)
			}
		}
		ack, err := s.handle(ctx, msg, cs)
		if err != nil {
			s.Log.Error("store failed", "team", s.TeamID, "err", err, "type", msg.Type, "seq", msg.Seq)
			reason = "store failed"
			c.Close(websocket.StatusInternalError, "store failed")
			return
		}
		out, _ := json.Marshal(wire.NewAck(ack))
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = c.Write(wctx, websocket.MessageText, out)
		cancel()
		if err != nil {
			reason = "write failed: " + err.Error()
			return
		}
	}
}

// clip shortens a label that came from the agent before it is logged.
func clip(s string) string {
	if len(s) > 64 {
		return s[:64]
	}
	return s
}

// handle processes one message and returns the seq to acknowledge.
//
// Only laps, events and hello are sequenced and durable. Live samples (scoring,
// telemetry) are always processed, whatever their seq, because the newest
// sample wins anyway; they are acknowledged with the highest durable seq.
func (s *Server) handle(ctx context.Context, m wire.Message, cs *connStats) (int64, error) {
	key := m.AgentID + "|" + m.SessionID
	last, known, err := s.lastSeq(ctx, key, m)
	if err != nil {
		return 0, err
	}
	switch b := m.Body.(type) {
	case wire.CommandResult:
		if s.Cmds != nil {
			s.Cmds.Result(b)
		}
		return last, nil
	case wire.Scoring:
		if !cs.st.live.UpdateScoring(m.SessionID, m.ET, b) {
			s.count(&s.stale)
		}
		s.backlogDone(cs)
		s.touchSession(ctx, cs.st, m.SessionID, m.ET, b.Track, b.SessionType, "", -1)
		if s.Pits != nil {
			s.Pits.Observe(ctx, strconv.FormatInt(cs.ref.ID, 10)+"|"+m.SessionID, m.ET, b)
		}
		if s.Field != nil && b.TrackLength > 0 {
			for _, v := range b.Vehicles {
				if v.LapDist != nil {
					s.Field.Observe(m.SessionID, CarKey(v.VehicleName, v.CarID), b.TrackLength,
						segments.Sample{Dist: *v.LapDist, ET: m.ET, InPits: v.InPits, Fuel: -1, Brake: -1})
				}
			}
		}
		if s.Maps != nil {
			s.Maps.Observe(ctx, m.SessionID, b)
		}
		for _, w := range cs.st.stood.observe(m.SessionID, m.ET, b.Vehicles) {
			if err := s.Store.AddStopped(ctx, m.SessionID, w.CarID, w.VehicleName, w.Standing); err != nil {
				s.Log.Error("save stand-still", "team", s.TeamID, "err", err)
			}
		}
		return last, nil
	case wire.Telemetry:
		if !cs.st.live.UpdateTelemetry(m.SessionID, m.ET, b) {
			s.count(&s.stale)
		}
		s.backlogDone(cs)
		s.touchSession(ctx, cs.st, m.SessionID, m.ET, "", "", b.VehicleName, b.CarID)
		s.observeOwnLap(cs.st.live, m.SessionID, m.ET, b)
		if c := cs.st.cond.observe(m.SessionID, m.ET, b, cs.st.live.Snapshot().Scoring); c != nil {
			if err := s.Store.SaveLapConditions(ctx, *c); err != nil {
				s.Log.Error("save lap conditions", "team", s.TeamID, "err", err)
			}
		}
		s.observeWear(ctx, cs.st.live, m.SessionID, b)
		cs.st.stood.telemetry(m.SessionID, m.ET, b)
		return last, nil
	}
	if _, ok := m.Body.(wire.Hello); ok {
		cs.helloAt = time.Now()
	}
	if known && m.Seq <= last {
		if _, isHello := m.Body.(wire.Hello); !isHello {
			cs.dups++
			s.count(&s.dup)
		}
		return last, nil // resend of something already processed
	}
	switch b := m.Body.(type) {
	case wire.Lap:
		if err := s.Store.SaveLap(ctx, m.SessionID, b, cs.ref.ID); err != nil {
			return 0, err
		}
	case wire.Event:
		if err := s.Store.SaveEvent(ctx, m.SessionID, m.ET, b); err != nil {
			return 0, err
		}
	}
	if err := s.Store.SetProgress(ctx, m.AgentID, m.SessionID, m.Seq); err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.received[key] = m.Seq
	s.mu.Unlock()
	if _, ok := m.Body.(wire.Hello); !ok {
		cs.stored++
	}
	return m.Seq, nil
}

// lastSeq returns the highest processed seq, falling back to the stored progress
// after a restart.
func (s *Server) lastSeq(ctx context.Context, key string, m wire.Message) (int64, bool, error) {
	s.mu.Lock()
	v, ok := s.received[key]
	s.mu.Unlock()
	if ok {
		return v, true, nil
	}
	v, ok, err := s.Store.Progress(ctx, m.AgentID, m.SessionID)
	if err != nil {
		return 0, false, err
	}
	if ok {
		s.mu.Lock()
		s.received[key] = v
		s.mu.Unlock()
	}
	return v, ok, nil
}

// observeWear attributes the wear of the driven car to track sections and stores every completed lap.
func (s *Server) observeWear(ctx context.Context, lv *live.State, sessionID string, tel wire.Telemetry) {
	if tel.LapDist == nil {
		return
	}
	snap := lv.Snapshot()
	if snap.Scoring == nil || snap.Scoring.TrackLength <= 0 {
		return
	}
	key := sessionID + "|" + strconv.Itoa(tel.CarID) + "|" + tel.VehicleName
	// The plugin counts the lap in progress from 0, the stored laps by the number of completed laps.
	done := s.wear.Observe(key, tel.Lap+1, *tel.LapDist, snap.Scoring.TrackLength, tel.Wear)
	if done == nil {
		return
	}
	if err := s.Store.SaveLapWear(ctx, sessionID, tel.CarID, tel.VehicleName, *done); err != nil {
		s.Log.Error("save lap wear", "team", s.TeamID, "err", err)
	}
}

// touchSession records that the session is alive; the store is written every few seconds, not per sample.
func (s *Server) touchSession(ctx context.Context, a *agentIngest, id string, et float64, track, sessionType, car string, carID int) {
	info, prev, switched := a.sess.updateSwitch(id, time.Now(), et, track, sessionType, car, carID)
	if switched {
		if prev == "" {
			s.Log.Info("session started", "team", s.TeamID, "session", id)
		} else {
			s.Log.Info("session changed", "team", s.TeamID, "from", prev, "to", id)
		}
	}
	if info == nil {
		return
	}
	if err := s.Store.TouchSession(ctx, *info); err != nil {
		s.Log.Error("save session", "team", s.TeamID, "err", err)
	}
}

// backlogDone writes the line about a resent backlog. The agent sends what its spool still holds right after the
// hello and only then its live samples, so the first live sample after a hello marks the end of the backlog.
func (s *Server) backlogDone(cs *connStats) {
	if cs.resent || cs.helloAt.IsZero() {
		return
	}
	cs.resent = true
	if n := cs.stored + cs.dups; n > 0 {
		s.Log.Info("backlog resent", "team", s.TeamID, "agent", cs.agent, "stored", cs.stored, "duplicates", cs.dups)
	}
}

// count adds one to a counter of dropped samples and, once per interval, reports and resets both counters. A
// healthy connection drops nothing; a resend after a reconnect shows up here as duplicates.
func (s *Server) count(counter *int) {
	every := s.CountEvery
	if every <= 0 {
		every = time.Minute
	}
	s.countMu.Lock()
	*counter++
	now := time.Now()
	if s.countedAt.IsZero() {
		s.countedAt = now
	}
	var stale, dup int
	if now.Sub(s.countedAt) >= every {
		stale, dup, s.stale, s.dup, s.countedAt = s.stale, s.dup, 0, 0, now
	}
	s.countMu.Unlock()
	if stale > 0 || dup > 0 {
		s.Log.Warn("samples dropped or deduplicated", "team", s.TeamID, "older_live_samples", stale,
			"resent_duplicates", dup, "interval_seconds", int(every.Seconds()))
	}
}

// CarKey identifies a car the way the recorders do: vehicle names are shared by several cars, so the slot id counts too.
func CarKey(vehicle string, id int) string { return vehicle + "|" + strconv.Itoa(id) }

// observeOwnLap follows the driven car from its telemetry: fuel and brake along the lap.
func (s *Server) observeOwnLap(lv *live.State, session string, et float64, t wire.Telemetry) {
	if s.Own == nil || t.LapDist == nil {
		return
	}
	snap := lv.Snapshot()
	if snap.Scoring == nil || snap.Scoring.TrackLength <= 0 {
		return
	}
	pits := false
	for _, v := range snap.Scoring.Vehicles {
		if v.CarID == t.CarID && v.VehicleName == t.VehicleName {
			pits = v.InPits
		}
	}
	brake := -1.0
	if t.Brake != nil {
		brake = *t.Brake
	}
	s.Own.Observe(session, CarKey(t.VehicleName, t.CarID), snap.Scoring.TrackLength,
		segments.Sample{Dist: *t.LapDist, ET: et, InPits: pits, Fuel: t.Fuel, Brake: brake})
}

// Defaults of the keep-alive of an agent connection.
const (
	defaultPingInterval = 20 * time.Second
	defaultDeadAfter    = 30 * time.Second
)

// keepAlive pings the agent and closes the connection when a ping gets no answer in time, so a connection that a
// tunnel or a proxy cut without telling either side does not stay open for ever. It ends with ctx or the connection.
// The read loop of ServeHTTP is what receives the answers.
func (s *Server) keepAlive(ctx context.Context, c *websocket.Conn, cs *connStats) {
	every, dead := s.PingInterval, s.DeadAfter
	if every <= 0 {
		every = defaultPingInterval
	}
	if dead <= 0 {
		dead = defaultDeadAfter
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, dead)
		err := c.Ping(pctx)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				cs.dead.Store(true)
				s.Log.Warn("agent connection dead, closing", "team", s.TeamID, "agent", cs.agent, "err", err)
			}
			c.CloseNow()
			return
		}
		s.Log.Debug("ping answered", "team", s.TeamID, "agent", cs.agent)
	}
}
