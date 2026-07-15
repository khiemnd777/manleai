package config

import "testing"

func TestNormalizeOpenAIRealtimeModelDefaultsAndMigratesLegacyPreview(t *testing.T) {
	if got := NormalizeOpenAIRealtimeModel(""); got != DefaultOpenAIRealtimeModel {
		t.Fatalf("blank model = %q, want %q", got, DefaultOpenAIRealtimeModel)
	}
	if got := NormalizeOpenAIRealtimeModel(LegacyOpenAIRealtimePreviewModel); got != DefaultOpenAIRealtimeModel {
		t.Fatalf("legacy preview model = %q, want %q", got, DefaultOpenAIRealtimeModel)
	}
	if got := NormalizeOpenAIRealtimeModel("gpt-realtime-1.5"); got != "gpt-realtime-1.5" {
		t.Fatalf("explicit model = %q", got)
	}
}

func TestNormalizeOpenAIRealtimeNoiseProfileUsesLocationNeutralPolicies(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "blank defaults to automatic", want: OpenAIRealtimeNoiseAutomatic},
		{name: "automatic remains canonical", value: "automatic", want: OpenAIRealtimeNoiseAutomatic},
		{name: "standard remains canonical", value: "standard", want: OpenAIRealtimeNoiseStandard},
		{name: "strong remains canonical", value: "strong_noise_rejection", want: OpenAIRealtimeNoiseStrongRejection},
		{name: "minimal remains canonical", value: "minimal_processing", want: OpenAIRealtimeNoiseMinimal},
		{name: "legacy noisy salon preserves strong behavior", value: "noisy_salon", want: OpenAIRealtimeNoiseStrongRejection},
		{name: "legacy balanced preserves standard behavior", value: "balanced", want: OpenAIRealtimeNoiseStandard},
		{name: "legacy quiet room preserves minimal behavior", value: "quiet_room", want: OpenAIRealtimeNoiseMinimal},
		{name: "unknown fails safe to automatic", value: "somewhere_loud", want: OpenAIRealtimeNoiseAutomatic},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeOpenAIRealtimeNoiseProfile(test.value); got != test.want {
				t.Fatalf("NormalizeOpenAIRealtimeNoiseProfile(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
