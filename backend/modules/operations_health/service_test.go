package operationshealth

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeStatusStore struct {
	jobs   []jobRecord
	queues []queueRecord
	err    error
}

type fakeTenantMetricSource struct {
	snapshot TenantMetricSnapshot
	err      error
}

func (f fakeTenantMetricSource) LoadTenantQueueMetrics(context.Context, string, string) (TenantMetricSnapshot, error) {
	return f.snapshot, f.err
}

func (f fakeStatusStore) LoadStatus(context.Context, string, string) ([]jobRecord, []queueRecord, error) {
	return f.jobs, f.queues, f.err
}

func TestServiceClassifiesStableThresholdsAndOmitsIrrelevantProviderRows(t *testing.T) {
	now := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	lastSuccess := now.Add(-time.Minute)
	completed := now.Add(-time.Minute)
	duration := int64(23)
	processed := 4
	service := NewService(fakeStatusStore{
		jobs: []jobRecord{{
			Name: "conversation_retention", Status: RunStatusSucceeded, StaleAfterSeconds: 120,
			LastStartedAt: completed, LastCompletedAt: &completed, LastSuccessAt: &lastSuccess,
			LastDurationMS: &duration, ProcessedCount: &processed, HeartbeatAt: completed,
		}, {
			Name: "square_booking_webhooks", Status: RunStatusFailed, StaleAfterSeconds: 120,
			LastStartedAt: completed, LastCompletedAt: &completed, HeartbeatAt: completed,
			ErrorClass: "dependency", ErrorCode: "JOB_RUN_FAILED",
		}},
		queues: []queueRecord{{Key: "conversation_retention", Available: true}},
	})
	service.now = func() time.Time { return now }

	result, err := service.Get(context.Background(), "salon", "owner")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var retention *JobHealth
	for i := range result.Jobs {
		if result.Jobs[i].Key == "square_booking_webhooks" {
			t.Fatal("Square row leaked for salon without relevant provider history")
		}
		if result.Jobs[i].Key == "conversation_retention" {
			retention = &result.Jobs[i]
		}
	}
	if retention == nil || retention.Status != HealthHealthy || retention.LastProcessedCount == nil || *retention.LastProcessedCount != 4 {
		t.Fatalf("retention health = %#v", retention)
	}
	if result.Status != HealthUnknown {
		t.Fatalf("aggregate=%q, want fail-closed unknown for missing stable jobs", result.Status)
	}
}

func TestServiceUsesSafeCodesWithoutRunIdentityOrRawErrors(t *testing.T) {
	now := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	completed := now.Add(-time.Minute)
	service := NewService(fakeStatusStore{jobs: []jobRecord{{
		Name: "future_safe_job", Status: RunStatusFailed, StaleAfterSeconds: 120,
		LastStartedAt: completed, LastCompletedAt: &completed, HeartbeatAt: completed,
		ErrorClass: "dependency", ErrorCode: "JOB_RUN_FAILED",
	}}})
	service.now = func() time.Time { return now }
	result, err := service.Get(context.Background(), "salon", "owner")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(raw)
	for _, forbidden := range []string{"worker_instance", "run_id", "provider_entity_id", "sensitive upstream failure"} {
		if contains(text, forbidden) {
			t.Fatalf("response contains forbidden detail %q: %s", forbidden, text)
		}
	}
	if !contains(text, "JOB_RUN_FAILED") {
		t.Fatalf("response omitted safe code: %s", text)
	}
}

func TestServiceComposesProviderOwnedTenantMetrics(t *testing.T) {
	now := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	oldest := now.Add(-10 * time.Minute)
	service := NewService(fakeStatusStore{}, fakeTenantMetricSource{snapshot: TenantMetricSnapshot{
		Relevant: true,
		Queues: []TenantQueueMetric{{
			Key: "square_booking_webhooks", BacklogCount: 2, OldestAt: &oldest,
			DeadLetterCount: 1, Available: true,
		}},
	}})
	service.now = func() time.Time { return now }
	result, err := service.Get(context.Background(), "salon", "owner")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var webhookQueue *QueueHealth
	for index := range result.Queues {
		if result.Queues[index].Key == "square_booking_webhooks" {
			webhookQueue = &result.Queues[index]
		}
	}
	if webhookQueue == nil || webhookQueue.Status != HealthDegraded || webhookQueue.BacklogCount != 2 || webhookQueue.DeadLetterCount != 1 {
		t.Fatalf("provider-owned webhook queue = %#v", webhookQueue)
	}
}

func TestClassifyJobTreatsExpiredLeaseAndThresholdAsStale(t *testing.T) {
	now := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)
	if got := classifyJob(now, jobRecord{Status: RunStatusRunning, StaleAfterSeconds: 120, HeartbeatAt: now.Add(-time.Minute), LeaseExpiresAt: &expired}); got != HealthStale {
		t.Fatalf("expired lease status=%q", got)
	}
	if got := classifyJob(now, jobRecord{Status: RunStatusFailed, StaleAfterSeconds: 120, HeartbeatAt: now.Add(-3 * time.Minute)}); got != HealthStale {
		t.Fatalf("old failure status=%q", got)
	}
}

func TestSchedulingPIIRetentionHasStableOwnerFacingHealthPolicy(t *testing.T) {
	policy := policyFor("scheduling_pii_retention")
	if policy.label != "Scheduling PII retention" || policy.href != "/dashboard/appointments" || policy.providerSpecific || policy.queueGrace != 30*time.Minute {
		t.Fatalf("retention display policy=%#v", policy)
	}
}

func TestPlatformStatusLinksStayInsidePlatformOperationsSurface(t *testing.T) {
	service := NewService(fakeStatusStore{})
	result, err := service.buildStatus(context.Background(), "salon-123", "owner", nil, []queueRecord{{
		Key: "notification_delivery", Available: true,
	}}, true)
	if err != nil {
		t.Fatalf("buildStatus: %v", err)
	}
	for _, item := range result.Jobs {
		for _, link := range item.Links {
			if link.Href != "/platform/tenants/salon-123/operations" {
				t.Fatalf("Platform job link=%q", link.Href)
			}
		}
	}
	for _, item := range result.Queues {
		for _, link := range item.Links {
			if link.Href != "/platform/tenants/salon-123/operations" {
				t.Fatalf("Platform queue link=%q", link.Href)
			}
		}
	}
}

func contains(text, fragment string) bool {
	for i := 0; i+len(fragment) <= len(text); i++ {
		if text[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
