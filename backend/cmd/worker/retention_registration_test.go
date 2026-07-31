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

func TestOpenAIRuntimeVerificationWorkerRegistrationContract(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read worker main: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		`openAIVerificationPollInterval`,
		`15 * time.Second`,
		`openAIVerificationBatchLimit`,
		`voice_openai.NewTenantBoundAdapter(integrationConfigService)`,
		`name:     "openai_runtime_verification"`,
		`return openAIVerificationProcessor.ProcessOnce(ctx, openAIVerificationBatchLimit)`,
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("OpenAI verification worker registration missing %q", fragment)
		}
	}
}

func TestTenantRegistrationRetentionWorkerRegistrationContract(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read worker main: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		`tenantRegistrationRetentionInterval`, `5 * time.Minute`,
		`tenantRegistrationRetentionLimit`,
		`tenantregistration.NewRetentionProcessor(tenantregistration.NewRepository(db))`,
		`name:     "tenant_registration_retention"`,
		`return tenantRegistrationRetention.ProcessOnce(ctx, tenantRegistrationRetentionLimit)`,
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("tenant registration retention worker missing %q", fragment)
		}
	}
}
