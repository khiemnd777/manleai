package migrations

import (
	"strings"
	"testing"
)

func TestV66SaaSTechnicalControlPlaneContract(t *testing.T) {
	raw, err := Files.ReadFile("V66__saas_technical_control_plane.sql")
	if err != nil {
		t.Fatalf("read V66: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE technical_resource_versions",
		"CREATE TABLE technical_actions",
		"CREATE TABLE technical_events",
		"UNIQUE (salon_id, actor_user_id, action_key)",
		"result_version = previous_version + 1",
		"technical_actions_details_safe",
		"technical_events_details_safe",
		"phase5_reject_technical_ledger_mutation",
		"'integration_config'",
		"'square', 'twilio', 'openai'",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V66 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"secrets_encrypted", "access_token_encrypted", "refresh_token_encrypted", "client_secret", "auth_token", "api_key"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V66 audit ledger contains forbidden secret-bearing fragment %q", forbidden)
		}
	}
}
