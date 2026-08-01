package operationshealth

import (
	"context"
	"sort"
	"strings"
	"time"
)

type Store interface {
	LoadStatus(ctx context.Context, salonID, ownerUserID string) ([]jobRecord, []queueRecord, error)
}

type platformStore interface {
	LoadStatusForSalon(ctx context.Context, salonID string) ([]jobRecord, []queueRecord, string, error)
}

type Service struct {
	store         Store
	metricSources []TenantMetricSource
	now           func() time.Time
}

func NewService(store Store, metricSources ...TenantMetricSource) *Service {
	return &Service{store: store, metricSources: metricSources, now: time.Now}
}

type displayPolicy struct {
	label            string
	href             string
	providerSpecific bool
	queueGrace       time.Duration
}

var displayPolicies = map[string]displayPolicy{
	"pos_sync_jobs":                     {"POS catalog sync", "/dashboard/integrations", true, 15 * time.Minute},
	"booking_lease_recovery":            {"Booking lease recovery", "/dashboard/appointments", true, 5 * time.Minute},
	"external_slot_claims_pre_dispatch": {"External slot claims before dispatch", "/dashboard/appointments", true, 5 * time.Minute},
	"external_slot_claims_unknown":      {"External slot outcomes awaiting reconciliation", "/dashboard/appointments", true, 5 * time.Minute},
	"availability_quote_cleanup":        {"Availability quote cleanup", "/dashboard/appointments", false, 30 * time.Minute},
	"square_booking_webhooks":           {"Square booking webhooks", "/dashboard/integrations", true, 5 * time.Minute},
	"square_calendar_repair":            {"Square calendar repair", "/dashboard/integrations", true, 15 * time.Minute},
	"conversation_retention":            {"Conversation retention", "/dashboard/calls", false, 6 * time.Hour},
	"scheduling_pii_retention":          {"Scheduling PII retention", "/dashboard/appointments", false, 30 * time.Minute},
	"tenant_registration_retention":     {"Registration request retention", "/platform/registration-requests", false, 30 * time.Minute},
	"openai_runtime_verification":       {"OpenAI runtime verification", "/platform/tenants", false, 15 * time.Minute},
	"notification_delivery":             {"Owner notification delivery", "/dashboard/appointments", false, 15 * time.Minute},
	"customer_notification_delivery":    {"Customer SMS delivery", "/dashboard/appointments", false, 15 * time.Minute},
	"customer_notifications":            {"Customer SMS queue", "/dashboard/appointments", false, 15 * time.Minute},
	"scheduling_requests":               {"Owner review requests", "/dashboard/appointments", false, 30 * time.Minute},
}

func (s *Service) Get(ctx context.Context, salonID, ownerUserID string) (*StatusResponse, error) {
	if s == nil || s.store == nil || strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	jobs, queues, err := s.store.LoadStatus(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return s.buildStatus(ctx, salonID, ownerUserID, jobs, queues, false)
}

func (s *Service) GetForPlatform(ctx context.Context, salonID string) (*StatusResponse, error) {
	if s == nil || s.store == nil || strings.TrimSpace(salonID) == "" {
		return nil, ErrValidation
	}
	store, ok := s.store.(platformStore)
	if !ok {
		return nil, ErrValidation
	}
	jobs, queues, ownerUserID, err := store.LoadStatusForSalon(ctx, salonID)
	if err != nil {
		return nil, err
	}
	return s.buildStatus(ctx, salonID, ownerUserID, jobs, queues, true)
}

func (s *Service) buildStatus(ctx context.Context, salonID, ownerUserID string, jobs []jobRecord, queues []queueRecord, platform bool) (*StatusResponse, error) {
	providerRelevant := false
	for _, source := range s.metricSources {
		if source == nil {
			continue
		}
		snapshot, sourceErr := source.LoadTenantQueueMetrics(ctx, salonID, ownerUserID)
		if sourceErr != nil {
			return nil, sourceErr
		}
		providerRelevant = providerRelevant || snapshot.Relevant
		for _, metric := range snapshot.Queues {
			queues = append(queues, queueRecord{
				Key: metric.Key, BacklogCount: metric.BacklogCount,
				OldestAt: metric.OldestAt, DeadLetterCount: metric.DeadLetterCount,
				Available: metric.Available, ErrorCode: metric.ErrorCode,
			})
		}
	}
	now := s.now().UTC()
	response := &StatusResponse{Status: HealthHealthy, EvaluatedAt: now, Jobs: []JobHealth{}, Queues: []QueueHealth{}}
	records := make(map[string]jobRecord, len(jobs))
	for _, item := range jobs {
		records[item.Name] = item
	}

	keys := make([]string, 0, len(displayPolicies)+len(records))
	seen := map[string]bool{}
	for key, policy := range displayPolicies {
		if key == "scheduling_requests" || (policy.providerSpecific && !providerRelevant) {
			continue
		}
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range records {
		policy := displayPolicies[key]
		if seen[key] || (policy.providerSpecific && !providerRelevant) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		policy := policyFor(key)
		item, ok := records[key]
		health := JobHealth{Key: key, Label: policy.label, Status: HealthUnknown, Links: linksFor(policy)}
		if ok {
			health.LastStartedAt = timePointer(item.LastStartedAt)
			health.LastCompletedAt = item.LastCompletedAt
			health.LastSuccessAt = item.LastSuccessAt
			health.LastHeartbeatAt = timePointer(item.HeartbeatAt)
			health.LastDurationMS = item.LastDurationMS
			health.LastProcessedCount = item.ProcessedCount
			health.ErrorClass = item.ErrorClass
			health.ErrorCode = item.ErrorCode
			health.StaleAfterSeconds = item.StaleAfterSeconds
			health.Status = classifyJob(now, item)
		}
		response.Jobs = append(response.Jobs, health)
		response.Status = combineHealth(response.Status, health.Status)
	}

	for _, item := range queues {
		policy := policyFor(item.Key)
		queue := QueueHealth{
			Key: item.Key, Label: policy.label, Status: HealthUnknown,
			BacklogCount: item.BacklogCount, OldestAt: item.OldestAt,
			DeadLetterCount: item.DeadLetterCount, ErrorCode: item.ErrorCode,
			Links: linksFor(policy),
		}
		if item.Available {
			queue.Status = HealthHealthy
			if item.DeadLetterCount > 0 || (item.BacklogCount > 0 && item.OldestAt != nil && now.Sub(*item.OldestAt) > policy.queueGrace) {
				queue.Status = HealthDegraded
			}
		}
		response.Queues = append(response.Queues, queue)
		response.Status = combineHealth(response.Status, queue.Status)
	}
	if platform {
		operationsHref := "/platform/tenants/" + salonID + "/operations"
		for i := range response.Jobs {
			for j := range response.Jobs[i].Links {
				response.Jobs[i].Links[j].Href = operationsHref
			}
		}
		for i := range response.Queues {
			for j := range response.Queues[i].Links {
				response.Queues[i].Links[j].Href = operationsHref
			}
		}
	}
	return response, nil
}

func classifyJob(now time.Time, item jobRecord) string {
	staleAfter := time.Duration(item.StaleAfterSeconds) * time.Second
	if staleAfter <= 0 || now.Sub(item.HeartbeatAt) > staleAfter {
		return HealthStale
	}
	if item.Status == RunStatusRunning {
		if item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) {
			return HealthStale
		}
		return HealthRunning
	}
	if item.Status != RunStatusSucceeded {
		return HealthDegraded
	}
	if item.LastSuccessAt == nil || now.Sub(*item.LastSuccessAt) > staleAfter {
		return HealthStale
	}
	return HealthHealthy
}

func combineHealth(current, next string) string {
	rank := map[string]int{HealthHealthy: 0, HealthRunning: 0, HealthDegraded: 1, HealthStale: 2, HealthUnknown: 3}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func policyFor(key string) displayPolicy {
	if policy, ok := displayPolicies[key]; ok {
		return policy
	}
	label := strings.ReplaceAll(key, "_", " ")
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	return displayPolicy{label: label, queueGrace: 30 * time.Minute}
}

func linksFor(policy displayPolicy) []JobLink {
	if policy.href == "" {
		return []JobLink{}
	}
	return []JobLink{{Label: "Open", Href: policy.href}}
}

func timePointer(value time.Time) *time.Time { item := value.UTC(); return &item }
