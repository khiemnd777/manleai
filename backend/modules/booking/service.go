package booking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/pos"
)

var (
	ErrValidation          = errors.New("booking validation failed")
	ErrProviderUnavailable = errors.New("pos provider unavailable")
	ErrOperationConflict   = errors.New("booking operation key conflicts with a different request")
	ErrOperationInProgress = errors.New("booking operation is already in progress")
)

const bookingOperationLeaseDuration = 5 * time.Minute

type Store interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error)
	GetBookableService(ctx context.Context, salonID string, provider string, serviceID string) (*ServiceRef, error)
	GetBookableStaff(ctx context.Context, salonID string, provider string, staffID string) (*StaffRef, error)
	ListBookableStaffRefs(ctx context.Context, salonID string, provider string) ([]StaffRef, error)
	ResolveBookingCustomer(ctx context.Context, salonID string, provider string, name string, phone string, email string) (*CustomerRef, error)
	LinkBookingCustomer(ctx context.Context, salonID string, provider string, customerID string, customer pos.Customer) (*CustomerRef, error)
	GetSchedule(ctx context.Context, salonID string) (*Schedule, error)
	GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error)
	ListRescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req RescheduleLookupRequest) ([]AppointmentActionRef, error)
	ClaimPendingBookingAttempt(ctx context.Context, record PendingBookingRecord) (*BookingOperationClaim, error)
	MarkBookingOperationStarted(ctx context.Context, salonID string, attemptID string, processingToken string, leaseExpiresAt time.Time) error
	ExpireBookingOperationLeases(ctx context.Context, salonID string) error
	SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error)
	SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error)
	ClaimPendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingOperationClaim, error)
	SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error)
	SaveCancelledAppointment(ctx context.Context, record CancelledAppointmentRecord) (*Appointment, error)
	SaveAppointmentActionFallback(ctx context.Context, record AppointmentActionFallbackRecord) (*BookingAttempt, error)
	LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error)
	ListAppointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) ([]Appointment, error)
	ListBookingAttempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]BookingAttempt, error)
	ListCalendarAppointments(ctx context.Context, salonID string, ownerUserID string, startTime time.Time, endTime time.Time) ([]Appointment, error)
	ListCalendarPendingRequests(ctx context.Context, salonID string, ownerUserID string, startTime time.Time, endTime time.Time) ([]BookingAttempt, error)
	ListCalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor CalendarEventCursor, limit int) ([]CalendarEvent, error)
	UpsertCalendarAppointments(ctx context.Context, salonID string, provider string, items []CalendarAppointmentImport) (CalendarSyncSummary, error)
	CreateCalendarSyncLog(ctx context.Context, salonID string, provider string) (string, error)
	CompleteCalendarSyncLog(ctx context.Context, id string, status string, message string) error
	MarkCalendarSyncComplete(ctx context.Context, salonID string, provider string, status string, message string) error
	LogPOSError(ctx context.Context, salonID string, provider string, operation string, err error) error
}

type Service struct {
	store     Store
	providers map[string]pos.POSProvider
}

type resolvedBookingSegment struct {
	Service            ServiceRef
	Staff              StaffRef
	StaffSelectionMode string
	SortOrder          int
}

type resolvedAvailabilitySegment struct {
	Service            ServiceRef
	Staff              *StaffRef
	StaffSelectionMode string
	SortOrder          int
}

func NewService(store Store, providers []pos.POSProvider) *Service {
	byName := make(map[string]pos.POSProvider, len(providers))
	for _, provider := range providers {
		if provider != nil {
			byName[provider.Name()] = provider
		}
	}
	return &Service{store: store, providers: byName}
}

func (s *Service) Create(ctx context.Context, salonID string, ownerUserID string, req CreateBookingRequest) (*BookingAttempt, error) {
	req = normalizeRequest(req)
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	activeProvider, err := s.store.GetActiveProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	resolvedSegments, err := s.resolveBookingSegments(ctx, salonID, activeProvider, req)
	if err != nil {
		return nil, err
	}
	primary := resolvedSegments[0]

	provider := s.providers[activeProvider]
	if provider == nil {
		return nil, ErrProviderUnavailable
	}

	durationMinutes := bookingSegmentsDuration(resolvedSegments)
	endTime := req.StartTime.Add(time.Duration(durationMinutes) * time.Minute)
	segments := bookingSegmentRecords(resolvedSegments)
	processingToken := uuid.NewString()
	leaseExpiresAt := time.Now().UTC().Add(bookingOperationLeaseDuration)
	operationKey := bookingOperationKey(req.OperationKey)
	claim, err := s.store.ClaimPendingBookingAttempt(ctx, PendingBookingRecord{
		SalonID:            salonID,
		Source:             req.Source,
		Provider:           provider.Name(),
		POSIdempotencyKey:  newPOSIdempotencyKey(),
		OperationKey:       operationKey,
		RequestFingerprint: createRequestFingerprint(provider.Name(), req, resolvedSegments),
		ProcessingToken:    processingToken,
		LeaseExpiresAt:     leaseExpiresAt,
		CustomerName:       req.CustomerName,
		CustomerPhone:      req.CustomerPhone,
		CustomerEmail:      req.CustomerEmail,
		Service:            primary.Service,
		Staff:              primary.Staff,
		StaffSelectionMode: req.StaffSelectionMode,
		Segments:           segments,
		StartTime:          req.StartTime,
		EndTime:            endTime,
		Notes:              req.Notes,
	})
	if err != nil {
		return nil, err
	}
	if claim == nil || claim.Attempt == nil {
		return nil, ErrOperationInProgress
	}
	if !claim.Acquired {
		return claim.Attempt, nil
	}
	pending := claim.Attempt
	if err := s.store.MarkBookingOperationStarted(ctx, salonID, pending.ID, processingToken, leaseExpiresAt); err != nil {
		return nil, err
	}

	persistCtx := postPOSPersistenceContext(ctx)
	customerRef, err := s.store.ResolveBookingCustomer(ctx, salonID, provider.Name(), req.CustomerName, req.CustomerPhone, req.CustomerEmail)
	if err != nil {
		return s.saveFallback(persistCtx, *pending, segments, req, endTime, "resolve_customer", err, false)
	}
	posCustomer, operation, err := s.resolvePOSCustomer(ctx, salonID, provider, *customerRef, req)
	if err != nil {
		return s.saveFallback(persistCtx, *pending, segments, req, endTime, operation, err, operation == "create_customer" && isUncertainProviderError(err))
	}

	appointment, err := provider.CreateAppointment(ctx, salonID, pos.CreateAppointmentInput{
		IdempotencyKey:  pending.POSIdempotencyKey,
		CustomerID:      posCustomer.POSCustomerID,
		ServiceID:       primary.Service.POSServiceID,
		ServiceVersion:  primary.Service.POSServiceVersion,
		StaffID:         primary.Staff.POSStaffID,
		StartTime:       req.StartTime,
		DurationMinutes: durationMinutes,
		Notes:           req.Notes,
		Segments:        posAppointmentSegments(resolvedSegments),
	})
	if err != nil {
		return s.saveFallback(persistCtx, *pending, segments, req, endTime, "create_booking", err, isUncertainProviderError(err))
	}
	if appointment == nil || strings.TrimSpace(appointment.POSAppointmentID) == "" {
		return s.saveFallback(persistCtx, *pending, segments, req, endTime, "create_booking", fmt.Errorf("pos booking id was not returned"), true)
	}
	if appointment.POSAppointmentVersion < 0 {
		return s.saveFallback(persistCtx, *pending, segments, req, endTime, "create_booking", fmt.Errorf("pos booking version was not returned"), true)
	}
	if !appointment.EndTime.IsZero() {
		endTime = appointment.EndTime
	}
	startTime := req.StartTime
	if !appointment.StartTime.IsZero() {
		startTime = appointment.StartTime
	}

	return s.store.SaveConfirmedBooking(persistCtx, ConfirmedBookingRecord{
		AttemptID:          pending.ID,
		SalonID:            salonID,
		Source:             req.Source,
		Provider:           provider.Name(),
		CustomerName:       req.CustomerName,
		CustomerPhone:      req.CustomerPhone,
		CustomerEmail:      req.CustomerEmail,
		Service:            primary.Service,
		Staff:              primary.Staff,
		StaffSelectionMode: req.StaffSelectionMode,
		Segments:           segments,
		StartTime:          startTime,
		EndTime:            endTime,
		Notes:              req.Notes,
		POSBookingID:       appointment.POSAppointmentID,
		POSBookingVersion:  appointment.POSAppointmentVersion,
		ProcessingToken:    processingToken,
	})
}

func (s *Service) Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req RescheduleRequest) (*Appointment, *BookingAttempt, error) {
	req = normalizeRescheduleRequest(req)
	if req.StartTime.IsZero() || !validOperationKey(req.OperationKey) {
		return nil, nil, ErrValidation
	}
	appointment, err := s.store.GetAppointmentForOwner(ctx, salonID, ownerUserID, strings.TrimSpace(appointmentID))
	if err != nil {
		return nil, nil, err
	}
	if appointment.Status == StatusCancelled || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion < 0 {
		return nil, nil, ErrValidation
	}
	segments := appointmentActionSegments(*appointment)
	staff := appointment.Staff
	if req.StaffID != "" {
		staffOverride := appointment.Staff
		if req.StaffID != appointment.Staff.ID {
			nextStaff, err := s.store.GetBookableStaff(ctx, salonID, appointment.POSProvider, req.StaffID)
			if err != nil {
				return nil, nil, err
			}
			staffOverride = *nextStaff
		}
		if staffOverride.POSProvider != appointment.POSProvider {
			return nil, nil, ErrValidation
		}
		staff = staffOverride
		segments = applyStaffToBookingSegments(segments, staffOverride)
	}
	if err := validateAppointmentActionSegments(*appointment, segments); err != nil {
		return nil, nil, err
	}
	primary := segments[0]
	if req.StaffID == "" {
		staff = primary.Staff
	}
	if strings.TrimSpace(staff.POSStaffID) == "" {
		return nil, nil, ErrValidation
	}

	provider := s.providers[appointment.POSProvider]
	if provider == nil {
		return nil, nil, ErrProviderUnavailable
	}

	durationMinutes := bookingSegmentRecordsDuration(segments)
	if durationMinutes <= 0 {
		return nil, nil, ErrValidation
	}
	notes := req.Notes
	if notes == "" {
		notes = appointment.Notes
	}
	endTime := req.StartTime.Add(time.Duration(durationMinutes) * time.Minute)
	processingToken := uuid.NewString()
	leaseExpiresAt := time.Now().UTC().Add(bookingOperationLeaseDuration)
	claim, err := s.store.ClaimPendingAppointmentAction(ctx, PendingAppointmentActionRecord{
		SalonID:            salonID,
		Appointment:        *appointment,
		Provider:           appointment.POSProvider,
		Source:             req.Source,
		OperationKey:       bookingOperationKey(req.OperationKey),
		RequestFingerprint: appointmentActionFingerprint(BookingActionReschedule, *appointment, req.StartTime, segments, notes),
		OperationType:      BookingActionReschedule,
		ProcessingToken:    processingToken,
		LeaseExpiresAt:     leaseExpiresAt,
		Segments:           segments,
		RequestedStartTime: req.StartTime,
		RequestedEndTime:   endTime,
		Notes:              notes,
		POSIdempotencyKey:  newPOSIdempotencyKey(),
	})
	if err != nil {
		return nil, nil, err
	}
	if claim == nil || claim.Attempt == nil {
		return nil, nil, ErrOperationInProgress
	}
	if !claim.Acquired {
		return appointmentActionClaimResult(BookingActionReschedule, claim.Attempt)
	}
	pending := claim.Attempt
	if err := s.store.MarkBookingOperationStarted(ctx, salonID, pending.ID, processingToken, leaseExpiresAt); err != nil {
		return nil, nil, err
	}

	persistCtx := postPOSPersistenceContext(ctx)
	posAppointment, err := provider.RescheduleAppointment(ctx, salonID, appointment.POSAppointmentID, pos.RescheduleInput{
		IdempotencyKey:  pending.POSIdempotencyKey,
		BookingVersion:  appointment.POSAppointmentVersion,
		ServiceID:       primary.Service.POSServiceID,
		ServiceVersion:  primary.Service.POSServiceVersion,
		StaffID:         primary.Staff.POSStaffID,
		StartTime:       req.StartTime,
		DurationMinutes: durationMinutes,
		Notes:           notes,
		Segments:        posAppointmentSegmentsFromRecords(segments),
	})
	if err != nil {
		fallback, saveErr := s.saveActionFallback(persistCtx, pending.ID, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, segments, req.StartTime, endTime, notes, err, processingToken, isUncertainProviderError(err))
		return nil, fallback, saveErr
	}
	if posAppointment == nil || strings.TrimSpace(posAppointment.POSAppointmentID) == "" {
		fallback, saveErr := s.saveActionFallback(persistCtx, pending.ID, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, segments, req.StartTime, endTime, notes, fmt.Errorf("pos booking id was not returned"), processingToken, true)
		return nil, fallback, saveErr
	}
	if posAppointment.POSAppointmentVersion < 0 {
		fallback, saveErr := s.saveActionFallback(persistCtx, pending.ID, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, segments, req.StartTime, endTime, notes, fmt.Errorf("pos booking version was not returned"), processingToken, true)
		return nil, fallback, saveErr
	}
	startTime := req.StartTime
	if !posAppointment.StartTime.IsZero() {
		startTime = posAppointment.StartTime
	}
	if !posAppointment.EndTime.IsZero() {
		endTime = posAppointment.EndTime
	}

	saved, err := s.store.SaveRescheduledAppointment(persistCtx, RescheduledAppointmentRecord{
		AttemptID:         pending.ID,
		Appointment:       *appointment,
		Staff:             primary.Staff,
		Source:            req.Source,
		Segments:          segments,
		StartTime:         startTime,
		EndTime:           endTime,
		Notes:             notes,
		POSBookingVersion: posAppointment.POSAppointmentVersion,
		ProcessingToken:   processingToken,
	})
	return saved, nil, err
}

func (s *Service) RescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req RescheduleLookupRequest) ([]AppointmentActionRef, error) {
	req = normalizeRescheduleLookupRequest(req)
	if req.CustomerPhone == "" {
		return nil, ErrValidation
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	return s.store.ListRescheduleCandidates(ctx, salonID, ownerUserID, req)
}

func (s *Service) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req AvailabilityRequest) (*AvailabilityResult, error) {
	req = normalizeAvailabilityRequest(req)
	if err := validateAvailabilityRequest(req); err != nil {
		return nil, err
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	activeProvider, err := s.store.GetActiveProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	resolvedSegments, err := s.resolveAvailabilitySegments(ctx, salonID, activeProvider, req)
	if err != nil {
		return nil, err
	}
	primary := resolvedSegments[0]
	durationMinutes := availabilitySegmentsDuration(resolvedSegments)
	provider := s.providers[activeProvider]
	if provider == nil {
		return nil, ErrProviderUnavailable
	}

	schedule, err := s.store.GetSchedule(ctx, salonID)
	if err != nil {
		return nil, err
	}
	result := availabilityResult(req, resolvedSegments, schedule, nil)
	businessHourPeriods := scheduleBusinessHourPeriods(schedule)
	if len(businessHourPeriods) == 0 {
		return result, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(schedule.Timezone))
	if err != nil {
		return nil, ErrValidation
	}

	staffByPOSID, err := s.bookableStaffByPOSID(ctx, salonID, primary.Service.POSProvider, nil)
	if err != nil {
		return nil, err
	}
	if len(staffByPOSID) == 0 {
		return result, nil
	}

	slots, err := provider.CheckAvailability(ctx, salonID, pos.AvailabilityInput{
		ServiceID:       primary.Service.POSServiceID,
		StaffID:         availabilityPrimaryStaffID(primary),
		PreferredDate:   req.PreferredDate,
		Timezone:        schedule.Timezone,
		DurationMinutes: durationMinutes,
		Segments:        posAvailabilitySegments(resolvedSegments),
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(slots, func(i, j int) bool {
		return slots[i].StartTime.Before(slots[j].StartTime)
	})

	filtered := make([]AvailabilitySlot, 0, availabilityLimit(req.Limit))
	for _, slot := range slots {
		if len(filtered) >= availabilityLimit(req.Limit) {
			break
		}
		startTime := slot.StartTime.UTC()
		endTime := slot.EndTime.UTC()
		if endTime.IsZero() && !startTime.IsZero() {
			endTime = startTime.Add(time.Duration(durationMinutes) * time.Minute)
		}
		if startTime.IsZero() || !endTime.After(startTime) {
			continue
		}
		slotSegments, ok := availabilitySlotSegments(slot, resolvedSegments, staffByPOSID)
		if !ok {
			continue
		}
		if !withinBusinessHourPeriods(startTime, endTime, businessHourPeriods, loc) {
			continue
		}
		filtered = append(filtered, AvailabilitySlot{
			StartTime:          startTime,
			EndTime:            endTime,
			StaffID:            slotSegments[0].StaffID,
			StaffName:          slotSegments[0].StaffName,
			StaffSelectionMode: req.StaffSelectionMode,
			Segments:           slotSegments,
		})
	}
	result.Slots = filtered
	return result, nil
}

func (s *Service) Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req CancelRequest) (*Appointment, *BookingAttempt, error) {
	req = normalizeCancelRequest(req)
	if !validOperationKey(req.OperationKey) {
		return nil, nil, ErrValidation
	}
	appointment, err := s.store.GetAppointmentForOwner(ctx, salonID, ownerUserID, strings.TrimSpace(appointmentID))
	if err != nil {
		return nil, nil, err
	}
	if appointment.Status == StatusCancelled || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion < 0 {
		return nil, nil, ErrValidation
	}
	segments := appointmentActionSegments(*appointment)

	provider := s.providers[appointment.POSProvider]
	if provider == nil {
		return nil, nil, ErrProviderUnavailable
	}

	processingToken := uuid.NewString()
	leaseExpiresAt := time.Now().UTC().Add(bookingOperationLeaseDuration)
	claim, err := s.store.ClaimPendingAppointmentAction(ctx, PendingAppointmentActionRecord{
		SalonID:            salonID,
		Appointment:        *appointment,
		Provider:           appointment.POSProvider,
		Source:             req.Source,
		OperationKey:       bookingOperationKey(req.OperationKey),
		RequestFingerprint: appointmentActionFingerprint(BookingActionCancel, *appointment, appointment.StartTime, segments, req.Reason),
		OperationType:      BookingActionCancel,
		ProcessingToken:    processingToken,
		LeaseExpiresAt:     leaseExpiresAt,
		Segments:           segments,
		RequestedStartTime: appointment.StartTime,
		RequestedEndTime:   appointment.EndTime,
		Notes:              req.Reason,
		POSIdempotencyKey:  newPOSIdempotencyKey(),
	})
	if err != nil {
		return nil, nil, err
	}
	if claim == nil || claim.Attempt == nil {
		return nil, nil, ErrOperationInProgress
	}
	if !claim.Acquired {
		return appointmentActionClaimResult(BookingActionCancel, claim.Attempt)
	}
	pending := claim.Attempt
	if err := s.store.MarkBookingOperationStarted(ctx, salonID, pending.ID, processingToken, leaseExpiresAt); err != nil {
		return nil, nil, err
	}

	persistCtx := postPOSPersistenceContext(ctx)
	posAppointment, err := provider.CancelAppointment(ctx, salonID, appointment.POSAppointmentID, pos.CancelInput{
		IdempotencyKey: pending.POSIdempotencyKey,
		BookingVersion: appointment.POSAppointmentVersion,
		Reason:         req.Reason,
	})
	if err != nil {
		fallback, saveErr := s.saveActionFallback(persistCtx, pending.ID, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, segments, appointment.StartTime, appointment.EndTime, req.Reason, err, processingToken, isUncertainProviderError(err))
		return nil, fallback, saveErr
	}
	if posAppointment == nil || strings.TrimSpace(posAppointment.POSAppointmentID) == "" {
		fallback, saveErr := s.saveActionFallback(persistCtx, pending.ID, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, segments, appointment.StartTime, appointment.EndTime, req.Reason, fmt.Errorf("pos booking id was not returned"), processingToken, true)
		return nil, fallback, saveErr
	}
	if posAppointment.POSAppointmentVersion < 0 {
		fallback, saveErr := s.saveActionFallback(persistCtx, pending.ID, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, segments, appointment.StartTime, appointment.EndTime, req.Reason, fmt.Errorf("pos booking version was not returned"), processingToken, true)
		return nil, fallback, saveErr
	}

	saved, err := s.store.SaveCancelledAppointment(persistCtx, CancelledAppointmentRecord{
		AttemptID:         pending.ID,
		Appointment:       *appointment,
		Source:            req.Source,
		Reason:            req.Reason,
		POSBookingVersion: posAppointment.POSAppointmentVersion,
		ProcessingToken:   processingToken,
	})
	return saved, nil, err
}

func (s *Service) Appointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) (*ListAppointmentsResponse, error) {
	pageLimit := clampLimit(limit)
	pageOffset := clampOffset(offset)
	items, err := s.store.ListAppointments(ctx, salonID, ownerUserID, pageLimit+1, pageOffset)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > pageLimit
	if hasMore {
		items = items[:pageLimit]
	}
	return &ListAppointmentsResponse{
		Appointments: items,
		Limit:        pageLimit,
		Offset:       pageOffset,
		HasMore:      hasMore,
	}, nil
}

func (s *Service) Attempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*ListBookingAttemptsResponse, error) {
	pageStatus, err := normalizeAttemptStatusFilter(status)
	if err != nil {
		return nil, err
	}
	pageLimit := clampLimit(limit)
	pageOffset := clampOffset(offset)
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := s.store.ExpireBookingOperationLeases(ctx, salonID); err != nil {
		return nil, err
	}
	items, err := s.store.ListBookingAttempts(ctx, salonID, ownerUserID, pageStatus, pageLimit+1, pageOffset)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > pageLimit
	if hasMore {
		items = items[:pageLimit]
	}
	return &ListBookingAttemptsResponse{
		BookingAttempts: items,
		Limit:           pageLimit,
		Offset:          pageOffset,
		HasMore:         hasMore,
		Status:          pageStatus,
	}, nil
}

func (s *Service) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := s.store.ExpireBookingOperationLeases(ctx, salonID); err != nil {
		return nil, err
	}
	return s.store.LatestTestBooking(ctx, salonID, ownerUserID)
}

func (s *Service) Calendar(ctx context.Context, salonID string, ownerUserID string, req CalendarRangeRequest) (*CalendarRangeResponse, error) {
	req = normalizeCalendarRangeRequest(req)
	if err := validateCalendarRange(req); err != nil {
		return nil, err
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if err := s.store.ExpireBookingOperationLeases(ctx, salonID); err != nil {
		return nil, err
	}
	appointments, err := s.store.ListCalendarAppointments(ctx, salonID, ownerUserID, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	pending, err := s.store.ListCalendarPendingRequests(ctx, salonID, ownerUserID, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	appointments = annotateCalendarAppointments(appointments)
	pending = annotateCalendarPendingRequests(pending)
	return &CalendarRangeResponse{
		SalonID:         salonID,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		View:            req.View,
		Appointments:    appointments,
		PendingRequests: pending,
		Warnings:        calendarWarningSummary(appointments, pending),
	}, nil
}

func (s *Service) EnsureCalendarEventAccess(ctx context.Context, salonID string, ownerUserID string) error {
	return s.store.EnsureSalonOwner(ctx, salonID, ownerUserID)
}

func (s *Service) CalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor CalendarEventCursor, limit int) ([]CalendarEvent, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	if cursor.CreatedAt.IsZero() {
		cursor.CreatedAt = time.Now().UTC()
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	events, err := s.store.ListCalendarEvents(ctx, salonID, ownerUserID, cursor, limit)
	if err != nil {
		return nil, err
	}
	for idx := range events {
		events[idx].Cursor = calendarEventCursor(events[idx].CreatedAt, events[idx].ID)
	}
	return events, nil
}

func (s *Service) SyncCalendar(ctx context.Context, salonID string, ownerUserID string, req CalendarSyncRequest) (*CalendarSyncResponse, error) {
	rangeReq := normalizeCalendarRangeRequest(CalendarRangeRequest{StartTime: req.StartTime, EndTime: req.EndTime})
	if err := validateCalendarRange(rangeReq); err != nil {
		return nil, err
	}
	providerName, err := s.store.GetActiveProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	provider := s.providers[providerName]
	if provider == nil {
		return nil, ErrProviderUnavailable
	}
	lister, ok := provider.(pos.AppointmentListProvider)
	if !ok {
		return nil, ErrProviderUnavailable
	}

	logID, logErr := s.store.CreateCalendarSyncLog(ctx, salonID, providerName)
	if logErr != nil {
		return nil, logErr
	}

	imported := make([]CalendarAppointmentImport, 0)
	skipped := 0
	cursor := ""
	for page := 0; page < 20; page++ {
		result, err := lister.ListAppointments(ctx, salonID, pos.AppointmentListInput{
			StartTime: rangeReq.StartTime,
			EndTime:   rangeReq.EndTime,
			Limit:     100,
			Cursor:    cursor,
		})
		if err != nil {
			_ = s.store.LogPOSError(context.WithoutCancel(ctx), salonID, providerName, "calendar_sync", err)
			_ = s.store.CompleteCalendarSyncLog(context.WithoutCancel(ctx), logID, "failed", err.Error())
			_ = s.store.MarkCalendarSyncComplete(context.WithoutCancel(ctx), salonID, providerName, pos.StatusError, err.Error())
			return nil, err
		}
		if result == nil {
			break
		}
		for _, item := range result.Appointments {
			mapped, ok := calendarImportFromPOSAppointment(providerName, item)
			if !ok {
				skipped++
				continue
			}
			imported = append(imported, mapped)
		}
		cursor = strings.TrimSpace(result.Cursor)
		if cursor == "" {
			break
		}
	}

	summary, err := s.store.UpsertCalendarAppointments(postPOSPersistenceContext(ctx), salonID, providerName, imported)
	if err != nil {
		_ = s.store.CompleteCalendarSyncLog(context.WithoutCancel(ctx), logID, "failed", err.Error())
		_ = s.store.MarkCalendarSyncComplete(context.WithoutCancel(ctx), salonID, providerName, pos.StatusError, err.Error())
		return nil, err
	}
	summary.Skipped += skipped
	message := calendarSyncSummaryMessage(summary)
	if err := s.store.CompleteCalendarSyncLog(postPOSPersistenceContext(ctx), logID, "succeeded", message); err != nil {
		return nil, err
	}
	if err := s.store.MarkCalendarSyncComplete(postPOSPersistenceContext(ctx), salonID, providerName, pos.StatusActive, ""); err != nil {
		return nil, err
	}
	return &CalendarSyncResponse{
		Provider: providerName,
		Summary:  summary,
		Range: CalendarSyncRange{
			StartTime: rangeReq.StartTime,
			EndTime:   rangeReq.EndTime,
		},
	}, nil
}

func (s *Service) saveFallback(ctx context.Context, pending BookingAttempt, segments []BookingSegmentRecord, req CreateBookingRequest, endTime time.Time, operation string, providerErr error, uncertain bool) (*BookingAttempt, error) {
	primary := segments[0]
	outcome, retryPolicy, reconciliation := fallbackOperationPolicy(uncertain)
	return s.store.SaveFallbackBooking(ctx, FallbackBookingRecord{
		AttemptID:          pending.ID,
		SalonID:            pending.SalonID,
		Source:             pending.Source,
		Provider:           pending.POSProvider,
		Operation:          operation,
		CustomerName:       req.CustomerName,
		CustomerPhone:      req.CustomerPhone,
		CustomerEmail:      req.CustomerEmail,
		Service:            primary.Service,
		Staff:              primary.Staff,
		StaffSelectionMode: req.StaffSelectionMode,
		Segments:           segments,
		StartTime:          req.StartTime,
		EndTime:            endTime,
		Notes:              req.Notes,
		ErrorCode:          posErrorCode(providerErr),
		ErrorMessage:       providerErr.Error(),
		ProcessingToken:    pending.ProcessingToken,
		ProviderOutcome:    outcome,
		RetryPolicy:        retryPolicy,
		Reconciliation:     reconciliation,
	})
}

func (s *Service) saveActionFallback(ctx context.Context, attemptID string, salonID string, appointment AppointmentActionRef, provider string, operation string, source string, notificationType string, segments []BookingSegmentRecord, startTime time.Time, endTime time.Time, notes string, providerErr error, processingToken string, uncertain bool) (*BookingAttempt, error) {
	if strings.TrimSpace(source) == "" {
		source = SourceOwnerDashboard
	}
	outcome, retryPolicy, reconciliation := fallbackOperationPolicy(uncertain)
	return s.store.SaveAppointmentActionFallback(ctx, AppointmentActionFallbackRecord{
		AttemptID:          attemptID,
		SalonID:            salonID,
		Appointment:        appointment,
		Provider:           provider,
		Operation:          operation,
		Source:             source,
		NotificationType:   notificationType,
		Segments:           segments,
		RequestedStartTime: startTime,
		RequestedEndTime:   endTime,
		Notes:              notes,
		ErrorCode:          posErrorCode(providerErr),
		ErrorMessage:       providerErr.Error(),
		ProcessingToken:    processingToken,
		ProviderOutcome:    outcome,
		RetryPolicy:        retryPolicy,
		Reconciliation:     reconciliation,
	})
}

func newPOSIdempotencyKey() string {
	return uuid.NewString()
}

func bookingOperationKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.NewString()
	}
	return value
}

func validOperationKey(value string) bool {
	return len(strings.TrimSpace(value)) <= 200
}

func createRequestFingerprint(provider string, req CreateBookingRequest, segments []resolvedBookingSegment) string {
	type fingerprintSegment struct {
		ServiceID          string `json:"service_id"`
		POSServiceID       string `json:"pos_service_id"`
		POSServiceVersion  int64  `json:"pos_service_version"`
		StaffID            string `json:"staff_id"`
		POSStaffID         string `json:"pos_staff_id"`
		StaffSelectionMode string `json:"staff_selection_mode"`
	}
	payload := struct {
		OperationType string               `json:"operation_type"`
		Provider      string               `json:"provider"`
		CustomerName  string               `json:"customer_name"`
		CustomerPhone string               `json:"customer_phone"`
		CustomerEmail string               `json:"customer_email"`
		StartTime     string               `json:"start_time"`
		Notes         string               `json:"notes"`
		Segments      []fingerprintSegment `json:"segments"`
	}{
		OperationType: BookingActionBook,
		Provider:      strings.TrimSpace(provider),
		CustomerName:  req.CustomerName,
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: strings.ToLower(req.CustomerEmail),
		StartTime:     req.StartTime.UTC().Format(time.RFC3339Nano),
		Notes:         req.Notes,
		Segments:      make([]fingerprintSegment, 0, len(segments)),
	}
	for _, segment := range segments {
		payload.Segments = append(payload.Segments, fingerprintSegment{
			ServiceID:          segment.Service.ID,
			POSServiceID:       segment.Service.POSServiceID,
			POSServiceVersion:  segment.Service.POSServiceVersion,
			StaffID:            segment.Staff.ID,
			POSStaffID:         segment.Staff.POSStaffID,
			StaffSelectionMode: segment.StaffSelectionMode,
		})
	}
	return fingerprintJSON(payload)
}

func appointmentActionFingerprint(operationType string, appointment AppointmentActionRef, startTime time.Time, segments []BookingSegmentRecord, notes string) string {
	type fingerprintSegment struct {
		ServiceID          string `json:"service_id"`
		POSServiceID       string `json:"pos_service_id"`
		POSServiceVersion  int64  `json:"pos_service_version"`
		StaffID            string `json:"staff_id"`
		POSStaffID         string `json:"pos_staff_id"`
		StaffSelectionMode string `json:"staff_selection_mode"`
	}
	payload := struct {
		OperationType         string               `json:"operation_type"`
		Provider              string               `json:"provider"`
		AppointmentID         string               `json:"appointment_id"`
		POSAppointmentID      string               `json:"pos_appointment_id"`
		POSAppointmentVersion int                  `json:"pos_appointment_version"`
		StartTime             string               `json:"start_time"`
		Notes                 string               `json:"notes"`
		Segments              []fingerprintSegment `json:"segments"`
	}{
		OperationType:         operationType,
		Provider:              appointment.POSProvider,
		AppointmentID:         appointment.ID,
		POSAppointmentID:      appointment.POSAppointmentID,
		POSAppointmentVersion: appointment.POSAppointmentVersion,
		StartTime:             startTime.UTC().Format(time.RFC3339Nano),
		Notes:                 strings.TrimSpace(notes),
		Segments:              make([]fingerprintSegment, 0, len(segments)),
	}
	for _, segment := range segments {
		payload.Segments = append(payload.Segments, fingerprintSegment{
			ServiceID:          segment.Service.ID,
			POSServiceID:       segment.Service.POSServiceID,
			POSServiceVersion:  segment.Service.POSServiceVersion,
			StaffID:            segment.Staff.ID,
			POSStaffID:         segment.Staff.POSStaffID,
			StaffSelectionMode: segment.StaffSelectionMode,
		})
	}
	return fingerprintJSON(payload)
}

func fingerprintJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func isUncertainProviderError(err error) bool {
	return pos.WriteOutcomeForError(err) == pos.WriteOutcomeUnknown
}

func fallbackOperationPolicy(uncertain bool) (string, string, string) {
	if uncertain {
		return ProviderOutcomeUnknown, RetryPolicyBlocked, ReconciliationRequired
	}
	return ProviderOutcomeFailed, RetryPolicySafe, ReconciliationNotRequired
}

func appointmentActionClaimResult(operationType string, attempt *BookingAttempt) (*Appointment, *BookingAttempt, error) {
	if attempt == nil {
		return nil, nil, ErrOperationInProgress
	}
	if operationType == BookingActionReschedule && attempt.Status == StatusRescheduled && attempt.Appointment != nil {
		return attempt.Appointment, nil, nil
	}
	if operationType == BookingActionCancel && attempt.Status == StatusCancelled && attempt.Appointment != nil {
		return attempt.Appointment, nil, nil
	}
	return nil, attempt, nil
}

func postPOSPersistenceContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func normalizeRequest(req CreateBookingRequest) CreateBookingRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = SourceOwnerDashboard
	}
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerPhone = validation.NormalizePhone(req.CustomerPhone)
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.StaffID = strings.TrimSpace(req.StaffID)
	staffIDForMode := req.StaffID
	if staffIDForMode == "" && len(req.Segments) > 0 {
		staffIDForMode = strings.TrimSpace(req.Segments[0].StaffID)
	}
	req.StaffSelectionMode = normalizeStaffSelectionMode(req.StaffSelectionMode, staffIDForMode)
	req.Segments = normalizeBookingSegmentRequests(req.Segments, req.StaffSelectionMode)
	req.Notes = strings.TrimSpace(req.Notes)
	return req
}

func normalizeCalendarRangeRequest(req CalendarRangeRequest) CalendarRangeRequest {
	req.View = strings.TrimSpace(strings.ToLower(req.View))
	if req.View == "" {
		req.View = "week"
	}
	req.StartTime = req.StartTime.UTC()
	req.EndTime = req.EndTime.UTC()
	return req
}

func validateCalendarRange(req CalendarRangeRequest) error {
	if req.StartTime.IsZero() || req.EndTime.IsZero() || !req.EndTime.After(req.StartTime) {
		return ErrValidation
	}
	if req.EndTime.Sub(req.StartTime) > 93*24*time.Hour {
		return ErrValidation
	}
	switch req.View {
	case "day", "week", "month", "agenda", "":
		return nil
	default:
		return ErrValidation
	}
}

func annotateCalendarAppointments(items []Appointment) []Appointment {
	for idx := range items {
		item := &items[idx]
		status := strings.TrimSpace(item.POSSyncStatus)
		if status == "" {
			status = POSSyncStatusSynced
			if item.LastPOSSyncedAt == nil {
				status = POSSyncStatusNotSynced
			}
		}
		item.POSSyncStatus = status
		item.CanEdit = calendarAppointmentCanChange(*item)
		item.CanDelete = item.CanEdit
		switch {
		case item.Status == StatusCancelled:
			item.CanEdit = false
			item.CanDelete = false
		case strings.TrimSpace(item.POSAppointmentID) == "" || item.POSAppointmentVersion < 0:
			item.SyncWarning = "Missing POS booking metadata. Do not treat this appointment as editable until Square sync is repaired."
			item.CanEdit = false
			item.CanDelete = false
		case status == POSSyncStatusFailed:
			item.SyncWarning = firstNonEmpty(item.POSSyncError, "Latest POS calendar sync failed for this appointment.")
		case status == POSSyncStatusNotSynced:
			item.SyncWarning = "This appointment has not been synced from Square Appointments yet."
		case status == POSSyncStatusPending:
			item.SyncWarning = "Square sync is pending for this appointment."
		}
	}
	return items
}

func calendarAppointmentCanChange(item Appointment) bool {
	return item.Status != StatusCancelled &&
		strings.TrimSpace(item.POSAppointmentID) != "" &&
		item.POSAppointmentVersion >= 0
}

func annotateCalendarPendingRequests(items []BookingAttempt) []BookingAttempt {
	for idx := range items {
		item := &items[idx]
		if item.Status == StatusPOSPending {
			item.SyncWarning = "Square booking is still pending. Do not treat this request as confirmed until Square returns a booking ID."
		} else {
			item.SyncWarning = "Pending request: Square Appointments did not confirm this action."
		}
		annotateBookingAttemptPolicy(item)
	}
	return items
}

func calendarWarningSummary(appointments []Appointment, pending []BookingAttempt) CalendarWarningSummary {
	summary := CalendarWarningSummary{}
	for _, item := range appointments {
		if strings.TrimSpace(item.SyncWarning) == "" {
			continue
		}
		switch item.POSSyncStatus {
		case POSSyncStatusFailed:
			summary.SyncFailed++
		case POSSyncStatusPending:
			summary.PendingPOSSync++
		default:
			summary.NotSynced++
		}
	}
	for _, item := range pending {
		if item.Status == StatusPOSPending {
			summary.PendingPOSSync++
		} else {
			summary.FallbackPending++
		}
	}
	summary.TotalWarnings = summary.SyncFailed + summary.NotSynced + summary.PendingPOSSync + summary.FallbackPending
	return summary
}

func parseCalendarEventCursor(raw string) (CalendarEventCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CalendarEventCursor{}, nil
	}
	createdAtRaw, id, ok := strings.Cut(raw, "|")
	if !ok {
		return CalendarEventCursor{}, ErrValidation
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return CalendarEventCursor{}, ErrValidation
	}
	return CalendarEventCursor{CreatedAt: createdAt.UTC(), ID: strings.TrimSpace(id)}, nil
}

func calendarEventCursor(createdAt time.Time, id string) string {
	return createdAt.UTC().Format(time.RFC3339Nano) + "|" + strings.TrimSpace(id)
}

func calendarImportFromPOSAppointment(provider string, item pos.ListedAppointment) (CalendarAppointmentImport, bool) {
	if strings.TrimSpace(item.POSAppointmentID) == "" || item.StartTime.IsZero() {
		return CalendarAppointmentImport{}, false
	}
	endTime := item.EndTime
	if endTime.IsZero() {
		duration := 0
		for _, segment := range item.Segments {
			duration += segment.DurationMinutes
		}
		if duration <= 0 {
			return CalendarAppointmentImport{}, false
		}
		endTime = item.StartTime.Add(time.Duration(duration) * time.Minute)
	}
	if !endTime.After(item.StartTime) {
		return CalendarAppointmentImport{}, false
	}
	segments := make([]CalendarAppointmentSegmentImport, 0, len(item.Segments))
	for _, segment := range item.Segments {
		segments = append(segments, CalendarAppointmentSegmentImport{
			POSServiceID:      strings.TrimSpace(segment.POSServiceID),
			POSServiceVersion: segment.POSServiceVersion,
			POSStaffID:        strings.TrimSpace(segment.POSStaffID),
			DurationMinutes:   segment.DurationMinutes,
		})
	}
	return CalendarAppointmentImport{
		Provider:              provider,
		POSAppointmentID:      strings.TrimSpace(item.POSAppointmentID),
		POSAppointmentVersion: item.POSAppointmentVersion,
		Status:                normalizeImportedAppointmentStatus(item.Status),
		POSCustomerID:         strings.TrimSpace(item.POSCustomerID),
		CustomerName:          defaultString(strings.TrimSpace(item.CustomerName), "Square customer"),
		CustomerPhone:         validation.NormalizePhone(item.CustomerPhone),
		CustomerEmail:         strings.TrimSpace(item.CustomerEmail),
		StartTime:             item.StartTime.UTC(),
		EndTime:               endTime.UTC(),
		Notes:                 strings.TrimSpace(item.Notes),
		Segments:              segments,
	}, true
}

func normalizeImportedAppointmentStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "cancelled", "canceled":
		return StatusCancelled
	case "rescheduled":
		return StatusRescheduled
	default:
		return StatusConfirmed
	}
}

func calendarSyncSummaryMessage(summary CalendarSyncSummary) string {
	return fmt.Sprintf("Calendar sync imported %d, updated %d, skipped %d.", summary.Imported, summary.Updated, summary.Skipped)
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeRescheduleRequest(req RescheduleRequest) RescheduleRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = SourceOwnerDashboard
	}
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.Notes = strings.TrimSpace(req.Notes)
	return req
}

func normalizeRescheduleLookupRequest(req RescheduleLookupRequest) RescheduleLookupRequest {
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerPhone = validation.NormalizePhone(req.CustomerPhone)
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Limit > 5 {
		req.Limit = 5
	}
	return req
}

func normalizeCancelRequest(req CancelRequest) CancelRequest {
	req.OperationKey = strings.TrimSpace(req.OperationKey)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = SourceOwnerDashboard
	}
	req.Reason = strings.TrimSpace(req.Reason)
	return req
}

func normalizeAvailabilityRequest(req AvailabilityRequest) AvailabilityRequest {
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.StaffID = strings.TrimSpace(req.StaffID)
	staffIDForMode := req.StaffID
	if staffIDForMode == "" && len(req.Segments) > 0 {
		staffIDForMode = strings.TrimSpace(req.Segments[0].StaffID)
	}
	req.StaffSelectionMode = normalizeStaffSelectionMode(req.StaffSelectionMode, staffIDForMode)
	req.Segments = normalizeBookingSegmentRequests(req.Segments, req.StaffSelectionMode)
	req.PreferredDate = strings.TrimSpace(req.PreferredDate)
	return req
}

func validateRequest(req CreateBookingRequest) error {
	if req.CustomerName == "" || req.CustomerPhone == "" || req.StartTime.IsZero() || !validOperationKey(req.OperationKey) {
		return ErrValidation
	}
	if !validStaffSelectionMode(req.StaffSelectionMode) {
		return ErrValidation
	}
	segments := requestSegments(req.Segments, req.ServiceID, req.StaffID, req.StaffSelectionMode)
	if len(segments) == 0 {
		return ErrValidation
	}
	for _, segment := range segments {
		if segment.ServiceID == "" || segment.StaffID == "" || !validStaffSelectionMode(segment.StaffSelectionMode) {
			return ErrValidation
		}
	}
	return nil
}

func validateAvailabilityRequest(req AvailabilityRequest) error {
	if req.PreferredDate == "" {
		return ErrValidation
	}
	if !validAvailabilityDate(req.PreferredDate) {
		return ErrValidation
	}
	if req.Limit < 0 {
		return ErrValidation
	}
	if !validStaffSelectionMode(req.StaffSelectionMode) {
		return ErrValidation
	}
	segments := requestSegments(req.Segments, req.ServiceID, req.StaffID, req.StaffSelectionMode)
	if len(segments) == 0 {
		return ErrValidation
	}
	for _, segment := range segments {
		if segment.ServiceID == "" || !validStaffSelectionMode(segment.StaffSelectionMode) {
			return ErrValidation
		}
	}
	return nil
}

func normalizeBookingSegmentRequests(segments []BookingSegmentRequest, fallbackMode string) []BookingSegmentRequest {
	if len(segments) == 0 {
		return nil
	}
	normalized := make([]BookingSegmentRequest, 0, len(segments))
	for _, segment := range segments {
		segment.ServiceID = strings.TrimSpace(segment.ServiceID)
		segment.StaffID = strings.TrimSpace(segment.StaffID)
		segment.StaffSelectionMode = normalizeSegmentStaffSelectionMode(segment.StaffSelectionMode, segment.StaffID, fallbackMode)
		normalized = append(normalized, segment)
	}
	return normalized
}

func normalizeSegmentStaffSelectionMode(mode string, staffID string, fallbackMode string) string {
	mode = strings.TrimSpace(mode)
	if mode != "" {
		return mode
	}
	fallbackMode = strings.TrimSpace(fallbackMode)
	if fallbackMode != "" {
		return fallbackMode
	}
	return normalizeStaffSelectionMode("", staffID)
}

func requestSegments(segments []BookingSegmentRequest, serviceID string, staffID string, staffSelectionMode string) []BookingSegmentRequest {
	if len(segments) > 0 {
		return segments
	}
	serviceID = strings.TrimSpace(serviceID)
	staffID = strings.TrimSpace(staffID)
	if serviceID == "" {
		return nil
	}
	return []BookingSegmentRequest{
		{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: normalizeSegmentStaffSelectionMode("", staffID, staffSelectionMode),
		},
	}
}

func normalizeStaffSelectionMode(mode string, staffID string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		if strings.TrimSpace(staffID) == "" {
			return StaffSelectionAnyone
		}
		return StaffSelectionSpecific
	}
	return mode
}

func validStaffSelectionMode(mode string) bool {
	return mode == StaffSelectionSpecific || mode == StaffSelectionAnyone
}

func bookableServiceHasProviderLink(service *ServiceRef) bool {
	return service != nil &&
		strings.TrimSpace(service.POSProvider) != "" &&
		strings.TrimSpace(service.POSServiceID) != "" &&
		service.POSServiceVersion > 0 &&
		service.DurationMinutes > 0
}

func bookableStaffHasProviderLink(staff *StaffRef) bool {
	return staff != nil &&
		strings.TrimSpace(staff.POSProvider) != "" &&
		strings.TrimSpace(staff.POSStaffID) != ""
}

func (s *Service) resolveBookingSegments(ctx context.Context, salonID string, activeProvider string, req CreateBookingRequest) ([]resolvedBookingSegment, error) {
	segments := requestSegments(req.Segments, req.ServiceID, req.StaffID, req.StaffSelectionMode)
	resolved := make([]resolvedBookingSegment, 0, len(segments))
	provider := ""
	for index, segment := range segments {
		service, err := s.store.GetBookableService(ctx, salonID, activeProvider, segment.ServiceID)
		if err != nil {
			return nil, err
		}
		if !bookableServiceHasProviderLink(service) || service.POSProvider != activeProvider {
			return nil, ErrValidation
		}
		staff, err := s.store.GetBookableStaff(ctx, salonID, activeProvider, segment.StaffID)
		if err != nil {
			return nil, err
		}
		if !bookableStaffHasProviderLink(staff) || staff.POSProvider != service.POSProvider {
			return nil, ErrValidation
		}
		if provider == "" {
			provider = service.POSProvider
		}
		if service.POSProvider != provider {
			return nil, ErrValidation
		}
		resolved = append(resolved, resolvedBookingSegment{
			Service:            *service,
			Staff:              *staff,
			StaffSelectionMode: segment.StaffSelectionMode,
			SortOrder:          index + 1,
		})
	}
	if len(resolved) == 0 {
		return nil, ErrValidation
	}
	return resolved, nil
}

func (s *Service) resolveAvailabilitySegments(ctx context.Context, salonID string, activeProvider string, req AvailabilityRequest) ([]resolvedAvailabilitySegment, error) {
	segments := requestSegments(req.Segments, req.ServiceID, req.StaffID, req.StaffSelectionMode)
	resolved := make([]resolvedAvailabilitySegment, 0, len(segments))
	provider := ""
	for index, segment := range segments {
		service, err := s.store.GetBookableService(ctx, salonID, activeProvider, segment.ServiceID)
		if err != nil {
			return nil, err
		}
		if !bookableServiceHasProviderLink(service) || service.POSProvider != activeProvider {
			return nil, ErrValidation
		}
		if provider == "" {
			provider = service.POSProvider
		}
		if service.POSProvider != provider {
			return nil, ErrValidation
		}
		var staff *StaffRef
		if segment.StaffID != "" {
			staff, err = s.store.GetBookableStaff(ctx, salonID, activeProvider, segment.StaffID)
			if err != nil {
				return nil, err
			}
			if !bookableStaffHasProviderLink(staff) || staff.POSProvider != service.POSProvider {
				return nil, ErrValidation
			}
		}
		resolved = append(resolved, resolvedAvailabilitySegment{
			Service:            *service,
			Staff:              staff,
			StaffSelectionMode: segment.StaffSelectionMode,
			SortOrder:          index + 1,
		})
	}
	if len(resolved) == 0 {
		return nil, ErrValidation
	}
	return resolved, nil
}

func (s *Service) resolvePOSCustomer(ctx context.Context, salonID string, provider pos.POSProvider, customer CustomerRef, req CreateBookingRequest) (*pos.Customer, string, error) {
	if strings.TrimSpace(customer.POSCustomerID) != "" {
		return &pos.Customer{
			ID:            customer.ID,
			POSCustomerID: customer.POSCustomerID,
			Name:          customer.Name,
			Phone:         customer.Phone,
			Email:         customer.Email,
		}, "", nil
	}
	posCustomer, err := provider.SearchCustomerByPhone(ctx, salonID, req.CustomerPhone)
	if err != nil {
		return nil, "search_customer", err
	}
	if posCustomer == nil {
		posCustomer, err = provider.CreateCustomer(ctx, salonID, pos.CreateCustomerInput{
			Name:  req.CustomerName,
			Phone: req.CustomerPhone,
			Email: req.CustomerEmail,
		})
		if err != nil {
			return nil, "create_customer", err
		}
	}
	if posCustomer == nil || strings.TrimSpace(posCustomer.POSCustomerID) == "" {
		return nil, "create_customer", fmt.Errorf("pos customer id was not returned")
	}
	linked, err := s.store.LinkBookingCustomer(ctx, salonID, provider.Name(), customer.ID, *posCustomer)
	if err != nil {
		return nil, "link_customer", err
	}
	if strings.TrimSpace(linked.POSCustomerID) == "" {
		return nil, "link_customer", fmt.Errorf("pos customer id was not persisted")
	}
	return &pos.Customer{
		ID:            linked.ID,
		POSCustomerID: linked.POSCustomerID,
		Name:          linked.Name,
		Phone:         linked.Phone,
		Email:         linked.Email,
	}, "", nil
}

func bookingSegmentsDuration(segments []resolvedBookingSegment) int {
	total := 0
	for _, segment := range segments {
		total += segment.Service.DurationMinutes
	}
	return total
}

func availabilitySegmentsDuration(segments []resolvedAvailabilitySegment) int {
	total := 0
	for _, segment := range segments {
		total += segment.Service.DurationMinutes
	}
	return total
}

func bookingSegmentRecords(segments []resolvedBookingSegment) []BookingSegmentRecord {
	records := make([]BookingSegmentRecord, 0, len(segments))
	for _, segment := range segments {
		records = append(records, BookingSegmentRecord{
			Service:            segment.Service,
			Staff:              segment.Staff,
			StaffSelectionMode: segment.StaffSelectionMode,
			SortOrder:          segment.SortOrder,
		})
	}
	return records
}

func appointmentActionSegments(appointment AppointmentActionRef) []BookingSegmentRecord {
	if len(appointment.Segments) > 0 {
		segments := make([]BookingSegmentRecord, 0, len(appointment.Segments))
		for index, segment := range appointment.Segments {
			if segment.SortOrder <= 0 {
				segment.SortOrder = index + 1
			}
			if segment.StaffSelectionMode == "" {
				segment.StaffSelectionMode = StaffSelectionSpecific
			}
			segments = append(segments, segment)
		}
		return segments
	}
	return singleBookingSegment(appointment.Service, appointment.Staff, appointment.StaffSelectionMode)
}

func applyStaffToBookingSegments(segments []BookingSegmentRecord, staff StaffRef) []BookingSegmentRecord {
	updated := make([]BookingSegmentRecord, 0, len(segments))
	for _, segment := range segments {
		segment.Staff = staff
		updated = append(updated, segment)
	}
	return updated
}

func validateAppointmentActionSegments(appointment AppointmentActionRef, segments []BookingSegmentRecord) error {
	if len(segments) == 0 {
		return ErrValidation
	}
	for _, segment := range segments {
		if segment.Service.POSProvider != appointment.POSProvider || strings.TrimSpace(segment.Service.POSServiceID) == "" || segment.Service.POSServiceVersion <= 0 || segment.Service.DurationMinutes <= 0 {
			return ErrValidation
		}
		if segment.Staff.POSProvider != appointment.POSProvider || strings.TrimSpace(segment.Staff.POSStaffID) == "" {
			return ErrValidation
		}
		if !validStaffSelectionMode(segment.StaffSelectionMode) {
			return ErrValidation
		}
	}
	return nil
}

func bookingSegmentRecordsDuration(segments []BookingSegmentRecord) int {
	total := 0
	for _, segment := range segments {
		total += segment.Service.DurationMinutes
	}
	return total
}

func posAppointmentSegments(segments []resolvedBookingSegment) []pos.AppointmentSegmentInput {
	inputs := make([]pos.AppointmentSegmentInput, 0, len(segments))
	for _, segment := range segments {
		inputs = append(inputs, posAppointmentSegment(segment.Service, segment.Staff))
	}
	return inputs
}

func posAppointmentSegmentsFromRecords(segments []BookingSegmentRecord) []pos.AppointmentSegmentInput {
	inputs := make([]pos.AppointmentSegmentInput, 0, len(segments))
	for _, segment := range segments {
		inputs = append(inputs, posAppointmentSegment(segment.Service, segment.Staff))
	}
	return inputs
}

func bookingSegmentSnapshots(segments []BookingSegmentRecord) []BookingSegmentSnapshot {
	snapshots := make([]BookingSegmentSnapshot, 0, len(segments))
	for index, segment := range segments {
		sortOrder := segment.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		mode := segment.StaffSelectionMode
		if mode == "" {
			mode = StaffSelectionSpecific
		}
		snapshots = append(snapshots, BookingSegmentSnapshot{
			ServiceID:          segment.Service.ID,
			ServiceName:        segment.Service.Name,
			StaffID:            segment.Staff.ID,
			StaffName:          segment.Staff.Name,
			StaffSelectionMode: mode,
			DurationMinutes:    segment.Service.DurationMinutes,
			SortOrder:          sortOrder,
		})
	}
	return snapshots
}

func posAvailabilitySegments(segments []resolvedAvailabilitySegment) []pos.AvailabilitySegmentInput {
	inputs := make([]pos.AvailabilitySegmentInput, 0, len(segments))
	for _, segment := range segments {
		staffID := ""
		if segment.Staff != nil {
			staffID = segment.Staff.POSStaffID
		}
		inputs = append(inputs, pos.AvailabilitySegmentInput{
			ServiceID:       segment.Service.POSServiceID,
			StaffID:         staffID,
			DurationMinutes: segment.Service.DurationMinutes,
		})
	}
	return inputs
}

func availabilityPrimaryStaffID(segment resolvedAvailabilitySegment) string {
	if segment.Staff == nil {
		return ""
	}
	return segment.Staff.POSStaffID
}

func availabilitySegmentsResult(segments []resolvedAvailabilitySegment) []AvailabilitySegment {
	items := make([]AvailabilitySegment, 0, len(segments))
	for _, segment := range segments {
		item := AvailabilitySegment{
			ServiceID:          segment.Service.ID,
			ServiceName:        segment.Service.Name,
			StaffSelectionMode: segment.StaffSelectionMode,
			DurationMinutes:    segment.Service.DurationMinutes,
		}
		if segment.Staff != nil {
			item.StaffID = segment.Staff.ID
			item.StaffName = segment.Staff.Name
		}
		items = append(items, item)
	}
	return items
}

func availabilitySlotSegments(slot pos.TimeSlot, requested []resolvedAvailabilitySegment, staffByPOSID map[string]StaffRef) ([]AvailabilitySegment, bool) {
	if len(requested) == 0 {
		return nil, false
	}
	if len(requested) > 1 && len(slot.Segments) == 0 {
		return nil, false
	}
	items := make([]AvailabilitySegment, 0, len(requested))
	for index, requestedSegment := range requested {
		posSegment := pos.TimeSlotSegment{}
		if len(slot.Segments) > 0 {
			if index >= len(slot.Segments) {
				return nil, false
			}
			posSegment = slot.Segments[index]
			if strings.TrimSpace(posSegment.ServiceID) != "" && strings.TrimSpace(posSegment.ServiceID) != requestedSegment.Service.POSServiceID {
				return nil, false
			}
		} else {
			posSegment = pos.TimeSlotSegment{
				ServiceID:       requestedSegment.Service.POSServiceID,
				StaffID:         slot.StaffID,
				DurationMinutes: requestedSegment.Service.DurationMinutes,
			}
		}
		staff, ok := availabilitySegmentStaff(posSegment.StaffID, requestedSegment.Staff, staffByPOSID)
		if !ok {
			return nil, false
		}
		durationMinutes := posSegment.DurationMinutes
		if durationMinutes <= 0 {
			durationMinutes = requestedSegment.Service.DurationMinutes
		}
		items = append(items, AvailabilitySegment{
			ServiceID:          requestedSegment.Service.ID,
			ServiceName:        requestedSegment.Service.Name,
			StaffID:            staff.ID,
			StaffName:          staff.Name,
			StaffSelectionMode: requestedSegment.StaffSelectionMode,
			DurationMinutes:    durationMinutes,
		})
	}
	return items, true
}

func availabilitySegmentStaff(posStaffID string, selectedStaff *StaffRef, staffByPOSID map[string]StaffRef) (StaffRef, bool) {
	posStaffID = strings.TrimSpace(posStaffID)
	if selectedStaff != nil {
		if posStaffID != "" && posStaffID != selectedStaff.POSStaffID {
			return StaffRef{}, false
		}
		return *selectedStaff, true
	}
	if posStaffID == "" {
		return StaffRef{}, false
	}
	staff, ok := staffByPOSID[posStaffID]
	return staff, ok
}

func validAvailabilityDate(value string) bool {
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return true
	}
	return false
}

func availabilityLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 20 {
		return 20
	}
	return limit
}

func appointmentDurationMinutes(appointment AppointmentActionRef) int {
	if appointment.Service.DurationMinutes > 0 {
		return appointment.Service.DurationMinutes
	}
	duration := appointment.EndTime.Sub(appointment.StartTime)
	if duration <= 0 {
		return 0
	}
	return int(duration.Minutes())
}

func appointmentFromActionRef(ref AppointmentActionRef) *Appointment {
	return &Appointment{
		ID:                    ref.ID,
		SalonID:               ref.SalonID,
		BookingAttemptID:      ref.BookingAttemptID,
		POSProvider:           ref.POSProvider,
		POSAppointmentID:      ref.POSAppointmentID,
		POSAppointmentVersion: ref.POSAppointmentVersion,
		Status:                ref.Status,
		CustomerName:          ref.CustomerName,
		CustomerPhone:         ref.CustomerPhone,
		CustomerEmail:         ref.CustomerEmail,
		ServiceID:             ref.Service.ID,
		StaffID:               ref.Staff.ID,
		StaffSelectionMode:    ref.StaffSelectionMode,
		Segments:              bookingSegmentSnapshots(appointmentActionSegments(ref)),
		StartTime:             ref.StartTime,
		EndTime:               ref.EndTime,
		Notes:                 ref.Notes,
		CreatedAt:             ref.CreatedAt,
		UpdatedAt:             ref.UpdatedAt,
	}
}

func singleBookingSegment(service ServiceRef, staff StaffRef, staffSelectionMode string) []BookingSegmentRecord {
	if staffSelectionMode == "" {
		staffSelectionMode = StaffSelectionSpecific
	}
	return []BookingSegmentRecord{
		{
			Service:            service,
			Staff:              staff,
			StaffSelectionMode: staffSelectionMode,
			SortOrder:          1,
		},
	}
}

func posAppointmentSegment(service ServiceRef, staff StaffRef) pos.AppointmentSegmentInput {
	return pos.AppointmentSegmentInput{
		ServiceID:       service.POSServiceID,
		ServiceVersion:  service.POSServiceVersion,
		StaffID:         staff.POSStaffID,
		DurationMinutes: service.DurationMinutes,
	}
}

func (s *Service) bookableStaffByPOSID(ctx context.Context, salonID string, provider string, selectedStaff *StaffRef) (map[string]StaffRef, error) {
	refs := make(map[string]StaffRef)
	if selectedStaff != nil {
		if bookableStaffHasProviderLink(selectedStaff) && selectedStaff.POSProvider == provider {
			refs[selectedStaff.POSStaffID] = *selectedStaff
		}
		return refs, nil
	}
	staff, err := s.store.ListBookableStaffRefs(ctx, salonID, provider)
	if err != nil {
		return nil, err
	}
	for _, item := range staff {
		if item.POSProvider != provider || !bookableStaffHasProviderLink(&item) {
			continue
		}
		refs[item.POSStaffID] = item
	}
	return refs, nil
}

func availabilityStaffRef(slot pos.TimeSlot, selectedStaff *StaffRef, staffByPOSID map[string]StaffRef) (StaffRef, bool) {
	posStaffID := strings.TrimSpace(slot.StaffID)
	if selectedStaff != nil {
		if posStaffID != "" && posStaffID != selectedStaff.POSStaffID {
			return StaffRef{}, false
		}
		return *selectedStaff, true
	}
	if posStaffID == "" {
		return StaffRef{}, false
	}
	staff, ok := staffByPOSID[posStaffID]
	return staff, ok
}

func availabilityResult(req AvailabilityRequest, segments []resolvedAvailabilitySegment, schedule *Schedule, slots []AvailabilitySlot) *AvailabilityResult {
	timezone := ""
	if schedule != nil {
		timezone = strings.TrimSpace(schedule.Timezone)
	}
	primary := segments[0]
	result := &AvailabilityResult{
		ServiceID:          primary.Service.ID,
		ServiceName:        primary.Service.Name,
		StaffSelectionMode: req.StaffSelectionMode,
		PreferredDate:      req.PreferredDate,
		DurationMinutes:    availabilitySegmentsDuration(segments),
		Timezone:           timezone,
		Segments:           availabilitySegmentsResult(segments),
		Slots:              slots,
	}
	if result.Slots == nil {
		result.Slots = []AvailabilitySlot{}
	}
	if primary.Staff != nil {
		result.StaffID = primary.Staff.ID
		result.StaffName = primary.Staff.Name
	}
	return result
}

func scheduleBusinessHourPeriods(schedule *Schedule) []BusinessHourPeriod {
	if schedule == nil {
		return nil
	}
	if len(schedule.BusinessHourPeriods) > 0 {
		return schedule.BusinessHourPeriods
	}
	periods := make([]BusinessHourPeriod, 0, len(schedule.BusinessHours))
	for _, hour := range schedule.BusinessHours {
		if hour.IsClosed {
			continue
		}
		periods = append(periods, BusinessHourPeriod{
			DayOfWeek:      hour.DayOfWeek,
			StartLocalTime: hour.OpenTime,
			EndLocalTime:   hour.CloseTime,
		})
	}
	return periods
}

func withinBusinessHourPeriods(startTime time.Time, endTime time.Time, periods []BusinessHourPeriod, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	startLocal := startTime.In(loc)
	endLocal := endTime.In(loc)
	if startLocal.Year() != endLocal.Year() || startLocal.YearDay() != endLocal.YearDay() {
		return false
	}
	for _, period := range periods {
		if period.DayOfWeek != int(startLocal.Weekday()) {
			continue
		}
		openAt, ok := localClockDuration(period.StartLocalTime)
		if !ok {
			continue
		}
		closeAt, ok := localClockDuration(period.EndLocalTime)
		if !ok || closeAt <= openAt {
			continue
		}
		startAt := time.Duration(startLocal.Hour())*time.Hour + time.Duration(startLocal.Minute())*time.Minute + time.Duration(startLocal.Second())*time.Second
		endAt := time.Duration(endLocal.Hour())*time.Hour + time.Duration(endLocal.Minute())*time.Minute + time.Duration(endLocal.Second())*time.Second
		if startAt >= openAt && endAt <= closeAt {
			return true
		}
	}
	return false
}

func withinBusinessHours(startTime time.Time, endTime time.Time, hours []BusinessHour, loc *time.Location) bool {
	return withinBusinessHourPeriods(startTime, endTime, scheduleBusinessHourPeriods(&Schedule{BusinessHours: hours}), loc)
}

func localClockDuration(value string) (time.Duration, bool) {
	parsed, err := time.Parse("15:04:05", strings.TrimSpace(value))
	if err != nil {
		parsed, err = time.Parse("15:04", strings.TrimSpace(value))
	}
	if err != nil {
		return 0, false
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute + time.Duration(parsed.Second())*time.Second, true
}

func posErrorCode(err error) string {
	if err == nil {
		return pos.ErrorUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return pos.ErrorTimeout
	case strings.Contains(msg, "token"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "expired"):
		return pos.ErrorTokenExpired
	case strings.Contains(msg, "permission"), strings.Contains(msg, "forbidden"):
		return pos.ErrorPermissionDenied
	case strings.Contains(msg, "location"):
		return pos.ErrorLocationNotSelected
	case strings.Contains(msg, "conflict"), strings.Contains(msg, "overlap"):
		return pos.ErrorBookingConflict
	case strings.Contains(msg, "availability"):
		return pos.ErrorAvailabilityFailed
	default:
		return pos.ErrorBookingFailed
	}
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func normalizeAttemptStatusFilter(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return "", nil
	}
	switch status {
	case StatusPOSPending, StatusConfirmed, StatusFallbackPending, StatusRescheduled, StatusCancelled:
		return status, nil
	case "started", "failed":
		return status, nil
	default:
		return "", ErrValidation
	}
}
