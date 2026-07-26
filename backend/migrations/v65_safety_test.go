package migrations

import (
	"strings"
	"testing"
)

func TestV65SaaSBusinessManagementContract(t *testing.T) {
	raw, err := Files.ReadFile("V65__saas_business_management.sql")
	if err != nil {
		t.Fatalf("read V65: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE business_resource_versions",
		"CREATE TABLE business_actions",
		"CREATE TABLE business_events",
		"UNIQUE (salon_id, actor_user_id, action_key)",
		"request_fingerprint ~ '^[0-9a-f]{64}$'",
		"phase3_reject_business_ledger_mutation",
		"business_actions_response_payload_check",
		"business_events_details_check",
		"phase3_ensure_business_resource_versions",
		"salons_ensure_business_resource_versions",
		"services_ensure_business_resource_version",
		"REFERENCES salons(id) ON DELETE CASCADE",
		"'staff_service_eligibility'",
		"SELECT salon_id, 'customer', id::text, 1 FROM customers",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V65 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"DROP TABLE services", "DROP TABLE staff", "DROP TABLE customers", "DROP COLUMN owner_user_id", "access_token", "refresh_token", "api_key"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V65 contains forbidden fragment %q", forbidden)
		}
	}
}
