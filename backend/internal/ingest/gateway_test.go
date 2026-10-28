package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/auth/authtest"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/ratelimit"
	"github.com/3lefeint/splash/backend/internal/store"
)

type gwRig struct {
	db      *store.Memory
	gw      *Gateway
	servers map[int64]*Server
	url     string
	def     int64
}

func newGatewayRig(t *testing.T, fallback string) *gwRig {
	t.Helper()
	r := &gwRig{db: store.NewMemory(), servers: map[int64]*Server{}, def: 1}
	gw := &Gateway{Dir: r.db, Fallback: fallback, DefaultTeam: r.def, Server: func(id int64) *Server {
		if r.servers[id] == nil {
			r.servers[id] = New(r.db.ForTeam(id), &live.State{}, nil)
		}
		return r.servers[id]
	}}
	r.gw = gw
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	r.url = "ws" + strings.TrimPrefix(ts.URL, "http")
	return r
}

// status dials with the token and returns the HTTP status of the handshake.
func (r *gwRig) status(token string) int { return r.statusJWT(token, "") }

// statusJWT also sends the Cloudflare Access token.
func (r *gwRig) statusJWT(token, jwt string) int {
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	if jwt != "" {
		h.Set(auth.Header, jwt)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, r.url, &websocket.DialOptions{HTTPHeader: h})
	if err == nil {
		c.CloseNow()
		return http.StatusSwitchingProtocols
	}
	return resp.StatusCode
}

func TestGatewayTeamComesFromTheToken(t *testing.T) {
	r := newGatewayRig(t, "shared")
	ctx := context.Background()
	team, _ := r.db.CreateTeam(ctx, "other")
	r.db.CreateAgent(ctx, "driver", team.ID, store.HashToken("other-token"))

	// An agent of the other team claims to be someone else in its messages; the data still goes to its own team.
	c := dial(t, r.url, "other-token")
	send(t, c, "someone-else", 1, "lap", fmt.Sprintf(lapPayload, 1))
	c2 := dial(t, r.url, "shared")
	send(t, c2, "a", 1, "lap", fmt.Sprintf(lapPayload, 1))
	send(t, c2, "a", 2, "lap", fmt.Sprintf(lapPayload, 2))

	if laps, _ := r.db.ForTeam(team.ID).Laps(ctx, "s1"); len(laps) != 1 {
		t.Fatalf("other team has %d laps, want 1", len(laps))
	}
	if laps, _ := r.db.ForTeam(r.def).Laps(ctx, "s1"); len(laps) != 2 {
		t.Fatalf("default team has %d laps, want 2", len(laps))
	}
	ag, _, _ := r.db.AgentByTokenHash(ctx, store.HashToken("other-token"))
	if ag.LastSeen == nil {
		t.Fatal("last_seen not updated")
	}
}

func TestGatewayRejectsRevokedAndUnknownTokens(t *testing.T) {
	r := newGatewayRig(t, "shared")
	ctx := context.Background()
	ag, _ := r.db.CreateAgent(ctx, "driver", 1, store.HashToken("good"))
	for token, want := range map[string]int{
		"good":    http.StatusSwitchingProtocols,
		"shared":  http.StatusSwitchingProtocols, // the fallback secret maps to the default team
		"unknown": http.StatusUnauthorized,
		"":        http.StatusUnauthorized,
	} {
		if got := r.status(token); got != want {
			t.Errorf("token %q: status %d, want %d", token, got, want)
		}
	}
	r.db.RevokeAgent(ctx, ag.ID)
	if got := r.status("good"); got != http.StatusUnauthorized {
		t.Errorf("revoked token: status %d", got)
	}
}

func TestGatewayRevokedTokenIsRejectedEvenWithoutFallback(t *testing.T) {
	r := newGatewayRig(t, "") // dev mode: unregistered tokens are accepted
	ctx := context.Background()
	ag, _ := r.db.CreateAgent(ctx, "driver", 1, store.HashToken("good"))
	r.db.RevokeAgent(ctx, ag.ID)
	if got := r.status("good"); got != http.StatusUnauthorized {
		t.Errorf("revoked token: status %d", got)
	}
	if got := r.status("anything"); got != http.StatusSwitchingProtocols {
		t.Errorf("dev mode: status %d", got)
	}
}

func TestGatewayLimitsNewConnectionsPerAgent(t *testing.T) {
	r := newGatewayRig(t, "shared")
	ctx := context.Background()
	r.db.CreateAgent(ctx, "driver", 1, store.HashToken("good"))
	r.gw.Limit = ratelimit.New(2, 2)
	for i := 0; i < 2; i++ {
		if got := r.status("good"); got != http.StatusSwitchingProtocols {
			t.Fatalf("connection %d: %d", i, got)
		}
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer good")
	_, resp, err := websocket.Dial(ctx, r.url, &websocket.DialOptions{HTTPHeader: h})
	if err == nil || resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("third connection: err=%v resp=%v", err, resp)
	}
	if got := r.status("shared"); got != http.StatusSwitchingProtocols {
		t.Fatalf("another agent: %d", got)
	}
}

// countingDir counts the lookups of agent tokens, to show that a request is turned away before its Splash token is read.
type countingDir struct {
	store.Directory
	lookups int
}

func (c *countingDir) AgentByTokenHash(ctx context.Context, h []byte) (store.Agent, bool, error) {
	c.lookups++
	return c.Directory.AgentByTokenHash(ctx, h)
}

func newAccessRig(t *testing.T) (*gwRig, *authtest.Issuer, *countingDir) {
	t.Helper()
	r := newGatewayRig(t, "shared")
	iss := authtest.NewIssuer(t)
	dir := &countingDir{Directory: r.db}
	r.gw.Dir = dir
	r.gw.Verifier = iss.Verifier()
	return r, iss, dir
}

// serviceJWT is what Access forwards for a service token: no e-mail, the client id as common name.
func serviceJWT(iss *authtest.Issuer, mod func(*authtest.Spec)) string {
	s := authtest.Spec{CommonName: "client-id.access"}
	if mod != nil {
		mod(&s)
	}
	return iss.Token(s)
}

func TestGatewayWithAccessNeedsTheJWTAndTheSplashToken(t *testing.T) {
	r, iss, _ := newAccessRig(t)
	ctx := context.Background()
	team, _ := r.db.CreateTeam(ctx, "other")
	r.db.CreateAgent(ctx, "driver", team.ID, store.HashToken("good"))
	revoked, _ := r.db.CreateAgent(ctx, "old", team.ID, store.HashToken("old"))
	r.db.RevokeAgent(ctx, revoked.ID)

	// a JWT without e-mail passes on the agent path, together with a valid Splash token
	jwt := serviceJWT(iss, nil)
	if got := r.statusJWT("good", jwt); got != http.StatusSwitchingProtocols {
		t.Fatalf("valid JWT and token: %d", got)
	}
	// the team still comes from the Splash token row
	c := dial2(t, r.url, "good", jwt)
	send(t, c, "x", 1, "lap", fmt.Sprintf(lapPayload, 1))
	if laps, _ := r.db.ForTeam(team.ID).Laps(ctx, "s1"); len(laps) != 1 {
		t.Fatalf("laps of the token's team: %d", len(laps))
	}
	if got := r.statusJWT("shared", jwt); got != http.StatusSwitchingProtocols { // the fallback is still required in addition
		t.Fatalf("fallback token: %d", got)
	}

	// the Splash token stays required
	for name, token := range map[string]string{"unknown": "nope", "revoked": "old", "none": ""} {
		if got := r.statusJWT(token, jwt); got != http.StatusUnauthorized {
			t.Errorf("valid JWT, %s Splash token: %d", name, got)
		}
	}
}

func TestGatewayWithAccessRejectsABadJWTBeforeLookingAtTheSplashToken(t *testing.T) {
	r, iss, dir := newAccessRig(t)
	r.db.CreateAgent(context.Background(), "driver", 1, store.HashToken("good"))
	forger := authtest.NewKey(t, "kid-1")
	for name, jwt := range map[string]string{
		"no JWT":         "",
		"garbage":        "a.b.c",
		"expired":        serviceJWT(iss, func(s *authtest.Spec) { s.Expires = time.Now().Add(-time.Hour) }),
		"wrong audience": serviceJWT(iss, func(s *authtest.Spec) { s.Audience = []string{"other-app"} }),
		"bad signature":  serviceJWT(iss, func(s *authtest.Spec) { s.SignWith = forger }),
		"wrong issuer":   serviceJWT(iss, func(s *authtest.Spec) { s.Issuer = "https://evil.example.com" }),
	} {
		before := dir.lookups
		if got := r.statusJWT("good", jwt); got != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, got)
		}
		if got := r.statusJWT("shared", jwt); got != http.StatusUnauthorized {
			t.Errorf("%s with the fallback token: %d, want 401", name, got)
		}
		if dir.lookups != before {
			t.Errorf("%s: the Splash token was looked up", name)
		}
	}
}

func TestGatewayWithoutAVerifierIgnoresTheJWT(t *testing.T) {
	r := newGatewayRig(t, "shared")
	r.db.CreateAgent(context.Background(), "driver", 1, store.HashToken("good"))
	for token, want := range map[string]int{"good": 101, "shared": 101, "unknown": 401} {
		if got := r.status(token); got != want {
			t.Errorf("%s: %d, want %d", token, got, want)
		}
	}
	if got := r.statusJWT("good", "not-a-jwt"); got != http.StatusSwitchingProtocols {
		t.Errorf("a JWT header must not matter without a verifier: %d", got)
	}
}

// dial2 connects with the Splash token and the Access token.
func dial2(t *testing.T, url, token, jwt string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set(auth.Header, jwt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func TestGatewayWithAccessNeverAcceptsUnregisteredTokensWithoutAFallbackSecret(t *testing.T) {
	r, iss, _ := newAccessRig(t)
	r.gw.Fallback = "" // SPLASH_AGENT_TOKEN not set
	r.db.CreateAgent(context.Background(), "driver", 1, store.HashToken("good"))
	jwt := serviceJWT(iss, nil)
	for name, token := range map[string]string{"unregistered": "anything", "none": ""} {
		if got := r.statusJWT(token, jwt); got != http.StatusUnauthorized {
			t.Errorf("%s token with a valid JWT and no fallback: %d, want 401", name, got)
		}
	}
	if got := r.statusJWT("good", jwt); got != http.StatusSwitchingProtocols {
		t.Errorf("registered token: %d", got)
	}
}

func TestGatewayWithAccessAcceptsTheFallbackSecretIntoTheDefaultTeam(t *testing.T) {
	r, iss, _ := newAccessRig(t) // fallback "shared"
	jwt := serviceJWT(iss, nil)
	c := dial2(t, r.url, "shared", jwt)
	send(t, c, "a", 1, "lap", fmt.Sprintf(lapPayload, 1))
	if laps, _ := r.db.ForTeam(r.def).Laps(context.Background(), "s1"); len(laps) != 1 {
		t.Fatalf("default team has %d laps, want 1", len(laps))
	}
	if got := r.statusJWT("not-shared", jwt); got != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d", got)
	}
}

func TestGatewayWithoutAVerifierAndWithoutFallbackStaysOpen(t *testing.T) {
	r := newGatewayRig(t, "") // dev
	for _, token := range []string{"anything", ""} {
		if got := r.status(token); got != http.StatusSwitchingProtocols {
			t.Errorf("token %q: %d, want 101 as today", token, got)
		}
	}
}

func TestServerClosesAConnectionThatDoesNotAnswerPings(t *testing.T) {
	r := newGatewayRig(t, "")
	srv := New(r.db.ForTeam(1), &live.State{}, nil)
	srv.PingInterval, srv.DeadAfter = 50*time.Millisecond, 100*time.Millisecond
	r.servers[1] = srv
	r.gw.Server = func(int64) *Server { return srv }

	// A client that never reads cannot answer pings: the server must give up on it.
	c := dial(t, r.url, "")
	time.Sleep(600 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("the dead connection was still open")
	} else if ctx.Err() != nil {
		t.Fatal("the read waited for the timeout: the server did not close the connection")
	}

	// A client that does read (and so answers pings) stays connected for longer than the dead-after time.
	live := dial(t, r.url, "")
	lctx, lcancel := context.WithCancel(context.Background())
	defer lcancel()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for {
			if _, _, err := live.Read(lctx); err != nil {
				return
			}
		}
	}()
	select {
	case <-ended:
		t.Fatal("the connection of a client that answers pings was closed")
	case <-time.After(600 * time.Millisecond):
	}
}
