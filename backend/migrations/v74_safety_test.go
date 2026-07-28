package migrations

import (
	"strings"
	"testing"
)

func TestV74EnforcesImmutablePrincipalIsolation(t *testing.T) {
	raw, err := Files.ReadFile("V74__principal_scope_isolation.sql")
	if err != nil {
		t.Fatalf("read V74: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"principal_scope",
		"tenant",
		"platform",
		"users_principal_scope_immutable_guard",
		"salons_owner_principal_scope_fk",
		"salon_memberships_user_principal_scope_fk",
		"platform_role_assignments_user_principal_scope_fk",
		"platform_salon_assignments_user_principal_scope_fk",
		"platform_pii_access_grants_user_principal_scope_fk",
		"existing users mix tenant and platform principals",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V74 missing %q", fragment)
		}
	}
}
