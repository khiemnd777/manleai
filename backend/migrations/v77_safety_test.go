package migrations

import (
	"strings"
	"testing"
)

func TestV77PlatformConfigurationTransferSafety(t *testing.T) {
	raw, err := Files.ReadFile("V77__platform_configuration_transfer.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, token := range []string{
		"configuration_transfer_runs",
		"configuration_transfer_events",
		"source_salon_id <> salon_id",
		"source_fingerprint",
		"target_fences",
		"actor_user_id",
		"source_active_pos_provider",
		"requires_secret_reentry",
		"configuration_transfer_events_immutable",
		"app_rls_salon_select_allowed",
		"app_rls_salon_write_allowed",
		"ai_receptionist",
		"knowledge_base",
		"service_categories_bump_transfer_collection_version",
		"salons_bump_transfer_fences",
		"salon_settings_bump_ai_receptionist_version",
		"local_hours_bump_business_resource_version",
		"integration_configs_bump_technical_resource_version",
		"configuration_transfer_runs_guard_update",
		"app.configuration_transfer",
	} {
		if !strings.Contains(sql, token) {
			t.Fatalf("V77 missing safety token %q", token)
		}
	}
	for _, forbidden := range []string{"secrets_encrypted BYTEA", "pos_oauth_token_id", "recording_id UUID", "transcript_id UUID"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V77 must not persist excluded payload %q", forbidden)
		}
	}
}
