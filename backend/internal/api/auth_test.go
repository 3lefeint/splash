package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/auth/authtest"
	"github.com/3lefeint/splash/backend/internal/ratelimit"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/teams"
)

// authRig runs the API with Access authentication on, against a fake Access and an in-memory database that has the
// team "default", the team "other", a normal member and an administrator in each.
type authRig struct {
	db  *store.Memory
	iss *authtest.Issuer
	api *API
	srv *httptest.Server
	def store.Team
	oth store.Team
}

const (
	userA  = "driver-a@example.com" // member of default
	userB  = "driver-b@example.com" // member of other
	adminA = "admin-a@example.com"  // admin of default
)

func newAuthRig(t *testing.T) *authRig {
	t.Helper()
	ctx := context.Background()
	db := store.NewMemory()
	reg, err := teams.NewRegistry(ctx, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	def, _, _ := db.TeamByName(ctx, store.DefaultTeamName)
	oth, _ := db.CreateTeam(ctx, "other")
	db.SetMember(ctx, store.Member{Email: userA, TeamID: def.ID})
	db.SetMember(ctx, store.Member{Email: userB, TeamID: oth.ID})
	db.SetMember(ctx, store.Member{Email: adminA, TeamID: def.ID, IsAdmin: true})

	iss := authtest.NewIssuer(t)
	a := New(reg)
	a.Verifier = iss.Verifier()
	mux := http.NewServeMux()
	a.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &authRig{db, iss, a, srv, def, oth}
}

func (r *authRig) do(t *testing.T, token, method, path, body string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set(auth.Header, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b)), resp.Header
}

func (r *authRig) tok(email string) string { return r.iss.Token(authtest.Spec{Email: email}) }

func TestAccessTokenMapsToTheTeamOfTheEmail(t *testing.T) {
	r := newAuthRig(t)
	if code, body, _ := r.do(t, r.tok(userA), "GET", "/api/me", ""); code != 200 ||
		!strings.Contains(body, `"team":"default"`) || !strings.Contains(body, `"is_admin":false`) || !strings.Contains(body, userA) {
		t.Fatalf("a: %d %s", code, body)
	}
	if code, body, _ := r.do(t, r.tok(userB), "GET", "/api/me", ""); code != 200 || !strings.Contains(body, `"team":"other"`) {
		t.Fatalf("b: %d %s", code, body)
	}
	// data written by one team is invisible to the other
	if code, body, _ := r.do(t, r.tok(userA), "PUT", "/api/stint-plan", `[{"driver":"x","duration":3600}]`); code != 200 {
		t.Fatalf("put %d %s", code, body)
	}
	if _, body, _ := r.do(t, r.tok(userB), "GET", "/api/stint-plan", ""); body != "[]" {
		t.Fatalf("team b sees %s", body)
	}
	if _, body, _ := r.do(t, r.tok(userA), "GET", "/api/stint-plan", ""); !strings.Contains(body, `"driver":"x"`) {
		t.Fatalf("team a lost its plan: %s", body)
	}
}

func TestRequestsWithoutAValidIdentityAreRefused(t *testing.T) {
	r := newAuthRig(t)
	forger := authtest.NewKey(t, "kid-1")
	for name, c := range map[string]struct {
		token string
		want  int
	}{
		"no token":        {"", 401},
		"garbage":         {"abc.def.ghi", 401},
		"expired":         {r.iss.Token(authtest.Spec{Email: userA, Expires: time.Now().Add(-time.Hour)}), 401},
		"wrong audience":  {r.iss.Token(authtest.Spec{Email: userA, Audience: []string{"other-app"}}), 401},
		"bad signature":   {r.iss.Token(authtest.Spec{Email: userA, SignWith: forger}), 401},
		"unknown e-mail":  {r.tok("stranger@example.com"), 403},
		"no e-mail claim": {r.tok(""), 403},
	} {
		for _, path := range []string{"/api/state", "/api/me", "/api/stream", "/api/admin/teams"} {
			if code, _, _ := r.do(t, c.token, "GET", path, ""); code != c.want {
				t.Errorf("%s %s: %d, want %d", name, path, code, c.want)
			}
		}
		if code, _, _ := r.do(t, c.token, "PUT", "/api/stint-plan", `[]`); code != c.want {
			t.Errorf("%s write: %d, want %d", name, code, c.want)
		}
	}
	if es, _ := r.db.Audit(context.Background(), 10); len(es) != 0 {
		t.Fatalf("refused requests must not be audited as the work of a member: %+v", es)
	}
}

func TestAdminAPIIsForAdminsOnly(t *testing.T) {
	r := newAuthRig(t)
	for _, p := range []struct{ method, path, body string }{
		{"GET", "/api/admin/teams", ""},
		{"POST", "/api/admin/teams", `{"name":"x"}`},
		{"GET", "/api/admin/members", ""},
		{"PUT", "/api/admin/members", `{"email":"x@example.com","team_id":1}`},
		{"DELETE", "/api/admin/members?email=" + userB, ""},
		{"GET", "/api/admin/agents", ""},
		{"POST", "/api/admin/agents", `{"name":"x","team_id":1}`},
		{"POST", "/api/admin/agents/1/revoke", ""},
	} {
		if code, _, _ := r.do(t, r.tok(userA), p.method, p.path, p.body); code != 403 {
			t.Errorf("non-admin %s %s: %d", p.method, p.path, code)
		}
	}
	if m, ok, _ := r.db.MemberByEmail(context.Background(), userB); !ok || m.TeamID != r.oth.ID {
		t.Fatal("a refused request changed a member")
	}
	if code, _, _ := r.do(t, r.tok(adminA), "GET", "/api/admin/teams", ""); code != 200 {
		t.Fatalf("admin: %d", code)
	}
}

func TestAdminManagesTeamsAndMembers(t *testing.T) {
	r := newAuthRig(t)
	admin := r.tok(adminA)
	code, body, _ := r.do(t, admin, "POST", "/api/admin/teams", `{"name":"third"}`)
	var team teamJSON
	if json.Unmarshal([]byte(body), &team); code != 201 || team.Name != "third" {
		t.Fatalf("%d %s", code, body)
	}
	if code, _, _ := r.do(t, admin, "POST", "/api/admin/teams", `{"name":"third"}`); code != 409 {
		t.Fatalf("duplicate team: %d", code)
	}
	if code, _, _ := r.do(t, admin, "POST", "/api/admin/teams", `{"name":" "}`); code != 400 {
		t.Fatalf("empty team name: %d", code)
	}

	// a new address is added, written in mixed case, and then moved: it stays one member
	if code, body, _ := r.do(t, admin, "PUT", "/api/admin/members", `{"email":"New@Example.com","team_id":`+itoa(team.ID)+`}`); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/members", `{"email":"new@example.com","team_id":`+itoa(r.oth.ID)+`,"is_admin":false}`); code != 200 {
		t.Fatalf("move: %d", code)
	}
	m, ok, _ := r.db.MemberByEmail(context.Background(), "new@example.com")
	if !ok || m.TeamID != r.oth.ID {
		t.Fatalf("member=%+v ok=%v", m, ok)
	}
	if all, _ := r.db.Members(context.Background()); len(all) != 4 {
		t.Fatalf("an address must belong to exactly one team: %+v", all)
	}
	// the new member can log in and lands in the new team
	if _, body, _ := r.do(t, r.tok("new@example.com"), "GET", "/api/me", ""); !strings.Contains(body, `"team":"other"`) {
		t.Fatalf("%s", body)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/members", `{"email":"nobody","team_id":1}`); code != 400 {
		t.Fatalf("invalid address: %d", code)
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/members", `{"email":"x@example.com","team_id":999}`); code != 400 {
		t.Fatalf("unknown team: %d", code)
	}
	// an admin cannot lock themselves out
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/members", `{"email":"`+adminA+`","team_id":1,"is_admin":false}`); code != 409 {
		t.Fatalf("self demotion: %d", code)
	}
	if code, _, _ := r.do(t, admin, "DELETE", "/api/admin/members?email="+adminA, ""); code != 409 {
		t.Fatalf("self removal: %d", code)
	}
	if code, _, _ := r.do(t, admin, "DELETE", "/api/admin/members?email=new@example.com", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := r.do(t, r.tok("new@example.com"), "GET", "/api/me", ""); code != 403 {
		t.Fatalf("removed member: %d", code)
	}
}

func TestAgentTokenIsShownOnceAndOnlyItsHashIsStored(t *testing.T) {
	r := newAuthRig(t)
	admin := r.tok(adminA)
	code, body, hdr := r.do(t, admin, "POST", "/api/admin/agents", `{"name":"driver pc","team_id":`+itoa(r.oth.ID)+`}`)
	var created struct {
		agentJSON
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || code != 201 || len(created.Token) < 40 {
		t.Fatalf("%d %s", code, body)
	}
	if hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", hdr.Get("Cache-Control"))
	}
	if created.TeamID != r.oth.ID {
		t.Fatalf("%+v", created)
	}

	// the stored agent is found by the hash of the token, and nothing readable holds the token
	ag, ok, _ := r.db.AgentByTokenHash(context.Background(), store.HashToken(created.Token))
	if !ok || ag.ID != created.ID || ag.TeamID != r.oth.ID || ag.Revoked {
		t.Fatalf("agent=%+v ok=%v", ag, ok)
	}
	if _, body, _ := r.do(t, admin, "GET", "/api/admin/agents", ""); strings.Contains(body, created.Token) || !strings.Contains(body, "driver pc") {
		t.Fatalf("listing: %s", body)
	}
	if es, _ := r.db.Audit(context.Background(), 50); strings.Contains(auditText(es), created.Token) {
		t.Fatal("the token is in the audit log")
	}
	// a second agent gets another token
	_, body2, _ := r.do(t, admin, "POST", "/api/admin/agents", `{"name":"spotter pc","team_id":1}`)
	if strings.Contains(body2, created.Token) {
		t.Fatal("tokens must differ")
	}
	if code, _, _ := r.do(t, admin, "POST", "/api/admin/agents", `{"name":"x","team_id":999}`); code != 400 {
		t.Fatalf("unknown team: %d", code)
	}

	if code, _, _ := r.do(t, admin, "POST", "/api/admin/agents/"+itoa(created.ID)+"/revoke", ""); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if ag, _, _ = r.db.AgentByTokenHash(context.Background(), store.HashToken(created.Token)); !ag.Revoked {
		t.Fatal("not revoked")
	}
	if _, body, _ := r.do(t, admin, "GET", "/api/admin/agents", ""); !strings.Contains(body, `"revoked":true`) {
		t.Fatalf("listing: %s", body)
	}
}

func auditText(es []store.AuditEntry) string {
	var b strings.Builder
	for _, e := range es {
		b.WriteString(e.Email + "|" + e.Team + "|" + e.Method + "|" + e.Path + "\n")
	}
	return b.String()
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestWritesAreAuditedAndReadsAreNot(t *testing.T) {
	r := newAuthRig(t)
	ctx := context.Background()
	r.do(t, r.tok(userA), "GET", "/api/state", "")
	r.do(t, r.tok(userA), "GET", "/api/stint-plan", "")
	r.do(t, r.tok(userA), "HEAD", "/api/stint-plan", "")
	r.do(t, r.tok(userA), "OPTIONS", "/api/stint-plan", "")
	if es, _ := r.db.Audit(ctx, 10); len(es) != 0 {
		t.Fatalf("reads were audited: %+v", es)
	}

	r.do(t, r.tok(userA), "PUT", "/api/stint-plan", `[{"driver":"secret-body","duration":60}]`)
	r.do(t, r.tok(userB), "PUT", "/api/overrides/fuel?token=hunter2", `{"value":5}`)
	r.do(t, r.tok(userA), "PUT", "/api/params?track=monza&car=me", `{"reserve":-1}`) // rejected by the handler, still a write
	es, _ := r.db.Audit(ctx, 10)
	if len(es) != 3 {
		t.Fatalf("entries: %+v", es)
	}
	// newest first
	want := []store.AuditEntry{
		{Email: userA, Team: "default", Method: "PUT", Path: "/api/params"},
		{Email: userB, Team: "other", Method: "PUT", Path: "/api/overrides/fuel"},
		{Email: userA, Team: "default", Method: "PUT", Path: "/api/stint-plan"},
	}
	for i, e := range es {
		if e.Email != want[i].Email || e.Team != want[i].Team || e.Method != want[i].Method || e.Path != want[i].Path || e.TS.IsZero() {
			t.Errorf("entry %d = %+v, want %+v", i, e, want[i])
		}
	}
	if txt := auditText(es); strings.Contains(txt, "secret-body") || strings.Contains(txt, "hunter2") {
		t.Fatalf("bodies or query strings in the audit log: %s", txt)
	}
}

func TestWriteRateLimitPerIdentity(t *testing.T) {
	r := newAuthRig(t)
	clock := time.Now()
	lim := ratelimit.New(2, 2)
	lim.SetClock(func() time.Time { return clock })
	r.api.WriteLimit = lim
	put := func(email string) (int, http.Header) {
		code, _, h := r.do(t, r.tok(email), "PUT", "/api/stint-plan", `[{"driver":"a","duration":3600}]`)
		return code, h
	}
	for i := 0; i < 2; i++ {
		if code, _ := put(userA); code != 200 {
			t.Fatalf("write %d: %d", i, code)
		}
	}
	code, h := put(userA)
	if code != 429 || h.Get("Retry-After") == "" || h.Get("Retry-After") == "0" {
		t.Fatalf("third write: %d Retry-After=%q", code, h.Get("Retry-After"))
	}
	// reads and other identities are not affected
	if code, _, _ := r.do(t, r.tok(userA), "GET", "/api/state", ""); code != 200 {
		t.Fatalf("read while limited: %d", code)
	}
	if code, _ := put(userB); code != 200 {
		t.Fatalf("other identity: %d", code)
	}
	clock = clock.Add(31 * time.Second) // 2 a minute: one token back after 30 s
	if code, _ := put(userA); code != 200 {
		t.Fatalf("after waiting: %d", code)
	}
}

func TestStreamRateLimitPerIdentity(t *testing.T) {
	r := newAuthRig(t)
	r.api.ConnLimit = ratelimit.New(1, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	open := func(email string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/stream", nil)
		req.Header.Set(auth.Header, r.tok(email))
		return http.DefaultClient.Do(req)
	}
	first, err := open(userA)
	if err != nil || first.StatusCode != 200 {
		t.Fatalf("first stream: %v %v", first, err)
	}
	defer first.Body.Close()
	second, err := open(userA)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != 429 || second.Header.Get("Retry-After") == "" {
		t.Fatalf("second stream: %d Retry-After=%q", second.StatusCode, second.Header.Get("Retry-After"))
	}
	third, err := open(userB)
	if err != nil || third.StatusCode != 200 {
		t.Fatalf("other identity: %v %v", third, err)
	}
	third.Body.Close()
}

func TestAuthOffBehavesAsBefore(t *testing.T) {
	r := newRig(t) // no Verifier
	if code, body := r.do(t, "GET", "/api/me", ""); code != 200 || !strings.Contains(body, `"team":"default"`) || !strings.Contains(body, `"is_admin":false`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := r.do(t, "PUT", "/api/overrides/fuel", `{"value":3}`); code != 200 {
		t.Fatalf("write without auth: %d", code)
	}
	// nobody is an administrator while authentication is off
	if code, _ := r.do(t, "GET", "/api/admin/agents", ""); code != 403 {
		t.Fatalf("admin without auth: %d", code)
	}
	es, _ := r.db.Audit(context.Background(), 10)
	if len(es) != 1 || es[0].Team != "default" || es[0].Email != "" || es[0].Method != "PUT" { // the admin GET is no write
		t.Fatalf("entries: %+v", es)
	}
}

func TestServiceTokenJWTWithoutEmailIsRefusedOnBrowserAndAdminPaths(t *testing.T) {
	r := newAuthRig(t)
	svc := r.iss.Token(authtest.Spec{CommonName: "client-id.access"}) // valid in every respect, but no e-mail
	for _, p := range []struct{ method, path, body string }{
		{"GET", "/api/me", ""},
		{"GET", "/api/state", ""},
		{"GET", "/api/stream", ""},
		{"PUT", "/api/stint-plan", `[]`},
		{"GET", "/api/admin/teams", ""},
		{"POST", "/api/admin/agents", `{"name":"x","team_id":1}`},
	} {
		if code, _, _ := r.do(t, svc, p.method, p.path, p.body); code != 403 {
			t.Errorf("%s %s: %d, want 403", p.method, p.path, code)
		}
	}
	if es, _ := r.db.Audit(context.Background(), 10); len(es) != 0 {
		t.Fatalf("audit: %+v", es)
	}
	if ags, _ := r.db.Agents(context.Background()); len(ags) != 0 {
		t.Fatalf("an agent was created: %+v", ags)
	}
}
