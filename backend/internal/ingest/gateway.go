package ingest

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/logx"
	"github.com/3lefeint/splash/backend/internal/ratelimit"
	"github.com/3lefeint/splash/backend/internal/store"
)

// Gateway authenticates the agent WebSocket handshake and hands the connection to the Server of the agent's team.
// The team comes from the token, never from a message.
type Gateway struct {
	Dir store.Directory
	// Verifier checks the Cloudflare Access token every request has passed on its way in. When it is set, a handshake
	// without a valid Cf-Access-Jwt-Assertion is rejected with 401 before the Splash token is looked at, so the backend
	// cannot be reached around Cloudflare. A service-token JWT has no e-mail; none is needed here. Nil (dev) skips the
	// check.
	Verifier *auth.Verifier
	// Fallback is the shared agent secret from the environment. It maps to DefaultTeam so dev runs and agents that
	// were set up before per-agent tokens keep working. Empty disables the check for tokens that are not registered, but
	// only without a Verifier: with one, an unregistered token needs the non-empty fallback secret.
	Fallback    string
	DefaultTeam int64
	// Server returns the ingest Server of a team.
	Server func(teamID int64) *Server
	// Limit limits new connections per agent. Nil means unlimited.
	Limit *ratelimit.Limiter
	// Throttle keeps floods of the same INFO line to one line per interval. Nil uses a one minute default.
	Throttle *logx.Throttle
	Log      *slog.Logger
}

func (g *Gateway) log() *slog.Logger {
	if g.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return g.Log
}

// defaultThrottle is used when the Gateway has none of its own.
var defaultThrottle = logx.NewThrottle(time.Minute)

// failed writes the INFO line for an agent that was turned away. The reason is a code (invalid_token codes of the
// Access check, unknown_token, revoked_agent); a flood becomes one line per interval and reason.
func (g *Gateway) failed(reason string, args ...any) {
	t := g.Throttle
	if t == nil {
		t = defaultThrottle
	}
	t.Log(g.log(), slog.LevelInfo, "agent:"+reason, "authentication failed", append([]any{"path", "agent", "reason", reason}, args...)...)
}

// team returns the team of the agent, or ok false with the status to answer with.
//
// Tokens are stored and compared as SHA-256 hashes: a registered token is looked up by its hash, the fallback secret
// is compared with subtle.ConstantTimeCompare over both hashes (equal length, no timing leak of the secret).
func (g *Gateway) team(r *http.Request) (team int64, key string, status int) {
	if g.Verifier != nil {
		claims, err := g.Verifier.Verify(r.Context(), r.Header.Get(auth.Header))
		switch {
		case errors.Is(err, auth.ErrInvalid):
			g.failed("access_" + auth.Reason(err))
			return 0, "", http.StatusUnauthorized
		case err != nil:
			g.log().Error("verify access token", "err", err)
			return 0, "", http.StatusServiceUnavailable
		}
		if claims.CommonName != "" {
			g.log().Debug("agent passed access", "common_name", claims.CommonName)
		}
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	hash := store.HashToken(bearer)
	if bearer != "" {
		a, ok, err := g.Dir.AgentByTokenHash(r.Context(), hash)
		if err != nil {
			g.log().Error("agent lookup failed", "err", err)
			return 0, "", http.StatusInternalServerError
		}
		if ok {
			if a.Revoked {
				g.failed("revoked_agent", "agent_id", a.ID, "team", a.TeamID)
				return 0, "", http.StatusUnauthorized
			}
			if err := g.Dir.TouchAgent(r.Context(), a.ID); err != nil {
				g.log().Warn("agent last seen", "err", err)
			}
			g.log().Debug("agent token accepted", "agent_id", a.ID, "team", a.TeamID)
			return a.TeamID, "agent:" + strconv.FormatInt(a.ID, 10), 0
		}
	}
	// Without a Verifier an empty fallback means dev mode and accepts any token. With one, the request has passed
	// Access, which proves nothing about the Splash token, so an unregistered token must equal a non-empty fallback.
	open := g.Fallback == "" && g.Verifier == nil
	if open || (g.Fallback != "" && subtle.ConstantTimeCompare(hash, store.HashToken(g.Fallback)) == 1) {
		g.log().Debug("agent accepted by the shared token or in dev mode", "team", g.DefaultTeam)
		return g.DefaultTeam, "agent:shared", 0
	}
	g.failed("unknown_token")
	return 0, "", http.StatusUnauthorized
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	team, key, status := g.team(r)
	if status != 0 {
		http.Error(w, http.StatusText(status), status)
		return
	}
	if ok, wait := g.Limit.Allow(key); !ok {
		id := logx.ShortID(key)
		t := g.Throttle
		if t == nil {
			t = defaultThrottle
		}
		t.Log(g.log(), slog.LevelInfo, "rl:"+id, "rate limited", "kind", "agent_connect", "identity", id)
		w.Header().Set("Retry-After", strconv.Itoa(max(int((wait+time.Second-1)/time.Second), 1)))
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}
	g.Server(team).ServeHTTP(w, r)
}
