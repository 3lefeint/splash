package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/store"
)

// Reasons a member cannot get the configuration of their agent yet. The page shows them as texts.
const (
	reasonNoMember     = "no_member"     // not signed in as a member (authentication is off)
	reasonNoShort      = "no_short"      // the administrator has not set a short name
	reasonNoCloudflare = "no_cloudflare" // the administrator has not entered the Cloudflare credentials
)

// myAgentJSON is what the page shows about the agent of the requesting member.
type myAgentJSON struct {
	Name  string `json:"name"`
	Short string `json:"short"`
	// Agent is the active agent of the member; nil before the first configuration.
	Agent *agentJSON `json:"agent"`
	// CanDownload says the configuration can be created now; Reason says why not.
	CanDownload bool   `json:"can_download"`
	Reason      string `json:"reason,omitempty"`
}

// memberReady loads the requesting member and says whether their agent configuration can be created, with the reason
// if not. The Cloudflare credentials are required because a backend with browser authentication also requires them on
// the agent path.
func (a *API) memberReady(r *http.Request) (m store.Member, reason string, err error) {
	me, _ := IdentityOf(r.Context())
	if me.Email == "" {
		return store.Member{}, reasonNoMember, nil
	}
	m, ok, err := a.Teams.Dir.MemberByEmail(r.Context(), me.Email)
	if err != nil {
		return m, "", err
	}
	switch {
	case !ok:
		return m, reasonNoMember, nil
	case m.Short == "":
		return m, reasonNoShort, nil
	case m.CFClientID == "" || !m.HasCFSecret || a.Secrets == nil:
		return m, reasonNoCloudflare, nil
	}
	return m, "", nil
}

// activeAgentsOf lists the agents of a member that are not revoked.
func (a *API) activeAgentsOf(r *http.Request, m store.Member) ([]store.Agent, error) {
	list, err := a.Teams.Dir.Agents(r.Context())
	if err != nil {
		return nil, err
	}
	var out []store.Agent
	for _, ag := range list {
		if ag.TeamID == m.TeamID && ag.Email == m.Email && !ag.Revoked {
			out = append(out, ag)
		}
	}
	return out, nil
}

// getMyAgent tells a member about their agent and whether they can download its configuration.
func (a *API) getMyAgent(w http.ResponseWriter, r *http.Request) {
	m, reason, err := a.memberReady(r)
	if err != nil {
		serverError(w, err)
		return
	}
	out := myAgentJSON{Name: m.Name, Short: m.Short, CanDownload: reason == "", Reason: reason}
	if reason != reasonNoMember {
		active, err := a.activeAgentsOf(r, m)
		if err != nil {
			serverError(w, err)
			return
		}
		if len(active) > 0 {
			ag := agentOf(active[len(active)-1])
			out.Agent = &ag
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// postMyAgentConfig issues a new token for the agent of the requesting member, revokes the earlier ones and answers
// with the finished agent.toml. The token exists in plain text only in this answer.
func (a *API) postMyAgentConfig(w http.ResponseWriter, r *http.Request) {
	m, reason, err := a.memberReady(r)
	if err != nil {
		serverError(w, err)
		return
	}
	if reason != "" {
		fail(w, http.StatusConflict, "the configuration cannot be created yet: "+reason)
		return
	}
	sealed, err := a.Teams.Dir.MemberSecret(r.Context(), m.Email)
	if err != nil {
		serverError(w, err)
		return
	}
	cfSecret, err := a.Secrets.Open(sealed, m.Email)
	if err != nil {
		a.Log.Error("cloudflare secret of a member cannot be opened", "member", m.Short, "err", err)
		fail(w, http.StatusConflict, "the Cloudflare secret cannot be read: ask the administrator to enter it again")
		return
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		serverError(w, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	earlier, err := a.activeAgentsOf(r, m)
	if err != nil {
		serverError(w, err)
		return
	}
	ag, err := a.Teams.Dir.CreateAgent(r.Context(), m.Short, m.TeamID, store.HashToken(token))
	if err != nil {
		serverError(w, err)
		return
	}
	if err := a.Teams.Dir.SetAgentEmail(r.Context(), ag.ID, m.Email); err != nil {
		serverError(w, err)
		return
	}
	for _, old := range earlier { // one agent per member: the new token replaces the old ones
		if err := a.Teams.Dir.RevokeAgent(r.Context(), old.ID); err != nil {
			serverError(w, err)
			return
		}
	}
	a.Teams.Get(m.TeamID).SetAgentEmail(ag.ID, m.Email)
	a.Log.Info("agent configuration issued", "agent_id", ag.ID, "team_id", m.TeamID, "replaced", len(earlier))

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="agent.toml"`)
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, agentConfig(a.agentURL(r), token, m, cfSecret))
}

// agentURL is the WebSocket address of the backend as an agent reaches it.
func (a *API) agentURL(r *http.Request) string {
	base := strings.TrimRight(a.PublicURL, "/")
	if base == "" {
		scheme := "http"
		// Browser authentication means the backend sits behind Cloudflare, which speaks https to the outside.
		if a.Verifier != nil || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base + ingest.Path
}

// tomlString writes s as a TOML basic string. The values come from the backend or the administrator and contain only
// printable ASCII (checked when they are saved), so quotes and backslashes are all that need escaping.
func tomlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// agentConfig is the finished agent.toml of a member.
func agentConfig(url, token string, m store.Member, cfSecret string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Splash agent configuration of %s, issued %s.\n", m.Short, time.Now().UTC().Format("2006-01-02"))
	b.WriteString("# Keep this file private: it holds the tokens of your agent. A new download replaces the token; the old\n")
	b.WriteString("# configuration stops working.\n\n")
	b.WriteString("[server]\n")
	fmt.Fprintf(&b, "url = %s\n", tomlString(url))
	fmt.Fprintf(&b, "token = %s\n", tomlString(token))
	fmt.Fprintf(&b, "cf_client_id = %s\n", tomlString(m.CFClientID))
	fmt.Fprintf(&b, "cf_client_secret = %s\n\n", tomlString(cfSecret))
	b.WriteString("[agent]\n")
	fmt.Fprintf(&b, "id = %s\n", tomlString(m.Short))
	return b.String()
}
