package api

import (
	"strings"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/teams"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// feedAgent gives an agent a session with one car and the fuel it has.
func feedAgent(a *teams.AgentRT, session string, et, fuel float64) {
	a.Live.UpdateScoring(session, et, wire.Scoring{Track: "monza", SessionType: "practice", EndET: 5000,
		Vehicles: []wire.ScoringVehicle{{CarID: 0, VehicleName: "bmw", Driver: "x", VehicleClass: "GT3", Place: 1}}})
	a.Live.UpdateTelemetry(session, et, wire.Telemetry{CarID: 0, VehicleName: "bmw", Lap: 3, Fuel: fuel, FuelCapacity: 90,
		Wear: []float64{1, 1, 1, 1}})
}

func TestStateIsPerAgent(t *testing.T) {
	r := newRig(t)
	one := r.rt.Agent(ingest.AgentRef{ID: 1, Name: "one"})
	two := r.rt.Agent(ingest.AgentRef{ID: 2, Name: "two"})
	feedAgent(one, "session-a", 100, 50)
	feedAgent(two, "session-b", 100, 30)

	for path, want := range map[string][]string{
		"/api/state?agent=1": {`"agent_id":1`, `"agent_name":"one"`, `"session_id":"session-a"`, `"fuel":50`},
		"/api/state?agent=2": {`"agent_id":2`, `"agent_name":"two"`, `"session_id":"session-b"`, `"fuel":30`},
	} {
		code, body := r.do(t, "GET", path, "")
		if code != 200 {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %s in %s", path, w, body)
			}
		}
	}
	if code, _ := r.do(t, "GET", "/api/state?agent=9", ""); code != 404 {
		t.Errorf("unknown agent: %d, want 404", code)
	}
	if code, _ := r.do(t, "GET", "/api/state?agent=x", ""); code != 400 {
		t.Errorf("malformed agent: %d, want 400", code)
	}
}

// Without ?agent= a request follows one active agent and stays with it, until browsers are tied to their agent.
func TestStateWithoutAgentStaysOnOneActiveAgent(t *testing.T) {
	r := newRig(t)
	if _, body := r.do(t, "GET", "/api/state", ""); !strings.Contains(body, `"ready":false`) {
		t.Fatalf("a team without agents: %s", body)
	}
	one := r.rt.Agent(ingest.AgentRef{ID: 1, Name: "one"})
	two := r.rt.Agent(ingest.AgentRef{ID: 2, Name: "two"})
	feedAgent(two, "session-b", 100, 30)
	feedAgent(one, "session-a", 100, 50)
	for i := 0; i < 3; i++ { // both are active: the view must not jump between them
		if _, body := r.do(t, "GET", "/api/state", ""); !strings.Contains(body, `"agent_id":1`) {
			t.Fatalf("round %d: expected agent 1, the lowest id of the active ones: %s", i, body)
		}
		feedAgent(two, "session-b", 110+float64(i), 29)
	}

	// Agent 1 falls silent while agent 2 keeps sending: the view moves to agent 2.
	r.rt.ActiveWindow = 100 * time.Millisecond
	time.Sleep(150 * time.Millisecond)
	feedAgent(two, "session-b", 300, 25)
	if _, body := r.do(t, "GET", "/api/state", ""); !strings.Contains(body, `"agent_id":2`) {
		t.Fatalf("agent 1 is silent: %s", body)
	}
	// Nobody is active any more: the one with the newest sample.
	time.Sleep(150 * time.Millisecond)
	if _, body := r.do(t, "GET", "/api/state", ""); !strings.Contains(body, `"agent_id":2`) {
		t.Fatalf("nobody active: %s", body)
	}
}

func TestOverridesArePerAgent(t *testing.T) {
	r := newRig(t)
	feedAgent(r.rt.Agent(ingest.AgentRef{ID: 1, Name: "one"}), "session-a", 100, 50)
	feedAgent(r.rt.Agent(ingest.AgentRef{ID: 2, Name: "two"}), "session-b", 100, 30)

	if code, body := r.do(t, "PUT", "/api/overrides/fuel?agent=1", `{"value":70}`); code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	if _, body := r.do(t, "GET", "/api/overrides?agent=1", ""); !strings.Contains(body, `"fuel"`) {
		t.Errorf("agent 1 has no override: %s", body)
	}
	if _, body := r.do(t, "GET", "/api/overrides?agent=2", ""); strings.Contains(body, `"fuel"`) {
		t.Errorf("agent 2 sees the override of agent 1: %s", body)
	}
	if _, body := r.do(t, "GET", "/api/state?agent=1", ""); !strings.Contains(body, `"fuel":70`) {
		t.Errorf("agent 1 state ignores its override: %s", body)
	}
	if _, body := r.do(t, "GET", "/api/state?agent=2", ""); !strings.Contains(body, `"fuel":30`) {
		t.Errorf("agent 2 state shows the override of agent 1: %s", body)
	}
	if code, _ := r.do(t, "DELETE", "/api/overrides/fuel?agent=2", ""); code != 204 {
		t.Errorf("delete: %d", code)
	}
	if _, body := r.do(t, "GET", "/api/overrides?agent=1", ""); !strings.Contains(body, `"fuel"`) {
		t.Errorf("deleting the override of agent 2 removed the one of agent 1: %s", body)
	}
}
