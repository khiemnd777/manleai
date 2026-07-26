package migrations

import (
	"strings"
	"testing"
)

func TestV69TenantRuntimeQuotaContract(t *testing.T) {
	raw, err := Files.ReadFile("V69__tenant_runtime_quotas_usage.sql")
	if err != nil {
		t.Fatalf("read V69: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE tenant_runtime_limits",
		"CREATE TABLE tenant_usage_minute_buckets",
		"CREATE OR REPLACE FUNCTION consume_tenant_runtime_quota",
		"FOR UPDATE",
		"current_used + requested_units",
		"rejected_count",
		"worker_claims_per_batch",
		"tenant_runtime_limit_actions_immutable",
		"ENABLE ROW LEVEL SECURITY",
		"counts only",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V69 missing %q", fragment)
		}
	}
}
