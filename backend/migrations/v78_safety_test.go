package migrations

import (
	"strings"
	"testing"
)

func TestV78SystemTenantContextExpandSafety(t *testing.T) {
	raw, err := Files.ReadFile("V78__system_tenant_context_expand.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, token := range []string{
		"app_request_system_salon_id",
		"app.system_salon_id",
		"app_provider_voice_route_salon",
		"app_provider_voice_phone_salon",
		"app_provider_owner_message_salon",
		"app_provider_customer_message_salon",
		"app_provider_square_webhook_targets",
		"public.app_database_scope() = 'provider'",
		"SECURITY DEFINER",
		"SET row_security = off",
		"LIMIT 2",
	} {
		if !strings.Contains(sql, token) {
			t.Fatalf("V78 missing expand-phase safety token %q", token)
		}
	}
	for _, forbidden := range []string{
		"CREATE OR REPLACE FUNCTION public.app_rls_salon_select_allowed",
		"CREATE OR REPLACE FUNCTION public.app_rls_salon_write_allowed",
		"DROP POLICY",
		"CREATE POLICY",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V78 expand phase must not tighten runtime RLS with %q", forbidden)
		}
	}
}
