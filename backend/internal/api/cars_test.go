package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/store"
)

func TestAdminManagesTeamCarsAndTheirNames(t *testing.T) {
	r := newAuthRig(t)
	admin := r.tok(adminA)
	team := itoa(r.def.ID)

	if code, _, _ := r.do(t, r.tok(userA), "GET", "/api/admin/cars?team_id="+team, ""); code != 403 {
		t.Fatalf("not an admin: %d", code)
	}
	code, body, _ := r.do(t, admin, "POST", "/api/admin/cars", `{"team_id":`+team+`,"name":" BMW "}`)
	var car store.TeamCar
	if err := json.Unmarshal([]byte(body), &car); err != nil || code != 201 || car.Name != "BMW" {
		t.Fatalf("create: %d %s", code, body)
	}
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"team_id":` + team + `,"name":"BMW"}`, 409},
		{`{"team_id":` + team + `,"name":" "}`, 400},
		{`{"team_id":99999,"name":"x"}`, 400},
		{`{"name":"x"}`, 400},
	} {
		if code, _, _ := r.do(t, admin, "POST", "/api/admin/cars", c.body); code != c.want {
			t.Errorf("create %s: %d, want %d", c.body, code, c.want)
		}
	}

	id := itoa(car.ID)
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/cars/"+id+"/names", `{"team_id":`+team+`,"vehicle_name":"BMW #1 red"}`); code != 204 {
		t.Fatalf("map: %d", code)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/cars/999/names", `{"team_id":`+team+`,"vehicle_name":"x"}`); code != 404 {
		t.Fatalf("map on an unknown car: %d", code)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/cars/"+id+"/names", `{"team_id":`+team+`,"vehicle_name":" "}`); code != 400 {
		t.Fatalf("empty name: %d", code)
	}

	// a vehicle name seen in a session shows up as unmapped until it is assigned
	st := r.api.Teams.Get(r.def.ID).Store
	st.TouchSession(context.Background(), store.SessionInfo{SessionID: "s", Track: "monza", Car: "BMW #2 blue"})
	var list struct {
		Cars     []store.TeamCar `json:"cars"`
		Unmapped []string        `json:"unmapped"`
	}
	_, body, _ = r.do(t, admin, "GET", "/api/admin/cars?team_id="+team, "")
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Cars) != 1 || len(list.Cars[0].Names) != 1 ||
		list.Cars[0].Names[0] != "BMW #1 red" || len(list.Unmapped) != 1 || list.Unmapped[0] != "BMW #2 blue" {
		t.Fatalf("list: %s", body)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/cars/"+id+"/names", `{"team_id":`+team+`,"vehicle_name":"BMW #2 blue"}`); code != 204 {
		t.Fatalf("map: %d", code)
	}
	if code, _, _ := r.do(t, admin, "DELETE", "/api/admin/car-names?team_id="+team+"&name="+"BMW%20%232%20blue", ""); code != 204 {
		t.Fatalf("unmap: %d", code)
	}
	if got, _ := st.CanonicalCar(context.Background(), "BMW #2 blue"); got != "BMW #2 blue" {
		t.Fatalf("after unmapping: %q", got)
	}

	// the other team neither sees the car nor can delete it
	if code, _, _ := r.do(t, admin, "DELETE", "/api/admin/cars/"+id+"?team_id="+itoa(r.oth.ID), ""); code != 404 {
		t.Fatalf("delete through another team: %d, want 404", code)
	}
	if code, _, _ := r.do(t, admin, "DELETE", "/api/admin/cars/"+id+"?team_id="+team, ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if es, _ := r.db.Audit(context.Background(), 50); !strings.Contains(auditText(es), "/api/admin/cars") {
		t.Fatal("car changes are not in the audit log")
	}
}

// Parameters saved while driving one livery apply to the other liveries of the same team car.
func TestParametersFollowTheTeamCar(t *testing.T) {
	r := newAuthRig(t)
	ctx := context.Background()
	rt := r.api.Teams.Get(r.def.ID)
	car, _ := rt.Store.CreateCar(ctx, "BMW")
	rt.Store.MapCarName(ctx, car.ID, "bmw")       // the livery feedAgent drives
	rt.Store.MapCarName(ctx, car.ID, "bmw-green") // another livery of the team car
	ag, _ := r.db.CreateAgent(ctx, "pc", r.def.ID, store.HashToken("p"))
	r.db.SetAgentEmail(ctx, ag.ID, userA)
	feedAgent(rt.Agent(ingest.AgentRef{ID: ag.ID, Name: "pc", Email: userA}), "session-a", 100, 50)

	user := r.tok(userA)
	if code, body, _ := r.do(t, user, "PUT", "/api/params?track=monza&car=bmw", `{"tyre_change":21}`); code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	_, body, _ := r.do(t, user, "GET", "/api/params?track=monza&car=bmw-green", "")
	if !strings.Contains(body, `"tyre_change":21`) || !strings.Contains(body, `"car":"BMW"`) {
		t.Fatalf("through the other livery: %s", body)
	}
	_, body, _ = r.do(t, user, "GET", "/api/state", "")
	if !strings.Contains(body, `"tyre_change":21`) {
		t.Fatalf("the state uses the parameters of the team car: %s", body)
	}
}
