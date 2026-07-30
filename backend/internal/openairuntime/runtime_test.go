package openairuntime

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
)

func TestValidateRequiresExactTenantCredentialAndDestinationFences(t *testing.T) {
	resolved := ResolvedConfig{
		SalonID: "salon_a", IntegrationConfigID: "config_a", ConfigVersion: 3,
		CredentialRevision: 2, CredentialIdentityEstablished: true,
		DestinationProfile: DestinationProfile, Enabled: true,
		Config: config.OpenAIVoiceConfig{
			APIKey: "secret", BaseURL: CanonicalBaseURL, TranscriptionModel: "stt",
			ReplyModel: "reply", SpeechModel: "speech", SpeechVoice: "voice",
		},
	}
	if result := Validate(resolved); !result.Ready || len(result.Blockers) != 0 {
		t.Fatalf("valid result=%#v", result)
	}
	resolved.SalonID = ""
	resolved.Config.BaseURL = "https://127.0.0.1/v1"
	result := Validate(resolved)
	if result.Ready || !contains(result.Blockers, "tenant_required") || !contains(result.Blockers, "destination_policy_invalid") {
		t.Fatalf("invalid result=%#v", result)
	}
}

func TestCredentialFingerprintIsStablePurposeSeparatedAndTenantUniqueComparable(t *testing.T) {
	first, err := CredentialFingerprint("root-secret", "sk-tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := CredentialFingerprint("root-secret", "sk-tenant-a")
	other, _ := CredentialFingerprint("root-secret", "sk-tenant-b")
	otherRoot, _ := CredentialFingerprint("other-root", "sk-tenant-a")
	if len(first) != 64 || first != replay || first == other || first == otherRoot || first == "sk-tenant-a" {
		t.Fatalf("unexpected credential identities first=%q replay=%q other=%q otherRoot=%q", first, replay, other, otherRoot)
	}
}

func TestDestinationPolicyRejectsRedirectableOrPrivateTargetsBeforeNetwork(t *testing.T) {
	for _, value := range []string{
		"http://api.openai.com/v1", "https://api.openai.com.evil.test/v1",
		"https://api.openai.com/v1?redirect=http://169.254.169.254", "https://127.0.0.1/v1",
		"https://localhost/v1", "https://10.0.0.1/v1", "https://169.254.169.254/v1",
		"https://user:password@api.openai.com/v1",
	} {
		if err := ValidateBaseURL(value); !errors.Is(err, ErrDestinationDenied) {
			t.Fatalf("ValidateBaseURL(%q)=%v", value, err)
		}
	}
	if err := ValidateBaseURL(CanonicalBaseURL); err != nil {
		t.Fatalf("canonical URL rejected: %v", err)
	}
	if _, err := SafeDialContext(context.Background(), "tcp", "127.0.0.1:443"); !errors.Is(err, ErrDestinationDenied) {
		t.Fatalf("private direct dial error=%v", err)
	}
}

func TestValidateRequiresRealtimeWhenBufferedRealtimeOutputIsSelected(t *testing.T) {
	resolved := ResolvedConfig{
		SalonID: "salon_a", IntegrationConfigID: "config_a", ConfigVersion: 3,
		CredentialRevision: 2, CredentialIdentityEstablished: true,
		DestinationProfile: DestinationProfile, Enabled: true,
		Config: config.OpenAIVoiceConfig{
			APIKey: "secret", BaseURL: CanonicalBaseURL, TranscriptionModel: "stt",
			ReplyModel: "reply", SpeechModel: "speech", SpeechVoice: "voice",
			SpeechOutputMode: config.OpenAISpeechOutputBufferedRealtime,
		},
	}
	result := Validate(resolved)
	if result.Ready || !contains(result.Blockers, "speech_output_dependency_missing") {
		t.Fatalf("buffered realtime dependency result=%#v", result)
	}
	resolved.Config.RealtimeEnabled = true
	resolved.Config.RealtimeModel = "realtime"
	resolved.Config.RealtimeVoice = "voice"
	if result := Validate(resolved); !result.Ready {
		t.Fatalf("configured buffered realtime result=%#v", result)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
