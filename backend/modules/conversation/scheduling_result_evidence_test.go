package conversation

import (
	"database/sql"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestValidateInternalEvidenceDistinguishesCurrentFromHistoricalResult(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeBookingConfirmed)
	snapshot := validInternalEvidenceSnapshot()

	evidence, ok := validateInternalEvidence(session, snapshot)
	if !ok || !evidence.Complete || !evidence.IsCurrent || evidence.CurrentStatus != booking.StatusConfirmed ||
		evidence.ResultChildCount != 2 || evidence.CurrentActiveChildCount != 2 {
		t.Fatalf("current internal evidence = %#v, ok=%v", evidence, ok)
	}

	snapshot.AppointmentStatus = booking.StatusCancelled
	snapshot.AppointmentVersion = 2
	snapshot.AppointmentSchedulingVersion = 9
	snapshot.AppointmentConfigVersion = 11
	snapshot.CurrentActiveChildCount = 0
	evidence, ok = validateInternalEvidence(session, snapshot)
	if !ok || !evidence.Complete || evidence.IsCurrent || evidence.ResultStatus != booking.StatusConfirmed ||
		evidence.CurrentStatus != booking.StatusCancelled || evidence.CurrentAuthorityAppointmentVersion != 2 {
		t.Fatalf("historical internal evidence = %#v, ok=%v", evidence, ok)
	}
}

func TestValidateOwnerRequestEvidencePreservesTargetAuthorityWithoutConfirming(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeOwnerReviewPending)
	session.SchedulingRequestID = "request-1"
	session.BookingAttemptID = ""
	session.AppointmentID = ""

	for _, status := range []string{"pending", "resolved", "dismissed"} {
		evidence, ok := validateOwnerRequestEvidence(
			session,
			"request-1",
			booking.BookingActionBook,
			status,
			booking.SchedulingAuthorityOwnerManual,
			sql.NullString{String: booking.SchedulingAuthorityExternalProvider, Valid: true},
		)
		if !ok || !evidence.Complete || evidence.Kind != SchedulingEvidenceKindPendingOwnerReview ||
			evidence.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual ||
			evidence.TargetSchedulingAuthority != booking.SchedulingAuthorityExternalProvider ||
			evidence.CurrentStatus != status || evidence.AppointmentID != "" || evidence.BookingAttemptID != "" {
			t.Fatalf("owner evidence for %s = %#v, ok=%v", status, evidence, ok)
		}
	}

	if evidence, ok := validateOwnerRequestEvidence(
		session,
		"request-1",
		booking.BookingActionBook,
		"pending",
		booking.SchedulingAuthorityOwnerManual,
		sql.NullString{String: "unknown_authority", Valid: true},
	); ok || evidence != nil {
		t.Fatalf("malformed target authority evidence = %#v, ok=%v", evidence, ok)
	}
}

func TestValidateInternalEvidenceRejectsProviderShapedAndPartialGraphs(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeBookingConfirmed)
	tests := []struct {
		name   string
		mutate func(*internalEvidenceSnapshot)
	}{
		{name: "provider evidence", mutate: func(value *internalEvidenceSnapshot) { value.HasProviderEvidence = true }},
		{name: "partial attempt graph", mutate: func(value *internalEvidenceSnapshot) { value.AttemptChildCount = 1 }},
		{name: "invalid active child", mutate: func(value *internalEvidenceSnapshot) { value.InvalidCurrentActiveChildCount = 1 }},
		{name: "wrong outcome", mutate: func(value *internalEvidenceSnapshot) { value.AttemptStatus = booking.StatusRescheduled }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := validInternalEvidenceSnapshot()
			test.mutate(&snapshot)
			if evidence, ok := validateInternalEvidence(session, snapshot); ok || evidence != nil {
				t.Fatalf("evidence = %#v, ok=%v", evidence, ok)
			}
		})
	}
}

func TestValidateInternalLifecycleEvidenceDistinguishesCurrentCancelAndHistoricalReschedule(t *testing.T) {
	rescheduleSession := completedEvidenceSession(booking.BookingActionReschedule, OutcomeBookingRescheduled)
	reschedule := validInternalEvidenceSnapshot()
	reschedule.AttemptStatus = booking.StatusRescheduled
	reschedule.OperationType = booking.BookingActionReschedule
	reschedule.TargetVersion = sql.NullInt64{Int64: 1, Valid: true}
	reschedule.AttemptVersion = 2
	reschedule.EventType = "appointment_rescheduled"
	reschedule.EventVersion = 2
	reschedule.AppointmentStatus = booking.StatusCancelled
	reschedule.AppointmentVersion = 3
	reschedule.AppointmentSchedulingVersion = 9
	reschedule.AppointmentConfigVersion = 11
	reschedule.CurrentActiveChildCount = 0
	evidence, ok := validateInternalEvidence(rescheduleSession, reschedule)
	if !ok || !evidence.Complete || evidence.IsCurrent || evidence.ResultStatus != booking.StatusRescheduled ||
		evidence.CurrentStatus != booking.StatusCancelled {
		t.Fatalf("historical reschedule evidence = %#v, ok=%v", evidence, ok)
	}

	cancelSession := completedEvidenceSession(booking.BookingActionCancel, OutcomeBookingCancelled)
	cancel := validInternalEvidenceSnapshot()
	cancel.AttemptStatus = booking.StatusCancelled
	cancel.OperationType = booking.BookingActionCancel
	cancel.TargetVersion = sql.NullInt64{Int64: 2, Valid: true}
	cancel.AttemptVersion = 3
	cancel.EventType = "appointment_cancelled"
	cancel.EventVersion = 3
	cancel.AppointmentStatus = booking.StatusCancelled
	cancel.AppointmentVersion = 3
	cancel.CurrentActiveChildCount = 0
	evidence, ok = validateInternalEvidence(cancelSession, cancel)
	if !ok || !evidence.Complete || !evidence.IsCurrent || evidence.ResultStatus != booking.StatusCancelled ||
		evidence.CurrentStatus != booking.StatusCancelled || evidence.CurrentActiveChildCount != 0 {
		t.Fatalf("current cancel evidence = %#v, ok=%v", evidence, ok)
	}
}

func TestValidateExternalEvidenceRequiresProviderSuccessAndMatchingMirror(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeBookingConfirmed)
	snapshot := validExternalEvidenceSnapshot()

	evidence, ok := validateExternalEvidence(session, snapshot, snapshot.OperationKey, false)
	if !ok || !evidence.Complete || !evidence.IsCurrent || evidence.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("external evidence = %#v, ok=%v", evidence, ok)
	}

	tests := []struct {
		name   string
		mutate func(*externalEvidenceSnapshot)
	}{
		{name: "local ids only", mutate: func(value *externalEvidenceSnapshot) { value.AttemptAuthorityID = "" }},
		{name: "provider did not succeed", mutate: func(value *externalEvidenceSnapshot) { value.ProviderOutcome = booking.ProviderOutcomeUnknown }},
		{name: "appointment mirror mismatch", mutate: func(value *externalEvidenceSnapshot) { value.AppointmentAuthorityID = "another-booking" }},
		{name: "incomplete child graph", mutate: func(value *externalEvidenceSnapshot) { value.InvalidAppointmentSegmentCount = 1 }},
		{name: "partial child graph", mutate: func(value *externalEvidenceSnapshot) { value.AppointmentSegmentCount = 0 }},
		{name: "mismatched ordered child graph", mutate: func(value *externalEvidenceSnapshot) { value.SegmentsMatch = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := validExternalEvidenceSnapshot()
			test.mutate(&candidate)
			if evidence, ok := validateExternalEvidence(session, candidate, candidate.OperationKey, false); ok || evidence != nil {
				t.Fatalf("evidence = %#v, ok=%v", evidence, ok)
			}
		})
	}
}

func TestValidateExternalEvidenceKeepsHistoricalSuccessWithoutCurrentConfirmation(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeBookingConfirmed)
	snapshot := validExternalEvidenceSnapshot()
	snapshot.AppointmentAuthorityVersion = 3
	snapshot.AppointmentPOSVersion = 3
	snapshot.AppointmentStatus = booking.StatusCancelled

	evidence, ok := validateExternalEvidence(session, snapshot, snapshot.OperationKey, false)
	if !ok || !evidence.Complete || evidence.IsCurrent || evidence.ResultStatus != booking.StatusConfirmed ||
		evidence.CurrentStatus != booking.StatusCancelled || evidence.CurrentActiveChildCount != 0 {
		t.Fatalf("historical external evidence = %#v, ok=%v", evidence, ok)
	}
}

func TestValidateExternalEvidenceAllowsDifferentValidCurrentGraphOnlyForHistoricalResult(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeBookingConfirmed)
	snapshot := validExternalEvidenceSnapshot()
	snapshot.AppointmentAuthorityVersion = 3
	snapshot.AppointmentPOSVersion = 3
	snapshot.AppointmentStatus = booking.StatusRescheduled
	snapshot.AppointmentSegmentCount = 2
	snapshot.SegmentsMatch = false

	evidence, ok := validateExternalEvidence(session, snapshot, snapshot.OperationKey, false)
	if !ok || !evidence.Complete || evidence.IsCurrent || evidence.ResultStatus != booking.StatusConfirmed ||
		evidence.CurrentStatus != booking.StatusRescheduled || evidence.CurrentActiveChildCount != 2 {
		t.Fatalf("historical external evidence with later graph = %#v, ok=%v", evidence, ok)
	}

	snapshot.AppointmentAuthorityVersion = snapshot.AttemptAuthorityVersion
	snapshot.AppointmentPOSVersion = snapshot.AttemptPOSVersion
	snapshot.AppointmentStatus = snapshot.AttemptStatus
	if evidence, ok := validateExternalEvidence(session, snapshot, snapshot.OperationKey, false); ok || evidence != nil {
		t.Fatalf("current mismatched external graph = %#v, ok=%v", evidence, ok)
	}
}

func TestSplitEvidenceInputsRejectsDuplicateOrIncompletePartyProof(t *testing.T) {
	session := completedEvidenceSession(booking.BookingActionBook, OutcomeBookingConfirmed)
	session.BookingAttemptID = "attempt-1"
	session.AppointmentID = "appointment-1"
	session.BookingSegments = []booking.BookingSegmentRequest{{ServiceID: "service-1"}, {ServiceID: "service-2"}}
	session.PartyPlan = &PartyPlan{
		PartySize:             2,
		SelectedSplitOptionID: "split-1",
		SplitOptions: []PartySplitOption{{ID: "split-1", Blocks: []PartySplitBlock{
			{Segments: []booking.BookingSegmentRequest{{ServiceID: "service-1"}}},
			{Segments: []booking.BookingSegmentRequest{{ServiceID: "service-2"}}},
		}}},
		SplitBookingAttemptIDs: []string{"attempt-1", "attempt-2"},
		SplitAppointmentIDs:    []string{"appointment-1", "appointment-2"},
	}
	inputs, claimed := splitEvidenceInputs(session)
	if !claimed || len(inputs) != 2 || inputs[1].OperationKey != "conversation:session-1:split:1:0" {
		t.Fatalf("inputs = %#v claimed=%v", inputs, claimed)
	}

	session.PartyPlan.SplitAppointmentIDs[1] = "appointment-1"
	inputs, claimed = splitEvidenceInputs(session)
	if !claimed || len(inputs) != 0 {
		t.Fatalf("duplicate proof inputs = %#v claimed=%v", inputs, claimed)
	}

	session.PartyPlan.SplitAppointmentIDs = []string{"appointment-1"}
	inputs, claimed = splitEvidenceInputs(session)
	if !claimed || len(inputs) != 0 {
		t.Fatalf("missing split child inputs = %#v claimed=%v", inputs, claimed)
	}
}

func completedEvidenceSession(operation, outcome string) Session {
	return Session{
		ID: "session-1", SalonID: "salon-1", BookingAction: operation, Outcome: outcome,
		BookingAttemptID: "attempt-1", AppointmentID: "appointment-1",
	}
}

func validInternalEvidenceSnapshot() internalEvidenceSnapshot {
	return internalEvidenceSnapshot{
		SessionID: "session-1", AttemptID: "attempt-1", AttemptStatus: booking.StatusConfirmed,
		OperationType: booking.BookingActionBook, AttemptVersion: 1, AttemptSchedulingVersion: 4,
		AttemptConfigVersion: 7, EventType: "appointment_confirmed", EventVersion: 1,
		EventSchedulingVersion: 4, EventConfigVersion: 7, AppointmentID: "appointment-1",
		AppointmentStatus: booking.StatusConfirmed, AppointmentVersion: 1,
		AppointmentSchedulingVersion: 4, AppointmentConfigVersion: 7, PartySize: 2,
		AttemptChildCount: 2, ResultChildCount: 2, CurrentActiveChildCount: 2,
	}
}

func validExternalEvidenceSnapshot() externalEvidenceSnapshot {
	return externalEvidenceSnapshot{
		SessionID: "session-1", AttemptID: "attempt-1",
		AttemptAuthority:         booking.SchedulingAuthorityExternalProvider,
		AttemptAuthorityProvider: "square", AttemptAuthorityID: "provider-booking-1", AttemptAuthorityVersion: 2,
		AttemptPOSProvider: "square", AttemptPOSID: "provider-booking-1", AttemptPOSVersion: 2,
		AttemptStatus: booking.StatusConfirmed, OperationType: booking.BookingActionBook,
		OperationKey: "conversation:session-1:book", ProviderOutcome: booking.ProviderOutcomeSucceeded,
		RetryPolicy: booking.RetryPolicyNone, ReconciliationStatus: booking.ReconciliationNotRequired,
		AppointmentID:               "appointment-1",
		AppointmentBookingAttemptID: "attempt-1", AppointmentAuthority: booking.SchedulingAuthorityExternalProvider,
		AppointmentAuthorityProvider: "square", AppointmentAuthorityID: "provider-booking-1",
		AppointmentAuthorityVersion: 2, AppointmentPOSProvider: "square",
		AppointmentPOSID: "provider-booking-1", AppointmentPOSVersion: 2,
		AppointmentStatus:   booking.StatusConfirmed,
		AttemptSegmentCount: 1, AppointmentSegmentCount: 1, SegmentsMatch: true,
		TargetAuthorityVersion: sql.NullInt64{},
	}
}
