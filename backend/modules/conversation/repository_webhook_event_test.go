package conversation

import (
	"encoding/json"
	"testing"
)

func TestApplyWebhookPayloadExposesOnlySafeRealtimeDiagnostics(t *testing.T) {
	payload, err := json.Marshal(map[string]string{
		"stage":             "transcript_done",
		"decision":          "accepted",
		"reason":            "confidence_and_vad_admitted",
		"profile":           "noisy_salon",
		"mean_logprob":      "-0.2000",
		"vad_duration_ms":   "700",
		"transcript":        "private caller speech",
		"audio":             "private audio payload",
		"unapproved_detail": "internal",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	item := WebhookEventLog{}
	applyWebhookPayload(&item, payload)
	if item.Stage != "transcript_done" || item.Diagnostics["decision"] != "accepted" || item.Diagnostics["profile"] != "noisy_salon" {
		t.Fatalf("safe diagnostics = %#v", item)
	}
	for _, key := range []string{"transcript", "audio", "unapproved_detail"} {
		if _, ok := item.Diagnostics[key]; ok {
			t.Fatalf("private or unapproved field %q leaked into diagnostics: %#v", key, item.Diagnostics)
		}
	}
}
