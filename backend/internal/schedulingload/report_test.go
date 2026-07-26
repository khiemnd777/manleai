package schedulingload

import (
	"testing"
	"time"
)

func TestSummarizeLatenciesUsesNearestRankAndMilliseconds(t *testing.T) {
	values := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 100 * time.Millisecond}
	got := summarizeLatencies(values)
	if got.P50 != 3 || got.P95 != 100 || got.P99 != 100 || got.Max != 100 {
		t.Fatalf("summarizeLatencies() = %#v", got)
	}
}

func TestFinishReportFailsEverySafetyViolationCategory(t *testing.T) {
	categories := []InvariantViolations{
		{Safety: 1}, {Tenant: 1}, {Idempotency: 1}, {Duplicates: 1}, {Orphans: 1}, {ProviderEvidence: 1}, {ProviderCalls: 1},
	}
	for _, violations := range categories {
		report, err := finishReport(Report{StartedAt: time.Now(), InvariantViolations: violations}, nil, nil)
		if err != nil || report.Passed || len(report.FailureReasons) == 0 {
			t.Fatalf("finishReport(%#v) = passed %v, reasons %#v, err %v", violations, report.Passed, report.FailureReasons, err)
		}
	}
}

func TestExpectedMigrationEvidenceIsVersionSortedAndChecksummed(t *testing.T) {
	items, err := expectedMigrationEvidence()
	if err != nil {
		t.Fatalf("expectedMigrationEvidence() error = %v", err)
	}
	if len(items) == 0 || len(migrationFingerprint(items)) != 64 {
		t.Fatalf("migration evidence = %#v", items)
	}
	for index, item := range items {
		if item.Version == "" || item.Name == "" || len(item.Checksum) != 64 {
			t.Fatalf("invalid migration evidence at %d: %#v", index, item)
		}
		if index > 0 && numericVersion(items[index-1].Version) >= numericVersion(item.Version) {
			t.Fatalf("migration evidence is not strictly version sorted at %d", index)
		}
	}
}
