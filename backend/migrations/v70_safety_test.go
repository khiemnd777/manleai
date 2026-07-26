package migrations

import (
	"strings"
	"testing"
)

func TestV70PlatformAIRuntimeControlContract(t *testing.T) {
	raw, err := Files.ReadFile("V70__platform_ai_runtime_control.sql")
	if err != nil {
		t.Fatalf("read V70: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"resource_type IN ('integration_config', 'ai_runtime')",
		"resource_type = 'ai_runtime' AND resource_id = 'ai_booking'",
		"INSERT INTO technical_resource_versions",
		"CREATE TRIGGER salons_seed_ai_runtime_version",
		"AFTER INSERT ON salons",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V70 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"UPDATE salons SET ai_enabled = true", "owner_user_id"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V70 contains forbidden implicit enablement/impersonation %q", forbidden)
		}
	}
}
