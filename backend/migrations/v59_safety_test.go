package migrations

import (
	"strings"
	"testing"
)

func TestV59CustomerSMSSafetyContract(t *testing.T) {
	raw, err := Files.ReadFile("V59__customer_sms_consent_delivery.sql")
	if err != nil {
		t.Fatalf("read V59: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"customer_sms_enabled BOOLEAN NOT NULL DEFAULT false",
		"customer_sms_policy_version BIGINT NOT NULL DEFAULT 1",
		"CREATE TABLE customer_sms_consents",
		"CREATE TABLE customer_sms_consent_events",
		"CREATE TABLE customer_notification_deliveries",
		"CREATE TABLE customer_notification_delivery_attempts",
		"CREATE TABLE customer_notification_delivery_events",
		"customer_sms_consent_events_immutable_guard",
		"customer_notification_delivery_events_immutable_guard",
		"CHECK (delivery_attempts >= 0 AND requeue_count BETWEEN 0 AND 1)",
		"customer_notification_deliveries_source_version_check CHECK (source_version >= 0)",
		"Reply STOP to opt out.",
		"It is not confirmed yet.",
		"scheduling_requests_customer_notification_outbox",
		"appointments_customer_notification_outbox",
		"attempt_row.source = 'pos_calendar_sync'",
		"ON CONFLICT (salon_id, dedupe_key) DO NOTHING",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V59 missing %q", fragment)
		}
	}
	lower := strings.ToLower(source)
	for _, forbidden := range []string{
		"body ilike",
		"body ~",
		"customer_sms_enabled boolean not null default true",
		"on conflict (salon_id, dedupe_key) do update",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("V59 contains forbidden customer SMS behavior %q", forbidden)
		}
	}
}
