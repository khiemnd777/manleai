package migrations

import (
	"strings"
	"testing"
)

func TestV39DuplicateNormalizationFailsClosedAfterProviderDispatch(t *testing.T) {
	source, err := Files.ReadFile("V39__booking_integrity_reconciliation_quotes.sql")
	if err != nil {
		t.Fatalf("read V39 migration: %v", err)
	}
	sql := string(source)
	requiredFragments := []string{
		"HAVING count(*) FILTER (WHERE provider_outcome <> 'not_started') > 1",
		"CASE WHEN provider_outcome <> 'not_started' THEN 0 ELSE 1 END",
		"AND ranked.provider_outcome = 'not_started'",
		"Reconcile the provider outcomes before retrying this migration.",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V39 is missing fail-closed duplicate normalization guard %q", fragment)
		}
	}
}

func TestV39PersistsProviderFenceAcrossQuoteAndAttempt(t *testing.T) {
	source, err := Files.ReadFile("V39__booking_integrity_reconciliation_quotes.sql")
	if err != nil {
		t.Fatalf("read V39 migration: %v", err)
	}
	sql := string(source)
	for _, fragment := range []string{
		"provider_location_id TEXT NOT NULL",
		"provider_snapshot_generation BIGINT NOT NULL",
		"ADD COLUMN provider_location_id TEXT",
		"ADD COLUMN provider_snapshot_generation BIGINT",
		"booking_attempts_provider_fence_pair_check",
		"provider_snapshot_generation > 0",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V39 is missing provider-fence persistence fragment %q", fragment)
		}
	}
}

func TestV39PersistsRawProviderIdentitiesForRetryAndCalendarComparison(t *testing.T) {
	source, err := Files.ReadFile("V39__booking_integrity_reconciliation_quotes.sql")
	if err != nil {
		t.Fatalf("read V39 migration: %v", err)
	}
	sql := string(source)
	for _, fragment := range []string{
		"ALTER TABLE appointments\n    ADD COLUMN pos_customer_id TEXT",
		"ALTER TABLE appointment_services\n    ADD COLUMN pos_staff_id TEXT",
		"ALTER TABLE booking_attempt_segments\n    ADD COLUMN pos_staff_id TEXT",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V39 is missing raw provider identity persistence fragment %q", fragment)
		}
	}
}
