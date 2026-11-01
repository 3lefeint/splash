package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func fl(v float64) *float64 { return &v }

// lapRig has two members with an agent each; both drove in one session, and a rival's lap is stored as well.
type lapRig struct {
	*authRig
	a, b store.Agent
	st   store.Store
}

func newLapRig(t *testing.T) *lapRig {
	t.Helper()
	r := newAuthRig(t)
	ctx := context.Background()
	a, _ := r.db.CreateAgent(ctx, "pc a", r.def.ID, store.HashToken("a"))
	b, _ := r.db.CreateAgent(ctx, "pc b", r.def.ID, store.HashToken("b"))
	r.db.SetAgentEmail(ctx, a.ID, userA)
	r.db.SetAgentEmail(ctx, b.ID, adminA)
	st := r.api.Teams.Get(r.def.ID).Store
	now := time.Now()
	st.TouchSession(ctx, store.SessionInfo{SessionID: "s1", Track: "monza", Car: "bmw", StartedAt: now, LastSeen: now})
	save := func(car, lap int, agent int64, fuel bool) {
		l := wire.Lap{CarID: car, VehicleName: "bmw", Lap: lap, Driver: "d", LapTime: fl(90)}
		if fuel {
			l.FuelStart, l.FuelEnd = fl(60), fl(57)
		}
		st.SaveLap(ctx, "s1", l, agent)
	}
	save(0, 1, a.ID, true)
	save(0, 2, a.ID, true)
	save(1, 1, b.ID, true)
	save(7, 1, a.ID, false) // a rival: no fuel figures, no conditions
	return &lapRig{r, a, b, st}
}

func lapsOf(t *testing.T, body string) sessionLaps {
	t.Helper()
	var out sessionLaps
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out
}

func TestLapTableListsTheDrivenCarsWithConditionsAndRights(t *testing.T) {
	r := newLapRig(t)
	ctx := context.Background()
	r.st.SaveLapConditions(ctx, store.LapConditions{SessionID: "s1", CarID: 0, VehicleName: "bmw", Lap: 1, Ambient: fl(21), Rain: fl(0),
		CompoundFront: "Medium", CompoundRear: "Medium", Pressure: []float64{150, 150, 148, 148}})

	code, body, _ := r.do(t, r.tok(userA), "GET", "/api/sessions/s1/laps", "")
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	got := lapsOf(t, body)
	if len(got.Laps) != 3 {
		t.Fatalf("want 3 laps of driven cars (the rival's is left out), got %+v", got.Laps)
	}
	l1 := got.Laps[0]
	if l1.Lap != 1 || l1.Agent != "pc a" || !l1.Mine || !l1.CanEdit || l1.FuelUsed == nil || *l1.FuelUsed != 3 ||
		l1.Compound != "Medium" || l1.Ambient == nil || *l1.Ambient != 21 || len(l1.Pressure) != 4 {
		t.Fatalf("lap %+v", l1)
	}
	other := got.Laps[2]
	if other.Agent != "pc b" || other.Mine || other.CanEdit {
		t.Fatalf("the lap of another member is not mine and not editable: %+v", other)
	}
	if got.CanEditSession {
		t.Fatal("a session with laps of another driver cannot be left out by a member")
	}
	// an administrator may change everything
	_, body, _ = r.do(t, r.tok(adminA), "GET", "/api/sessions/s1/laps", "")
	adm := lapsOf(t, body)
	if !adm.CanEditSession || !adm.Laps[0].CanEdit || !adm.Laps[2].Mine {
		t.Fatalf("admin %+v", adm)
	}
}

func TestOnlyTheDriverOrAnAdminLeavesALapOut(t *testing.T) {
	r := newLapRig(t)
	put := func(tok string, car, lap int, excluded string) int {
		code, _, _ := r.do(t, tok, "PUT", "/api/laps/excluded",
			`{"session_id":"s1","car_id":`+itoa(int64(car))+`,"vehicle_name":"bmw","lap":`+itoa(int64(lap))+`,"excluded":`+excluded+`}`)
		return code
	}
	user, admin := r.tok(userA), r.tok(adminA)
	if code := put(user, 0, 1, "true"); code != 204 {
		t.Fatalf("own lap: %d", code)
	}
	if code := put(user, 1, 1, "true"); code != 403 {
		t.Fatalf("lap of another driver: %d, want 403", code)
	}
	if code := put(admin, 1, 1, "true"); code != 204 {
		t.Fatalf("admin on a lap of another driver: %d", code)
	}
	if code := put(user, 9, 9, "true"); code != 404 {
		t.Fatalf("unknown lap: %d, want 404", code)
	}
	_, body, _ := r.do(t, user, "GET", "/api/sessions/s1/laps", "")
	got := lapsOf(t, body)
	if !got.Laps[0].Excluded || got.Laps[1].Excluded || !got.Laps[2].Excluded {
		t.Fatalf("exclusion: %+v", got.Laps)
	}
	// null lets the lap follow its session again
	if code := put(user, 0, 1, "null"); code != 204 {
		t.Fatalf("clear: %d", code)
	}
	_, body, _ = r.do(t, user, "GET", "/api/sessions/s1/laps", "")
	if got := lapsOf(t, body); got.Laps[0].Excluded || got.Laps[0].ExcludedFlag != nil {
		t.Fatalf("after clearing: %+v", got.Laps[0])
	}
	if es, _ := r.db.Audit(context.Background(), 50); !strings.Contains(auditText(es), "/api/laps/excluded") {
		t.Fatal("the change is not in the audit log")
	}
}

func TestASessionIsLeftOutByAdminsOrByItsOnlyDriver(t *testing.T) {
	r := newLapRig(t)
	ctx := context.Background()
	user, admin := r.tok(userA), r.tok(adminA)
	if code, _, _ := r.do(t, user, "PUT", "/api/sessions/s1/excluded", `{"excluded":true}`); code != 403 {
		t.Fatalf("member, shared session: %d, want 403", code)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/sessions/s1/excluded", `{"excluded":true}`); code != 204 {
		t.Fatalf("admin: %d", code)
	}
	_, body, _ := r.do(t, user, "GET", "/api/sessions/s1/laps", "")
	got := lapsOf(t, body)
	if !got.Excluded || !got.Laps[0].Excluded || !got.Laps[2].Excluded {
		t.Fatalf("session left out: %+v", got)
	}
	// a lap taken in explicitly inside a left-out session counts again
	if code, _, _ := r.do(t, user, "PUT", "/api/laps/excluded", `{"session_id":"s1","car_id":0,"vehicle_name":"bmw","lap":1,"excluded":false}`); code != 204 {
		t.Fatalf("take in: %d", code)
	}
	_, body, _ = r.do(t, user, "GET", "/api/sessions/s1/laps", "")
	if got := lapsOf(t, body); got.Laps[0].Excluded || got.Laps[1].Excluded == false {
		t.Fatalf("lap taken in: %+v", got.Laps)
	}

	// a session that only the member drove
	now := time.Now()
	r.st.TouchSession(ctx, store.SessionInfo{SessionID: "solo", Track: "monza", Car: "bmw", StartedAt: now, LastSeen: now})
	r.st.SaveLap(ctx, "solo", wire.Lap{CarID: 0, VehicleName: "bmw", Lap: 1, LapTime: fl(90), FuelStart: fl(60), FuelEnd: fl(57)}, r.a.ID)
	if code, _, _ := r.do(t, user, "PUT", "/api/sessions/solo/excluded", `{"excluded":true}`); code != 204 {
		t.Fatalf("member, own session: %d", code)
	}
	if code, _, _ := r.do(t, user, "PUT", "/api/sessions/nope/excluded", `{"excluded":true}`); code != 404 {
		t.Fatalf("unknown session: %d, want 404", code)
	}
	_, body, _ = r.do(t, user, "GET", "/api/sessions", "")
	if !strings.Contains(body, `"session_id":"solo"`) || !strings.Contains(body, `"excluded":true`) {
		t.Fatalf("the summary shows the exclusion: %s", body)
	}
}
