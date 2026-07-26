package scheduling_owner_manual

import (
	"context"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type fakeRequestCreator struct {
	request *scheduling.SchedulingRequest
	err     error
	calls   int
}

func (f *fakeRequestCreator) CreateRequest(context.Context, string, string, scheduling.ActionRequest) (*scheduling.SchedulingRequest, bool, error) {
	f.calls++
	return f.request, false, f.err
}

func TestExecutorReturnsRequestOnlyAvailabilityWithoutSyntheticSlots(t *testing.T) {
	executor := NewExecutor(&fakeRequestCreator{})
	result, err := executor.CheckAvailability(context.Background(), "salon-1", "owner-1", booking.AvailabilityRequest{PreferredDate: "2026-08-02"})
	if err != nil {
		t.Fatalf("CheckAvailability returned error: %v", err)
	}
	if result.Kind != scheduling.AvailabilityKindRequestOnly || result.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || result.VerifiedSlots != nil {
		t.Fatalf("availability result = %#v, want request_only without verified slots", result)
	}
}

func TestExecutorReturnsPendingOwnerReviewWithoutAppointmentOrExternalFallback(t *testing.T) {
	creator := &fakeRequestCreator{request: &scheduling.SchedulingRequest{
		ID:      "request-1",
		Status:  scheduling.SchedulingRequestStatusPending,
		Version: 1,
	}}
	executor := NewExecutor(creator)
	result, err := executor.ExecuteAction(context.Background(), "salon-1", "owner-1", scheduling.ActionRequest{
		OperationType: scheduling.OperationKindBook,
		OperationKey:  "owner-manual-book-1",
	})
	if err != nil {
		t.Fatalf("ExecuteAction returned error: %v", err)
	}
	if result.Kind != scheduling.ActionKindPendingOwnerReview || result.PendingOwnerReview == nil || result.PendingOwnerReview.SchedulingRequestID != "request-1" {
		t.Fatalf("action result = %#v, want pending owner review", result)
	}
	if result.ConfirmedAppointment != nil || result.ExternalFallbackPending != nil || creator.calls != 1 {
		t.Fatalf("executor manufactured appointment/fallback or wrong calls: result=%#v calls=%d", result, creator.calls)
	}
}

func TestExecutorPersistsPartySemanticsAsOnePendingOwnerRequest(t *testing.T) {
	store := &fakeStore{createResult: &scheduling.SchedulingRequest{
		ID:      "request-party",
		Status:  scheduling.SchedulingRequestStatusPending,
		Version: 1,
	}}
	executor := NewExecutor(NewService(store))
	requestedStart := time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
	req := scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "owner-party",
		Source:             booking.SourceAIVoiceCall,
		CustomerName:       "Party Caller",
		CustomerPhone:      "+13125550101",
		RequestedTimezone:  "America/Chicago",
		RequestedStartTime: requestedStart,
		PartySize:          3,
		Segments: []scheduling.ActionSegment{
			{
				ServiceID:          "service-1",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				GuestReference:     "guest-1",
				Quantity:           2,
				RequestedStartTime: requestedStart,
				RequestedEndTime:   requestedStart.Add(45 * time.Minute),
			},
			{
				ServiceID:          "service-2",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				GuestReference:     "guest-2",
				Quantity:           1,
				RequestedStartTime: requestedStart,
				RequestedEndTime:   requestedStart.Add(60 * time.Minute),
			},
		},
	}

	result, err := executor.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
	if err != nil || result.Kind != scheduling.ActionKindPendingOwnerReview || result.PendingOwnerReview.SchedulingRequestID != "request-party" {
		t.Fatalf("party result = %#v/%v", result, err)
	}
	if store.createRequest.PartySize != 3 || len(store.createRequest.Segments) != 2 ||
		store.createRequest.Segments[0].Quantity != 2 || store.createRequest.Segments[0].GuestReference != "guest-1" ||
		store.createRequest.Segments[0].RequestedStartTime.IsZero() {
		t.Fatalf("owner request lost party semantics: %#v", store.createRequest)
	}
}
