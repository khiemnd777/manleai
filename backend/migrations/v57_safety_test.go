package migrations

import (
	"strings"
	"testing"
)

func TestV57OperationsHealthSafetyContract(t *testing.T) {
	raw, err := Files.ReadFile("V57__operations_health_job_ledger.sql")
	if err != nil {
		t.Fatalf("read V57: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE worker_job_heartbeats",
		"CREATE TABLE worker_job_runs",
		"worker_job_heartbeats_active_run_fk",
		"DEFERRABLE INITIALLY DEFERRED",
		"worker_job_runs_terminal_immutable_guard",
		"last_processed_count BETWEEN 0 AND 1000000",
		"last_error_code ~ '^[A-Z][A-Z0-9_]{0,63}$'",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V57 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"\n    payload ", "provider_entity", "customer_name", "customer_phone", "raw_error", "error_message"} {
		if strings.Contains(strings.ToLower(source), forbidden) {
			t.Fatalf("V57 contains forbidden operational data field %q", forbidden)
		}
	}
}
