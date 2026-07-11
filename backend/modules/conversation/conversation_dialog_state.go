package conversation

import "github.com/manleai/ai-receptionist/modules/booking"

func normalizedDialogState(state DialogState) DialogState {
	state.Version = DialogStateVersion
	if state.Phase == "" {
		state.Phase = DialogPhaseOpen
	}
	if state.DraftRevision <= 0 {
		state.DraftRevision = 1
	}
	if state.ReviewedRevision != state.DraftRevision || state.AuthorizedRevision != state.DraftRevision {
		state.ReviewAccepted = false
	}
	if state.Pending != nil {
		pending := *state.Pending
		pending.SourceServiceIDs = append([]string(nil), state.Pending.SourceServiceIDs...)
		pending.TargetServiceIDs = append([]string(nil), state.Pending.TargetServiceIDs...)
		state.Pending = &pending
	}
	if state.LastMutation != nil {
		mutation := cloneDraftMutation(*state.LastMutation)
		state.LastMutation = &mutation
	}
	if state.MutationHistory != nil {
		history := make([]DraftMutation, len(state.MutationHistory))
		for index := range state.MutationHistory {
			history[index] = cloneDraftMutation(state.MutationHistory[index])
		}
		state.MutationHistory = history
	}
	return state
}

func cloneDraftMutation(source DraftMutation) DraftMutation {
	cloned := source
	cloned.BeforeServiceIDs = append([]string(nil), source.BeforeServiceIDs...)
	cloned.BeforeSegments = append([]booking.BookingSegmentRequest(nil), source.BeforeSegments...)
	cloned.AfterServiceIDs = append([]string(nil), source.AfterServiceIDs...)
	cloned.AfterSegments = append([]booking.BookingSegmentRequest(nil), source.AfterSegments...)
	return cloned
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
	state.ReviewedRevision = 0
	state.AuthorizedRevision = 0
	return state
}
