package booking

import (
	"context"
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
)

type Store interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	GetBookableService(ctx context.Context, salonID string, serviceID string) (*ServiceRef, error)
	GetBookableStaff(ctx context.Context, salonID string, staffID string) (*StaffRef, error)
	ListBookableStaffRefs(ctx context.Context, salonID string) ([]StaffRef, error)
	GetSchedule(ctx context.Context, salonID string) (*Schedule, error)
	GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error)
	CreatePendingBookingAttempt(ctx context.Context, record PendingBookingRecord) (*BookingAttempt, error)
	SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error)
	SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error)
	CreatePendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingAttempt, error)
	SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error)
	SaveCancelledAppointment(ctx context.Context, record CancelledAppointmentRecord) (*Appointment, error)
	SaveAppointmentActionFallback(ctx context.Context, record AppointmentActionFallbackRecord) (*BookingAttempt, error)
	LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error)
	ListAppointments(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Appointment, error)
	ListBookingAttempts(ctx context.Context, salonID string, ownerUserID string, limit int) ([]BookingAttempt, error)
}

type Service struct {
	store     Store
	providers map[string]pos.POSProvider
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
	service, err := s.store.GetBookableService(ctx, salonID, req.ServiceID)
	if err != nil {
		return nil, err
	}
	if service.DurationMinutes <= 0 {
		return nil, ErrValidation
	}
	staff, err := s.store.GetBookableStaff(ctx, salonID, req.StaffID)
	if err != nil {
		return nil, err
	}
	if staff.POSProvider != service.POSProvider {
		return nil, ErrValidation
	}

	provider := s.providers[service.POSProvider]
	if provider == nil {
		return nil, ErrProviderUnavailable
	}

	endTime := req.StartTime.Add(time.Duration(service.DurationMinutes) * time.Minute)
	pending, err := s.store.CreatePendingBookingAttempt(ctx, PendingBookingRecord{
		SalonID:           salonID,
		Source:            req.Source,
		Provider:          provider.Name(),
		POSIdempotencyKey: newPOSIdempotencyKey(),
		CustomerName:      req.CustomerName,
		CustomerPhone:     req.CustomerPhone,
		CustomerEmail:     req.CustomerEmail,
		Service:           *service,
		Staff:             *staff,
		StartTime:         req.StartTime,
		EndTime:           endTime,
		Notes:             req.Notes,
	})
	if err != nil {
		return nil, err
	}

	customer, err := provider.SearchCustomerByPhone(ctx, salonID, req.CustomerPhone)
	if err != nil {
		return s.saveFallback(ctx, *pending, *service, *staff, req, endTime, "search_customer", err)
	}
	if customer == nil {
		customer, err = provider.CreateCustomer(ctx, salonID, pos.CreateCustomerInput{
			Name:  req.CustomerName,
			Phone: req.CustomerPhone,
			Email: req.CustomerEmail,
		})
		if err != nil {
			return s.saveFallback(ctx, *pending, *service, *staff, req, endTime, "create_customer", err)
		}
	}
	if customer == nil || strings.TrimSpace(customer.POSCustomerID) == "" {
		return s.saveFallback(ctx, *pending, *service, *staff, req, endTime, "create_customer", fmt.Errorf("pos customer id was not returned"))
	}

	appointment, err := provider.CreateAppointment(ctx, salonID, pos.CreateAppointmentInput{
		IdempotencyKey:  pending.POSIdempotencyKey,
		CustomerID:      customer.POSCustomerID,
		ServiceID:       service.POSServiceID,
		ServiceVersion:  service.POSServiceVersion,
		StaffID:         staff.POSStaffID,
		StartTime:       req.StartTime,
		DurationMinutes: service.DurationMinutes,
		Notes:           req.Notes,
	})
	if err != nil {
		return s.saveFallback(ctx, *pending, *service, *staff, req, endTime, "create_booking", err)
	}
	if appointment == nil || strings.TrimSpace(appointment.POSAppointmentID) == "" {
		return s.saveFallback(ctx, *pending, *service, *staff, req, endTime, "create_booking", fmt.Errorf("pos booking id was not returned"))
	}
	if appointment.POSAppointmentVersion < 0 {
		return s.saveFallback(ctx, *pending, *service, *staff, req, endTime, "create_booking", fmt.Errorf("pos booking version was not returned"))
	}
	if !appointment.EndTime.IsZero() {
		endTime = appointment.EndTime
	}
	startTime := req.StartTime
	if !appointment.StartTime.IsZero() {
		startTime = appointment.StartTime
	}

	return s.store.SaveConfirmedBooking(ctx, ConfirmedBookingRecord{
		AttemptID:         pending.ID,
		SalonID:           salonID,
		Source:            req.Source,
		Provider:          provider.Name(),
		CustomerName:      req.CustomerName,
		CustomerPhone:     req.CustomerPhone,
		CustomerEmail:     req.CustomerEmail,
		Service:           *service,
		Staff:             *staff,
		StartTime:         startTime,
		EndTime:           endTime,
		Notes:             req.Notes,
		POSBookingID:      appointment.POSAppointmentID,
		POSBookingVersion: appointment.POSAppointmentVersion,
	})
}

func (s *Service) Reschedule(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req RescheduleRequest) (*Appointment, *BookingAttempt, error) {
	req = normalizeRescheduleRequest(req)
	if req.StartTime.IsZero() {
		return nil, nil, ErrValidation
	}
	appointment, err := s.store.GetAppointmentForOwner(ctx, salonID, ownerUserID, strings.TrimSpace(appointmentID))
	if err != nil {
		return nil, nil, err
	}
	if appointment.Status == StatusCancelled || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion < 0 {
		return nil, nil, ErrValidation
	}
	if appointment.Service.POSProvider != appointment.POSProvider || strings.TrimSpace(appointment.Service.POSServiceID) == "" || appointment.Service.POSServiceVersion <= 0 {
		return nil, nil, ErrValidation
	}

	staff := appointment.Staff
	if req.StaffID != "" && req.StaffID != appointment.Staff.ID {
		nextStaff, err := s.store.GetBookableStaff(ctx, salonID, req.StaffID)
		if err != nil {
			return nil, nil, err
		}
		if nextStaff.POSProvider != appointment.POSProvider {
			return nil, nil, ErrValidation
		}
		staff = *nextStaff
	}
	if strings.TrimSpace(staff.POSStaffID) == "" {
		return nil, nil, ErrValidation
	}

	provider := s.providers[appointment.POSProvider]
	if provider == nil {
		return nil, nil, ErrProviderUnavailable
	}

	durationMinutes := appointmentDurationMinutes(*appointment)
	if durationMinutes <= 0 {
		return nil, nil, ErrValidation
	}
	notes := req.Notes
	if notes == "" {
		notes = appointment.Notes
	}
	endTime := req.StartTime.Add(time.Duration(durationMinutes) * time.Minute)
	pending, err := s.store.CreatePendingAppointmentAction(ctx, PendingAppointmentActionRecord{
		SalonID:            salonID,
		Appointment:        *appointment,
		Provider:           appointment.POSProvider,
		Source:             req.Source,
		RequestedStartTime: req.StartTime,
		RequestedEndTime:   endTime,
		Notes:              notes,
		POSIdempotencyKey:  newPOSIdempotencyKey(),
	})
	if err != nil {
		return nil, nil, err
	}

	posAppointment, err := provider.RescheduleAppointment(ctx, salonID, appointment.POSAppointmentID, pos.RescheduleInput{
		IdempotencyKey:  pending.POSIdempotencyKey,
		BookingVersion:  appointment.POSAppointmentVersion,
		ServiceID:       appointment.Service.POSServiceID,
		ServiceVersion:  appointment.Service.POSServiceVersion,
		StaffID:         staff.POSStaffID,
		StartTime:       req.StartTime,
		DurationMinutes: durationMinutes,
		Notes:           notes,
	})
	if err != nil {
		fallback, saveErr := s.saveActionFallback(ctx, pending.ID, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, req.StartTime, endTime, notes, err)
		return nil, fallback, saveErr
	}
	if posAppointment == nil || strings.TrimSpace(posAppointment.POSAppointmentID) == "" {
		fallback, saveErr := s.saveActionFallback(ctx, pending.ID, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, req.StartTime, endTime, notes, fmt.Errorf("pos booking id was not returned"))
		return nil, fallback, saveErr
	}
	if posAppointment.POSAppointmentVersion < 0 {
		fallback, saveErr := s.saveActionFallback(ctx, pending.ID, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, req.StartTime, endTime, notes, fmt.Errorf("pos booking version was not returned"))
		return nil, fallback, saveErr
	}
	startTime := req.StartTime
	if !posAppointment.StartTime.IsZero() {
		startTime = posAppointment.StartTime
	}
	if !posAppointment.EndTime.IsZero() {
		endTime = posAppointment.EndTime
	}

	saved, err := s.store.SaveRescheduledAppointment(ctx, RescheduledAppointmentRecord{
		AttemptID:         pending.ID,
		Appointment:       *appointment,
		Staff:             staff,
		Source:            req.Source,
		StartTime:         startTime,
		EndTime:           endTime,
		Notes:             notes,
		POSBookingVersion: posAppointment.POSAppointmentVersion,
	})
	return saved, nil, err
}

func (s *Service) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req AvailabilityRequest) (*AvailabilityResult, error) {
	req = normalizeAvailabilityRequest(req)
	if err := validateAvailabilityRequest(req); err != nil {
		return nil, err
	}
	if err := s.store.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	service, err := s.store.GetBookableService(ctx, salonID, req.ServiceID)
	if err != nil {
		return nil, err
	}
	if service.DurationMinutes <= 0 || strings.TrimSpace(service.POSServiceID) == "" {
		return nil, ErrValidation
	}
	provider := s.providers[service.POSProvider]
	if provider == nil {
		return nil, ErrProviderUnavailable
	}

	var selectedStaff *StaffRef
	if req.StaffID != "" {
		staff, err := s.store.GetBookableStaff(ctx, salonID, req.StaffID)
		if err != nil {
			return nil, err
		}
		if staff.POSProvider != service.POSProvider || strings.TrimSpace(staff.POSStaffID) == "" {
			return nil, ErrValidation
		}
		selectedStaff = staff
	}

	schedule, err := s.store.GetSchedule(ctx, salonID)
	if err != nil {
		return nil, err
	}
	result := availabilityResult(req, *service, selectedStaff, schedule, nil)
	if schedule == nil || len(schedule.BusinessHours) == 0 {
		return result, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(schedule.Timezone))
	if err != nil {
		return nil, ErrValidation
	}

	staffByPOSID, err := s.bookableStaffByPOSID(ctx, salonID, service.POSProvider, selectedStaff)
	if err != nil {
		return nil, err
	}
	if len(staffByPOSID) == 0 {
		return result, nil
	}

	posStaffID := ""
	if selectedStaff != nil {
		posStaffID = selectedStaff.POSStaffID
	}
	slots, err := provider.CheckAvailability(ctx, salonID, pos.AvailabilityInput{
		ServiceID:       service.POSServiceID,
		StaffID:         posStaffID,
		PreferredDate:   req.PreferredDate,
		DurationMinutes: service.DurationMinutes,
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
			endTime = startTime.Add(time.Duration(service.DurationMinutes) * time.Minute)
		}
		if startTime.IsZero() || !endTime.After(startTime) {
			continue
		}
		staffRef, ok := availabilityStaffRef(slot, selectedStaff, staffByPOSID)
		if !ok {
			continue
		}
		if !withinBusinessHours(startTime, endTime, schedule.BusinessHours, loc) {
			continue
		}
		filtered = append(filtered, AvailabilitySlot{
			StartTime: startTime,
			EndTime:   endTime,
			StaffID:   staffRef.ID,
			StaffName: staffRef.Name,
		})
	}
	result.Slots = filtered
	return result, nil
}

func (s *Service) Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req CancelRequest) (*Appointment, *BookingAttempt, error) {
	req = normalizeCancelRequest(req)
	appointment, err := s.store.GetAppointmentForOwner(ctx, salonID, ownerUserID, strings.TrimSpace(appointmentID))
	if err != nil {
		return nil, nil, err
	}
	if appointment.Status == StatusCancelled || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion < 0 {
		return nil, nil, ErrValidation
	}

	provider := s.providers[appointment.POSProvider]
	if provider == nil {
		return nil, nil, ErrProviderUnavailable
	}

	pending, err := s.store.CreatePendingAppointmentAction(ctx, PendingAppointmentActionRecord{
		SalonID:            salonID,
		Appointment:        *appointment,
		Provider:           appointment.POSProvider,
		Source:             req.Source,
		RequestedStartTime: appointment.StartTime,
		RequestedEndTime:   appointment.EndTime,
		Notes:              req.Reason,
		POSIdempotencyKey:  newPOSIdempotencyKey(),
	})
	if err != nil {
		return nil, nil, err
	}

	posAppointment, err := provider.CancelAppointment(ctx, salonID, appointment.POSAppointmentID, pos.CancelInput{
		IdempotencyKey: pending.POSIdempotencyKey,
		BookingVersion: appointment.POSAppointmentVersion,
		Reason:         req.Reason,
	})
	if err != nil {
		fallback, saveErr := s.saveActionFallback(ctx, pending.ID, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, appointment.StartTime, appointment.EndTime, req.Reason, err)
		return nil, fallback, saveErr
	}
	if posAppointment == nil || strings.TrimSpace(posAppointment.POSAppointmentID) == "" {
		fallback, saveErr := s.saveActionFallback(ctx, pending.ID, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, appointment.StartTime, appointment.EndTime, req.Reason, fmt.Errorf("pos booking id was not returned"))
		return nil, fallback, saveErr
	}
	if posAppointment.POSAppointmentVersion < 0 {
		fallback, saveErr := s.saveActionFallback(ctx, pending.ID, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, appointment.StartTime, appointment.EndTime, req.Reason, fmt.Errorf("pos booking version was not returned"))
		return nil, fallback, saveErr
	}

	saved, err := s.store.SaveCancelledAppointment(ctx, CancelledAppointmentRecord{
		AttemptID:         pending.ID,
		Appointment:       *appointment,
		Source:            req.Source,
		Reason:            req.Reason,
		POSBookingVersion: posAppointment.POSAppointmentVersion,
	})
	return saved, nil, err
}

func (s *Service) Appointments(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Appointment, error) {
	return s.store.ListAppointments(ctx, salonID, ownerUserID, clampLimit(limit))
}

func (s *Service) Attempts(ctx context.Context, salonID string, ownerUserID string, limit int) ([]BookingAttempt, error) {
	return s.store.ListBookingAttempts(ctx, salonID, ownerUserID, clampLimit(limit))
}

func (s *Service) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	return s.store.LatestTestBooking(ctx, salonID, ownerUserID)
}

func (s *Service) saveFallback(ctx context.Context, pending BookingAttempt, service ServiceRef, staff StaffRef, req CreateBookingRequest, endTime time.Time, operation string, providerErr error) (*BookingAttempt, error) {
	return s.store.SaveFallbackBooking(ctx, FallbackBookingRecord{
		AttemptID:     pending.ID,
		SalonID:       pending.SalonID,
		Source:        pending.Source,
		Provider:      pending.POSProvider,
		Operation:     operation,
		CustomerName:  req.CustomerName,
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
		Service:       service,
		Staff:         staff,
		StartTime:     req.StartTime,
		EndTime:       endTime,
		Notes:         req.Notes,
		ErrorCode:     posErrorCode(providerErr),
		ErrorMessage:  providerErr.Error(),
	})
}

func (s *Service) saveActionFallback(ctx context.Context, attemptID string, salonID string, appointment AppointmentActionRef, provider string, operation string, source string, notificationType string, startTime time.Time, endTime time.Time, notes string, providerErr error) (*BookingAttempt, error) {
	if strings.TrimSpace(source) == "" {
		source = SourceOwnerDashboard
	}
	return s.store.SaveAppointmentActionFallback(ctx, AppointmentActionFallbackRecord{
		AttemptID:          attemptID,
		SalonID:            salonID,
		Appointment:        appointment,
		Provider:           provider,
		Operation:          operation,
		Source:             source,
		NotificationType:   notificationType,
		RequestedStartTime: startTime,
		RequestedEndTime:   endTime,
		Notes:              notes,
		ErrorCode:          posErrorCode(providerErr),
		ErrorMessage:       providerErr.Error(),
	})
}

func newPOSIdempotencyKey() string {
	return uuid.NewString()
}

func normalizeRequest(req CreateBookingRequest) CreateBookingRequest {
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = SourceOwnerDashboard
	}
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerPhone = validation.NormalizePhone(req.CustomerPhone)
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.Notes = strings.TrimSpace(req.Notes)
	return req
}

func normalizeRescheduleRequest(req RescheduleRequest) RescheduleRequest {
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = SourceOwnerDashboard
	}
	req.StaffID = strings.TrimSpace(req.StaffID)
	req.Notes = strings.TrimSpace(req.Notes)
	return req
}

func normalizeCancelRequest(req CancelRequest) CancelRequest {
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
	req.PreferredDate = strings.TrimSpace(req.PreferredDate)
	return req
}

func validateRequest(req CreateBookingRequest) error {
	if req.CustomerName == "" || req.CustomerPhone == "" || req.ServiceID == "" || req.StaffID == "" || req.StartTime.IsZero() {
		return ErrValidation
	}
	return nil
}

func validateAvailabilityRequest(req AvailabilityRequest) error {
	if req.ServiceID == "" || req.PreferredDate == "" {
		return ErrValidation
	}
	if !validAvailabilityDate(req.PreferredDate) {
		return ErrValidation
	}
	if req.Limit < 0 {
		return ErrValidation
	}
	return nil
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
		StartTime:             ref.StartTime,
		EndTime:               ref.EndTime,
		Notes:                 ref.Notes,
		CreatedAt:             ref.CreatedAt,
		UpdatedAt:             ref.UpdatedAt,
	}
}

func (s *Service) bookableStaffByPOSID(ctx context.Context, salonID string, provider string, selectedStaff *StaffRef) (map[string]StaffRef, error) {
	refs := make(map[string]StaffRef)
	if selectedStaff != nil {
		refs[selectedStaff.POSStaffID] = *selectedStaff
		return refs, nil
	}
	staff, err := s.store.ListBookableStaffRefs(ctx, salonID)
	if err != nil {
		return nil, err
	}
	for _, item := range staff {
		if item.POSProvider != provider || strings.TrimSpace(item.POSStaffID) == "" {
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

func availabilityResult(req AvailabilityRequest, service ServiceRef, staff *StaffRef, schedule *Schedule, slots []AvailabilitySlot) *AvailabilityResult {
	timezone := ""
	if schedule != nil {
		timezone = strings.TrimSpace(schedule.Timezone)
	}
	result := &AvailabilityResult{
		ServiceID:       service.ID,
		ServiceName:     service.Name,
		PreferredDate:   req.PreferredDate,
		DurationMinutes: service.DurationMinutes,
		Timezone:        timezone,
		Slots:           slots,
	}
	if result.Slots == nil {
		result.Slots = []AvailabilitySlot{}
	}
	if staff != nil {
		result.StaffID = staff.ID
		result.StaffName = staff.Name
	}
	return result
}

func withinBusinessHours(startTime time.Time, endTime time.Time, hours []BusinessHour, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	startLocal := startTime.In(loc)
	endLocal := endTime.In(loc)
	if startLocal.Year() != endLocal.Year() || startLocal.YearDay() != endLocal.YearDay() {
		return false
	}
	for _, hour := range hours {
		if hour.DayOfWeek != int(startLocal.Weekday()) || hour.IsClosed {
			continue
		}
		openAt, ok := localClockDuration(hour.OpenTime)
		if !ok {
			continue
		}
		closeAt, ok := localClockDuration(hour.CloseTime)
		if !ok || closeAt <= openAt {
			continue
		}
		startAt := time.Duration(startLocal.Hour())*time.Hour + time.Duration(startLocal.Minute())*time.Minute + time.Duration(startLocal.Second())*time.Second
		endAt := time.Duration(endLocal.Hour())*time.Hour + time.Duration(endLocal.Minute())*time.Minute + time.Duration(endLocal.Second())*time.Second
		return startAt >= openAt && endAt <= closeAt
	}
	return false
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
