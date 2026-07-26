package main

import (
	"os"
	"strings"
	"testing"
)

func TestSchedulingPIIRetentionWorkerRegistrationContract(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read worker main: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		`schedulingPIIRetentionPollInterval`,
		`5 * time.Minute`,
		`schedulingPIIRetentionBatchLimit`,
		`schedulingretention.DefaultProcessBatch`,
		`schedulingPIIRetention := schedulingretention.NewProcessor(schedulingretention.NewRepository(db))`,
		`name:     "scheduling_pii_retention"`,
		`return schedulingPIIRetention.ProcessOnce(ctx, schedulingPIIRetentionBatchLimit)`,
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("worker registration missing %q", fragment)
		}
	}
}
