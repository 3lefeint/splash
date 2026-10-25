// Package ingest receives agent messages over WebSocket, stores the durable ones
// and acknowledges them.
package ingest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
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
func (s *Server) handle(ctx context.Context, m wire.Message) (int64, error) {
	key := m.AgentID + "|" + m.SessionID
	last, known, err := s.lastSeq(ctx, key, m)
	if err != nil {
		return 0, err
	}
	if known && m.Seq <= last {
		return last, nil // resend of something already processed
	}
	switch b := m.Body.(type) {
	case wire.Scoring:
		s.Live.UpdateScoring(m.SessionID, m.ET, b)
	case wire.Telemetry:
		s.Live.UpdateTelemetry(m.SessionID, m.ET, b)
	case wire.Lap:
		if err := s.Store.SaveLap(ctx, m.SessionID, b); err != nil {
			return 0, err
		}
	case wire.Event:
		if err := s.Store.SaveEvent(ctx, m.SessionID, m.ET, b); err != nil {
			return 0, err
		}
	}
	// Only laps, events and hello are durable. Live samples are ephemeral, so
	// their seq stays in memory; after a restart the agent resends from the
	// last durable seq, which is harmless.
	switch m.Body.(type) {
	case wire.Lap, wire.Event, wire.Hello:
		if err := s.Store.SetProgress(ctx, m.AgentID, m.SessionID, m.Seq); err != nil {
			return 0, err
		}
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
