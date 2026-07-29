package migrations

import (
	"strings"
	"testing"
)

func TestV79SystemTenantContractPreparationSafety(t *testing.T) {
	raw, err := Files.ReadFile("V79__system_tenant_contract_preparation.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"app_worker_discovery_allowed",
		"public.app_database_scope() = 'worker'",
		"public.app_request_system_salon_id() IS NULL",
		"app_worker_claim_pos_sync_jobs",
		"app_worker_expired_booking_leases",
		"app_worker_cleanup_availability_quotes",
		"app_worker_claim_square_booking_webhooks",
		"app_worker_claim_square_calendar_repairs",
		"app_worker_claim_owner_notifications",
		"app_worker_expired_owner_notification_leases",
		"app_worker_claim_customer_notifications",
		"app_worker_expired_customer_notification_leases",
		"app_worker_expired_call_sessions",
		"app_worker_scheduling_retention_candidate",
		"SECURITY DEFINER",
		"SET row_security = off",
		"FOR UPDATE OF",
		"SKIP LOCKED",
		"call child tenant preflight failed",
		"FOREIGN KEY (salon_id, session_id)",
		"FOREIGN KEY (salon_id, call_session_id)",
		"REFERENCES public.call_sessions(salon_id, id)",
		"VALIDATE CONSTRAINT",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V79 missing contract-preparation token %q", token)
		}
	}
	for _, forbidden := range []string{
		"CREATE OR REPLACE FUNCTION public.app_rls_salon_select_allowed",
		"CREATE OR REPLACE FUNCTION public.app_rls_salon_write_allowed",
		"DROP POLICY",
		"CREATE POLICY",
		"UPDATE public.call_transcript_messages SET salon_id",
		"UPDATE public.handoff_requests SET salon_id",
		"DELETE FROM public.call_transcript_messages",
		"DELETE FROM public.handoff_requests",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V79 must remain additive and fail closed; found %q", forbidden)
		}
	}
}
