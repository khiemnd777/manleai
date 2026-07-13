package conversation

import (
	"encoding/json"
	"testing"
)

func TestApplyWebhookPayloadExposesOnlySafeRealtimeDiagnostics(t *testing.T) {
	payload, err := json.Marshal(map[string]string{
		"stage":               "transcript_done",
		"decision":            "accepted",
		"reason":              "confidence_and_vad_admitted",
		"profile":             "noisy_salon",
		"mean_logprob":        "-0.2000",
		"vad_duration_ms":     "700",
		"provider_request_id": "speech_req_1",
		"audio_chunk_count":   "7",
		"audio_bytes":         "1120",
		"rejection_streak":    "3",
		"recovery_action":     "noise_coaching",
		"transcript":          "private caller speech",
		"audio":               "private audio payload",
		"unapproved_detail":   "internal",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	item := WebhookEventLog{}
	applyWebhookPayload(&item, payload)
	if item.Stage != "transcript_done" || item.Diagnostics["decision"] != "accepted" || item.Diagnostics["profile"] != "noisy_salon" {
		t.Fatalf("safe diagnostics = %#v", item)
	}
	if item.Diagnostics["provider_request_id"] != "speech_req_1" || item.Diagnostics["audio_chunk_count"] != "7" || item.Diagnostics["audio_bytes"] != "1120" {
		t.Fatalf("streaming diagnostics = %#v", item.Diagnostics)
	}
	if item.Diagnostics["rejection_streak"] != "3" || item.Diagnostics["recovery_action"] != "noise_coaching" {
		t.Fatalf("recovery diagnostics = %#v", item.Diagnostics)
	}
	for _, key := range []string{"transcript", "audio", "unapproved_detail"} {
		if _, ok := item.Diagnostics[key]; ok {
			t.Fatalf("private or unapproved field %q leaked into diagnostics: %#v", key, item.Diagnostics)
		}
	}
}
