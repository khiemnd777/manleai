package booking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestAppointmentsReturnsPaginationMetadata(t *testing.T) {
	store := newFakeStore()
	store.appointments = make([]Appointment, 201)
	for i := range store.appointments {
		store.appointments[i] = Appointment{ID: "appointment_1"}
	}
	service := NewService(store, nil)

	res, err := service.Appointments(context.Background(), "salon_1", "owner_1", 500, 30)
	if err != nil {
		t.Fatalf("Appointments returned error: %v", err)
	}
	if len(res.Appointments) != 200 {
		t.Fatalf("appointments = %d, want 200", len(res.Appointments))
	}
	if res.Limit != 200 {
		t.Fatalf("response limit = %d, want 200", res.Limit)
	}
	if res.Offset != 30 {
		t.Fatalf("response offset = %d, want 30", res.Offset)
	}
	if !res.HasMore {
		t.Fatal("has_more = false, want true")
	}
	if store.listAppointmentLimit != 201 {
		t.Fatalf("store limit = %d, want 201", store.listAppointmentLimit)
	}
	if store.listAppointmentOffset != 30 {
		t.Fatalf("store offset = %d, want 30", store.listAppointmentOffset)
	}
}

func TestAppointmentsDefaultsPagination(t *testing.T) {
	store := newFakeStore()
	store.appointments = []Appointment{{ID: "appointment_1"}}
	service := NewService(store, nil)

	res, err := service.Appointments(context.Background(), "salon_1", "owner_1", 0, -10)
	if err != nil {
		t.Fatalf("Appointments returned error: %v", err)
	}
	if len(res.Appointments) != 1 {
		t.Fatalf("appointments = %d, want 1", len(res.Appointments))
	}
	if res.Limit != 50 {
		t.Fatalf("response limit = %d, want 50", res.Limit)
	}
	if res.Offset != 0 {
		t.Fatalf("response offset = %d, want 0", res.Offset)
	}
	if res.HasMore {
		t.Fatal("has_more = true, want false")
	}
	if store.listAppointmentLimit != 51 {
		t.Fatalf("store limit = %d, want 51", store.listAppointmentLimit)
	}
	if store.listAppointmentOffset != 0 {
		t.Fatalf("store offset = %d, want 0", store.listAppointmentOffset)
	}
}

func TestAttemptsReturnsPaginationMetadata(t *testing.T) {
	store := newFakeStore()
	store.bookingAttempts = make([]BookingAttempt, 51)
	for i := range store.bookingAttempts {
		store.bookingAttempts[i] = BookingAttempt{ID: "attempt_1", Status: StatusFallbackPending}
	}
	service := NewService(store, nil)

	res, err := service.Attempts(context.Background(), "salon_1", "owner_1", StatusFallbackPending, 50, 20)
	if err != nil {
		t.Fatalf("Attempts returned error: %v", err)
	}
	if len(res.BookingAttempts) != 50 {
		t.Fatalf("booking attempts = %d, want 50", len(res.BookingAttempts))
	}
	if res.Limit != 50 {
		t.Fatalf("response limit = %d, want 50", res.Limit)
	}
	if res.Offset != 20 {
		t.Fatalf("response offset = %d, want 20", res.Offset)
	}
	if res.Status != StatusFallbackPending {
		t.Fatalf("response status = %s, want fallback_pending", res.Status)
	}
	if !res.HasMore {
		t.Fatal("has_more = false, want true")
	}
	if store.listBookingAttemptStatus != StatusFallbackPending {
		t.Fatalf("store status = %s, want fallback_pending", store.listBookingAttemptStatus)
	}
	if store.listBookingAttemptLimit != 51 {
		t.Fatalf("store limit = %d, want 51", store.listBookingAttemptLimit)
	}
	if store.listBookingAttemptOffset != 20 {
		t.Fatalf("store offset = %d, want 20", store.listBookingAttemptOffset)
	}
}

func TestAttemptsRejectsInvalidStatusFilter(t *testing.T) {
	store := newFakeStore()
	service := NewService(store, nil)

	_, err := service.Attempts(context.Background(), "salon_1", "owner_1", "unknown", 10, 0)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestCalendarReturnsRangeAppointmentsPendingRequestsAndWarnings(t *testing.T) {
	store := newFakeStore()
	lastSyncedAt := testStartTime().Add(-time.Hour)
	store.calendarAppointments = []Appointment{
		{
			ID:                    "appointment_synced",
			POSAppointmentID:      "booking_synced",
			POSAppointmentVersion: 2,
			Status:                StatusConfirmed,
			POSSyncStatus:         POSSyncStatusSynced,
			LastPOSSyncedAt:       &lastSyncedAt,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
		},
		{
			ID:                    "appointment_failed",
			POSAppointmentID:      "booking_failed",
			POSAppointmentVersion: 3,
			Status:                StatusConfirmed,
			POSSyncStatus:         POSSyncStatusFailed,
			POSSyncError:          "Square timeout",
			StartTime:             testStartTime().Add(time.Hour),
			EndTime:               testStartTime().Add(2 * time.Hour),
		},
	}
	store.calendarPendingRequests = []BookingAttempt{{
		ID:                 "attempt_pending",
		Status:             StatusFallbackPending,
		RequestedStartTime: testStartTime().Add(3 * time.Hour),
		RequestedEndTime:   testStartTime().Add(4 * time.Hour),
	}}
	service := NewService(store, nil)
	start := testStartTime().Add(-24 * time.Hour)
	end := testStartTime().Add(24 * time.Hour)

	res, err := service.Calendar(context.Background(), "salon_1", "owner_1", CalendarRangeRequest{
		StartTime: start,
		EndTime:   end,
		View:      "week",
	})
	if err != nil {
		t.Fatalf("Calendar returned error: %v", err)
	}
	if !store.calendarStartTime.Equal(start) || !store.calendarEndTime.Equal(end) {
		t.Fatalf("calendar range = %s/%s, want %s/%s", store.calendarStartTime, store.calendarEndTime, start, end)
	}
	if len(res.Appointments) != 2 || len(res.PendingRequests) != 1 {
		t.Fatalf("calendar counts appointments=%d pending=%d, want 2/1", len(res.Appointments), len(res.PendingRequests))
	}
	if res.Appointments[0].SyncWarning != "" {
		t.Fatalf("synced appointment warning = %q, want empty", res.Appointments[0].SyncWarning)
	}
	if res.Appointments[1].SyncWarning != "Square timeout" {
		t.Fatalf("failed warning = %q, want Square timeout", res.Appointments[1].SyncWarning)
	}
	if res.PendingRequests[0].SyncWarning == "" || !res.PendingRequests[0].CanRetry {
		t.Fatalf("pending request warning/retry = %#v, want warning and retry", res.PendingRequests[0])
	}
	if res.Warnings.SyncFailed != 1 || res.Warnings.FallbackPending != 1 || res.Warnings.TotalWarnings != 2 {
		t.Fatalf("warnings = %#v, want one sync failed and one fallback", res.Warnings)
	}
}

func TestSyncCalendarListsPOSAppointmentsAndUpsertsProviderNeutralImports(t *testing.T) {
	store := newFakeStore()
	store.calendarSyncSummary = CalendarSyncSummary{Imported: 1, Updated: 0}
	provider := &fakeProvider{
		listAppointments: []pos.ListedAppointment{{
			POSAppointmentID:      "booking_square_1",
			POSAppointmentVersion: 5,
			Status:                "accepted",
			POSCustomerID:         "customer_square_1",
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Notes:                 "Imported from Square",
			Segments: []pos.ListedAppointmentSegment{{
				POSServiceID:      "square_service_1",
				POSServiceVersion: 123,
				POSStaffID:        "square_staff_1",
				DurationMinutes:   45,
			}},
		}},
	}
	service := NewService(store, []pos.POSProvider{provider})
	start := testStartTime().Add(-24 * time.Hour)
	end := testStartTime().Add(24 * time.Hour)

	res, err := service.SyncCalendar(context.Background(), "salon_1", "owner_1", CalendarSyncRequest{
		StartTime: start,
		EndTime:   end,
	})
	if err != nil {
		t.Fatalf("SyncCalendar returned error: %v", err)
	}
	if provider.listAppointmentCalls != 1 {
		t.Fatalf("list calls = %d, want 1", provider.listAppointmentCalls)
	}
	if !provider.lastListInput.StartTime.Equal(start) || !provider.lastListInput.EndTime.Equal(end) {
		t.Fatalf("provider range = %#v, want start/end", provider.lastListInput)
	}
	if len(store.calendarImports) != 1 {
		t.Fatalf("imports = %d, want 1", len(store.calendarImports))
	}
	got := store.calendarImports[0]
	if got.POSAppointmentID != "booking_square_1" || got.Status != StatusConfirmed || got.Segments[0].POSServiceID != "square_service_1" {
		t.Fatalf("import = %#v, want provider-neutral Square booking", got)
	}
	if store.calendarSyncLogStatus != "succeeded" || store.calendarProviderStatus != pos.StatusActive {
		t.Fatalf("sync log/provider status = %s/%s, want succeeded/active", store.calendarSyncLogStatus, store.calendarProviderStatus)
	}
	if res.Provider != pos.ProviderSquare || res.Summary.Imported != 1 {
		t.Fatalf("response = %#v, want square imported summary", res)
	}
}

func TestRescheduleCandidatesRequiresCallerPhone(t *testing.T) {
	store := newFakeStore()
	service := NewService(store, nil)

	_, err := service.RescheduleCandidates(context.Background(), "salon_1", "owner_1", RescheduleLookupRequest{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestRescheduleCandidatesNormalizesPhoneAndClampsLimit(t *testing.T) {
	store := newFakeStore()
	service := NewService(store, nil)

	candidates, err := service.RescheduleCandidates(context.Background(), "salon_1", "owner_1", RescheduleLookupRequest{
		CustomerName:  " Linh Tran ",
		CustomerPhone: "(312) 555-0101",
		Limit:         50,
	})
	if err != nil {
		t.Fatalf("RescheduleCandidates returned error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != "appointment_1" {
		t.Fatalf("candidates = %#v, want appointment_1", candidates)
	}
	if store.rescheduleLookup.CustomerPhone != "3125550101" {
		t.Fatalf("lookup phone = %q, want normalized", store.rescheduleLookup.CustomerPhone)
	}
	if store.rescheduleLookup.Limit != 5 {
		t.Fatalf("lookup limit = %d, want 5", store.rescheduleLookup.Limit)
	}
}

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

func TestCreatePersistsStaffSelectionModeAndSingleSegment(t *testing.T) {
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
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.StaffSelectionMode = StaffSelectionAnyone

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if provider.lastCreateInput.StaffID != "square_staff_1" {
		t.Fatalf("provider staff id = %s, want resolved Square staff", provider.lastCreateInput.StaffID)
	}
	if len(provider.lastCreateInput.Segments) != 1 {
		t.Fatalf("provider create segments = %#v, want one segment", provider.lastCreateInput.Segments)
	}
	createSegment := provider.lastCreateInput.Segments[0]
	if createSegment.ServiceID != "square_service_1" || createSegment.ServiceVersion != 123 || createSegment.StaffID != "square_staff_1" || createSegment.DurationMinutes != 45 {
		t.Fatalf("provider create segment = %#v, want POS-backed segment", createSegment)
	}
	if store.pending == nil || store.pending.StaffSelectionMode != StaffSelectionAnyone {
		t.Fatalf("pending mode = %#v, want anyone", store.pending)
	}
	if len(store.pending.Segments) != 1 {
		t.Fatalf("pending segments = %#v, want one segment", store.pending.Segments)
	}
	pendingSegment := store.pending.Segments[0]
	if pendingSegment.StaffSelectionMode != StaffSelectionAnyone || pendingSegment.Service.ID != "service_1" || pendingSegment.Staff.ID != "staff_1" || pendingSegment.SortOrder != 1 {
		t.Fatalf("pending segment = %#v, want anyone segment snapshot", pendingSegment)
	}
	if store.confirmed == nil || store.confirmed.StaffSelectionMode != StaffSelectionAnyone {
		t.Fatalf("confirmed mode = %#v, want anyone", store.confirmed)
	}
	if len(store.confirmed.Segments) != 1 {
		t.Fatalf("confirmed segments = %#v, want one segment", store.confirmed.Segments)
	}
	confirmedSegment := store.confirmed.Segments[0]
	if confirmedSegment.StaffSelectionMode != StaffSelectionAnyone || confirmedSegment.Service.POSServiceID != "square_service_1" || confirmedSegment.Staff.POSStaffID != "square_staff_1" {
		t.Fatalf("confirmed segment = %#v, want POS-backed segment snapshot", confirmedSegment)
	}
}

func TestCreateSupportsMultipleSegments(t *testing.T) {
	store := newFakeStore()
	store.services = append(store.services, ServiceRef{
		ID:                "service_2",
		POSProvider:       pos.ProviderSquare,
		POSServiceID:      "square_service_2",
		POSServiceVersion: 456,
		Name:              "Gel Removal",
		DurationMinutes:   30,
		PriceFrom:         15,
	})
	store.staffRefs = append(store.staffRefs, StaffRef{
		ID:          "staff_2",
		POSProvider: pos.ProviderSquare,
		POSStaffID:  "square_staff_2",
		Name:        "An Nguyen",
	})
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 7,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(75 * time.Minute),
			Status:                StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.ServiceID = ""
	req.StaffID = ""
	req.Segments = []BookingSegmentRequest{
		{ServiceID: "service_1", StaffID: "staff_1"},
		{ServiceID: "service_2", StaffID: "staff_2"},
	}

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if len(provider.lastCreateInput.Segments) != 2 {
		t.Fatalf("provider segments = %#v, want two", provider.lastCreateInput.Segments)
	}
	if provider.lastCreateInput.Segments[0].ServiceID != "square_service_1" || provider.lastCreateInput.Segments[1].ServiceID != "square_service_2" {
		t.Fatalf("provider segments = %#v, want ordered POS services", provider.lastCreateInput.Segments)
	}
	if store.pending == nil || len(store.pending.Segments) != 2 {
		t.Fatalf("pending segments = %#v, want two", store.pending)
	}
	if store.pending.EndTime.Sub(store.pending.StartTime) != 75*time.Minute {
		t.Fatalf("pending duration = %s, want 75m", store.pending.EndTime.Sub(store.pending.StartTime))
	}
	if store.confirmed == nil || len(store.confirmed.Segments) != 2 {
		t.Fatalf("confirmed segments = %#v, want two", store.confirmed)
	}
	if store.confirmed.Segments[1].Service.ID != "service_2" || store.confirmed.Segments[1].Staff.ID != "staff_2" || store.confirmed.Segments[1].SortOrder != 2 {
		t.Fatalf("second confirmed segment = %#v, want service_2/staff_2", store.confirmed.Segments[1])
	}
}

func TestCreateAcceptsPOSBookingVersionZero(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 0,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if store.confirmed == nil || store.confirmed.POSBookingVersion != 0 {
		t.Fatalf("confirmed version = %#v, want 0", store.confirmed)
	}
	if store.fallback != nil {
		t.Fatalf("fallback should not be persisted on POS success")
	}
}

func TestCreateFinalizesConfirmedBookingAfterRequestContextCancelledPostPOSSuccess(t *testing.T) {
	store := newFakeStore()
	ctx, cancel := context.WithCancel(context.Background())
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 0,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                StatusConfirmed,
		},
		afterCreateAppointment: cancel,
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(ctx, "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatalf("test did not cancel request context")
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if store.confirmed == nil || store.confirmed.POSBookingID != "booking_1" {
		t.Fatalf("confirmed booking = %#v, want POS booking persisted", store.confirmed)
	}
}

func TestCreateStoresFallbackWhenPOSBookingVersionMissing(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: -1,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusFallbackPending {
		t.Fatalf("status = %s, want fallback_pending", attempt.Status)
	}
	if store.confirmed != nil {
		t.Fatalf("confirmed booking should not be persisted without POS version")
	}
	if store.fallback == nil || store.fallback.ErrorMessage != "pos booking version was not returned" {
		t.Fatalf("fallback = %#v, want missing version", store.fallback)
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

func TestCreateUsesLinkedCanonicalCustomerWithoutPOSLookup(t *testing.T) {
	store := newFakeStore()
	store.customer.POSProvider = pos.ProviderSquare
	store.customer.POSCustomerID = "square_customer_linked"
	provider := &fakeProvider{
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 7,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if provider.searchCustomerCalls != 0 || provider.createCustomerCalls != 0 {
		t.Fatalf("customer POS calls search=%d create=%d, want none", provider.searchCustomerCalls, provider.createCustomerCalls)
	}
	if provider.lastCreateInput.CustomerID != "square_customer_linked" {
		t.Fatalf("provider customer id = %s, want linked customer", provider.lastCreateInput.CustomerID)
	}
}

func TestCreateLinksSearchedPOSCustomerBeforeBooking(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "square_customer_found", Name: "Linh Tran", Phone: "3125550101", Email: "linh@example.com"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 7,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusConfirmed {
		t.Fatalf("status = %s, want confirmed", attempt.Status)
	}
	if provider.searchCustomerCalls != 1 || provider.createCustomerCalls != 0 {
		t.Fatalf("customer POS calls search=%d create=%d, want search only", provider.searchCustomerCalls, provider.createCustomerCalls)
	}
	if store.linkedCustomer == nil || store.linkedCustomer.POSCustomerID != "square_customer_found" {
		t.Fatalf("linked customer = %#v, want searched POS customer", store.linkedCustomer)
	}
	if provider.lastCreateInput.CustomerID != "square_customer_found" {
		t.Fatalf("provider customer id = %s, want searched customer", provider.lastCreateInput.CustomerID)
	}
}

func TestCreateStoresFallbackWhenCustomerLinkFails(t *testing.T) {
	store := newFakeStore()
	store.linkCustomerErr = errors.New("link customer failed")
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "square_customer_found", Name: "Linh Tran", Phone: "3125550101"},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusFallbackPending {
		t.Fatalf("status = %s, want fallback_pending", attempt.Status)
	}
	if store.pending == nil {
		t.Fatalf("pending booking attempt was not created before customer link failure")
	}
	if store.confirmed != nil {
		t.Fatalf("confirmed booking should not be persisted when customer link fails")
	}
	if provider.createAppointmentCalls != 0 {
		t.Fatalf("create appointment calls = %d, want 0", provider.createAppointmentCalls)
	}
	if store.fallback == nil || store.fallback.Operation != "link_customer" || store.fallback.ErrorCode != pos.ErrorBookingFailed {
		t.Fatalf("fallback = %#v, want link_customer booking failure", store.fallback)
	}
}

func TestCreateRejectsUnlinkedCanonicalRecordsBeforePOS(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeStore)
	}{
		{
			name: "service without provider link",
			mutate: func(store *fakeStore) {
				store.services[0].POSServiceID = ""
				store.services[0].POSServiceVersion = 0
			},
		},
		{
			name: "service without provider version",
			mutate: func(store *fakeStore) {
				store.services[0].POSServiceVersion = 0
			},
		},
		{
			name: "staff without provider link",
			mutate: func(store *fakeStore) {
				store.staffRefs[0].POSStaffID = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			tt.mutate(store)
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
			service := NewService(store, []pos.POSProvider{provider})

			attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Create error = %v, want ErrValidation", err)
			}
			if attempt != nil {
				t.Fatalf("attempt = %#v, want nil", attempt)
			}
			if store.pending != nil || store.confirmed != nil || store.fallback != nil {
				t.Fatalf("persistence happened for unlinked record: pending=%#v confirmed=%#v fallback=%#v", store.pending, store.confirmed, store.fallback)
			}
			if provider.createAppointmentCalls != 0 {
				t.Fatalf("provider create calls = %d, want 0", provider.createAppointmentCalls)
			}
		})
	}
}

func TestAvailableSlotsRejectsUnlinkedCanonicalRecordsBeforePOS(t *testing.T) {
	tests := []struct {
		name   string
		req    AvailabilityRequest
		mutate func(*fakeStore)
	}{
		{
			name: "service without provider link",
			req: AvailabilityRequest{
				ServiceID:     "service_1",
				PreferredDate: "2026-06-15",
			},
			mutate: func(store *fakeStore) {
				store.services[0].POSServiceID = ""
			},
		},
		{
			name: "service without provider version",
			req: AvailabilityRequest{
				ServiceID:     "service_1",
				PreferredDate: "2026-06-15",
			},
			mutate: func(store *fakeStore) {
				store.services[0].POSServiceVersion = 0
			},
		},
		{
			name: "staff without provider link",
			req: AvailabilityRequest{
				ServiceID:     "service_1",
				StaffID:       "staff_1",
				PreferredDate: "2026-06-15",
			},
			mutate: func(store *fakeStore) {
				store.staffRefs[0].POSStaffID = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			tt.mutate(store)
			provider := &fakeProvider{
				availabilitySlots: []pos.TimeSlot{
					{
						StartTime: testStartTime(),
						EndTime:   testStartTime().Add(45 * time.Minute),
						StaffID:   "square_staff_1",
					},
				},
			}
			service := NewService(store, []pos.POSProvider{provider})

			result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", tt.req)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("AvailableSlots error = %v, want ErrValidation", err)
			}
			if result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
			if provider.availabilityCalls != 0 {
				t.Fatalf("provider availability calls = %d, want 0", provider.availabilityCalls)
			}
		})
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
	if len(provider.lastRescheduleInput.Segments) != 1 {
		t.Fatalf("provider reschedule segments = %#v, want one segment", provider.lastRescheduleInput.Segments)
	}
	rescheduleSegment := provider.lastRescheduleInput.Segments[0]
	if rescheduleSegment.ServiceID != "square_service_1" || rescheduleSegment.ServiceVersion != 123 || rescheduleSegment.StaffID != "square_staff_1" || rescheduleSegment.DurationMinutes != 45 {
		t.Fatalf("provider reschedule segment = %#v, want POS-backed segment", rescheduleSegment)
	}
	if store.pendingAction == nil {
		t.Fatalf("pending reschedule attempt was not created before POS call")
	}
	if provider.lastRescheduleInput.IdempotencyKey != store.pendingAction.POSIdempotencyKey {
		t.Fatalf("idempotency key = %s, want %s", provider.lastRescheduleInput.IdempotencyKey, store.pendingAction.POSIdempotencyKey)
	}
}

func TestRescheduleSupportsMultipleSegments(t *testing.T) {
	store := newFakeStore()
	store.addSecondAppointmentSegment()
	nextStart := testStartTime().Add(24 * time.Hour)
	provider := &fakeProvider{
		rescheduledAppointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 8,
			StartTime:             nextStart,
			EndTime:               nextStart.Add(75 * time.Minute),
			Status:                StatusRescheduled,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		StartTime: nextStart,
	})
	if err != nil {
		t.Fatalf("Reschedule returned error: %v", err)
	}
	if fallback != nil {
		t.Fatalf("fallback should not be persisted on POS success")
	}
	if appointment == nil || len(appointment.Segments) != 2 {
		t.Fatalf("appointment segments = %#v, want two", appointment)
	}
	if provider.lastRescheduleInput.DurationMinutes != 75 {
		t.Fatalf("duration = %d, want 75", provider.lastRescheduleInput.DurationMinutes)
	}
	if len(provider.lastRescheduleInput.Segments) != 2 {
		t.Fatalf("provider reschedule segments = %#v, want two", provider.lastRescheduleInput.Segments)
	}
	if provider.lastRescheduleInput.Segments[0].ServiceID != "square_service_1" || provider.lastRescheduleInput.Segments[1].ServiceID != "square_service_2" {
		t.Fatalf("provider reschedule segments = %#v, want ordered POS services", provider.lastRescheduleInput.Segments)
	}
	if provider.lastRescheduleInput.Segments[1].StaffID != "square_staff_2" || provider.lastRescheduleInput.Segments[1].DurationMinutes != 30 {
		t.Fatalf("second provider segment = %#v, want staff_2 30m", provider.lastRescheduleInput.Segments[1])
	}
	if store.pendingAction == nil || len(store.pendingAction.Segments) != 2 {
		t.Fatalf("pending action segments = %#v, want two", store.pendingAction)
	}
	if store.rescheduled == nil || len(store.rescheduled.Segments) != 2 {
		t.Fatalf("rescheduled segments = %#v, want two", store.rescheduled)
	}
	if store.appointment.EndTime.Sub(store.appointment.StartTime) != 75*time.Minute {
		t.Fatalf("appointment duration = %s, want 75m", store.appointment.EndTime.Sub(store.appointment.StartTime))
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

func TestRescheduleMultiSegmentFallbackLeavesAppointmentUnchanged(t *testing.T) {
	store := newFakeStore()
	store.addSecondAppointmentSegment()
	originalStart := store.appointment.StartTime
	originalEnd := store.appointment.EndTime
	originalSegments := append([]BookingSegmentRecord(nil), store.appointment.Segments...)
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
	if store.appointment.Status != StatusConfirmed || !store.appointment.StartTime.Equal(originalStart) || !store.appointment.EndTime.Equal(originalEnd) {
		t.Fatalf("appointment changed after POS failure: %#v", store.appointment)
	}
	if len(store.appointment.Segments) != len(originalSegments) || store.appointment.Segments[1].Staff.ID != originalSegments[1].Staff.ID {
		t.Fatalf("appointment segments changed after POS failure: %#v", store.appointment.Segments)
	}
	if store.pendingAction == nil || len(store.pendingAction.Segments) != 2 {
		t.Fatalf("pending action segments = %#v, want two", store.pendingAction)
	}
	if store.actionFallback == nil || len(store.actionFallback.Segments) != 2 {
		t.Fatalf("action fallback segments = %#v, want two", store.actionFallback)
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

func TestCancelFallbackSnapshotsMultipleSegments(t *testing.T) {
	store := newFakeStore()
	store.addSecondAppointmentSegment()
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
	if fallback == nil || len(fallback.Segments) != 2 {
		t.Fatalf("fallback segments = %#v, want two", fallback)
	}
	if store.pendingAction == nil || len(store.pendingAction.Segments) != 2 {
		t.Fatalf("pending cancel segments = %#v, want two", store.pendingAction)
	}
	if store.actionFallback == nil || len(store.actionFallback.Segments) != 2 {
		t.Fatalf("action fallback segments = %#v, want two", store.actionFallback)
	}
	if store.cancelled != nil {
		t.Fatalf("cancel should not be persisted on POS failure")
	}
}

func TestAvailableSlotsFiltersBusinessHoursAndMapsStaff(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	store := newFakeStore()
	provider := &fakeProvider{
		availabilitySlots: []pos.TimeSlot{
			{
				StartTime: time.Date(2026, 6, 15, 8, 45, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 9, 30, 0, 0, loc).UTC(),
				StaffID:   "square_staff_1",
			},
			{
				StartTime: time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 10, 45, 0, 0, loc).UTC(),
				StaffID:   "square_staff_1",
			},
			{
				StartTime: time.Date(2026, 6, 15, 18, 30, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 19, 15, 0, 0, loc).UTC(),
				StaffID:   "square_staff_1",
			},
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		ServiceID:     "service_1",
		StaffID:       "staff_1",
		PreferredDate: "2026-06-15",
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("AvailableSlots returned error: %v", err)
	}
	if provider.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", provider.availabilityCalls)
	}
	if provider.lastAvailabilityInput.ServiceID != "square_service_1" || provider.lastAvailabilityInput.StaffID != "square_staff_1" {
		t.Fatalf("provider input = %#v, want POS service/staff IDs", provider.lastAvailabilityInput)
	}
	if len(result.Slots) != 1 {
		t.Fatalf("slots = %#v, want one business-hours slot", result.Slots)
	}
	slot := result.Slots[0]
	if slot.StaffID != "staff_1" || slot.StaffName != "Mai Nguyen" {
		t.Fatalf("slot staff = %s/%s, want internal staff mapping", slot.StaffID, slot.StaffName)
	}
	if !slot.StartTime.Equal(time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC()) {
		t.Fatalf("slot start = %s, want 10am local", slot.StartTime)
	}
}

func TestAvailableSlotsRequiresSlotInsideOneBusinessHourPeriod(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	store := newFakeStore()
	store.schedule.BusinessHours = nil
	store.schedule.BusinessHourPeriods = []BusinessHourPeriod{
		{DayOfWeek: 1, StartLocalTime: "09:30:00", EndLocalTime: "12:00:00"},
		{DayOfWeek: 1, StartLocalTime: "13:00:00", EndLocalTime: "19:00:00"},
	}
	provider := &fakeProvider{
		availabilitySlots: []pos.TimeSlot{
			{
				StartTime: time.Date(2026, 6, 15, 11, 30, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 12, 15, 0, 0, loc).UTC(),
				StaffID:   "square_staff_1",
			},
			{
				StartTime: time.Date(2026, 6, 15, 13, 15, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 14, 0, 0, 0, loc).UTC(),
				StaffID:   "square_staff_1",
			},
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		ServiceID:     "service_1",
		StaffID:       "staff_1",
		PreferredDate: "2026-06-15",
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("AvailableSlots returned error: %v", err)
	}
	if len(result.Slots) != 1 {
		t.Fatalf("slots = %#v, want only the slot fully inside one period", result.Slots)
	}
	if !result.Slots[0].StartTime.Equal(time.Date(2026, 6, 15, 13, 15, 0, 0, loc).UTC()) {
		t.Fatalf("slot start = %s, want 13:15 local", result.Slots[0].StartTime)
	}
}

func TestAvailableSlotsWithoutStaffSkipsUnknownOrBlockedStaff(t *testing.T) {
	store := newFakeStore()
	store.staffRefs = append(store.staffRefs, StaffRef{
		ID:          "staff_2",
		POSProvider: pos.ProviderSquare,
		POSStaffID:  "square_staff_2",
		Name:        "An Nguyen",
	})
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	provider := &fakeProvider{
		availabilitySlots: []pos.TimeSlot{
			{
				StartTime: time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 10, 45, 0, 0, loc).UTC(),
				StaffID:   "square_staff_2",
			},
			{
				StartTime: time.Date(2026, 6, 15, 11, 0, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 11, 45, 0, 0, loc).UTC(),
				StaffID:   "square_staff_blocked",
			},
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		ServiceID:     "service_1",
		PreferredDate: "2026-06-15",
	})
	if err != nil {
		t.Fatalf("AvailableSlots returned error: %v", err)
	}
	if provider.lastAvailabilityInput.StaffID != "" {
		t.Fatalf("provider staff filter = %q, want no staff filter", provider.lastAvailabilityInput.StaffID)
	}
	if result.StaffSelectionMode != StaffSelectionAnyone {
		t.Fatalf("staff selection mode = %s, want anyone", result.StaffSelectionMode)
	}
	if len(result.Slots) != 1 || result.Slots[0].StaffID != "staff_2" {
		t.Fatalf("slots = %#v, want only AI-bookable mapped staff", result.Slots)
	}
	if result.Slots[0].StaffSelectionMode != StaffSelectionAnyone {
		t.Fatalf("slot staff selection mode = %s, want anyone", result.Slots[0].StaffSelectionMode)
	}
}

func TestAvailableSlotsSupportsMultipleSegments(t *testing.T) {
	store := newFakeStore()
	store.services = append(store.services, ServiceRef{
		ID:                "service_2",
		POSProvider:       pos.ProviderSquare,
		POSServiceID:      "square_service_2",
		POSServiceVersion: 456,
		Name:              "Gel Removal",
		DurationMinutes:   30,
		PriceFrom:         15,
	})
	store.staffRefs = append(store.staffRefs, StaffRef{
		ID:          "staff_2",
		POSProvider: pos.ProviderSquare,
		POSStaffID:  "square_staff_2",
		Name:        "An Nguyen",
	})
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	provider := &fakeProvider{
		availabilitySlots: []pos.TimeSlot{
			{
				StartTime: time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC(),
				EndTime:   time.Date(2026, 6, 15, 11, 15, 0, 0, loc).UTC(),
				StaffID:   "square_staff_1",
				Segments: []pos.TimeSlotSegment{
					{ServiceID: "square_service_1", StaffID: "square_staff_1", DurationMinutes: 45},
					{ServiceID: "square_service_2", StaffID: "square_staff_2", DurationMinutes: 30},
				},
			},
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		PreferredDate: "2026-06-15",
		Segments: []BookingSegmentRequest{
			{ServiceID: "service_1"},
			{ServiceID: "service_2"},
		},
	})
	if err != nil {
		t.Fatalf("AvailableSlots returned error: %v", err)
	}
	if len(provider.lastAvailabilityInput.Segments) != 2 {
		t.Fatalf("provider segments = %#v, want two", provider.lastAvailabilityInput.Segments)
	}
	if provider.lastAvailabilityInput.Segments[0].ServiceID != "square_service_1" || provider.lastAvailabilityInput.Segments[1].ServiceID != "square_service_2" {
		t.Fatalf("provider segments = %#v, want ordered POS services", provider.lastAvailabilityInput.Segments)
	}
	if len(result.Slots) != 1 {
		t.Fatalf("slots = %#v, want one", result.Slots)
	}
	slot := result.Slots[0]
	if len(slot.Segments) != 2 {
		t.Fatalf("slot segments = %#v, want two", slot.Segments)
	}
	if slot.Segments[0].ServiceID != "service_1" || slot.Segments[0].StaffID != "staff_1" {
		t.Fatalf("first slot segment = %#v, want service_1/staff_1", slot.Segments[0])
	}
	if slot.Segments[1].ServiceID != "service_2" || slot.Segments[1].StaffID != "staff_2" {
		t.Fatalf("second slot segment = %#v, want service_2/staff_2", slot.Segments[1])
	}
	if slot.EndTime.Sub(slot.StartTime) != 75*time.Minute {
		t.Fatalf("slot duration = %s, want 75m", slot.EndTime.Sub(slot.StartTime))
	}
}

func TestAvailableSlotsReturnsEmptyWhenBusinessHoursMissing(t *testing.T) {
	store := newFakeStore()
	store.schedule.BusinessHours = nil
	provider := &fakeProvider{
		availabilitySlots: []pos.TimeSlot{
			{
				StartTime: testStartTime(),
				EndTime:   testStartTime().Add(45 * time.Minute),
				StaffID:   "square_staff_1",
			},
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		ServiceID:     "service_1",
		StaffID:       "staff_1",
		PreferredDate: "2026-06-10",
	})
	if err != nil {
		t.Fatalf("AvailableSlots returned error: %v", err)
	}
	if len(result.Slots) != 0 {
		t.Fatalf("slots = %#v, want none without configured business hours", result.Slots)
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
	service                  ServiceRef
	services                 []ServiceRef
	staff                    StaffRef
	staffRefs                []StaffRef
	customer                 CustomerRef
	schedule                 Schedule
	appointment              AppointmentActionRef
	pending                  *PendingBookingRecord
	confirmed                *ConfirmedBookingRecord
	fallback                 *FallbackBookingRecord
	pendingAction            *PendingAppointmentActionRecord
	rescheduled              *RescheduledAppointmentRecord
	cancelled                *CancelledAppointmentRecord
	actionFallback           *AppointmentActionFallbackRecord
	appointments             []Appointment
	bookingAttempts          []BookingAttempt
	calendarAppointments     []Appointment
	calendarPendingRequests  []BookingAttempt
	calendarStartTime        time.Time
	calendarEndTime          time.Time
	calendarImports          []CalendarAppointmentImport
	calendarSyncSummary      CalendarSyncSummary
	calendarSyncLogStatus    string
	calendarSyncLogMessage   string
	calendarProviderStatus   string
	calendarProviderMessage  string
	posErrorOperation        string
	listAppointmentLimit     int
	listAppointmentOffset    int
	listBookingAttemptStatus string
	listBookingAttemptLimit  int
	listBookingAttemptOffset int
	rescheduleLookup         RescheduleLookupRequest
	linkedCustomer           *CustomerRef
	linkCustomerErr          error
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
		customer: CustomerRef{
			ID:    "customer_1",
			Name:  "Linh Tran",
			Phone: "3125550101",
			Email: "linh@example.com",
		},
		schedule: Schedule{
			Timezone: "America/Chicago",
			BusinessHours: []BusinessHour{
				{DayOfWeek: 0, IsClosed: true},
				{DayOfWeek: 1, OpenTime: "09:30:00", CloseTime: "19:00:00"},
				{DayOfWeek: 2, OpenTime: "09:30:00", CloseTime: "19:00:00"},
				{DayOfWeek: 3, OpenTime: "09:30:00", CloseTime: "19:00:00"},
				{DayOfWeek: 4, OpenTime: "09:30:00", CloseTime: "19:00:00"},
				{DayOfWeek: 5, OpenTime: "09:30:00", CloseTime: "19:00:00"},
				{DayOfWeek: 6, OpenTime: "09:30:00", CloseTime: "19:00:00"},
			},
		},
	}
	store.services = []ServiceRef{store.service}
	store.staffRefs = []StaffRef{store.staff}
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
		StaffSelectionMode:    StaffSelectionSpecific,
		StartTime:             testStartTime(),
		EndTime:               testStartTime().Add(45 * time.Minute),
		Notes:                 "First visit",
	}
	store.appointment.Segments = singleBookingSegment(store.service, store.staff, StaffSelectionSpecific)
	return store
}

func (f *fakeStore) addSecondAppointmentSegment() {
	secondService := ServiceRef{
		ID:                "service_2",
		POSProvider:       pos.ProviderSquare,
		POSServiceID:      "square_service_2",
		POSServiceVersion: 456,
		Name:              "Gel Removal",
		DurationMinutes:   30,
		PriceFrom:         15,
	}
	secondStaff := StaffRef{
		ID:          "staff_2",
		POSProvider: pos.ProviderSquare,
		POSStaffID:  "square_staff_2",
		Name:        "An Nguyen",
	}
	f.services = append(f.services, secondService)
	f.staffRefs = append(f.staffRefs, secondStaff)
	f.appointment.EndTime = f.appointment.StartTime.Add(75 * time.Minute)
	f.appointment.Segments = []BookingSegmentRecord{
		{Service: f.service, Staff: f.staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1},
		{Service: secondService, Staff: secondStaff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 2},
	}
}

func (f *fakeStore) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return pos.ErrNotFound
	}
	return nil
}

func (f *fakeStore) GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return "", pos.ErrNotFound
	}
	return pos.ProviderSquare, nil
}

func (f *fakeStore) GetBookableService(ctx context.Context, salonID string, serviceID string) (*ServiceRef, error) {
	for _, service := range f.services {
		if serviceID == service.ID {
			item := service
			return &item, nil
		}
	}
	return nil, pos.ErrNotFound
}

func (f *fakeStore) GetBookableStaff(ctx context.Context, salonID string, staffID string) (*StaffRef, error) {
	for _, staff := range f.staffRefs {
		if staffID == staff.ID {
			item := staff
			return &item, nil
		}
	}
	return nil, pos.ErrNotFound
}

func (f *fakeStore) ListBookableStaffRefs(ctx context.Context, salonID string) ([]StaffRef, error) {
	return f.staffRefs, nil
}

func (f *fakeStore) ResolveBookingCustomer(ctx context.Context, salonID string, provider string, name string, phone string, email string) (*CustomerRef, error) {
	if salonID != "salon_1" || provider != pos.ProviderSquare {
		return nil, pos.ErrNotFound
	}
	item := f.customer
	if item.Name == "" {
		item.Name = name
	}
	if item.Phone == "" {
		item.Phone = phone
	}
	if item.Email == "" {
		item.Email = email
	}
	return &item, nil
}

func (f *fakeStore) LinkBookingCustomer(ctx context.Context, salonID string, provider string, customerID string, customer pos.Customer) (*CustomerRef, error) {
	if f.linkCustomerErr != nil {
		return nil, f.linkCustomerErr
	}
	if salonID != "salon_1" || provider != pos.ProviderSquare || customerID != f.customer.ID {
		return nil, pos.ErrNotFound
	}
	item := f.customer
	item.POSProvider = provider
	item.POSCustomerID = customer.POSCustomerID
	if item.Phone == "" {
		item.Phone = customer.Phone
	}
	if item.Email == "" {
		item.Email = customer.Email
	}
	f.customer = item
	f.linkedCustomer = &item
	return &item, nil
}

func (f *fakeStore) GetSchedule(ctx context.Context, salonID string) (*Schedule, error) {
	return &f.schedule, nil
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
		StaffSelectionMode: record.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
	}, nil
}

func (f *fakeStore) SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		StaffSelectionMode: record.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		Appointment: &Appointment{
			ID:                    "appointment_1",
			POSAppointmentID:      record.POSBookingID,
			POSAppointmentVersion: record.POSBookingVersion,
			Status:                StatusConfirmed,
			StaffSelectionMode:    record.StaffSelectionMode,
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
		StaffSelectionMode: record.StaffSelectionMode,
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

func (f *fakeStore) ListRescheduleCandidates(ctx context.Context, salonID string, ownerUserID string, req RescheduleLookupRequest) ([]AppointmentActionRef, error) {
	f.rescheduleLookup = req
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return nil, pos.ErrNotFound
	}
	if last10Digits(req.CustomerPhone) != last10Digits(f.appointment.CustomerPhone) {
		return nil, nil
	}
	return []AppointmentActionRef{f.appointment}, nil
}

func last10Digits(value string) string {
	digits := ""
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits += string(r)
		}
	}
	if len(digits) <= 10 {
		return digits
	}
	return digits[len(digits)-10:]
}

func (f *fakeStore) CreatePendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingAttempt, error) {
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	primary := segments[0]
	record.Segments = segments
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
		ServiceID:          primary.Service.ID,
		StaffID:            primary.Staff.ID,
		StaffSelectionMode: primary.StaffSelectionMode,
		Segments:           bookingSegmentSnapshots(segments),
		RequestedStartTime: record.RequestedStartTime,
		RequestedEndTime:   record.RequestedEndTime,
	}, nil
}

func (f *fakeStore) SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error) {
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
		if record.Staff.ID != "" {
			segments = applyStaffToBookingSegments(segments, record.Staff)
		}
	}
	primary := segments[0]
	record.Segments = segments
	f.rescheduled = &record
	f.appointment.Status = StatusRescheduled
	f.appointment.StartTime = record.StartTime
	f.appointment.EndTime = record.EndTime
	f.appointment.Service = primary.Service
	f.appointment.Staff = primary.Staff
	f.appointment.StaffSelectionMode = primary.StaffSelectionMode
	f.appointment.Segments = segments
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
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	primary := segments[0]
	record.Segments = segments
	f.actionFallback = &record
	return &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Status:             StatusFallbackPending,
		POSProvider:        record.Provider,
		POSBookingID:       record.Appointment.POSAppointmentID,
		CustomerName:       record.Appointment.CustomerName,
		CustomerPhone:      record.Appointment.CustomerPhone,
		ServiceID:          primary.Service.ID,
		StaffID:            primary.Staff.ID,
		StaffSelectionMode: primary.StaffSelectionMode,
		Segments:           bookingSegmentSnapshots(segments),
		RequestedStartTime: record.RequestedStartTime,
		RequestedEndTime:   record.RequestedEndTime,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
	}, nil
}

func (f *fakeStore) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	return nil, pos.ErrNotFound
}

func (f *fakeStore) ListAppointments(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) ([]Appointment, error) {
	f.listAppointmentLimit = limit
	f.listAppointmentOffset = offset
	return f.appointments, nil
}

func (f *fakeStore) ListBookingAttempts(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]BookingAttempt, error) {
	f.listBookingAttemptStatus = status
	f.listBookingAttemptLimit = limit
	f.listBookingAttemptOffset = offset
	return f.bookingAttempts, nil
}

func (f *fakeStore) ListCalendarAppointments(ctx context.Context, salonID string, ownerUserID string, startTime time.Time, endTime time.Time) ([]Appointment, error) {
	f.calendarStartTime = startTime
	f.calendarEndTime = endTime
	return f.calendarAppointments, nil
}

func (f *fakeStore) ListCalendarPendingRequests(ctx context.Context, salonID string, ownerUserID string, startTime time.Time, endTime time.Time) ([]BookingAttempt, error) {
	f.calendarStartTime = startTime
	f.calendarEndTime = endTime
	return f.calendarPendingRequests, nil
}

func (f *fakeStore) UpsertCalendarAppointments(ctx context.Context, salonID string, provider string, items []CalendarAppointmentImport) (CalendarSyncSummary, error) {
	f.calendarImports = append([]CalendarAppointmentImport(nil), items...)
	return f.calendarSyncSummary, nil
}

func (f *fakeStore) CreateCalendarSyncLog(ctx context.Context, salonID string, provider string) (string, error) {
	return "sync_log_1", nil
}

func (f *fakeStore) CompleteCalendarSyncLog(ctx context.Context, id string, status string, message string) error {
	f.calendarSyncLogStatus = status
	f.calendarSyncLogMessage = message
	return nil
}

func (f *fakeStore) MarkCalendarSyncComplete(ctx context.Context, salonID string, provider string, status string, message string) error {
	f.calendarProviderStatus = status
	f.calendarProviderMessage = message
	return nil
}

func (f *fakeStore) LogPOSError(ctx context.Context, salonID string, provider string, operation string, err error) error {
	f.posErrorOperation = operation
	return nil
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
	listAppointmentsErr    error
	availabilityErr        error
	store                  *fakeStore
	availabilitySlots      []pos.TimeSlot
	listAppointments       []pos.ListedAppointment
	lastAvailabilityInput  pos.AvailabilityInput
	lastCreateInput        pos.CreateAppointmentInput
	lastRescheduleInput    pos.RescheduleInput
	lastCancelInput        pos.CancelInput
	lastListInput          pos.AppointmentListInput
	searchSawPending       bool
	searchCustomerCalls    int
	createCustomerCalls    int
	availabilityCalls      int
	createAppointmentCalls int
	rescheduleCalls        int
	cancelCalls            int
	listAppointmentCalls   int
	afterCreateAppointment func()
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
	f.searchCustomerCalls++
	if f.store != nil && f.store.pending != nil {
		f.searchSawPending = true
	}
	if f.searchCustomerErr != nil {
		return nil, f.searchCustomerErr
	}
	return f.customer, nil
}

func (f *fakeProvider) CreateCustomer(ctx context.Context, salonID string, input pos.CreateCustomerInput) (*pos.Customer, error) {
	f.createCustomerCalls++
	if f.createCustomerErr != nil {
		return nil, f.createCustomerErr
	}
	return &pos.Customer{POSCustomerID: "cust_created", Name: input.Name, Phone: input.Phone, Email: input.Email}, nil
}

func (f *fakeProvider) CheckAvailability(ctx context.Context, salonID string, input pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	f.availabilityCalls++
	f.lastAvailabilityInput = input
	if f.availabilityErr != nil {
		return nil, f.availabilityErr
	}
	return f.availabilitySlots, nil
}

func (f *fakeProvider) CreateAppointment(ctx context.Context, salonID string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	f.createAppointmentCalls++
	f.lastCreateInput = input
	if f.createBookingErr != nil {
		return nil, f.createBookingErr
	}
	if f.afterCreateAppointment != nil {
		f.afterCreateAppointment()
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

func (f *fakeProvider) ListAppointments(ctx context.Context, salonID string, input pos.AppointmentListInput) (*pos.AppointmentListResult, error) {
	f.listAppointmentCalls++
	f.lastListInput = input
	if f.listAppointmentsErr != nil {
		return nil, f.listAppointmentsErr
	}
	return &pos.AppointmentListResult{Appointments: f.listAppointments}, nil
}

func (f *fakeProvider) Sync(ctx context.Context, salonID string) error {
	return nil
}
