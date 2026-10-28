package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/3lefeint/splash/backend/internal/store"
)

// BootstrapAdmins makes every address in the comma separated list an administrator, so the first admin can be named
// without an address being kept in the repository. An address that is not yet a member joins teamID; one that already
// belongs to a team stays there and only gets the admin flag. It returns the number of addresses handled.
func BootstrapAdmins(ctx context.Context, dir store.Directory, teamID int64, list string) (int, error) {
	n := 0
	for _, raw := range strings.Split(list, ",") {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}
		if !strings.Contains(email, "@") {
			return n, fmt.Errorf("admin address %q is not an e-mail address", raw)
		}
		m, ok, err := dir.MemberByEmail(ctx, email)
		if err != nil {
			return n, err
		}
		if !ok {
			m = store.Member{Email: email, TeamID: teamID}
		}
		m.IsAdmin = true
		if err := dir.SetMember(ctx, m); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
