package scheduling

import (
	"context"
	"fmt"
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type AuthorityResolver interface {
	ResolveSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string) (string, error)
	ResolveConversationSchedulingPolicy(ctx context.Context, salonID string, ownerUserID string) (ConversationPolicyFence, error)
	ResolveAvailabilityQuoteSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, quoteID string) (string, error)
	FindOperationSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, operationKey string) (string, bool, error)
	FindOperationSchedulingOrigin(ctx context.Context, salonID string, ownerUserID string, operationKey string) (PersistedOperationOrigin, bool, error)
	ResolveAttemptSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, attemptID string) (string, error)
	ResolveAppointmentSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (string, error)
}

type availabilityRetryAuthorityResolver interface {
	ResolveAvailabilityRetrySchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, attemptID string) (string, error)
}

type Executor interface {
	SchedulingAuthority() string
}

type LegacyExecutor interface {
	Executor
	AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error)
	Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error)
	Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error)
	Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error)
}

type NeutralExecutor interface {
	Executor
	CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*AvailabilityResult, error)
	ExecuteAction(ctx context.Context, salonID string, ownerUserID string, req ActionRequest) (*ActionResult, error)
}

type HistoryService interface {
	ReplayCreate(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, bool, error)
	ReplayCancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, bool, error)
	RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error)
	Appointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) (*booking.ListAppointmentsResponse, error)
	Attempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*booking.ListBookingAttemptsResponse, error)
	ReconciliationTasks(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*booking.ListReconciliationTasksResponse, error)
	ReconciliationCandidates(ctx context.Context, salonID string, ownerUserID string, attemptID string) (*booking.ListReconciliationCandidatesResponse, error)
	ResolveReconciliation(ctx context.Context, salonID string, ownerUserID string, attemptID string, req booking.ResolveReconciliationRequest) (*booking.ReconciliationTask, error)
	LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*booking.TestBookingRecord, error)
	Calendar(ctx context.Context, salonID string, ownerUserID string, req booking.CalendarRangeRequest) (*booking.CalendarRangeResponse, error)
	EnsureCalendarEventAccess(ctx context.Context, salonID string, ownerUserID string) error
	CalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor booking.CalendarEventCursor, limit int) ([]booking.CalendarEvent, error)
	SyncCalendar(ctx context.Context, salonID string, ownerUserID string, req booking.CalendarSyncRequest) (*booking.CalendarSyncResponse, error)
}

type AuthorityNotReadyError struct {
	Authority string
}

func (e *AuthorityNotReadyError) Error() string {
	if e == nil || e.Authority == "" {
		return booking.ErrSchedulingAuthorityNotReady.Error()
	}
	return fmt.Sprintf("%s: %s", booking.ErrSchedulingAuthorityNotReady, e.Authority)
}

func (e *AuthorityNotReadyError) Unwrap() error {
	return booking.ErrSchedulingAuthorityNotReady
}

type Service struct {
	resolver  AuthorityResolver
	history   HistoryService
	executors map[string]Executor
}

func NewService(resolver AuthorityResolver, history HistoryService, executors ...Executor) *Service {
	registered := make(map[string]Executor, len(executors))
	for _, executor := range executors {
		if executor == nil {
			continue
		}
		registered[executor.SchedulingAuthority()] = executor
	}
	return &Service{resolver: resolver, history: history, executors: registered}
}

func (s *Service) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	var executor Executor
	var err error
	targetAppointmentID := strings.TrimSpace(req.TargetAppointmentID)
	retryOfAttemptID := strings.TrimSpace(req.RetryOfAttemptID)
	if targetAppointmentID != "" && retryOfAttemptID != "" {
		return nil, booking.ErrValidation
	}
	if retryOfAttemptID != "" {
		executor, err = s.availabilityRetryExecutor(ctx, salonID, ownerUserID, retryOfAttemptID)
	} else if targetAppointmentID == "" {
		executor, err = s.currentExecutor(ctx, salonID, ownerUserID)
	} else {
		authority, resolveErr := s.resolver.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerUserID, targetAppointmentID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		executor, err = s.executorForAuthority(authority)
	}
	if err != nil {
		return nil, err
	}
	legacy, err := legacyExecutor(executor)
	if err != nil {
		return nil, err
	}
	return legacy.AvailableSlots(ctx, salonID, ownerUserID, req)
}

func (s *Service) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	executor, err := s.operationExecutor(ctx, salonID, ownerUserID, req.OperationKey, req.RetryOfAttemptID, "")
	if err != nil {
		return nil, err
	}
	legacy, err := legacyExecutor(executor)
	if err != nil {
		return nil, err
	}
	return legacy.Create(ctx, salonID, ownerUserID, req)
}

func (s *Service) RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req booking.RescheduleLookupRequest) ([]booking.AppointmentActionRef, error) {
	return s.history.RescheduleCandidates(ctx, salonID, ownerUserID, req)
}

func (s *Service) Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.RescheduleRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	executor, err := s.operationExecutor(ctx, salonID, ownerUserID, req.OperationKey, req.RetryOfAttemptID, appointmentID)
	if err != nil {
		return nil, nil, err
	}
	legacy, err := legacyExecutor(executor)
	if err != nil {
		return nil, nil, err
	}
	return legacy.Reschedule(ctx, salonID, ownerUserID, appointmentID, req)
}

func (s *Service) Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	executor, err := s.operationExecutor(ctx, salonID, ownerUserID, req.OperationKey, req.RetryOfAttemptID, appointmentID)
	if err != nil {
		return nil, nil, err
	}
	legacy, err := legacyExecutor(executor)
	if err != nil {
		return nil, nil, err
	}
	return legacy.Cancel(ctx, salonID, ownerUserID, appointmentID, req)
}

func (s *Service) CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*AvailabilityResult, error) {
	targetAppointmentID := strings.TrimSpace(req.TargetAppointmentID)
	retryOfAttemptID := strings.TrimSpace(req.RetryOfAttemptID)
	var executor Executor
	var err error
	if targetAppointmentID != "" && retryOfAttemptID != "" {
		return nil, booking.ErrValidation
	}
	if retryOfAttemptID != "" {
		executor, err = s.availabilityRetryExecutor(ctx, salonID, ownerUserID, retryOfAttemptID)
	} else if targetAppointmentID == "" {
		executor, err = s.currentExecutor(ctx, salonID, ownerUserID)
	} else {
		authority, resolveErr := s.resolver.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerUserID, targetAppointmentID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		executor, err = s.executorForAuthority(authority)
	}
	if err != nil {
		return nil, err
	}
	return checkAvailabilityWithExecutor(ctx, executor, salonID, ownerUserID, req)
}

func (s *Service) availabilityRetryExecutor(ctx context.Context, salonID string, ownerUserID string, attemptID string) (Executor, error) {
	resolver, ok := s.resolver.(availabilityRetryAuthorityResolver)
	if !ok {
		return nil, booking.ErrOperationConflict
	}
	authority, err := resolver.ResolveAvailabilityRetrySchedulingAuthority(ctx, salonID, ownerUserID, attemptID)
	if err != nil {
		return nil, err
	}
	return s.executorForAuthority(authority)
}

func checkAvailabilityWithExecutor(ctx context.Context, executor Executor, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*AvailabilityResult, error) {
	if neutral, ok := executor.(NeutralExecutor); ok {
		return neutral.CheckAvailability(ctx, salonID, ownerUserID, req)
	}
	legacy, err := legacyExecutor(executor)
	if err != nil {
		return nil, err
	}
	result, err := legacy.AvailableSlots(ctx, salonID, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	return &AvailabilityResult{
		Kind:                AvailabilityKindVerifiedSlots,
		SchedulingAuthority: executor.SchedulingAuthority(),
		VerifiedSlots:       result,
	}, nil
}

// CheckConversationAvailability applies the salon's AI receptionist policy
// without changing the semantics of the administrator-facing
// CheckAvailability entrypoint. Pending approval still verifies availability
// through the selected executable authority, except owner_manual which remains
// request-only. Disabled mode performs no executor call.
func (s *Service) CheckConversationAvailability(ctx context.Context, salonID string, ownerUserID string, reviewedMode BookingMode, req booking.AvailabilityRequest) (*AvailabilityResult, error) {
	policy, err := s.resolver.ResolveConversationSchedulingPolicy(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if err := validateConversationPolicy(policy); err != nil {
		return nil, err
	}
	if reviewedMode != policy.BookingMode {
		return nil, booking.ErrOperationConflict
	}
	if policy.BookingMode == BookingModeDisabled {
		return nil, ErrConversationSchedulingDisabled
	}
	var executor Executor
	targetAppointmentID := strings.TrimSpace(req.TargetAppointmentID)
	if targetAppointmentID == "" {
		executor, err = s.executorForAuthority(policy.SchedulingAuthority)
	} else {
		targetAuthority, resolveErr := s.resolver.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerUserID, targetAppointmentID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		executor, err = s.executorForAuthority(targetAuthority)
	}
	if err != nil {
		return nil, err
	}
	return checkAvailabilityWithExecutor(ctx, executor, salonID, ownerUserID, req)
}

func (s *Service) ExecuteAction(ctx context.Context, salonID string, ownerUserID string, req ActionRequest) (*ActionResult, error) {
	var err error
	var persistedOrigin *PersistedOperationOrigin
	req, persistedOrigin, err = s.hydratePersistedRequestTarget(ctx, salonID, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	var targetAppointmentID string
	switch req.OperationType {
	case OperationKindBook:
	case OperationKindReschedule, OperationKindCancel:
		targetAppointmentID = req.TargetAppointmentID
	default:
		return nil, ErrInvalidSchedulingAction
	}
	var resolvedTargetAuthority string
	if strings.TrimSpace(targetAppointmentID) != "" {
		resolvedTargetAuthority, err = s.resolver.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerUserID, strings.TrimSpace(targetAppointmentID))
		if err != nil {
			return nil, err
		}
		if req.TargetAuthority != "" && req.TargetAuthority != resolvedTargetAuthority {
			return nil, booking.ErrOperationConflict
		}
		req.TargetAuthority = resolvedTargetAuthority
	}
	var resolvedQuoteAuthority string
	if strings.TrimSpace(req.AvailabilityQuoteID) != "" {
		resolvedQuoteAuthority, err = s.resolver.ResolveAvailabilityQuoteSchedulingAuthority(ctx, salonID, ownerUserID, strings.TrimSpace(req.AvailabilityQuoteID))
		if err != nil {
			return nil, err
		}
	}
	executor, err := s.operationExecutorWithResolvedEvidenceAndOrigin(ctx, salonID, ownerUserID, req.OperationKey, req.RetryOfAttemptID, targetAppointmentID, resolvedTargetAuthority, resolvedQuoteAuthority, persistedOrigin)
	if err != nil {
		return nil, err
	}
	if neutral, ok := executor.(NeutralExecutor); ok {
		return neutral.ExecuteAction(ctx, salonID, ownerUserID, req)
	}
	return executeLegacyAction(ctx, executor, salonID, ownerUserID, req)
}

// ExecuteConversationAction is replay-first. Persisted attempts and owner
// requests retain their original executor after a later booking-mode or
// authority change. Only origin-free work applies the current reviewed AI
// policy. Pending approval deliberately strips execution proof and dispatches
// to owner_manual while retaining the selected/target authority as request
// evidence.
func (s *Service) ExecuteConversationAction(ctx context.Context, salonID string, ownerUserID string, reviewed ConversationPolicyFence, req ActionRequest) (*ActionResult, error) {
	hydrated, origin, err := s.hydratePersistedRequestTarget(ctx, salonID, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	if origin != nil {
		return s.ExecuteAction(ctx, salonID, ownerUserID, hydrated)
	}

	policy, err := s.resolver.ResolveConversationSchedulingPolicy(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if err := validateConversationPolicy(policy); err != nil {
		return nil, err
	}
	if reviewed.BookingMode != policy.BookingMode || strings.TrimSpace(reviewed.SchedulingAuthority) != policy.SchedulingAuthority {
		return nil, booking.ErrOperationConflict
	}
	switch policy.BookingMode {
	case BookingModeDisabled:
		return nil, ErrConversationSchedulingDisabled
	case BookingModeConfirmedBooking:
		return s.ExecuteAction(ctx, salonID, ownerUserID, hydrated)
	case BookingModePendingApproval:
		pending := hydrated
		pending.RetryOfAttemptID = ""
		pending.AvailabilityQuoteID = ""
		pending.SlotFingerprint = ""
		pending.ExpectedTargetAuthorityAppointmentVersion = 0
		if strings.TrimSpace(pending.TargetAppointmentID) != "" {
			targetAuthority, resolveErr := s.resolver.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerUserID, strings.TrimSpace(pending.TargetAppointmentID))
			if resolveErr != nil {
				return nil, resolveErr
			}
			if pending.TargetAuthority != "" && pending.TargetAuthority != targetAuthority {
				return nil, booking.ErrOperationConflict
			}
			pending.TargetAuthority = targetAuthority
		} else {
			pending.TargetAuthority = policy.SchedulingAuthority
		}
		executor, executorErr := s.executorForAuthority(booking.SchedulingAuthorityOwnerManual)
		if executorErr != nil {
			return nil, executorErr
		}
		neutral, ok := executor.(NeutralExecutor)
		if !ok {
			return nil, &AuthorityNotReadyError{Authority: booking.SchedulingAuthorityOwnerManual}
		}
		return neutral.ExecuteAction(ctx, salonID, ownerUserID, pending)
	default:
		return nil, &AuthorityNotReadyError{Authority: policy.SchedulingAuthority}
	}
}

func (s *Service) hydratePersistedRequestTarget(ctx context.Context, salonID string, ownerUserID string, req ActionRequest) (ActionRequest, *PersistedOperationOrigin, error) {
	origin, found, err := s.resolver.FindOperationSchedulingOrigin(ctx, salonID, ownerUserID, strings.TrimSpace(req.OperationKey))
	if err != nil || !found {
		return req, nil, err
	}
	if !origin.SchedulingRequest {
		return req, &origin, nil
	}
	provided := strings.TrimSpace(req.TargetAuthority)
	if origin.RequestTargetAuthorityPresent {
		if provided != "" && provided != origin.RequestTargetAuthority {
			return ActionRequest{}, nil, booking.ErrOperationConflict
		}
		req.TargetAuthority = origin.RequestTargetAuthority
	} else if provided != "" {
		return ActionRequest{}, nil, booking.ErrOperationConflict
	}
	return req, &origin, nil
}

func validateConversationPolicy(policy ConversationPolicyFence) error {
	_, err := ConversationBehavior(policy)
	return err
}

func (s *Service) CurrentSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	authority, err := s.resolver.ResolveSchedulingAuthority(ctx, salonID, ownerUserID)
	if err != nil {
		return "", err
	}
	if !isKnownSchedulingAuthority(authority) {
		return "", &AuthorityNotReadyError{Authority: authority}
	}
	return authority, nil
}

func (s *Service) ResolveCreateSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string) (string, error) {
	authority, found, err := s.resolvePersistedOperationAuthority(ctx, salonID, ownerUserID, operationKey, retryOfAttemptID)
	if err != nil {
		return "", err
	}
	if !found {
		return s.CurrentSchedulingAuthority(ctx, salonID, ownerUserID)
	}
	if !isKnownSchedulingAuthority(authority) {
		return "", &AuthorityNotReadyError{Authority: authority}
	}
	return authority, nil
}

func (s *Service) currentExecutor(ctx context.Context, salonID string, ownerUserID string) (Executor, error) {
	authority, err := s.CurrentSchedulingAuthority(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return s.executorForAuthority(authority)
}

func (s *Service) operationExecutor(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string, targetAppointmentID string) (Executor, error) {
	return s.operationExecutorWithResolvedTarget(ctx, salonID, ownerUserID, operationKey, retryOfAttemptID, targetAppointmentID, "")
}

func (s *Service) operationExecutorWithResolvedTarget(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string, targetAppointmentID string, resolvedTargetAuthority string) (Executor, error) {
	return s.operationExecutorWithResolvedEvidence(ctx, salonID, ownerUserID, operationKey, retryOfAttemptID, targetAppointmentID, resolvedTargetAuthority, "")
}

func (s *Service) operationExecutorWithResolvedEvidence(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string, targetAppointmentID string, resolvedTargetAuthority string, resolvedQuoteAuthority string) (Executor, error) {
	return s.operationExecutorWithResolvedEvidenceAndOrigin(ctx, salonID, ownerUserID, operationKey, retryOfAttemptID, targetAppointmentID, resolvedTargetAuthority, resolvedQuoteAuthority, nil)
}

func (s *Service) operationExecutorWithResolvedEvidenceAndOrigin(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string, targetAppointmentID string, resolvedTargetAuthority string, resolvedQuoteAuthority string, persistedOrigin *PersistedOperationOrigin) (Executor, error) {
	targetAppointmentID = strings.TrimSpace(targetAppointmentID)
	authority, found, err := s.resolvePersistedOperationAuthorityWithOrigin(ctx, salonID, ownerUserID, operationKey, retryOfAttemptID, persistedOrigin)
	if err != nil {
		return nil, err
	}
	if targetAppointmentID != "" {
		targetAuthority := resolvedTargetAuthority
		if targetAuthority == "" {
			var targetErr error
			targetAuthority, targetErr = s.resolver.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerUserID, targetAppointmentID)
			if targetErr != nil {
				return nil, targetErr
			}
		}
		if found && authority != targetAuthority {
			return nil, booking.ErrOperationConflict
		}
		authority = targetAuthority
		found = true
	}
	if resolvedQuoteAuthority != "" {
		if found && authority != resolvedQuoteAuthority {
			return nil, booking.ErrOperationConflict
		}
		authority = resolvedQuoteAuthority
		found = true
	}
	if !found {
		return s.currentExecutor(ctx, salonID, ownerUserID)
	}
	return s.executorForAuthority(authority)
}

func (s *Service) resolvePersistedOperationAuthorityWithOrigin(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string, persistedOrigin *PersistedOperationOrigin) (string, bool, error) {
	if persistedOrigin == nil {
		return s.resolvePersistedOperationAuthority(ctx, salonID, ownerUserID, operationKey, retryOfAttemptID)
	}
	authority := persistedOrigin.SchedulingAuthority
	found := true
	retryOfAttemptID = strings.TrimSpace(retryOfAttemptID)
	if retryOfAttemptID == "" {
		return authority, found, nil
	}
	retryAuthority, err := s.resolver.ResolveAttemptSchedulingAuthority(ctx, salonID, ownerUserID, retryOfAttemptID)
	if err != nil {
		return "", false, err
	}
	if authority != retryAuthority {
		return "", false, booking.ErrOperationConflict
	}
	return authority, true, nil
}

func (s *Service) resolvePersistedOperationAuthority(ctx context.Context, salonID string, ownerUserID string, operationKey string, retryOfAttemptID string) (string, bool, error) {
	operationKey = strings.TrimSpace(operationKey)
	retryOfAttemptID = strings.TrimSpace(retryOfAttemptID)
	authority, found, err := s.resolver.FindOperationSchedulingAuthority(ctx, salonID, ownerUserID, operationKey)
	if err != nil {
		return "", false, err
	}
	if retryOfAttemptID == "" {
		return authority, found, nil
	}
	retryAuthority, err := s.resolver.ResolveAttemptSchedulingAuthority(ctx, salonID, ownerUserID, retryOfAttemptID)
	if err != nil {
		return "", false, err
	}
	if found && authority != retryAuthority {
		return "", false, booking.ErrOperationConflict
	}
	return retryAuthority, true, nil
}

func (s *Service) executorForAuthority(authority string) (Executor, error) {
	if !isKnownSchedulingAuthority(authority) {
		return nil, &AuthorityNotReadyError{Authority: authority}
	}
	executor := s.executors[authority]
	if executor == nil {
		return nil, &AuthorityNotReadyError{Authority: authority}
	}
	return executor, nil
}

func legacyExecutor(executor Executor) (LegacyExecutor, error) {
	legacy, ok := executor.(LegacyExecutor)
	if !ok {
		return nil, &AuthorityNotReadyError{Authority: executor.SchedulingAuthority()}
	}
	return legacy, nil
}

func executeLegacyAction(ctx context.Context, executor Executor, salonID string, ownerUserID string, req ActionRequest) (*ActionResult, error) {
	if err := validateLegacyActionRepresentable(req); err != nil {
		return nil, err
	}
	legacy, err := legacyExecutor(executor)
	if err != nil {
		return nil, err
	}
	switch req.OperationType {
	case OperationKindBook:
		attempt, err := legacy.Create(ctx, salonID, ownerUserID, createBookingRequest(req))
		if err != nil {
			return nil, err
		}
		return actionResultFromExternalAttempt(req.OperationType, attempt)
	case OperationKindReschedule:
		appointment, fallback, err := legacy.Reschedule(ctx, salonID, ownerUserID, req.TargetAppointmentID, rescheduleBookingRequest(req))
		if err != nil {
			return nil, err
		}
		return actionResultFromExternalMutation(req.OperationType, appointment, fallback)
	case OperationKindCancel:
		appointment, fallback, err := legacy.Cancel(ctx, salonID, ownerUserID, req.TargetAppointmentID, cancelBookingRequest(req))
		if err != nil {
			return nil, err
		}
		return actionResultFromExternalMutation(req.OperationType, appointment, fallback)
	default:
		return nil, ErrInvalidSchedulingAction
	}
}

// Legacy booking DTOs have no party/guest/quantity fields. Segment times are
// accepted only as redundant evidence of the top-level provider mutation.
// Owner-manual neutral executors do not use this adapter and retain the full
// request.
func validateLegacyActionRepresentable(req ActionRequest) error {
	if req.PartySize < 0 || req.PartySize > 1 {
		return ErrInvalidSchedulingAction
	}
	for _, segment := range req.Segments {
		if segment.Quantity < 0 || segment.Quantity > 1 || strings.TrimSpace(segment.GuestReference) != "" {
			return ErrInvalidSchedulingAction
		}
		segmentHasStart := !segment.RequestedStartTime.IsZero()
		segmentHasEnd := !segment.RequestedEndTime.IsZero()
		if !segmentHasStart && !segmentHasEnd {
			continue
		}
		if !legacySegmentTimingRepresentable(req, segment, segmentHasStart, segmentHasEnd) {
			return ErrInvalidSchedulingAction
		}
	}
	return nil
}

func legacySegmentTimingRepresentable(req ActionRequest, segment ActionSegment, segmentHasStart bool, segmentHasEnd bool) bool {
	if req.OperationType == OperationKindReschedule || req.OperationType == OperationKindCancel {
		if strings.TrimSpace(req.TargetAppointmentID) == "" || !segmentHasStart || req.RequestedStartTime.IsZero() ||
			!segment.RequestedStartTime.Equal(req.RequestedStartTime) {
			return false
		}
		if !req.RequestedEndTime.IsZero() && !req.RequestedEndTime.After(req.RequestedStartTime) {
			return false
		}
		if !segmentHasEnd {
			return true
		}
		return segment.RequestedEndTime.After(segment.RequestedStartTime) &&
			(req.RequestedEndTime.IsZero() || !segment.RequestedEndTime.After(req.RequestedEndTime))
	}

	return segmentHasStart && segmentHasEnd &&
		!req.RequestedStartTime.IsZero() && !req.RequestedEndTime.IsZero() &&
		req.RequestedEndTime.After(req.RequestedStartTime) &&
		segment.RequestedStartTime.Equal(req.RequestedStartTime) &&
		segment.RequestedEndTime.After(segment.RequestedStartTime) &&
		!segment.RequestedEndTime.After(req.RequestedEndTime)
}

func createBookingRequest(req ActionRequest) booking.CreateBookingRequest {
	segments := bookingSegments(req.Segments)
	result := booking.CreateBookingRequest{
		OperationKey:        req.OperationKey,
		RetryOfAttemptID:    req.RetryOfAttemptID,
		AvailabilityQuoteID: req.AvailabilityQuoteID,
		SlotFingerprint:     req.SlotFingerprint,
		Source:              req.Source,
		CustomerName:        req.CustomerName,
		CustomerPhone:       req.CustomerPhone,
		CustomerEmail:       req.CustomerEmail,
		Segments:            segments,
		StartTime:           req.RequestedStartTime,
		Notes:               req.Notes,
	}
	if len(segments) > 0 {
		result.ServiceID = segments[0].ServiceID
		result.StaffID = segments[0].StaffID
		result.StaffSelectionMode = segments[0].StaffSelectionMode
	}
	return result
}

func rescheduleBookingRequest(req ActionRequest) booking.RescheduleRequest {
	result := booking.RescheduleRequest{
		OperationKey:        req.OperationKey,
		RetryOfAttemptID:    req.RetryOfAttemptID,
		AvailabilityQuoteID: req.AvailabilityQuoteID,
		SlotFingerprint:     req.SlotFingerprint,
		StartTime:           req.RequestedStartTime,
		Notes:               req.Notes,
		Source:              req.Source,
	}
	if len(req.Segments) > 0 {
		result.StaffID = req.Segments[0].StaffID
	}
	return result
}

func cancelBookingRequest(req ActionRequest) booking.CancelRequest {
	return booking.CancelRequest{
		OperationKey:     req.OperationKey,
		RetryOfAttemptID: req.RetryOfAttemptID,
		Reason:           req.Notes,
		Source:           req.Source,
	}
}

func bookingSegments(segments []ActionSegment) []booking.BookingSegmentRequest {
	result := make([]booking.BookingSegmentRequest, 0, len(segments))
	for _, segment := range segments {
		result = append(result, booking.BookingSegmentRequest{
			ServiceID:          segment.ServiceID,
			StaffID:            segment.StaffID,
			StaffSelectionMode: segment.StaffSelectionMode,
		})
	}
	return result
}

func actionResultFromExternalAttempt(operation OperationKind, attempt *booking.BookingAttempt) (*ActionResult, error) {
	if attempt == nil {
		return nil, ErrInvalidSchedulingResult
	}
	if attempt.Status == booking.StatusFallbackPending || attempt.Status == booking.StatusPOSPending || attempt.Status == booking.StatusProviderPending {
		if strings.TrimSpace(attempt.ID) == "" || attempt.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider {
			return nil, ErrInvalidSchedulingResult
		}
		return &ActionResult{
			Kind:                ActionKindExternalFallbackPending,
			OperationType:       operation,
			SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
			ExternalFallbackPending: &ExternalFallbackPendingResult{
				ExternalAttemptID: attempt.ID,
				ExternalAttempt:   attempt,
			},
		}, nil
	}
	if attempt.Status != booking.StatusConfirmed || strings.TrimSpace(attempt.ID) == "" ||
		attempt.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider ||
		strings.TrimSpace(attempt.AuthorityProvider) == "" || strings.TrimSpace(attempt.AuthorityAppointmentID) == "" || attempt.AuthorityAppointmentVersion < 0 ||
		strings.TrimSpace(attempt.POSProvider) == "" || strings.TrimSpace(attempt.POSBookingID) == "" || attempt.POSBookingVersion < 0 ||
		attempt.Appointment == nil || strings.TrimSpace(attempt.Appointment.ID) == "" ||
		attempt.Appointment.Status != booking.StatusConfirmed ||
		attempt.Appointment.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider ||
		strings.TrimSpace(attempt.Appointment.AuthorityProvider) == "" || strings.TrimSpace(attempt.Appointment.AuthorityAppointmentID) == "" || attempt.Appointment.AuthorityAppointmentVersion < 0 ||
		strings.TrimSpace(attempt.Appointment.POSProvider) == "" || strings.TrimSpace(attempt.Appointment.POSAppointmentID) == "" || attempt.Appointment.POSAppointmentVersion < 0 ||
		attempt.AuthorityProvider != attempt.Appointment.AuthorityProvider || attempt.AuthorityAppointmentID != attempt.Appointment.AuthorityAppointmentID ||
		attempt.AuthorityAppointmentVersion != attempt.Appointment.AuthorityAppointmentVersion ||
		attempt.POSProvider != attempt.Appointment.POSProvider || attempt.POSBookingID != attempt.Appointment.POSAppointmentID || attempt.POSBookingVersion != attempt.Appointment.POSAppointmentVersion {
		return nil, ErrInvalidSchedulingResult
	}
	return &ActionResult{
		Kind:                ActionKindConfirmedAppointment,
		OperationType:       operation,
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		ConfirmedAppointment: &ConfirmedAppointmentResult{
			AppointmentID:     attempt.Appointment.ID,
			ExternalAttemptID: attempt.ID,
			AppointmentStatus: attempt.Appointment.Status,
			ActiveChildCount:  appointmentActiveChildCount(attempt.Appointment),
			Appointment:       attempt.Appointment,
			ExternalAttempt:   attempt,
		},
	}, nil
}

func actionResultFromExternalMutation(operation OperationKind, appointment *booking.Appointment, fallback *booking.BookingAttempt) (*ActionResult, error) {
	if fallback != nil {
		if strings.TrimSpace(fallback.ID) == "" || fallback.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider ||
			(fallback.Status != booking.StatusFallbackPending && fallback.Status != booking.StatusPOSPending && fallback.Status != booking.StatusProviderPending) {
			return nil, ErrInvalidSchedulingResult
		}
		return &ActionResult{
			Kind:                ActionKindExternalFallbackPending,
			OperationType:       operation,
			SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
			ExternalFallbackPending: &ExternalFallbackPendingResult{
				ExternalAttemptID: fallback.ID,
				ExternalAttempt:   fallback,
			},
		}, nil
	}
	wantStatus := booking.StatusRescheduled
	if operation == OperationKindCancel {
		wantStatus = booking.StatusCancelled
	}
	if appointment == nil || strings.TrimSpace(appointment.ID) == "" || appointment.Status != wantStatus ||
		appointment.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider ||
		strings.TrimSpace(appointment.AuthorityProvider) == "" || strings.TrimSpace(appointment.AuthorityAppointmentID) == "" || appointment.AuthorityAppointmentVersion < 0 ||
		strings.TrimSpace(appointment.POSProvider) == "" || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion < 0 ||
		appointment.AuthorityProvider != appointment.POSProvider || appointment.AuthorityAppointmentID != appointment.POSAppointmentID || appointment.AuthorityAppointmentVersion != appointment.POSAppointmentVersion {
		return nil, ErrInvalidSchedulingResult
	}
	return &ActionResult{
		Kind:                ActionKindConfirmedAppointment,
		OperationType:       operation,
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		ConfirmedAppointment: &ConfirmedAppointmentResult{
			AppointmentID:     appointment.ID,
			AppointmentStatus: appointment.Status,
			ActiveChildCount:  appointmentActiveChildCount(appointment),
			Appointment:       appointment,
		},
	}, nil
}

func appointmentActiveChildCount(appointment *booking.Appointment) int {
	if appointment == nil || appointment.Status == booking.StatusCancelled {
		return 0
	}
	if len(appointment.Segments) > 0 {
		return len(appointment.Segments)
	}
	if strings.TrimSpace(appointment.ServiceID) != "" {
		return 1
	}
	return 0
}

func isKnownSchedulingAuthority(authority string) bool {
	switch authority {
	case booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
		booking.SchedulingAuthorityExternalProvider:
		return true
	default:
		return false
	}
}

func (s *Service) ReplayCreate(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, bool, error) {
	return s.history.ReplayCreate(ctx, salonID, ownerUserID, req)
}

func (s *Service) ReplayCancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, bool, error) {
	return s.history.ReplayCancel(ctx, salonID, ownerUserID, appointmentID, req)
}

func (s *Service) Appointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) (*booking.ListAppointmentsResponse, error) {
	return s.history.Appointments(ctx, salonID, ownerUserID, limit, offset)
}

func (s *Service) Attempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*booking.ListBookingAttemptsResponse, error) {
	return s.history.Attempts(ctx, salonID, ownerUserID, status, limit, offset)
}

func (s *Service) ReconciliationTasks(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*booking.ListReconciliationTasksResponse, error) {
	return s.history.ReconciliationTasks(ctx, salonID, ownerUserID, status, limit, offset)
}

func (s *Service) ReconciliationCandidates(ctx context.Context, salonID string, ownerUserID string, attemptID string) (*booking.ListReconciliationCandidatesResponse, error) {
	return s.history.ReconciliationCandidates(ctx, salonID, ownerUserID, attemptID)
}

func (s *Service) ResolveReconciliation(ctx context.Context, salonID string, ownerUserID string, attemptID string, req booking.ResolveReconciliationRequest) (*booking.ReconciliationTask, error) {
	return s.history.ResolveReconciliation(ctx, salonID, ownerUserID, attemptID, req)
}

func (s *Service) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*booking.TestBookingRecord, error) {
	return s.history.LatestTestBooking(ctx, salonID, ownerUserID)
}

func (s *Service) Calendar(ctx context.Context, salonID string, ownerUserID string, req booking.CalendarRangeRequest) (*booking.CalendarRangeResponse, error) {
	return s.history.Calendar(ctx, salonID, ownerUserID, req)
}

func (s *Service) EnsureCalendarEventAccess(ctx context.Context, salonID string, ownerUserID string) error {
	return s.history.EnsureCalendarEventAccess(ctx, salonID, ownerUserID)
}

func (s *Service) CalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor booking.CalendarEventCursor, limit int) ([]booking.CalendarEvent, error) {
	return s.history.CalendarEvents(ctx, salonID, ownerUserID, cursor, limit)
}

func (s *Service) SyncCalendar(ctx context.Context, salonID string, ownerUserID string, req booking.CalendarSyncRequest) (*booking.CalendarSyncResponse, error) {
	return s.history.SyncCalendar(ctx, salonID, ownerUserID, req)
}

var _ booking.HandlerService = (*Service)(nil)
