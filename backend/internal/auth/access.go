// Package auth verifies the Cloudflare Access JWT that identifies a browser user.
//
// The token is accepted only after its RS256 signature, issuer, audience and expiry have been checked against the keys
// the Access team publishes. There is no mode that skips a check.
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Header is the request header Cloudflare Access puts the token in.
const Header = "Cf-Access-Jwt-Assertion"

const (
	// leeway is the clock difference tolerated between this host and the token issuer.
	leeway = 30 * time.Second
	// keyTTL is how long fetched keys are trusted before they are fetched again.
	keyTTL = time.Hour
	// refreshGap is the shortest time between two fetches caused by unknown key ids, so a flood of forged tokens
	// cannot turn into a flood of requests to the Access team.
	refreshGap = 30 * time.Second
	maxToken   = 8 << 10
	maxCerts   = 1 << 20
)

// ErrInvalid is wrapped by every error that means the token must not be trusted.
var ErrInvalid = errors.New("invalid access token")

// Verifier checks Access tokens against the keys of one Access team and one application.
type Verifier struct {
	issuer   string
	audience string
	certsURL string
	client   *http.Client
	now      func() time.Time

	log *slog.Logger

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time // last successful fetch
	triedAt   time.Time // last fetch attempt
}

// NewVerifier returns a Verifier for the Access team domain (e.g. "myteam.cloudflareaccess.com") and the audience tag
// of the application.
func NewVerifier(teamDomain, audience string) (*Verifier, error) {
	d := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(teamDomain), "https://"), "http://"), "/")
	if d == "" || strings.ContainsAny(d, "/ ?#@") {
		return nil, fmt.Errorf("invalid Access team domain %q", teamDomain)
	}
	if strings.TrimSpace(audience) == "" {
		return nil, errors.New("the Access audience is empty")
	}
	return &Verifier{
		issuer:   "https://" + d,
		audience: strings.TrimSpace(audience),
		certsURL: "https://" + d + "/cdn-cgi/access/certs",
		client:   &http.Client{Timeout: 10 * time.Second},
		now:      time.Now,
		log:      slog.New(slog.DiscardHandler),
		keys:     map[string]*rsa.PublicKey{},
	}, nil
}

// SetLogger sets where the Verifier writes its DEBUG lines (verification outcome with a reason code, key cache hits
// and refreshes) and the WARN line for a failed key fetch. Tokens are never logged.
func (v *Verifier) SetLogger(l *slog.Logger) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.log = l
}

func (v *Verifier) logger() *slog.Logger {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.log
}

// UseKeysAt makes the Verifier fetch its keys from url with client instead of the Access team. It exists for tests
// that run a fake key server; the token checks are not affected.
func (v *Verifier) UseKeysAt(url string, client *http.Client) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.certsURL, v.client = url, client
}

// SetClock replaces the clock used for expiry checks and key refresh, for tests.
func (v *Verifier) SetClock(now func() time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = now
}

// Claims are the verified claims the backend uses.
type Claims struct {
	// Email is the lower-cased address of a user; empty for a service token.
	Email string
	// CommonName is the client id of the service token, empty for a user.
	CommonName string
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtClaims struct {
	Iss   string          `json:"iss"`
	Aud   json.RawMessage `json:"aud"`
	Exp   *float64        `json:"exp"`
	Nbf   *float64        `json:"nbf"`
	Email string          `json:"email"`
	// CommonName is set by Access for service tokens (to be confirmed against real requests).
	CommonName string `json:"common_name"`
}

// InvalidError is the error for a token that must not be trusted. Reason is a short stable code for the log
// (missing, malformed, algorithm, unknown_key, bad_signature, wrong_issuer, wrong_audience, no_expiry, expired,
// not_yet_valid); it never contains parts of the token.
type InvalidError struct {
	Reason string
	Detail string
}

func (e *InvalidError) Error() string { return ErrInvalid.Error() + ": " + e.Detail }

// Is makes errors.Is(err, ErrInvalid) true.
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(reason, format string, a ...any) error {
	return &InvalidError{Reason: reason, Detail: fmt.Sprintf(format, a...)}
}

// Reason returns the reason code of an invalid-token error, or "invalid" for any other error.
func Reason(err error) string {
	var ie *InvalidError
	if errors.As(err, &ie) {
		return ie.Reason
	}
	return "invalid"
}

// Verify checks the token and returns its claims. An error wrapping ErrInvalid means the token is not trusted; any
// other error means the keys could not be fetched.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	c, err := v.verify(ctx, token)
	switch {
	case err == nil:
		v.logger().Debug("access token verified", "email_claim", c.Email != "", "common_name_claim", c.CommonName != "")
	case errors.Is(err, ErrInvalid):
		v.logger().Debug("access token rejected", "reason", Reason(err))
	}
	return c, err
}

func (v *Verifier) verify(ctx context.Context, token string) (Claims, error) {
	if token == "" || len(token) > maxToken {
		return Claims{}, invalid("missing", "missing or oversized token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, invalid("malformed", "not a JWT")
	}
	var h jwtHeader
	if err := decodePart(parts[0], &h); err != nil {
		return Claims{}, invalid("malformed", "header: %v", err)
	}
	// Only RS256 is accepted, so "none" and key-confusion tokens (HS256 with the public key) cannot pass.
	if h.Alg != "RS256" {
		return Claims{}, invalid("algorithm", "algorithm %q is not accepted", h.Alg)
	}
	if h.Kid == "" {
		return Claims{}, invalid("malformed", "no key id")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, invalid("malformed", "signature encoding")
	}
	key, err := v.key(ctx, h.Kid)
	if err != nil {
		return Claims{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return Claims{}, invalid("bad_signature", "bad signature")
	}

	var c jwtClaims
	if err := decodePart(parts[1], &c); err != nil {
		return Claims{}, invalid("malformed", "claims: %v", err)
	}
	if c.Iss != v.issuer {
		return Claims{}, invalid("wrong_issuer", "wrong issuer")
	}
	if !v.hasAudience(c.Aud) {
		return Claims{}, invalid("wrong_audience", "wrong audience")
	}
	now := v.clock()
	if c.Exp == nil {
		return Claims{}, invalid("no_expiry", "no expiry")
	}
	if now.After(unix(*c.Exp).Add(leeway)) {
		return Claims{}, invalid("expired", "expired")
	}
	if c.Nbf != nil && now.Add(leeway).Before(unix(*c.Nbf)) {
		return Claims{}, invalid("not_yet_valid", "not valid yet")
	}
	return Claims{Email: strings.ToLower(strings.TrimSpace(c.Email)), CommonName: c.CommonName}, nil
}

func (v *Verifier) clock() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now()
}

func unix(sec float64) time.Time { return time.Unix(int64(sec), 0) }

func decodePart(s string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// hasAudience reports whether the aud claim (a string or a list of strings) names the application.
func (v *Verifier) hasAudience(raw json.RawMessage) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == v.audience
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == v.audience {
				return true
			}
		}
	}
	return false
}

// key returns the public key with this id. Keys are cached; an unknown id triggers one refresh, at most once per
// refreshGap.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	if k, ok := v.keys[kid]; ok && now.Sub(v.fetchedAt) < keyTTL {
		v.log.Debug("access keys cache hit")
		return k, nil
	}
	if now.Sub(v.triedAt) >= refreshGap {
		v.triedAt = now
		keys, err := v.fetch(ctx)
		if err != nil {
			v.log.Warn("access keys fetch failed, will retry with a later request", "err", err)
			return nil, err
		}
		v.keys, v.fetchedAt = keys, now
		v.log.Debug("access keys refreshed", "keys", len(keys))
	}
	if k, ok := v.keys[kid]; ok && now.Sub(v.fetchedAt) < keyTTL {
		return k, nil
	}
	return nil, invalid("unknown_key", "unknown key id")
}

func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Access keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch Access keys: %s", resp.Status)
	}
	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCerts)).Decode(&set); err != nil {
		return nil, fmt.Errorf("fetch Access keys: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		exp := 0
		for _, b := range e {
			exp = exp<<8 | int(b)
		}
		if exp < 3 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}
	}
	if len(keys) == 0 {
		return nil, errors.New("fetch Access keys: no usable keys")
	}
	return keys, nil
}
