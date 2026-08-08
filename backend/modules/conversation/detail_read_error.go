package conversation

import "errors"

type conversationDetailStage string

const (
	conversationDetailStageTranscript         conversationDetailStage = "transcript"
	conversationDetailStageHandoff            conversationDetailStage = "handoff"
	conversationDetailStagePartyRequest       conversationDetailStage = "party_request"
	conversationDetailStageSchedulingEvidence conversationDetailStage = "scheduling_evidence"
)

type conversationDetailReadError struct {
	stage conversationDetailStage
	err   error
}

func newConversationDetailReadError(stage conversationDetailStage, err error) error {
	if err == nil {
		return nil
	}
	return &conversationDetailReadError{stage: stage, err: err}
}

func (e *conversationDetailReadError) Error() string {
	return "conversation detail read failed at " + string(e.stage)
}

func (e *conversationDetailReadError) Unwrap() error { return e.err }

func conversationDetailReadStage(err error) (conversationDetailStage, bool) {
	var detailErr *conversationDetailReadError
	if !errors.As(err, &detailErr) || detailErr == nil {
		return "", false
	}
	return detailErr.stage, true
}
