package api

import (
	"context"
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

// Identity is who a browser request comes from. Without authentication Email is empty and the team is "default".
type Identity struct {
	Email   string
	TeamID  int64
	Team    string
	IsAdmin bool
}

// statusError is an error that says which HTTP status to answer with.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

type identityKey struct{}

// IdentityOf returns the identity guard attached to the request.
func IdentityOf(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// identify is the default Identify. With a Verifier the Access token must be valid and its e-mail address must belong
// to a team; without one authentication is off (dev) and the request belongs to the team "default".
func (a *API) identify(r *http.Request) (Identity, error) {
	if a.Verifier == nil {
		return Identity{TeamID: a.Teams.DefaultID(), Team: store.DefaultTeamName}, nil
	}
	claims, err := a.Verifier.Verify(r.Context(), r.Header.Get(auth.Header))
	switch {
	case errors.Is(err, auth.ErrInvalid):
		a.authFailed(auth.Reason(err))
		return Identity{}, &statusError{http.StatusUnauthorized, "not authenticated"}
	case err != nil:
		a.Log.Error("verify access token", "err", err)
		return Identity{}, &statusError{http.StatusServiceUnavailable, "cannot verify the access token right now"}
	case claims.Email == "":
		a.authFailed("no_email")
		return Identity{}, &statusError{http.StatusForbidden, "the access token carries no e-mail address"}
	}
	m, ok, err := a.Teams.Dir.MemberByEmail(r.Context(), claims.Email)
	if err != nil {
		return Identity{}, err
	}
	if !ok {
		a.authFailed("unknown_member")
		return Identity{}, &statusError{http.StatusForbidden, "not a member of a team"}
	}
	return Identity{Email: m.Email, TeamID: m.TeamID, Team: m.Team, IsAdmin: m.IsAdmin}, nil
}

// authFailed writes the INFO line for a browser request that was turned away. The reason is a code, not a message
// that could carry parts of the token or an address; a flood becomes one line per interval and reason.
func (a *API) authFailed(reason string) {
	a.Throttle.Log(a.Log, slog.LevelInfo, "auth:"+reason, "authentication failed", "path", "browser", "reason", reason)
}

func isWrite(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func (a *API) limited(w http.ResponseWriter, l *ratelimit.Limiter, kind, key string) bool {
	ok, wait := l.Allow(key)
	if ok {
		return false
	}
	id := logx.ShortID(key)
	a.Throttle.Log(a.Log, slog.LevelInfo, "rl:"+kind+":"+id, "rate limited", "kind", kind, "identity", id)
	secs := int((wait + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
	fail(w, http.StatusTooManyRequests, "too many requests")
	return true
}

// guard wraps a handler: it identifies the request, limits the rate of writes and new streams per identity, and
// appends every write to the audit log before the handler runs. The audit entry holds method and path only.
func (a *API) guard(pattern string, next http.HandlerFunc) http.HandlerFunc {
	_, route, _ := strings.Cut(pattern, " ")
	stream := route == "/api/stream"
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := a.Identify(r)
		if err != nil {
			var se *statusError
			if errors.As(err, &se) {
				fail(w, se.status, se.msg)
			} else {
				a.Log.Error("identify failed", "err", err)
				fail(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
		if info := logx.ReqFrom(r.Context()); info != nil {
			info.Team = id.TeamID
		}
		key := id.Email
		if key == "" {
			key = "anonymous"
		}
		if isWrite(r.Method) {
			if a.limited(w, a.WriteLimit, "write", key) {
				return
			}
			path := r.URL.Path
			if len(path) > 300 {
				path = path[:300]
			}
			e := store.AuditEntry{TS: a.now(), Email: id.Email, Team: id.Team, Method: r.Method, Path: path}
			if err := a.Teams.Dir.AddAudit(r.Context(), e); err != nil {
				a.Log.Error("audit write failed", "err", err, "team", id.TeamID)
				fail(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if stream && a.limited(w, a.ConnLimit, "stream", key) {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	}
}

// getMe tells the frontend who it is talking to.
func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityOf(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"email": id.Email, "team_id": id.TeamID, "team": id.Team, "is_admin": id.IsAdmin})
}
