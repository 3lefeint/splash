package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/commands"
	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wire"
)

type rig struct {
	st  *store.Memory
	lv  *live.State
	api *API
	srv *httptest.Server
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st := store.NewMemory()
	lv := &live.State{}
	a := New(st, engine.New(st, lv))
	a.Hub.Interval = 20 * time.Millisecond
	mux := http.NewServeMux()
	a.Register(mux)
	srv := httptest.NewServer(mux)
	ctx, cancel := context.WithCancel(context.Background())
	go a.Hub.Run(ctx)
	t.Cleanup(func() { cancel(); srv.Close() })
	return &rig{st, lv, a, srv}
}

func (r *rig) do(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func (r *rig) feed(fuel float64) {
	r.lv.UpdateScoring("s1", 100, wire.Scoring{Track: "monza", SessionType: "race", EndET: 1000,
		Vehicles: []wire.ScoringVehicle{{CarID: 0, VehicleName: "me", Driver: "a", VehicleClass: "GT3", Place: 1}}})
	r.lv.UpdateTelemetry("s1", 100, wire.Telemetry{CarID: 0, VehicleName: "me", Lap: 3, Fuel: fuel, FuelCapacity: 90,
		Wear: []float64{1, 1, 1, 1}})
}

func TestParamsRoundTrip(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "GET", "/api/params?track=monza&car=me", ""); code != 404 {
		t.Fatalf("code=%d", code)
	}
	code, body := r.do(t, "PUT", "/api/params?track=monza&car=me", `{"pit_through_green":21.5,"refuel_rate":2.5,"reserve":2,"parallel_service":true}`)
	if code != 200 {
		t.Fatalf("code=%d body=%s", code, body)
	}
	code, body = r.do(t, "GET", "/api/params?track=monza&car=me", "")
	var p store.Params
	if json.Unmarshal([]byte(body), &p); code != 200 || p.PitThroughGreen != 21.5 || !p.ParallelService || p.Track != "monza" {
		t.Fatalf("code=%d p=%+v", code, p)
	}
	if code, body := r.do(t, "GET", "/api/params", ""); code != 200 || !strings.HasPrefix(body, "[{") {
		t.Fatalf("list: %d %s", code, body)
	}
}

func TestParamsValidation(t *testing.T) {
	r := newRig(t)
	cases := map[string]string{
		"no track":      `{"car":"me"}`,
		"negative":      `{"track":"t","car":"c","reserve":-1}`,
		"unknown field": `{"track":"t","car":"c","bogus":1}`,
		"not json":      `nope`,
	}
	for name, body := range cases {
		if code, _ := r.do(t, "PUT", "/api/params", body); code != 400 {
			t.Errorf("%s: code=%d", name, code)
		}
	}
}

func TestStintPlan(t *testing.T) {
	r := newRig(t)
	if _, body := r.do(t, "GET", "/api/stint-plan", ""); body != "[]" {
		t.Fatalf("empty plan must be [], got %s", body)
	}
	code, _ := r.do(t, "PUT", "/api/stint-plan", `[{"driver":"a","duration":3900},{"driver":"b","duration":3900}]`)
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if _, body := r.do(t, "GET", "/api/stint-plan", ""); !strings.Contains(body, `"driver":"b"`) {
		t.Fatalf("body=%s", body)
	}
	for _, bad := range []string{`[{"driver":"","duration":10}]`, `[{"driver":"a","duration":0}]`, `[{"driver":"a","duration":-5}]`} {
		if code, _ := r.do(t, "PUT", "/api/stint-plan", bad); code != 400 {
			t.Errorf("%s: code=%d", bad, code)
		}
	}
	if _, body := r.do(t, "GET", "/api/stint-plan", ""); !strings.Contains(body, `"driver":"a"`) {
		t.Fatalf("a rejected write must not change the plan: %s", body)
	}
}

func TestOverrides(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "PUT", "/api/overrides/fuel", `{"value":42}`); code != 200 {
		t.Fatalf("code=%d", code)
	}
	if _, body := r.do(t, "GET", "/api/overrides", ""); !strings.Contains(body, `"value":42`) {
		t.Fatalf("body=%s", body)
	}
	if code, _ := r.do(t, "PUT", "/api/overrides/bogus", `{"value":1}`); code != 404 {
		t.Fatalf("code=%d", code)
	}
	if code, _ := r.do(t, "PUT", "/api/overrides/fuel", `{"value":-1}`); code != 400 {
		t.Fatalf("code=%d", code)
	}
	if code, _ := r.do(t, "DELETE", "/api/overrides/fuel", ""); code != 204 {
		t.Fatalf("code=%d", code)
	}
	if _, body := r.do(t, "GET", "/api/overrides", ""); body != "{}" {
		t.Fatalf("body=%s", body)
	}
}

func TestStateEndpoint(t *testing.T) {
	r := newRig(t)
	_, body := r.do(t, "GET", "/api/state", "")
	if !strings.Contains(body, `"ready":false`) {
		t.Fatalf("body=%s", body)
	}
	r.feed(50)
	_, body = r.do(t, "GET", "/api/state", "")
	if !strings.Contains(body, `"ready":true`) || !strings.Contains(body, `"fuel":50`) {
		t.Fatalf("body=%s", body)
	}
}

func readEvent(t *testing.T, sc *bufio.Scanner) engine.State {
	t.Helper()
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			var st engine.State
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &st); err != nil {
				t.Fatal(err)
			}
			return st
		}
	}
	t.Fatalf("stream ended: %v", sc.Err())
	return engine.State{}
}

func TestStreamSendsCurrentStateThenUpdates(t *testing.T) {
	r := newRig(t)
	r.feed(50)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	if st := readEvent(t, sc); !st.Ready || st.Car.Fuel != 50 {
		t.Fatalf("first event: %+v", st)
	}
	r.feed(49) // newer sample would need a higher ET; same ET replaces
	for i := 0; i < 20; i++ {
		if st := readEvent(t, sc); st.Car.Fuel == 49 {
			return
		}
	}
	t.Fatal("update never arrived")
}

func TestTrackMapEndpoint(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "GET", "/api/track-map", ""); code != 400 {
		t.Fatalf("missing track: %d", code)
	}
	if code, _ := r.do(t, "GET", "/api/track-map?track=ring", ""); code != 404 {
		t.Fatalf("unknown track: %d", code)
	}
	r.st.SaveTrackMap(context.Background(), trackmap.Map{Track: "ring", Length: 1000, Points: [][2]float64{{1, 2}, {3, 4}}})
	code, body := r.do(t, "GET", "/api/track-map?track=ring", "")
	if code != 200 || !strings.Contains(body, `"points":[[1,2],[3,4]]`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := r.do(t, "DELETE", "/api/track-map?track=ring", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := r.do(t, "GET", "/api/track-map?track=ring", ""); code != 404 {
		t.Fatalf("after delete: %d", code)
	}
}

func TestPitSetupsAreStoredListedAndDeleted(t *testing.T) {
	r := newRig(t)
	body := `{"car":"gt3","name":"Max","steps":[{"category":"FUEL:","choice":"max"},{"category":"TIRES:","choice":"medium"}]}`
	if code, _ := r.do(t, "PUT", "/api/pit-setups", body); code != 200 {
		t.Fatalf("put %d", code)
	}
	if code, _ := r.do(t, "PUT", "/api/pit-setups", `{"car":"gt3","name":"","steps":[]}`); code != 400 {
		t.Fatalf("invalid put %d", code)
	}
	code, out := r.do(t, "GET", "/api/pit-setups?car=gt3", "")
	var list []map[string]any
	if json.Unmarshal([]byte(out), &list); code != 200 || len(list) != 1 || list[0]["name"] != "Max" {
		t.Fatalf("list=%v", out)
	}
	if code, _ := r.do(t, "DELETE", "/api/pit-setups?car=gt3&name=Max", ""); code != 204 {
		t.Fatalf("delete %d", code)
	}
	step := `{"name":"Max","steps":[{"category":"FUEL:","choice":"max"}]}`
	if code, _ := r.do(t, "POST", "/api/pit-setups/apply", step); code != 503 {
		t.Fatalf("without hub %d", code)
	}
	// without a connected driver agent the command cannot be sent
	r.api.Cmds = commands.New()
	if code, _ := r.do(t, "POST", "/api/pit-setups/apply", step); code != 409 {
		t.Fatalf("apply %d", code)
	}
}

func TestMapProgressEndpoint(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "GET", "/api/track-map/progress?track=ring", ""); code != 404 {
		t.Fatalf("without recorder %d", code)
	}
	r.api.MapProgress = func(track string) *trackmap.Progress {
		if track != "ring" {
			return nil
		}
		return &trackmap.Progress{Track: "ring", Covered: 0.4, Points: [][2]float64{{0, 0}, {1, 1}}}
	}
	code, body := r.do(t, "GET", "/api/track-map/progress?track=ring", "")
	if code != 200 || !strings.Contains(body, `"covered":0.4`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := r.do(t, "GET", "/api/track-map/progress?track=other", ""); code != 404 {
		t.Fatalf("other track %d", code)
	}
}

func TestSessionsListAndCsvExport(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.st.TouchSession(ctx, store.SessionInfo{SessionID: "s1", Track: "monza", SessionType: "race", Car: "me", LastSeen: time.Now()})
	lt := 98.4
	r.st.SaveLap(ctx, "s1", wire.Lap{CarID: 0, VehicleName: "me", Lap: 1, LapTime: &lt, Driver: "a, b"})
	r.st.SaveLap(ctx, "s1", wire.Lap{CarID: 7, VehicleName: "rival", Lap: 1})
	amb := 20.5
	r.st.SaveLapConditions(ctx, store.LapConditions{SessionID: "s1", CarID: 0, VehicleName: "me", Lap: 1, Ambient: &amb,
		Surface: []float64{80, 81, 82, 83}, CompoundFront: "Medium"})
	code, out := r.do(t, "GET", "/api/sessions", "")
	if code != 200 || !strings.Contains(out, `"track":"monza"`) || !strings.Contains(out, `"laps":1`) || !strings.Contains(out, `"conditions":1`) {
		t.Fatalf("%d %s", code, out)
	}
	code, out = r.do(t, "GET", "/api/sessions/s1/export?kind=laps", "")
	lines := strings.Split(out, "\n")
	if code != 200 || len(lines) != 3 || !strings.HasPrefix(lines[0], "car_id,vehicle_name") || !strings.Contains(lines[1], `"a, b"`) || !strings.Contains(lines[1], "98.4") {
		t.Fatalf("%d %q", code, out)
	}
	code, out = r.do(t, "GET", "/api/sessions/s1/export?kind=conditions", "")
	if code != 200 || !strings.Contains(out, "surface_rr") || !strings.Contains(out, "20.5") || !strings.Contains(out, "Medium") {
		t.Fatalf("%d %q", code, out)
	}
	if code, _ := r.do(t, "GET", "/api/sessions/s1/export?kind=stops", ""); code != 200 {
		t.Fatalf("stops %d", code)
	}
	if code, _ := r.do(t, "GET", "/api/sessions/s1/export?kind=nope", ""); code != 400 {
		t.Fatalf("bad kind %d", code)
	}
}

func TestPitRequestNeedsADriverAgent(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "POST", "/api/pit-request", ""); code != 503 {
		t.Fatalf("without hub %d", code)
	}
	r.api.Cmds = commands.New()
	if code, _ := r.do(t, "POST", "/api/pit-request?name=Stopp", ""); code != 409 {
		t.Fatalf("without agent %d", code)
	}
}

func TestCornersEndpoint(t *testing.T) {
	r := newRig(t)
	if code, _ := r.do(t, "GET", "/api/track-corners", ""); code != 400 {
		t.Fatalf("missing track: %d", code)
	}
	if code, body := r.do(t, "GET", "/api/track-corners?track=ring", ""); code != 200 || strings.TrimSpace(body) != "[]" {
		t.Fatalf("empty: %d %s", code, body)
	}
	code, body := r.do(t, "PUT", "/api/track-corners?track=ring", `[{"name":"Hatzenbach","from":1200.5,"to":1400,"label":[5,-6]},{"name":"Flugplatz","from":null,"to":null,"label":null}]`)
	if code != 200 || !strings.Contains(body, `"from":1200.5`) || !strings.Contains(body, `"label":[5,-6]`) {
		t.Fatalf("put: %d %s", code, body)
	}
	if code, body := r.do(t, "GET", "/api/track-corners?track=ring", ""); code != 200 || !strings.Contains(body, "Flugplatz") {
		t.Fatalf("get: %d %s", code, body)
	}
	for _, bad := range []string{`[{"name":"","from":1}]`, `[{"name":"x","from":-5}]`, `[{"name":"x","to":1e9}]`, `[{"name":"x","label":[1e12,0]}]`} {
		if code, _ := r.do(t, "PUT", "/api/track-corners?track=ring", bad); code != 400 {
			t.Fatalf("%s: %d", bad, code)
		}
	}
}

func TestTracksEndpoint(t *testing.T) {
	r := newRig(t)
	if code, body := r.do(t, "GET", "/api/tracks", ""); code != 200 || strings.TrimSpace(body) != "[]" {
		t.Fatalf("empty: %d %s", code, body)
	}
	r.st.SaveTrackMap(context.Background(), trackmap.Map{Track: "ring", Length: 1000, Points: [][2]float64{{1, 2}, {3, 4}}})
	if code, body := r.do(t, "GET", "/api/tracks", ""); code != 200 || !strings.Contains(body, `"track":"ring"`) || !strings.Contains(body, `"length":1000`) {
		t.Fatalf("%d %s", code, body)
	}
}
