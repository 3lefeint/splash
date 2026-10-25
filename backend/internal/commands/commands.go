// Package commands sends commands to the driver agent and keeps the state of the last one.
package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
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
	Role    string
	AgentID string
	ws      *websocket.Conn
}

// AgentStatus is what the dashboard shows about an agent: whether it is connected and how fresh its data is.
type AgentStatus struct {
	AgentID   string    `json:"agent_id"`
	Role      string    `json:"role"`
	Version   string    `json:"version,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Connected bool      `json:"connected"`
	Since     time.Time `json:"since"`     // connected since, or since when it is gone
	LastData  time.Time `json:"last_data"` // time of its last message
}

// Hub knows the connected agents.
type Hub struct {
	mu     sync.Mutex
	conns  map[*Conn]struct{}
	cur    map[string]*Conn // the newest connection of each agent
	agents map[string]*AgentStatus
	status *Status
	now    func() time.Time
}

func New() *Hub {
	return &Hub{conns: map[*Conn]struct{}{}, cur: map[string]*Conn{}, agents: map[string]*AgentStatus{}, now: time.Now}
}

// Add registers a connection; call Remove when it ends.
func (h *Hub) Add(ws *websocket.Conn, role, agentID string) *Conn {
	c := &Conn{Role: role, AgentID: agentID, ws: ws}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = struct{}{}
	h.cur[agentID] = c
	now := h.now()
	a := h.agents[agentID]
	if a == nil {
		a = &AgentStatus{AgentID: agentID}
		h.agents[agentID] = a
	}
	a.Role, a.Connected, a.Since, a.LastData = role, true, now, now
	return c
}

// Remove forgets a connection. The agent stays in the list as not connected unless a newer connection took over.
func (h *Hub) Remove(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	if h.cur[c.AgentID] == c {
		delete(h.cur, c.AgentID)
		if a := h.agents[c.AgentID]; a != nil {
			a.Connected, a.Since = false, h.now()
		}
	}
}

// Seen records a message of the agent behind c.
func (h *Hub) Seen(c *Conn, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a := h.agents[c.AgentID]; a != nil && h.cur[c.AgentID] == c {
		a.LastData, a.SessionID = h.now(), sessionID
	}
}

// SetVersion records the version an agent reported in its hello.
func (h *Hub) SetVersion(c *Conn, version string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if a := h.agents[c.AgentID]; a != nil {
		a.Version = version
	}
}

// Agents lists the agents seen since the backend started, drivers first.
func (h *Hub) Agents() []AgentStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]AgentStatus, 0, len(h.agents))
	for _, a := range h.agents {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role == "driver"
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out
}

// ErrNoDriver is returned when no driver agent is connected.
var ErrNoDriver = errors.New("no driver agent is connected")

// ErrBusy is returned while an earlier command is still running.
var ErrBusy = errors.New("the previous command is still running")

// Send gives the steps to the driver agent and returns the command id. Name is shown with the result.
func (h *Hub) Send(ctx context.Context, name string, steps []wire.Step) (string, error) {
	return h.send(ctx, name, wire.Command{Type: "command", Kind: wire.CommandPitMenu, Steps: steps})
}

// PitRequest asks the driver agent to toggle the pit request of the car.
func (h *Hub) PitRequest(ctx context.Context, name string) (string, error) {
	return h.send(ctx, name, wire.Command{Type: "command", Kind: wire.CommandPitRequest})
}

func (h *Hub) send(ctx context.Context, name string, cmd wire.Command) (string, error) {
	h.mu.Lock()
	if h.status != nil && h.status.State == Pending && h.now().Sub(h.status.At) < pendingTimeout {
		h.mu.Unlock()
		return "", ErrBusy
	}
	var target *Conn
	for c := range h.conns {
		if c.Role == "driver" {
			target = c
		}
	}
	if target == nil {
		h.mu.Unlock()
		return "", ErrNoDriver
	}
	id := newID()
	cmd.ID = id
	h.status = &Status{ID: id, Name: name, State: Pending, At: h.now()}
	h.mu.Unlock()

	data, _ := json.Marshal(cmd)
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := target.ws.Write(wctx, websocket.MessageText, data); err != nil {
		h.mu.Lock()
		h.status = &Status{ID: id, Name: name, State: Failed, Message: "could not reach the agent", At: h.now()}
		h.mu.Unlock()
		return "", err
	}
	return id, nil
}

// Result records the agent's answer. Answers to older commands are ignored.
func (h *Hub) Result(r wire.CommandResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.status == nil || h.status.ID != r.ID {
		return
	}
	h.status.State, h.status.Message, h.status.Applied = Done, r.Message, r.Applied
	if !r.OK {
		h.status.State = Failed
	}
}

// Status returns the newest command or nil. A command nobody answered turns into a failure.
func (h *Hub) Status() *Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.status == nil {
		return nil
	}
	s := *h.status
	if s.State == Pending && h.now().Sub(s.At) >= pendingTimeout {
		s.State, s.Message = Failed, "the agent did not answer"
	}
	return &s
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
