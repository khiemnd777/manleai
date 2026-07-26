package schedulingretention

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryUsesBoundedSkipLockedRedactionWithoutProviderCalls(t *testing.T) {
	raw, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository: %v", err)
	}
	source := string(raw)
	if got := strings.Count(source, "FOR UPDATE"); got < 6 {
		t.Fatalf("FOR UPDATE count=%d, want all expiry and redaction work items locked", got)
	}
	if got := strings.Count(source, "SKIP LOCKED"); got < 6 {
		t.Fatalf("SKIP LOCKED count=%d, want all expiry and redaction work items concurrent-safe", got)
	}
	for _, fragment := range []string{
		"LIMIT 1",
		"retention_expires_at <= now()",
		"expires_at <= now()",
		"delivery_claim_token IS NULL",
		"processing_token IS NULL",
		"task.status='resolved'",
		"destination_hash=NULL",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("retention repository missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"fallback_pending", "DELETE FROM", "UPDATE owner_notifications notification", "UPDATE customer_notification_deliveries delivery", "http.", "twilio", "square"} {
		if strings.Contains(strings.ToLower(source), strings.ToLower(forbidden)) {
			t.Fatalf("retention repository contains forbidden dependency/behavior %q", forbidden)
		}
	}
}
