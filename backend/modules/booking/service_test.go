package booking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestCreateStoresConfirmedBookingOnlyAfterPOSSuccess(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 7,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                StatusConfirmed,
		},
	}
	provider.store = store
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if attempt.POSBookingID != "booking_1" {
		t.Fatalf("pos booking id = %s, want booking_1", attempt.POSBookingID)
	}
	if attempt.Appointment == nil {
		t.Fatalf("expected confirmed appointment")
	}
	if store.confirmed == nil {
		t.Fatalf("confirmed booking was not persisted")
	}
	if store.pending == nil {
		t.Fatalf("pending booking attempt was not created before POS call")
	}
	if !provider.searchSawPending {
		t.Fatalf("provider search did not see pending booking attempt")
	}
	if provider.lastCreateInput.IdempotencyKey != store.pending.POSIdempotencyKey {
		t.Fatalf("idempotency key = %s, want %s", provider.lastCreateInput.IdempotencyKey, store.pending.POSIdempotencyKey)
	}
	if store.fallback != nil {
		t.Fatalf("fallback should not be persisted on POS success")
	}
	if provider.createAppointmentCalls != 1 {
		t.Fatalf("create appointment calls = %d, want 1", provider.createAppointmentCalls)
	}
}

func TestCreateStoresFallbackPendingWhenPOSBookingFails(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer:         &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		createBookingErr: errors.New("square booking conflict"),
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusFallbackPending {
		t.Fatalf("status = %s, want fallback_pending", attempt.Status)
	}
	if attempt.POSBookingID != "" {
		t.Fatalf("fallback should not have POS booking id: %s", attempt.POSBookingID)
	}
	if attempt.Appointment != nil {
		t.Fatalf("fallback should not include confirmed appointment")
	}
	if store.confirmed != nil {
		t.Fatalf("confirmed booking should not be persisted on POS failure")
	}
	if store.pending == nil {
		t.Fatalf("pending booking attempt was not created before POS failure")
	}
	if store.fallback == nil {
		t.Fatalf("fallback was not persisted")
	}
	if store.fallback.AttemptID != "attempt_1" {
		t.Fatalf("fallback attempt id = %s, want attempt_1", store.fallback.AttemptID)
	}
	if store.fallback.ErrorCode != pos.ErrorBookingConflict {
		t.Fatalf("error code = %s, want %s", store.fallback.ErrorCode, pos.ErrorBookingConflict)
	}
}

func TestReschedulePersistsOnlyAfterPOSSuccess(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		rescheduledAppointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 8,
			StartTime:             testStartTime().Add(24 * time.Hour),
			EndTime:               testStartTime().Add(24*time.Hour + 45*time.Minute),
			Status:                StatusRescheduled,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		StartTime: testStartTime().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Reschedule returned error: %v", err)
	}
	if fallback != nil {
		t.Fatalf("fallback should not be persisted on POS success")
	}
	if appointment == nil || appointment.Status != StatusRescheduled {
		t.Fatalf("appointment status = %#v, want rescheduled", appointment)
	}
	if store.rescheduled == nil {
		t.Fatalf("rescheduled appointment was not persisted")
	}
	if store.actionFallback != nil {
		t.Fatalf("action fallback should not be persisted on POS success")
	}
	if provider.rescheduleCalls != 1 {
		t.Fatalf("reschedule calls = %d, want 1", provider.rescheduleCalls)
	}
	if provider.lastRescheduleInput.BookingVersion != 7 {
		t.Fatalf("booking version = %d, want 7", provider.lastRescheduleInput.BookingVersion)
	}
	if store.pendingAction == nil {
		t.Fatalf("pending reschedule attempt was not created before POS call")
	}
	if provider.lastRescheduleInput.IdempotencyKey != store.pendingAction.POSIdempotencyKey {
		t.Fatalf("idempotency key = %s, want %s", provider.lastRescheduleInput.IdempotencyKey, store.pendingAction.POSIdempotencyKey)
	}
}

func TestRescheduleStoresFallbackWhenPOSFails(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{rescheduleErr: errors.New("square booking conflict")}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		StartTime: testStartTime().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Reschedule returned error: %v", err)
	}
	if appointment != nil {
		t.Fatalf("appointment should not be updated on POS failure")
	}
	if fallback == nil || fallback.Status != StatusFallbackPending {
		t.Fatalf("fallback = %#v, want fallback_pending", fallback)
	}
	if store.rescheduled != nil {
		t.Fatalf("reschedule should not be persisted on POS failure")
	}
	if store.actionFallback == nil {
		t.Fatalf("action fallback was not persisted")
	}
	if store.actionFallback.AttemptID != "attempt_action_1" {
		t.Fatalf("fallback attempt id = %s, want attempt_action_1", store.actionFallback.AttemptID)
	}
	if store.actionFallback.ErrorCode != pos.ErrorBookingConflict {
		t.Fatalf("error code = %s, want %s", store.actionFallback.ErrorCode, pos.ErrorBookingConflict)
	}
}

func TestCancelPersistsOnlyAfterPOSSuccess(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		cancelledAppointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 8,
			Status:                StatusCancelled,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
		Reason: "Customer requested cancellation",
	})
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if fallback != nil {
		t.Fatalf("fallback should not be persisted on POS success")
	}
	if appointment == nil || appointment.Status != StatusCancelled {
		t.Fatalf("appointment status = %#v, want cancelled", appointment)
	}
	if store.cancelled == nil {
		t.Fatalf("cancelled appointment was not persisted")
	}
	if store.actionFallback != nil {
		t.Fatalf("action fallback should not be persisted on POS success")
	}
	if provider.cancelCalls != 1 {
		t.Fatalf("cancel calls = %d, want 1", provider.cancelCalls)
	}
	if provider.lastCancelInput.BookingVersion != 7 {
		t.Fatalf("booking version = %d, want 7", provider.lastCancelInput.BookingVersion)
	}
	if store.pendingAction == nil {
		t.Fatalf("pending cancel attempt was not created before POS call")
	}
	if provider.lastCancelInput.IdempotencyKey != store.pendingAction.POSIdempotencyKey {
		t.Fatalf("idempotency key = %s, want %s", provider.lastCancelInput.IdempotencyKey, store.pendingAction.POSIdempotencyKey)
	}
}

func TestCancelStoresFallbackWhenPOSFails(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{cancelErr: errors.New("square permission denied")}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
		Reason: "Customer requested cancellation",
	})
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if appointment != nil {
		t.Fatalf("appointment should not be updated on POS failure")
	}
	if fallback == nil || fallback.Status != StatusFallbackPending {
		t.Fatalf("fallback = %#v, want fallback_pending", fallback)
	}
	if store.cancelled != nil {
		t.Fatalf("cancel should not be persisted on POS failure")
	}
	if store.actionFallback == nil {
		t.Fatalf("action fallback was not persisted")
	}
	if store.actionFallback.AttemptID != "attempt_action_1" {
		t.Fatalf("fallback attempt id = %s, want attempt_action_1", store.actionFallback.AttemptID)
	}
	if store.actionFallback.ErrorCode != pos.ErrorPermissionDenied {
		t.Fatalf("error code = %s, want %s", store.actionFallback.ErrorCode, pos.ErrorPermissionDenied)
	}
}

func validCreateRequest() CreateBookingRequest {
	return CreateBookingRequest{
		CustomerName:  "Linh Tran",
		CustomerPhone: "312-555-0101",
		CustomerEmail: "linh@example.com",
		ServiceID:     "service_1",
		StaffID:       "staff_1",
		StartTime:     testStartTime(),
		Notes:         "First visit",
	}
}

func testStartTime() time.Time {
	return time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
}

type fakeStore struct {
	service        ServiceRef
	staff          StaffRef
	appointment    AppointmentActionRef
	pending        *PendingBookingRecord
	confirmed      *ConfirmedBookingRecord
	fallback       *FallbackBookingRecord
	pendingAction  *PendingAppointmentActionRecord
	rescheduled    *RescheduledAppointmentRecord
	cancelled      *CancelledAppointmentRecord
	actionFallback *AppointmentActionFallbackRecord
}

func newFakeStore() *fakeStore {
	store := &fakeStore{
		service: ServiceRef{
			ID:                "service_1",
			POSProvider:       pos.ProviderSquare,
			POSServiceID:      "square_service_1",
			POSServiceVersion: 123,
			Name:              "Classic Manicure",
			DurationMinutes:   45,
			PriceFrom:         35,
		},
		staff: StaffRef{
			ID:          "staff_1",
			POSProvider: pos.ProviderSquare,
			POSStaffID:  "square_staff_1",
			Name:        "Mai Nguyen",
		},
	}
	store.appointment = AppointmentActionRef{
		ID:                    "appointment_1",
		SalonID:               "salon_1",
		POSProvider:           pos.ProviderSquare,
		POSAppointmentID:      "booking_1",
		POSAppointmentVersion: 7,
		Status:                StatusConfirmed,
		CustomerName:          "Linh Tran",
		CustomerPhone:         "+13125550101",
		CustomerEmail:         "linh@example.com",
		Service:               store.service,
		Staff:                 store.staff,
		StartTime:             testStartTime(),
		EndTime:               testStartTime().Add(45 * time.Minute),
		Notes:                 "First visit",
	}
	return store
}

func (f *fakeStore) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return pos.ErrNotFound
	}
	return nil
}

func (f *fakeStore) GetBookableService(ctx context.Context, salonID string, serviceID string) (*ServiceRef, error) {
	if serviceID != f.service.ID {
		return nil, pos.ErrNotFound
	}
	return &f.service, nil
}

func (f *fakeStore) GetBookableStaff(ctx context.Context, salonID string, staffID string) (*StaffRef, error) {
	if staffID != f.staff.ID {
		return nil, pos.ErrNotFound
	}
	return &f.staff, nil
}

func (f *fakeStore) CreatePendingBookingAttempt(ctx context.Context, record PendingBookingRecord) (*BookingAttempt, error) {
	f.pending = &record
	return &BookingAttempt{
		ID:                 "attempt_1",
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusPOSPending,
		POSProvider:        record.Provider,
		POSIdempotencyKey:  record.POSIdempotencyKey,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
	}, nil
}

func (f *fakeStore) SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error) {
	f.confirmed = &record
	return &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusConfirmed,
		POSProvider:        record.Provider,
		POSBookingID:       record.POSBookingID,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		Appointment: &Appointment{
			ID:                    "appointment_1",
			POSAppointmentID:      record.POSBookingID,
			POSAppointmentVersion: record.POSBookingVersion,
			Status:                StatusConfirmed,
		},
	}, nil
}

func (f *fakeStore) SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error) {
	f.fallback = &record
	return &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusFallbackPending,
		POSProvider:        record.Provider,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
	}, nil
}

func (f *fakeStore) GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error) {
	if salonID != "salon_1" || ownerUserID != "owner_1" || appointmentID != f.appointment.ID {
		return nil, pos.ErrNotFound
	}
	return &f.appointment, nil
}

func (f *fakeStore) CreatePendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingAttempt, error) {
	f.pendingAction = &record
	return &BookingAttempt{
		ID:                 "attempt_action_1",
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusPOSPending,
		POSProvider:        record.Provider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		POSIdempotencyKey:  record.POSIdempotencyKey,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		ServiceID:          record.Appointment.Service.ID,
		StaffID:            record.Appointment.Staff.ID,
		RequestedStartTime: record.RequestedStartTime,
		RequestedEndTime:   record.RequestedEndTime,
	}, nil
}

func (f *fakeStore) SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error) {
	f.rescheduled = &record
	f.appointment.Status = StatusRescheduled
	f.appointment.StartTime = record.StartTime
	f.appointment.EndTime = record.EndTime
	f.appointment.Staff = record.Staff
	f.appointment.POSAppointmentVersion = record.POSBookingVersion
	return appointmentFromActionRef(f.appointment), nil
}

func (f *fakeStore) SaveCancelledAppointment(ctx context.Context, record CancelledAppointmentRecord) (*Appointment, error) {
	f.cancelled = &record
	f.appointment.Status = StatusCancelled
	f.appointment.POSAppointmentVersion = record.POSBookingVersion
	return appointmentFromActionRef(f.appointment), nil
}

func (f *fakeStore) SaveAppointmentActionFallback(ctx context.Context, record AppointmentActionFallbackRecord) (*BookingAttempt, error) {
	f.actionFallback = &record
	return &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Status:             StatusFallbackPending,
		POSProvider:        record.Provider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		ServiceID:          record.Appointment.Service.ID,
		StaffID:            record.Appointment.Staff.ID,
		RequestedStartTime: record.RequestedStartTime,
		RequestedEndTime:   record.RequestedEndTime,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
	}, nil
}

func (f *fakeStore) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	return nil, pos.ErrNotFound
}

func (f *fakeStore) ListAppointments(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Appointment, error) {
	return nil, nil
}

func (f *fakeStore) ListBookingAttempts(ctx context.Context, salonID string, ownerUserID string, limit int) ([]BookingAttempt, error) {
	return nil, nil
}

type fakeProvider struct {
	customer               *pos.Customer
	appointment            *pos.Appointment
	rescheduledAppointment *pos.Appointment
	cancelledAppointment   *pos.Appointment
	searchCustomerErr      error
	createCustomerErr      error
	createBookingErr       error
	rescheduleErr          error
	cancelErr              error
	store                  *fakeStore
	lastCreateInput        pos.CreateAppointmentInput
	lastRescheduleInput    pos.RescheduleInput
	lastCancelInput        pos.CancelInput
	searchSawPending       bool
	createAppointmentCalls int
	rescheduleCalls        int
	cancelCalls            int
}

func (f *fakeProvider) Name() string {
	return pos.ProviderSquare
}

func (f *fakeProvider) Connect(ctx context.Context, input pos.ConnectInput) (*pos.Connection, error) {
	return nil, nil
}

func (f *fakeProvider) HealthCheck(ctx context.Context, salonID string) error {
	return nil
}

func (f *fakeProvider) ListLocations(ctx context.Context, salonID string) ([]pos.Location, error) {
	return nil, nil
}

func (f *fakeProvider) ListServices(ctx context.Context, salonID string) ([]pos.Service, error) {
	return nil, nil
}

func (f *fakeProvider) ListStaff(ctx context.Context, salonID string) ([]pos.StaffMember, error) {
	return nil, nil
}

func (f *fakeProvider) SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*pos.Customer, error) {
	if f.store != nil && f.store.pending != nil {
		f.searchSawPending = true
	}
	if f.searchCustomerErr != nil {
		return nil, f.searchCustomerErr
	}
	return f.customer, nil
}

func (f *fakeProvider) CreateCustomer(ctx context.Context, salonID string, input pos.CreateCustomerInput) (*pos.Customer, error) {
	if f.createCustomerErr != nil {
		return nil, f.createCustomerErr
	}
	return &pos.Customer{POSCustomerID: "cust_created", Name: input.Name, Phone: input.Phone, Email: input.Email}, nil
}

func (f *fakeProvider) CheckAvailability(ctx context.Context, salonID string, input pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	return nil, nil
}

func (f *fakeProvider) CreateAppointment(ctx context.Context, salonID string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	f.createAppointmentCalls++
	f.lastCreateInput = input
	if f.createBookingErr != nil {
		return nil, f.createBookingErr
	}
	return f.appointment, nil
}

func (f *fakeProvider) RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input pos.RescheduleInput) (*pos.Appointment, error) {
	f.rescheduleCalls++
	f.lastRescheduleInput = input
	if f.rescheduleErr != nil {
		return nil, f.rescheduleErr
	}
	return f.rescheduledAppointment, nil
}

func (f *fakeProvider) CancelAppointment(ctx context.Context, salonID string, appointmentID string, input pos.CancelInput) (*pos.Appointment, error) {
	f.cancelCalls++
	f.lastCancelInput = input
	if f.cancelErr != nil {
		return nil, f.cancelErr
	}
	return f.cancelledAppointment, nil
}

func (f *fakeProvider) Sync(ctx context.Context, salonID string) error {
	return nil
}
