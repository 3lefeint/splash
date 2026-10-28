package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/auth/authtest"
)

func verify(v *auth.Verifier, token string) (auth.Claims, error) {
	return v.Verify(context.Background(), token)
}

func TestValidTokenGivesTheLowerCasedEmail(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := iss.Verifier()
	c, err := verify(v, iss.Token(authtest.Spec{Email: "Driver@Example.com"}))
	if err != nil || c.Email != "driver@example.com" {
		t.Fatalf("claims=%+v err=%v", c, err)
	}
	// the audience may also be a plain string
	if _, err := verify(v, iss.Token(authtest.Spec{Email: "a@example.com", Audience: iss.Audience})); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidTokensAreRejected(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := iss.Verifier()
	forger := authtest.NewKey(t, "kid-1") // same key id, other key
	for name, spec := range map[string]authtest.Spec{
		"expired":        {Email: "a@example.com", Expires: time.Now().Add(-time.Hour)},
		"wrong audience": {Email: "a@example.com", Audience: []string{"other-app"}},
		"empty audience": {Email: "a@example.com", Audience: []string{}},
		"wrong issuer":   {Email: "a@example.com", Issuer: "https://evil.example.com"},
		"bad signature":  {Email: "a@example.com", SignWith: forger},
		"alg none":       {Email: "a@example.com", Alg: "none"},
		"alg HS256":      {Email: "a@example.com", Alg: "HS256"},
		"no expiry":      {Email: "a@example.com", NoExp: true},
		"not yet valid":  {Email: "a@example.com", NotBefore: time.Now().Add(time.Hour)},
		"unknown key id": {Email: "a@example.com", Kid: "nope"},
	} {
		if _, err := verify(v, iss.Token(spec)); !errors.Is(err, auth.ErrInvalid) {
			t.Errorf("%s: err=%v, want ErrInvalid", name, err)
		}
	}
	for _, junk := range []string{"", "abc", "a.b", "a.b.c", "a.b.c.d"} {
		if _, err := verify(v, junk); !errors.Is(err, auth.ErrInvalid) {
			t.Errorf("%q: err=%v", junk, err)
		}
	}
}

func TestTamperedPayloadIsRejected(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := iss.Verifier()
	good := iss.Token(authtest.Spec{Email: "a@example.com"})
	evil := iss.Token(authtest.Spec{Email: "admin@example.com", SignWith: authtest.NewKey(t, "kid-1")})
	// the payload of the forged token with the signature of the good one
	g, e := split(good), split(evil)
	if _, err := verify(v, e[0]+"."+e[1]+"."+g[2]); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func split(tok string) [3]string {
	var out [3]string
	i := 0
	for _, r := range tok {
		if r == '.' {
			i++
			continue
		}
		out[i] += string(r)
	}
	return out
}

func TestKeysAreCachedAndRefreshedOnAnUnknownKeyID(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := iss.Verifier()
	clock := time.Now()
	v.SetClock(func() time.Time { return clock })

	for i := 0; i < 3; i++ {
		if _, err := verify(v, iss.Token(authtest.Spec{Email: "a@example.com"})); err != nil {
			t.Fatal(err)
		}
	}
	if n := iss.Fetches(); n != 1 {
		t.Fatalf("keys fetched %d times, want 1 (cached)", n)
	}

	iss.Rotate()
	tok := iss.Token(authtest.Spec{Email: "a@example.com"})
	// right after the last fetch an unknown key id must not cause another request
	if _, err := verify(v, tok); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
	if n := iss.Fetches(); n != 1 {
		t.Fatalf("keys fetched %d times, a flood of unknown ids must not reach the key server", n)
	}

	clock = clock.Add(time.Minute)
	if _, err := verify(v, tok); err != nil {
		t.Fatalf("after the refresh gap the new key must be found: %v", err)
	}
	if n := iss.Fetches(); n != 2 {
		t.Fatalf("keys fetched %d times, want 2", n)
	}
	// the old key is gone with the rotation: its tokens fail now
	oldKey := authtest.NewKey(t, "kid-1")
	if _, err := verify(v, iss.Token(authtest.Spec{Email: "a@example.com", Kid: "kid-1", SignWith: oldKey})); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestKeysExpireFromTheCache(t *testing.T) {
	iss := authtest.NewIssuer(t)
	v := iss.Verifier()
	clock := time.Now()
	v.SetClock(func() time.Time { return clock })
	tok := func() string {
		return iss.Token(authtest.Spec{Email: "a@example.com", Expires: clock.Add(10 * time.Hour)})
	}
	if _, err := verify(v, tok()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Hour)
	if _, err := verify(v, tok()); err != nil {
		t.Fatal(err)
	}
	if n := iss.Fetches(); n != 2 {
		t.Fatalf("keys fetched %d times, want 2 after the cache lifetime", n)
	}
}

func TestNewVerifierRejectsBadConfig(t *testing.T) {
	for _, c := range [][2]string{{"", "aud"}, {"team.example.com", ""}, {"team.example.com/path", "aud"}} {
		if _, err := auth.NewVerifier(c[0], c[1]); err == nil {
			t.Errorf("%q should be rejected", c)
		}
	}
	if _, err := auth.NewVerifier("https://team.example.com/", "aud"); err != nil {
		t.Error(err)
	}
}
