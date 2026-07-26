package tenantruntime

import "time"

const (
	MetricExpensiveRequest = "expensive_request"
	MetricSchedulingWrite  = "scheduling_write"
	MetricProviderWrite    = "provider_write"
	MetricVoiceStart       = "voice_start"
)

type Limits struct {
	SalonID                    string    `json:"salon_id"`
	ExpensiveRequestsPerMinute int       `json:"expensive_requests_per_minute"`
	SchedulingWritesPerMinute  int       `json:"scheduling_writes_per_minute"`
	ProviderWritesPerMinute    int       `json:"provider_writes_per_minute"`
	VoiceStartsPerMinute       int       `json:"voice_starts_per_minute"`
	WorkerClaimsPerBatch       int       `json:"worker_claims_per_batch"`
	Version                    int64     `json:"version"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type UpdateLimitsRequest struct {
	ActionKey                  string `json:"action_key"`
	ExpectedVersion            int64  `json:"expected_version"`
	ExpensiveRequestsPerMinute int    `json:"expensive_requests_per_minute"`
	SchedulingWritesPerMinute  int    `json:"scheduling_writes_per_minute"`
	ProviderWritesPerMinute    int    `json:"provider_writes_per_minute"`
	VoiceStartsPerMinute       int    `json:"voice_starts_per_minute"`
	WorkerClaimsPerBatch       int    `json:"worker_claims_per_batch"`
}

type Decision struct {
	Allowed       bool      `json:"allowed"`
	Metric        string    `json:"metric"`
	Limit         int       `json:"limit"`
	Used          int64     `json:"used"`
	Remaining     int64     `json:"remaining"`
	Rejected      int64     `json:"rejected"`
	ResetAt       time.Time `json:"reset_at"`
	RetryAfterSec int       `json:"retry_after_seconds"`
}

type UsageMetric struct {
	Metric   string `json:"metric"`
	Used     int64  `json:"used"`
	Rejected int64  `json:"rejected"`
	Limit    int    `json:"current_per_minute_limit"`
}

type RuntimeProfile struct {
	Limits        Limits        `json:"limits"`
	WindowMinutes int           `json:"window_minutes"`
	Usage         []UsageMetric `json:"usage"`
	EvaluatedAt   time.Time     `json:"evaluated_at"`
}
