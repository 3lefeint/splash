package auth_test

import (
	"context"
	"testing"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/store"
)

func TestBootstrapAdmins(t *testing.T) {
	ctx := context.Background()
	db := store.NewMemory()
	other, _ := db.CreateTeam(ctx, "other")
	db.SetMember(ctx, store.Member{Email: "old@example.com", TeamID: other.ID})

	n, err := auth.BootstrapAdmins(ctx, db, 1, " First@Example.com, old@example.com ,,")
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if m, ok, _ := db.MemberByEmail(ctx, "first@example.com"); !ok || !m.IsAdmin || m.TeamID != 1 {
		t.Fatalf("new admin: %+v", m)
	}
	if m, _, _ := db.MemberByEmail(ctx, "old@example.com"); !m.IsAdmin || m.TeamID != other.ID {
		t.Fatalf("existing member keeps the team: %+v", m)
	}
	if _, err := auth.BootstrapAdmins(ctx, db, 1, "not-an-address"); err == nil {
		t.Fatal("expected an error")
	}
}
