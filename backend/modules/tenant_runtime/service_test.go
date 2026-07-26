package tenantruntime

import "testing"

func TestValidLimitsKeepsEveryTenantControlBounded(t *testing.T) {
	valid := UpdateLimitsRequest{
		ExpensiveRequestsPerMinute: 60,
		SchedulingWritesPerMinute:  120,
		ProviderWritesPerMinute:    30,
		VoiceStartsPerMinute:       30,
		WorkerClaimsPerBatch:       2,
	}
	if !validLimits(valid) {
		t.Fatal("valid runtime limits rejected")
	}
	valid.WorkerClaimsPerBatch = 51
	if validLimits(valid) {
		t.Fatal("unbounded worker claim limit accepted")
	}
	valid.WorkerClaimsPerBatch = 2
	valid.VoiceStartsPerMinute = 0
	if validLimits(valid) {
		t.Fatal("zero voice quota accepted")
	}
}

func TestLimitsFingerprintExcludesActionKeyButIncludesVersionedIntent(t *testing.T) {
	first := UpdateLimitsRequest{ActionKey: "first", ExpectedVersion: 1, ExpensiveRequestsPerMinute: 60, SchedulingWritesPerMinute: 120, ProviderWritesPerMinute: 30, VoiceStartsPerMinute: 30, WorkerClaimsPerBatch: 2}
	retry := first
	retry.ActionKey = "retry"
	if LimitsFingerprint(first) != LimitsFingerprint(retry) {
		t.Fatal("action key changed the limits intent fingerprint")
	}
	retry.ProviderWritesPerMinute++
	if LimitsFingerprint(first) == LimitsFingerprint(retry) {
		t.Fatal("changed limits reused the same fingerprint")
	}
}
