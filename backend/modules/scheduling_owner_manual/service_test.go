package scheduling_owner_manual

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type fakeStore struct {
	authorityVersion     int64
	eligibleServiceCount int
	readinessErr         error
	createRequest        scheduling.ActionRequest
	createFingerprint    string
	createResult         *scheduling.SchedulingRequest
	createReplayed       bool
	createErr            error

	listResult *scheduling.ListSchedulingRequestsResponse
	listErr    error
	getResult  *scheduling.SchedulingRequest
	getErr     error

	transitionRequest     scheduling.TransitionSchedulingRequest
	transitionFingerprint string
	transitionResult      *scheduling.SchedulingRequest
	transitionReplayed    bool
	transitionErr         error
}

func (f *fakeStore) SchedulingTargetReadinessFacts(context.Context, string, string) (int64, int, error) {
	return f.authorityVersion, f.eligibleServiceCount, f.readinessErr
}

func TestSchedulingTargetReadinessIsRequestOnlyAndCatalogBacked(t *testing.T) {
	ready, err := NewService(&fakeStore{authorityVersion: 7, eligibleServiceCount: 2}).SchedulingTargetReadiness(context.Background(), "salon-1", "owner-1")
	if err != nil {
		t.Fatalf("SchedulingTargetReadiness returned error: %v", err)
	}
	if !ready.AvailabilityReady || ready.ExecutionReady || !ready.Ready || ready.AuthorityVersion != 7 || ready.EligibleServiceCount != 2 {
		t.Fatalf("owner-manual readiness = %#v", ready)
	}
	if len(ready.ExecutionBlockers) != 1 || ready.ExecutionBlockers[0].Code != "OWNER_MANUAL_REQUEST_ONLY" {
		t.Fatalf("execution blockers = %#v", ready.ExecutionBlockers)
	}

	blocked, err := NewService(&fakeStore{authorityVersion: 8}).SchedulingTargetReadiness(context.Background(), "salon-1", "owner-1")
	if err != nil {
		t.Fatalf("blocked SchedulingTargetReadiness returned error: %v", err)
	}
	if blocked.AvailabilityReady || blocked.Ready || len(blocked.AvailabilityBlockers) != 1 || blocked.AvailabilityBlockers[0].Code != "OWNER_MANUAL_ELIGIBLE_SERVICE_REQUIRED" {
		t.Fatalf("blocked owner-manual readiness = %#v", blocked)
	}
}

func (f *fakeStore) CreateOrReplay(_ context.Context, _ string, _ string, req scheduling.ActionRequest, fingerprint string) (*scheduling.SchedulingRequest, bool, error) {
	f.createRequest = req
	f.createFingerprint = fingerprint
	return f.createResult, f.createReplayed, f.createErr
}

func (f *fakeStore) List(context.Context, string, string, scheduling.SchedulingRequestStatus, int, int) (*scheduling.ListSchedulingRequestsResponse, error) {
	return f.listResult, f.listErr
}

func (f *fakeStore) Get(context.Context, string, string, string) (*scheduling.SchedulingRequest, error) {
	return f.getResult, f.getErr
}

func (f *fakeStore) Transition(_ context.Context, _ string, _ string, _ string, req scheduling.TransitionSchedulingRequest, fingerprint string) (*scheduling.SchedulingRequest, bool, error) {
	f.transitionRequest = req
	f.transitionFingerprint = fingerprint
	return f.transitionResult, f.transitionReplayed, f.transitionErr
}

func TestServiceNormalizesPartySizeFromDistinctGuestsWithoutCountingServices(t *testing.T) {
	tests := []struct {
		name          string
		segments      []scheduling.ActionSegment
		wantPartySize int
	}{
		{
			name: "one guest with two services",
			segments: []scheduling.ActionSegment{
				{ServiceID: "service-manicure", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-1", Quantity: 1},
				{ServiceID: "service-pedicure", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-1", Quantity: 1},
			},
			wantPartySize: 1,
		},
		{
			name: "two guests with uneven service counts",
			segments: []scheduling.ActionSegment{
				{ServiceID: "service-a", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-1", Quantity: 1},
				{ServiceID: "service-b", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-1", Quantity: 1},
				{ServiceID: "service-c", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-2", Quantity: 1},
			},
			wantPartySize: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{createResult: &scheduling.SchedulingRequest{ID: "request-1"}}
			service := NewService(store)
			_, _, err := service.CreateRequest(context.Background(), "salon-1", "owner-1", validBookRequest(test.segments))
			if err != nil {
				t.Fatalf("CreateRequest returned error: %v", err)
			}
			if store.createRequest.PartySize != test.wantPartySize {
				t.Fatalf("party_size = %d, want %d", store.createRequest.PartySize, test.wantPartySize)
			}
		})
	}
}

func TestServiceActionFingerprintIncludesSource(t *testing.T) {
	firstStore := &fakeStore{createResult: &scheduling.SchedulingRequest{ID: "request-1"}}
	service := NewService(firstStore)
	req := validBookRequest([]scheduling.ActionSegment{{ServiceID: "service-1", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1}})
	if _, _, err := service.CreateRequest(context.Background(), "salon-1", "owner-1", req); err != nil {
		t.Fatalf("first CreateRequest returned error: %v", err)
	}
	firstFingerprint := firstStore.createFingerprint

	secondStore := &fakeStore{createResult: &scheduling.SchedulingRequest{ID: "request-1"}}
	service = NewService(secondStore)
	req.Source = "conversation"
	if _, _, err := service.CreateRequest(context.Background(), "salon-1", "owner-1", req); err != nil {
		t.Fatalf("second CreateRequest returned error: %v", err)
	}
	if firstFingerprint == secondStore.createFingerprint {
		t.Fatalf("source change did not change request fingerprint")
	}
}

func TestServiceCanonicalizesEquivalentInstantsToUTCBeforeFingerprint(t *testing.T) {
	instant := time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	firstStore := &fakeStore{createResult: &scheduling.SchedulingRequest{ID: "request-1"}}
	first := validBookRequest([]scheduling.ActionSegment{{
		ServiceID:          "service-1",
		StaffSelectionMode: booking.StaffSelectionAnyone,
		Quantity:           1,
		RequestedStartTime: instant,
	}})
	first.RequestedStartTime = instant
	if _, _, err := NewService(firstStore).CreateRequest(context.Background(), "salon-1", "owner-1", first); err != nil {
		t.Fatalf("first CreateRequest: %v", err)
	}

	secondStore := &fakeStore{createResult: &scheduling.SchedulingRequest{ID: "request-1"}}
	offset := time.FixedZone("UTC-5", -5*60*60)
	second := first
	second.RequestedStartTime = instant.In(offset)
	second.Segments = append([]scheduling.ActionSegment(nil), first.Segments...)
	second.Segments[0].RequestedStartTime = instant.In(offset)
	if _, _, err := NewService(secondStore).CreateRequest(context.Background(), "salon-1", "owner-1", second); err != nil {
		t.Fatalf("second CreateRequest: %v", err)
	}
	if firstStore.createFingerprint != secondStore.createFingerprint {
		t.Fatalf("equivalent instants produced different fingerprints: %s != %s", firstStore.createFingerprint, secondStore.createFingerprint)
	}
	if secondStore.createRequest.RequestedStartTime.Location() != time.UTC || secondStore.createRequest.Segments[0].RequestedStartTime.Location() != time.UTC {
		t.Fatalf("timestamps were not canonicalized to UTC: %#v", secondStore.createRequest)
	}
}

func TestServiceRejectsIncompleteCustomerAndUnsafeTargetShapes(t *testing.T) {
	base := validBookRequest([]scheduling.ActionSegment{{ServiceID: "service-1", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1}})
	tests := []struct {
		name   string
		mutate func(*scheduling.ActionRequest)
	}{
		{name: "missing customer name", mutate: func(req *scheduling.ActionRequest) { req.CustomerName = "" }},
		{name: "missing customer phone", mutate: func(req *scheduling.ActionRequest) { req.CustomerPhone = "" }},
		{name: "missing requested timezone", mutate: func(req *scheduling.ActionRequest) { req.RequestedTimezone = "" }},
		{name: "book includes target", mutate: func(req *scheduling.ActionRequest) { req.TargetDescription = "existing appointment" }},
		{name: "reschedule without durable target", mutate: func(req *scheduling.ActionRequest) {
			req.OperationType = scheduling.OperationKindReschedule
		}},
		{name: "target appointment without authority", mutate: func(req *scheduling.ActionRequest) {
			req.OperationType = scheduling.OperationKindReschedule
			req.TargetAppointmentID = "appointment-1"
		}},
		{name: "anyone mode with staff ID", mutate: func(req *scheduling.ActionRequest) {
			req.Segments[0].StaffID = "staff-1"
		}},
		{name: "too many segments", mutate: func(req *scheduling.ActionRequest) {
			req.Segments = make([]scheduling.ActionSegment, maxSegmentsPerRequest+1)
			for i := range req.Segments {
				req.Segments[i] = scheduling.ActionSegment{ServiceID: "service-1", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1}
			}
		}},
		{name: "party size over limit", mutate: func(req *scheduling.ActionRequest) { req.PartySize = maxPartySize + 1 }},
		{name: "segment quantity over limit", mutate: func(req *scheduling.ActionRequest) { req.Segments[0].Quantity = maxSegmentQuantity + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			req.Segments = append([]scheduling.ActionSegment(nil), base.Segments...)
			test.mutate(&req)
			service := NewService(&fakeStore{})
			if _, _, err := service.CreateRequest(context.Background(), "salon-1", "owner-1", req); !errors.Is(err, scheduling.ErrInvalidSchedulingAction) {
				t.Fatalf("error = %v, want ErrInvalidSchedulingAction", err)
			}
		})
	}
}

func TestServiceAcceptsDocumentedNumericBoundaries(t *testing.T) {
	segments := make([]scheduling.ActionSegment, maxSegmentsPerRequest)
	for i := range segments {
		segments[i] = scheduling.ActionSegment{
			ServiceID:          "service-" + strings.Repeat("x", i%3+1),
			StaffSelectionMode: booking.StaffSelectionAnyone,
			Quantity:           maxSegmentQuantity,
		}
	}
	req := validBookRequest(segments)
	req.PartySize = maxPartySize
	store := &fakeStore{createResult: &scheduling.SchedulingRequest{ID: "request-1"}}
	if _, _, err := NewService(store).CreateRequest(context.Background(), "salon-1", "owner-1", req); err != nil {
		t.Fatalf("boundary request returned error: %v", err)
	}
}

func TestRequestCreatedActorUsesTrustedSourceInvariant(t *testing.T) {
	if got := requestCreatedActorUserID(booking.SourceOwnerDashboard, "owner-1"); got != "owner-1" {
		t.Fatalf("owner dashboard actor = %#v, want owner-1", got)
	}
	for _, source := range []string{booking.SourceAIConversationSimulator, booking.SourceAIVoiceCall} {
		if got := requestCreatedActorUserID(source, "owner-1"); got != nil {
			t.Fatalf("source %q actor = %#v, want nil system actor", source, got)
		}
	}
}

func TestServiceTransitionRequiresBoundedTerminalReasonAndStableFingerprint(t *testing.T) {
	store := &fakeStore{transitionResult: &scheduling.SchedulingRequest{ID: "request-1", Status: scheduling.SchedulingRequestStatusResolved}}
	service := NewService(store)
	request := scheduling.TransitionSchedulingRequest{
		ActionKey:        "resolve-request-1",
		ExpectedVersion:  2,
		Status:           scheduling.SchedulingRequestStatusResolved,
		ResolutionReason: "Owner completed follow-up outside automated scheduling.",
		Note:             "Customer was contacted.",
	}
	result, replayed, err := service.Transition(context.Background(), "salon-1", "owner-1", "request-1", request)
	if err != nil || replayed || result != store.transitionResult {
		t.Fatalf("Transition result=%#v replayed=%t err=%v", result, replayed, err)
	}
	if store.transitionFingerprint == "" || store.transitionRequest.ResolutionReason != request.ResolutionReason {
		t.Fatalf("transition was not normalized/fingerprinted: %#v", store.transitionRequest)
	}

	request.ActionKey = "resolve-without-reason"
	request.ResolutionReason = ""
	if _, _, err := service.Transition(context.Background(), "salon-1", "owner-1", "request-1", request); !errors.Is(err, scheduling.ErrInvalidSchedulingAction) {
		t.Fatalf("missing resolution reason error = %v", err)
	}
}

func validBookRequest(segments []scheduling.ActionSegment) scheduling.ActionRequest {
	return scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "owner-manual-book-1",
		Source:             booking.SourceOwnerDashboard,
		CustomerName:       "Linh Tran",
		CustomerPhone:      "+13125550101",
		RequestedStartTime: time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC),
		RequestedTimezone:  "America/Chicago",
		Segments:           segments,
	}
}
