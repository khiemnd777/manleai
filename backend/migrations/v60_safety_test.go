package migrations

import (
	"strings"
	"testing"
)

func TestV60SquareWebhookOperationsSafetyContract(t *testing.T) {
	raw, err := Files.ReadFile("V60__square_webhook_operations.sql")
	if err != nil {
		t.Fatalf("read V60: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"processing_status = 'dead_letter'",
		"processing_attempts >= 10",
		"SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED",
		"last_error = NULL",
		"square_booking_webhook_events_dead_letter_shape_check",
		"requeue_count BETWEEN 0 AND 3",
		"CREATE TABLE IF NOT EXISTS square_booking_webhook_actions",
		"UNIQUE (salon_id, action_key)",
		"square_booking_webhook_event_evidence_immutable_guard",
		"square_webhook_operational_error_redaction_guard",
		"square_calendar_repair_error_redaction_guard",
		"square_booking_webhook_action_immutable_guard",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V60 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"raw_payload",
		"signature_key",
		"access_token",
		"refresh_token",
		"customer_name",
		"customer_phone",
	} {
		if strings.Contains(strings.ToLower(source), forbidden) {
			t.Fatalf("V60 contains forbidden operational field %q", forbidden)
		}
	}
}
