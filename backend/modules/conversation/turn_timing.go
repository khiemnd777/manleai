package conversation

import (
	"context"
	"time"
)

const (
	TurnTimingStageSessionLoad      = "session_load"
	TurnTimingStageAnswerContext    = "answer_context"
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

func turnTimingResult(err error) string {
	if err != nil {
		return TurnTimingResultError
	}
	return TurnTimingResultOK
}
