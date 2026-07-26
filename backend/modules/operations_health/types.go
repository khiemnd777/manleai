package operationshealth

import (
	"context"
	"time"
)

const (
	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"
	RunStatusPanicked  = "panicked"
	RunStatusAbandoned = "abandoned"

	HealthHealthy  = "healthy"
	HealthRunning  = "running"
	HealthDegraded = "degraded"
	HealthStale    = "stale"
	HealthUnknown  = "unknown"

	MaxProcessedCount = 1_000_000
)

type StartRunInput struct {
	JobName          string
	WorkerInstanceID string
	Interval         time.Duration
	StaleAfter       time.Duration
	LeaseDuration    time.Duration
}

type FinishRunInput struct {
	JobName          string
	RunID            string
	WorkerInstanceID string
	Status           string
	ProcessedCount   int
	ErrorClass       string
	ErrorCode        string
}

type JobLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

type JobHealth struct {
	Key                string     `json:"key"`
	Label              string     `json:"label"`
	Status             string     `json:"status"`
	LastStartedAt      *time.Time `json:"last_started_at,omitempty"`
	LastCompletedAt    *time.Time `json:"last_completed_at,omitempty"`
	LastSuccessAt      *time.Time `json:"last_success_at,omitempty"`
	LastHeartbeatAt    *time.Time `json:"last_heartbeat_at,omitempty"`
	LastDurationMS     *int64     `json:"last_duration_ms,omitempty"`
	LastProcessedCount *int       `json:"last_processed_count,omitempty"`
	ErrorClass         string     `json:"error_class,omitempty"`
	ErrorCode          string     `json:"error_code,omitempty"`
	StaleAfterSeconds  int        `json:"stale_after_seconds"`
	Links              []JobLink  `json:"links"`
}

type QueueHealth struct {
	Key             string     `json:"key"`
	Label           string     `json:"label"`
	Status          string     `json:"status"`
	BacklogCount    int64      `json:"backlog_count"`
	OldestAt        *time.Time `json:"oldest_at,omitempty"`
	DeadLetterCount int64      `json:"dead_letter_count"`
	ErrorCode       string     `json:"error_code,omitempty"`
	Links           []JobLink  `json:"links"`
}

type StatusResponse struct {
	Status      string        `json:"status"`
	EvaluatedAt time.Time     `json:"evaluated_at"`
	Jobs        []JobHealth   `json:"jobs"`
	Queues      []QueueHealth `json:"queues"`
}

// TenantQueueMetric is the provider-neutral safe aggregate accepted from a
// provider-owned repository. It intentionally cannot carry raw errors,
// payloads, provider identities, or cross-tenant totals.
type TenantQueueMetric struct {
	Key             string
	BacklogCount    int64
	OldestAt        *time.Time
	DeadLetterCount int64
	Available       bool
	ErrorCode       string
}

type TenantMetricSnapshot struct {
	Relevant bool
	Queues   []TenantQueueMetric
}

type TenantMetricSource interface {
	LoadTenantQueueMetrics(context.Context, string, string) (TenantMetricSnapshot, error)
}

type jobRecord struct {
	Name              string
	Status            string
	StaleAfterSeconds int
	LastStartedAt     time.Time
	LastCompletedAt   *time.Time
	LastSuccessAt     *time.Time
	LastDurationMS    *int64
	ProcessedCount    *int
	ErrorClass        string
	ErrorCode         string
	HeartbeatAt       time.Time
	LeaseExpiresAt    *time.Time
}

type queueRecord struct {
	Key             string
	BacklogCount    int64
	OldestAt        *time.Time
	DeadLetterCount int64
	Available       bool
	ErrorCode       string
}
