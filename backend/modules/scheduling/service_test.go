package scheduling

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type resolutionCall struct {
	salonID     string
	ownerUserID string
}

type fakeAuthorityResolver struct {
	authority              string
	err                    error
	calls                  []resolutionCall
	operationAuthorities   map[string]string
	operationOrigins       map[string]PersistedOperationOrigin
	attemptAuthorities     map[string]string
	appointmentAuthorities map[string]string
	quoteAuthorities       map[string]string
	policyMode             BookingMode
	policyAuthority        string
	operationErr           error
	attemptErr             error
	availabilityRetryErr   error
	appointmentErr         error
	operationCalls         []string
	attemptCalls           []string
	availabilityRetryCalls []string
	appointmentCalls       []string
	quoteCalls             []string
}

func (f *fakeAuthorityResolver) ResolveAvailabilityQuoteSchedulingAuthority(_ context.Context, _ string, _ string, quoteID string) (string, error) {
	f.quoteCalls = append(f.quoteCalls, quoteID)
	if strings.TrimSpace(quoteID) == "" {
		return "", booking.ErrAvailabilityQuoteStale
	}
	if authority, found := f.quoteAuthorities[quoteID]; found {
		return authority, nil
	}
	return f.authority, nil
}

func (f *fakeAuthorityResolver) ResolveSchedulingAuthority(_ context.Context, salonID string, ownerUserID string) (string, error) {
	f.calls = append(f.calls, resolutionCall{salonID: salonID, ownerUserID: ownerUserID})
	return f.authority, f.err
}

func (f *fakeAuthorityResolver) ResolveConversationSchedulingPolicy(_ context.Context, salonID string, ownerUserID string) (ConversationPolicyFence, error) {
	f.calls = append(f.calls, resolutionCall{salonID: salonID, ownerUserID: ownerUserID})
	mode := f.policyMode
	if mode == "" {
		mode = BookingModePendingApproval
	}
	authority := f.policyAuthority
	if authority == "" {
		authority = f.authority
	}
	return ConversationPolicyFence{BookingMode: mode, SchedulingAuthority: authority}, f.err
}

func (f *fakeAuthorityResolver) FindOperationSchedulingAuthority(_ context.Context, _ string, _ string, operationKey string) (string, bool, error) {
	f.operationCalls = append(f.operationCalls, operationKey)
	if f.operationErr != nil {
		return "", false, f.operationErr
	}
	authority, found := f.operationAuthorities[operationKey]
	return authority, found, nil
}

func (f *fakeAuthorityResolver) FindOperationSchedulingOrigin(_ context.Context, _ string, _ string, operationKey string) (PersistedOperationOrigin, bool, error) {
	f.operationCalls = append(f.operationCalls, operationKey)
	if f.operationErr != nil {
		return PersistedOperationOrigin{}, false, f.operationErr
	}
	if origin, found := f.operationOrigins[operationKey]; found {
		return origin, true, nil
	}
	authority, found := f.operationAuthorities[operationKey]
	return PersistedOperationOrigin{SchedulingAuthority: authority}, found, nil
}

func (f *fakeAuthorityResolver) ResolveAttemptSchedulingAuthority(_ context.Context, _ string, _ string, attemptID string) (string, error) {
	f.attemptCalls = append(f.attemptCalls, attemptID)
	if f.attemptErr != nil {
		return "", f.attemptErr
	}
	if authority, found := f.attemptAuthorities[attemptID]; found {
		return authority, nil
	}
	return f.authority, nil
}

func (f *fakeAuthorityResolver) ResolveAvailabilityRetrySchedulingAuthority(_ context.Context, _ string, _ string, attemptID string) (string, error) {
	f.availabilityRetryCalls = append(f.availabilityRetryCalls, attemptID)
	if f.availabilityRetryErr != nil {
		return "", f.availabilityRetryErr
	}
	if authority, found := f.attemptAuthorities[attemptID]; found {
		return authority, nil
	}
	return "", booking.ErrOperationConflict
}

func (f *fakeAuthorityResolver) ResolveAppointmentSchedulingAuthority(_ context.Context, _ string, _ string, appointmentID string) (string, error) {
	f.appointmentCalls = append(f.appointmentCalls, appointmentID)
	if f.appointmentErr != nil {
		return "", f.appointmentErr
	}
	if authority, found := f.appointmentAuthorities[appointmentID]; found {
		return authority, nil
	}
	return f.authority, nil
}

type fakeExecutor struct {
	authority string
	calls     []string

	availabilityRequest booking.AvailabilityRequest
	availabilityResult  *booking.AvailabilityResult
	availabilityErr     error

	createRequest booking.CreateBookingRequest
	createResult  *booking.BookingAttempt
	createErr     error

	candidatesRequest booking.RescheduleLookupRequest
	candidatesResult  []booking.AppointmentActionRef
	candidatesErr     error

	rescheduleAppointmentID string
	rescheduleRequest       booking.RescheduleRequest
	rescheduleAppointment   *booking.Appointment
	rescheduleFallback      *booking.BookingAttempt
	rescheduleErr           error

	cancelAppointmentID string
	cancelRequest       booking.CancelRequest
	cancelAppointment   *booking.Appointment
	cancelFallback      *booking.BookingAttempt
	cancelErr           error
}

type fakeNeutralExecutor struct {
	authority          string
	availabilityResult *AvailabilityResult
	actionResult       *ActionResult
	actionRequest      ActionRequest
	availabilityCalls  int
	actionCalls        int
}

func (f *fakeNeutralExecutor) SchedulingAuthority() string { return f.authority }

func (f *fakeNeutralExecutor) CheckAvailability(context.Context, string, string, booking.AvailabilityRequest) (*AvailabilityResult, error) {
	f.availabilityCalls++
	return f.availabilityResult, nil
}

func (f *fakeNeutralExecutor) ExecuteAction(_ context.Context, _ string, _ string, req ActionRequest) (*ActionResult, error) {
	f.actionCalls++
	f.actionRequest = req
	return f.actionResult, nil
}

func (f *fakeExecutor) SchedulingAuthority() string { return f.authority }

func (f *fakeExecutor) AvailableSlots(_ context.Context, _ string, _ string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.calls = append(f.calls, "availability")
	f.availabilityRequest = req
	return f.availabilityResult, f.availabilityErr
}

func (f *fakeExecutor) Create(_ context.Context, _ string, _ string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls = append(f.calls, "create")
	f.createRequest = req
	return f.createResult, f.createErr
}

func (f *fakeExecutor) RescheduleCandidates(_ context.Context, _ string, _ string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	f.calls = append(f.calls, "candidates")
	f.candidatesRequest = req
	return f.candidatesResult, f.candidatesErr
}

func (f *fakeExecutor) Reschedule(_ context.Context, _ string, _ string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.calls = append(f.calls, "reschedule")
	f.rescheduleAppointmentID = appointmentID
	f.rescheduleRequest = req
	return f.rescheduleAppointment, f.rescheduleFallback, f.rescheduleErr
}

func (f *fakeExecutor) Cancel(_ context.Context, _ string, _ string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.calls = append(f.calls, "cancel")
	f.cancelAppointmentID = appointmentID
	f.cancelRequest = req
	return f.cancelAppointment, f.cancelFallback, f.cancelErr
}

func TestConversationBookingModeMatrixDispatchesWithoutChangingAdminBoundary(t *testing.T) {
	tests := []struct {
		name              string
		mode              BookingMode
		authority         string
		wantExecutor      string
		wantTarget        string
		wantDisabledError bool
	}{
		{name: "owner manual pending", mode: BookingModePendingApproval, authority: booking.SchedulingAuthorityOwnerManual, wantExecutor: booking.SchedulingAuthorityOwnerManual, wantTarget: booking.SchedulingAuthorityOwnerManual},
		{name: "internal pending", mode: BookingModePendingApproval, authority: booking.SchedulingAuthorityManleAICalendar, wantExecutor: booking.SchedulingAuthorityOwnerManual, wantTarget: booking.SchedulingAuthorityManleAICalendar},
		{name: "external pending", mode: BookingModePendingApproval, authority: booking.SchedulingAuthorityExternalProvider, wantExecutor: booking.SchedulingAuthorityOwnerManual, wantTarget: booking.SchedulingAuthorityExternalProvider},
		{name: "internal confirmed", mode: BookingModeConfirmedBooking, authority: booking.SchedulingAuthorityManleAICalendar, wantExecutor: booking.SchedulingAuthorityManleAICalendar},
		{name: "external confirmed", mode: BookingModeConfirmedBooking, authority: booking.SchedulingAuthorityExternalProvider, wantExecutor: booking.SchedulingAuthorityExternalProvider},
		{name: "disabled", mode: BookingModeDisabled, authority: booking.SchedulingAuthorityExternalProvider, wantDisabledError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeAuthorityResolver{
				authority: tt.authority, policyMode: tt.mode, policyAuthority: tt.authority,
				operationAuthorities: map[string]string{}, operationOrigins: map[string]PersistedOperationOrigin{},
			}
			owner := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityOwnerManual, actionResult: &ActionResult{Kind: ActionKindPendingOwnerReview, SchedulingAuthority: booking.SchedulingAuthorityOwnerManual}}
			internal := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityManleAICalendar, actionResult: &ActionResult{Kind: ActionKindConfirmedAppointment, SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar}}
			external := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityExternalProvider, actionResult: &ActionResult{Kind: ActionKindConfirmedAppointment, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}}
			service := NewService(resolver, nil, owner, internal, external)
			request := ActionRequest{
				OperationType: OperationKindBook, OperationKey: "matrix-op", AvailabilityQuoteID: "quote", SlotFingerprint: strings.Repeat("a", 64),
				CustomerName: "Kim", CustomerPhone: "+13125550101", RequestedTimezone: "America/Chicago",
			}
			_, err := service.ExecuteConversationAction(context.Background(), "salon", "owner", ConversationPolicyFence{BookingMode: tt.mode, SchedulingAuthority: tt.authority}, request)
			if tt.wantDisabledError {
				if !errors.Is(err, ErrConversationSchedulingDisabled) {
					t.Fatalf("disabled error=%v", err)
				}
				if owner.actionCalls+internal.actionCalls+external.actionCalls != 0 {
					t.Fatalf("disabled executor calls owner/internal/external=%d/%d/%d", owner.actionCalls, internal.actionCalls, external.actionCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := map[string]int{
				booking.SchedulingAuthorityOwnerManual:      owner.actionCalls,
				booking.SchedulingAuthorityManleAICalendar:  internal.actionCalls,
				booking.SchedulingAuthorityExternalProvider: external.actionCalls,
			}
			for authority, count := range calls {
				want := 0
				if authority == tt.wantExecutor {
					want = 1
				}
				if count != want {
					t.Fatalf("executor %s calls=%d want=%d", authority, count, want)
				}
			}
			if tt.mode == BookingModePendingApproval {
				if owner.actionRequest.TargetAuthority != tt.wantTarget || owner.actionRequest.AvailabilityQuoteID != "" || owner.actionRequest.SlotFingerprint != "" {
					t.Fatalf("pending request target/quote/fingerprint=%q/%q/%q", owner.actionRequest.TargetAuthority, owner.actionRequest.AvailabilityQuoteID, owner.actionRequest.SlotFingerprint)
				}
			}
		})
	}
}

func TestConversationReplayUsesDurableRequestTargetAfterPolicyChange(t *testing.T) {
	resolver := &fakeAuthorityResolver{
		authority:            booking.SchedulingAuthorityExternalProvider,
		policyMode:           BookingModeConfirmedBooking,
		operationAuthorities: map[string]string{"pending-op": booking.SchedulingAuthorityOwnerManual},
		operationOrigins: map[string]PersistedOperationOrigin{
			"pending-op": {
				SchedulingAuthority: booking.SchedulingAuthorityOwnerManual, SchedulingRequest: true,
				RequestTargetAuthority: booking.SchedulingAuthorityManleAICalendar, RequestTargetAuthorityPresent: true,
			},
		},
	}
	owner := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityOwnerManual, actionResult: &ActionResult{Kind: ActionKindPendingOwnerReview, SchedulingAuthority: booking.SchedulingAuthorityOwnerManual}}
	external := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	service := NewService(resolver, nil, owner, external)
	_, err := service.ExecuteConversationAction(context.Background(), "salon", "owner", ConversationPolicyFence{BookingMode: BookingModeConfirmedBooking, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}, ActionRequest{OperationType: OperationKindBook, OperationKey: "pending-op"})
	if err != nil {
		t.Fatal(err)
	}
	if owner.actionCalls != 1 || external.actionCalls != 0 || owner.actionRequest.TargetAuthority != booking.SchedulingAuthorityManleAICalendar {
		t.Fatalf("replay calls owner/external=%d/%d target=%q", owner.actionCalls, external.actionCalls, owner.actionRequest.TargetAuthority)
	}
}

func TestConversationLegacyTargetNullReplayDoesNotInventAuthority(t *testing.T) {
	resolver := &fakeAuthorityResolver{
		authority:            booking.SchedulingAuthorityExternalProvider,
		policyMode:           BookingModeConfirmedBooking,
		operationAuthorities: map[string]string{"legacy-op": booking.SchedulingAuthorityOwnerManual},
		operationOrigins: map[string]PersistedOperationOrigin{
			"legacy-op": {SchedulingAuthority: booking.SchedulingAuthorityOwnerManual, SchedulingRequest: true},
		},
	}
	owner := &fakeNeutralExecutor{authority: booking.SchedulingAuthorityOwnerManual, actionResult: &ActionResult{Kind: ActionKindPendingOwnerReview, SchedulingAuthority: booking.SchedulingAuthorityOwnerManual}}
	service := NewService(resolver, nil, owner)
	_, err := service.ExecuteConversationAction(context.Background(), "salon", "owner", ConversationPolicyFence{BookingMode: BookingModeConfirmedBooking, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}, ActionRequest{OperationType: OperationKindBook, OperationKey: "legacy-op"})
	if err != nil {
		t.Fatal(err)
	}
	if owner.actionRequest.TargetAuthority != "" {
		t.Fatalf("legacy replay invented target authority %q", owner.actionRequest.TargetAuthority)
	}
}

func TestServiceCurrentSchedulingAuthorityIsOwnerScopedAndValidated(t *testing.T) {
	tests := []struct {
		name       string
		authority  string
		resolveErr error
		wantErr    error
	}{
		{name: "known internal authority", authority: booking.SchedulingAuthorityOwnerManual},
		{name: "known external authority", authority: booking.SchedulingAuthorityExternalProvider},
		{name: "unknown authority", authority: "future_authority", wantErr: booking.ErrSchedulingAuthorityNotReady},
		{name: "owner lookup failure", resolveErr: pos.ErrNotFound, wantErr: pos.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeAuthorityResolver{authority: test.authority, err: test.resolveErr}
			service := NewService(resolver, nil)

			authority, err := service.CurrentSchedulingAuthority(context.Background(), "salon-1", "owner-1")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && authority != test.authority {
				t.Fatalf("authority = %q, want %q", authority, test.authority)
			}
			if test.wantErr != nil && authority != "" {
				t.Fatalf("authority = %q, want empty on error", authority)
			}
			if !reflect.DeepEqual(resolver.calls, []resolutionCall{{salonID: "salon-1", ownerUserID: "owner-1"}}) {
				t.Fatalf("resolver calls = %#v, want owner-scoped lookup", resolver.calls)
			}
		})
	}
}

func TestServiceResolveCreateSchedulingAuthorityUsesPersistedLineageBeforeCurrentMode(t *testing.T) {
	tests := []struct {
		name              string
		resolver          *fakeAuthorityResolver
		operationKey      string
		retryOfAttemptID  string
		wantAuthority     string
		wantErr           error
		wantCurrentCalls  int
		wantAttemptCalls  []string
		wantOperationCall []string
	}{
		{
			name: "external retry survives current internal mode",
			resolver: &fakeAuthorityResolver{
				authority:          booking.SchedulingAuthorityOwnerManual,
				attemptAuthorities: map[string]string{"attempt-external": booking.SchedulingAuthorityExternalProvider},
			},
			operationKey:      "operation-retry",
			retryOfAttemptID:  "attempt-external",
			wantAuthority:     booking.SchedulingAuthorityExternalProvider,
			wantAttemptCalls:  []string{"attempt-external"},
			wantOperationCall: []string{"operation-retry"},
		},
		{
			name: "conflicting operation and retry origins",
			resolver: &fakeAuthorityResolver{
				authority:            booking.SchedulingAuthorityOwnerManual,
				operationAuthorities: map[string]string{"operation-conflict": booking.SchedulingAuthorityExternalProvider},
				attemptAuthorities:   map[string]string{"attempt-internal": booking.SchedulingAuthorityManleAICalendar},
			},
			operationKey:      "operation-conflict",
			retryOfAttemptID:  "attempt-internal",
			wantErr:           booking.ErrOperationConflict,
			wantAttemptCalls:  []string{"attempt-internal"},
			wantOperationCall: []string{"operation-conflict"},
		},
		{
			name:              "cross tenant retry remains not found",
			resolver:          &fakeAuthorityResolver{authority: booking.SchedulingAuthorityOwnerManual, attemptErr: pos.ErrNotFound},
			operationKey:      "operation-cross-tenant",
			retryOfAttemptID:  "attempt-other-tenant",
			wantErr:           pos.ErrNotFound,
			wantAttemptCalls:  []string{"attempt-other-tenant"},
			wantOperationCall: []string{"operation-cross-tenant"},
		},
		{
			name:              "new operation uses current internal mode",
			resolver:          &fakeAuthorityResolver{authority: booking.SchedulingAuthorityOwnerManual},
			operationKey:      "operation-new",
			wantAuthority:     booking.SchedulingAuthorityOwnerManual,
			wantCurrentCalls:  1,
			wantOperationCall: []string{"operation-new"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			service := NewService(test.resolver, nil, executor)

			authority, err := service.ResolveCreateSchedulingAuthority(context.Background(), "salon-1", "owner-1", test.operationKey, test.retryOfAttemptID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if authority != test.wantAuthority {
				t.Fatalf("authority = %q, want %q", authority, test.wantAuthority)
			}
			if len(test.resolver.calls) != test.wantCurrentCalls {
				t.Fatalf("current authority calls = %#v, want %d", test.resolver.calls, test.wantCurrentCalls)
			}
			if !reflect.DeepEqual(test.resolver.operationCalls, test.wantOperationCall) || !reflect.DeepEqual(test.resolver.attemptCalls, test.wantAttemptCalls) {
				t.Fatalf("persisted origin calls = operation:%#v attempt:%#v", test.resolver.operationCalls, test.resolver.attemptCalls)
			}
			if len(executor.calls) != 0 {
				t.Fatalf("read-only authority resolution dispatched executor calls: %#v", executor.calls)
			}
		})
	}
}

func TestServiceExternalProviderDelegatesAllAuthorityOperationsExactly(t *testing.T) {
	delegateErr := errors.New("delegate evidence")
	availabilityResult := &booking.AvailabilityResult{QuoteID: "quote-1"}
	createResult := &booking.BookingAttempt{ID: "attempt-create"}
	candidatesResult := []booking.AppointmentActionRef{{ID: "candidate-1"}}
	rescheduleAppointment := &booking.Appointment{ID: "appointment-rescheduled"}
	rescheduleFallback := &booking.BookingAttempt{ID: "attempt-reschedule"}
	cancelAppointment := &booking.Appointment{ID: "appointment-cancelled"}
	cancelFallback := &booking.BookingAttempt{ID: "attempt-cancel"}
	executor := &fakeExecutor{
		authority:             booking.SchedulingAuthorityExternalProvider,
		availabilityResult:    availabilityResult,
		availabilityErr:       delegateErr,
		createResult:          createResult,
		createErr:             delegateErr,
		candidatesResult:      candidatesResult,
		candidatesErr:         delegateErr,
		rescheduleAppointment: rescheduleAppointment,
		rescheduleFallback:    rescheduleFallback,
		rescheduleErr:         delegateErr,
		cancelAppointment:     cancelAppointment,
		cancelFallback:        cancelFallback,
		cancelErr:             delegateErr,
	}
	resolver := &fakeAuthorityResolver{authority: booking.SchedulingAuthorityExternalProvider}
	history := &fakeHistoryService{rescheduleCandidates: candidatesResult, err: delegateErr}
	service := NewService(resolver, history, executor)
	ctx := context.Background()

	availabilityReq := booking.AvailabilityRequest{ServiceID: "service-1", PreferredDate: "2026-08-01", Limit: 7}
	gotAvailability, err := service.AvailableSlots(ctx, "salon-1", "owner-1", availabilityReq)
	if gotAvailability != availabilityResult || !errors.Is(err, delegateErr) || !reflect.DeepEqual(executor.availabilityRequest, availabilityReq) {
		t.Fatalf("availability delegation = %#v/%v request=%#v", gotAvailability, err, executor.availabilityRequest)
	}

	createReq := booking.CreateBookingRequest{OperationKey: "operation-create", CustomerPhone: "+13125550101"}
	gotCreate, err := service.Create(ctx, "salon-1", "owner-1", createReq)
	if gotCreate != createResult || !errors.Is(err, delegateErr) || !reflect.DeepEqual(executor.createRequest, createReq) {
		t.Fatalf("create delegation = %#v/%v request=%#v", gotCreate, err, executor.createRequest)
	}

	candidatesReq := booking.RescheduleLookupRequest{CustomerPhone: "+13125550101", Limit: 4}
	gotCandidates, err := service.RescheduleCandidates(ctx, "salon-1", "owner-1", candidatesReq)
	if !reflect.DeepEqual(gotCandidates, candidatesResult) || !errors.Is(err, delegateErr) || !reflect.DeepEqual(history.rescheduleCandidatesRequest, candidatesReq) {
		t.Fatalf("candidate delegation = %#v/%v request=%#v", gotCandidates, err, history.rescheduleCandidatesRequest)
	}

	rescheduleReq := booking.RescheduleRequest{OperationKey: "operation-reschedule", StartTime: time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)}
	gotAppointment, gotFallback, err := service.Reschedule(ctx, "salon-1", "owner-1", "appointment-1", rescheduleReq)
	if gotAppointment != rescheduleAppointment || gotFallback != rescheduleFallback || !errors.Is(err, delegateErr) || executor.rescheduleAppointmentID != "appointment-1" || !reflect.DeepEqual(executor.rescheduleRequest, rescheduleReq) {
		t.Fatalf("reschedule delegation = %#v/%#v/%v appointment=%q request=%#v", gotAppointment, gotFallback, err, executor.rescheduleAppointmentID, executor.rescheduleRequest)
	}

	cancelReq := booking.CancelRequest{OperationKey: "operation-cancel", Reason: "customer request"}
	gotAppointment, gotFallback, err = service.Cancel(ctx, "salon-1", "owner-1", "appointment-2", cancelReq)
	if gotAppointment != cancelAppointment || gotFallback != cancelFallback || !errors.Is(err, delegateErr) || executor.cancelAppointmentID != "appointment-2" || !reflect.DeepEqual(executor.cancelRequest, cancelReq) {
		t.Fatalf("cancel delegation = %#v/%#v/%v appointment=%q request=%#v", gotAppointment, gotFallback, err, executor.cancelAppointmentID, executor.cancelRequest)
	}

	wantCalls := []string{"availability", "create", "reschedule", "cancel"}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("executor calls = %#v, want %#v", executor.calls, wantCalls)
	}
	if len(resolver.calls) != 2 {
		t.Fatalf("current-authority resolver calls = %d, want 2", len(resolver.calls))
	}
	for _, call := range resolver.calls {
		if call.salonID != "salon-1" || call.ownerUserID != "owner-1" {
			t.Fatalf("resolver call = %#v", call)
		}
	}
}

func TestServiceFutureMissingAndUnknownAuthoritiesFailClosedWithoutExecutorCalls(t *testing.T) {
	authorities := []string{
		booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
		"",
		"external_provider ",
		"corrupt_authority",
	}
	for _, authority := range authorities {
		t.Run(authority, func(t *testing.T) {
			executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			resolver := &fakeAuthorityResolver{authority: authority}
			service := NewService(resolver, nil, executor)
			assertNotReady := func(err error) {
				t.Helper()
				if !errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
					t.Fatalf("error = %v, want ErrSchedulingAuthorityNotReady", err)
				}
				var typed *AuthorityNotReadyError
				if !errors.As(err, &typed) || typed.Authority != authority {
					t.Fatalf("typed error = %#v, want authority %q", typed, authority)
				}
			}

			_, err := service.AvailableSlots(context.Background(), "salon-1", "owner-1", booking.AvailabilityRequest{})
			assertNotReady(err)
			_, err = service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{})
			assertNotReady(err)
			_, _, err = service.Reschedule(context.Background(), "salon-1", "owner-1", "appointment-1", booking.RescheduleRequest{})
			assertNotReady(err)
			_, _, err = service.Cancel(context.Background(), "salon-1", "owner-1", "appointment-1", booking.CancelRequest{})
			assertNotReady(err)

			if len(executor.calls) != 0 {
				t.Fatalf("executor calls = %#v, want none", executor.calls)
			}
			if len(resolver.calls) != 2 {
				t.Fatalf("current-authority resolver calls = %d, want 2", len(resolver.calls))
			}
		})
	}
}

func TestServiceResolverErrorFailsClosedWithoutExecutorCalls(t *testing.T) {
	executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	resolver := &fakeAuthorityResolver{err: pos.ErrNotFound}
	service := NewService(resolver, nil, executor)

	_, err := service.Create(context.Background(), "salon-1", "other-owner", booking.CreateBookingRequest{})
	if !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("error = %v, want pos.ErrNotFound", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %#v, want none", executor.calls)
	}
}

func TestServiceNeutralFacadePreservesRequestOnlyAndPendingOwnerReview(t *testing.T) {
	availability := &AvailabilityResult{
		Kind:                AvailabilityKindRequestOnly,
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
	}
	action := &ActionResult{
		Kind:                ActionKindPendingOwnerReview,
		OperationType:       OperationKindBook,
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		PendingOwnerReview:  &PendingOwnerReviewResult{SchedulingRequestID: "request-1", Status: string(SchedulingRequestStatusPending), Version: 1},
	}
	executor := &fakeNeutralExecutor{
		authority:          booking.SchedulingAuthorityOwnerManual,
		availabilityResult: availability,
		actionResult:       action,
	}
	resolver := &fakeAuthorityResolver{authority: booking.SchedulingAuthorityOwnerManual}
	service := NewService(resolver, nil, executor)

	gotAvailability, err := service.CheckAvailability(context.Background(), "salon-1", "owner-1", booking.AvailabilityRequest{})
	if err != nil || gotAvailability != availability || gotAvailability.VerifiedSlots != nil {
		t.Fatalf("availability = %#v/%v, want exact request_only result", gotAvailability, err)
	}
	gotAction, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{OperationType: OperationKindBook, OperationKey: "operation-1"})
	if err != nil || gotAction != action || gotAction.ConfirmedAppointment != nil || gotAction.ExternalFallbackPending != nil {
		t.Fatalf("action = %#v/%v, want exact pending_owner_review result", gotAction, err)
	}
	if executor.availabilityCalls != 1 || executor.actionCalls != 1 {
		t.Fatalf("neutral calls = availability:%d action:%d", executor.availabilityCalls, executor.actionCalls)
	}
}

func TestServiceNeutralActionReplayUsesPersistedOwnerAuthorityAfterSwitch(t *testing.T) {
	owner := &fakeNeutralExecutor{
		authority: booking.SchedulingAuthorityOwnerManual,
		actionResult: &ActionResult{
			Kind:                ActionKindPendingOwnerReview,
			OperationType:       OperationKindBook,
			SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
			PendingOwnerReview:  &PendingOwnerReviewResult{SchedulingRequestID: "request-1", Status: string(SchedulingRequestStatusPending), Version: 1},
		},
	}
	external := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	resolver := &fakeAuthorityResolver{
		authority:            booking.SchedulingAuthorityExternalProvider,
		operationAuthorities: map[string]string{"operation-owner": booking.SchedulingAuthorityOwnerManual},
	}
	service := NewService(resolver, nil, external, owner)

	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{OperationType: OperationKindBook, OperationKey: "operation-owner"})
	if err != nil || result != owner.actionResult {
		t.Fatalf("replayed action = %#v/%v", result, err)
	}
	if owner.actionCalls != 1 || len(external.calls) != 0 || len(resolver.calls) != 0 {
		t.Fatalf("dispatch = owner:%d external:%#v current:%#v", owner.actionCalls, external.calls, resolver.calls)
	}
}

func TestServiceNewActionUsesQuoteAuthorityAfterCurrentAuthoritySwitch(t *testing.T) {
	internal := &fakeNeutralExecutor{
		authority: booking.SchedulingAuthorityManleAICalendar,
		actionResult: &ActionResult{
			Kind: ActionKindConfirmedAppointment, OperationType: OperationKindBook,
			SchedulingAuthority:  booking.SchedulingAuthorityManleAICalendar,
			ConfirmedAppointment: &ConfirmedAppointmentResult{AppointmentID: "appointment-1", BookingAttemptID: "attempt-1"},
		},
	}
	external := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	resolver := &fakeAuthorityResolver{
		authority:        booking.SchedulingAuthorityExternalProvider,
		quoteAuthorities: map[string]string{"quote-1": booking.SchedulingAuthorityManleAICalendar},
	}
	service := NewService(resolver, nil, external, internal)

	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{
		OperationType: OperationKindBook, OperationKey: "operation-1", AvailabilityQuoteID: "quote-1",
	})
	if err != nil || result != internal.actionResult {
		t.Fatalf("result = %#v/%v", result, err)
	}
	if internal.actionCalls != 1 || len(external.calls) != 0 || len(resolver.calls) != 0 || !reflect.DeepEqual(resolver.quoteCalls, []string{"quote-1"}) {
		t.Fatalf("dispatch = internal:%d external:%#v current:%#v quote:%#v", internal.actionCalls, external.calls, resolver.calls, resolver.quoteCalls)
	}
}

func TestServiceExecuteActionDerivesTargetAuthorityAndRejectsSpoofedSnapshot(t *testing.T) {
	owner := &fakeNeutralExecutor{
		authority: booking.SchedulingAuthorityOwnerManual,
		actionResult: &ActionResult{
			Kind:                ActionKindPendingOwnerReview,
			OperationType:       OperationKindCancel,
			SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
			PendingOwnerReview:  &PendingOwnerReviewResult{SchedulingRequestID: "request-1", Status: string(SchedulingRequestStatusPending), Version: 1},
		},
	}
	resolver := &fakeAuthorityResolver{
		authority:              booking.SchedulingAuthorityExternalProvider,
		appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityOwnerManual},
	}
	service := NewService(resolver, nil, owner)

	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{
		OperationType:       OperationKindCancel,
		OperationKey:        "cancel-1",
		TargetAppointmentID: "appointment-1",
	})
	if err != nil || result != owner.actionResult {
		t.Fatalf("derived target result = %#v/%v", result, err)
	}
	if owner.actionRequest.TargetAuthority != booking.SchedulingAuthorityOwnerManual || !reflect.DeepEqual(resolver.appointmentCalls, []string{"appointment-1"}) {
		t.Fatalf("derived target = request:%#v calls:%#v", owner.actionRequest, resolver.appointmentCalls)
	}

	owner.actionCalls = 0
	resolver.appointmentCalls = nil
	result, err = service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{
		OperationType:       OperationKindCancel,
		OperationKey:        "cancel-spoofed",
		TargetAppointmentID: "appointment-1",
		TargetAuthority:     booking.SchedulingAuthorityExternalProvider,
	})
	if !errors.Is(err, booking.ErrOperationConflict) || result != nil {
		t.Fatalf("spoofed target result = %#v/%v", result, err)
	}
	if owner.actionCalls != 0 || !reflect.DeepEqual(resolver.appointmentCalls, []string{"appointment-1"}) {
		t.Fatalf("spoofed target dispatch = owner:%d resolver:%#v", owner.actionCalls, resolver.appointmentCalls)
	}
}

func TestAppointmentActiveChildCountUsesCurrentExternalEvidence(t *testing.T) {
	appointment := &booking.Appointment{
		Status:    booking.StatusRescheduled,
		ServiceID: "service-1",
		Segments: []booking.BookingSegmentSnapshot{
			{ServiceID: "service-1"},
			{ServiceID: "service-2"},
		},
	}
	if got := appointmentActiveChildCount(appointment); got != 2 {
		t.Fatalf("active child count = %d, want 2", got)
	}
	appointment.Segments = nil
	if got := appointmentActiveChildCount(appointment); got != 1 {
		t.Fatalf("single-service child count = %d, want 1", got)
	}
	appointment.Status = booking.StatusCancelled
	if got := appointmentActiveChildCount(appointment); got != 0 {
		t.Fatalf("cancelled active child count = %d, want 0", got)
	}
}

func TestServiceExternalLegacyDispatchRejectsUnrepresentablePartySemanticsBeforeProviderCall(t *testing.T) {
	segment := ActionSegment{ServiceID: "service-1", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1}
	requestedStart := time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*ActionRequest)
	}{
		{name: "party size greater than one", mutate: func(req *ActionRequest) { req.PartySize = 2 }},
		{name: "segment quantity greater than one", mutate: func(req *ActionRequest) { req.Segments[0].Quantity = 2 }},
		{name: "guest reference", mutate: func(req *ActionRequest) { req.Segments[0].GuestReference = "guest-1" }},
		{name: "segment requested start", mutate: func(req *ActionRequest) { req.Segments[0].RequestedStartTime = requestedStart }},
		{name: "segment requested end", mutate: func(req *ActionRequest) { req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute) }},
		{name: "partial segment start with complete top-level range", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.RequestedEndTime = requestedStart.Add(time.Hour)
			req.Segments[0].RequestedStartTime = requestedStart
		}},
		{name: "partial segment end with complete top-level range", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.RequestedEndTime = requestedStart.Add(time.Hour)
			req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute)
		}},
		{name: "segment range without top-level evidence", mutate: func(req *ActionRequest) {
			req.Segments[0].RequestedStartTime = requestedStart
			req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute)
		}},
		{name: "segment range without top-level end", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.Segments[0].RequestedStartTime = requestedStart
			req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute)
		}},
		{name: "segment range without top-level start", mutate: func(req *ActionRequest) {
			req.RequestedEndTime = requestedStart.Add(time.Hour)
			req.Segments[0].RequestedStartTime = requestedStart
			req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute)
		}},
		{name: "segment start differs from top-level start", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.RequestedEndTime = requestedStart.Add(time.Hour)
			req.Segments[0].RequestedStartTime = requestedStart.Add(15 * time.Minute)
			req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute)
		}},
		{name: "segment end exceeds top-level end", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.RequestedEndTime = requestedStart.Add(45 * time.Minute)
			req.Segments[0].RequestedStartTime = requestedStart
			req.Segments[0].RequestedEndTime = requestedStart.Add(time.Hour)
		}},
		{name: "segment end is not after start", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.RequestedEndTime = requestedStart.Add(time.Hour)
			req.Segments[0].RequestedStartTime = requestedStart
			req.Segments[0].RequestedEndTime = requestedStart
		}},
		{name: "invalid top-level range", mutate: func(req *ActionRequest) {
			req.RequestedStartTime = requestedStart
			req.RequestedEndTime = requestedStart.Add(-time.Minute)
			req.Segments[0].RequestedStartTime = requestedStart
			req.Segments[0].RequestedEndTime = requestedStart.Add(45 * time.Minute)
		}},
		{name: "negative party size", mutate: func(req *ActionRequest) { req.PartySize = -1 }},
		{name: "negative segment quantity", mutate: func(req *ActionRequest) { req.Segments[0].Quantity = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			external := &fakeExecutor{
				authority:    booking.SchedulingAuthorityExternalProvider,
				createResult: validExternalConfirmedAttempt(),
			}
			resolver := &fakeAuthorityResolver{authority: booking.SchedulingAuthorityExternalProvider}
			service := NewService(resolver, nil, external)
			req := ActionRequest{
				OperationType: OperationKindBook,
				OperationKey:  "external-party-safety",
				PartySize:     1,
				Segments:      []ActionSegment{segment},
			}
			test.mutate(&req)

			result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
			if !errors.Is(err, ErrInvalidSchedulingAction) || result != nil {
				t.Fatalf("result = %#v/%v, want fail-closed validation", result, err)
			}
			if len(external.calls) != 0 {
				t.Fatalf("external provider calls = %#v, want none", external.calls)
			}
		})
	}
}

func TestServiceExternalLegacyDispatchAcceptsConversationProducedSingleCustomerShapes(t *testing.T) {
	requestedStart := time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
	requestedEnd := requestedStart.Add(90 * time.Minute)
	conversationSegments := []ActionSegment{
		{
			ServiceID:          "service-1",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			Quantity:           1,
			RequestedStartTime: requestedStart,
			RequestedEndTime:   requestedStart.Add(45 * time.Minute),
		},
		{
			ServiceID:          "service-2",
			StaffID:            "staff-1",
			StaffSelectionMode: booking.StaffSelectionSpecific,
			Quantity:           1,
			RequestedStartTime: requestedStart,
			RequestedEndTime:   requestedEnd,
		},
	}
	tests := []struct {
		name         string
		operation    OperationKind
		wantCall     string
		configure    func(*fakeExecutor)
		assertLegacy func(*testing.T, *fakeExecutor)
	}{
		{
			name:      "book",
			operation: OperationKindBook,
			wantCall:  "create",
			configure: func(external *fakeExecutor) { external.createResult = validExternalConfirmedAttempt() },
			assertLegacy: func(t *testing.T, external *fakeExecutor) {
				t.Helper()
				if !external.createRequest.StartTime.Equal(requestedStart) || len(external.createRequest.Segments) != 2 {
					t.Fatalf("book conversion = %#v", external.createRequest)
				}
			},
		},
		{
			name:      "reschedule",
			operation: OperationKindReschedule,
			wantCall:  "reschedule",
			configure: func(external *fakeExecutor) {
				external.rescheduleAppointment = validExternalAppointment(booking.StatusRescheduled)
			},
			assertLegacy: func(t *testing.T, external *fakeExecutor) {
				t.Helper()
				if external.rescheduleAppointmentID != "appointment-1" || !external.rescheduleRequest.StartTime.Equal(requestedStart) {
					t.Fatalf("reschedule conversion = appointment:%q request:%#v", external.rescheduleAppointmentID, external.rescheduleRequest)
				}
			},
		},
		{
			name:      "cancel",
			operation: OperationKindCancel,
			wantCall:  "cancel",
			configure: func(external *fakeExecutor) {
				external.cancelAppointment = validExternalAppointment(booking.StatusCancelled)
			},
			assertLegacy: func(t *testing.T, external *fakeExecutor) {
				t.Helper()
				if external.cancelAppointmentID != "appointment-1" || external.cancelRequest.Reason != "Customer requested the change." {
					t.Fatalf("cancel conversion = appointment:%q request:%#v", external.cancelAppointmentID, external.cancelRequest)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			external := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			test.configure(external)
			resolver := &fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityExternalProvider,
				appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityExternalProvider},
			}
			service := NewService(resolver, nil, external)
			req := ActionRequest{
				OperationType:      test.operation,
				OperationKey:       "conversation-" + string(test.operation),
				PartySize:          1,
				Segments:           append([]ActionSegment(nil), conversationSegments...),
				RequestedStartTime: requestedStart,
				RequestedEndTime:   requestedEnd,
				Notes:              "Customer requested the change.",
			}
			if test.operation != OperationKindBook {
				req.TargetAppointmentID = "appointment-1"
			}

			result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
			if err != nil || result == nil || result.Kind != ActionKindConfirmedAppointment {
				t.Fatalf("conversation %s result = %#v/%v", test.operation, result, err)
			}
			if !reflect.DeepEqual(external.calls, []string{test.wantCall}) {
				t.Fatalf("external calls = %#v, want %q", external.calls, test.wantCall)
			}
			test.assertLegacy(t, external)
		})
	}
}

func TestServiceExternalLegacyMutationAcceptsHistoricalStartOnlySegmentSnapshot(t *testing.T) {
	requestedStart := time.Date(2026, time.August, 15, 16, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		operation  OperationKind
		wantCall   string
		segmentEnd time.Duration
		configure  func(*fakeExecutor)
	}{
		{
			name:      "reschedule archived service absent from current catalog",
			operation: OperationKindReschedule,
			wantCall:  "reschedule",
			configure: func(external *fakeExecutor) {
				external.rescheduleAppointment = validExternalAppointment(booking.StatusRescheduled)
			},
		},
		{
			name:      "cancel archived service absent from current catalog",
			operation: OperationKindCancel,
			wantCall:  "cancel",
			configure: func(external *fakeExecutor) {
				external.cancelAppointment = validExternalAppointment(booking.StatusCancelled)
			},
		},
		{
			name:       "reschedule paired segment without top-level end",
			operation:  OperationKindReschedule,
			wantCall:   "reschedule",
			segmentEnd: 45 * time.Minute,
			configure: func(external *fakeExecutor) {
				external.rescheduleAppointment = validExternalAppointment(booking.StatusRescheduled)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			external := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			test.configure(external)
			service := NewService(&fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityExternalProvider,
				appointmentAuthorities: map[string]string{"appointment-historical": booking.SchedulingAuthorityExternalProvider},
			}, nil, external)
			segment := ActionSegment{
				ServiceID:          "service-inactive-archived-not-in-guidance",
				StaffSelectionMode: booking.StaffSelectionAnyone,
				Quantity:           1,
				RequestedStartTime: requestedStart,
			}
			if test.segmentEnd > 0 {
				segment.RequestedEndTime = requestedStart.Add(test.segmentEnd)
			}
			req := ActionRequest{
				OperationType:       test.operation,
				OperationKey:        "historical-" + string(test.operation),
				TargetAppointmentID: "appointment-historical",
				PartySize:           1,
				RequestedStartTime:  requestedStart,
				Segments:            []ActionSegment{segment},
			}

			result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
			if err != nil || result == nil || result.Kind != ActionKindConfirmedAppointment {
				t.Fatalf("historical %s result = %#v/%v", test.operation, result, err)
			}
			if !reflect.DeepEqual(external.calls, []string{test.wantCall}) {
				t.Fatalf("external calls = %#v, want exactly %q", external.calls, test.wantCall)
			}
		})
	}
}

func TestServiceExternalLegacyMutationRejectsDivergentHistoricalTimingAndPartySemantics(t *testing.T) {
	requestedStart := time.Date(2026, time.August, 15, 16, 30, 0, 0, time.UTC)
	base := ActionRequest{
		OperationType:       OperationKindReschedule,
		OperationKey:        "historical-reschedule-invalid",
		TargetAppointmentID: "appointment-historical",
		PartySize:           1,
		RequestedStartTime:  requestedStart,
		RequestedEndTime:    requestedStart.Add(time.Hour),
		Segments: []ActionSegment{{
			ServiceID:          "service-inactive-archived-not-in-guidance",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			Quantity:           1,
			RequestedStartTime: requestedStart,
		}},
	}
	tests := []struct {
		name   string
		mutate func(*ActionRequest)
	}{
		{name: "lossy party", mutate: func(req *ActionRequest) { req.PartySize = 2 }},
		{name: "missing persisted target", mutate: func(req *ActionRequest) { req.TargetAppointmentID = "" }},
		{name: "missing top-level start", mutate: func(req *ActionRequest) { req.RequestedStartTime = time.Time{} }},
		{name: "shifted segment start", mutate: func(req *ActionRequest) { req.Segments[0].RequestedStartTime = requestedStart.Add(15 * time.Minute) }},
		{name: "end without start", mutate: func(req *ActionRequest) {
			req.Segments[0].RequestedStartTime = time.Time{}
			req.Segments[0].RequestedEndTime = requestedStart.Add(30 * time.Minute)
		}},
		{name: "invalid paired end", mutate: func(req *ActionRequest) { req.Segments[0].RequestedEndTime = requestedStart }},
		{name: "paired end exceeds top-level end", mutate: func(req *ActionRequest) { req.Segments[0].RequestedEndTime = requestedStart.Add(90 * time.Minute) }},
		{name: "invalid optional top-level end", mutate: func(req *ActionRequest) { req.RequestedEndTime = requestedStart.Add(-time.Minute) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			external := &fakeExecutor{
				authority:             booking.SchedulingAuthorityExternalProvider,
				rescheduleAppointment: validExternalAppointment(booking.StatusRescheduled),
			}
			service := NewService(&fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityExternalProvider,
				appointmentAuthorities: map[string]string{"appointment-historical": booking.SchedulingAuthorityExternalProvider},
			}, nil, external)
			req := base
			req.Segments = append([]ActionSegment(nil), base.Segments...)
			test.mutate(&req)

			result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
			if !errors.Is(err, ErrInvalidSchedulingAction) || result != nil {
				t.Fatalf("result = %#v/%v, want fail-closed validation", result, err)
			}
			if len(external.calls) != 0 {
				t.Fatalf("external calls = %#v, want zero", external.calls)
			}
		})
	}
}

func TestServiceOperationResolutionErrorNeverFallsBackOrDispatches(t *testing.T) {
	operationErr := errors.New("operation origin query failed")
	resolver := &fakeAuthorityResolver{
		authority:    booking.SchedulingAuthorityExternalProvider,
		operationErr: operationErr,
	}
	external := &fakeExecutor{
		authority:    booking.SchedulingAuthorityExternalProvider,
		createResult: validExternalConfirmedAttempt(),
	}
	service := NewService(resolver, nil, external)
	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", ActionRequest{
		OperationType: OperationKindBook,
		OperationKey:  "operation-query-error",
		PartySize:     1,
		Segments:      []ActionSegment{{ServiceID: "service-1", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1}},
	})
	if !errors.Is(err, operationErr) || result != nil {
		t.Fatalf("operation query error result = %#v/%v", result, err)
	}
	if len(resolver.calls) != 0 || len(external.calls) != 0 {
		t.Fatalf("error path called current authority/executor: current=%#v external=%#v", resolver.calls, external.calls)
	}
}

func TestServiceExternalLegacyDispatchPreservesRepresentableSingleCustomerMultiServiceBooking(t *testing.T) {
	external := &fakeExecutor{
		authority:    booking.SchedulingAuthorityExternalProvider,
		createResult: validExternalConfirmedAttempt(),
	}
	resolver := &fakeAuthorityResolver{authority: booking.SchedulingAuthorityExternalProvider}
	service := NewService(resolver, nil, external)
	req := ActionRequest{
		OperationType: OperationKindBook,
		OperationKey:  "external-single-customer",
		PartySize:     1,
		Segments: []ActionSegment{
			{ServiceID: "service-1", StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1},
			{ServiceID: "service-2", StaffID: "staff-1", StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1},
		},
	}

	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
	if err != nil || result == nil || result.Kind != ActionKindConfirmedAppointment {
		t.Fatalf("representable action = %#v/%v", result, err)
	}
	if !reflect.DeepEqual(external.calls, []string{"create"}) || len(external.createRequest.Segments) != 2 ||
		external.createRequest.Segments[0].ServiceID != "service-1" || external.createRequest.Segments[1].ServiceID != "service-2" {
		t.Fatalf("external conversion = calls:%#v request:%#v", external.calls, external.createRequest)
	}
}

func TestServiceOwnerManualNeutralDispatchRetainsPartySemantics(t *testing.T) {
	owner := &fakeNeutralExecutor{
		authority: booking.SchedulingAuthorityOwnerManual,
		actionResult: &ActionResult{
			Kind:                ActionKindPendingOwnerReview,
			OperationType:       OperationKindBook,
			SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
			PendingOwnerReview:  &PendingOwnerReviewResult{SchedulingRequestID: "request-party", Status: string(SchedulingRequestStatusPending), Version: 1},
		},
	}
	service := NewService(&fakeAuthorityResolver{authority: booking.SchedulingAuthorityOwnerManual}, nil, owner)
	requestedStart := time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
	req := ActionRequest{
		OperationType: OperationKindBook,
		OperationKey:  "owner-party",
		PartySize:     3,
		Segments: []ActionSegment{{
			ServiceID:          "service-1",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			GuestReference:     "guest-1",
			Quantity:           2,
			RequestedStartTime: requestedStart,
			RequestedEndTime:   requestedStart.Add(45 * time.Minute),
		}},
	}

	result, err := service.ExecuteAction(context.Background(), "salon-1", "owner-1", req)
	if err != nil || result != owner.actionResult || owner.actionCalls != 1 {
		t.Fatalf("owner party action = %#v/%v calls=%d", result, err, owner.actionCalls)
	}
	if !reflect.DeepEqual(owner.actionRequest, req) {
		t.Fatalf("owner request lost party semantics: got %#v want %#v", owner.actionRequest, req)
	}
}

func TestExternalActionResultRequiresCompleteProviderConfirmationEvidence(t *testing.T) {
	valid := validExternalConfirmedAttempt()
	result, err := actionResultFromExternalAttempt(OperationKindBook, valid)
	if err != nil || result.Kind != ActionKindConfirmedAppointment {
		t.Fatalf("valid result = %#v/%v", result, err)
	}

	tests := []struct {
		name   string
		mutate func(*booking.BookingAttempt)
	}{
		{name: "missing attempt id", mutate: func(item *booking.BookingAttempt) { item.ID = "" }},
		{name: "missing provider booking id", mutate: func(item *booking.BookingAttempt) { item.POSBookingID = "" }},
		{name: "missing authority", mutate: func(item *booking.BookingAttempt) { item.SchedulingAuthority = "" }},
		{name: "missing appointment", mutate: func(item *booking.BookingAttempt) { item.Appointment = nil }},
		{name: "appointment not confirmed", mutate: func(item *booking.BookingAttempt) { item.Appointment.Status = booking.StatusProviderPending }},
		{name: "provider identity mismatch", mutate: func(item *booking.BookingAttempt) { item.Appointment.POSAppointmentID = "other-id" }},
		{name: "version mismatch", mutate: func(item *booking.BookingAttempt) { item.Appointment.POSAppointmentVersion++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempt := validExternalConfirmedAttempt()
			test.mutate(attempt)
			if result, err := actionResultFromExternalAttempt(OperationKindBook, attempt); !errors.Is(err, ErrInvalidSchedulingResult) || result != nil {
				t.Fatalf("result = %#v/%v, want invalid scheduling result", result, err)
			}
		})
	}
}

func TestExternalMutationNeverUpgradesMalformedAppointmentOrFallback(t *testing.T) {
	appointment := validExternalAppointment(booking.StatusRescheduled)
	result, err := actionResultFromExternalMutation(OperationKindReschedule, appointment, nil)
	if err != nil || result.Kind != ActionKindConfirmedAppointment {
		t.Fatalf("valid mutation result = %#v/%v", result, err)
	}
	appointment.POSAppointmentID = ""
	if result, err := actionResultFromExternalMutation(OperationKindReschedule, appointment, nil); !errors.Is(err, ErrInvalidSchedulingResult) || result != nil {
		t.Fatalf("malformed appointment result = %#v/%v", result, err)
	}
	malformedFallback := &booking.BookingAttempt{ID: "attempt-fallback", SchedulingAuthority: booking.SchedulingAuthorityExternalProvider, Status: booking.StatusConfirmed}
	if result, err := actionResultFromExternalMutation(OperationKindReschedule, nil, malformedFallback); !errors.Is(err, ErrInvalidSchedulingResult) || result != nil {
		t.Fatalf("malformed fallback result = %#v/%v", result, err)
	}
	validFallback := &booking.BookingAttempt{ID: "attempt-fallback", SchedulingAuthority: booking.SchedulingAuthorityExternalProvider, Status: booking.StatusFallbackPending}
	result, err = actionResultFromExternalMutation(OperationKindReschedule, nil, validFallback)
	if err != nil || result.Kind != ActionKindExternalFallbackPending {
		t.Fatalf("valid fallback result = %#v/%v", result, err)
	}
}

func validExternalConfirmedAttempt() *booking.BookingAttempt {
	appointment := validExternalAppointment(booking.StatusConfirmed)
	return &booking.BookingAttempt{
		ID:                          "attempt-1",
		Status:                      booking.StatusConfirmed,
		SchedulingAuthority:         booking.SchedulingAuthorityExternalProvider,
		AuthorityProvider:           "square",
		AuthorityAppointmentID:      "provider-appointment-1",
		AuthorityAppointmentVersion: 7,
		POSProvider:                 "square",
		POSBookingID:                "provider-appointment-1",
		POSBookingVersion:           7,
		Appointment:                 appointment,
	}
}

func validExternalAppointment(status string) *booking.Appointment {
	return &booking.Appointment{
		ID:                          "appointment-1",
		Status:                      status,
		SchedulingAuthority:         booking.SchedulingAuthorityExternalProvider,
		AuthorityProvider:           "square",
		AuthorityAppointmentID:      "provider-appointment-1",
		AuthorityAppointmentVersion: 7,
		POSProvider:                 "square",
		POSAppointmentID:            "provider-appointment-1",
		POSAppointmentVersion:       7,
	}
}

func TestServiceUsesPersistedOriginsAfterCurrentAuthoritySwitch(t *testing.T) {
	tests := []struct {
		name            string
		resolver        *fakeAuthorityResolver
		invoke          func(*Service) error
		wantCall        string
		wantOperation   []string
		wantAttempt     []string
		wantAppointment []string
	}{
		{
			name: "create operation replay",
			resolver: &fakeAuthorityResolver{
				authority:            booking.SchedulingAuthorityOwnerManual,
				operationAuthorities: map[string]string{"operation-replay": booking.SchedulingAuthorityExternalProvider},
			},
			invoke: func(service *Service) error {
				_, err := service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{OperationKey: "operation-replay"})
				return err
			},
			wantCall:      "create",
			wantOperation: []string{"operation-replay"},
		},
		{
			name: "create retry lineage",
			resolver: &fakeAuthorityResolver{
				authority:          booking.SchedulingAuthorityOwnerManual,
				attemptAuthorities: map[string]string{"attempt-original": booking.SchedulingAuthorityExternalProvider},
			},
			invoke: func(service *Service) error {
				_, err := service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{OperationKey: "operation-retry", RetryOfAttemptID: "attempt-original"})
				return err
			},
			wantCall:      "create",
			wantOperation: []string{"operation-retry"},
			wantAttempt:   []string{"attempt-original"},
		},
		{
			name: "reschedule target origin",
			resolver: &fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityOwnerManual,
				appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityExternalProvider},
			},
			invoke: func(service *Service) error {
				_, _, err := service.Reschedule(context.Background(), "salon-1", "owner-1", "appointment-1", booking.RescheduleRequest{OperationKey: "operation-reschedule"})
				return err
			},
			wantCall:        "reschedule",
			wantOperation:   []string{"operation-reschedule"},
			wantAppointment: []string{"appointment-1"},
		},
		{
			name: "cancel operation replay agrees with target",
			resolver: &fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityOwnerManual,
				operationAuthorities:   map[string]string{"operation-cancel": booking.SchedulingAuthorityExternalProvider},
				appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityExternalProvider},
			},
			invoke: func(service *Service) error {
				_, _, err := service.Cancel(context.Background(), "salon-1", "owner-1", "appointment-1", booking.CancelRequest{OperationKey: "operation-cancel"})
				return err
			},
			wantCall:        "cancel",
			wantOperation:   []string{"operation-cancel"},
			wantAppointment: []string{"appointment-1"},
		},
		{
			name: "reschedule availability target origin",
			resolver: &fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityOwnerManual,
				appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityExternalProvider},
			},
			invoke: func(service *Service) error {
				_, err := service.AvailableSlots(context.Background(), "salon-1", "owner-1", booking.AvailabilityRequest{TargetAppointmentID: "appointment-1"})
				return err
			},
			wantCall:        "availability",
			wantAppointment: []string{"appointment-1"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			service := NewService(test.resolver, nil, executor)
			if err := test.invoke(service); err != nil {
				t.Fatalf("invoke returned error: %v", err)
			}
			if !reflect.DeepEqual(executor.calls, []string{test.wantCall}) {
				t.Fatalf("executor calls = %#v, want %q", executor.calls, test.wantCall)
			}
			if len(test.resolver.calls) != 0 {
				t.Fatalf("current authority was consulted: %#v", test.resolver.calls)
			}
			if !reflect.DeepEqual(test.resolver.operationCalls, test.wantOperation) ||
				!reflect.DeepEqual(test.resolver.attemptCalls, test.wantAttempt) ||
				!reflect.DeepEqual(test.resolver.appointmentCalls, test.wantAppointment) {
				t.Fatalf("origin calls = operation:%#v attempt:%#v appointment:%#v", test.resolver.operationCalls, test.resolver.attemptCalls, test.resolver.appointmentCalls)
			}
		})
	}
}

func TestServiceRetryAvailabilityUsesSafePersistedOriginAfterAuthoritySwitch(t *testing.T) {
	resolver := &fakeAuthorityResolver{
		authority:          booking.SchedulingAuthorityOwnerManual,
		attemptAuthorities: map[string]string{"attempt-safe": booking.SchedulingAuthorityExternalProvider},
	}
	external := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	service := NewService(resolver, nil, external)
	req := booking.AvailabilityRequest{RetryOfAttemptID: "attempt-safe"}

	if _, err := service.AvailableSlots(context.Background(), "salon-1", "owner-1", req); err != nil {
		t.Fatalf("retry availability: %v", err)
	}
	if !reflect.DeepEqual(external.calls, []string{"availability"}) || external.availabilityRequest.RetryOfAttemptID != "attempt-safe" {
		t.Fatalf("external availability calls/request = %#v/%#v", external.calls, external.availabilityRequest)
	}
	if len(resolver.calls) != 0 || !reflect.DeepEqual(resolver.availabilityRetryCalls, []string{"attempt-safe"}) {
		t.Fatalf("resolver current/retry calls = %#v/%#v", resolver.calls, resolver.availabilityRetryCalls)
	}
}

func TestServiceRetryAvailabilityRejectsUnsafeOriginAndMixedTarget(t *testing.T) {
	tests := []booking.AvailabilityRequest{
		{RetryOfAttemptID: "attempt-unsafe"},
		{RetryOfAttemptID: "attempt-safe", TargetAppointmentID: "appointment-1"},
	}
	for _, req := range tests {
		resolver := &fakeAuthorityResolver{authority: booking.SchedulingAuthorityOwnerManual}
		external := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
		service := NewService(resolver, nil, external)
		_, err := service.AvailableSlots(context.Background(), "salon-1", "owner-1", req)
		if req.TargetAppointmentID != "" {
			if !errors.Is(err, booking.ErrValidation) {
				t.Fatalf("mixed target/retry error = %v, want validation", err)
			}
		} else if !errors.Is(err, booking.ErrOperationConflict) {
			t.Fatalf("unsafe retry error = %v, want operation conflict", err)
		}
		if len(external.calls) != 0 || len(resolver.calls) != 0 {
			t.Fatalf("rejected request called executor/current resolver: %#v/%#v", external.calls, resolver.calls)
		}
	}
}

func TestServiceRejectsConflictingOperationAndRetryOriginsWithoutExecutorCalls(t *testing.T) {
	resolver := &fakeAuthorityResolver{
		authority:            booking.SchedulingAuthorityOwnerManual,
		operationAuthorities: map[string]string{"operation-1": booking.SchedulingAuthorityExternalProvider},
		attemptAuthorities:   map[string]string{"attempt-1": booking.SchedulingAuthorityOwnerManual},
	}
	executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	service := NewService(resolver, nil, executor)

	_, err := service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{OperationKey: "operation-1", RetryOfAttemptID: "attempt-1"})
	if !errors.Is(err, booking.ErrOperationConflict) {
		t.Fatalf("error = %v, want ErrOperationConflict", err)
	}
	if len(executor.calls) != 0 || len(resolver.calls) != 0 {
		t.Fatalf("conflicting origin called executor/current resolver: executor=%#v resolver=%#v", executor.calls, resolver.calls)
	}
}

func TestServiceRejectsConflictingOperationOrRetryAndTargetOriginsWithoutExecutorCalls(t *testing.T) {
	tests := []struct {
		name     string
		resolver *fakeAuthorityResolver
		invoke   func(*Service) error
	}{
		{
			name: "operation target mismatch",
			resolver: &fakeAuthorityResolver{
				operationAuthorities:   map[string]string{"operation-1": booking.SchedulingAuthorityExternalProvider},
				appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityOwnerManual},
			},
			invoke: func(service *Service) error {
				_, _, err := service.Cancel(context.Background(), "salon-1", "owner-1", "appointment-1", booking.CancelRequest{OperationKey: "operation-1"})
				return err
			},
		},
		{
			name: "retry target mismatch",
			resolver: &fakeAuthorityResolver{
				attemptAuthorities:     map[string]string{"attempt-1": booking.SchedulingAuthorityExternalProvider},
				appointmentAuthorities: map[string]string{"appointment-1": booking.SchedulingAuthorityManleAICalendar},
			},
			invoke: func(service *Service) error {
				_, _, err := service.Reschedule(context.Background(), "salon-1", "owner-1", "appointment-1", booking.RescheduleRequest{OperationKey: "operation-1", RetryOfAttemptID: "attempt-1"})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			service := NewService(test.resolver, nil, executor)
			if err := test.invoke(service); !errors.Is(err, booking.ErrOperationConflict) {
				t.Fatalf("error = %v, want ErrOperationConflict", err)
			}
			if len(executor.calls) != 0 || len(test.resolver.calls) != 0 {
				t.Fatalf("mismatched target called executor/current resolver: executor=%#v resolver=%#v", executor.calls, test.resolver.calls)
			}
			if len(test.resolver.appointmentCalls) != 1 {
				t.Fatalf("target authority calls = %#v, want one", test.resolver.appointmentCalls)
			}
		})
	}
}

func TestServiceFailsClosedForCrossTenantAndCorruptPersistedOrigins(t *testing.T) {
	tests := []struct {
		name     string
		resolver *fakeAuthorityResolver
		invoke   func(*Service) error
		wantErr  error
	}{
		{
			name:     "cross tenant retry",
			resolver: &fakeAuthorityResolver{authority: booking.SchedulingAuthorityExternalProvider, attemptErr: pos.ErrNotFound},
			invoke: func(service *Service) error {
				_, err := service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{OperationKey: "operation-1", RetryOfAttemptID: "other-tenant-attempt"})
				return err
			},
			wantErr: pos.ErrNotFound,
		},
		{
			name:     "cross tenant target",
			resolver: &fakeAuthorityResolver{authority: booking.SchedulingAuthorityExternalProvider, appointmentErr: pos.ErrNotFound},
			invoke: func(service *Service) error {
				_, _, err := service.Cancel(context.Background(), "salon-1", "owner-1", "other-tenant-appointment", booking.CancelRequest{OperationKey: "operation-1"})
				return err
			},
			wantErr: pos.ErrNotFound,
		},
		{
			name: "corrupt operation authority",
			resolver: &fakeAuthorityResolver{
				authority:            booking.SchedulingAuthorityExternalProvider,
				operationAuthorities: map[string]string{"operation-1": "corrupt"},
			},
			invoke: func(service *Service) error {
				_, err := service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{OperationKey: "operation-1"})
				return err
			},
			wantErr: booking.ErrSchedulingAuthorityNotReady,
		},
		{
			name: "missing operation authority",
			resolver: &fakeAuthorityResolver{
				authority:            booking.SchedulingAuthorityExternalProvider,
				operationAuthorities: map[string]string{"operation-1": ""},
			},
			invoke: func(service *Service) error {
				_, err := service.Create(context.Background(), "salon-1", "owner-1", booking.CreateBookingRequest{OperationKey: "operation-1"})
				return err
			},
			wantErr: booking.ErrSchedulingAuthorityNotReady,
		},
		{
			name: "corrupt appointment authority",
			resolver: &fakeAuthorityResolver{
				authority:              booking.SchedulingAuthorityExternalProvider,
				appointmentAuthorities: map[string]string{"appointment-1": "corrupt"},
			},
			invoke: func(service *Service) error {
				_, err := service.AvailableSlots(context.Background(), "salon-1", "owner-1", booking.AvailabilityRequest{TargetAppointmentID: "appointment-1"})
				return err
			},
			wantErr: booking.ErrSchedulingAuthorityNotReady,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
			service := NewService(test.resolver, nil, executor)
			err := test.invoke(service)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if len(executor.calls) != 0 || len(test.resolver.calls) != 0 {
				t.Fatalf("failed origin called executor/current resolver: executor=%#v resolver=%#v", executor.calls, test.resolver.calls)
			}
		})
	}
}

type fakeHistoryService struct {
	calls []string
	err   error

	appointments                *booking.ListAppointmentsResponse
	rescheduleCandidates        []booking.AppointmentActionRef
	rescheduleCandidatesRequest booking.RescheduleLookupRequest
	attempts                    *booking.ListBookingAttemptsResponse
	reconciliationTasks         *booking.ListReconciliationTasksResponse
	reconciliationCandidates    *booking.ListReconciliationCandidatesResponse
	reconciliationTask          *booking.ReconciliationTask
	testBooking                 *booking.TestBookingRecord
	calendar                    *booking.CalendarRangeResponse
	calendarEvents              []booking.CalendarEvent
	calendarSync                *booking.CalendarSyncResponse
	replayCreateAttempt         *booking.BookingAttempt
	replayCreateFound           bool
	replayCancelAppointment     *booking.Appointment
	replayCancelFallback        *booking.BookingAttempt
	replayCancelFound           bool
}

func (f *fakeHistoryService) ReplayCreate(context.Context, string, string, booking.CreateBookingRequest) (*booking.BookingAttempt, bool, error) {
	f.calls = append(f.calls, "replay_create")
	return f.replayCreateAttempt, f.replayCreateFound, f.err
}

func (f *fakeHistoryService) ReplayCancel(context.Context, string, string, string, booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, bool, error) {
	f.calls = append(f.calls, "replay_cancel")
	return f.replayCancelAppointment, f.replayCancelFallback, f.replayCancelFound, f.err
}

func (f *fakeHistoryService) RescheduleCandidates(_ context.Context, _ string, _ string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	f.calls = append(f.calls, "reschedule_candidates")
	f.rescheduleCandidatesRequest = req
	return f.rescheduleCandidates, f.err
}

func (f *fakeHistoryService) Appointments(context.Context, string, string, int, int) (*booking.ListAppointmentsResponse, error) {
	f.calls = append(f.calls, "appointments")
	return f.appointments, f.err
}

func (f *fakeHistoryService) Attempts(context.Context, string, string, string, int, int) (*booking.ListBookingAttemptsResponse, error) {
	f.calls = append(f.calls, "attempts")
	return f.attempts, f.err
}

func (f *fakeHistoryService) ReconciliationTasks(context.Context, string, string, string, int, int) (*booking.ListReconciliationTasksResponse, error) {
	f.calls = append(f.calls, "reconciliation_tasks")
	return f.reconciliationTasks, f.err
}

func (f *fakeHistoryService) ReconciliationCandidates(context.Context, string, string, string) (*booking.ListReconciliationCandidatesResponse, error) {
	f.calls = append(f.calls, "reconciliation_candidates")
	return f.reconciliationCandidates, f.err
}

func (f *fakeHistoryService) ResolveReconciliation(context.Context, string, string, string, booking.ResolveReconciliationRequest) (*booking.ReconciliationTask, error) {
	f.calls = append(f.calls, "resolve_reconciliation")
	return f.reconciliationTask, f.err
}

func (f *fakeHistoryService) LatestTestBooking(context.Context, string, string) (*booking.TestBookingRecord, error) {
	f.calls = append(f.calls, "latest_test_booking")
	return f.testBooking, f.err
}

func (f *fakeHistoryService) Calendar(context.Context, string, string, booking.CalendarRangeRequest) (*booking.CalendarRangeResponse, error) {
	f.calls = append(f.calls, "calendar")
	return f.calendar, f.err
}

func (f *fakeHistoryService) EnsureCalendarEventAccess(context.Context, string, string) error {
	f.calls = append(f.calls, "calendar_access")
	return f.err
}

func (f *fakeHistoryService) CalendarEvents(context.Context, string, string, booking.CalendarEventCursor, int) ([]booking.CalendarEvent, error) {
	f.calls = append(f.calls, "calendar_events")
	return f.calendarEvents, f.err
}

func (f *fakeHistoryService) SyncCalendar(context.Context, string, string, booking.CalendarSyncRequest) (*booking.CalendarSyncResponse, error) {
	f.calls = append(f.calls, "calendar_sync")
	return f.calendarSync, f.err
}

func TestServiceHistoryAndProviderSyncDelegateWithoutAuthorityReclassification(t *testing.T) {
	delegateErr := errors.New("history evidence")
	history := &fakeHistoryService{
		err:                      delegateErr,
		appointments:             &booking.ListAppointmentsResponse{Limit: 1},
		rescheduleCandidates:     []booking.AppointmentActionRef{{ID: "candidate-1"}},
		attempts:                 &booking.ListBookingAttemptsResponse{Limit: 2},
		reconciliationTasks:      &booking.ListReconciliationTasksResponse{Limit: 3},
		reconciliationCandidates: &booking.ListReconciliationCandidatesResponse{},
		reconciliationTask:       &booking.ReconciliationTask{ID: "task-1"},
		testBooking:              &booking.TestBookingRecord{BookingAttemptID: "attempt-1"},
		calendar:                 &booking.CalendarRangeResponse{SalonID: "salon-1"},
		calendarEvents:           []booking.CalendarEvent{{ID: "event-1"}},
		calendarSync:             &booking.CalendarSyncResponse{},
		replayCreateAttempt:      &booking.BookingAttempt{ID: "replay-create"},
		replayCreateFound:        true,
		replayCancelAppointment:  &booking.Appointment{ID: "replay-cancel"},
		replayCancelFallback:     &booking.BookingAttempt{ID: "replay-cancel-attempt"},
		replayCancelFound:        true,
	}
	resolver := &fakeAuthorityResolver{authority: booking.SchedulingAuthorityOwnerManual}
	executor := &fakeExecutor{authority: booking.SchedulingAuthorityExternalProvider}
	service := NewService(resolver, history, executor)
	ctx := context.Background()
	replayedCreate, found, err := service.ReplayCreate(ctx, "salon-1", "owner-1", booking.CreateBookingRequest{})
	if replayedCreate != history.replayCreateAttempt || !found || !errors.Is(err, delegateErr) {
		t.Fatalf("replay create = %#v/%t/%v", replayedCreate, found, err)
	}
	replayedCancel, replayedFallback, found, err := service.ReplayCancel(ctx, "salon-1", "owner-1", "appointment-1", booking.CancelRequest{})
	if replayedCancel != history.replayCancelAppointment || replayedFallback != history.replayCancelFallback || !found || !errors.Is(err, delegateErr) {
		t.Fatalf("replay cancel = %#v/%#v/%t/%v", replayedCancel, replayedFallback, found, err)
	}
	rescheduleCandidates, err := service.RescheduleCandidates(ctx, "salon-1", "owner-1", booking.RescheduleLookupRequest{})
	assertExactDelegation(t, reflect.DeepEqual(rescheduleCandidates, history.rescheduleCandidates), err, delegateErr, "reschedule candidates")

	appointments, err := service.Appointments(ctx, "salon-1", "owner-1", 1, 2)
	assertExactDelegation(t, appointments == history.appointments, err, delegateErr, "appointments")
	attempts, err := service.Attempts(ctx, "salon-1", "owner-1", "confirmed", 1, 2)
	assertExactDelegation(t, attempts == history.attempts, err, delegateErr, "attempts")
	tasks, err := service.ReconciliationTasks(ctx, "salon-1", "owner-1", "open", 1, 2)
	assertExactDelegation(t, tasks == history.reconciliationTasks, err, delegateErr, "reconciliation tasks")
	candidates, err := service.ReconciliationCandidates(ctx, "salon-1", "owner-1", "attempt-1")
	assertExactDelegation(t, candidates == history.reconciliationCandidates, err, delegateErr, "reconciliation candidates")
	task, err := service.ResolveReconciliation(ctx, "salon-1", "owner-1", "attempt-1", booking.ResolveReconciliationRequest{})
	assertExactDelegation(t, task == history.reconciliationTask, err, delegateErr, "resolve reconciliation")
	testBooking, err := service.LatestTestBooking(ctx, "salon-1", "owner-1")
	assertExactDelegation(t, testBooking == history.testBooking, err, delegateErr, "latest test booking")
	calendar, err := service.Calendar(ctx, "salon-1", "owner-1", booking.CalendarRangeRequest{})
	assertExactDelegation(t, calendar == history.calendar, err, delegateErr, "calendar")
	if err := service.EnsureCalendarEventAccess(ctx, "salon-1", "owner-1"); !errors.Is(err, delegateErr) {
		t.Fatalf("calendar access error = %v", err)
	}
	events, err := service.CalendarEvents(ctx, "salon-1", "owner-1", booking.CalendarEventCursor{}, 20)
	assertExactDelegation(t, reflect.DeepEqual(events, history.calendarEvents), err, delegateErr, "calendar events")
	syncResult, err := service.SyncCalendar(ctx, "salon-1", "owner-1", booking.CalendarSyncRequest{})
	assertExactDelegation(t, syncResult == history.calendarSync, err, delegateErr, "calendar sync")

	wantCalls := []string{"replay_create", "replay_cancel", "reschedule_candidates", "appointments", "attempts", "reconciliation_tasks", "reconciliation_candidates", "resolve_reconciliation", "latest_test_booking", "calendar", "calendar_access", "calendar_events", "calendar_sync"}
	if !reflect.DeepEqual(history.calls, wantCalls) {
		t.Fatalf("history calls = %#v, want %#v", history.calls, wantCalls)
	}
	if len(resolver.calls) != 0 || len(executor.calls) != 0 {
		t.Fatalf("read path resolved/mutated authority: resolver=%#v executor=%#v", resolver.calls, executor.calls)
	}
}

func assertExactDelegation(t *testing.T, sameResult bool, gotErr error, wantErr error, operation string) {
	t.Helper()
	if !sameResult || !errors.Is(gotErr, wantErr) {
		t.Fatalf("%s delegation result=%t error=%v", operation, sameResult, gotErr)
	}
}
