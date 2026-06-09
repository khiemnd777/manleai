package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error)
	SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error)
	SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error)
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
	customer, err := provider.SearchCustomerByPhone(ctx, salonID, req.CustomerPhone)
	if err != nil {
		return s.saveFallback(ctx, salonID, *service, *staff, req, endTime, provider.Name(), "search_customer", err)
	}
	if customer == nil {
		customer, err = provider.CreateCustomer(ctx, salonID, pos.CreateCustomerInput{
			Name:  req.CustomerName,
			Phone: req.CustomerPhone,
			Email: req.CustomerEmail,
		})
		if err != nil {
			return s.saveFallback(ctx, salonID, *service, *staff, req, endTime, provider.Name(), "create_customer", err)
		}
	}
	if customer == nil || strings.TrimSpace(customer.POSCustomerID) == "" {
		return s.saveFallback(ctx, salonID, *service, *staff, req, endTime, provider.Name(), "create_customer", fmt.Errorf("pos customer id was not returned"))
	}

	appointment, err := provider.CreateAppointment(ctx, salonID, pos.CreateAppointmentInput{
		CustomerID:      customer.POSCustomerID,
		ServiceID:       service.POSServiceID,
		ServiceVersion:  service.POSServiceVersion,
		StaffID:         staff.POSStaffID,
		StartTime:       req.StartTime,
		DurationMinutes: service.DurationMinutes,
		Notes:           req.Notes,
	})
	if err != nil {
		return s.saveFallback(ctx, salonID, *service, *staff, req, endTime, provider.Name(), "create_booking", err)
	}
	if appointment == nil || strings.TrimSpace(appointment.POSAppointmentID) == "" {
		return s.saveFallback(ctx, salonID, *service, *staff, req, endTime, provider.Name(), "create_booking", fmt.Errorf("pos booking id was not returned"))
	}
	if appointment.POSAppointmentVersion <= 0 {
		return s.saveFallback(ctx, salonID, *service, *staff, req, endTime, provider.Name(), "create_booking", fmt.Errorf("pos booking version was not returned"))
	}
	if !appointment.EndTime.IsZero() {
		endTime = appointment.EndTime
	}
	startTime := req.StartTime
	if !appointment.StartTime.IsZero() {
		startTime = appointment.StartTime
	}

	return s.store.SaveConfirmedBooking(ctx, ConfirmedBookingRecord{
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
	if appointment.Status == StatusCancelled || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion <= 0 {
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
	posAppointment, err := provider.RescheduleAppointment(ctx, salonID, appointment.POSAppointmentID, pos.RescheduleInput{
		BookingVersion:  appointment.POSAppointmentVersion,
		ServiceID:       appointment.Service.POSServiceID,
		ServiceVersion:  appointment.Service.POSServiceVersion,
		StaffID:         staff.POSStaffID,
		StartTime:       req.StartTime,
		DurationMinutes: durationMinutes,
		Notes:           notes,
	})
	if err != nil {
		fallback, saveErr := s.saveActionFallback(ctx, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, req.StartTime, endTime, notes, err)
		return nil, fallback, saveErr
	}
	if posAppointment == nil || strings.TrimSpace(posAppointment.POSAppointmentID) == "" {
		fallback, saveErr := s.saveActionFallback(ctx, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, req.StartTime, endTime, notes, fmt.Errorf("pos booking id was not returned"))
		return nil, fallback, saveErr
	}
	if posAppointment.POSAppointmentVersion <= 0 {
		fallback, saveErr := s.saveActionFallback(ctx, salonID, *appointment, appointment.POSProvider, "reschedule_booking", req.Source, NotificationTypeRescheduleFallback, req.StartTime, endTime, notes, fmt.Errorf("pos booking version was not returned"))
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

func (s *Service) Cancel(ctx context.Context, salonID string, ownerUserID string, appointmentID string, req CancelRequest) (*Appointment, *BookingAttempt, error) {
	req = normalizeCancelRequest(req)
	appointment, err := s.store.GetAppointmentForOwner(ctx, salonID, ownerUserID, strings.TrimSpace(appointmentID))
	if err != nil {
		return nil, nil, err
	}
	if appointment.Status == StatusCancelled || strings.TrimSpace(appointment.POSAppointmentID) == "" || appointment.POSAppointmentVersion <= 0 {
		return nil, nil, ErrValidation
	}

	provider := s.providers[appointment.POSProvider]
	if provider == nil {
		return nil, nil, ErrProviderUnavailable
	}

	posAppointment, err := provider.CancelAppointment(ctx, salonID, appointment.POSAppointmentID, pos.CancelInput{
		BookingVersion: appointment.POSAppointmentVersion,
		Reason:         req.Reason,
	})
	if err != nil {
		fallback, saveErr := s.saveActionFallback(ctx, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, appointment.StartTime, appointment.EndTime, req.Reason, err)
		return nil, fallback, saveErr
	}
	if posAppointment == nil || strings.TrimSpace(posAppointment.POSAppointmentID) == "" {
		fallback, saveErr := s.saveActionFallback(ctx, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, appointment.StartTime, appointment.EndTime, req.Reason, fmt.Errorf("pos booking id was not returned"))
		return nil, fallback, saveErr
	}
	if posAppointment.POSAppointmentVersion <= 0 {
		fallback, saveErr := s.saveActionFallback(ctx, salonID, *appointment, appointment.POSProvider, "cancel_booking", req.Source, NotificationTypeCancellationFallback, appointment.StartTime, appointment.EndTime, req.Reason, fmt.Errorf("pos booking version was not returned"))
		return nil, fallback, saveErr
	}

	saved, err := s.store.SaveCancelledAppointment(ctx, CancelledAppointmentRecord{
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

func (s *Service) saveFallback(ctx context.Context, salonID string, service ServiceRef, staff StaffRef, req CreateBookingRequest, endTime time.Time, provider string, operation string, providerErr error) (*BookingAttempt, error) {
	return s.store.SaveFallbackBooking(ctx, FallbackBookingRecord{
		SalonID:       salonID,
		Source:        req.Source,
		Provider:      provider,
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

func (s *Service) saveActionFallback(ctx context.Context, salonID string, appointment AppointmentActionRef, provider string, operation string, source string, notificationType string, startTime time.Time, endTime time.Time, notes string, providerErr error) (*BookingAttempt, error) {
	if strings.TrimSpace(source) == "" {
		source = SourceOwnerDashboard
	}
	return s.store.SaveAppointmentActionFallback(ctx, AppointmentActionFallbackRecord{
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

func validateRequest(req CreateBookingRequest) error {
	if req.CustomerName == "" || req.CustomerPhone == "" || req.ServiceID == "" || req.StaffID == "" || req.StartTime.IsZero() {
		return ErrValidation
	}
	return nil
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
