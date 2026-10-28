// Package authtest is a fake Cloudflare Access for tests: it signs tokens with a locally generated RSA key and serves
// the matching key set over HTTP. Nothing here relaxes the checks of the real Verifier.
package authtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/auth"
)

// Issuer signs tokens and publishes its public keys.
type Issuer struct {
	Domain   string
	Audience string

	t       testing.TB
	srv     *httptest.Server
	mu      sync.Mutex
	current *Key
	listed  []*Key // the keys the key server publishes
	fetches int
}

// Key is an RSA key with its id.
type Key struct {
	ID  string
	Key *rsa.PrivateKey
}

// NewKey generates a key; other keys than the issuer's are used to forge signatures.
func NewKey(t testing.TB, id string) *Key {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &Key{id, k}
}

// NewIssuer starts the key server; it stops with the test.
func NewIssuer(t testing.TB) *Issuer {
	t.Helper()
	i := &Issuer{Domain: "team.example.com", Audience: "aud-tag-1", t: t}
	i.current = NewKey(t, "kid-1")
	i.listed = []*Key{i.current}
	i.srv = httptest.NewServer(http.HandlerFunc(i.serveKeys))
	t.Cleanup(i.srv.Close)
	return i
}

func (i *Issuer) serveKeys(w http.ResponseWriter, _ *http.Request) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.fetches++
	type jwk struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	for _, k := range i.listed {
		pub := k.Key.PublicKey
		set.Keys = append(set.Keys, jwk{k.ID, "RSA", "RS256", "sig",
			base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())})
	}
	json.NewEncoder(w).Encode(set)
}

// Verifier returns a Verifier that fetches its keys from this issuer.
func (i *Issuer) Verifier() *auth.Verifier {
	v, err := auth.NewVerifier(i.Domain, i.Audience)
	if err != nil {
		i.t.Fatal(err)
	}
	v.UseKeysAt(i.srv.URL, i.srv.Client())
	return v
}

// KeysURL is the address of the key set.
func (i *Issuer) KeysURL() string { return i.srv.URL }

// Fetches is how often the key set was requested.
func (i *Issuer) Fetches() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.fetches
}

// Rotate replaces the signing key by a new one with a new id; the old key is no longer published.
func (i *Issuer) Rotate() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.current = NewKey(i.t, "kid-"+time.Now().Format("150405.000000000"))
	i.listed = []*Key{i.current}
}

// Spec describes a token; empty fields get valid defaults.
type Spec struct {
	Email      string
	CommonName string // set instead of Email for a service token
	Audience   any    // string or []string; default the application's audience
	Issuer     string
	Expires    time.Time // default one hour from now
	NotBefore  time.Time
	Alg        string // default RS256
	Kid        string // default the current key's id
	SignWith   *Key   // default the current key
	NoExp      bool
}

// Token signs a token as described by s.
func (i *Issuer) Token(s Spec) string {
	i.mu.Lock()
	cur := i.current
	i.mu.Unlock()
	if s.Audience == nil {
		s.Audience = []string{i.Audience}
	}
	if s.Issuer == "" {
		s.Issuer = "https://" + i.Domain
	}
	if s.Expires.IsZero() {
		s.Expires = time.Now().Add(time.Hour)
	}
	if s.Alg == "" {
		s.Alg = "RS256"
	}
	if s.Kid == "" {
		s.Kid = cur.ID
	}
	if s.SignWith == nil {
		s.SignWith = cur
	}
	claims := map[string]any{"iss": s.Issuer, "aud": s.Audience, "email": s.Email, "iat": time.Now().Unix()}
	if s.CommonName != "" {
		claims["common_name"] = s.CommonName
	}
	if !s.NoExp {
		claims["exp"] = s.Expires.Unix()
	}
	if !s.NotBefore.IsZero() {
		claims["nbf"] = s.NotBefore.Unix()
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": s.Alg, "kid": s.Kid, "typ": "JWT"}) + "." + enc(claims)
	if s.Alg == "none" {
		return signing + "."
	}
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.SignWith.Key, crypto.SHA256, digest[:])
	if err != nil {
		i.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}
