package scheduling_owner_manual

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

const (
	maxOperationKeyLength      = 200
	maxActionKeyLength         = 200
	maxCustomerNameLength      = 200
	maxCustomerPhoneLength     = 64
	maxCustomerEmailLength     = 320
	maxTimezoneLength          = 100
	maxTargetDescriptionLength = 2000
	maxNotesLength             = 4000
	maxResolutionReasonLength  = 500
	maxTransitionNoteLength    = 2000
	// These protocol safety bounds cap one transaction's row/work amplification;
	// salon services, party membership, and quantities remain runtime data.
	maxSegmentsPerRequest = 50
	maxPartySize          = 50
	maxSegmentQuantity    = 50
)

type Store interface {
	SchedulingTargetReadinessFacts(ctx context.Context, salonID string, ownerUserID string) (authorityVersion int64, eligibleServiceCount int, err error)
	CreateOrReplay(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest, requestFingerprint string) (*scheduling.SchedulingRequest, bool, error)
	List(ctx context.Context, salonID string, ownerUserID string, status scheduling.SchedulingRequestStatus, limit int, offset int) (*scheduling.ListSchedulingRequestsResponse, error)
	Get(ctx context.Context, salonID string, ownerUserID string, requestID string) (*scheduling.SchedulingRequest, error)
	Transition(ctx context.Context, salonID string, ownerUserID string, requestID string, req scheduling.TransitionSchedulingRequest, actionFingerprint string) (*scheduling.SchedulingRequest, bool, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) SchedulingTargetReadiness(ctx context.Context, salonID string, ownerUserID string) (scheduling.TargetReadiness, error) {
	authorityVersion, eligibleServiceCount, err := s.store.SchedulingTargetReadinessFacts(ctx, salonID, ownerUserID)
	if err != nil {
		return scheduling.TargetReadiness{}, err
	}
	result := scheduling.TargetReadiness{
		TargetSchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		AuthorityVersion:          authorityVersion,
		EligibleServiceCount:      eligibleServiceCount,
		Checks:                    make([]scheduling.TargetReadinessCheck, 0, 2),
		Blockers:                  make([]scheduling.TargetReadinessBlocker, 0),
		AvailabilityBlockers:      make([]scheduling.TargetReadinessBlocker, 0),
		ExecutionBlockers:         make([]scheduling.TargetReadinessBlocker, 0, 1),
	}
	result.Checks = append(result.Checks,
		scheduling.TargetReadinessCheck{Code: "OWNER_MANUAL_REQUEST_EXECUTOR_READY", Ready: true, Scope: "executor"},
		scheduling.TargetReadinessCheck{Code: "OWNER_MANUAL_ELIGIBLE_SERVICE_REQUIRED", Ready: eligibleServiceCount > 0, Scope: "service"},
	)
	if eligibleServiceCount == 0 {
		blocker := scheduling.TargetReadinessBlocker{
			Code: "OWNER_MANUAL_ELIGIBLE_SERVICE_REQUIRED", Scope: "service",
			Message: "Add at least one active, AI-bookable service with a positive duration.",
		}
		result.Blockers = append(result.Blockers, blocker)
		result.AvailabilityBlockers = append(result.AvailabilityBlockers, blocker)
	}
	result.AvailabilityReady = len(result.AvailabilityBlockers) == 0
	result.ExecutionBlockers = append(result.ExecutionBlockers, scheduling.TargetReadinessBlocker{
		Code: "OWNER_MANUAL_REQUEST_ONLY", Scope: "executor",
		Message: "Owner-managed scheduling creates a pending owner-review request and never confirms automatically.",
	})
	result.ExecutionReady = false
	result.Ready = result.AvailabilityReady
	return result, nil
}

func (s *Service) CreateRequest(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest) (*scheduling.SchedulingRequest, bool, error) {
	normalized, err := normalizeActionRequest(req)
	if err != nil {
		return nil, false, err
	}
	fingerprint, err := actionRequestFingerprint(normalized)
	if err != nil {
		return nil, false, err
	}
	return s.store.CreateOrReplay(ctx, salonID, ownerUserID, normalized, fingerprint)
}

func (s *Service) List(ctx context.Context, salonID string, ownerUserID string, status scheduling.SchedulingRequestStatus, limit int, offset int) (*scheduling.ListSchedulingRequestsResponse, error) {
	if status != "" && !validRequestStatus(status) {
		return nil, scheduling.ErrInvalidSchedulingAction
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.List(ctx, salonID, ownerUserID, status, limit, offset)
}

func (s *Service) Get(ctx context.Context, salonID string, ownerUserID string, requestID string) (*scheduling.SchedulingRequest, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, scheduling.ErrInvalidSchedulingAction
	}
	return s.store.Get(ctx, salonID, ownerUserID, strings.TrimSpace(requestID))
}

func (s *Service) Transition(ctx context.Context, salonID string, ownerUserID string, requestID string, req scheduling.TransitionSchedulingRequest) (*scheduling.SchedulingRequest, bool, error) {
	req.ActionKey = strings.TrimSpace(req.ActionKey)
	req.ResolutionReason = strings.TrimSpace(req.ResolutionReason)
	req.Note = strings.TrimSpace(req.Note)
	if strings.TrimSpace(requestID) == "" || req.ActionKey == "" || len(req.ActionKey) > maxActionKeyLength || req.ExpectedVersion <= 0 ||
		!validTransitionTarget(req.Status) || len(req.ResolutionReason) > maxResolutionReasonLength || len(req.Note) > maxTransitionNoteLength {
		return nil, false, scheduling.ErrInvalidSchedulingAction
	}
	if (req.Status == scheduling.SchedulingRequestStatusResolved || req.Status == scheduling.SchedulingRequestStatusDismissed) && req.ResolutionReason == "" {
		return nil, false, scheduling.ErrInvalidSchedulingAction
	}
	fingerprint, err := transitionFingerprint(req)
	if err != nil {
		return nil, false, err
	}
	return s.store.Transition(ctx, salonID, ownerUserID, strings.TrimSpace(requestID), req, fingerprint)
}

func normalizeActionRequest(req scheduling.ActionRequest) (scheduling.ActionRequest, error) {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.RetryOfAttemptID = strings.TrimSpace(req.RetryOfAttemptID)
	req.AvailabilityQuoteID = strings.TrimSpace(req.AvailabilityQuoteID)
	req.SlotFingerprint = strings.TrimSpace(req.SlotFingerprint)
	req.Source = strings.TrimSpace(req.Source)
	req.CallSessionID = strings.TrimSpace(req.CallSessionID)
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerPhone = strings.TrimSpace(req.CustomerPhone)
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)
	req.RequestedTimezone = strings.TrimSpace(req.RequestedTimezone)
	req.Notes = strings.TrimSpace(req.Notes)
	req.TargetAppointmentID = strings.TrimSpace(req.TargetAppointmentID)
	req.TargetAuthority = strings.TrimSpace(req.TargetAuthority)
	req.TargetDescription = strings.TrimSpace(req.TargetDescription)
	if !req.RequestedStartTime.IsZero() {
		req.RequestedStartTime = req.RequestedStartTime.UTC()
	}
	if !req.RequestedEndTime.IsZero() {
		req.RequestedEndTime = req.RequestedEndTime.UTC()
	}
	if req.OperationKey == "" || len(req.OperationKey) > maxOperationKeyLength || req.Source == "" || req.CustomerName == "" || req.CustomerPhone == "" || req.RequestedTimezone == "" ||
		len(req.CustomerName) > maxCustomerNameLength || len(req.CustomerPhone) > maxCustomerPhoneLength || len(req.CustomerEmail) > maxCustomerEmailLength ||
		len(req.RequestedTimezone) > maxTimezoneLength || len(req.TargetDescription) > maxTargetDescriptionLength || len(req.Notes) > maxNotesLength {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	if req.OperationType != scheduling.OperationKindBook && req.OperationType != scheduling.OperationKindReschedule && req.OperationType != scheduling.OperationKindCancel {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	if req.RetryOfAttemptID != "" || req.AvailabilityQuoteID != "" || req.SlotFingerprint != "" {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	if req.TargetAuthority != "" && req.TargetAuthority != booking.SchedulingAuthorityOwnerManual &&
		req.TargetAuthority != booking.SchedulingAuthorityManleAICalendar && req.TargetAuthority != booking.SchedulingAuthorityExternalProvider {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	if req.OperationType == scheduling.OperationKindBook && (req.TargetAppointmentID != "" || req.TargetDescription != "") {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	if req.OperationType == scheduling.OperationKindReschedule || req.OperationType == scheduling.OperationKindCancel {
		if req.TargetAppointmentID == "" && req.TargetDescription == "" {
			return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
		}
		if req.TargetAppointmentID != "" && req.TargetAuthority == "" {
			return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
		}
	}
	if len(req.Segments) > maxSegmentsPerRequest ||
		((req.OperationType == scheduling.OperationKindBook || req.OperationType == scheduling.OperationKindReschedule) && len(req.Segments) == 0) {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	guestReferences := make(map[string]struct{})
	for i := range req.Segments {
		segment := &req.Segments[i]
		segment.ServiceID = strings.TrimSpace(segment.ServiceID)
		segment.StaffID = strings.TrimSpace(segment.StaffID)
		segment.StaffSelectionMode = strings.TrimSpace(segment.StaffSelectionMode)
		segment.GuestReference = strings.TrimSpace(segment.GuestReference)
		if !segment.RequestedStartTime.IsZero() {
			segment.RequestedStartTime = segment.RequestedStartTime.UTC()
		}
		if !segment.RequestedEndTime.IsZero() {
			segment.RequestedEndTime = segment.RequestedEndTime.UTC()
		}
		if segment.Quantity == 0 {
			segment.Quantity = 1
		}
		if segment.ServiceID == "" || segment.Quantity < 1 || segment.Quantity > maxSegmentQuantity ||
			(segment.StaffSelectionMode != booking.StaffSelectionSpecific && segment.StaffSelectionMode != booking.StaffSelectionAnyone) ||
			(segment.StaffSelectionMode == booking.StaffSelectionSpecific && segment.StaffID == "") ||
			(segment.StaffSelectionMode == booking.StaffSelectionAnyone && segment.StaffID != "") ||
			(!segment.RequestedEndTime.IsZero() && (segment.RequestedStartTime.IsZero() || !segment.RequestedEndTime.After(segment.RequestedStartTime))) {
			return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
		}
		if segment.GuestReference != "" {
			guestReferences[segment.GuestReference] = struct{}{}
		}
	}
	if req.PartySize == 0 {
		req.PartySize = len(guestReferences)
		if req.PartySize == 0 {
			req.PartySize = 1
		}
	}
	if req.PartySize < 1 || req.PartySize > maxPartySize ||
		(!req.RequestedEndTime.IsZero() && (req.RequestedStartTime.IsZero() || !req.RequestedEndTime.After(req.RequestedStartTime))) {
		return scheduling.ActionRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	return req, nil
}

func actionRequestFingerprint(req scheduling.ActionRequest) (string, error) {
	return hashCanonical(struct {
		scheduling.ActionRequest
		Source string `json:"source"`
	}{ActionRequest: req, Source: req.Source})
}

func transitionFingerprint(req scheduling.TransitionSchedulingRequest) (string, error) {
	return hashCanonical(req)
}

func hashCanonical(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func validRequestStatus(status scheduling.SchedulingRequestStatus) bool {
	switch status {
	case scheduling.SchedulingRequestStatusPending,
		scheduling.SchedulingRequestStatusContacted,
		scheduling.SchedulingRequestStatusResolved,
		scheduling.SchedulingRequestStatusDismissed:
		return true
	default:
		return false
	}
}

func validTransitionTarget(status scheduling.SchedulingRequestStatus) bool {
	return status == scheduling.SchedulingRequestStatusContacted || status == scheduling.SchedulingRequestStatusResolved || status == scheduling.SchedulingRequestStatusDismissed
}
