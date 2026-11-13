package ingest

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
)

// twoAgents is a team with two agents that each have their own live state.
type twoAgents struct {
	st       store.Store
	one, two store.Agent
	c1, c2   *websocket.Conn
}

func newTwoAgents(t *testing.T) *twoAgents {
	t.Helper()
	ctx := context.Background()
	db := store.NewMemory()
	st := db.ForTeam(1)
	r := &twoAgents{st: st}
	r.one, _ = db.CreateAgent(ctx, "one", 1, store.HashToken("token-one"))
	r.two, _ = db.CreateAgent(ctx, "two", 1, store.HashToken("token-two"))
	srv := New(st, nil, nil)
	srv.LiveFor = func(AgentRef) *live.State { return &live.State{} }
	ts := httptest.NewServer(&Gateway{Dir: db, DefaultTeam: 1, Server: func(int64) *Server { return srv }})
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	r.c1, r.c2 = dial(t, url, "token-one"), dial(t, url, "token-two")
	return r
}

const (
	sessionID = "sw"
	// the rival is seen by both agents, "mine" is the car of agent one
	rivalEvent = `{"kind":"pit_entry","car_id":1,"vehicle_name":"rival"}`
	rivalLap   = `{"car_id":1,"vehicle_name":"rival","lap":%d,"lap_time":99.5}`
	ownTel     = `{"car_id":0,"vehicle_name":"mine","lap":2,"fuel":50,"fuel_capacity":90,"wear":[1,1,1,1]}`
	scoring    = `{"track":"ring","track_length":5000,"session_type":%q,"vehicles":[{"car_id":1,"vehicle_name":"rival","place":1,"pit_state":%d}]}`
)

// standStill lets both agents see the rival stand in its pit box from game time 30 to 40 and report its pit entry.
func (r *twoAgents) standStill(t *testing.T, sessionType string) {
	t.Helper()
	sendIn(t, r.c1, "one", sessionID, 1, "scoring", fmt.Sprintf(scoring, sessionType, 0))
	sendIn(t, r.c2, "two", sessionID, 1, "scoring", fmt.Sprintf(scoring, sessionType, 0))
	sendIn(t, r.c1, "one", sessionID, 2, "event", rivalEvent)
	sendIn(t, r.c2, "two", sessionID, 2, "event", rivalEvent)
	sendIn(t, r.c1, "one", sessionID, 3, "scoring", fmt.Sprintf(scoring, sessionType, 3))
	sendIn(t, r.c2, "two", sessionID, 3, "scoring", fmt.Sprintf(scoring, sessionType, 3))
	sendIn(t, r.c1, "one", sessionID, 4, "scoring", fmt.Sprintf(scoring, sessionType, 0))
	sendIn(t, r.c2, "two", sessionID, 4, "scoring", fmt.Sprintf(scoring, sessionType, 0))
}

func (r *twoAgents) stops(t *testing.T) []store.StopRecord {
	t.Helper()
	got, err := r.st.Stops(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Two agents that see the same stand-still count it once, in a race and in practice.
func TestAStandStillSeenByTwoAgentsIsCountedOnce(t *testing.T) {
	for _, typ := range []string{"race", "practice"} {
		t.Run(typ, func(t *testing.T) {
			r := newTwoAgents(t)
			sendIn(t, r.c1, "one", sessionID, 1, "telemetry", ownTel) // agent one drives, agent two only watches
			r.standStill(t, typ)
			got := r.stops(t)
			if len(got) != 1 || got[0].Stopped != 10 {
				t.Fatalf("stops %+v, want one stop with 10 s stand-still", got)
			}
		})
	}
}

// In a race only the driving agent writes. The spotter's laps and pit entries are left out.
func TestInARaceOnlyTheDrivingAgentWrites(t *testing.T) {
	r := newTwoAgents(t)
	sendIn(t, r.c1, "one", sessionID, 1, "telemetry", ownTel)
	sendIn(t, r.c1, "one", sessionID, 2, "scoring", fmt.Sprintf(scoring, "race", 0))
	sendIn(t, r.c2, "two", sessionID, 1, "scoring", fmt.Sprintf(scoring, "race", 0))
	sendIn(t, r.c2, "two", sessionID, 2, "lap", fmt.Sprintf(rivalLap, 1))
	sendIn(t, r.c2, "two", sessionID, 3, "event", rivalEvent)
	laps, _ := r.st.Laps(context.Background(), sessionID)
	if len(laps) != 0 || len(r.stops(t)) != 0 {
		t.Fatalf("the spotter wrote laps %+v stops %+v", laps, r.stops(t))
	}
	sendIn(t, r.c1, "one", sessionID, 3, "lap", fmt.Sprintf(rivalLap, 1))
	sendIn(t, r.c1, "one", sessionID, 4, "event", rivalEvent)
	laps, _ = r.st.Laps(context.Background(), sessionID)
	got := r.stops(t)
	if len(laps) != 1 || laps[0].AgentID != r.one.ID || len(got) != 1 || got[0].AgentID != r.one.ID {
		t.Fatalf("laps %+v stops %+v, want one each from agent one", laps, got)
	}
}

// No telemetry in a race (nobody is detected as the driver): the reference agent writes, so nothing is lost.
func TestWithoutADriverTheReferenceAgentWrites(t *testing.T) {
	r := newTwoAgents(t)
	sendIn(t, r.c1, "one", sessionID, 1, "scoring", fmt.Sprintf(scoring, "race", 0))
	sendIn(t, r.c2, "two", sessionID, 1, "scoring", fmt.Sprintf(scoring, "race", 0))
	sendIn(t, r.c2, "two", sessionID, 2, "lap", fmt.Sprintf(rivalLap, 1))
	sendIn(t, r.c1, "one", sessionID, 2, "lap", fmt.Sprintf(rivalLap, 1))
	laps, _ := r.st.Laps(context.Background(), sessionID)
	if len(laps) != 1 || laps[0].AgentID != r.one.ID {
		t.Fatalf("laps %+v, want one lap from the agent with the lowest id", laps)
	}
}

// In practice every agent writes the car it drives; a car nobody drives is written once, by the reference agent.
func TestInPracticeEveryAgentWritesItsOwnCar(t *testing.T) {
	r := newTwoAgents(t)
	sendIn(t, r.c1, "one", sessionID, 1, "telemetry", ownTel) // agent one drives "mine"
	sendIn(t, r.c2, "two", sessionID, 1, "telemetry", `{"car_id":2,"vehicle_name":"other","lap":2,"fuel":50,"fuel_capacity":90,"wear":[1,1,1,1]}`)
	for _, c := range []struct {
		conn  *websocket.Conn
		agent string
	}{{r.c1, "one"}, {r.c2, "two"}} {
		sendIn(t, c.conn, c.agent, sessionID, 2, "lap", `{"car_id":0,"vehicle_name":"mine","lap":1,"lap_time":90}`)
		sendIn(t, c.conn, c.agent, sessionID, 3, "lap", `{"car_id":2,"vehicle_name":"other","lap":1,"lap_time":91}`)
		sendIn(t, c.conn, c.agent, sessionID, 4, "lap", fmt.Sprintf(rivalLap, 1))
	}
	byCar := map[int]int64{}
	laps, _ := r.st.Laps(context.Background(), sessionID)
	for _, l := range laps {
		byCar[l.CarID] = l.AgentID
	}
	if len(laps) != 3 || byCar[0] != r.one.ID || byCar[2] != r.two.ID || byCar[1] != r.one.ID {
		t.Fatalf("laps %+v, want car 0 from agent one, car 2 from agent two, the rival from the reference agent", laps)
	}
}
