// Package ingest receives agent messages over WebSocket, stores the durable ones
// and acknowledges them.
package ingest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	Live  *live.State
	// Token is the shared agent secret. Empty disables the check.
	Token string
	Log   *slog.Logger
	// Maps records track outlines from the scoring stream. Nil disables it.
	Maps *MapRecorder
	// Pits learns the pit lane from the scoring stream. Nil disables it.
	Pits *PitRecorder
	// Field and Own measure how every car and the driven car spend a lap, section by section. Nil disables them.
	Field, Own *segments.Recorder
	// Cmds sends commands to the driver agent and takes its answers. Nil disables them.
	Cmds *commands.Hub

	wear  wear.Tracker
	stood stationary
	cond  conditionLog
	sess  sessionLog

	mu       sync.Mutex
	received map[string]int64 // highest seq processed per agent and session
}

// New returns a Server. A nil logger discards logs.
func New(st store.Store, lv *live.State, token string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{Store: st, Live: lv, Token: token, Log: log, received: map[string]int64{}}
}

func (s *Server) authorized(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

// ServeHTTP upgrades the request and processes messages until the agent disconnects.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(readLimit)
	ctx := r.Context()
	var conn *commands.Conn
	defer func() {
		if conn != nil {
			s.Cmds.Remove(conn)
		}
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		msg, err := wire.Decode(data)
		if err != nil {
			s.Log.Warn("invalid message", "err", err)
			c.Close(websocket.StatusUnsupportedData, "invalid message")
			return
		}
		if conn == nil && s.Cmds != nil {
			conn = s.Cmds.Add(c, msg.Role, msg.AgentID)
		}
		if conn != nil {
			s.Cmds.Seen(conn, msg.SessionID)
			if h, ok := msg.Body.(wire.Hello); ok {
				s.Cmds.SetVersion(conn, h.AgentVersion)
			}
		}
		ack, err := s.handle(ctx, msg)
		if err != nil {
			s.Log.Error("store failed", "err", err, "type", msg.Type, "seq", msg.Seq)
			c.Close(websocket.StatusInternalError, "store failed")
			return
		}
		out, _ := json.Marshal(wire.NewAck(ack))
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = c.Write(wctx, websocket.MessageText, out)
		cancel()
		if err != nil {
			return
		}
	}
}

// handle processes one message and returns the seq to acknowledge.
//
// Only laps, events and hello are sequenced and durable. Live samples (scoring,
// telemetry) are always processed, whatever their seq, because the newest
// sample wins anyway; they are acknowledged with the highest durable seq.
func (s *Server) handle(ctx context.Context, m wire.Message) (int64, error) {
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
		s.Live.UpdateScoring(m.SessionID, m.ET, b)
		s.touchSession(ctx, m.SessionID, m.ET, b.Track, b.SessionType, "", -1)
		if s.Pits != nil {
			s.Pits.Observe(ctx, b)
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
		for _, w := range s.stood.observe(m.SessionID, m.ET, b.Vehicles) {
			if err := s.Store.AddStopped(ctx, m.SessionID, w.CarID, w.VehicleName, w.Standing); err != nil {
				s.Log.Warn("save stand-still", "err", err)
			}
		}
		return last, nil
	case wire.Telemetry:
		s.Live.UpdateTelemetry(m.SessionID, m.ET, b)
		s.touchSession(ctx, m.SessionID, m.ET, "", "", b.VehicleName, b.CarID)
		s.observeOwnLap(m.SessionID, m.ET, b)
		if c := s.cond.observe(m.SessionID, m.ET, b, s.Live.Snapshot().Scoring); c != nil {
			if err := s.Store.SaveLapConditions(ctx, *c); err != nil {
				s.Log.Warn("save lap conditions", "err", err)
			}
		}
		s.observeWear(ctx, m.SessionID, b)
		s.stood.telemetry(m.SessionID, m.ET, b)
		return last, nil
	}
	if known && m.Seq <= last {
		return last, nil // resend of something already processed
	}
	switch b := m.Body.(type) {
	case wire.Lap:
		if err := s.Store.SaveLap(ctx, m.SessionID, b); err != nil {
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
func (s *Server) observeWear(ctx context.Context, sessionID string, tel wire.Telemetry) {
	if tel.LapDist == nil {
		return
	}
	snap := s.Live.Snapshot()
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
		s.Log.Warn("save lap wear", "err", err)
	}
}

// touchSession records that the session is alive; the store is written every few seconds, not per sample.
func (s *Server) touchSession(ctx context.Context, id string, et float64, track, sessionType, car string, carID int) {
	info := s.sess.update(id, time.Now(), et, track, sessionType, car, carID)
	if info == nil {
		return
	}
	if err := s.Store.TouchSession(ctx, *info); err != nil {
		s.Log.Warn("save session", "err", err)
	}
}

// CarKey identifies a car the way the recorders do: vehicle names are shared by several cars, so the slot id counts too.
func CarKey(vehicle string, id int) string { return vehicle + "|" + strconv.Itoa(id) }

// observeOwnLap follows the driven car from its telemetry: fuel and brake along the lap.
func (s *Server) observeOwnLap(session string, et float64, t wire.Telemetry) {
	if s.Own == nil || t.LapDist == nil {
		return
	}
	snap := s.Live.Snapshot()
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
