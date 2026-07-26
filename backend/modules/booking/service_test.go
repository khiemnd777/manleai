package booking

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
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

	_, err := service.Attempts(context.Background(), "salon_1", "owner_1", "not-a-status", 10, 0)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestResolveReconciliationRequiresStableActionKeyAndServerFingerprint(t *testing.T) {
	store := newFakeStore()
	store.reconciliationTasks = []ReconciliationTask{{
		ID:               "task_1",
		SalonID:          "salon_1",
		BookingAttemptID: "attempt_1",
		Status:           "open",
	}}
	service := NewService(store, nil)

	if _, err := service.ResolveReconciliation(context.Background(), "salon_1", "owner_1", "attempt_1", ResolveReconciliationRequest{
		Action: ReconciliationActionNotCreated,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing action key error = %v, want validation", err)
	}
	task, err := service.ResolveReconciliation(context.Background(), "salon_1", "owner_1", "attempt_1", ResolveReconciliationRequest{
		ActionKey:          " owner-resolution-1 ",
		Action:             " NOT_CREATED ",
		Note:               " verified in provider ",
		PayloadFingerprint: "caller-controlled-value",
	})
	if err != nil {
		t.Fatalf("ResolveReconciliation returned error: %v", err)
	}
	if task.Status != "resolved" {
		t.Fatalf("task status = %s, want resolved", task.Status)
	}
	got := store.reconciliationRequest
	if got.ActionKey != "owner-resolution-1" || got.Action != ReconciliationActionNotCreated || got.Note != "verified in provider" {
		t.Fatalf("normalized reconciliation request = %#v", got)
	}
	if len(got.PayloadFingerprint) != 64 || got.PayloadFingerprint == "caller-controlled-value" {
		t.Fatalf("payload fingerprint = %q, want server-computed SHA-256", got.PayloadFingerprint)
	}
}

func TestReconciliationCandidatesReturnsOnlyStoreVerifiedMatches(t *testing.T) {
	store := newFakeStore()
	store.reconciliationCandidates = []ReconciliationCandidate{{
		AppointmentID:              "appointment_1",
		Provider:                   pos.ProviderSquare,
		ProviderAppointmentID:      "square_booking_1",
		ProviderAppointmentVersion: 7,
		ProviderStatus:             string(pos.AppointmentStatusAccepted),
	}}
	service := NewService(store, nil)

	if _, err := service.ReconciliationCandidates(context.Background(), "salon_1", "owner_1", "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank attempt id error = %v, want validation", err)
	}
	response, err := service.ReconciliationCandidates(context.Background(), "salon_1", "owner_1", " attempt_1 ")
	if err != nil {
		t.Fatalf("ReconciliationCandidates returned error: %v", err)
	}
	if len(response.Candidates) != 1 || response.Candidates[0].ProviderAppointmentID != "square_booking_1" || response.Candidates[0].ProviderAppointmentVersion != 7 {
		t.Fatalf("candidates = %#v, want verified provider booking", response.Candidates)
	}
	if store.reconciliationCandidateAttemptID != "attempt_1" {
		t.Fatalf("store attempt id = %q, want trimmed attempt_1", store.reconciliationCandidateAttemptID)
	}
}

func TestLatestTestBookingExpiresOperationLeaseBeforeRead(t *testing.T) {
	store := newFakeStore()
	store.latestTest = &TestBookingRecord{BookingAttemptID: "attempt_test_1", Status: StatusFallbackPending}
	service := NewService(store, nil)

	latest, err := service.LatestTestBooking(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("LatestTestBooking returned error: %v", err)
	}
	if latest == nil || latest.BookingAttemptID != "attempt_test_1" {
		t.Fatalf("latest = %#v, want persisted test attempt", latest)
	}
	if store.expireLeaseCalls != 1 {
		t.Fatalf("expire lease calls = %d, want one before latest test read", store.expireLeaseCalls)
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
		RetryPolicy:        RetryPolicySafe,
		RequestedStartTime: testStartTime().Add(3 * time.Hour),
		RequestedEndTime:   testStartTime().Add(4 * time.Hour),
	}, {
		ID:                 "attempt_pos_pending",
		Status:             StatusPOSPending,
		RequestedStartTime: testStartTime().Add(5 * time.Hour),
		RequestedEndTime:   testStartTime().Add(6 * time.Hour),
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
	if len(res.Appointments) != 2 || len(res.PendingRequests) != 2 {
		t.Fatalf("calendar counts appointments=%d pending=%d, want 2/2", len(res.Appointments), len(res.PendingRequests))
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
	if res.PendingRequests[1].SyncWarning == "" || res.PendingRequests[1].CanRetry {
		t.Fatalf("pos pending warning/retry = %#v, want warning and no retry", res.PendingRequests[1])
	}
	if res.Warnings.SyncFailed != 1 || res.Warnings.FallbackPending != 1 || res.Warnings.PendingPOSSync != 1 || res.Warnings.TotalWarnings != 3 {
		t.Fatalf("warnings = %#v, want one sync failed, one fallback, one pending POS sync", res.Warnings)
	}
}

func TestCalendarEventsDefaultsLimitAndAssignsReplayCursor(t *testing.T) {
	store := newFakeStore()
	createdAt := testStartTime()
	store.calendarEvents = []CalendarEvent{{
		ID:            "notification_1",
		SalonID:       "salon_1",
		Type:          NotificationTypeBookingConfirmed,
		Title:         "New POS-confirmed appointment",
		BookingStatus: StatusConfirmed,
		CustomerName:  "Kevin",
		StartTime:     createdAt.Add(time.Hour),
		EndTime:       createdAt.Add(2 * time.Hour),
		CreatedAt:     createdAt,
	}}
	service := NewService(store, nil)
	cursor := CalendarEventCursor{CreatedAt: createdAt.Add(-time.Second), ID: "notification_0"}

	events, err := service.CalendarEvents(context.Background(), "salon_1", "owner_1", cursor, 0)
	if err != nil {
		t.Fatalf("CalendarEvents returned error: %v", err)
	}
	if store.calendarEventLimit != 20 {
		t.Fatalf("event limit = %d, want default 20", store.calendarEventLimit)
	}
	if !store.calendarEventCursor.CreatedAt.Equal(cursor.CreatedAt) || store.calendarEventCursor.ID != cursor.ID {
		t.Fatalf("event cursor = %#v, want %#v", store.calendarEventCursor, cursor)
	}
	if len(events) != 1 || events[0].Cursor == "" {
		t.Fatalf("events = %#v, want event with replay cursor", events)
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
	if store.activeProviderFenceCalls != 1 {
		t.Fatalf("active provider fence reads = %d, want one capture for the whole sync", store.activeProviderFenceCalls)
	}
	if !sameProviderFence(provider.lastListInput.ProviderFence, store.activeProviderFence) || !sameProviderFence(store.calendarUpsertFence, store.activeProviderFence) {
		t.Fatalf("calendar sync fences = list %#v / upsert %#v, want captured %#v", provider.lastListInput.ProviderFence, store.calendarUpsertFence, store.activeProviderFence)
	}
	if len(store.calendarImports) != 1 {
		t.Fatalf("imports = %d, want 1", len(store.calendarImports))
	}
	got := store.calendarImports[0]
	if got.POSAppointmentID != "booking_square_1" || got.Status != StatusConfirmed || got.Segments[0].POSServiceID != "square_service_1" {
		t.Fatalf("import = %#v, want provider-neutral Square booking", got)
	}
	if store.calendarSyncLogStatus != "succeeded" {
		t.Fatalf("sync log status = %s, want succeeded", store.calendarSyncLogStatus)
	}
	if res.Provider != pos.ProviderSquare || res.Summary.Imported != 1 {
		t.Fatalf("response = %#v, want square imported summary", res)
	}
}

func TestSyncCalendarRejectsRepeatedProviderCursorInsteadOfSilentlyTruncating(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{listAppointmentsCursor: "repeated-cursor"}
	service := NewService(store, []pos.POSProvider{provider})

	_, err := service.SyncCalendar(context.Background(), "salon_1", "owner_1", CalendarSyncRequest{
		StartTime: testStartTime().Add(-24 * time.Hour),
		EndTime:   testStartTime().Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrCalendarSyncCursorRepeated) {
		t.Fatalf("error = %v, want repeated cursor safety error", err)
	}
	if provider.listAppointmentCalls != 2 {
		t.Fatalf("list calls = %d, want two pages before repeated-cursor detection", provider.listAppointmentCalls)
	}
	if store.activeProviderFenceCalls != 1 || len(provider.listAppointmentInputs) != 2 {
		t.Fatalf("fence reads/list inputs = %d/%d, want one capture and two page inputs", store.activeProviderFenceCalls, len(provider.listAppointmentInputs))
	}
	for index, input := range provider.listAppointmentInputs {
		if !sameProviderFence(input.ProviderFence, store.activeProviderFence) {
			t.Fatalf("page %d fence = %#v, want captured %#v", index, input.ProviderFence, store.activeProviderFence)
		}
	}
	if store.calendarSyncLogStatus != "failed" {
		t.Fatalf("sync log status = %s, want failed", store.calendarSyncLogStatus)
	}
}

func TestCalendarImportPreservesKnownProviderStatusesAndPersistsUnknownFailClosed(t *testing.T) {
	statuses := map[string]string{
		"ACCEPTED":              StatusConfirmed,
		"PENDING":               StatusProviderPending,
		"CANCELLED_BY_CUSTOMER": StatusCancelled,
		"DECLINED":              StatusDeclined,
		"NO_SHOW":               StatusNoShow,
	}
	for providerStatus, expected := range statuses {
		t.Run(providerStatus, func(t *testing.T) {
			item, ok := calendarImportFromPOSAppointment(pos.ProviderSquare, pos.ListedAppointment{
				POSAppointmentID: "booking_1",
				Status:           providerStatus,
				StartTime:        testStartTime(),
				EndTime:          testStartTime().Add(45 * time.Minute),
			})
			if !ok || item.Status != expected {
				t.Fatalf("import = %#v ok=%t, want status %s", item, ok, expected)
			}
		})
	}
	item, ok := calendarImportFromPOSAppointment(pos.ProviderSquare, pos.ListedAppointment{
		POSAppointmentID: "booking_unknown",
		Status:           "mystery_status",
		StartTime:        testStartTime(),
		EndTime:          testStartTime().Add(45 * time.Minute),
	})
	if !ok || item.Status != StatusUnknown {
		t.Fatalf("unknown provider status import = %#v ok=%t, want persisted fail-closed unknown", item, ok)
	}
	outcome, retryPolicy, reconciliation := calendarImportAttemptPolicy(item.Status)
	if outcome != ProviderOutcomeUnknown || retryPolicy != RetryPolicyBlocked || reconciliation != ReconciliationRequired {
		t.Fatalf("unknown status policy = %s/%s/%s, want unknown/blocked/required", outcome, retryPolicy, reconciliation)
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
		ProviderFence:     store.service.ProviderFence,
	})
	store.staffRefs = append(store.staffRefs, StaffRef{
		ID:            "staff_2",
		POSProvider:   pos.ProviderSquare,
		POSStaffID:    "square_staff_2",
		Name:          "An Nguyen",
		ProviderFence: store.staff.ProviderFence,
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

func TestCreateDoesNotConfirmAcceptedProviderResponseWithoutBookingID(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentVersion: 7,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                string(pos.AppointmentStatusAccepted),
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusFallbackPending || attempt.ProviderOutcome != ProviderOutcomeUnknown {
		t.Fatalf("attempt = %#v, want fallback_pending/unknown", attempt)
	}
	if attempt.POSBookingID != "" || attempt.Appointment != nil || store.confirmed != nil {
		t.Fatalf("accepted-looking response without booking ID must remain unconfirmed: %#v", attempt)
	}
	if store.fallback == nil || store.fallback.ErrorMessage != pos.SafeErrorMessage(pos.ErrorBookingFailed) {
		t.Fatalf("fallback = %#v, want missing booking ID evidence", store.fallback)
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
	if store.fallback == nil || store.fallback.ErrorMessage != pos.SafeErrorMessage(pos.ErrorBookingFailed) {
		t.Fatalf("fallback = %#v, want missing version", store.fallback)
	}
}

func TestCreateStoresFallbackPendingWhenPOSBookingFails(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer:         &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		createBookingErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square booking conflict")),
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
	if attempt.ProviderOutcome != ProviderOutcomeFailed || attempt.RetryPolicy != RetryPolicySafe || !attempt.CanRetry {
		t.Fatalf("fallback policy = outcome=%s retry=%s can_retry=%t, want failed/safe/true", attempt.ProviderOutcome, attempt.RetryPolicy, attempt.CanRetry)
	}

	store.activeProvider = "provider-no-longer-ready"
	replayReq := validCreateRequest()
	replayReq.AvailabilityQuoteID = "00000000-0000-4000-8000-000000000040"
	replayReq.SlotFingerprint = strings.Repeat("b", 64)
	replayed, err := service.Create(context.Background(), "salon_1", "owner_1", replayReq)
	if err != nil {
		t.Fatalf("fallback replay returned error after readiness changed: %v", err)
	}
	if replayed.Status != StatusFallbackPending || provider.createAppointmentCalls != 1 {
		t.Fatalf("fallback replay = %#v provider calls=%d, want prior fallback and one POS call", replayed, provider.createAppointmentCalls)
	}
}

func TestBookingWritesRequireExplicitOperationKey(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		store := newFakeStore()
		provider := &fakeProvider{}
		service := NewService(store, []pos.POSProvider{provider})
		req := validCreateRequest()
		req.OperationKey = ""

		attempt, err := service.Create(context.Background(), "salon_1", "owner_1", req)
		if !errors.Is(err, ErrValidation) || attempt != nil {
			t.Fatalf("Create result attempt=%#v err=%v, want ErrValidation", attempt, err)
		}
		if provider.createAppointmentCalls != 0 {
			t.Fatalf("Create provider calls = %d, want 0", provider.createAppointmentCalls)
		}
	})

	t.Run("reschedule", func(t *testing.T) {
		store := newFakeStore()
		provider := &fakeProvider{}
		service := NewService(store, []pos.POSProvider{provider})

		appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
			AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
			SlotFingerprint:     strings.Repeat("a", 64),
			StartTime:           testStartTime().Add(24 * time.Hour),
		})
		if !errors.Is(err, ErrValidation) || appointment != nil || fallback != nil {
			t.Fatalf("Reschedule result appointment=%#v fallback=%#v err=%v, want ErrValidation", appointment, fallback, err)
		}
		if provider.rescheduleCalls != 0 {
			t.Fatalf("Reschedule provider calls = %d, want 0", provider.rescheduleCalls)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store := newFakeStore()
		provider := &fakeProvider{}
		service := NewService(store, []pos.POSProvider{provider})

		appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{Reason: "Customer changed plans"})
		if !errors.Is(err, ErrValidation) || appointment != nil || fallback != nil {
			t.Fatalf("Cancel result appointment=%#v fallback=%#v err=%v, want ErrValidation", appointment, fallback, err)
		}
		if provider.cancelCalls != 0 {
			t.Fatalf("Cancel provider calls = %d, want 0", provider.cancelCalls)
		}
	})
}

func TestCreateFingerprintAllowsNewSnapshotGenerationButBindsDurationAndLocation(t *testing.T) {
	store := newFakeStore()
	req := normalizeRequest(validCreateRequest())
	baseSegment := resolvedBookingSegment{
		Service:            store.service,
		Staff:              store.staff,
		StaffSelectionMode: StaffSelectionSpecific,
		SortOrder:          1,
	}
	base := createRequestFingerprint(pos.ProviderSquare, req, []resolvedBookingSegment{baseSegment})

	newGeneration := baseSegment
	newGeneration.Service.ProviderFence.SnapshotGeneration = 2
	newGeneration.Staff.ProviderFence.SnapshotGeneration = 2
	if got := createRequestFingerprint(pos.ProviderSquare, req, []resolvedBookingSegment{newGeneration}); got != base {
		t.Fatalf("snapshot generation changed logical create fingerprint: got %s want %s", got, base)
	}

	changedDuration := newGeneration
	changedDuration.Service.DurationMinutes++
	if got := createRequestFingerprint(pos.ProviderSquare, req, []resolvedBookingSegment{changedDuration}); got == base {
		t.Fatalf("duration change did not change logical create fingerprint: %s", got)
	}

	changedLocation := newGeneration
	changedLocation.Service.ProviderFence.LocationID = "loc_2"
	changedLocation.Staff.ProviderFence.LocationID = "loc_2"
	if got := createRequestFingerprint(pos.ProviderSquare, req, []resolvedBookingSegment{changedLocation}); got == base {
		t.Fatalf("location change did not change logical create fingerprint: %s", got)
	}
}

func TestAppointmentActionFingerprintBindsRawOverridesAndDuration(t *testing.T) {
	store := newFakeStore()
	appointment := store.appointment
	segments := appointmentActionSegments(appointment)
	startTime := testStartTime().Add(24 * time.Hour)
	fence := store.service.ProviderFence
	base := appointmentActionFingerprint(
		BookingActionReschedule,
		appointment,
		startTime,
		segments,
		"Keep the same shape",
		fence,
		"staff_1",
		"Keep the same shape",
	)

	newGeneration := fence
	newGeneration.SnapshotGeneration = 2
	if got := appointmentActionFingerprint(BookingActionReschedule, appointment, startTime, segments, "Keep the same shape", newGeneration, "staff_1", "Keep the same shape"); got != base {
		t.Fatalf("snapshot generation changed logical action fingerprint: got %s want %s", got, base)
	}

	changedDuration := append([]BookingSegmentRecord(nil), segments...)
	changedDuration[0].Service.DurationMinutes++
	if got := appointmentActionFingerprint(BookingActionReschedule, appointment, startTime, changedDuration, "Keep the same shape", newGeneration, "staff_1", "Keep the same shape"); got == base {
		t.Fatalf("duration change did not change logical action fingerprint: %s", got)
	}

	if got := appointmentActionFingerprint(BookingActionReschedule, appointment, startTime, segments, "Keep the same shape", newGeneration, "staff_1", ""); got == base {
		t.Fatalf("explicit notes-to-omitted change did not change logical action fingerprint: %s", got)
	}
	if got := appointmentActionFingerprint(BookingActionReschedule, appointment, startTime, segments, "Keep the same shape", newGeneration, "", "Keep the same shape"); got == base {
		t.Fatalf("explicit staff-to-omitted change did not change logical action fingerprint: %s", got)
	}
}

func TestSquareTestCreateSafeRetryReachesProviderExactlyOnce(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer:         &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		createBookingErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square booking conflict")),
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.Source = SourceSquareTestBooking
	req.OperationKey = "square-test-create-initial"

	first, err := service.Create(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("initial Create returned error: %v", err)
	}
	if first.Status != StatusFallbackPending || first.RetryPolicy != RetryPolicySafe || !first.CanRetry {
		t.Fatalf("initial attempt = %#v, want safe fallback", first)
	}
	if provider.createAppointmentCalls != 1 {
		t.Fatalf("initial provider calls = %d, want 1", provider.createAppointmentCalls)
	}

	provider.createBookingErr = nil
	provider.appointment = &pos.Appointment{
		POSAppointmentID:      "booking_retry_1",
		POSAppointmentVersion: 8,
		StartTime:             testStartTime(),
		EndTime:               testStartTime().Add(45 * time.Minute),
		Status:                StatusConfirmed,
	}
	store.setSnapshotGeneration(2)
	retry := req
	retry.OperationKey = "square-test-create-retry"
	retry.RetryOfAttemptID = first.ID
	retry.AvailabilityQuoteID = "00000000-0000-4000-8000-000000000040"
	retry.SlotFingerprint = strings.Repeat("b", 64)
	confirmed, err := service.Create(context.Background(), "salon_1", "owner_1", retry)
	if err != nil {
		t.Fatalf("retry Create returned error: %v", err)
	}
	if confirmed.Status != StatusConfirmed || confirmed.POSBookingID != "booking_retry_1" {
		t.Fatalf("retry attempt = %#v, want confirmed provider booking", confirmed)
	}
	if provider.createAppointmentCalls != 2 {
		t.Fatalf("provider calls after retry = %d, want exactly one additional call", provider.createAppointmentCalls)
	}
	if store.pending == nil || store.pending.RetryOfAttemptID != first.ID {
		t.Fatalf("retry pending record = %#v, want lineage to %s", store.pending, first.ID)
	}

	replayed, err := service.Create(context.Background(), "salon_1", "owner_1", retry)
	if err != nil {
		t.Fatalf("replayed retry Create returned error: %v", err)
	}
	if replayed.Status != StatusConfirmed || provider.createAppointmentCalls != 2 {
		t.Fatalf("replayed retry = %#v provider calls=%d, want prior confirmation and no duplicate provider call", replayed, provider.createAppointmentCalls)
	}
}

func TestCreateBlocksRetryAndRequiresReconciliationWhenPOSResultIsUnknown(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer:         &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		createBookingErr: context.DeadlineExceeded,
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.OperationKey = "voice-call-1-booking"

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusFallbackPending || attempt.ProviderOutcome != ProviderOutcomeUnknown {
		t.Fatalf("attempt = %#v, want fallback_pending/unknown", attempt)
	}
	if attempt.RetryPolicy != RetryPolicyBlocked || attempt.Reconciliation != ReconciliationRequired || attempt.CanRetry {
		t.Fatalf("unknown result policy = retry=%s reconciliation=%s can_retry=%t, want blocked/required/false", attempt.RetryPolicy, attempt.Reconciliation, attempt.CanRetry)
	}
}

func TestCreateDoesNotConfirmProviderPendingAppointment(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID:      "booking_pending_1",
			POSAppointmentVersion: 3,
			StartTime:             testStartTime(),
			EndTime:               testStartTime().Add(45 * time.Minute),
			Status:                "PENDING",
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.Status != StatusProviderPending || attempt.POSBookingID != "booking_pending_1" {
		t.Fatalf("attempt = %#v, want provider_pending with provider booking id", attempt)
	}
	if attempt.ProviderOutcome != ProviderOutcomeSucceeded || attempt.RetryPolicy != RetryPolicyBlocked || attempt.Reconciliation != ReconciliationRequired {
		t.Fatalf("provider pending policy = %#v, want succeeded/blocked/required", attempt)
	}
	if store.confirmed != nil {
		t.Fatal("provider PENDING must not persist a confirmed appointment")
	}
}

func TestProviderPendingNotificationDoesNotDescribeKnownPendingStatusAsUnknown(t *testing.T) {
	record := FallbackBookingRecord{
		Status:         StatusProviderPending,
		CustomerName:   "Linh Tran",
		Service:        ServiceRef{Name: "Classic Manicure"},
		Staff:          StaffRef{Name: "Mai Nguyen"},
		StartTime:      testStartTime(),
		ErrorCode:      pos.ErrorBookingFailed,
		ErrorMessage:   "provider returned pending",
		Reconciliation: ReconciliationRequired,
	}
	message := fallbackNotificationMessage(record)
	if !strings.Contains(message, "has not accepted") || strings.Contains(message, "result is unknown") {
		t.Fatalf("pending notification = %q, want explicit not-accepted wording without unknown-result claim", message)
	}
}

func TestCreateTreatsUntypedPostDispatchErrorAsUnknown(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer:         &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		createBookingErr: io.ErrUnexpectedEOF,
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.OperationKey = "voice-call-truncated-provider-response"

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if attempt.ProviderOutcome != ProviderOutcomeUnknown || attempt.RetryPolicy != RetryPolicyBlocked || attempt.Reconciliation != ReconciliationRequired || attempt.CanRetry {
		t.Fatalf("attempt = %#v, want unknown provider result with retry blocked", attempt)
	}
}

func TestCreateReusesDurableOperationResultWithoutSecondPOSCall(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID: "booking_1", POSAppointmentVersion: 7,
			StartTime: testStartTime(), EndTime: testStartTime().Add(45 * time.Minute), Status: StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.OperationKey = "voice-call-2-booking"

	first, err := service.Create(context.Background(), "salon_1", "owner_1", req)
	if err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	store.setSnapshotGeneration(2)
	store.activeProvider = "provider-no-longer-ready"
	replayReq := req
	replayReq.AvailabilityQuoteID = "00000000-0000-4000-8000-000000000040"
	replayReq.SlotFingerprint = strings.Repeat("b", 64)
	second, err := service.Create(context.Background(), "salon_1", "owner_1", replayReq)
	if err != nil {
		t.Fatalf("second Create returned error: %v", err)
	}
	if first.Status != StatusConfirmed || second.Status != StatusConfirmed || second.Appointment == nil {
		t.Fatalf("durable results first=%#v second=%#v, want confirmed result reused", first, second)
	}
	if provider.createAppointmentCalls != 1 {
		t.Fatalf("POS create calls = %d, want 1", provider.createAppointmentCalls)
	}

	ownerReplay, found, err := service.ReplayCreate(context.Background(), "salon_1", "different_owner", replayReq)
	if err != nil || found || ownerReplay != nil {
		t.Fatalf("cross-owner replay = attempt %#v found=%t err=%v, want scoped not-found", ownerReplay, found, err)
	}
	if provider.createAppointmentCalls != 1 {
		t.Fatalf("owner-scoped replay consulted provider; calls = %d, want 1", provider.createAppointmentCalls)
	}
}

func TestCreateDeduplicatesSameIntentAcrossDifferentOperationKeys(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID: "booking_1", POSAppointmentVersion: 7,
			StartTime: testStartTime(), EndTime: testStartTime().Add(45 * time.Minute), Status: string(pos.AppointmentStatusAccepted),
		},
	}
	service := NewService(store, []pos.POSProvider{provider})
	firstRequest := validCreateRequest()
	firstRequest.OperationKey = "intent-key-1"
	secondRequest := firstRequest
	secondRequest.OperationKey = "intent-key-2"

	first, err := service.Create(context.Background(), "salon_1", "owner_1", firstRequest)
	if err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	second, err := service.Create(context.Background(), "salon_1", "owner_1", secondRequest)
	if err != nil {
		t.Fatalf("second Create returned error: %v", err)
	}
	if first.ID != second.ID || provider.createAppointmentCalls != 1 {
		t.Fatalf("attempts = %s/%s POS calls=%d, want one durable intent", first.ID, second.ID, provider.createAppointmentCalls)
	}
}

func TestCreateRejectsOperationKeyReuseWithDifferentPayload(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID: "booking_1", POSAppointmentVersion: 7,
			StartTime: testStartTime(), EndTime: testStartTime().Add(45 * time.Minute), Status: StatusConfirmed,
		},
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.OperationKey = "voice-call-3-booking"
	if _, err := service.Create(context.Background(), "salon_1", "owner_1", req); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	store.activeProvider = "provider-no-longer-ready"
	req.Notes = "Different request payload"
	if _, err := service.Create(context.Background(), "salon_1", "owner_1", req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("second Create error = %v, want operation conflict", err)
	}
	if provider.createAppointmentCalls != 1 {
		t.Fatalf("POS create calls = %d, want 1", provider.createAppointmentCalls)
	}
}

func TestCreateConcurrentDuplicateClaimsOnlyOnePOSWriter(t *testing.T) {
	store := newFakeStore()
	started := make(chan struct{})
	release := make(chan struct{})
	provider := &fakeProvider{
		customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"},
		appointment: &pos.Appointment{
			POSAppointmentID: "booking_1", POSAppointmentVersion: 7,
			StartTime: testStartTime(), EndTime: testStartTime().Add(45 * time.Minute), Status: StatusConfirmed,
		},
		beforeCreateAppointment: func() {
			close(started)
			<-release
		},
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := validCreateRequest()
	req.OperationKey = "voice-call-4-booking"

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Create(context.Background(), "salon_1", "owner_1", req)
		firstDone <- err
	}()
	<-started
	store.activeProvider = "provider-no-longer-ready"
	duplicateReq := req
	duplicateReq.AvailabilityQuoteID = "00000000-0000-4000-8000-000000000040"
	duplicateReq.SlotFingerprint = strings.Repeat("b", 64)
	duplicate, err := service.Create(context.Background(), "salon_1", "owner_1", duplicateReq)
	if err != nil {
		t.Fatalf("duplicate Create returned error: %v", err)
	}
	if duplicate.Status != StatusPOSPending || duplicate.ProviderOutcome != ProviderOutcomeInFlight {
		t.Fatalf("duplicate = %#v, want existing in-flight attempt", duplicate)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	if provider.createAppointmentCalls != 1 {
		t.Fatalf("POS create calls = %d, want 1", provider.createAppointmentCalls)
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
			Status:                string(pos.AppointmentStatusAccepted),
		},
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := RescheduleRequest{
		OperationKey:        "test-reschedule-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		StartTime:           testStartTime().Add(24 * time.Hour),
		Notes:               "Keep the same shape",
	}

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", req)
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
	if !sameProviderFence(provider.lastRescheduleInput.ProviderFence, store.appointment.ProviderFence) {
		t.Fatalf("reschedule provider fence = %#v, want current appointment fence %#v", provider.lastRescheduleInput.ProviderFence, store.appointment.ProviderFence)
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

	store.appointment.Status = StatusCancelled
	store.setSnapshotGeneration(2)
	replayReq := req
	replayReq.AvailabilityQuoteID = "00000000-0000-4000-8000-000000000040"
	replayReq.SlotFingerprint = strings.Repeat("b", 64)
	replayed, replayFallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", replayReq)
	if err != nil {
		t.Fatalf("successful reschedule replay returned error after target mutation: %v", err)
	}
	if replayFallback != nil || replayed == nil || replayed.Status != StatusRescheduled || provider.rescheduleCalls != 1 {
		t.Fatalf("reschedule replay appointment=%#v fallback=%#v calls=%d, want prior success and one POS call", replayed, replayFallback, provider.rescheduleCalls)
	}

	changed := replayReq
	changed.Notes = ""
	conflictAppointment, conflictFallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", changed)
	if !errors.Is(err, ErrOperationConflict) || conflictAppointment != nil || conflictFallback != nil {
		t.Fatalf("notes omission replay appointment=%#v fallback=%#v err=%v, want operation conflict", conflictAppointment, conflictFallback, err)
	}
	if provider.rescheduleCalls != 1 {
		t.Fatalf("conflicting reschedule replay called provider %d times, want 1", provider.rescheduleCalls)
	}
}

func TestRescheduleRejectsMismatchedOrNonAdvancingProviderResult(t *testing.T) {
	tests := []struct {
		name        string
		bookingID   string
		version     int
		startOffset time.Duration
	}{
		{name: "different target", bookingID: "booking_other", version: 8},
		{name: "same version", bookingID: "booking_1", version: 7},
		{name: "older version", bookingID: "booking_1", version: 6},
		{name: "different returned range", bookingID: "booking_1", version: 8, startOffset: 49 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			startOffset := tt.startOffset
			if startOffset == 0 {
				startOffset = 48 * time.Hour
			}
			provider := &fakeProvider{rescheduledAppointment: &pos.Appointment{
				POSAppointmentID:      tt.bookingID,
				POSAppointmentVersion: tt.version,
				StartTime:             testStartTime().Add(startOffset),
				EndTime:               testStartTime().Add(startOffset + 45*time.Minute),
				Status:                string(pos.AppointmentStatusAccepted),
			}}
			service := NewService(store, []pos.POSProvider{provider})

			appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
				OperationKey:        "test-reschedule-operation",
				AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
				SlotFingerprint:     strings.Repeat("a", 64),
				StartTime:           testStartTime().Add(48 * time.Hour),
			})
			if err != nil {
				t.Fatalf("Reschedule returned error: %v", err)
			}
			if appointment != nil || store.rescheduled != nil {
				t.Fatalf("provider-inconsistent result persisted appointment: %#v / %#v", appointment, store.rescheduled)
			}
			if fallback == nil || fallback.ProviderOutcome != ProviderOutcomeUnknown || fallback.RetryPolicy != RetryPolicyBlocked || fallback.Reconciliation != ReconciliationRequired {
				t.Fatalf("fallback = %#v, want blocked unknown reconciliation", fallback)
			}
		})
	}
}

func TestRescheduleConvertsNewerCalendarPersistenceConflictToUnknownFallback(t *testing.T) {
	store := newFakeStore()
	store.saveRescheduleErr = ErrOperationConflict
	provider := &fakeProvider{rescheduledAppointment: &pos.Appointment{
		POSAppointmentID:      "booking_1",
		POSAppointmentVersion: 9,
		StartTime:             testStartTime().Add(72 * time.Hour),
		EndTime:               testStartTime().Add(72*time.Hour + 45*time.Minute),
		Status:                string(pos.AppointmentStatusAccepted),
	}}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		OperationKey:        "test-reschedule-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		StartTime:           testStartTime().Add(72 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Reschedule returned error: %v", err)
	}
	if appointment != nil || fallback == nil || fallback.ProviderOutcome != ProviderOutcomeUnknown || fallback.Reconciliation != ReconciliationRequired {
		t.Fatalf("result = appointment %#v fallback %#v, want unknown reconciliation fallback", appointment, fallback)
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
			Status:                string(pos.AppointmentStatusAccepted),
		},
	}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		OperationKey:        "test-reschedule-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		StartTime:           nextStart,
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
	provider := &fakeProvider{rescheduleErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square booking conflict"))}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		OperationKey:        "test-reschedule-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		StartTime:           testStartTime().Add(24 * time.Hour),
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
	if fallback.RetryPolicy != RetryPolicySafe || !fallback.CanRetry || fallback.Reconciliation != ReconciliationNotRequired {
		t.Fatalf("reschedule fallback policy = %#v, want retry-safe without reconciliation", fallback)
	}

	store.appointment.Status = StatusCancelled
	replayedAppointment, replayedFallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		OperationKey:        "test-reschedule-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000040",
		SlotFingerprint:     strings.Repeat("b", 64),
		StartTime:           testStartTime().Add(24 * time.Hour),
	})
	if err != nil || replayedAppointment != nil || replayedFallback == nil || provider.rescheduleCalls != 1 {
		t.Fatalf("fallback replay appointment=%#v fallback=%#v err=%v calls=%d, want prior fallback and one POS call", replayedAppointment, replayedFallback, err, provider.rescheduleCalls)
	}
}

func TestRescheduleMultiSegmentFallbackLeavesAppointmentUnchanged(t *testing.T) {
	store := newFakeStore()
	store.addSecondAppointmentSegment()
	originalStart := store.appointment.StartTime
	originalEnd := store.appointment.EndTime
	originalSegments := append([]BookingSegmentRecord(nil), store.appointment.Segments...)
	provider := &fakeProvider{rescheduleErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square booking conflict"))}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		OperationKey:        "test-reschedule-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		StartTime:           testStartTime().Add(24 * time.Hour),
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
	req := CancelRequest{
		OperationKey: "test-cancel-operation",
		Reason:       "Customer requested cancellation",
	}

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", req)
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
	if !sameProviderFence(provider.lastCancelInput.ProviderFence, store.appointment.ProviderFence) {
		t.Fatalf("cancel provider fence = %#v, want current appointment fence %#v", provider.lastCancelInput.ProviderFence, store.appointment.ProviderFence)
	}
	if store.pendingAction == nil {
		t.Fatalf("pending cancel attempt was not created before POS call")
	}
	if provider.lastCancelInput.IdempotencyKey != store.pendingAction.POSIdempotencyKey {
		t.Fatalf("idempotency key = %s, want %s", provider.lastCancelInput.IdempotencyKey, store.pendingAction.POSIdempotencyKey)
	}

	replayed, replayFallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", req)
	if err != nil {
		t.Fatalf("successful cancel replay returned error after target was cancelled: %v", err)
	}
	if replayFallback != nil || replayed == nil || replayed.Status != StatusCancelled || provider.cancelCalls != 1 {
		t.Fatalf("cancel replay appointment=%#v fallback=%#v calls=%d, want prior success and one POS call", replayed, replayFallback, provider.cancelCalls)
	}

	changed := req
	changed.Reason = "Caller used a different cancellation reason"
	conflictAppointment, conflictFallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", changed)
	if !errors.Is(err, ErrOperationConflict) || conflictAppointment != nil || conflictFallback != nil {
		t.Fatalf("changed cancel replay appointment=%#v fallback=%#v err=%v, want operation conflict", conflictAppointment, conflictFallback, err)
	}
	if provider.cancelCalls != 1 {
		t.Fatalf("conflicting cancel replay called provider %d times, want 1", provider.cancelCalls)
	}
}

func TestAppointmentActionsBlockAfterProviderLocationSwitchBeforePOSCall(t *testing.T) {
	t.Run("reschedule", func(t *testing.T) {
		store := newFakeStore()
		store.setProviderLocation("loc_2")
		provider := &fakeProvider{}
		service := NewService(store, []pos.POSProvider{provider})
		_, _, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
			OperationKey:        "location-switch-reschedule",
			AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
			SlotFingerprint:     strings.Repeat("a", 64),
			StartTime:           testStartTime().Add(24 * time.Hour),
		})
		if err == nil {
			t.Fatal("reschedule after location switch should fail closed")
		}
		if provider.rescheduleCalls != 0 {
			t.Fatalf("reschedule provider calls = %d, want zero", provider.rescheduleCalls)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store := newFakeStore()
		store.setProviderLocation("loc_2")
		provider := &fakeProvider{}
		service := NewService(store, []pos.POSProvider{provider})
		_, _, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
			OperationKey: "location-switch-cancel",
			Reason:       "Caller changed plans",
		})
		if err == nil {
			t.Fatal("cancel after location switch should fail closed")
		}
		if provider.cancelCalls != 0 {
			t.Fatalf("cancel provider calls = %d, want zero", provider.cancelCalls)
		}
	})
}

func TestAppointmentActionsAllowNewSnapshotAtSameOriginLocation(t *testing.T) {
	t.Run("reschedule", func(t *testing.T) {
		store := newFakeStore()
		store.setSnapshotGeneration(2)
		start := testStartTime().Add(24 * time.Hour)
		provider := &fakeProvider{rescheduledAppointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 8,
			Status:                string(pos.AppointmentStatusAccepted),
			StartTime:             start,
			EndTime:               start.Add(45 * time.Minute),
		}}
		service := NewService(store, []pos.POSProvider{provider})
		_, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
			OperationKey:        "new-generation-reschedule",
			AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
			SlotFingerprint:     strings.Repeat("a", 64),
			StartTime:           start,
		})
		if err != nil || fallback != nil || provider.rescheduleCalls != 1 {
			t.Fatalf("same-location newer-generation reschedule fallback/error/calls = %#v/%v/%d", fallback, err, provider.rescheduleCalls)
		}
		if provider.lastRescheduleInput.ProviderFence.SnapshotGeneration != 2 {
			t.Fatalf("reschedule generation = %d, want 2", provider.lastRescheduleInput.ProviderFence.SnapshotGeneration)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		store := newFakeStore()
		store.setSnapshotGeneration(2)
		provider := &fakeProvider{cancelledAppointment: &pos.Appointment{
			POSAppointmentID:      "booking_1",
			POSAppointmentVersion: 8,
			Status:                string(pos.AppointmentStatusCancelled),
		}}
		service := NewService(store, []pos.POSProvider{provider})
		_, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
			OperationKey: "new-generation-cancel",
			Reason:       "Caller changed plans",
		})
		if err != nil || fallback != nil || provider.cancelCalls != 1 {
			t.Fatalf("same-location newer-generation cancel fallback/error/calls = %#v/%v/%d", fallback, err, provider.cancelCalls)
		}
		if provider.lastCancelInput.ProviderFence.SnapshotGeneration != 2 {
			t.Fatalf("cancel generation = %d, want 2", provider.lastCancelInput.ProviderFence.SnapshotGeneration)
		}
	})
}

func TestCancelRejectsMismatchedOrNonAdvancingProviderResult(t *testing.T) {
	tests := []struct {
		name      string
		bookingID string
		version   int
	}{
		{name: "different target", bookingID: "booking_other", version: 8},
		{name: "same version", bookingID: "booking_1", version: 7},
		{name: "older version", bookingID: "booking_1", version: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			provider := &fakeProvider{cancelledAppointment: &pos.Appointment{
				POSAppointmentID:      tt.bookingID,
				POSAppointmentVersion: tt.version,
				Status:                string(pos.AppointmentStatusCancelled),
			}}
			service := NewService(store, []pos.POSProvider{provider})

			appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
				OperationKey: "test-cancel-operation",
				Reason:       "Caller changed plans",
			})
			if err != nil {
				t.Fatalf("Cancel returned error: %v", err)
			}
			if appointment != nil || store.cancelled != nil {
				t.Fatalf("provider-inconsistent result persisted appointment: %#v / %#v", appointment, store.cancelled)
			}
			if fallback == nil || fallback.ProviderOutcome != ProviderOutcomeUnknown || fallback.RetryPolicy != RetryPolicyBlocked || fallback.Reconciliation != ReconciliationRequired {
				t.Fatalf("fallback = %#v, want blocked unknown reconciliation", fallback)
			}
		})
	}
}

func TestCancelConvertsNewerCalendarPersistenceConflictToUnknownFallback(t *testing.T) {
	store := newFakeStore()
	store.saveCancelErr = ErrOperationConflict
	provider := &fakeProvider{cancelledAppointment: &pos.Appointment{
		POSAppointmentID:      "booking_1",
		POSAppointmentVersion: 10,
		Status:                string(pos.AppointmentStatusCancelled),
	}}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{OperationKey: "test-cancel-operation", Reason: "Schedule changed"})
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if appointment != nil || fallback == nil || fallback.ProviderOutcome != ProviderOutcomeUnknown || fallback.Reconciliation != ReconciliationRequired {
		t.Fatalf("result = appointment %#v fallback %#v, want unknown reconciliation fallback", appointment, fallback)
	}
}

func TestCancelStoresFallbackWhenPOSFails(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{cancelErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square permission denied"))}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
		OperationKey: "test-cancel-operation",
		Reason:       "Customer requested cancellation",
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
	if store.actionFallback.ErrorMessage != pos.SafeErrorMessage(pos.ErrorPermissionDenied) || strings.Contains(store.actionFallback.ErrorMessage, "square permission denied") {
		t.Fatalf("error message = %q, want sanitized stable copy", store.actionFallback.ErrorMessage)
	}
	if fallback.RetryPolicy != RetryPolicySafe || !fallback.CanRetry || fallback.Reconciliation != ReconciliationNotRequired {
		t.Fatalf("cancel fallback policy = %#v, want retry-safe without reconciliation", fallback)
	}

	store.appointment.Status = StatusCancelled
	replayedAppointment, replayedFallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
		OperationKey: "test-cancel-operation",
		Reason:       "Customer requested cancellation",
	})
	if err != nil || replayedAppointment != nil || replayedFallback == nil || provider.cancelCalls != 1 {
		t.Fatalf("fallback replay appointment=%#v fallback=%#v err=%v calls=%d, want prior fallback and one POS call", replayedAppointment, replayedFallback, err, provider.cancelCalls)
	}
}

func TestSquareTestCancelSafeRetryReachesProviderExactlyOnce(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		cancelErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square permission denied")),
	}
	service := NewService(store, []pos.POSProvider{provider})
	req := CancelRequest{
		OperationKey: "square-test-cancel-initial",
		Reason:       "AI booking readiness test cleanup",
		Source:       SourceSquareTestBooking,
	}

	appointment, first, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", req)
	if err != nil {
		t.Fatalf("initial Cancel returned error: %v", err)
	}
	if appointment != nil || first == nil || first.Status != StatusFallbackPending || first.RetryPolicy != RetryPolicySafe || !first.CanRetry {
		t.Fatalf("initial cancel result appointment=%#v attempt=%#v, want safe fallback", appointment, first)
	}
	if provider.cancelCalls != 1 {
		t.Fatalf("initial provider calls = %d, want 1", provider.cancelCalls)
	}

	provider.cancelErr = nil
	provider.cancelledAppointment = &pos.Appointment{
		POSAppointmentID:      "booking_1",
		POSAppointmentVersion: 8,
		StartTime:             testStartTime(),
		EndTime:               testStartTime().Add(45 * time.Minute),
		Status:                StatusCancelled,
	}
	retry := req
	retry.OperationKey = "square-test-cancel-retry"
	retry.RetryOfAttemptID = first.ID
	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", retry)
	if err != nil {
		t.Fatalf("retry Cancel returned error: %v", err)
	}
	if fallback != nil || appointment == nil || appointment.Status != StatusCancelled {
		t.Fatalf("retry cancel result appointment=%#v fallback=%#v, want cancelled", appointment, fallback)
	}
	if provider.cancelCalls != 2 {
		t.Fatalf("provider calls after retry = %d, want exactly one additional call", provider.cancelCalls)
	}
	if store.pendingAction == nil || store.pendingAction.RetryOfAttemptID != first.ID {
		t.Fatalf("retry pending action = %#v, want lineage to %s", store.pendingAction, first.ID)
	}
}

func TestCancelTimeoutBlocksRetryUntilProviderReconciliation(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{cancelErr: context.DeadlineExceeded}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
		OperationKey: "voice-call-cancel-timeout",
		Reason:       "Caller asked to cancel the appointment",
	})
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if appointment != nil {
		t.Fatalf("appointment should remain unchanged after unknown POS result")
	}
	if fallback == nil || fallback.ProviderOutcome != ProviderOutcomeUnknown || fallback.RetryPolicy != RetryPolicyBlocked || fallback.Reconciliation != ReconciliationRequired || fallback.CanRetry {
		t.Fatalf("timeout fallback = %#v, want unknown/blocked/reconciliation-required", fallback)
	}
	if store.appointment.Status != StatusConfirmed {
		t.Fatalf("stored appointment status = %s, want original confirmed state", store.appointment.Status)
	}
}

func TestCancelFallbackSnapshotsMultipleSegments(t *testing.T) {
	store := newFakeStore()
	store.addSecondAppointmentSegment()
	provider := &fakeProvider{cancelErr: pos.NewWriteError(pos.WriteOutcomeDefinitiveFailure, pos.WritePhaseDispatch, errors.New("square permission denied"))}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Cancel(context.Background(), "salon_1", "owner_1", "appointment_1", CancelRequest{
		OperationKey: "test-cancel-operation",
		Reason:       "Customer requested cancellation",
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
	if provider.lastAvailabilityInput.Timezone != "America/Chicago" {
		t.Fatalf("provider timezone = %q, want salon timezone", provider.lastAvailabilityInput.Timezone)
	}
	if len(result.Slots) != 1 {
		t.Fatalf("slots = %#v, want one business-hours slot", result.Slots)
	}
	if result.QuoteID == "" || len(result.RequestFingerprint) != 64 || result.ExpiresAt == nil {
		t.Fatalf("availability quote = id=%q fingerprint=%q expires=%v, want immutable quote metadata", result.QuoteID, result.RequestFingerprint, result.ExpiresAt)
	}
	if store.availabilityQuote == nil || store.availabilityQuote.RequestFingerprint != result.RequestFingerprint {
		t.Fatalf("stored quote = %#v, want returned request fingerprint", store.availabilityQuote)
	}
	slot := result.Slots[0]
	if len(slot.Fingerprint) != 64 {
		t.Fatalf("slot fingerprint = %q, want SHA-256 fingerprint", slot.Fingerprint)
	}
	if slot.StaffID != "staff_1" || slot.StaffName != "Mai Nguyen" {
		t.Fatalf("slot staff = %s/%s, want internal staff mapping", slot.StaffID, slot.StaffName)
	}
	if !slot.StartTime.Equal(time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC()) {
		t.Fatalf("slot start = %s, want 10am local", slot.StartTime)
	}
}

func TestAvailableSlotsPersistsExternalTargetOriginForRescheduleVersionZero(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	store := newFakeStore()
	store.appointment.ID = "00000000-0000-4000-8000-000000000041"
	store.appointment.AuthorityAppointmentVersion = 0
	store.appointment.POSAppointmentVersion = 0
	provider := &fakeProvider{availabilitySlots: []pos.TimeSlot{{
		StartTime: time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC(),
		EndTime:   time.Date(2026, 6, 15, 10, 45, 0, 0, loc).UTC(),
		StaffID:   "square_staff_1",
	}}}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		TargetAppointmentID: store.appointment.ID,
		ServiceID:           "service_1",
		StaffID:             "staff_1",
		PreferredDate:       "2026-06-15",
	})
	if err != nil {
		t.Fatalf("target-origin availability: %v", err)
	}
	if result.QuoteID == "" || result.TargetAuthorityAppointmentVersion != 0 {
		t.Fatalf("target-origin result=%#v", result)
	}
	if store.availabilityQuote == nil || store.availabilityQuote.OperationType != BookingActionReschedule || store.availabilityQuote.TargetAppointmentID != store.appointment.ID || store.availabilityQuote.TargetAuthorityAppointmentVersion != 0 {
		t.Fatalf("target-origin quote=%#v", store.availabilityQuote)
	}
	if provider.availabilityCalls != 1 {
		t.Fatalf("provider availability calls=%d, want 1", provider.availabilityCalls)
	}
}

func TestAvailableSlotsCreatesFreshRetryOriginQuoteForExactSafeFallback(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	store := newFakeStore()
	retryAttemptID := "00000000-0000-4000-8000-000000000051"
	start := time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC()
	store.operations["original-safe-fallback"] = &BookingAttempt{
		ID:                  retryAttemptID,
		SalonID:             "salon_1",
		SchedulingAuthority: SchedulingAuthorityExternalProvider,
		POSProvider:         pos.ProviderSquare,
		ProviderFence:       store.service.ProviderFence,
		OperationType:       BookingActionBook,
		Status:              StatusFallbackPending,
		RetryPolicy:         RetryPolicySafe,
		RequestedStartTime:  start,
		RequestedEndTime:    start.Add(45 * time.Minute),
		Segments: []BookingSegmentSnapshot{{
			ServiceID:          store.service.ID,
			POSServiceID:       store.service.POSServiceID,
			POSServiceVersion:  store.service.POSServiceVersion,
			StaffID:            store.staff.ID,
			POSStaffID:         store.staff.POSStaffID,
			StaffSelectionMode: StaffSelectionSpecific,
			DurationMinutes:    store.service.DurationMinutes,
			SortOrder:          1,
		}},
	}
	provider := &fakeProvider{availabilitySlots: []pos.TimeSlot{
		{StartTime: start.Add(-time.Hour), EndTime: start.Add(-15 * time.Minute), StaffID: store.staff.POSStaffID},
		{StartTime: start, EndTime: start.Add(45 * time.Minute), StaffID: store.staff.POSStaffID},
	}}
	service := NewService(store, []pos.POSProvider{provider})

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		RetryOfAttemptID: retryAttemptID,
		ServiceID:        store.service.ID,
		StaffID:          store.staff.ID,
		PreferredDate:    "2026-06-15",
	})
	if err != nil {
		t.Fatalf("retry availability: %v", err)
	}
	if result.QuoteID == "" || len(result.Slots) != 1 || !result.Slots[0].StartTime.Equal(start) {
		t.Fatalf("retry availability result = %#v, want one exact original slot and a fresh quote", result)
	}
	if store.availabilityQuote == nil || store.availabilityQuote.RetryOfAttemptID != retryAttemptID ||
		store.availabilityQuote.OperationType != "" || len(store.availabilityQuote.Slots) != 1 {
		t.Fatalf("retry quote = %#v, want fresh retry-origin quote", store.availabilityQuote)
	}
	if provider.availabilityCalls != 1 {
		t.Fatalf("provider availability calls = %d, want one fresh lookup", provider.availabilityCalls)
	}
}

func TestAvailableSlotsRejectsChangedOrUnsafeRetryBeforeProviderLookup(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start := time.Date(2026, 6, 15, 10, 0, 0, 0, loc).UTC()
	tests := []struct {
		name   string
		mutate func(*fakeStore, *BookingAttempt)
	}{
		{
			name: "unsafe retry policy",
			mutate: func(_ *fakeStore, attempt *BookingAttempt) {
				attempt.RetryPolicy = RetryPolicyBlocked
			},
		},
		{
			name: "changed provider service version",
			mutate: func(store *fakeStore, _ *BookingAttempt) {
				store.service.POSServiceVersion++
				store.services[0] = store.service
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeStore()
			origin := &BookingAttempt{
				ID:                  "00000000-0000-4000-8000-000000000052",
				SalonID:             "salon_1",
				SchedulingAuthority: SchedulingAuthorityExternalProvider,
				POSProvider:         pos.ProviderSquare,
				ProviderFence:       store.service.ProviderFence,
				OperationType:       BookingActionBook,
				Status:              StatusFallbackPending,
				RetryPolicy:         RetryPolicySafe,
				RequestedStartTime:  start,
				RequestedEndTime:    start.Add(45 * time.Minute),
				Segments: []BookingSegmentSnapshot{{
					ServiceID:          store.service.ID,
					POSServiceID:       store.service.POSServiceID,
					POSServiceVersion:  store.service.POSServiceVersion,
					StaffID:            store.staff.ID,
					POSStaffID:         store.staff.POSStaffID,
					StaffSelectionMode: StaffSelectionSpecific,
					DurationMinutes:    store.service.DurationMinutes,
					SortOrder:          1,
				}},
			}
			store.operations["unsafe-or-changed"] = origin
			test.mutate(store, origin)
			provider := &fakeProvider{availabilitySlots: []pos.TimeSlot{{StartTime: start, EndTime: start.Add(45 * time.Minute), StaffID: store.staff.POSStaffID}}}
			service := NewService(store, []pos.POSProvider{provider})

			_, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
				RetryOfAttemptID: origin.ID,
				ServiceID:        store.service.ID,
				StaffID:          store.staff.ID,
				PreferredDate:    "2026-06-15",
			})
			if !errors.Is(err, ErrOperationConflict) {
				t.Fatalf("error = %v, want operation conflict", err)
			}
			if provider.availabilityCalls != 0 || store.availabilityQuote != nil {
				t.Fatalf("provider calls/quote = %d/%#v, want no new provider or quote side effect", provider.availabilityCalls, store.availabilityQuote)
			}
		})
	}
}

func TestCreateAndAvailabilityRejectStaleNonActiveProviderMappings(t *testing.T) {
	store := newFakeStore()
	store.activeProvider = "future_pos"
	provider := &fakeProvider{}
	service := NewService(store, []pos.POSProvider{provider})

	attempt, err := service.Create(context.Background(), "salon_1", "owner_1", validCreateRequest())
	if !errors.Is(err, pos.ErrNotFound) || attempt != nil {
		t.Fatalf("Create = attempt %#v err %v, want stale Square mapping rejected", attempt, err)
	}
	if provider.createAppointmentCalls != 0 {
		t.Fatalf("provider create calls = %d, want zero", provider.createAppointmentCalls)
	}

	result, err := service.AvailableSlots(context.Background(), "salon_1", "owner_1", AvailabilityRequest{
		ServiceID:     "service_1",
		StaffID:       "staff_1",
		PreferredDate: "2026-06-15",
	})
	if !errors.Is(err, pos.ErrNotFound) || result != nil {
		t.Fatalf("AvailableSlots = result %#v err %v, want stale Square mapping rejected", result, err)
	}
	if provider.availabilityCalls != 0 {
		t.Fatalf("provider availability calls = %d, want zero", provider.availabilityCalls)
	}
}

func TestRescheduleKeepsHistoricalAppointmentProviderAfterActiveProviderSwitch(t *testing.T) {
	store := newFakeStore()
	store.activeProvider = "future_pos"
	provider := &fakeProvider{rescheduledAppointment: &pos.Appointment{
		POSAppointmentID:      "booking_1",
		POSAppointmentVersion: 8,
		StartTime:             testStartTime().Add(24 * time.Hour),
		EndTime:               testStartTime().Add(24*time.Hour + 45*time.Minute),
		Status:                StatusConfirmed,
	}}
	service := NewService(store, []pos.POSProvider{provider})

	appointment, fallback, err := service.Reschedule(context.Background(), "salon_1", "owner_1", "appointment_1", RescheduleRequest{
		OperationKey:        "historical-square-reschedule",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		StartTime:           testStartTime().Add(24 * time.Hour),
	})
	if err != nil || fallback != nil || appointment == nil {
		t.Fatalf("Reschedule = appointment %#v fallback %#v err %v, want historical Square action", appointment, fallback, err)
	}
	if provider.rescheduleCalls != 1 {
		t.Fatalf("Square reschedule calls = %d, want one", provider.rescheduleCalls)
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
		ID:            "staff_2",
		POSProvider:   pos.ProviderSquare,
		POSStaffID:    "square_staff_2",
		Name:          "An Nguyen",
		ProviderFence: store.staff.ProviderFence,
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
		ProviderFence:     store.service.ProviderFence,
	})
	store.staffRefs = append(store.staffRefs, StaffRef{
		ID:            "staff_2",
		POSProvider:   pos.ProviderSquare,
		POSStaffID:    "square_staff_2",
		Name:          "An Nguyen",
		ProviderFence: store.staff.ProviderFence,
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

func TestMergeCalendarCustomerDetailsHydratesOnlyMissingProviderFields(t *testing.T) {
	item := mergeCalendarCustomerDetails(CalendarAppointmentImport{
		CustomerName:  "Square customer",
		CustomerPhone: "",
		CustomerEmail: "provider@example.com",
	}, "Linh Tran", "(312) 555-0101", "canonical@example.com")
	if item.CustomerName != "Linh Tran" || item.CustomerPhone != "3125550101" {
		t.Fatalf("hydrated item = %#v, want canonical name and phone", item)
	}
	if item.CustomerEmail != "provider@example.com" {
		t.Fatalf("email = %q, want richer provider value preserved", item.CustomerEmail)
	}
}

func TestPostPOSPersistenceContextIsCancellationIndependentAndBounded(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	persistCtx, cancelPersist := postPOSPersistenceContext(requestCtx)
	defer cancelPersist()
	if err := persistCtx.Err(); err != nil {
		t.Fatalf("persistence context inherited request cancellation: %v", err)
	}
	deadline, ok := persistCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > postPOSPersistenceTimeout+time.Second {
		t.Fatalf("deadline = %v ok=%t, want bounded persistence deadline", deadline, ok)
	}
}

func validCreateRequest() CreateBookingRequest {
	return CreateBookingRequest{
		OperationKey:        "test-create-operation",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000039",
		SlotFingerprint:     strings.Repeat("a", 64),
		Source:              SourceAIConversationSimulator,
		CustomerName:        "Linh Tran",
		CustomerPhone:       "312-555-0101",
		CustomerEmail:       "linh@example.com",
		ServiceID:           "service_1",
		StaffID:             "staff_1",
		StartTime:           testStartTime(),
		Notes:               "First visit",
	}
}

func testStartTime() time.Time {
	return time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
}

func TestAppointmentActionFallbackResultReturnsConvergedTerminalAppointment(t *testing.T) {
	appointment := &Appointment{ID: "appointment-1", Status: StatusRescheduled}
	attempt := &BookingAttempt{Status: StatusRescheduled, Appointment: appointment}
	saved, fallback, err := appointmentActionFallbackResult(BookingActionReschedule, attempt, nil)
	if err != nil || saved != appointment || fallback != nil {
		t.Fatalf("converged fallback result = appointment %#v fallback %#v err %v", saved, fallback, err)
	}

	pending := &BookingAttempt{Status: StatusFallbackPending}
	saved, fallback, err = appointmentActionFallbackResult(BookingActionReschedule, pending, nil)
	if err != nil || saved != nil || fallback != pending {
		t.Fatalf("unresolved fallback result = appointment %#v fallback %#v err %v", saved, fallback, err)
	}

	wantErr := errors.New("persist failed")
	saved, fallback, err = appointmentActionFallbackResult(BookingActionReschedule, pending, wantErr)
	if !errors.Is(err, wantErr) || saved != nil || fallback != pending {
		t.Fatalf("failed fallback result = appointment %#v fallback %#v err %v", saved, fallback, err)
	}
}

type fakeStore struct {
	mu                               sync.Mutex
	operations                       map[string]*BookingAttempt
	activeProvider                   string
	activeProviderFence              pos.ProviderFence
	activeProviderFenceCalls         int
	service                          ServiceRef
	services                         []ServiceRef
	staff                            StaffRef
	staffRefs                        []StaffRef
	customer                         CustomerRef
	schedule                         Schedule
	appointment                      AppointmentActionRef
	pending                          *PendingBookingRecord
	confirmed                        *ConfirmedBookingRecord
	fallback                         *FallbackBookingRecord
	pendingAction                    *PendingAppointmentActionRecord
	rescheduled                      *RescheduledAppointmentRecord
	cancelled                        *CancelledAppointmentRecord
	actionFallback                   *AppointmentActionFallbackRecord
	appointments                     []Appointment
	bookingAttempts                  []BookingAttempt
	calendarAppointments             []Appointment
	calendarPendingRequests          []BookingAttempt
	calendarEvents                   []CalendarEvent
	calendarEventCursor              CalendarEventCursor
	calendarEventLimit               int
	calendarStartTime                time.Time
	calendarEndTime                  time.Time
	calendarImports                  []CalendarAppointmentImport
	calendarUpsertFence              pos.ProviderFence
	calendarUpsertErr                error
	calendarSyncSummary              CalendarSyncSummary
	calendarSyncLogStatus            string
	calendarSyncLogMessage           string
	posErrorOperation                string
	listAppointmentLimit             int
	listAppointmentOffset            int
	listBookingAttemptStatus         string
	listBookingAttemptLimit          int
	listBookingAttemptOffset         int
	rescheduleLookup                 RescheduleLookupRequest
	linkedCustomer                   *CustomerRef
	linkCustomerErr                  error
	expireLeaseCalls                 int
	latestTest                       *TestBookingRecord
	availabilityQuote                *AvailabilityQuoteRecord
	reconciliationTasks              []ReconciliationTask
	reconciliationCandidates         []ReconciliationCandidate
	reconciliationCandidateAttemptID string
	reconciliationRequest            ResolveReconciliationRequest
	saveRescheduleErr                error
	saveCancelErr                    error
}

func newFakeStore() *fakeStore {
	fence := pos.ProviderFence{LocationID: "loc_1", SnapshotGeneration: 1}
	store := &fakeStore{
		operations:          map[string]*BookingAttempt{},
		activeProvider:      pos.ProviderSquare,
		activeProviderFence: fence,
		service: ServiceRef{
			ID:                "service_1",
			POSProvider:       pos.ProviderSquare,
			POSServiceID:      "square_service_1",
			POSServiceVersion: 123,
			Name:              "Classic Manicure",
			DurationMinutes:   45,
			PriceFrom:         35,
			ProviderFence:     fence,
		},
		staff: StaffRef{
			ID:            "staff_1",
			POSProvider:   pos.ProviderSquare,
			POSStaffID:    "square_staff_1",
			Name:          "Mai Nguyen",
			ProviderFence: fence,
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
		ID:                          "appointment_1",
		SalonID:                     "salon_1",
		SchedulingAuthority:         SchedulingAuthorityExternalProvider,
		AuthorityAppointmentVersion: 7,
		POSProvider:                 pos.ProviderSquare,
		ProviderLocationID:          fence.LocationID,
		ProviderFence:               fence,
		POSAppointmentID:            "booking_1",
		POSAppointmentVersion:       7,
		Status:                      StatusConfirmed,
		CustomerName:                "Linh Tran",
		CustomerPhone:               "+13125550101",
		CustomerEmail:               "linh@example.com",
		Service:                     store.service,
		Staff:                       store.staff,
		StaffSelectionMode:          StaffSelectionSpecific,
		StartTime:                   testStartTime(),
		EndTime:                     testStartTime().Add(45 * time.Minute),
		Notes:                       "First visit",
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
		ProviderFence:     f.service.ProviderFence,
	}
	secondStaff := StaffRef{
		ID:            "staff_2",
		POSProvider:   pos.ProviderSquare,
		POSStaffID:    "square_staff_2",
		Name:          "An Nguyen",
		ProviderFence: f.staff.ProviderFence,
	}
	f.services = append(f.services, secondService)
	f.staffRefs = append(f.staffRefs, secondStaff)
	f.appointment.EndTime = f.appointment.StartTime.Add(75 * time.Minute)
	f.appointment.Segments = []BookingSegmentRecord{
		{Service: f.service, Staff: f.staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1},
		{Service: secondService, Staff: secondStaff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 2},
	}
}

func (f *fakeStore) setSnapshotGeneration(generation int64) {
	f.activeProviderFence.SnapshotGeneration = generation
	f.service.ProviderFence.SnapshotGeneration = generation
	f.staff.ProviderFence.SnapshotGeneration = generation
	for index := range f.services {
		f.services[index].ProviderFence.SnapshotGeneration = generation
	}
	for index := range f.staffRefs {
		f.staffRefs[index].ProviderFence.SnapshotGeneration = generation
	}
	f.appointment.Service.ProviderFence.SnapshotGeneration = generation
	f.appointment.Staff.ProviderFence.SnapshotGeneration = generation
	f.appointment.ProviderFence.SnapshotGeneration = generation
	for index := range f.appointment.Segments {
		f.appointment.Segments[index].Service.ProviderFence.SnapshotGeneration = generation
		f.appointment.Segments[index].Staff.ProviderFence.SnapshotGeneration = generation
	}
}

func (f *fakeStore) setProviderLocation(locationID string) {
	f.activeProviderFence.LocationID = locationID
	f.service.ProviderFence.LocationID = locationID
	f.staff.ProviderFence.LocationID = locationID
	for index := range f.services {
		f.services[index].ProviderFence.LocationID = locationID
	}
	for index := range f.staffRefs {
		f.staffRefs[index].ProviderFence.LocationID = locationID
	}
	f.appointment.Service.ProviderFence.LocationID = locationID
	f.appointment.Staff.ProviderFence.LocationID = locationID
	f.appointment.ProviderFence.LocationID = locationID
	for index := range f.appointment.Segments {
		f.appointment.Segments[index].Service.ProviderFence.LocationID = locationID
		f.appointment.Segments[index].Staff.ProviderFence.LocationID = locationID
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
	return f.activeProvider, nil
}

func (f *fakeStore) GetActiveProviderFence(ctx context.Context, salonID string, ownerUserID string) (string, pos.ProviderFence, error) {
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return "", pos.ProviderFence{}, pos.ErrNotFound
	}
	f.activeProviderFenceCalls++
	return f.activeProvider, f.activeProviderFence, nil
}

func (f *fakeStore) GetBookableService(ctx context.Context, salonID string, provider string, serviceID string) (*ServiceRef, error) {
	for _, service := range f.services {
		if serviceID == service.ID && provider == service.POSProvider {
			item := service
			return &item, nil
		}
	}
	return nil, pos.ErrNotFound
}

func (f *fakeStore) GetBookableStaff(ctx context.Context, salonID string, provider string, staffID string) (*StaffRef, error) {
	for _, staff := range f.staffRefs {
		if staffID == staff.ID && provider == staff.POSProvider {
			item := staff
			return &item, nil
		}
	}
	return nil, pos.ErrNotFound
}

func (f *fakeStore) ListBookableStaffRefs(ctx context.Context, salonID string, provider string) ([]StaffRef, error) {
	items := make([]StaffRef, 0, len(f.staffRefs))
	for _, staff := range f.staffRefs {
		if staff.POSProvider == provider {
			items = append(items, staff)
		}
	}
	return items, nil
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

func (f *fakeStore) GetSchedule(ctx context.Context, salonID string, provider string, fence pos.ProviderFence) (*Schedule, error) {
	if provider != f.activeProvider || !sameProviderFence(fence, f.service.ProviderFence) {
		return nil, ErrAvailabilityQuoteStale
	}
	return &f.schedule, nil
}

func (f *fakeStore) GetSafeRetryAvailabilityOrigin(ctx context.Context, salonID string, ownerUserID string, attemptID string) (*BookingAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return nil, ErrOperationConflict
	}
	for _, attempt := range f.operations {
		if attempt.ID == attemptID && attempt.SchedulingAuthority == SchedulingAuthorityExternalProvider &&
			attempt.OperationType == BookingActionBook && attempt.Status == StatusFallbackPending &&
			attempt.RetryPolicy == RetryPolicySafe && attempt.SupersededAt == nil {
			copy := *attempt
			copy.Segments = append([]BookingSegmentSnapshot(nil), attempt.Segments...)
			return &copy, nil
		}
	}
	return nil, ErrOperationConflict
}

func (f *fakeStore) CreateAvailabilityQuote(ctx context.Context, record AvailabilityQuoteRecord) (*AvailabilityQuote, error) {
	f.availabilityQuote = &record
	return &AvailabilityQuote{
		ID:                                "00000000-0000-4000-8000-000000000039",
		SalonID:                           record.SalonID,
		Provider:                          record.Provider,
		ProviderFence:                     record.ProviderFence,
		RequestFingerprint:                record.RequestFingerprint,
		OperationType:                     record.OperationType,
		TargetAppointmentID:               record.TargetAppointmentID,
		TargetAuthorityAppointmentVersion: record.TargetAuthorityAppointmentVersion,
		ExpiresAt:                         record.ExpiresAt,
		CreatedAt:                         time.Now().UTC(),
		Slots:                             append([]AvailabilitySlot(nil), record.Slots...),
	}, nil
}

func (f *fakeStore) GetAvailabilityQuoteProviderFence(ctx context.Context, salonID string, provider string, quoteID string, slotFingerprint string) (pos.ProviderFence, error) {
	if salonID != "salon_1" || provider != pos.ProviderSquare || quoteID == "" || len(slotFingerprint) != 64 {
		return pos.ProviderFence{}, ErrAvailabilityQuoteStale
	}
	if f.availabilityQuote != nil && validProviderFence(f.availabilityQuote.ProviderFence) {
		return f.availabilityQuote.ProviderFence, nil
	}
	return f.service.ProviderFence, nil
}

func (f *fakeStore) GetBookingOperation(ctx context.Context, salonID string, ownerUserID string, operationKey string) (*BookingAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if salonID != "salon_1" || ownerUserID != "owner_1" {
		return nil, pos.ErrNotFound
	}
	attempt, ok := f.operations[operationKey]
	if !ok {
		return nil, pos.ErrNotFound
	}
	copy := *attempt
	copy.Segments = append([]BookingSegmentSnapshot(nil), attempt.Segments...)
	if attempt.Appointment != nil {
		appointmentCopy := *attempt.Appointment
		appointmentCopy.Segments = append([]BookingSegmentSnapshot(nil), attempt.Appointment.Segments...)
		copy.Appointment = &appointmentCopy
	}
	return &copy, nil
}

func (f *fakeStore) ClaimPendingBookingAttempt(ctx context.Context, record PendingBookingRecord) (*BookingOperationClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = &record
	if existing, ok := f.operations[record.OperationKey]; ok {
		if existing.RequestFingerprint != record.RequestFingerprint {
			return nil, ErrOperationConflict
		}
		copy := *existing
		return &BookingOperationClaim{Attempt: &copy, Acquired: false}, nil
	}
	var retryTarget *BookingAttempt
	if record.RetryOfAttemptID != "" {
		for _, existing := range f.operations {
			if existing.ID == record.RetryOfAttemptID && existing.RequestFingerprint == record.RequestFingerprint && existing.Status == StatusFallbackPending && existing.RetryPolicy == RetryPolicySafe && existing.SupersededAt == nil {
				retryTarget = existing
				break
			}
		}
		if retryTarget == nil {
			return nil, ErrOperationConflict
		}
		now := time.Now().UTC()
		retryTarget.SupersededAt = &now
	}
	for _, existing := range f.operations {
		if existing.RequestFingerprint == record.RequestFingerprint && existing.SupersededAt == nil {
			copy := *existing
			return &BookingOperationClaim{Attempt: &copy, Acquired: false}, nil
		}
	}
	attemptID := "attempt_1"
	if record.Source == SourceSquareTestBooking {
		attemptID = "00000000-0000-4000-8000-000000000041"
		if record.RetryOfAttemptID != "" {
			attemptID = "00000000-0000-4000-8000-000000000042"
		}
	}
	attempt := &BookingAttempt{
		ID:                  attemptID,
		SalonID:             record.SalonID,
		Source:              record.Source,
		Status:              StatusPOSPending,
		POSProvider:         record.Provider,
		POSIdempotencyKey:   record.POSIdempotencyKey,
		OperationKey:        record.OperationKey,
		RequestFingerprint:  record.RequestFingerprint,
		RetryOfAttemptID:    record.RetryOfAttemptID,
		AvailabilityQuoteID: record.AvailabilityQuoteID,
		SlotFingerprint:     record.SlotFingerprint,
		ProviderFence:       record.ProviderFence,
		OperationType:       BookingActionBook,
		ProcessingToken:     record.ProcessingToken,
		ProviderOutcome:     ProviderOutcomeNotStarted,
		RetryPolicy:         RetryPolicyNone,
		Reconciliation:      ReconciliationNotRequired,
		CustomerName:        record.CustomerName,
		CustomerPhone:       record.CustomerPhone,
		CustomerEmail:       record.CustomerEmail,
		ServiceID:           record.Service.ID,
		StaffID:             record.Staff.ID,
		StaffSelectionMode:  record.StaffSelectionMode,
		Segments:            bookingSegmentSnapshots(record.Segments),
		RequestedStartTime:  record.StartTime,
		RequestedEndTime:    record.EndTime,
		Notes:               record.Notes,
	}
	if retryTarget != nil {
		retryTarget.SupersededByAttemptID = attempt.ID
		attempt.RetrySequence = retryTarget.RetrySequence + 1
	}
	f.operations[record.OperationKey] = attempt
	copy := *attempt
	return &BookingOperationClaim{Attempt: &copy, Acquired: true}, nil
}

func (f *fakeStore) MarkBookingOperationStarted(ctx context.Context, salonID string, attemptID string, processingToken string, leaseExpiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, attempt := range f.operations {
		if attempt.ID == attemptID && attempt.SalonID == salonID && attempt.ProcessingToken == processingToken {
			attempt.ProviderOutcome = ProviderOutcomeInFlight
			value := leaseExpiresAt
			attempt.ProcessingLeaseEnds = &value
			return nil
		}
	}
	return ErrOperationInProgress
}

func (f *fakeStore) ExpireBookingOperationLeases(ctx context.Context, salonID string) error {
	f.expireLeaseCalls++
	return nil
}

func (f *fakeStore) SaveConfirmedBooking(ctx context.Context, record ConfirmedBookingRecord) (*BookingAttempt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.confirmed = &record
	attempt := &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             StatusConfirmed,
		OperationType:      BookingActionBook,
		ProviderOutcome:    ProviderOutcomeSucceeded,
		RetryPolicy:        RetryPolicyNone,
		Reconciliation:     ReconciliationNotRequired,
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
	}
	f.setOperationResult(record.AttemptID, attempt)
	return attempt, nil
}

func (f *fakeStore) SaveFallbackBooking(ctx context.Context, record FallbackBookingRecord) (*BookingAttempt, error) {
	f.fallback = &record
	status := record.Status
	if status == "" {
		status = StatusFallbackPending
	}
	attempt := &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Source:             record.Source,
		Status:             status,
		OperationType:      BookingActionBook,
		ProviderOutcome:    record.ProviderOutcome,
		RetryPolicy:        record.RetryPolicy,
		Reconciliation:     record.Reconciliation,
		POSProvider:        record.Provider,
		POSBookingID:       record.POSBookingID,
		CustomerName:       record.CustomerName,
		CustomerPhone:      record.CustomerPhone,
		ServiceID:          record.Service.ID,
		StaffID:            record.Staff.ID,
		StaffSelectionMode: record.StaffSelectionMode,
		RequestedStartTime: record.StartTime,
		RequestedEndTime:   record.EndTime,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
	}
	annotateBookingAttemptPolicy(attempt)
	f.setOperationResult(record.AttemptID, attempt)
	return attempt, nil
}

func (f *fakeStore) GetAppointmentForOwner(ctx context.Context, salonID string, ownerUserID string, appointmentID string) (*AppointmentActionRef, error) {
	if salonID != "salon_1" || ownerUserID != "owner_1" || appointmentID != f.appointment.ID {
		return nil, pos.ErrNotFound
	}
	if strings.TrimSpace(f.appointment.ProviderLocationID) == "" ||
		strings.TrimSpace(f.appointment.ProviderLocationID) != strings.TrimSpace(f.activeProviderFence.LocationID) ||
		f.activeProviderFence.SnapshotGeneration <= 0 {
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

func (f *fakeStore) ClaimPendingAppointmentAction(ctx context.Context, record PendingAppointmentActionRecord) (*BookingOperationClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segments := record.Segments
	if len(segments) == 0 {
		segments = appointmentActionSegments(record.Appointment)
	}
	primary := segments[0]
	record.Segments = segments
	f.pendingAction = &record
	if existing, ok := f.operations[record.OperationKey]; ok {
		if existing.RequestFingerprint != record.RequestFingerprint {
			return nil, ErrOperationConflict
		}
		copy := *existing
		return &BookingOperationClaim{Attempt: &copy, Acquired: false}, nil
	}
	var retryTarget *BookingAttempt
	if record.RetryOfAttemptID != "" {
		for _, existing := range f.operations {
			if existing.ID == record.RetryOfAttemptID && existing.RequestFingerprint == record.RequestFingerprint && existing.Status == StatusFallbackPending && existing.RetryPolicy == RetryPolicySafe && existing.SupersededAt == nil {
				retryTarget = existing
				break
			}
		}
		if retryTarget == nil {
			return nil, ErrOperationConflict
		}
		now := time.Now().UTC()
		retryTarget.SupersededAt = &now
	}
	for _, existing := range f.operations {
		if existing.OperationType == record.OperationType && existing.RequestFingerprint == record.RequestFingerprint && existing.SupersededAt == nil {
			copy := *existing
			return &BookingOperationClaim{Attempt: &copy, Acquired: false}, nil
		}
	}
	attemptID := "attempt_action_1"
	if record.Source == SourceSquareTestBooking {
		attemptID = "00000000-0000-4000-8000-000000000051"
		if record.RetryOfAttemptID != "" {
			attemptID = "00000000-0000-4000-8000-000000000052"
		}
	}
	attempt := &BookingAttempt{
		ID:                      attemptID,
		SalonID:                 record.SalonID,
		Source:                  record.Source,
		Status:                  StatusPOSPending,
		POSProvider:             record.Provider,
		POSBookingID:            record.Appointment.POSAppointmentID,
		POSIdempotencyKey:       record.POSIdempotencyKey,
		OperationKey:            record.OperationKey,
		RequestFingerprint:      record.RequestFingerprint,
		RetryOfAttemptID:        record.RetryOfAttemptID,
		AvailabilityQuoteID:     record.AvailabilityQuoteID,
		SlotFingerprint:         record.SlotFingerprint,
		ProviderFence:           record.ProviderFence,
		OperationType:           record.OperationType,
		TargetAppointmentID:     record.Appointment.ID,
		TargetPOSBookingVersion: record.Appointment.POSAppointmentVersion,
		ProcessingToken:         record.ProcessingToken,
		ProviderOutcome:         ProviderOutcomeNotStarted,
		RetryPolicy:             RetryPolicyNone,
		Reconciliation:          ReconciliationNotRequired,
		CustomerName:            record.Appointment.CustomerName,
		CustomerPhone:           record.Appointment.CustomerPhone,
		CustomerEmail:           record.Appointment.CustomerEmail,
		ServiceID:               primary.Service.ID,
		StaffID:                 primary.Staff.ID,
		StaffSelectionMode:      primary.StaffSelectionMode,
		Segments:                bookingSegmentSnapshots(segments),
		RequestedStartTime:      record.RequestedStartTime,
		RequestedEndTime:        record.RequestedEndTime,
		Notes:                   record.Notes,
	}
	if retryTarget != nil {
		retryTarget.SupersededByAttemptID = attempt.ID
		attempt.RetrySequence = retryTarget.RetrySequence + 1
	}
	f.operations[record.OperationKey] = attempt
	copy := *attempt
	return &BookingOperationClaim{Attempt: &copy, Acquired: true}, nil
}

func (f *fakeStore) SaveRescheduledAppointment(ctx context.Context, record RescheduledAppointmentRecord) (*Appointment, error) {
	if f.saveRescheduleErr != nil {
		return nil, f.saveRescheduleErr
	}
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
	f.setOperationResult(record.AttemptID, &BookingAttempt{
		ID: record.AttemptID, SalonID: record.Appointment.SalonID, Status: StatusRescheduled,
		OperationType: BookingActionReschedule, ProviderOutcome: ProviderOutcomeSucceeded,
		RetryPolicy: RetryPolicyNone, Reconciliation: ReconciliationNotRequired,
		Appointment: appointmentFromActionRef(f.appointment),
	})
	return appointmentFromActionRef(f.appointment), nil
}

func (f *fakeStore) SaveCancelledAppointment(ctx context.Context, record CancelledAppointmentRecord) (*Appointment, error) {
	if f.saveCancelErr != nil {
		return nil, f.saveCancelErr
	}
	f.cancelled = &record
	f.appointment.Status = StatusCancelled
	f.appointment.POSAppointmentVersion = record.POSBookingVersion
	f.setOperationResult(record.AttemptID, &BookingAttempt{
		ID: record.AttemptID, SalonID: record.Appointment.SalonID, Status: StatusCancelled,
		OperationType: BookingActionCancel, ProviderOutcome: ProviderOutcomeSucceeded,
		RetryPolicy: RetryPolicyNone, Reconciliation: ReconciliationNotRequired,
		Appointment: appointmentFromActionRef(f.appointment),
	})
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
	status := record.Status
	if status == "" {
		status = StatusFallbackPending
	}
	attempt := &BookingAttempt{
		ID:                 record.AttemptID,
		SalonID:            record.SalonID,
		Status:             status,
		ProviderOutcome:    record.ProviderOutcome,
		RetryPolicy:        record.RetryPolicy,
		Reconciliation:     record.Reconciliation,
		POSProvider:        record.Provider,
		POSBookingID:       firstNonEmpty(record.POSBookingID, record.Appointment.POSAppointmentID),
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
	}
	if record.NotificationType == NotificationTypeRescheduleFallback {
		attempt.OperationType = BookingActionReschedule
	} else {
		attempt.OperationType = BookingActionCancel
	}
	annotateBookingAttemptPolicy(attempt)
	f.setOperationResult(record.AttemptID, attempt)
	return attempt, nil
}

func (f *fakeStore) setOperationResult(attemptID string, result *BookingAttempt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, existing := range f.operations {
		if existing.ID != attemptID {
			continue
		}
		copy := *result
		copy.OperationKey = existing.OperationKey
		copy.RequestFingerprint = existing.RequestFingerprint
		copy.POSIdempotencyKey = existing.POSIdempotencyKey
		copy.Source = existing.Source
		copy.POSProvider = existing.POSProvider
		if copy.POSBookingID == "" {
			copy.POSBookingID = existing.POSBookingID
		}
		copy.RetryOfAttemptID = existing.RetryOfAttemptID
		copy.RetrySequence = existing.RetrySequence
		copy.AvailabilityQuoteID = existing.AvailabilityQuoteID
		copy.SlotFingerprint = existing.SlotFingerprint
		copy.ProviderFence = existing.ProviderFence
		copy.TargetAppointmentID = existing.TargetAppointmentID
		copy.TargetPOSBookingVersion = existing.TargetPOSBookingVersion
		copy.CustomerName = existing.CustomerName
		copy.CustomerPhone = existing.CustomerPhone
		copy.CustomerEmail = existing.CustomerEmail
		copy.ServiceID = existing.ServiceID
		copy.StaffID = existing.StaffID
		copy.StaffSelectionMode = existing.StaffSelectionMode
		copy.Segments = append([]BookingSegmentSnapshot(nil), existing.Segments...)
		copy.RequestedStartTime = existing.RequestedStartTime
		copy.RequestedEndTime = existing.RequestedEndTime
		copy.Notes = existing.Notes
		f.operations[key] = &copy
		return
	}
}

func (f *fakeStore) LatestTestBooking(ctx context.Context, salonID string, ownerUserID string) (*TestBookingRecord, error) {
	if f.latestTest == nil {
		return nil, pos.ErrNotFound
	}
	item := *f.latestTest
	return &item, nil
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

func (f *fakeStore) ListReconciliationTasks(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) ([]ReconciliationTask, error) {
	if err := f.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	items := make([]ReconciliationTask, 0, len(f.reconciliationTasks))
	for _, item := range f.reconciliationTasks {
		if item.Status == status {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeStore) ListReconciliationCandidates(ctx context.Context, salonID string, ownerUserID string, attemptID string) ([]ReconciliationCandidate, error) {
	if err := f.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	f.reconciliationCandidateAttemptID = attemptID
	return append([]ReconciliationCandidate(nil), f.reconciliationCandidates...), nil
}

func (f *fakeStore) ResolveReconciliationTask(ctx context.Context, salonID string, ownerUserID string, attemptID string, req ResolveReconciliationRequest) (*ReconciliationTask, error) {
	if err := f.EnsureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	f.reconciliationRequest = req
	for idx := range f.reconciliationTasks {
		if f.reconciliationTasks[idx].BookingAttemptID != attemptID {
			continue
		}
		f.reconciliationTasks[idx].Resolution = req.Action
		f.reconciliationTasks[idx].ResolutionNote = req.Note
		if req.Action == ReconciliationActionEscalated {
			f.reconciliationTasks[idx].Status = "escalated"
		} else {
			f.reconciliationTasks[idx].Status = "resolved"
		}
		item := f.reconciliationTasks[idx]
		return &item, nil
	}
	return nil, pos.ErrNotFound
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

func (f *fakeStore) ListCalendarEvents(ctx context.Context, salonID string, ownerUserID string, cursor CalendarEventCursor, limit int) ([]CalendarEvent, error) {
	f.calendarEventCursor = cursor
	f.calendarEventLimit = limit
	return f.calendarEvents, nil
}

func (f *fakeStore) UpsertCalendarAppointments(ctx context.Context, salonID string, provider string, fence pos.ProviderFence, items []CalendarAppointmentImport) (CalendarSyncSummary, error) {
	f.calendarImports = append([]CalendarAppointmentImport(nil), items...)
	f.calendarUpsertFence = fence
	return f.calendarSyncSummary, f.calendarUpsertErr
}

func (f *fakeStore) CreateCalendarSyncLog(ctx context.Context, salonID string, provider string) (string, error) {
	return "sync_log_1", nil
}

func (f *fakeStore) CompleteCalendarSyncLog(ctx context.Context, id string, status string, message string) error {
	f.calendarSyncLogStatus = status
	f.calendarSyncLogMessage = message
	return nil
}

func (f *fakeStore) LogPOSError(ctx context.Context, salonID string, provider string, operation string, err error) error {
	f.posErrorOperation = operation
	return nil
}

type fakeProvider struct {
	customer                *pos.Customer
	appointment             *pos.Appointment
	rescheduledAppointment  *pos.Appointment
	cancelledAppointment    *pos.Appointment
	searchCustomerErr       error
	createCustomerErr       error
	createBookingErr        error
	rescheduleErr           error
	cancelErr               error
	listAppointmentsErr     error
	availabilityErr         error
	store                   *fakeStore
	availabilitySlots       []pos.TimeSlot
	listAppointments        []pos.ListedAppointment
	listAppointmentsCursor  string
	listAppointmentInputs   []pos.AppointmentListInput
	lastAvailabilityInput   pos.AvailabilityInput
	lastCreateInput         pos.CreateAppointmentInput
	lastRescheduleInput     pos.RescheduleInput
	lastCancelInput         pos.CancelInput
	lastListInput           pos.AppointmentListInput
	searchSawPending        bool
	searchCustomerCalls     int
	createCustomerCalls     int
	availabilityCalls       int
	createAppointmentCalls  int
	rescheduleCalls         int
	cancelCalls             int
	listAppointmentCalls    int
	afterCreateAppointment  func()
	beforeCreateAppointment func()
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
	if f.beforeCreateAppointment != nil {
		f.beforeCreateAppointment()
	}
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
	f.listAppointmentInputs = append(f.listAppointmentInputs, input)
	if f.listAppointmentsErr != nil {
		return nil, f.listAppointmentsErr
	}
	return &pos.AppointmentListResult{Appointments: f.listAppointments, Cursor: f.listAppointmentsCursor}, nil
}

func (f *fakeProvider) Sync(ctx context.Context, salonID string) error {
	return nil
}
