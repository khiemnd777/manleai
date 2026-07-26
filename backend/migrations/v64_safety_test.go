package migrations

import (
	"strings"
	"testing"
)

func TestV64SaaSAccessControlFoundationContract(t *testing.T) {
	raw, err := Files.ReadFile("V64__saas_access_control_foundation.sql")
	if err != nil {
		t.Fatalf("read V64: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS scope",
		"ADD COLUMN IF NOT EXISTS delegation_scope",
		"'tenant_owner'",
		"'tenant_business_manager'",
		"'platform_admin'",
		"'platform_ops'",
		"CREATE TABLE IF NOT EXISTS salon_memberships",
		"CREATE TABLE IF NOT EXISTS platform_role_assignments",
		"CREATE TABLE IF NOT EXISTS platform_salon_assignments",
		"CREATE TABLE IF NOT EXISTS platform_salon_assignment_permissions",
		"CREATE TABLE IF NOT EXISTS platform_pii_access_grants",
		"CREATE TABLE IF NOT EXISTS access_control_actions",
		"CREATE TABLE IF NOT EXISTS access_control_events",
		"SELECT delegation_scope INTO referenced_scope",
		"referenced_scope IS DISTINCT FROM 'salon'",
		"expires_at <= created_at + interval '24 hours'",
		"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$",
		"UNIQUE (actor_user_id, action_key)",
		"phase2_reject_access_ledger_mutation",
		"INSERT INTO salon_memberships",
		"salons_sync_owner_membership",
		"salons.owner_user_id",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V64 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE user_roles",
		"DROP TABLE role_permissions",
		"DROP COLUMN owner_user_id",
		"ALTER COLUMN owner_user_id DROP NOT NULL",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V64 contains destructive compatibility change %q", forbidden)
		}
	}
}
