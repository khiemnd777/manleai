package schedulingload

import (
	"sort"
	"time"
)

type Report struct {
	SchemaVersion       string              `json:"schema_version"`
	Release             string              `json:"release"`
	RunID               string              `json:"run_id"`
	Seed                int64               `json:"seed"`
	StartedAt           time.Time           `json:"started_at"`
	CompletedAt         time.Time           `json:"completed_at"`
	ElapsedMilliseconds int64               `json:"elapsed_ms"`
	Config              ReportConfig        `json:"config"`
	Database            DatabaseEvidence    `json:"database"`
	TenantIDs           []string            `json:"tenant_ids"`
	Workloads           []WorkloadReport    `json:"workloads"`
	Totals              Totals              `json:"totals"`
	InvariantViolations InvariantViolations `json:"invariant_violations"`
	Passed              bool                `json:"passed"`
	FailureReasons      []string            `json:"failure_reasons"`
	CapacityClaim       string              `json:"capacity_claim"`
}

type ReportConfig struct {
	Concurrency           int    `json:"concurrency"`
	OperationsPerWorkload int    `json:"operations_per_workload"`
	DurationMilliseconds  int64  `json:"duration_ms"`
	BaseTime              string `json:"base_time"`
	DatabasePrefix        string `json:"database_prefix"`
}

type DatabaseEvidence struct {
	Name                         string              `json:"name"`
	User                         string              `json:"user"`
	MigrationCount               int                 `json:"migration_count"`
	MigrationChecksumFingerprint string              `json:"migration_checksum_fingerprint"`
	Migrations                   []MigrationEvidence `json:"migrations"`
	Pool                         PoolEvidence        `json:"pool"`
}

type MigrationEvidence struct {
	Version  string `json:"version"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

type PoolEvidence struct {
	MaxOpenConnections int   `json:"max_open_connections"`
	OpenConnections    int   `json:"open_connections"`
	InUse              int   `json:"in_use"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDurationMS     int64 `json:"wait_duration_ms"`
}

type WorkloadReport struct {
	Name                string         `json:"name"`
	Attempted           int            `json:"attempted"`
	Succeeded           int            `json:"succeeded"`
	Replayed            int            `json:"replayed"`
	ExpectedConflicts   int            `json:"expected_conflicts"`
	UnexpectedErrors    int            `json:"unexpected_errors"`
	Latency             LatencySummary `json:"latency_ms"`
	ThroughputPerSecond float64        `json:"throughput_per_second"`
}

type LatencySummary struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type Totals struct {
	Attempted            int            `json:"attempted"`
	Succeeded            int            `json:"succeeded"`
	Replayed             int            `json:"replayed"`
	ExpectedConflicts    int            `json:"expected_conflicts"`
	UnexpectedErrors     int            `json:"unexpected_errors"`
	ElapsedMilliseconds  int64          `json:"elapsed_ms"`
	ThroughputPerSecond  float64        `json:"throughput_per_second"`
	WorstWorkloadLatency LatencySummary `json:"worst_workload_latency_ms"`
}

type InvariantViolations struct {
	Safety           int `json:"safety"`
	Tenant           int `json:"tenant"`
	Idempotency      int `json:"idempotency"`
	Duplicates       int `json:"duplicates"`
	Orphans          int `json:"orphans"`
	ProviderEvidence int `json:"provider_evidence"`
	ProviderCalls    int `json:"provider_calls"`
}

type operationSample struct {
	latency          time.Duration
	success          bool
	replayed         bool
	expectedConflict bool
	unexpectedError  bool
}

func summarizeWorkload(name string, started time.Time, samples []operationSample) WorkloadReport {
	report := WorkloadReport{Name: name, Attempted: len(samples)}
	latencies := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		latencies = append(latencies, sample.latency)
		if sample.success {
			report.Succeeded++
		}
		if sample.replayed {
			report.Replayed++
		}
		if sample.expectedConflict {
			report.ExpectedConflicts++
		}
		if sample.unexpectedError {
			report.UnexpectedErrors++
		}
	}
	elapsed := time.Since(started)
	if elapsed > 0 {
		report.ThroughputPerSecond = float64(report.Attempted) / elapsed.Seconds()
	}
	report.Latency = summarizeLatencies(latencies)
	return report
}

func summarizeLatencies(values []time.Duration) LatencySummary {
	if len(values) == 0 {
		return LatencySummary{}
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	ms := func(value time.Duration) float64 { return float64(value.Microseconds()) / 1000 }
	percentile := func(percent float64) float64 {
		index := int(float64(len(sorted)-1)*percent + 0.5)
		if index < 0 {
			index = 0
		}
		if index >= len(sorted) {
			index = len(sorted) - 1
		}
		return ms(sorted[index])
	}
	return LatencySummary{P50: percentile(0.50), P95: percentile(0.95), P99: percentile(0.99), Max: ms(sorted[len(sorted)-1])}
}
