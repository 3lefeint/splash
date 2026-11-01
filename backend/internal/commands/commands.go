// Package commands sends commands to an agent and keeps, per agent, the state of the last one.
package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/wire"
)

// Command states.
const (
	Pending = "pending"
	Done    = "done"
	Failed  = "failed"
)

// pendingTimeout is how long a command may stay unanswered before it counts as failed.
const pendingTimeout = 90 * time.Second

// Status is what the dashboard shows about the newest command.
type Status struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	State   string      `json:"state"`
	Message string      `json:"message,omitempty"`
	Applied []wire.Step `json:"applied,omitempty"`
	At      time.Time   `json:"at"`
}

// Conn is a connected agent that commands can be written to.
type Conn struct {
	AgentID string // the id the agent calls itself in its messages
	// Token is the registered agent whose token the connection used; 0 for the shared dev token.
	Token int64
	at    time.Time
	ws    *websocket.Conn
}

// agentKey tells agents apart. Two agents may call themselves the same, so the registered agent is part of it.
type agentKey struct {
	token int64
	id    string
}

// AgentStatus is what the dashboard shows about an agent: whether it is connected and how fresh its data is.
type AgentStatus struct {
	AgentID string `json:"agent_id"`
	// TokenID is the registered agent whose token the agent used; 0 for the shared dev token.
	TokenID int64  `json:"token_id"`
	Version string `json:"version,omitempty"`
	// Outdated is true when the version is older than the minimum the backend asks for.
	Outdated  bool      `json:"outdated,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Connected bool      `json:"connected"`
	Since     time.Time `json:"since"`     // connected since, or since when it is gone
	LastData  time.Time `json:"last_data"` // time of its last message
}

// Hub knows the connected agents.
type Hub struct {
	// MinVersion is the oldest agent version that is up to date, e.g. "0.2.0". Empty marks no agent as outdated.
	MinVersion string

	mu       sync.Mutex
	conns    map[*Conn]struct{}
	cur      map[agentKey]*Conn // the newest connection of each agent
	agents   map[agentKey]*AgentStatus
	statuses map[int64]*Status // the newest command of each registered agent
	now      func() time.Time
}

func New() *Hub {
	return &Hub{conns: map[*Conn]struct{}{}, cur: map[agentKey]*Conn{}, agents: map[agentKey]*AgentStatus{},
		statuses: map[int64]*Status{}, now: time.Now}
}

// Add registers a connection; call Remove when it ends.
func (h *Hub) Add(ws *websocket.Conn, agentID string, token int64) *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	c := &Conn{AgentID: agentID, Token: token, at: now, ws: ws}
	key := agentKey{token, agentID}
	h.conns[c] = struct{}{}
	h.cur[key] = c
	a := h.agents[key]
	if a == nil {
		a = &AgentStatus{AgentID: agentID, TokenID: token}
		h.agents[key] = a
	}
	a.Connected, a.Since, a.LastData = true, now, now
	return c
}

// Remove forgets a connection. The agent stays in the list as not connected unless a newer connection took over.
func (h *Hub) Remove(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	key := agentKey{c.Token, c.AgentID}
	if h.cur[key] == c {
		delete(h.cur, key)
		if a := h.agents[key]; a != nil {
			a.Connected, a.Since = false, h.now()
		}
	}
}

// Seen records a message of the agent behind c.
func (h *Hub) Seen(c *Conn, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := agentKey{c.Token, c.AgentID}
	if a := h.agents[key]; a != nil && h.cur[key] == c {
		a.LastData, a.SessionID = h.now(), sessionID
	}
}

// SetVersion records the version an agent reported in its hello.
func (h *Hub) SetVersion(c *Conn, version string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a := h.agents[agentKey{c.Token, c.AgentID}]; a != nil {
		a.Version = version
	}
}

// Agents lists the agents seen since the backend started, by registered agent.
func (h *Hub) Agents() []AgentStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]AgentStatus, 0, len(h.agents))
	for _, a := range h.agents {
		st := *a
		st.Outdated = Outdated(st.Version, h.MinVersion)
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TokenID != out[j].TokenID {
			return out[i].TokenID < out[j].TokenID
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out
}

// ErrNotConnected is returned when the agent a command is for is not connected.
var ErrNotConnected = errors.New("the agent is not connected")

// ErrBusy is returned while an earlier command is still running.
var ErrBusy = errors.New("the previous command is still running")

// Send gives the steps to the agent with this registered id (0 is the shared dev token) and returns the command id.
// Name is shown with the result.
func (h *Hub) Send(ctx context.Context, agent int64, name string, steps []wire.Step) (string, error) {
	return h.send(ctx, agent, name, wire.Command{Type: "command", Kind: wire.CommandPitMenu, Steps: steps})
}

// PitRequest asks the agent to toggle the pit request of its car.
func (h *Hub) PitRequest(ctx context.Context, agent int64, name string) (string, error) {
	return h.send(ctx, agent, name, wire.Command{Type: "command", Kind: wire.CommandPitRequest})
}

func (h *Hub) send(ctx context.Context, agent int64, name string, cmd wire.Command) (string, error) {
	h.mu.Lock()
	if st := h.statuses[agent]; st != nil && st.State == Pending && h.now().Sub(st.At) < pendingTimeout {
		h.mu.Unlock()
		return "", ErrBusy
	}
	var target *Conn
	for c := range h.conns {
		if c.Token == agent && (target == nil || c.at.After(target.at)) {
			target = c
		}
	}
	if target == nil {
		h.mu.Unlock()
		return "", ErrNotConnected
	}
	id := newID()
	cmd.ID = id
	h.statuses[agent] = &Status{ID: id, Name: name, State: Pending, At: h.now()}
	h.mu.Unlock()

	data, _ := json.Marshal(cmd)
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := target.ws.Write(wctx, websocket.MessageText, data); err != nil {
		h.mu.Lock()
		h.statuses[agent] = &Status{ID: id, Name: name, State: Failed, Message: "could not reach the agent", At: h.now()}
		h.mu.Unlock()
		return "", err
	}
	return id, nil
}

// Result records an agent's answer. Answers to older commands are ignored.
func (h *Hub) Result(r wire.CommandResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, st := range h.statuses {
		if st.ID != r.ID {
			continue
		}
		st.State, st.Message, st.Applied = Done, r.Message, r.Applied
		if !r.OK {
			st.State = Failed
		}
		return
	}
}

// Status returns the newest command of an agent or nil. A command nobody answered turns into a failure.
func (h *Hub) Status(agent int64) *Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.statuses[agent]
	if st == nil {
		return nil
	}
	s := *st
	if s.State == Pending && h.now().Sub(s.At) >= pendingTimeout {
		s.State, s.Message = Failed, "the agent did not answer"
	}
	return &s
}

// Outdated says whether version is older than min. Versions are dotted numbers with an optional leading v; a version
// or minimum that cannot be read never counts as outdated.
func Outdated(version, min string) bool {
	v, ok1 := parseVersion(version)
	m, ok2 := parseVersion(min)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < len(v) || i < len(m); i++ {
		var a, b int
		if i < len(v) {
			a = v[i]
		}
		if i < len(m) {
			b = m[i]
		}
		if a != b {
			return a < b
		}
	}
	return false
}

func parseVersion(s string) ([]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil, false
	}
	var out []int
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
