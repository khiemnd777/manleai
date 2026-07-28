package migrations

import (
	"strings"
	"testing"
)

func TestV75DefinesOwnerAuthorizedPlatformSupportWithoutImplicitAccess(t *testing.T) {
	raw, err := Files.ReadFile("V75__owner_authorized_platform_support.sql")
	if err != nil {
		t.Fatalf("read V75: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"platform_support_access_requests",
		"platform_support_access_request_permissions",
		"pending_owner_review",
		"approved_expires_at > now()",
		"app_active_support_authorization",
		"app_active_support_pii_grant",
		"account.principal_scope = 'platform'",
		"role.name = 'platform_admin'",
		"role.name = 'platform_ops'",
		"base_permission.name = required_capability",
		"app_actor_feature_access",
		"app_rls_feature_access",
		"support_access_request_id",
		"services.read",
		"training.write",
		"calls.redact",
		"interval '24 hours'",
		"interval '30 days'",
		"owner_corrections",
		"('services', 'services.read', 'services.write'",
		"('service_categories', 'services.read', 'services.write'",
		"('service_consultation_profiles', 'services.read', 'services.write'",
		"public.app_rls_feature_access(salon_id, ''calls.read'', ''calls'')",
		"saas_rls_call_sessions_insert",
		"saas_rls_call_transcript_messages_update",
		"'calls.simulate', 'calls'",
		"app_calls_linked_scheduling_row",
		"saas_rls_booking_attempts_calls_select",
		"saas_rls_appointments_calls_select",
		"FOR SELECT USING (public.app_calls_linked_scheduling_row",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V75 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "INSERT INTO platform_support_access_requests") {
		t.Fatal("V75 must not seed or implicitly approve Platform support access")
	}
	if strings.Contains(sql, "public.app_rls_salon_select_allowed(salon_id, 'calls', false)") {
		t.Fatal("V75 must replace legacy direct Calls PII grant policies with Owner-linked feature policies")
	}
}
