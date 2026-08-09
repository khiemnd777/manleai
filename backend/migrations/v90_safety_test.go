package migrations

import (
	"strings"
	"testing"
)

func TestV90WorkerClaimsRecheckLiveEligibilityAtLockAndUpdate(t *testing.T) {
	raw, err := Files.ReadFile("V90__worker_claim_atomicity.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	tests := []struct {
		function       string
		identity       string
		eligibility    []string
		minimumRepeats int
	}{
		{
			function: "public.app_worker_claim_pos_sync_jobs",
			identity: "job.id = candidates.id",
			eligibility: []string{
				"job.status IN ('queued', 'failed')",
				"job.attempt_count < job.max_attempts",
				"job.next_attempt_at <= now()",
			},
			minimumRepeats: 3,
		},
		{
			function: "public.app_worker_claim_square_booking_webhooks",
			identity: "event.id = candidates.id",
			eligibility: []string{
				"event.processing_attempts < (event.requeue_count + 1)",
				"event.processing_status IN ('pending', 'failed') AND event.next_attempt_at <= now()",
				"event.processing_status = 'processing' AND event.processing_lease_expires_at < now()",
			},
			minimumRepeats: 3,
		},
		{
			function: "public.app_worker_claim_owner_notifications",
			identity: "notification.id = candidates.id",
			eligibility: []string{
				"notification.delivery_status IN ('queued', 'failed')",
				"notification.next_delivery_at <= now()",
				"notification.delivery_attempts::BIGINT <",
			},
			minimumRepeats: 3,
		},
		{
			function: "public.app_worker_claim_customer_notifications",
			identity: "delivery.id = candidates.id",
			eligibility: []string{
				"delivery.delivery_status IN ('queued', 'quiet_hours', 'failed')",
				"delivery.next_delivery_at <= now()",
				"delivery.delivery_attempts::BIGINT <",
			},
			minimumRepeats: 3,
		},
		{
			function: "public.app_worker_claim_openai_runtime_verifications",
			identity: "run.id = candidates.id",
			eligibility: []string{
				"run.attempt_count < 3",
				"run.status = 'queued' OR (run.status = 'claimed' AND run.lease_expires_at <= now())",
			},
			minimumRepeats: 3,
		},
	}
	for _, test := range tests {
		section := v90FunctionSection(t, source, test.function)
		for _, token := range []string{
			"SECURITY DEFINER",
			"SET row_security = off",
			"FOR UPDATE OF",
			"SKIP LOCKED",
			"FROM candidates",
			test.identity,
		} {
			if !strings.Contains(section, token) {
				t.Fatalf("%s missing claim atomicity token %q", test.function, token)
			}
		}
		if count := strings.Count(section, "public.app_worker_discovery_allowed()"); count < 3 {
			t.Fatalf("%s discovery scope checks=%d, want ranked/candidate/update checks", test.function, count)
		}
		for _, token := range test.eligibility {
			if count := strings.Count(section, token); count < test.minimumRepeats {
				t.Fatalf("%s eligibility token %q count=%d, want at least %d", test.function, token, count, test.minimumRepeats)
			}
		}
	}

	if strings.Contains(source, "CREATE OR REPLACE FUNCTION public.app_worker_claim_square_calendar_repairs") {
		t.Fatal("V90 must not rewrite the calendar-repair claim, whose live eligibility is already lock-bound")
	}
	for _, forbidden := range []string{"DROP FUNCTION", "DROP TABLE", "TRUNCATE ", "DELETE FROM"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V90 must remain a forward function replacement; found %q", forbidden)
		}
	}
}

func v90FunctionSection(t *testing.T, source, function string) string {
	t.Helper()
	marker := "CREATE OR REPLACE FUNCTION " + function
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("missing V90 function %s", function)
	}
	remainder := source[start+len(marker):]
	if next := strings.Index(remainder, "CREATE OR REPLACE FUNCTION "); next >= 0 {
		return source[start : start+len(marker)+next]
	}
	return source[start:]
}
