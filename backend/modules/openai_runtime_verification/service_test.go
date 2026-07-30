package openairuntimeverification

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestVerificationPlanRequiresRealtimeOnlyWhenTenantEnablesIt(t *testing.T) {
	resolved := verificationResolved("salon_a")
	plans := verificationPlan(resolved)
	if len(plans) != len(openairuntime.CapabilityOrder) || plans[len(plans)-1].Capability != openairuntime.CapabilityRealtime || plans[len(plans)-1].Required {
		t.Fatalf("realtime-disabled plan=%#v", plans)
	}

	resolved.Config.RealtimeEnabled = true
	resolved.Config.RealtimeModel = "realtime"
	resolved.Config.RealtimeVoice = "voice"
	plans = verificationPlan(resolved)
	for _, plan := range plans {
		if !plan.Required {
			t.Fatalf("enabled runtime left capability optional: %#v", plan)
		}
	}
}

func TestRunMatchesResolvedRequiresExactTenantAndRuntimeFences(t *testing.T) {
	resolved := verificationResolved("salon_a")
	run := &claimedRun{
		RunStatus: RunStatus{
			SalonID: "salon_a", ConfigVersion: resolved.ConfigVersion,
			CredentialRevision:          resolved.CredentialRevision,
			DestinationPolicyVersion:    openairuntime.DestinationPolicyVersion,
			VerificationContractVersion: openairuntime.VerificationContract,
		},
		IntegrationConfigID: resolved.IntegrationConfigID,
	}
	if !runMatchesResolved(run, resolved) {
		t.Fatal("matching tenant-bound verification fences were rejected")
	}

	for name, mutate := range map[string]func(*openairuntime.ResolvedConfig){
		"tenant":              func(value *openairuntime.ResolvedConfig) { value.SalonID = "salon_b" },
		"config version":      func(value *openairuntime.ResolvedConfig) { value.ConfigVersion++ },
		"credential revision": func(value *openairuntime.ResolvedConfig) { value.CredentialRevision++ },
		"destination":         func(value *openairuntime.ResolvedConfig) { value.DestinationProfile = "custom" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := resolved
			mutate(&changed)
			if runMatchesResolved(run, changed) {
				t.Fatal("stale or cross-tenant verification fence was accepted")
			}
		})
	}
}

func TestClassifyErrorReturnsOnlyBoundedOperationalCodes(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "provider_timeout"},
		{&voice.ProviderRequestError{StatusCode: 401, Err: errors.New("secret provider body")}, "credential_rejected"},
		{&voice.ProviderRequestError{StatusCode: 429}, "provider_rate_limited"},
		{&voice.ProviderRequestError{StatusCode: 503}, "provider_unavailable"},
		{&voice.ProviderRequestError{StatusCode: 400}, "provider_request_rejected"},
		{errors.New("sk-secret and response body"), "capability_probe_failed"},
	}
	for _, item := range tests {
		if got := classifyError(item.err); got != item.want {
			t.Fatalf("classifyError(%T)=%q want %q", item.err, got, item.want)
		}
	}
}

func verificationResolved(salonID string) openairuntime.ResolvedConfig {
	return openairuntime.ResolvedConfig{
		SalonID: salonID, IntegrationConfigID: "config_a", ConfigVersion: 7,
		CredentialRevision: 3, CredentialIdentityEstablished: true,
		DestinationProfile: openairuntime.DestinationProfile, Enabled: true,
		Config: config.OpenAIVoiceConfig{
			APIKey: "test-key", BaseURL: openairuntime.CanonicalBaseURL,
			TranscriptionModel: "transcribe", ReplyModel: "reply",
			SpeechModel: "speech", SpeechVoice: "voice",
		},
	}
}
