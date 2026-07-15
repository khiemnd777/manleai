package voice

import (
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

func TestBackendTurnDiagnosticsWhitelistsSafeInterpreterFailureFields(t *testing.T) {
	diagnostics := newBackendTurnDiagnostics()
	diagnostics.Record(conversation.TurnTiming{
		Stage: conversation.TurnTimingStageTurnInterpreter, Result: conversation.TurnTimingPathProviderFallback,
		Attributes: map[string]string{
			"turn_interpreter_outcome":           conversation.TurnInterpreterOutcomeProviderError,
			"turn_interpreter_provider":          ProviderOpenAI,
			"turn_interpreter_failure_stage":     "turn_interpretation_response",
			"turn_interpreter_http_status":       "503",
			"turn_interpreter_http_status_class": "5xx",
			"turn_interpreter_request_id":        "req_safe_123",
			"customer_message":                   "must not escape",
		},
	})

	snapshot := diagnostics.Snapshot()
	if snapshot["turn_interpreter_http_status_class"] != "5xx" || snapshot["turn_interpreter_request_id"] != "req_safe_123" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, exists := snapshot["customer_message"]; exists {
		t.Fatalf("snapshot leaked non-whitelisted field: %#v", snapshot)
	}
}
