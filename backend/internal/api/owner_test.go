package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/store"
)

func TestAdminTiesAnAgentToAMemberOfItsTeam(t *testing.T) {
	r := newAuthRig(t)
	admin := r.tok(adminA)
	ctx := context.Background()

	// creating with an address
	code, body, _ := r.do(t, admin, "POST", "/api/admin/agents", `{"name":"pc","team_id":`+itoa(r.def.ID)+`,"email":"`+strings.ToUpper(userA)+`"}`)
	var created agentJSON
	if err := json.Unmarshal([]byte(body), &created); err != nil || code != 201 || created.Email != userA {
		t.Fatalf("create: %d %s", code, body)
	}
	if ag, _, _ := r.db.AgentByTokenHash(ctx, store.HashToken(tokenOf(t, body))); ag.Email != userA {
		t.Fatalf("stored %+v", ag)
	}
	// an address of another team, or of nobody, is refused
	for _, email := range []string{userB, "nobody@example.com", "not an address"} {
		if code, _, _ := r.do(t, admin, "POST", "/api/admin/agents", `{"name":"x","team_id":`+itoa(r.def.ID)+`,"email":"`+email+`"}`); code != 400 {
			t.Errorf("create with %q: %d, want 400", email, code)
		}
		if code, _, _ := r.do(t, admin, "PUT", "/api/admin/agents/"+itoa(created.ID), `{"email":"`+email+`"}`); code != 400 {
			t.Errorf("set %q: %d, want 400", email, code)
		}
	}
	// changing and releasing
	if code, body, _ := r.do(t, admin, "PUT", "/api/admin/agents/"+itoa(created.ID), `{"email":"`+adminA+`"}`); code != 200 || !strings.Contains(body, adminA) {
		t.Fatalf("set: %d %s", code, body)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/agents/"+itoa(created.ID), `{"email":""}`); code != 200 {
		t.Fatalf("release: %d", code)
	}
	if _, body, _ := r.do(t, admin, "GET", "/api/admin/agents", ""); !strings.Contains(body, `"email":""`) {
		t.Fatalf("listing %s", body)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/agents/999", `{"email":""}`); code != 404 {
		t.Fatalf("unknown agent: %d", code)
	}
	if code, _, _ := r.do(t, r.tok(userA), "PUT", "/api/admin/agents/"+itoa(created.ID), `{"email":""}`); code != 403 {
		t.Fatalf("not an admin: %d", code)
	}
}

func tokenOf(t *testing.T, body string) string {
	t.Helper()
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil || v.Token == "" {
		t.Fatalf("no token in %s", body)
	}
	return v.Token
}

// A member sees their own agent; one without an agent sees an empty page, not the state of somebody else.
func TestAMemberSeesTheirOwnAgentOrNothing(t *testing.T) {
	r := newAuthRig(t)
	ctx := context.Background()
	mine, _ := r.db.CreateAgent(ctx, "mine", r.def.ID, store.HashToken("m"))
	other, _ := r.db.CreateAgent(ctx, "other pc", r.def.ID, store.HashToken("o"))
	r.db.SetAgentEmail(ctx, mine.ID, userA)
	r.db.SetAgentEmail(ctx, other.ID, adminA)
	rt := r.api.Teams.Get(r.def.ID)
	feedAgent(rt.Agent(ingest.AgentRef{ID: mine.ID, Name: "mine", Email: userA}), "session-a", 100, 50)
	feedAgent(rt.Agent(ingest.AgentRef{ID: other.ID, Name: "other pc", Email: adminA}), "session-b", 100, 30)

	_, body, _ := r.do(t, r.tok(userA), "GET", "/api/state", "")
	if !strings.Contains(body, `"agent_id":`+itoa(mine.ID)) || !strings.Contains(body, `"fuel":50`) {
		t.Fatalf("member with an agent: %s", body)
	}
	_, body, _ = r.do(t, r.tok(adminA), "GET", "/api/state", "")
	if !strings.Contains(body, `"agent_id":`+itoa(other.ID)) {
		t.Fatalf("the other member: %s", body)
	}
	// the other member's agent can be looked at on request
	_, body, _ = r.do(t, r.tok(userA), "GET", "/api/state?agent="+itoa(other.ID), "")
	if !strings.Contains(body, `"fuel":30`) {
		t.Fatalf("explicit agent: %s", body)
	}
	// an agent of another team cannot
	if code, _, _ := r.do(t, r.tok(userB), "GET", "/api/state?agent="+itoa(mine.ID), ""); code != 404 {
		t.Fatalf("agent of another team: %d, want 404", code)
	}

	// a member without an agent: empty state, no override writes, but a way to see the agent list
	r.db.SetMember(ctx, store.Member{Email: "new@example.com", TeamID: r.def.ID})
	nobody := r.tok("new@example.com")
	_, body, _ = r.do(t, nobody, "GET", "/api/state", "")
	if !strings.Contains(body, `"ready":false`) || !strings.Contains(body, `"agent_id":-1`) {
		t.Fatalf("member without an agent: %s", body)
	}
	if code, _, _ := r.do(t, nobody, "PUT", "/api/overrides/fuel", `{"value":10}`); code != 409 {
		t.Fatalf("override without an agent: %d, want 409", code)
	}
	if code, body, _ := r.do(t, nobody, "GET", "/api/overrides", ""); code != 200 || strings.Contains(body, "fuel") {
		t.Fatalf("overrides without an agent: %d %s", code, body)
	}

	// an address change takes effect at once
	if code, _, _ := r.do(t, r.tok(adminA), "PUT", "/api/admin/agents/"+itoa(other.ID), `{"email":"new@example.com"}`); code != 200 {
		t.Fatalf("set owner: %d", code)
	}
	_, body, _ = r.do(t, nobody, "GET", "/api/state", "")
	if !strings.Contains(body, `"agent_id":`+itoa(other.ID)) {
		t.Fatalf("after the change: %s", body)
	}
}

func TestAgentListShowsTheTeamsAgentsAndWhichAreMine(t *testing.T) {
	r := newAuthRig(t)
	ctx := context.Background()
	mine, _ := r.db.CreateAgent(ctx, "mine", r.def.ID, store.HashToken("m"))
	other, _ := r.db.CreateAgent(ctx, "other pc", r.def.ID, store.HashToken("o"))
	gone, _ := r.db.CreateAgent(ctx, "revoked", r.def.ID, store.HashToken("g"))
	foreign, _ := r.db.CreateAgent(ctx, "foreign", r.oth.ID, store.HashToken("f"))
	r.db.SetAgentEmail(ctx, mine.ID, userA)
	r.db.RevokeAgent(ctx, gone.ID)
	feedAgent(r.api.Teams.Get(r.def.ID).Agent(ingest.AgentRef{ID: other.ID, Name: "other pc"}), "session-b", 100, 30)

	var list []agentInfo
	code, body, _ := r.do(t, r.tok(userA), "GET", "/api/agents", "")
	if err := json.Unmarshal([]byte(body), &list); err != nil || code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if len(list) != 2 {
		t.Fatalf("want my agent and the other one, got %+v", list)
	}
	if list[0].ID != mine.ID || !list[0].Mine || list[1].ID != other.ID || list[1].Mine || !list[1].Active || list[1].SessionID != "session-b" {
		t.Fatalf("%+v", list)
	}
	if strings.Contains(body, "@") || strings.Contains(body, itoa(foreign.ID)+`,"name":"foreign"`) {
		t.Fatalf("the list leaks an address or another team's agent: %s", body)
	}
}

// A pit command goes to the requesting member's own agent; nobody sends commands to somebody else's car.
func TestPitCommandsGoToTheMembersOwnAgent(t *testing.T) {
	r := newAuthRig(t)
	ctx := context.Background()
	mine, _ := r.db.CreateAgent(ctx, "mine", r.def.ID, store.HashToken("m"))
	theirs, _ := r.db.CreateAgent(ctx, "theirs", r.def.ID, store.HashToken("t"))
	r.db.SetAgentEmail(ctx, mine.ID, userA)
	r.db.SetAgentEmail(ctx, theirs.ID, adminA)
	rt := r.api.Teams.Get(r.def.ID)
	feedAgent(rt.Agent(ingest.AgentRef{ID: mine.ID, Name: "mine", Email: userA}), "session-a", 100, 50)
	feedAgent(rt.Agent(ingest.AgentRef{ID: theirs.ID, Name: "theirs", Email: adminA}), "session-b", 100, 30)

	// neither agent has a connection, so a command for the right agent fails as "not connected" (409), not as forbidden
	if code, body, _ := r.do(t, r.tok(userA), "POST", "/api/pit-request", ""); code != 409 || !strings.Contains(body, "not connected") {
		t.Fatalf("own agent: %d %s", code, body)
	}
	if code, _, _ := r.do(t, r.tok(userA), "POST", "/api/pit-request?agent="+itoa(theirs.ID), ""); code != 403 {
		t.Fatalf("somebody else's agent: %d, want 403", code)
	}
	if code, body, _ := r.do(t, r.tok(adminA), "POST", "/api/pit-request?agent="+itoa(mine.ID), ""); code != 409 || !strings.Contains(body, "not connected") {
		t.Fatalf("an administrator may name any agent: %d %s", code, body)
	}
	r.db.SetMember(ctx, store.Member{Email: "new@example.com", TeamID: r.def.ID})
	if code, body, _ := r.do(t, r.tok("new@example.com"), "POST", "/api/pit-request", ""); code != 409 || !strings.Contains(body, "no agent") {
		t.Fatalf("member without an agent: %d %s", code, body)
	}
}
