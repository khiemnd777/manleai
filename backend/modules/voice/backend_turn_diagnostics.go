package voice

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

const backendTimingStageRouteConfig = "route_config"

type backendTurnDiagnostics struct {
	mu        sync.Mutex
	durations map[string]time.Duration
	present   map[string]bool
	paths     map[string]string
}

func newBackendTurnDiagnostics() *backendTurnDiagnostics {
	return &backendTurnDiagnostics{
		durations: map[string]time.Duration{},
		present:   map[string]bool{},
		paths:     map[string]string{},
	}
}

func (d *backendTurnDiagnostics) Record(timing conversation.TurnTiming) {
	if d == nil {
		return
	}
	d.add(timing.Stage, timing.Duration, timing.Result)
	if len(timing.Attributes) > 0 {
		d.mu.Lock()
		for key, value := range timing.Attributes {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key != "" && value != "" {
				d.paths[key] = value
			}
		}
		d.mu.Unlock()
	}
}

func (d *backendTurnDiagnostics) add(stage string, duration time.Duration, result string) {
	stage = strings.TrimSpace(stage)
	if d == nil || stage == "" {
		return
	}
	if duration < 0 {
		duration = 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.durations[stage] += duration
	d.present[stage] = true
	if stage == conversation.TurnTimingStageTurnInterpreter && strings.TrimSpace(result) != "" {
		d.paths[stage] = strings.TrimSpace(result)
	}
}

func (d *backendTurnDiagnostics) Snapshot() map[string]string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	diagnostics := map[string]string{}
	for _, stage := range []string{
		backendTimingStageRouteConfig,
		conversation.TurnTimingStageSessionLoad,
		conversation.TurnTimingStageAnswerContext,
		conversation.TurnTimingStageTurnRouter,
		conversation.TurnTimingStageTurnInterpreter,
		conversation.TurnTimingStageAvailabilityPOS,
		conversation.TurnTimingStageSaveTurn,
	} {
		if !d.present[stage] {
			continue
		}
		diagnostics[stage+"_ms"] = strconv.FormatInt(d.durations[stage].Milliseconds(), 10)
	}
	if path := d.paths[conversation.TurnTimingStageTurnInterpreter]; path != "" {
		diagnostics[conversation.TurnTimingStageTurnInterpreter+"_path"] = path
	}
	for _, key := range []string{
		"turn_route", "turn_expected_input", "turn_route_reason", "turn_deterministic_coverage",
		"turn_interpreter_outcome", "turn_model_service_count", "turn_model_staff_count",
		"turn_interpreter_provider", "turn_interpreter_failure_stage", "turn_interpreter_http_status",
		"turn_interpreter_http_status_class", "turn_interpreter_request_id", "turn_interpreter_error_type",
		"turn_interpreter_error_code", "turn_interpreter_error_param", "turn_interpreter_schema_fingerprint", "turn_interpreter_circuit_open",
	} {
		if value := d.paths[key]; value != "" {
			diagnostics[key] = value
		}
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return diagnostics
}
