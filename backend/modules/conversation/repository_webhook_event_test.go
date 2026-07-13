package conversation

import (
	"encoding/json"
	"testing"
)

func TestApplyWebhookPayloadExposesOnlySafeRealtimeDiagnostics(t *testing.T) {
	payload, err := json.Marshal(map[string]string{
		"stage":                 "transcript_done",
		"decision":              "accepted",
		"reason":                "confidence_and_vad_admitted",
		"profile":               "noisy_salon",
		"mean_logprob":          "-0.2000",
		"vad_duration_ms":       "700",
		"provider_request_id":   "speech_req_1",
		"audio_chunk_count":     "7",
		"audio_bytes":           "1120",
		"input_sample_rate":     "24000",
		"producer_duration_ms":  "350",
		"producer_active_ms":    "330",
		"producer_rate_x1000":   "4000",
		"provider_gap_max_ms":   "14",
		"backpressure_total_ms": "420",
		"backpressure_events":   "21",
		"playout_duration_ms":   "1400",
		"queue_max_frames":      "22",
		"underrun_count":        "0",
		"write_max_ms":          "3",
		"rejection_streak":      "3",
		"recovery_action":       "noise_coaching",
		"route_config_ms":       "12",
		"session_load_ms":       "8",
		"answer_context_ms":     "4",
		"turn_interpreter_ms":   "310",
		"turn_interpreter_path": "structured_ai",
		"availability_pos_ms":   "95",
		"save_turn_ms":          "7",
		"transcript":            "private caller speech",
		"audio":                 "private audio payload",
		"unapproved_detail":     "internal",
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
	for key, want := range map[string]string{
		"input_sample_rate":     "24000",
		"producer_duration_ms":  "350",
		"producer_active_ms":    "330",
		"producer_rate_x1000":   "4000",
		"provider_gap_max_ms":   "14",
		"backpressure_total_ms": "420",
		"backpressure_events":   "21",
		"playout_duration_ms":   "1400",
		"queue_max_frames":      "22",
		"underrun_count":        "0",
		"write_max_ms":          "3",
	} {
		if got := item.Diagnostics[key]; got != want {
			t.Fatalf("playout diagnostic %s = %q, want %q; all=%#v", key, got, want, item.Diagnostics)
		}
	}
	if item.Diagnostics["rejection_streak"] != "3" || item.Diagnostics["recovery_action"] != "noise_coaching" {
		t.Fatalf("recovery diagnostics = %#v", item.Diagnostics)
	}
	for key, want := range map[string]string{
		"route_config_ms":       "12",
		"session_load_ms":       "8",
		"answer_context_ms":     "4",
		"turn_interpreter_ms":   "310",
		"turn_interpreter_path": "structured_ai",
		"availability_pos_ms":   "95",
		"save_turn_ms":          "7",
	} {
		if got := item.Diagnostics[key]; got != want {
			t.Fatalf("backend diagnostic %s = %q, want %q; all=%#v", key, got, want, item.Diagnostics)
		}
	}
	for _, key := range []string{"transcript", "audio", "unapproved_detail"} {
		if _, ok := item.Diagnostics[key]; ok {
			t.Fatalf("private or unapproved field %q leaked into diagnostics: %#v", key, item.Diagnostics)
		}
	}
}
