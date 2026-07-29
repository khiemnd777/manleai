package migrations

import (
	"strings"
	"testing"
)

func TestV80StrictSystemTenantRLSContract(t *testing.T) {
	raw, err := Files.ReadFile("V80__strict_system_tenant_rls_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"app_rls_system_salon_allowed",
		"app_request_system_salon_id() = target_salon_id",
		"COALESCE(",
		"CREATE OR REPLACE FUNCTION public.app_rls_salon_select_allowed",
		"CREATE OR REPLACE FUNCTION public.app_rls_salon_write_allowed",
		"('call_sessions')",
		"prefix || '_select'",
		"saas_rls_call_transcript_messages_insert",
		"saas_rls_voice_audio_outputs_insert",
		"saas_rls_voice_webhook_events_insert",
		"saas_rls_service_aliases_select",
		"saas_rls_owner_corrections_select",
		"platform_support_requests_select",
		"platform_support_permissions_all",
		"pg_catalog.pg_policies",
		"strict system tenant RLS policy audit failed",
		"strict_system_tenant_rls_policy_audit",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V80 missing strict contract token %q", token)
		}
	}
	for _, forbidden := range []string{
		"WHEN 'worker' THEN true",
		"WHEN 'provider' THEN true",
		"app_database_scope() IN ('worker', 'provider')",
		"UPDATE public.call_transcript_messages SET salon_id",
		"UPDATE public.handoff_requests SET salon_id",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V80 contains unsafe system tenant shortcut %q", forbidden)
		}
	}
}
