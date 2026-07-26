package migrations

import (
	"strings"
	"testing"
)

func TestV61SchedulingPIIRetentionSafetyContract(t *testing.T) {
	raw, err := Files.ReadFile("V61__scheduling_pii_retention.sql")
	if err != nil {
		t.Fatalf("read V61: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"retention_safe_audit_payload",
		"INTERVAL '90 days'",
		"scheduling_requests_retention_expiry_guard",
		"scheduling_requests_redaction_shape_check",
		"scheduling_request_segments_redaction_shape_check",
		"scheduling_request_events_redaction_shape_check",
		"owner_notifications_redaction_irreversible_guard",
		"customer_notification_deliveries_redaction_irreversible_guard",
		"voice_audio_outputs_redaction_irreversible_guard",
		"destination_hash IS NULL",
		"octet_length(audio_data) = 0",
		"redaction_version = 1",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V61 missing %q", fragment)
		}
	}

	lower := strings.ToLower(source)
	for _, forbidden := range []string{
		"delete from scheduling_requests",
		"delete from owner_notifications",
		"delete from customer_notification_deliveries",
		"delete from voice_audio_outputs",
		"truncate ",
		"fallback_pending",
		"redacted_at >= expires_at",
		"raw_payload",
		"access_token",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("V61 contains forbidden retention behavior %q", forbidden)
		}
	}
}
