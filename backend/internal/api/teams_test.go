package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/teams"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// twoTeams is a rig whose requests name their team in the header X-Team ("a" is the default team).
type twoTeams struct {
	*rig
	a, b *teams.Runtime
}

func newTwoTeams(t *testing.T) *twoTeams {
	t.Helper()
	r := newRig(t)
	other, err := r.db.CreateTeam(context.Background(), "other")
	if err != nil {
		t.Fatal(err)
	}
	r.api.Identify = func(req *http.Request) (Identity, error) {
		switch req.Header.Get("X-Team") {
		case "", "a":
			return Identity{TeamID: r.api.Teams.DefaultID(), Team: store.DefaultTeamName}, nil
		case "b":
			return Identity{TeamID: other.ID, Team: other.Name}, nil
		}
		return Identity{}, &statusError{http.StatusForbidden, "unknown team"}
	}
	return &twoTeams{r, r.rt, r.api.Teams.Get(other.ID)}
}

func (r *twoTeams) as(t *testing.T, team, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	req.Header.Set("X-Team", team)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func feedTeam(rt *teams.Runtime, fuel float64) {
	rt.Live.UpdateScoring("s1", 100, wire.Scoring{Track: "monza", SessionType: "race", EndET: 1000,
		Vehicles: []wire.ScoringVehicle{{CarID: 0, VehicleName: "me", Driver: "a", VehicleClass: "GT3", Place: 1}}})
	rt.Live.UpdateTelemetry("s1", 100, wire.Telemetry{CarID: 0, VehicleName: "me", Lap: 3, Fuel: fuel, FuelCapacity: 90,
		Wear: []float64{1, 1, 1, 1}})
}

func TestTeamsDoNotSeeEachOthersLiveStateOrStream(t *testing.T) {
	r := newTwoTeams(t)
	feedTeam(r.a, 50)
	if _, body := r.as(t, "b", "GET", "/api/state", ""); !strings.Contains(body, `"ready":false`) {
		t.Fatalf("team b sees the state of team a: %s", body)
	}
	feedTeam(r.b, 30)
	if _, body := r.as(t, "a", "GET", "/api/state", ""); !strings.Contains(body, `"fuel":50`) {
		t.Fatalf("a: %s", body)
	}
	if _, body := r.as(t, "b", "GET", "/api/state", ""); !strings.Contains(body, `"fuel":30`) {
		t.Fatalf("b: %s", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	open := func(team string) *bufio.Scanner {
		req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/stream", nil)
		req.Header.Set("X-Team", team)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return bufio.NewScanner(resp.Body)
	}
	sa, sb := open("a"), open("b")
	if st := readEvent(t, sa); st.Car.Fuel != 50 {
		t.Fatalf("stream a first event: %+v", st.Car)
	}
	if st := readEvent(t, sb); st.Car.Fuel != 30 {
		t.Fatalf("stream b first event: %+v", st.Car)
	}
	// A new sample of team b must never show up on the stream of team a, and the other way round.
	feedTeam(r.b, 29)
	feedTeam(r.a, 49)
	for i := 0; ; i++ {
		st := readEvent(t, sa)
		if st.Car.Fuel == 49 {
			break
		}
		if st.Car.Fuel != 50 || i > 20 {
			t.Fatalf("stream a delivered %+v", st.Car)
		}
	}
	for i := 0; ; i++ {
		st := readEvent(t, sb)
		if st.Car.Fuel == 29 {
			break
		}
		if st.Car.Fuel != 30 || i > 20 {
			t.Fatalf("stream b delivered %+v", st.Car)
		}
	}
}

func TestTeamsDoNotSeeEachOthersWrittenData(t *testing.T) {
	r := newTwoTeams(t)

	r.as(t, "a", "PUT", "/api/stint-plan", `[{"driver":"x","duration":3600}]`)
	if _, body := r.as(t, "b", "GET", "/api/stint-plan", ""); body != "[]" {
		t.Fatalf("stint plan: %s", body)
	}

	r.as(t, "a", "PUT", "/api/overrides/fuel", `{"value":42}`)
	if _, body := r.as(t, "b", "GET", "/api/overrides", ""); body != "{}" {
		t.Fatalf("overrides: %s", body)
	}
	r.feed(50)
	feedTeam(r.b, 30)
	if _, body := r.as(t, "b", "GET", "/api/state", ""); strings.Contains(body, `"fuel":42`) || !strings.Contains(body, `"fuel":30`) {
		t.Fatalf("override of team a applied to team b: %s", body)
	}

	r.as(t, "a", "PUT", "/api/pit-setups", `{"car":"gt3","name":"Max","steps":[{"category":"FUEL:","choice":"max"}]}`)
	if _, body := r.as(t, "b", "GET", "/api/pit-setups?car=gt3", ""); body != "[]" {
		t.Fatalf("pit setups: %s", body)
	}

	r.as(t, "a", "PUT", "/api/params?track=monza&car=me", `{"reserve":2}`)
	if code, _ := r.as(t, "b", "GET", "/api/params?track=monza&car=me", ""); code != 404 {
		t.Fatalf("params visible to team b: %d", code)
	}
	if _, body := r.as(t, "b", "GET", "/api/params", ""); body != "[]" {
		t.Fatalf("param list: %s", body)
	}

	r.a.Store.TouchSession(context.Background(), store.SessionInfo{SessionID: "s1", Track: "monza", LastSeen: time.Now()})
	if _, body := r.as(t, "b", "GET", "/api/sessions", ""); body != "[]" {
		t.Fatalf("sessions: %s", body)
	}
	lt := 90.0
	r.a.Store.SaveLap(context.Background(), "s1", wire.Lap{CarID: 0, VehicleName: "me", Lap: 1, LapTime: &lt})
	exportA := func() string { _, b := r.as(t, "a", "GET", "/api/sessions/s1/export?kind=laps", ""); return b }
	exportB := func() string { _, b := r.as(t, "b", "GET", "/api/sessions/s1/export?kind=laps", ""); return b }
	if got, other := strings.Count(exportA(), "\n"), strings.Count(exportB(), "\n"); got != 1 || other != 0 {
		t.Fatalf("export lines: team a %d, team b %d\n%s\n--\n%s", got, other, exportA(), exportB())
	}

	// The data of team a is untouched by all of that.
	if _, body := r.as(t, "a", "GET", "/api/stint-plan", ""); !strings.Contains(body, `"driver":"x"`) {
		t.Fatalf("team a lost its plan: %s", body)
	}
	if code, _ := r.as(t, "c", "GET", "/api/state", ""); code != http.StatusForbidden {
		t.Fatalf("unresolvable team: %d", code)
	}
}

func TestTrackDataIsSharedBetweenTeams(t *testing.T) {
	r := newTwoTeams(t)
	r.as(t, "a", "PUT", "/api/track-corners?track=ring", `[{"name":"T1"}]`)
	if _, body := r.as(t, "b", "GET", "/api/track-corners?track=ring", ""); !strings.Contains(body, "T1") {
		t.Fatalf("corners: %s", body)
	}
}

func TestStreamSendsAKeepAliveAtTheConfiguredInterval(t *testing.T) {
	r := newRig(t)
	r.api.Heartbeat = 60 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	var stamps []time.Time
	var comments, intervals int
	for sc.Scan() && len(stamps) < 4 {
		switch line := sc.Text(); {
		case line == ": ping":
			comments++
		case line == "data: 60": // the interval in milliseconds, for the page's watchdog
			intervals++
			stamps = append(stamps, time.Now())
		}
	}
	if len(stamps) < 4 || comments < 4 || intervals < 4 {
		t.Fatalf("stamps=%d comments=%d intervals=%d, err=%v", len(stamps), comments, intervals, sc.Err())
	}
	if gap := stamps[3].Sub(stamps[0]); gap < 120*time.Millisecond || gap > 600*time.Millisecond {
		t.Fatalf("three intervals of 60 ms took %v", gap)
	}
}

// A team without any data must get lists as [] and maps as {}, never null: the page calls .map and .length on them.
func TestEmptyTeamGetsNoNullInAnyListResponse(t *testing.T) {
	r := newTwoTeams(t)
	for _, path := range []string{
		"/api/params", "/api/stint-plan", "/api/overrides", "/api/sessions", "/api/tracks",
		"/api/track-corners?track=nowhere", "/api/pit-setups?car=gt3",
	} {
		code, body := r.as(t, "b", "GET", path, "")
		if code != 200 {
			t.Errorf("%s: %d %s", path, code, body)
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if hasNull(v) {
			t.Errorf("%s contains null: %s", path, body)
		}
		if body != "[]" && body != "{}" {
			t.Errorf("%s of an empty team should be [] or {}, got %s", path, body)
		}
	}
}

func hasNull(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		for _, e := range x {
			if hasNull(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasNull(e) {
				return true
			}
		}
	}
	return false
}
