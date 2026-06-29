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
