package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/secretbox"
	"github.com/3lefeint/splash/backend/internal/store"
)

const memberCFSecret = "cf-secret-0123456789abcdef"

func withSecrets(t *testing.T, r *authRig) {
	t.Helper()
	box, err := secretbox.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	r.api.Secrets = box
}

func putMember(t *testing.T, r *authRig, body string) (int, memberJSON) {
	t.Helper()
	code, resp, _ := r.do(t, r.tok(adminA), "PUT", "/api/admin/members", body)
	var m memberJSON
	json.Unmarshal([]byte(resp), &m)
	return code, m
}

func TestAdminSetsNameShortAndCloudflareDataOfAMember(t *testing.T) {
	r := newAuthRig(t)
	withSecrets(t, r)
	team := itoa(r.def.ID)
	code, m := putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"name":"Driver A","short":" AB ","cf_client_id":"id-1.access","cf_secret":"`+memberCFSecret+`"}`)
	if code != 200 || m.Name != "Driver A" || m.Short != "ab" || m.CFClientID != "id-1.access" || !m.HasCFSecret {
		t.Fatalf("%d %+v", code, m)
	}
	// the secret is never sent back, and the database holds it only sealed
	for _, path := range []string{"/api/admin/members", "/api/me", "/api/agents", "/api/state"} {
		if _, body, _ := r.do(t, r.tok(adminA), "GET", path, ""); strings.Contains(body, memberCFSecret) {
			t.Fatalf("%s sends the secret", path)
		}
	}
	sealed, _ := r.db.MemberSecret(context.Background(), userA)
	if len(sealed) == 0 || bytes.Contains(sealed, []byte(memberCFSecret)) {
		t.Fatalf("stored value %v", sealed)
	}
	if plain, err := r.api.Secrets.Open(sealed, userA); err != nil || plain != memberCFSecret {
		t.Fatalf("opened %q %v", plain, err)
	}
	if _, err := r.api.Secrets.Open(sealed, userB); err == nil {
		t.Fatal("the secret must be bound to its member")
	}
	if es, _ := r.db.Audit(context.Background(), 50); strings.Contains(auditText(es), memberCFSecret) {
		t.Fatal("the secret is in the audit log")
	}

	// fields left out keep their value, the secret stays
	if code, m = putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`}`); code != 200 || m.Name != "Driver A" || m.Short != "ab" || !m.HasCFSecret {
		t.Fatalf("saving again: %d %+v", code, m)
	}
	// a changed client id without a new secret drops the old secret, which belonged to the old id
	if _, m = putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"cf_client_id":"id-2.access"}`); m.HasCFSecret || m.CFClientID != "id-2.access" {
		t.Fatalf("changed id: %+v", m)
	}
	putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"cf_secret":"`+memberCFSecret+`"}`)
	if _, m = putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"clear_cf":true}`); m.HasCFSecret || m.CFClientID != "" || m.Short != "ab" {
		t.Fatalf("cleared: %+v", m)
	}
}

func TestMemberDataIsChecked(t *testing.T) {
	r := newAuthRig(t)
	withSecrets(t, r)
	team := itoa(r.def.ID)
	putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"short":"ab"}`)
	for body, want := range map[string]int{
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"short":"AB"}`:                             409, // taken, whatever the case
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"short":"a"}`:                              400,
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"short":"toolongname"}`:                    400,
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"short":"a b"}`:                            400,
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"cf_secret":"x"}`:                          400, // no client id
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"name":"` + strings.Repeat("n", 61) + `"}`: 400,
		`{"email":"` + adminA + `","team_id":` + team + `,"is_admin":true,"short":"cd","name":"Admin"}`:              200,
	} {
		if code, _ := putMember(t, r, body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	// the same short name is free in another team
	if code, _ := putMember(t, r, `{"email":"`+userB+`","team_id":`+itoa(r.oth.ID)+`,"short":"ab"}`); code != 200 {
		t.Errorf("same short in another team: %d", code)
	}
}

func TestWithoutAKeyNoSecretIsStored(t *testing.T) {
	r := newAuthRig(t) // no SPLASH_SECRET_KEY
	team := itoa(r.def.ID)
	if code, _ := putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"cf_client_id":"id.access","cf_secret":"`+memberCFSecret+`"}`); code != 409 {
		t.Fatalf("secret without a key: %d, want 409", code)
	}
	if code, m := putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"cf_client_id":"id.access"}`); code != 200 || m.CFClientID != "id.access" || m.HasCFSecret {
		t.Fatalf("client id alone: %d %+v", code, m)
	}
}

func TestChangingTheShortNameRenamesTheAgentOfTheMember(t *testing.T) {
	r := newAuthRig(t)
	ctx := context.Background()
	team := itoa(r.def.ID)
	putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"short":"ab"}`)
	mine, _ := r.db.CreateAgent(ctx, "ab", r.def.ID, store.HashToken("m"))
	old, _ := r.db.CreateAgent(ctx, "ab", r.def.ID, store.HashToken("o"))
	other, _ := r.db.CreateAgent(ctx, "somebody", r.def.ID, store.HashToken("s"))
	r.db.SetAgentEmail(ctx, mine.ID, userA)
	r.db.SetAgentEmail(ctx, old.ID, userA)
	r.db.RevokeAgent(ctx, old.ID)
	r.db.SetAgentEmail(ctx, other.ID, adminA)
	putMember(t, r, `{"email":"`+userA+`","team_id":`+team+`,"short":"xy"}`)
	names := map[int64]string{}
	list, _ := r.db.Agents(ctx)
	for _, a := range list {
		names[a.ID] = a.Name
	}
	if names[mine.ID] != "xy" || names[old.ID] != "ab" || names[other.ID] != "somebody" {
		t.Fatalf("names %v: only the active agent of the member is renamed", names)
	}
}
