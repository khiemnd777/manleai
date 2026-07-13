package conversation

import (
	"context"
	"time"
)

const (
	TurnTimingStageSessionLoad      = "session_load"
	TurnTimingStageAnswerContext    = "answer_context"
	TurnTimingStageTurnRouter       = "turn_router"
	TurnTimingStageTurnInterpreter  = "turn_interpreter"
	TurnTimingStageAvailabilityPOS  = "availability_pos"
	TurnTimingStageSaveTurn         = "save_turn"
	TurnTimingResultOK              = "ok"
	TurnTimingResultError           = "error"
	TurnTimingResultDeduplicated    = "deduplicated"
	TurnTimingResultSessionClosed   = "session_closed"
	TurnTimingPathStructuredAI      = "structured_ai"
	TurnTimingPathProviderFallback  = "provider_fallback"
	TurnTimingPathDeterministic     = "deterministic_owned"
	TurnTimingPathInterpreterAbsent = "interpreter_absent"
	TurnTimingPathStateScoped       = "state_scoped_fast_path"
	TurnTimingPathFastLane          = "fast_lane"
	TurnTimingPathAnswerLane        = "answer_lane"
	TurnTimingPathActionLane        = "action_lane"
	TurnTimingPathRecoveryLane      = "recovery_lane"
)

type turnTimingContextKey struct{}

func withTurnTimingRecorder(ctx context.Context, recorder TurnTimingRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, turnTimingContextKey{}, recorder)
}

func recordTurnTiming(ctx context.Context, stage string, startedAt time.Time, result string) {
	recorder, _ := ctx.Value(turnTimingContextKey{}).(TurnTimingRecorder)
	if recorder == nil {
		return
	}
	duration := time.Since(startedAt)
	if duration < 0 {
		duration = 0
	}
	recorder(TurnTiming{Stage: stage, Duration: duration, Result: result})
}

func recordSkippedTurnTiming(ctx context.Context, stage string, result string) {
	recorder, _ := ctx.Value(turnTimingContextKey{}).(TurnTimingRecorder)
	if recorder == nil {
		return
	}
	recorder(TurnTiming{Stage: stage, Result: result})
}

func recordSkippedTurnTimingWithAttributes(ctx context.Context, stage string, result string, attributes map[string]string) {
	recorder, _ := ctx.Value(turnTimingContextKey{}).(TurnTimingRecorder)
	if recorder == nil {
		return
	}
	cloned := make(map[string]string, len(attributes))
	for key, value := range attributes {
		cloned[key] = value
	}
	recorder(TurnTiming{Stage: stage, Result: result, Attributes: cloned})
}

func recordTurnTimingWithAttributes(ctx context.Context, stage string, startedAt time.Time, result string, attributes map[string]string) {
	recorder, _ := ctx.Value(turnTimingContextKey{}).(TurnTimingRecorder)
	if recorder == nil {
		return
	}
	duration := time.Since(startedAt)
	if duration < 0 {
		duration = 0
	}
	cloned := make(map[string]string, len(attributes))
	for key, value := range attributes {
		cloned[key] = value
	}
	recorder(TurnTiming{Stage: stage, Duration: duration, Result: result, Attributes: cloned})
}

func turnTimingResult(err error) string {
	if err != nil {
		return TurnTimingResultError
	}
	return TurnTimingResultOK
}
