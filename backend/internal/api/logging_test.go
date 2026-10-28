package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/auth/authtest"
	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/logx"
	"github.com/3lefeint/splash/backend/internal/ratelimit"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/teams"
)

const (
	logMember   = "leaky.person@example.com"
	logAdmin    = "boss.person@example.com"
	logStranger = "stranger.person@example.com"
	logNewcomer = "new.member@example.com"
	splashToken = "splash-secret-token-0123456789"
	cfSecret    = "cf-secret-value-abcdef"
	cfClientID  = "cf-client-id.access"
	fakePW      = "fakepassword-xyz"
)

// logRig is the whole server (API behind the request middleware, agent gateway) with a DEBUG logger that is captured.
type logRig struct {
	db   *store.Memory
	iss  *authtest.Issuer
	api  *API
	gw   *ingest.Gateway
	srv  *httptest.Server
	out  *bytes.Buffer
	team store.Team
}

// lockedWriter makes the shared buffer safe for the many goroutines that log.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

const slogDebug = slog.LevelDebug

func rateLimiter(n int) *ratelimit.Limiter { return ratelimit.New(n, n) }

func newLogRig(t *testing.T) *logRig {
	t.Helper()
	ctx := context.Background()
	buf := &bytes.Buffer{}
	log := logx.New(&lockedWriter{w: buf}, slogDebug)
	db := store.NewMemory()
	reg, err := teams.NewRegistry(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	def, _, _ := db.TeamByName(ctx, store.DefaultTeamName)
	db.SetMember(ctx, store.Member{Email: logMember, TeamID: def.ID})
	db.SetMember(ctx, store.Member{Email: logAdmin, TeamID: def.ID, IsAdmin: true})
	db.CreateAgent(ctx, "driver", def.ID, store.HashToken(splashToken))

	iss := authtest.NewIssuer(t)
	a := New(reg)
	a.Log = log
	a.Verifier = iss.Verifier()
	a.Verifier.SetLogger(log)
	mux := http.NewServeMux()
	gw := &ingest.Gateway{Dir: db, Verifier: a.Verifier, DefaultTeam: reg.DefaultID(), Log: log,
		Server: func(id int64) *ingest.Server { return reg.Get(id).Ingest }}
	mux.Handle(ingest.Path, gw)
	a.Register(mux)
	srv := httptest.NewServer(logx.Middleware(log, mux))
	t.Cleanup(srv.Close)
	return &logRig{db, iss, a, gw, srv, buf, def}
}

func (r *logRig) request(t *testing.T, jwt, method, path, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if jwt != "" {
		req.Header.Set(auth.Header, jwt)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func (r *logRig) dialAgent(t *testing.T, splash, jwt string) (*websocket.Conn, int) {
	t.Helper()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+splash)
	h.Set("CF-Access-Client-Id", cfClientID)
	h.Set("CF-Access-Client-Secret", cfSecret)
	if jwt != "" {
		h.Set(auth.Header, jwt)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(r.srv.URL, "http")+ingest.Path, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return nil, resp.StatusCode
	}
	return c, http.StatusSwitchingProtocols
}

func (r *logRig) lines(substr string) []string {
	var out []string
	for _, l := range strings.Split(r.out.String(), "\n") {
		if strings.Contains(l, substr) {
			out = append(out, l)
		}
	}
	return out
}

func TestNoSecretReachesTheLogAtDebugLevel(t *testing.T) {
	r := newLogRig(t)
	member := r.iss.Token(authtest.Spec{Email: logMember})
	admin := r.iss.Token(authtest.Spec{Email: logAdmin})
	stranger := r.iss.Token(authtest.Spec{Email: logStranger})
	expired := r.iss.Token(authtest.Spec{Email: logMember, Expires: time.Now().Add(-time.Hour)})
	service := r.iss.Token(authtest.Spec{CommonName: cfClientID})
	badAud := r.iss.Token(authtest.Spec{Email: logMember, Audience: []string{"other"}})

	// browser path: success, failures of every kind, a write
	r.request(t, member, "GET", "/api/me", "")
	r.request(t, stranger, "GET", "/api/me", "")
	r.request(t, expired, "GET", "/api/state", "")
	r.request(t, badAud, "GET", "/api/state", "")
	r.request(t, "junk.token.value", "GET", "/api/state", "")
	r.request(t, member, "PUT", "/api/stint-plan?token=hunter2", `[{"driver":"d","duration":60}]`)
	r.request(t, member, "GET", "/api/admin/teams", "") // not an admin

	// admin path: a member with an address, an agent with a token, a revoke
	r.request(t, admin, "PUT", "/api/admin/members", `{"email":"`+logNewcomer+`","team_id":`+itoa(r.team.ID)+`}`)
	req, _ := http.NewRequest("POST", r.srv.URL+"/api/admin/agents", strings.NewReader(`{"name":"spotter","team_id":`+itoa(r.team.ID)+`}`))
	req.Header.Set(auth.Header, admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.Token == "" {
		t.Fatal("no token created")
	}
	r.request(t, admin, "POST", "/api/admin/agents/"+itoa(created.ID)+"/revoke", "")
	r.request(t, admin, "DELETE", "/api/admin/members?email="+logNewcomer, "")

	// the stream, opened and closed
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	sreq, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/api/stream", nil)
	sreq.Header.Set(auth.Header, member)
	if sresp, err := http.DefaultClient.Do(sreq); err == nil {
		buf := make([]byte, 64)
		sresp.Body.Read(buf)
		sresp.Body.Close()
	}
	cancel()

	// agent path: a good connection with the Cloudflare headers, frames, and refusals
	c, status := r.dialAgent(t, splashToken, service)
	if status != http.StatusSwitchingProtocols {
		t.Fatalf("agent handshake: %d", status)
	}
	for i, m := range []string{
		`{"v":1,"type":"hello","agent_id":"driver-pc","role":"driver","session_id":"s1","seq":0,"et":0,"payload":{"agent_version":"1"}}`,
		`{"v":1,"type":"lap","agent_id":"driver-pc","role":"driver","session_id":"s1","seq":1,"et":10,"payload":{"car_id":0,"vehicle_name":"v","lap":1,"in_lap":false,"out_lap":false}}`,
	} {
		wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Second)
		c.Write(wctx, websocket.MessageText, []byte(m))
		if _, _, err := c.Read(wctx); err != nil {
			t.Fatalf("ack %d: %v", i, err)
		}
		wcancel()
	}
	c.Close(websocket.StatusNormalClosure, "bye")
	r.dialAgent(t, splashToken, "")                   // no Access token
	r.dialAgent(t, "wrong-splash-token-xyz", service) // unknown Splash token
	r.dialAgent(t, created.Token, service)            // revoked
	time.Sleep(100 * time.Millisecond)                // let the disconnect line be written

	r.api.Log.Error("probe", "err", errors.New("dial postgres://svc:"+fakePW+"@db.example.com/x"), "who", logMember)

	out := r.out.String()
	if testing.Verbose() {
		t.Log("\n" + out) // lets a person look at real lines: go test -v -run NoSecret
	}
	secrets := map[string]string{
		"splash token": splashToken, "created agent token": created.Token, "wrong token": "wrong-splash-token-xyz",
		"cloudflare secret": cfSecret, "password": fakePW, "query value": "hunter2",
		"member address": logMember, "admin address": logAdmin, "stranger address": logStranger, "newcomer address": logNewcomer,
		"Authorization header": "Authorization", "bearer": "Bearer", "member name part": "leaky.person",
	}
	for _, jwt := range []string{member, admin, stranger, expired, service, badAud} {
		parts := strings.Split(jwt, ".")
		secrets["jwt"] = jwt
		secrets["jwt header"], secrets["jwt claims"], secrets["jwt signature"] = parts[0], parts[1], parts[2]
		secrets["jwt signature start"] = parts[2][:24]
		for what, s := range secrets {
			if strings.Contains(out, s) {
				t.Errorf("%s (%q) is in the log", what, s)
			}
		}
	}
	for what, s := range secrets {
		if strings.Contains(out, s) && !strings.HasPrefix(what, "jwt") {
			t.Errorf("%s (%q) is in the log", what, s)
		}
	}

	// the test must not pass by logging nothing: the lines of every kind are there
	for _, want := range []string{
		"level=DEBUG msg=request", "msg=\"access token verified\"", "msg=\"access token rejected\" reason=expired",
		"msg=\"access token rejected\" reason=wrong_audience", "msg=\"access keys refreshed\"", "msg=\"access keys cache hit\"",
		"msg=\"authentication failed\" path=browser reason=expired", "reason=unknown_member", "reason=malformed",
		"msg=\"admin action\" action=member_set", "action=agent_created", "action=agent_revoked", "action=member_removed",
		"msg=\"stream connected\"", "msg=\"agent connected\"", "msg=\"agent disconnected\"", "msg=frame",
		"path=agent reason=access_missing", "path=agent reason=unknown_token", "path=agent reason=revoked_agent",
		"msg=\"state computed\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected a line with %q\n%s", want, out)
		}
	}
	for _, l := range r.lines("msg=\"admin action\"") {
		if strings.Contains(l, "email") {
			t.Errorf("admin line mentions an address: %s", l)
		}
	}
}

func TestAFloodOfFailedLoginsBecomesOneLine(t *testing.T) {
	r := newLogRig(t)
	now := time.Now()
	th := logx.NewThrottle(time.Minute)
	th.SetClock(func() time.Time { return now })
	r.api.Throttle, r.gw.Throttle = th, th
	for i := 0; i < 60; i++ {
		r.request(t, "junk.token.value", "GET", "/api/state", "")
	}
	for i := 0; i < 30; i++ {
		r.dialAgent(t, "wrong-splash-token-xyz", r.iss.Token(authtest.Spec{CommonName: cfClientID}))
	}
	if n := len(r.lines("path=browser reason=malformed")); n != 1 {
		t.Fatalf("%d browser lines for 60 failures", n)
	}
	if n := len(r.lines("path=agent reason=unknown_token")); n != 1 {
		t.Fatalf("%d agent lines for 30 failures", n)
	}
	now = now.Add(2 * time.Minute)
	r.request(t, "junk.token.value", "GET", "/api/state", "")
	if l := r.lines("suppressed=59"); len(l) != 1 {
		t.Fatalf("the next line must say how many were held back:\n%s", r.out.String())
	}
}

func TestRateLimitHitsAreLoggedWithAHashNotTheAddress(t *testing.T) {
	r := newLogRig(t)
	r.api.WriteLimit = rateLimiter(1)
	member := r.iss.Token(authtest.Spec{Email: logMember})
	r.request(t, member, "PUT", "/api/overrides/fuel", `{"value":1}`)
	if code := r.request(t, member, "PUT", "/api/overrides/fuel", `{"value":1}`); code != 429 {
		t.Fatalf("status %d", code)
	}
	l := r.lines("msg=\"rate limited\"")
	if len(l) != 1 || !strings.Contains(l[0], "kind=write") || !strings.Contains(l[0], "identity="+logx.ShortID(logMember)) {
		t.Fatalf("%v", l)
	}
	if strings.Contains(r.out.String(), logMember) {
		t.Fatal("the address is in the log")
	}
}

func TestAServerErrorIsLoggedAtErrorLevel(t *testing.T) {
	r := newLogRig(t)
	r.api.Identify = func(*http.Request) (Identity, error) { return Identity{}, fmt.Errorf("database is down") }
	r.request(t, "x", "GET", "/api/state", "")
	if testing.Verbose() {
		t.Log("\n" + r.out.String())
	}
	if l := r.lines("level=ERROR"); len(l) < 2 {
		t.Fatalf("want the identify failure and the failed request:\n%s", r.out.String())
	}
}
