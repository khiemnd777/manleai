package scheduling_manleai_calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type ExecutionStore interface {
	CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest, now time.Time) (*booking.AvailabilityResult, error)
	CreateAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalCreateRequest, now time.Time) (*InternalCreateResult, bool, error)
	RescheduleAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalLifecycleRequest, now time.Time) (*InternalCreateResult, bool, error)
	CancelAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalLifecycleRequest, now time.Time) (*InternalCreateResult, bool, error)
}

type Executor struct {
	store ExecutionStore
	clock Clock
}

type InternalCreateRequest struct {
	OperationKey        string
	RequestFingerprint  string
	AvailabilityQuoteID string
	SlotFingerprint     string
	Source              string
	CustomerName        string
	CustomerPhone       string
	CustomerEmail       string
	PartySize           int
	Segments            []InternalCreateSegment
	RequestedStartTime  time.Time
	RequestedEndTime    time.Time
	RequestedTimezone   string
	Notes               string
}

type InternalCreateSegment struct {
	ServiceID          string
	StaffID            string
	StaffSelectionMode string
	GuestReference     string
	Quantity           int
	RequestedStartTime time.Time
	RequestedEndTime   time.Time
}

type InternalCreateSegmentResult struct {
	AppointmentServiceID string
	GuestReference       string
	ServiceID            string
	StaffID              string
	StaffSelectionMode   string
	Quantity             int
	ScheduledStartTime   time.Time
	ScheduledEndTime     time.Time
	OccupiedStartTime    time.Time
	OccupiedEndTime      time.Time
	BufferBeforeMinutes  int
	BufferAfterMinutes   int
	ResourceAllocations  []InternalResourceAllocation
}

type InternalCreateResult struct {
	AppointmentID                     string
	BookingAttemptID                  string
	AppointmentStatus                 string
	TargetAuthorityAppointmentVersion int
	AuthorityAppointmentVersion       int
	ActiveChildCount                  int
	Children                          []InternalCreateSegmentResult
}

type InternalLifecycleRequest struct {
	OperationType                             scheduling.OperationKind
	OperationKey                              string
	RequestFingerprint                        string
	AvailabilityQuoteID                       string
	SlotFingerprint                           string
	Source                                    string
	CustomerName                              string
	CustomerPhone                             string
	CustomerEmail                             string
	PartySize                                 int
	Segments                                  []InternalCreateSegment
	RequestedStartTime                        time.Time
	RequestedEndTime                          time.Time
	RequestedTimezone                         string
	Notes                                     string
	TargetAppointmentID                       string
	ExpectedTargetAuthorityAppointmentVersion int
}

func NewExecutor(store ExecutionStore, clock Clock) *Executor {
	if clock == nil {
		clock = systemClock{}
	}
	return &Executor{store: store, clock: clock}
}

func (e *Executor) SchedulingAuthority() string {
	return booking.SchedulingAuthorityManleAICalendar
}

func (e *Executor) CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error) {
	if _, err := normalizeAggregateAvailabilityRequest(req); err != nil {
		return nil, err
	}
	result, err := e.store.CheckAvailability(ctx, salonID, ownerUserID, req, e.clock.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &scheduling.AvailabilityResult{
		Kind: scheduling.AvailabilityKindVerifiedSlots, SchedulingAuthority: e.SchedulingAuthority(),
		TargetAuthorityAppointmentVersion: result.TargetAuthorityAppointmentVersion, VerifiedSlots: result,
	}, nil
}

func (e *Executor) ExecuteAction(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	var (
		created               *InternalCreateResult
		replayed              bool
		expectedSegments      []InternalCreateSegment
		expectedTargetVersion int
		err                   error
	)
	switch req.OperationType {
	case scheduling.OperationKindBook:
		var normalized InternalCreateRequest
		normalized, err = normalizeInternalCreateRequest(req)
		if err == nil {
			expectedSegments = normalized.Segments
			created, replayed, err = e.store.CreateAppointment(ctx, salonID, ownerUserID, normalized, e.clock.Now().UTC())
		}
	case scheduling.OperationKindReschedule:
		var normalized InternalLifecycleRequest
		normalized, err = normalizeInternalLifecycleRequest(req)
		if err == nil {
			expectedSegments = normalized.Segments
			expectedTargetVersion = normalized.ExpectedTargetAuthorityAppointmentVersion
			created, replayed, err = e.store.RescheduleAppointment(ctx, salonID, ownerUserID, normalized, e.clock.Now().UTC())
		}
	case scheduling.OperationKindCancel:
		var normalized InternalLifecycleRequest
		normalized, err = normalizeInternalLifecycleRequest(req)
		if err == nil {
			expectedTargetVersion = normalized.ExpectedTargetAuthorityAppointmentVersion
			created, replayed, err = e.store.CancelAppointment(ctx, salonID, ownerUserID, normalized, e.clock.Now().UTC())
		}
	default:
		err = scheduling.ErrInvalidSchedulingAction
	}
	if err != nil {
		return nil, err
	}
	if !validInternalSchedulingResult(req.OperationType, created, expectedSegments, expectedTargetVersion) {
		return nil, scheduling.ErrInvalidSchedulingResult
	}
	children := make([]scheduling.ConfirmedAppointmentSegment, 0, len(created.Children))
	for _, child := range created.Children {
		allocations := make([]scheduling.ConfirmedResourceAllocation, 0, len(child.ResourceAllocations))
		for _, allocation := range child.ResourceAllocations {
			allocations = append(allocations, scheduling.ConfirmedResourceAllocation{
				ResourcePoolID: allocation.ResourcePoolID, ResourceName: allocation.ResourceName, UnitsAllocated: allocation.UnitsAllocated,
			})
		}
		children = append(children, scheduling.ConfirmedAppointmentSegment{
			AppointmentServiceID: child.AppointmentServiceID, GuestReference: child.GuestReference,
			ServiceID: child.ServiceID, StaffID: child.StaffID, StaffSelectionMode: child.StaffSelectionMode, Quantity: child.Quantity,
			ScheduledStartTime: child.ScheduledStartTime, ScheduledEndTime: child.ScheduledEndTime,
			OccupiedStartTime: child.OccupiedStartTime, OccupiedEndTime: child.OccupiedEndTime,
			BufferBeforeMinutes: child.BufferBeforeMinutes, BufferAfterMinutes: child.BufferAfterMinutes, ResourceAllocations: allocations,
		})
	}
	return &scheduling.ActionResult{
		Kind: scheduling.ActionKindConfirmedAppointment, OperationType: req.OperationType,
		SchedulingAuthority: e.SchedulingAuthority(), Replayed: replayed,
		TargetAuthorityAppointmentVersion: created.TargetAuthorityAppointmentVersion,
		AuthorityAppointmentVersion:       created.AuthorityAppointmentVersion,
		ConfirmedAppointment: &scheduling.ConfirmedAppointmentResult{
			AppointmentID: created.AppointmentID, BookingAttemptID: created.BookingAttemptID,
			AppointmentStatus: created.AppointmentStatus,
			ActiveChildCount:  created.ActiveChildCount,
			Children:          children,
		},
	}, nil
}

func expectedInternalAppointmentStatus(operation scheduling.OperationKind) string {
	switch operation {
	case scheduling.OperationKindReschedule:
		return booking.StatusRescheduled
	case scheduling.OperationKindCancel:
		return booking.StatusCancelled
	default:
		return booking.StatusConfirmed
	}
}

func validInternalSchedulingResult(
	operation scheduling.OperationKind,
	result *InternalCreateResult,
	expectedSegments []InternalCreateSegment,
	expectedTargetVersion int,
) bool {
	if result == nil || strings.TrimSpace(result.AppointmentID) == "" ||
		strings.TrimSpace(result.BookingAttemptID) == "" ||
		result.AppointmentStatus != expectedInternalAppointmentStatus(operation) ||
		len(result.Children) == 0 {
		return false
	}
	switch operation {
	case scheduling.OperationKindBook:
		if result.TargetAuthorityAppointmentVersion != 0 ||
			result.AuthorityAppointmentVersion != 1 ||
			result.ActiveChildCount != len(result.Children) {
			return false
		}
	case scheduling.OperationKindReschedule:
		if expectedTargetVersion < 1 ||
			result.TargetAuthorityAppointmentVersion != expectedTargetVersion ||
			result.AuthorityAppointmentVersion != expectedTargetVersion+1 ||
			result.ActiveChildCount != len(result.Children) {
			return false
		}
	case scheduling.OperationKindCancel:
		if expectedTargetVersion < 1 ||
			result.TargetAuthorityAppointmentVersion != expectedTargetVersion ||
			result.AuthorityAppointmentVersion != expectedTargetVersion+1 ||
			result.ActiveChildCount != 0 {
			return false
		}
	default:
		return false
	}
	if operation != scheduling.OperationKindCancel && len(result.Children) != len(expectedSegments) {
		return false
	}
	for index, child := range result.Children {
		if !validInternalSchedulingChild(child) {
			return false
		}
		if operation != scheduling.OperationKindCancel && !internalSchedulingChildMatchesSegment(child, expectedSegments[index]) {
			return false
		}
	}
	return true
}

func validInternalSchedulingChild(child InternalCreateSegmentResult) bool {
	if strings.TrimSpace(child.AppointmentServiceID) == "" ||
		strings.TrimSpace(child.ServiceID) == "" || strings.TrimSpace(child.StaffID) == "" ||
		(child.StaffSelectionMode != booking.StaffSelectionSpecific && child.StaffSelectionMode != booking.StaffSelectionAnyone) ||
		child.Quantity != 1 || child.ScheduledStartTime.IsZero() ||
		!child.ScheduledEndTime.After(child.ScheduledStartTime) ||
		child.OccupiedStartTime.IsZero() || !child.OccupiedEndTime.After(child.OccupiedStartTime) ||
		child.OccupiedStartTime.After(child.ScheduledStartTime) ||
		child.OccupiedEndTime.Before(child.ScheduledEndTime) ||
		child.BufferBeforeMinutes < 0 || child.BufferAfterMinutes < 0 {
		return false
	}
	for _, allocation := range child.ResourceAllocations {
		if strings.TrimSpace(allocation.ResourcePoolID) == "" || allocation.UnitsAllocated <= 0 {
			return false
		}
	}
	return true
}

func internalSchedulingChildMatchesSegment(child InternalCreateSegmentResult, segment InternalCreateSegment) bool {
	return strings.TrimSpace(child.ServiceID) == strings.TrimSpace(segment.ServiceID) &&
		strings.TrimSpace(child.StaffID) == strings.TrimSpace(segment.StaffID) &&
		child.StaffSelectionMode == segment.StaffSelectionMode &&
		strings.TrimSpace(child.GuestReference) == strings.TrimSpace(segment.GuestReference) &&
		child.Quantity == segment.Quantity &&
		child.ScheduledStartTime.Equal(segment.RequestedStartTime) &&
		child.ScheduledEndTime.Equal(segment.RequestedEndTime)
}

func normalizeInternalLifecycleRequest(req scheduling.ActionRequest) (InternalLifecycleRequest, error) {
	result := InternalLifecycleRequest{
		OperationType: req.OperationType, OperationKey: strings.TrimSpace(req.OperationKey),
		AvailabilityQuoteID: strings.TrimSpace(req.AvailabilityQuoteID), SlotFingerprint: strings.TrimSpace(req.SlotFingerprint),
		Source: strings.TrimSpace(req.Source), CustomerName: strings.TrimSpace(req.CustomerName),
		CustomerPhone: strings.TrimSpace(req.CustomerPhone), CustomerEmail: strings.TrimSpace(req.CustomerEmail),
		RequestedStartTime: req.RequestedStartTime.UTC(), RequestedEndTime: req.RequestedEndTime.UTC(),
		RequestedTimezone: strings.TrimSpace(req.RequestedTimezone), Notes: strings.TrimSpace(req.Notes),
		TargetAppointmentID:                       strings.TrimSpace(req.TargetAppointmentID),
		ExpectedTargetAuthorityAppointmentVersion: req.ExpectedTargetAuthorityAppointmentVersion,
	}
	if result.OperationKey == "" || len(result.OperationKey) > 256 || result.Source == "" ||
		!validUUID(result.TargetAppointmentID) || result.ExpectedTargetAuthorityAppointmentVersion < 1 ||
		strings.TrimSpace(req.RetryOfAttemptID) != "" ||
		(req.TargetAuthority != "" && req.TargetAuthority != booking.SchedulingAuthorityManleAICalendar) ||
		(strings.TrimSpace(req.CallSessionID) != "" && !validUUID(req.CallSessionID)) {
		return InternalLifecycleRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	switch req.OperationType {
	case scheduling.OperationKindReschedule:
		planRequest := req
		planRequest.OperationType = scheduling.OperationKindBook
		planRequest.TargetAppointmentID = ""
		planRequest.TargetAuthority = ""
		planRequest.ExpectedTargetAuthorityAppointmentVersion = 0
		normalizedPlan, err := normalizeInternalCreateRequest(planRequest)
		if err != nil {
			return InternalLifecycleRequest{}, err
		}
		result.CustomerName = normalizedPlan.CustomerName
		result.CustomerPhone = normalizedPlan.CustomerPhone
		result.CustomerEmail = normalizedPlan.CustomerEmail
		result.PartySize = normalizedPlan.PartySize
		result.Segments = normalizedPlan.Segments
		result.RequestedStartTime = normalizedPlan.RequestedStartTime
		result.RequestedEndTime = normalizedPlan.RequestedEndTime
		result.RequestedTimezone = normalizedPlan.RequestedTimezone
		if !validUUID(result.AvailabilityQuoteID) || !validFingerprint(result.SlotFingerprint) {
			return InternalLifecycleRequest{}, scheduling.ErrInvalidSchedulingAction
		}
	case scheduling.OperationKindCancel:
		if result.AvailabilityQuoteID != "" || result.SlotFingerprint != "" || req.PartySize != 0 || len(req.Segments) != 0 ||
			!req.RequestedStartTime.IsZero() || !req.RequestedEndTime.IsZero() || result.RequestedTimezone != "" {
			return InternalLifecycleRequest{}, scheduling.ErrInvalidSchedulingAction
		}
	default:
		return InternalLifecycleRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	result.RequestFingerprint = hashCalendarValue(struct {
		OperationType                             scheduling.OperationKind `json:"operation_type"`
		OperationKey                              string                   `json:"operation_key"`
		Source                                    string                   `json:"source"`
		TargetAppointmentID                       string                   `json:"target_appointment_id"`
		ExpectedTargetAuthorityAppointmentVersion int                      `json:"expected_target_authority_appointment_version"`
		AvailabilityQuoteID                       string                   `json:"availability_quote_id,omitempty"`
		SlotFingerprint                           string                   `json:"slot_fingerprint,omitempty"`
		CustomerName                              string                   `json:"customer_name,omitempty"`
		CustomerPhone                             string                   `json:"customer_phone,omitempty"`
		CustomerEmail                             string                   `json:"customer_email,omitempty"`
		PartySize                                 int                      `json:"party_size,omitempty"`
		Segments                                  []InternalCreateSegment  `json:"segments,omitempty"`
		StartTime                                 string                   `json:"start_time,omitempty"`
		EndTime                                   string                   `json:"end_time,omitempty"`
		Timezone                                  string                   `json:"timezone,omitempty"`
		Notes                                     string                   `json:"notes,omitempty"`
	}{
		OperationType: result.OperationType, OperationKey: result.OperationKey, Source: result.Source,
		TargetAppointmentID:                       result.TargetAppointmentID,
		ExpectedTargetAuthorityAppointmentVersion: result.ExpectedTargetAuthorityAppointmentVersion,
		AvailabilityQuoteID:                       result.AvailabilityQuoteID, SlotFingerprint: result.SlotFingerprint,
		CustomerName: result.CustomerName, CustomerPhone: result.CustomerPhone, CustomerEmail: result.CustomerEmail,
		PartySize: result.PartySize, Segments: result.Segments,
		StartTime: formatOptionalTime(result.RequestedStartTime), EndTime: formatOptionalTime(result.RequestedEndTime),
		Timezone: result.RequestedTimezone, Notes: result.Notes,
	})
	return result, nil
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func normalizeInternalCreateRequest(req scheduling.ActionRequest) (InternalCreateRequest, error) {
	result := InternalCreateRequest{
		OperationKey: strings.TrimSpace(req.OperationKey), AvailabilityQuoteID: strings.TrimSpace(req.AvailabilityQuoteID),
		SlotFingerprint: strings.TrimSpace(req.SlotFingerprint), Source: strings.TrimSpace(req.Source),
		CustomerName: strings.TrimSpace(req.CustomerName), CustomerPhone: strings.TrimSpace(req.CustomerPhone),
		CustomerEmail: strings.TrimSpace(req.CustomerEmail), RequestedStartTime: req.RequestedStartTime.UTC(),
		RequestedEndTime: req.RequestedEndTime.UTC(), RequestedTimezone: strings.TrimSpace(req.RequestedTimezone),
		Notes: strings.TrimSpace(req.Notes),
	}
	partySize := req.PartySize
	if partySize == 0 {
		partySize = 1
	}
	if req.OperationType != scheduling.OperationKindBook || partySize < 1 || len(req.Segments) < 1 ||
		strings.TrimSpace(req.RetryOfAttemptID) != "" || strings.TrimSpace(req.TargetAppointmentID) != "" ||
		strings.TrimSpace(req.CallSessionID) != "" && !validUUID(req.CallSessionID) {
		return InternalCreateRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	result.PartySize = partySize
	guestReferences := make([]string, 0, len(req.Segments))
	result.Segments = make([]InternalCreateSegment, 0, len(req.Segments))
	for _, raw := range req.Segments {
		quantity := raw.Quantity
		if quantity == 0 {
			quantity = 1
		}
		segment := InternalCreateSegment{
			ServiceID: strings.TrimSpace(raw.ServiceID), StaffID: strings.TrimSpace(raw.StaffID), StaffSelectionMode: strings.TrimSpace(raw.StaffSelectionMode),
			GuestReference: strings.TrimSpace(raw.GuestReference), Quantity: quantity,
			RequestedStartTime: raw.RequestedStartTime.UTC(), RequestedEndTime: raw.RequestedEndTime.UTC(),
		}
		if segment.StaffSelectionMode == "" {
			segment.StaffSelectionMode = booking.StaffSelectionSpecific
		}
		if quantity != 1 || !validUUID(segment.ServiceID) || !validUUID(segment.StaffID) ||
			(segment.StaffSelectionMode != booking.StaffSelectionSpecific && segment.StaffSelectionMode != booking.StaffSelectionAnyone) ||
			segment.RequestedStartTime.IsZero() || !segment.RequestedEndTime.After(segment.RequestedStartTime) || len(segment.GuestReference) > 200 {
			return InternalCreateRequest{}, scheduling.ErrInvalidSchedulingAction
		}
		guestReferences = append(guestReferences, segment.GuestReference)
		result.Segments = append(result.Segments, segment)
	}
	if !validGuestReferenceParty(partySize, guestReferences) {
		return InternalCreateRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	rootStart, rootEnd := result.Segments[0].RequestedStartTime, result.Segments[0].RequestedEndTime
	for _, segment := range result.Segments[1:] {
		if segment.RequestedStartTime.Before(rootStart) {
			rootStart = segment.RequestedStartTime
		}
		if segment.RequestedEndTime.After(rootEnd) {
			rootEnd = segment.RequestedEndTime
		}
	}
	if result.OperationKey == "" || len(result.OperationKey) > 256 || !validUUID(result.AvailabilityQuoteID) || !validFingerprint(result.SlotFingerprint) ||
		result.Source == "" || result.CustomerName == "" || result.CustomerPhone == "" || result.RequestedStartTime.IsZero() ||
		!result.RequestedEndTime.After(result.RequestedStartTime) || result.RequestedTimezone == "" ||
		!rootStart.Equal(result.RequestedStartTime) || !rootEnd.Equal(result.RequestedEndTime) {
		return InternalCreateRequest{}, scheduling.ErrInvalidSchedulingAction
	}
	fingerprintPayload := struct {
		OperationType       scheduling.OperationKind `json:"operation_type"`
		OperationKey        string                   `json:"operation_key"`
		Source              string                   `json:"source"`
		CustomerName        string                   `json:"customer_name"`
		CustomerPhone       string                   `json:"customer_phone"`
		CustomerEmail       string                   `json:"customer_email"`
		Segments            []InternalCreateSegment  `json:"segments"`
		StartTime           string                   `json:"start_time"`
		EndTime             string                   `json:"end_time"`
		Timezone            string                   `json:"timezone"`
		PartySize           int                      `json:"party_size"`
		Notes               string                   `json:"notes"`
		AvailabilityQuoteID string                   `json:"availability_quote_id"`
		SlotFingerprint     string                   `json:"slot_fingerprint"`
	}{
		OperationType: req.OperationType, OperationKey: result.OperationKey, Source: result.Source,
		CustomerName: result.CustomerName, CustomerPhone: result.CustomerPhone, CustomerEmail: result.CustomerEmail,
		Segments:  result.Segments,
		StartTime: result.RequestedStartTime.Format(time.RFC3339Nano), EndTime: result.RequestedEndTime.Format(time.RFC3339Nano),
		Timezone: result.RequestedTimezone, PartySize: partySize, Notes: result.Notes,
		AvailabilityQuoteID: result.AvailabilityQuoteID, SlotFingerprint: result.SlotFingerprint,
	}
	payload, _ := json.Marshal(fingerprintPayload)
	sum := sha256.Sum256(payload)
	result.RequestFingerprint = hex.EncodeToString(sum[:])
	return result, nil
}

func validUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
