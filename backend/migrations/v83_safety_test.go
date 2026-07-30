package migrations

import (
	"strings"
	"testing"
)

func TestV83TwilioVoiceTenantRouteExpandSafety(t *testing.T) {
	raw, err := Files.ReadFile("V83__twilio_voice_tenant_route_expand.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"salon_integration_configs_twilio_voice_number_e164_check",
		"idx_twilio_voice_active_inbound_number",
		"CREATE OR REPLACE FUNCTION public.app_provider_twilio_voice_route_salon(target_route_id UUID)",
		"CREATE OR REPLACE FUNCTION public.app_provider_voice_route_salon(",
		"public.app_database_scope() = 'provider'",
		"public.app_request_system_salon_id() IS NULL",
		"integration.id = target_route_id",
		"integration.provider = 'twilio'",
		"integration.enabled = true",
		"integration.settings->>'voice_routing_enabled' = 'true'",
		"SET row_security = off",
		"idx_voice_webhook_events_twilio_route_verified",
		"twilio_inbound_route_verified",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V83 missing tenant-route safety token %q", token)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE",
		"DROP COLUMN",
		"CREATE OR REPLACE FUNCTION public.app_provider_voice_phone_salon",
		"salons.phone",
		"ORDER BY salon.created_at",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V83 contains expand-incompatible token %q", forbidden)
		}
	}
}
