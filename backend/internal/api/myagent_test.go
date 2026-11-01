package api

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
)

func myAgent(t *testing.T, r *authRig, tok string) (int, myAgentJSON) {
	t.Helper()
	code, body, _ := r.do(t, tok, "GET", "/api/me/agent", "")
	var out myAgentJSON
	json.Unmarshal([]byte(body), &out)
	return code, out
}

// readyMember gives userA a short name and Cloudflare credentials.
func readyMember(t *testing.T, r *authRig) {
	t.Helper()
	withSecrets(t, r)
	putMember(t, r, `{"email":"`+userA+`","team_id":`+itoa(r.def.ID)+`,"name":"Driver A","short":"ab","cf_client_id":"abc.access","cf_secret":"`+memberCFSecret+`"}`)
}

func TestAMemberCannotGetAConfigurationBeforeTheAdministratorHasSetThingsUp(t *testing.T) {
	r := newAuthRig(t)
	withSecrets(t, r)
	user := r.tok(userA)
	if code, m := myAgent(t, r, user); code != 200 || m.CanDownload || m.Reason != reasonNoShort {
		t.Fatalf("no short name: %d %+v", code, m)
	}
	if code, _, _ := r.do(t, user, "POST", "/api/me/agent/config", ""); code != 409 {
		t.Fatalf("download without a short name: %d, want 409", code)
	}
	putMember(t, r, `{"email":"`+userA+`","team_id":`+itoa(r.def.ID)+`,"short":"ab"}`)
	if _, m := myAgent(t, r, user); m.CanDownload || m.Reason != reasonNoCloudflare {
		t.Fatalf("no Cloudflare data: %+v", m)
	}
	putMember(t, r, `{"email":"`+userA+`","team_id":`+itoa(r.def.ID)+`,"cf_client_id":"abc.access"}`) // id but no secret
	if code, _, _ := r.do(t, user, "POST", "/api/me/agent/config", ""); code != 409 {
		t.Fatalf("download with only a client id: %d, want 409", code)
	}
	if es, _ := r.db.Audit(context.Background(), 5); len(es) == 0 {
		t.Fatal("writes are audited")
	}
	if list, _ := r.db.Agents(context.Background()); len(list) != 0 {
		t.Fatalf("no agent may be created by a refused download: %+v", list)
	}
}

func TestTheConfigurationHoldsEverythingTheAgentNeeds(t *testing.T) {
	r := newAuthRig(t)
	readyMember(t, r)
	r.api.PublicURL = "https://splash.example.com/" // a trailing slash is tolerated
	user := r.tok(userA)
	if _, m := myAgent(t, r, user); !m.CanDownload || m.Agent != nil || m.Short != "ab" {
		t.Fatalf("before: %+v", m)
	}
	code, body, hdr := r.do(t, user, "POST", "/api/me/agent/config", "")
	if code != 200 || !strings.Contains(hdr.Get("Content-Disposition"), "agent.toml") || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", code, hdr)
	}
	for _, want := range []string{
		`url = "wss://splash.example.com/v1/agent/ws"`, `cf_client_id = "abc.access"`, `cf_client_secret = "` + memberCFSecret + `"`, `id = "ab"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	token := regexp.MustCompile(`token = "([^"]+)"`).FindStringSubmatch(body)
	if token == nil || len(token[1]) < 40 {
		t.Fatalf("no token in\n%s", body)
	}
	// the token is registered for this member, with the short name as agent name, and only its hash is stored
	ag, ok, _ := r.db.AgentByTokenHash(context.Background(), store.HashToken(token[1]))
	if !ok || ag.Name != "ab" || ag.Email != userA || ag.TeamID != r.def.ID || ag.Revoked {
		t.Fatalf("agent %+v ok=%v", ag, ok)
	}
	if _, m := myAgent(t, r, user); m.Agent == nil || m.Agent.ID != ag.ID || m.Agent.Name != "ab" {
		t.Fatalf("after: %+v", m)
	}
	// the token appears nowhere else
	if _, b, _ := r.do(t, r.tok(adminA), "GET", "/api/admin/agents", ""); strings.Contains(b, token[1]) || strings.Contains(b, memberCFSecret) {
		t.Fatal("the admin listing shows a secret")
	}
	if es, _ := r.db.Audit(context.Background(), 50); strings.Contains(auditText(es), token[1]) {
		t.Fatal("the token is in the audit log")
	}
}

func TestANewDownloadReplacesTheOldToken(t *testing.T) {
	r := newAuthRig(t)
	readyMember(t, r)
	user := r.tok(userA)
	grab := func() string {
		_, body, _ := r.do(t, user, "POST", "/api/me/agent/config", "")
		return regexp.MustCompile(`token = "([^"]+)"`).FindStringSubmatch(body)[1]
	}
	first, second := grab(), grab()
	if first == second {
		t.Fatal("tokens must differ")
	}
	a1, _, _ := r.db.AgentByTokenHash(context.Background(), store.HashToken(first))
	a2, _, _ := r.db.AgentByTokenHash(context.Background(), store.HashToken(second))
	if !a1.Revoked || a2.Revoked {
		t.Fatalf("the old token must be revoked and the new one active: %+v %+v", a1, a2)
	}
	active := 0
	list, _ := r.db.Agents(context.Background())
	for _, a := range list {
		if a.Email == userA && !a.Revoked {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("%d active agents, want exactly one", active)
	}
	// another member's agent is not touched
	otherAgent, _ := r.db.CreateAgent(context.Background(), "x", r.def.ID, store.HashToken("x"))
	r.db.SetAgentEmail(context.Background(), otherAgent.ID, adminA)
	grab()
	if got, _, _ := r.db.AgentByTokenHash(context.Background(), store.HashToken("x")); got.Revoked {
		t.Fatal("the agent of another member must stay")
	}
}

func TestTheServerAddressComesFromTheRequestWithoutAPublicURL(t *testing.T) {
	r := newAuthRig(t)
	readyMember(t, r)
	_, body, _ := r.do(t, r.tok(userA), "POST", "/api/me/agent/config", "")
	// behind Cloudflare (browser authentication on) the address is https, so the agent speaks wss
	if !regexp.MustCompile(`url = "wss://127\.0\.0\.1:\d+/v1/agent/ws"`).MatchString(body) {
		t.Fatalf("address in\n%s", body)
	}
}

func TestACloudflareSecretThatCannotBeReadAsksForNewInput(t *testing.T) {
	r := newAuthRig(t)
	readyMember(t, r)
	r.db.SetMemberSecret(context.Background(), userA, []byte("not a sealed value at all, just bytes"))
	code, body, _ := r.do(t, r.tok(userA), "POST", "/api/me/agent/config", "")
	if code != 409 || !strings.Contains(body, "administrator") {
		t.Fatalf("%d %s", code, body)
	}
	if list, _ := r.db.Agents(context.Background()); len(list) != 0 {
		t.Fatal("no agent without a readable secret")
	}
}

func TestWithoutAuthenticationThereIsNoMemberToConfigure(t *testing.T) {
	r := newRig(t)
	code, body := r.do(t, "GET", "/api/me/agent", "")
	if code != 200 || !strings.Contains(body, reasonNoMember) || strings.Contains(body, `"can_download":true`) {
		t.Fatalf("%d %s", code, body)
	}
}

func TestConfigurationValuesAreEscaped(t *testing.T) {
	m := store.Member{Short: "ab", CFClientID: `id"with\quote`}
	got := agentConfig("wss://h/v1/agent/ws", "tok", m, `s"e\cret`)
	for _, want := range []string{`cf_client_id = "id\"with\\quote"`, `cf_client_secret = "s\"e\\cret"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
}

func TestCloudflareValuesMustBePrintable(t *testing.T) {
	r := newAuthRig(t)
	withSecrets(t, r)
	for _, body := range []string{
		`{"email":"` + userA + `","team_id":` + itoa(r.def.ID) + `,"cf_client_id":"a\nb"}`,
		`{"email":"` + userA + `","team_id":` + itoa(r.def.ID) + `,"cf_client_id":"id","cf_secret":"se\tcret"}`,
		`{"email":"` + userA + `","team_id":` + itoa(r.def.ID) + `,"cf_client_id":"idü"}`,
	} {
		if code, _ := putMember(t, r, body); code != 400 {
			t.Errorf("%s: %d, want 400", body, code)
		}
	}
}
