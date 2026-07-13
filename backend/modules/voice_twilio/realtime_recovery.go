package voice_twilio

import "strings"

const realtimeInputHandoffThreshold = 4

const (
	realtimeRecoveryRepeatShort  = "repeat_short"
	realtimeRecoveryRepeatScoped = "repeat_scoped"
	realtimeRecoveryNoiseCoach   = "noise_coaching"
	realtimeRecoveryOwnerHandoff = "owner_handoff"
)

type realtimeRecoveryDecision struct {
	Streak  int
	Action  string
	Message string
	Handoff bool
}

type realtimeInputRecovery struct {
	consecutiveRejections int
}

func (r *realtimeInputRecovery) Reject(lastApprovedPrompt string) realtimeRecoveryDecision {
	r.consecutiveRejections++
	prompt := strings.TrimSpace(lastApprovedPrompt)
	switch r.consecutiveRejections {
	case 1:
		return realtimeRecoveryDecision{
			Streak:  1,
			Action:  realtimeRecoveryRepeatShort,
			Message: "Sorry, could you say that again?",
		}
	case 2:
		return realtimeRecoveryDecision{
			Streak:  2,
			Action:  realtimeRecoveryRepeatScoped,
			Message: recoveryMessage("There's some background noise. Please give one short answer.", prompt),
		}
	case 3:
		return realtimeRecoveryDecision{
			Streak:  3,
			Action:  realtimeRecoveryNoiseCoach,
			Message: recoveryMessage("I don't want to get your details wrong. Please move closer to the phone or somewhere quieter, then answer once more.", prompt),
		}
	default:
		return realtimeRecoveryDecision{
			Streak:  r.consecutiveRejections,
			Action:  realtimeRecoveryOwnerHandoff,
			Handoff: true,
		}
	}
}

func (r *realtimeInputRecovery) Reset() {
	r.consecutiveRejections = 0
}

func recoveryMessage(instruction string, prompt string) string {
	instruction = strings.TrimSpace(instruction)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return instruction
	}
	return instruction + " " + prompt
}
