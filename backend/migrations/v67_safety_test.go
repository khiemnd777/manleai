package migrations

import (
	"strings"
	"testing"
)

func TestV67TenantRuntimeMembershipBoundary(t *testing.T) {
	raw, err := Files.ReadFile("V67__tenant_runtime_membership_boundary.sql")
	if err != nil {
		t.Fatalf("read V67: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"CREATE OR REPLACE FUNCTION public.has_active_tenant_membership",
		"salon_memberships",
		"membership.status = 'active'",
		"role.scope = 'tenant'",
		"'tenant_owner', 'tenant_business_manager'",
		"CREATE OR REPLACE FUNCTION public.has_platform_salon_capability",
		"platform_salon_assignment_permissions",
		"permission.name = required_capability",
		"role.name = 'platform_admin'",
		"role.name = 'platform_ops'",
		"SECURITY INVOKER",
		"preserves the actual actor",
		"without impersonating a tenant owner",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V67 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"SECURITY DEFINER", "owner_user_id = actor_user_id", "SET ROLE", "BYPASSRLS"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V67 contains forbidden privilege shortcut %q", forbidden)
		}
	}
}
