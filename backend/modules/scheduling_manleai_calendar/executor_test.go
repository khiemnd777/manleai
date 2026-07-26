package scheduling_manleai_calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type fakeExecutionStore struct {
	availability      *booking.AvailabilityResult
	created           *InternalCreateResult
	replayed          bool
	err               error
	availabilityCalls int
	createCalls       int
	rescheduleCalls   int
	cancelCalls       int
	lifecycleRequest  InternalLifecycleRequest
}

func (f *fakeExecutionStore) CheckAvailability(context.Context, string, string, booking.AvailabilityRequest, time.Time) (*booking.AvailabilityResult, error) {
	f.availabilityCalls++
	return f.availability, f.err
}

func (f *fakeExecutionStore) CreateAppointment(context.Context, string, string, InternalCreateRequest, time.Time) (*InternalCreateResult, bool, error) {
	f.createCalls++
	return f.created, f.replayed, f.err
}

func (f *fakeExecutionStore) RescheduleAppointment(_ context.Context, _ string, _ string, req InternalLifecycleRequest, _ time.Time) (*InternalCreateResult, bool, error) {
	f.rescheduleCalls++
	f.lifecycleRequest = req
	return f.created, f.replayed, f.err
}

func (f *fakeExecutionStore) CancelAppointment(_ context.Context, _ string, _ string, req InternalLifecycleRequest, _ time.Time) (*InternalCreateResult, bool, error) {
	f.cancelCalls++
	f.lifecycleRequest = req
	return f.created, f.replayed, f.err
}

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

func TestExecutorReturnsVerifiedInternalAvailability(t *testing.T) {
	store := &fakeExecutionStore{availability: &booking.AvailabilityResult{QuoteID: "quote", Slots: []booking.AvailabilitySlot{{Fingerprint: "slot"}}}}
	executor := NewExecutor(store, fixedClock{now: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)})
	result, err := executor.CheckAvailability(context.Background(), "salon", "owner", booking.AvailabilityRequest{
		ServiceID: "22222222-2222-4222-8222-222222222222", StaffSelectionMode: booking.StaffSelectionAnyone,
		PreferredDate: "2026-02-10", Limit: 1,
	})
	if err != nil {
		t.Fatalf("check availability: %v", err)
	}
	if result.Kind != scheduling.AvailabilityKindVerifiedSlots || result.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar || result.VerifiedSlots != store.availability {
		t.Fatalf("availability result = %#v", result)
	}
}

func TestExecutorReturnsOnlyDurableInternalConfirmationEvidenceAndReplay(t *testing.T) {
	action := validInternalAction()
	store := &fakeExecutionStore{
		created:  validInternalStoreResult("appointment-id", "attempt-id", scheduling.OperationKindBook, 0, action.Segments),
		replayed: true,
	}
	executor := NewExecutor(store, fixedClock{now: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)})
	result, err := executor.ExecuteAction(context.Background(), "salon", "owner", action)
	if err != nil {
		t.Fatalf("execute action: %v", err)
	}
	if result.Kind != scheduling.ActionKindConfirmedAppointment || result.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar || !result.Replayed {
		t.Fatalf("action result = %#v", result)
	}
	if result.ConfirmedAppointment == nil || result.ConfirmedAppointment.AppointmentID != "appointment-id" || result.ConfirmedAppointment.BookingAttemptID != "attempt-id" {
		t.Fatalf("confirmation evidence = %#v", result.ConfirmedAppointment)
	}
	if result.ConfirmedAppointment.AppointmentStatus != booking.StatusConfirmed || result.ConfirmedAppointment.ActiveChildCount != 1 {
		t.Fatalf("root confirmation evidence = %#v", result.ConfirmedAppointment)
	}
	if len(result.ConfirmedAppointment.Children) != 1 || result.ConfirmedAppointment.Children[0].AppointmentServiceID != "appointment-id-service-1" {
		t.Fatalf("child confirmation evidence = %#v", result.ConfirmedAppointment.Children)
	}
	if result.ConfirmedAppointment.Appointment != nil || result.ConfirmedAppointment.ExternalAttempt != nil || result.ConfirmedAppointment.ExternalAttemptID != "" {
		t.Fatalf("internal confirmation leaked external/provider DTOs: %#v", result.ConfirmedAppointment)
	}
}

func TestExecutorRoutesWholeRootLifecycleWithExpectedVersion(t *testing.T) {
	store := &fakeExecutionStore{}
	executor := NewExecutor(store, fixedClock{})
	reschedule := validInternalAction()
	reschedule.OperationType = scheduling.OperationKindReschedule
	reschedule.TargetAppointmentID = "77777777-7777-4777-8777-777777777777"
	reschedule.TargetAuthority = booking.SchedulingAuthorityManleAICalendar
	reschedule.ExpectedTargetAuthorityAppointmentVersion = 2
	store.created = validInternalStoreResult("appointment-id", "attempt-id", scheduling.OperationKindReschedule, 2, reschedule.Segments)
	result, err := executor.ExecuteAction(context.Background(), "salon", "owner", reschedule)
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if store.rescheduleCalls != 1 || store.createCalls != 0 ||
		store.lifecycleRequest.ExpectedTargetAuthorityAppointmentVersion != 2 {
		t.Fatalf("reschedule routing = calls:%d/%d request:%#v", store.rescheduleCalls, store.createCalls, store.lifecycleRequest)
	}
	if result.TargetAuthorityAppointmentVersion != 2 || result.AuthorityAppointmentVersion != 3 {
		t.Fatalf("reschedule result metadata = %#v", result)
	}
	if result.ConfirmedAppointment == nil || result.ConfirmedAppointment.AppointmentStatus != booking.StatusRescheduled ||
		result.ConfirmedAppointment.ActiveChildCount != 1 {
		t.Fatalf("reschedule root evidence = %#v", result.ConfirmedAppointment)
	}

	cancel := scheduling.ActionRequest{
		OperationType: scheduling.OperationKindCancel, OperationKey: "cancel-key",
		Source: booking.SourceOwnerDashboard, Notes: "customer request",
		TargetAppointmentID:                       "77777777-7777-4777-8777-777777777777",
		TargetAuthority:                           booking.SchedulingAuthorityManleAICalendar,
		ExpectedTargetAuthorityAppointmentVersion: 2,
	}
	store.created = validInternalStoreResult("appointment-id", "cancel-attempt-id", scheduling.OperationKindCancel, 2, reschedule.Segments)
	cancelResult, err := executor.ExecuteAction(context.Background(), "salon", "owner", cancel)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if store.cancelCalls != 1 {
		t.Fatalf("cancel calls = %d", store.cancelCalls)
	}
	if cancelResult.ConfirmedAppointment == nil || cancelResult.ConfirmedAppointment.AppointmentStatus != booking.StatusCancelled ||
		cancelResult.ConfirmedAppointment.ActiveChildCount != 0 {
		t.Fatalf("cancel root evidence = %#v", cancelResult.ConfirmedAppointment)
	}
}

func TestExecutorRejectsStoreIDsWithoutAuthoritativeInternalResultEvidence(t *testing.T) {
	action := validInternalAction()
	base := validInternalStoreResult("appointment-id", "attempt-id", scheduling.OperationKindBook, 0, action.Segments)
	tests := []struct {
		name   string
		mutate func(*InternalCreateResult)
	}{
		{name: "missing status", mutate: func(result *InternalCreateResult) { result.AppointmentStatus = "" }},
		{name: "wrong status", mutate: func(result *InternalCreateResult) { result.AppointmentStatus = booking.StatusRescheduled }},
		{name: "missing authority version", mutate: func(result *InternalCreateResult) { result.AuthorityAppointmentVersion = 0 }},
		{name: "wrong authority version", mutate: func(result *InternalCreateResult) { result.AuthorityAppointmentVersion = 2 }},
		{name: "missing active child count", mutate: func(result *InternalCreateResult) { result.ActiveChildCount = 0 }},
		{name: "wrong active child count", mutate: func(result *InternalCreateResult) { result.ActiveChildCount = 2 }},
		{name: "missing child evidence", mutate: func(result *InternalCreateResult) { result.Children = nil }},
		{name: "mismatched child service", mutate: func(result *InternalCreateResult) { result.Children[0].ServiceID = "different-service" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyResult := *base
			copyResult.Children = append([]InternalCreateSegmentResult{}, base.Children...)
			test.mutate(&copyResult)
			executor := NewExecutor(&fakeExecutionStore{created: &copyResult}, fixedClock{})
			result, err := executor.ExecuteAction(context.Background(), "salon", "owner", action)
			if !errors.Is(err, scheduling.ErrInvalidSchedulingResult) || result != nil {
				t.Fatalf("result = %#v, error = %v", result, err)
			}
		})
	}
}

func TestNormalizeInternalLifecycleRejectsPartialCancelAndChangedProof(t *testing.T) {
	cancel := scheduling.ActionRequest{
		OperationType: scheduling.OperationKindCancel, OperationKey: "cancel-key",
		Source: booking.SourceOwnerDashboard, TargetAppointmentID: "77777777-7777-4777-8777-777777777777",
		TargetAuthority: booking.SchedulingAuthorityManleAICalendar,
		ExpectedTargetAuthorityAppointmentVersion: 2,
	}
	normalized, err := normalizeInternalLifecycleRequest(cancel)
	if err != nil {
		t.Fatalf("normalize cancel: %v", err)
	}
	partial := cancel
	partial.Segments = []scheduling.ActionSegment{{ServiceID: "22222222-2222-4222-8222-222222222222"}}
	if _, err := normalizeInternalLifecycleRequest(partial); !errors.Is(err, scheduling.ErrInvalidSchedulingAction) {
		t.Fatalf("partial cancel error = %v", err)
	}
	changed := cancel
	changed.ExpectedTargetAuthorityAppointmentVersion = 3
	changedNormalized, err := normalizeInternalLifecycleRequest(changed)
	if err != nil {
		t.Fatalf("normalize changed cancel: %v", err)
	}
	if normalized.RequestFingerprint == changedNormalized.RequestFingerprint {
		t.Fatal("changed expected target version reused lifecycle fingerprint")
	}
}

func TestExecutorRejectsMixedGuestReferencesBeforeAvailabilityOrCreateStore(t *testing.T) {
	store := &fakeExecutionStore{}
	executor := NewExecutor(store, fixedClock{})
	availability := booking.AvailabilityRequest{
		PartySize: 1, PreferredDate: "2026-02-10", Limit: 1,
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: "22222222-2222-4222-8222-222222222222", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-a", Quantity: 1},
			{ServiceID: "55555555-5555-4555-8555-555555555555", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1},
		},
	}
	if _, err := executor.CheckAvailability(context.Background(), "salon", "owner", availability); !errors.Is(err, booking.ErrValidation) {
		t.Fatalf("mixed guest availability error = %v", err)
	}
	if store.availabilityCalls != 0 {
		t.Fatalf("mixed guest availability reached store %d times", store.availabilityCalls)
	}

	action := validInternalAction()
	action.Segments[0].GuestReference = "guest-a"
	second := action.Segments[0]
	second.ServiceID = "55555555-5555-4555-8555-555555555555"
	second.StaffID = "66666666-6666-4666-8666-666666666666"
	second.GuestReference = ""
	action.Segments = append(action.Segments, second)
	if _, err := executor.ExecuteAction(context.Background(), "salon", "owner", action); !errors.Is(err, scheduling.ErrInvalidSchedulingAction) {
		t.Fatalf("mixed guest action error = %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("mixed guest action reached create store %d times", store.createCalls)
	}
}

func TestNormalizeInternalCreateRequestPreservesOrderedConcretePartySegments(t *testing.T) {
	req := validInternalAction()
	req.PartySize = 2
	second := req.Segments[0]
	req.Segments[0].GuestReference = "guest-a"
	second.GuestReference = "guest-b"
	second.ServiceID = "55555555-5555-4555-8555-555555555555"
	second.StaffID = "66666666-6666-4666-8666-666666666666"
	req.Segments = append(req.Segments, second)
	normalized, err := normalizeInternalCreateRequest(req)
	if err != nil {
		t.Fatalf("normalize party: %v", err)
	}
	if normalized.PartySize != 2 || len(normalized.Segments) != 2 || normalized.Segments[0].GuestReference != "guest-a" || normalized.Segments[1].GuestReference != "guest-b" {
		t.Fatalf("normalized party = %#v", normalized)
	}
	changed := req
	changed.Segments = append([]scheduling.ActionSegment{}, req.Segments...)
	changed.Segments[1].StaffID = "77777777-7777-4777-8777-777777777777"
	changedNormalized, err := normalizeInternalCreateRequest(changed)
	if err != nil {
		t.Fatalf("normalize changed party: %v", err)
	}
	if normalized.RequestFingerprint == changedNormalized.RequestFingerprint {
		t.Fatal("changed ordered segment evidence produced the same operation fingerprint")
	}
}

func TestNormalizeInternalCreateRequestRejectsInvalidGuestPartyEvidence(t *testing.T) {
	tests := []struct {
		name      string
		partySize int
		guests    []string
	}{
		{name: "mixed references for one guest", partySize: 1, guests: []string{"guest-a", ""}},
		{name: "duplicate reference for two guests", partySize: 2, guests: []string{"guest-a", "guest-a"}},
		{name: "anonymous multi guest", partySize: 2, guests: []string{"", ""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validInternalAction()
			req.PartySize = test.partySize
			req.Segments[0].GuestReference = test.guests[0]
			second := req.Segments[0]
			second.ServiceID = "55555555-5555-4555-8555-555555555555"
			second.StaffID = "66666666-6666-4666-8666-666666666666"
			second.GuestReference = test.guests[1]
			req.Segments = append(req.Segments, second)
			if _, err := normalizeInternalCreateRequest(req); !errors.Is(err, scheduling.ErrInvalidSchedulingAction) {
				t.Fatalf("normalize error = %v", err)
			}
		})
	}
}

func TestInternalCreateFingerprintIncludesExactQuoteEvidence(t *testing.T) {
	left, err := normalizeInternalCreateRequest(validInternalAction())
	if err != nil {
		t.Fatalf("normalize left: %v", err)
	}
	rightRequest := validInternalAction()
	rightRequest.AvailabilityQuoteID = "44444444-4444-4444-8444-444444444444"
	rightRequest.SlotFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	right, err := normalizeInternalCreateRequest(rightRequest)
	if err != nil {
		t.Fatalf("normalize right: %v", err)
	}
	if left.RequestFingerprint == right.RequestFingerprint {
		t.Fatal("changed quote/slot evidence produced the same operation fingerprint")
	}
}

func validInternalAction() scheduling.ActionRequest {
	start := time.Date(2026, 2, 10, 15, 0, 0, 0, time.UTC)
	return scheduling.ActionRequest{
		OperationType: scheduling.OperationKindBook, OperationKey: "operation-key",
		AvailabilityQuoteID: "11111111-1111-4111-8111-111111111111",
		SlotFingerprint:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Source:              booking.SourceOwnerDashboard,
		CustomerName:        "Linh Nguyen", CustomerPhone: "+13125550101",
		Segments: []scheduling.ActionSegment{{
			ServiceID: "22222222-2222-4222-8222-222222222222", StaffID: "33333333-3333-4333-8333-333333333333",
			StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
			RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute),
		}},
		RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute),
		RequestedTimezone: "America/Chicago", PartySize: 1,
	}
}

func validInternalStoreResult(
	appointmentID string,
	attemptID string,
	operation scheduling.OperationKind,
	targetVersion int,
	segments []scheduling.ActionSegment,
) *InternalCreateResult {
	status := booking.StatusConfirmed
	version := 1
	activeChildCount := len(segments)
	if operation == scheduling.OperationKindReschedule {
		status = booking.StatusRescheduled
		version = targetVersion + 1
	}
	if operation == scheduling.OperationKindCancel {
		status = booking.StatusCancelled
		version = targetVersion + 1
		activeChildCount = 0
	}
	result := &InternalCreateResult{
		AppointmentID:                     appointmentID,
		BookingAttemptID:                  attemptID,
		AppointmentStatus:                 status,
		TargetAuthorityAppointmentVersion: targetVersion,
		AuthorityAppointmentVersion:       version,
		ActiveChildCount:                  activeChildCount,
	}
	for index, segment := range segments {
		result.Children = append(result.Children, InternalCreateSegmentResult{
			AppointmentServiceID: appointmentID + "-service-" + string(rune('1'+index)),
			GuestReference:       segment.GuestReference,
			ServiceID:            segment.ServiceID,
			StaffID:              segment.StaffID,
			StaffSelectionMode:   segment.StaffSelectionMode,
			Quantity:             segment.Quantity,
			ScheduledStartTime:   segment.RequestedStartTime,
			ScheduledEndTime:     segment.RequestedEndTime,
			OccupiedStartTime:    segment.RequestedStartTime,
			OccupiedEndTime:      segment.RequestedEndTime,
		})
	}
	return result
}
