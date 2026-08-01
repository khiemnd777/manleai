package schedulingload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	_ "github.com/lib/pq"
)

const capacityClaim = "This bounded synthetic harness verifies scheduling safety invariants; it is not production capacity proof until an approved witnessed run is completed in a representative environment."

func Run(ctx context.Context, rawConfig Config) (Report, error) {
	config, err := rawConfig.NormalizeAndValidate()
	if err != nil {
		return Report{}, err
	}
	report := Report{
		SchemaVersion: ReportSchemaVersion,
		Release:       config.Release,
		RunID:         config.RunID,
		Seed:          config.Seed,
		StartedAt:     time.Now().UTC(),
		Config: ReportConfig{
			Concurrency:           config.Concurrency,
			OperationsPerWorkload: config.OperationsPerWorkload,
			DurationMilliseconds:  config.Duration.Milliseconds(),
			BaseTime:              config.BaseTime.Format(time.RFC3339),
			DatabasePrefix:        config.DatabasePrefix,
		},
		Workloads:      []WorkloadReport{},
		TenantIDs:      []string{},
		FailureReasons: []string{},
		CapacityClaim:  capacityClaim,
	}
	runContext, cancel := context.WithTimeout(ctx, config.Duration)
	defer cancel()

	db, err := sql.Open("postgres", config.DatabaseURL)
	if err != nil {
		return finishReport(report, nil, fmt.Errorf("open isolated database: %w", err))
	}
	defer db.Close()
	poolSize := config.Concurrency + 4
	if poolSize > MaxConcurrency {
		poolSize = MaxConcurrency
	}
	db.SetMaxOpenConns(poolSize)
	db.SetMaxIdleConns(poolSize)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(runContext); err != nil {
		return finishReport(report, db, fmt.Errorf("ping isolated database: %w", err))
	}
	databaseEvidence, err := validateDatabaseTarget(runContext, db, config)
	if err != nil {
		return finishReport(report, db, err)
	}
	report.Database = databaseEvidence

	seed, err := seedRun(runContext, db, config)
	if err != nil {
		return finishReport(report, db, err)
	}
	report.TenantIDs = []string{
		seed.OwnerManual.SalonID,
		seed.Calendar.SalonID,
		seed.SwitchReplay.SalonID,
		seed.SwitchRace.SalonID,
		seed.External.SalonID,
		seed.ExternalOther.SalonID,
	}
	sort.Strings(report.TenantIDs)

	ownerReports, ownerErr := runOwnerManualWorkload(runContext, db, config, seed, &report.InvariantViolations)
	report.Workloads = append(report.Workloads, ownerReports...)
	if ownerErr != nil {
		return finishReport(report, db, fmt.Errorf("owner_manual workload: %w", ownerErr))
	}
	calendarReports, calendarErr := runCalendarWorkload(runContext, db, config, seed, &report.InvariantViolations)
	report.Workloads = append(report.Workloads, calendarReports...)
	if calendarErr != nil {
		return finishReport(report, db, fmt.Errorf("manleai_calendar workload: %w", calendarErr))
	}
	switchReports, switchErr := runAuthoritySwitchWorkload(runContext, db, config, seed, &report.InvariantViolations)
	report.Workloads = append(report.Workloads, switchReports...)
	if switchErr != nil {
		return finishReport(report, db, fmt.Errorf("authority switch workload: %w", switchErr))
	}
	externalReports, externalEvidence, replicaPools, externalErr := runExternalSlotCommitWorkload(runContext, config, seed, &report.InvariantViolations)
	report.Workloads = append(report.Workloads, externalReports...)
	report.ExternalSlotCommit = externalEvidence
	report.Database.ReplicaPools = replicaPools
	if externalErr != nil {
		return finishReport(report, db, fmt.Errorf("external atomic slot commit workload: %w", externalErr))
	}
	return finishReport(report, db, nil)
}

func finishReport(report Report, db *sql.DB, runErr error) (Report, error) {
	report.CompletedAt = time.Now().UTC()
	report.ElapsedMilliseconds = report.CompletedAt.Sub(report.StartedAt).Milliseconds()
	if db != nil {
		stats := db.Stats()
		report.Database.Pool = PoolEvidence{
			MaxOpenConnections: stats.MaxOpenConnections,
			OpenConnections:    stats.OpenConnections,
			InUse:              stats.InUse,
			Idle:               stats.Idle,
			WaitCount:          stats.WaitCount,
			WaitDurationMS:     stats.WaitDuration.Milliseconds(),
		}
	}
	report.Totals = summarizeTotals(report.Workloads, time.Duration(report.ElapsedMilliseconds)*time.Millisecond)
	if runErr != nil {
		report.FailureReasons = append(report.FailureReasons, sanitizedFailureReason(runErr))
	}
	if report.Totals.UnexpectedErrors > 0 {
		report.FailureReasons = append(report.FailureReasons, fmt.Sprintf("unexpected workload errors: %d", report.Totals.UnexpectedErrors))
	}
	if count := violationCount(report.InvariantViolations); count > 0 {
		report.FailureReasons = append(report.FailureReasons, fmt.Sprintf("safety invariant violations: %d", count))
	}
	external := report.ExternalSlotCommit
	if external.ExpectedFakeProviderDispatches > 0 {
		if external.FakeProviderDispatches != external.ExpectedFakeProviderDispatches {
			report.FailureReasons = append(report.FailureReasons, fmt.Sprintf("fake provider dispatch mismatch: got %d want %d", external.FakeProviderDispatches, external.ExpectedFakeProviderDispatches))
		}
		if external.ObservedConflictCount != external.ExpectedConflictCount {
			report.FailureReasons = append(report.FailureReasons, fmt.Sprintf("external conflict mismatch: got %d want %d", external.ObservedConflictCount, external.ExpectedConflictCount))
		}
		if external.UnknownClaims == 0 || external.ReconciliationRequired == 0 {
			report.FailureReasons = append(report.FailureReasons, "external unknown outcome was not retained for reconciliation")
		}
		if external.ConflictLoserProviderDispatches+external.DuplicateConfirmations+external.UnexpectedClaimReleases+
			external.OrphanClaims+external.OrphanIntervals+external.OrphanEvents+external.RealProviderRuntimeCalls > 0 {
			report.FailureReasons = append(report.FailureReasons, "external atomic slot commit safety counters are non-zero")
		}
	}
	report.Passed = len(report.FailureReasons) == 0
	return report, runErr
}

func summarizeTotals(workloads []WorkloadReport, elapsed time.Duration) Totals {
	totals := Totals{ElapsedMilliseconds: elapsed.Milliseconds()}
	for _, workload := range workloads {
		totals.Attempted += workload.Attempted
		totals.Succeeded += workload.Succeeded
		totals.Replayed += workload.Replayed
		totals.ExpectedConflicts += workload.ExpectedConflicts
		totals.UnexpectedErrors += workload.UnexpectedErrors
		if workload.Latency.P50 > totals.WorstWorkloadLatency.P50 {
			totals.WorstWorkloadLatency.P50 = workload.Latency.P50
		}
		if workload.Latency.P95 > totals.WorstWorkloadLatency.P95 {
			totals.WorstWorkloadLatency.P95 = workload.Latency.P95
		}
		if workload.Latency.P99 > totals.WorstWorkloadLatency.P99 {
			totals.WorstWorkloadLatency.P99 = workload.Latency.P99
		}
		if workload.Latency.Max > totals.WorstWorkloadLatency.Max {
			totals.WorstWorkloadLatency.Max = workload.Latency.Max
		}
	}
	if elapsed > 0 {
		totals.ThroughputPerSecond = float64(totals.Attempted) / elapsed.Seconds()
	}
	return totals
}

func violationCount(violations InvariantViolations) int {
	return violations.Safety + violations.Tenant + violations.Idempotency + violations.Duplicates + violations.Orphans + violations.ProviderEvidence + violations.ProviderCalls
}

func sanitizedFailureReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "bounded run duration exceeded"
	case errors.Is(err, context.Canceled):
		return "run cancelled"
	case errors.Is(err, ErrUnsafeTarget):
		return ErrUnsafeTarget.Error()
	case errors.Is(err, ErrRunAlreadyExists):
		return ErrRunAlreadyExists.Error()
	default:
		return err.Error()
	}
}
