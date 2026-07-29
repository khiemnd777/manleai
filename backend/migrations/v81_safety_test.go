package migrations

import (
	"strings"
	"testing"
)

func TestV81ConversationAnswerContextCollectionFenceSafety(t *testing.T) {
	raw, err := Files.ReadFile("V81__conversation_answer_context_resource_versions.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"('service'), ('staff')",
		"(NEW.id, 'service', 'collection', 1)",
		"(NEW.id, 'staff', 'collection', 1)",
		"phase13_bump_answer_context_collection_version",
		"services_bump_answer_context_collection_insert_delete",
		"services_bump_answer_context_collection_update",
		"service_category_id",
		"pos_service_version",
		"staff_bump_answer_context_collection_insert_delete",
		"staff_bump_answer_context_collection_update",
		"AFTER UPDATE OF name, ai_bookable, active, archived_at, pos_provider, sync_status",
		"ON CONFLICT (salon_id, resource_type, resource_id)",
		"version = business_resource_versions.version + 1",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V81 missing answer-context fence token %q", token)
		}
	}
	for _, forbidden := range []string{
		"ALTER TABLE business_resource_versions",
		"AFTER UPDATE OF phone",
		"AFTER UPDATE OF email",
		"app.configuration_transfer",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V81 contains unsafe or over-broad token %q", forbidden)
		}
	}
}
