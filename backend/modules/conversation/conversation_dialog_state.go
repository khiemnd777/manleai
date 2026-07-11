package conversation

import "github.com/manleai/ai-receptionist/modules/booking"

func normalizedDialogState(state DialogState) DialogState {
	if state.Version <= 0 {
		state.Version = DialogStateVersion
	}
	if state.Phase == "" {
		state.Phase = DialogPhaseOpen
	}
	if state.Pending != nil {
		pending := *state.Pending
		pending.SourceServiceIDs = append([]string(nil), state.Pending.SourceServiceIDs...)
		pending.TargetServiceIDs = append([]string(nil), state.Pending.TargetServiceIDs...)
		state.Pending = &pending
	}
	if state.LastMutation != nil {
		mutation := *state.LastMutation
		mutation.BeforeServiceIDs = append([]string(nil), state.LastMutation.BeforeServiceIDs...)
		mutation.BeforeSegments = append([]booking.BookingSegmentRequest(nil), state.LastMutation.BeforeSegments...)
		mutation.AfterServiceIDs = append([]string(nil), state.LastMutation.AfterServiceIDs...)
		mutation.AfterSegments = append([]booking.BookingSegmentRequest(nil), state.LastMutation.AfterSegments...)
		state.LastMutation = &mutation
	}
	return state
}

func cloneDialogState(state DialogState) DialogState {
	return normalizedDialogState(state)
}

func resetDialogProgress(state DialogState, phase string) DialogState {
	state = normalizedDialogState(state)
	state.Phase = phase
	state.Pending = nil
	state.NoProgressCount = 0
	state.LastPromptKey = ""
	state.ReviewAccepted = false
	return state
}
