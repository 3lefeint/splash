package api

import (
	"strings"
	"testing"
)

func TestAdminSavesTheTeamLayoutAndMembersReadIt(t *testing.T) {
	r := newAuthRig(t)
	admin, member, other := r.tok(adminA), r.tok(userA), r.tok(userB)

	if code, body, _ := r.do(t, member, "GET", "/api/layout", ""); code != 200 || strings.TrimSpace(body) != "{}" {
		t.Fatalf("a team without a default: %d %s", code, body)
	}
	doc := `{"layout":{"items":[{"id":"fuel","x":0,"y":0,"w":4,"h":3}],"hidden":["map"]},"ranges":{"brake":{"low":300,"high":700}}}`
	if code, _, _ := r.do(t, member, "PUT", "/api/admin/layout", doc); code != 403 {
		t.Fatalf("a member saves: %d", code)
	}
	for _, bad := range []string{``, `[]`, `null`, `"x"`, `{"a":`} {
		if code, _, _ := r.do(t, admin, "PUT", "/api/admin/layout", bad); code != 400 {
			t.Errorf("save %q: %d, want 400", bad, code)
		}
	}
	if code, _, _ := r.do(t, admin, "PUT", "/api/admin/layout", `{"big":"`+strings.Repeat("x", maxBody)+`"}`); code != 400 {
		t.Errorf("an oversized document: %d, want 400", code)
	}
	if code, body, _ := r.do(t, admin, "PUT", "/api/admin/layout", doc); code != 204 {
		t.Fatalf("save: %d %s", code, body)
	}
	if code, body, _ := r.do(t, member, "GET", "/api/layout", ""); code != 200 || body != doc {
		t.Fatalf("the member reads: %d %s", code, body)
	}
	// the default belongs to the team of the administrator
	if _, body, _ := r.do(t, other, "GET", "/api/layout", ""); strings.TrimSpace(body) != "{}" {
		t.Fatalf("the other team sees %s", body)
	}
}
