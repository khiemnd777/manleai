package conversation

import (
	"reflect"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type conversationDraftFingerprint struct {
	BookingAction       string
	TargetAppointmentID string
	CustomerName        string
	CustomerPhone       string
	CustomerEmail       string
	ServiceID           string
	StaffID             string
	StaffSelectionMode  string
	RequestedDate       string
	RequestedStartTime  string
	BookingSegments     []booking.BookingSegmentRequest
	PartyPlan           *PartyPlan
}

func draftFingerprint(session Session) conversationDraftFingerprint {
	requestedStart := ""
	if session.RequestedStartTime != nil {
		requestedStart = session.RequestedStartTime.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
	}
	return conversationDraftFingerprint{
		BookingAction:       bookingActionForSession(session),
		TargetAppointmentID: session.TargetAppointmentID,
		CustomerName:        session.CustomerName,
		CustomerPhone:       session.CustomerPhone,
		CustomerEmail:       session.CustomerEmail,
		ServiceID:           session.ServiceID,
		StaffID:             session.StaffID,
		StaffSelectionMode:  staffSelectionModeForSession(session),
		RequestedDate:       session.RequestedDate,
		RequestedStartTime:  requestedStart,
		BookingSegments:     append([]booking.BookingSegmentRequest(nil), session.BookingSegments...),
		PartyPlan:           clonePartyPlan(session.PartyPlan),
	}
}

func advanceDraftRevision(before Session, after *Session) bool {
	if after == nil {
		return false
	}
	state := normalizedDialogState(after.DialogState)
	beforeState := normalizedDialogState(before.DialogState)
	if reflect.DeepEqual(draftFingerprint(before), draftFingerprint(*after)) {
		after.DialogState = state
		return false
	}
	state.DraftRevision = beforeState.DraftRevision + 1
	if state.LastMutation != nil {
		state.LastMutationRevision = state.DraftRevision
	}
	state.ReviewAccepted = false
	state.ReviewedRevision = 0
	state.AuthorizedRevision = 0
	if state.Phase == DialogPhaseReview {
		state.Phase = DialogPhaseDrafting
	}
	after.DialogState = state
	return true
}

func reviewAuthorizationCurrent(state DialogState) bool {
	state = normalizedDialogState(state)
	return state.ReviewRequired &&
		state.ReviewAccepted &&
		state.ReviewedRevision == state.DraftRevision &&
		state.AuthorizedRevision == state.DraftRevision
}
