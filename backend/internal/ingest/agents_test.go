package ingest

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
)

// Two members train at the same time, each in a session of their own. Neither may disturb the other's live state.
func TestAgentsKeepTheirOwnLiveStateAndSession(t *testing.T) {
	ctx := context.Background()
	db := store.NewMemory()
	st := db.ForTeam(1)
	one, _ := db.CreateAgent(ctx, "one", 1, store.HashToken("token-one"))
	two, _ := db.CreateAgent(ctx, "two", 1, store.HashToken("token-two"))

	var mu sync.Mutex
	lives := map[int64]*live.State{}
	srv := New(st, nil, nil)
	srv.LiveFor = func(ref AgentRef) *live.State {
		mu.Lock()
		defer mu.Unlock()
		lives[ref.ID] = &live.State{}
		return lives[ref.ID]
	}
	ts := httptest.NewServer(&Gateway{Dir: db, DefaultTeam: 1, Server: func(int64) *Server { return srv }})
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	c1, c2 := dial(t, url, "token-one"), dial(t, url, "token-two")
	tel := `{"car_id":0,"vehicle_name":"bmw","lap":3,"fuel":%d,"fuel_capacity":90,"wear":[1,1,1,1]}`
	sendIn(t, c1, "a", "session-a", 1, "telemetry", fmt.Sprintf(tel, 50))
	sendIn(t, c2, "b", "session-b", 1, "telemetry", fmt.Sprintf(tel, 30))
	sendIn(t, c1, "a", "session-a", 2, "telemetry", fmt.Sprintf(tel, 49))
	sendIn(t, c2, "b", "session-b", 2, "telemetry", fmt.Sprintf(tel, 29))

	mu.Lock()
	defer mu.Unlock()
	if len(lives) != 2 {
		t.Fatalf("%d live states, want one per agent", len(lives))
	}
	for _, c := range []struct {
		agent   int64
		session string
		fuel    float64
	}{{one.ID, "session-a", 49}, {two.ID, "session-b", 29}} {
		snap := lives[c.agent].Snapshot()
		if snap.SessionID != c.session || snap.Telemetry == nil || snap.Telemetry.Fuel != c.fuel {
			t.Errorf("agent %d: session %q, telemetry %+v; want %q with fuel %v", c.agent, snap.SessionID, snap.Telemetry, c.session, c.fuel)
		}
	}
}

// The same agent connecting again continues with its live state.
func TestAnAgentKeepsItsLiveStateAcrossConnections(t *testing.T) {
	ctx := context.Background()
	db := store.NewMemory()
	ag, _ := db.CreateAgent(ctx, "one", 1, store.HashToken("token-one"))
	made := 0
	srv := New(db.ForTeam(1), nil, nil)
	srv.LiveFor = func(AgentRef) *live.State { made++; return &live.State{} }
	ts := httptest.NewServer(&Gateway{Dir: db, DefaultTeam: 1, Server: func(int64) *Server { return srv }})
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	tel := `{"car_id":0,"vehicle_name":"bmw","lap":3,"fuel":%d,"fuel_capacity":90,"wear":[1,1,1,1]}`

	sendIn(t, dial(t, url, "token-one"), "a", "session-a", 1, "telemetry", fmt.Sprintf(tel, 50))
	sendIn(t, dial(t, url, "token-one"), "a", "session-a", 2, "telemetry", fmt.Sprintf(tel, 49))
	if made != 1 {
		t.Fatalf("%d live states created for agent %d, want 1", made, ag.ID)
	}
}
