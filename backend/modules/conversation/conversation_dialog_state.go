package conversation

import "github.com/manleai/ai-receptionist/modules/booking"

func normalizedDialogState(state DialogState) DialogState {
	// Dialog state is persisted as JSONB. Promote the legacy guidance prompt
	// fields into their typed owner before stamping the current schema version.
	// Other users of the top-level prompt/counter fields remain unchanged.
	if state.Guidance == nil && isLegacyGuidanceRecoveryPrompt(state.LastPromptKey) {
		state.Guidance = &GuidanceRecoveryState{
			Stage:                legacyGuidanceRecoveryStage(state.LastPromptKey),
			NoProgressCount:      state.NoProgressCount,
			ProviderFailureCount: state.ProviderFailureCount,
			ProgressFingerprint:  state.ProgressFingerprint,
		}
		state.LastPromptKey = ""
		state.NoProgressCount = 0
		state.ProviderFailureCount = 0
		state.ProgressFingerprint = ""
	}
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
	if state.TimePreference != nil {
		preference := *state.TimePreference
		state.TimePreference = &preference
	}
	if state.Consultation != nil {
		consultation := *state.Consultation
		consultation.Needs.Priorities = append([]string(nil), state.Consultation.Needs.Priorities...)
		consultation.Needs.DesiredFinishes = append([]string(nil), state.Consultation.Needs.DesiredFinishes...)
		consultation.Needs.ComparedServiceIDs = append([]string(nil), state.Consultation.Needs.ComparedServiceIDs...)
		consultation.CandidateServiceIDs = append([]string(nil), state.Consultation.CandidateServiceIDs...)
		consultation.RecommendedServiceIDs = append([]string(nil), state.Consultation.RecommendedServiceIDs...)
		consultation.LastQuestionOptions = append([]string(nil), state.Consultation.LastQuestionOptions...)
		consultation.ProfileRevisions = cloneStringIntMap(state.Consultation.ProfileRevisions)
		consultation.RecommendationReasons = cloneStringSliceMap(state.Consultation.RecommendationReasons)
		consultation.LastInterpreterDiagnostics = cloneStringMap(state.Consultation.LastInterpreterDiagnostics)
		state.Consultation = &consultation
	}
	if state.Guidance != nil {
		guidance := *state.Guidance
		guidance.OfferedActions = append([]string(nil), state.Guidance.OfferedActions...)
		state.Guidance = &guidance
	}
	return state
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneStringIntMap(source map[string]int) map[string]int {
	if source == nil {
		return nil
	}
	cloned := make(map[string]int, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneStringSliceMap(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string][]string, len(source))
	for key, value := range source {
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
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
	state.ProviderFailureCount = 0
	state.ProgressFingerprint = ""
	state.LastPromptKey = ""
	state.Guidance = nil
	state.ReviewAccepted = false
	state.ReviewedRevision = 0
	state.AuthorizedRevision = 0
	return state
}
