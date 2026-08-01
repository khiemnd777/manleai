package pos_square

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestSquareStateRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	state, nonceHash, expiresAt, err := encodeState("salon-123", "secret", now)
	if err != nil {
		t.Fatalf("encode state failed: %v", err)
	}
	if !expiresAt.Equal(now.Add(squareOAuthStateTTL)) {
		t.Fatalf("unexpected expiry: %s", expiresAt)
	}
	salonID, decodedNonceHash, err := decodeState(state, "secret", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("decode state failed: %v", err)
	}
	if salonID != "salon-123" {
		t.Fatalf("unexpected salon id: %s", salonID)
	}
	if decodedNonceHash != nonceHash {
		t.Fatalf("unexpected nonce hash")
	}
}

func TestSquareStateRejectsWrongProvider(t *testing.T) {
	_, _, err := decodeState("bm90LXNxdWFyZToxMjM", "secret", time.Now().UTC())
	if err == nil {
		t.Fatalf("expected invalid state")
	}
}

func TestSquareStateRejectsTampering(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	state, _, _, err := encodeState("salon-123", "secret", now)
	if err != nil {
		t.Fatalf("encode state failed: %v", err)
	}
	_, _, err = decodeState(state+"x", "secret", now.Add(time.Minute))
	if err == nil {
		t.Fatalf("expected tampered state to fail")
	}
}

func TestSquareStateRejectsExpiredState(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	state, _, _, err := encodeState("salon-123", "secret", now)
	if err != nil {
		t.Fatalf("encode state failed: %v", err)
	}
	_, _, err = decodeState(state, "secret", now.Add(squareOAuthStateTTL+time.Second))
	if err == nil {
		t.Fatalf("expected expired state to fail")
	}
}

func TestBuildReadinessAllowsEnableWhenSquareIsBookingReady(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	connection := &pos.Connection{
		ID:         "connection_1",
		Status:     pos.StatusActive,
		LocationID: "loc_1",
		LastSyncAt: &now,
	}
	services := []pos.Service{
		{
			POSServiceID:      "svc_1",
			POSServiceVersion: 123,
			DurationMinutes:   45,
			AIBookable:        true,
			Active:            true,
			SyncStatus:        pos.SyncStatusSynced,
			POSLinked:         true,
		},
	}
	staff := []pos.StaffMember{
		{POSStaffID: "staff_1", AIBookable: true, Active: true, SyncStatus: pos.SyncStatusSynced, POSLinked: true},
	}
	periods := []pos.BusinessHourPeriod{
		{DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "17:00:00", Source: pos.BusinessHourSourceImported, Provider: pos.ProviderSquare},
	}

	confirmed := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, &booking.TestBookingRecord{
		AppointmentID:     "appointment_1",
		Status:            booking.StatusConfirmed,
		AppointmentStatus: booking.StatusConfirmed,
		POSBookingID:      "booking_1",
	}, nil, nil)
	if confirmed.CanTestBooking {
		t.Fatalf("test booking must fail closed without verified atomic no-overlap capability")
	}
	if !confirmed.CanCancelTestBooking {
		t.Fatalf("expected cancel test booking to be allowed")
	}
	if confirmed.CanEnableAIBooking {
		t.Fatalf("enable must remain blocked without verified atomic no-overlap capability")
	}
	if check := findReadinessCheck(confirmed.Checks, "atomic_slot_commit"); check == nil || check.Complete {
		t.Fatalf("atomic slot commit check=%#v, want incomplete", check)
	}

	cancelled := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, &booking.TestBookingRecord{
		AppointmentID:     "appointment_1",
		Status:            booking.StatusCancelled,
		AppointmentStatus: booking.StatusCancelled,
		POSBookingID:      "booking_1",
	}, nil, nil)
	if cancelled.CanCancelTestBooking {
		t.Fatalf("cancel should not be allowed after test booking is cancelled")
	}
	if cancelled.CanEnableAIBooking {
		t.Fatalf("cancelled test booking must not bypass atomic slot safety")
	}

	withoutTest := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, nil, nil, nil)
	if withoutTest.CanEnableAIBooking {
		t.Fatalf("enable must remain blocked without atomic slot safety")
	}

	pending := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, &booking.TestBookingRecord{
		Status:          booking.StatusPOSPending,
		ProviderOutcome: booking.ProviderOutcomeInFlight,
		RetryPolicy:     booking.RetryPolicyNone,
		Reconciliation:  booking.ReconciliationNotRequired,
	}, nil, nil)
	if pending.CanTestBooking || pending.CanCancelTestBooking {
		t.Fatalf("another test write must be blocked while the prior operation is in flight")
	}
	if pending.CanEnableAIBooking {
		t.Fatalf("an in-flight test must not bypass atomic slot safety")
	}
}

func TestBuildReadinessBuyerEvidenceEnablesOnlySingleCreate(t *testing.T) {
	now := time.Now().UTC()
	connection := &pos.Connection{ID: "connection-1", Status: pos.StatusActive, LocationID: "location-1", LastSyncAt: &now}
	services := []pos.Service{{Active: true, AIBookable: true, DurationMinutes: 45, SyncStatus: pos.SyncStatusSynced, POSServiceID: "service-1", POSServiceVersion: 1, POSLinked: true}}
	staff := []pos.StaffMember{{Active: true, AIBookable: true, SyncStatus: pos.SyncStatusSynced, POSStaffID: "staff-1", POSLinked: true}}
	periods := []pos.BusinessHourPeriod{{Provider: pos.ProviderSquare, Source: pos.BusinessHourSourceImported}}
	capability := pos.SchedulingCapabilityEvaluation{
		AutomaticSingleCreate: true, WritePermissionMode: pos.SchedulingWriteModeBuyer,
		EvidenceCurrent: true, ConnectionCapabilityVersion: 4, IntegrationConfigVersion: 6,
	}
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, nil, nil, nil, capability)
	if !readiness.CanEnableAIBooking || !readiness.CanTestBooking || !readiness.AutomaticSingleCreate {
		t.Fatalf("buyer single-create readiness=%#v", readiness)
	}
	if readiness.AutomaticReschedule || readiness.AutomaticPartyCreate || readiness.ResourceCapacity {
		t.Fatalf("unsupported capabilities became ready: %#v", readiness)
	}

	capability.WritePermissionMode = pos.SchedulingWriteModeSeller
	capability.AutomaticSingleCreate = false
	capability.ReconnectRequired = true
	readiness = buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, nil, nil, nil, capability)
	if readiness.CanEnableAIBooking || readiness.CanTestBooking || readiness.AutomaticSingleCreate || !readiness.ReconnectRequired {
		t.Fatalf("seller-write readiness did not fail closed: %#v", readiness)
	}
}

func TestBusinessReadinessProjectionOmitsTechnicalConnectionEvidence(t *testing.T) {
	response := businessReadinessResponse(&ReadinessStatus{
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		CanEnableAIBooking:  true,
		ServiceCount:        2,
		StaffCount:          3,
		BusinessHourCount:   4,
	})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal business readiness: %v", err)
	}
	for _, forbidden := range []string{"connection", "merchant", "location", "scope", "sync_logs", "latest_test_booking", "error_message"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("tenant-safe readiness leaked %q: %s", forbidden, encoded)
		}
	}
	if !response.ReadyForExternalNewWork || response.ServiceCount != 2 || response.StaffCount != 3 {
		t.Fatalf("unexpected projection: %+v", response)
	}
}

func TestBuildReadinessBlocksTestBookingWithoutBookableRecords(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, &pos.Connection{
		ID:         "connection_1",
		Status:     pos.StatusActive,
		LocationID: "loc_1",
		LastSyncAt: &now,
	}, []pos.Service{}, []pos.StaffMember{}, nil, nil, nil, nil)
	if readiness.CanTestBooking {
		t.Fatalf("test booking should be blocked without bookable services and staff")
	}
	if readiness.ServiceCount != 0 || readiness.StaffCount != 0 {
		t.Fatalf("unexpected counts: services=%d staff=%d", readiness.ServiceCount, readiness.StaffCount)
	}
}

func TestBuildReadinessReportsInternalAuthorityWithoutHidingSquareSetup(t *testing.T) {
	now := time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)
	connection, services, staff, periods := squareReadyPrerequisites(now)

	readiness := buildReadiness(false, booking.SchedulingAuthorityOwnerManual, connection, services, staff, periods, &booking.TestBookingRecord{
		AppointmentID:       "appointment_external",
		AppointmentStatus:   booking.StatusConfirmed,
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		POSBookingID:        "square_booking_external",
	}, nil, nil)

	if readiness.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual {
		t.Fatalf("scheduling authority = %q, want %q", readiness.SchedulingAuthority, booking.SchedulingAuthorityOwnerManual)
	}
	if readiness.CanTestBooking || readiness.CanEnableAIBooking {
		t.Fatalf("internal authority must block Square create/enable gates: %#v", readiness)
	}
	if readiness.canTestExternalProviderBooking() {
		t.Fatalf("new provider writes must fail closed without verified atomic no-overlap capability")
	}
	if !readiness.CanCancelTestBooking {
		t.Fatalf("existing external-origin test appointment must remain cancellable after an authority switch")
	}
	for _, key := range []string{"connect_square", "select_location", "sync_services", "sync_staff", "sync_business_hours", "booking_writes"} {
		check := findReadinessCheck(readiness.Checks, key)
		if check == nil || !check.Complete {
			t.Fatalf("provider setup check %q = %#v, want complete", key, check)
		}
	}
	target := buildSchedulingTargetReadiness(pos.ProviderSquare, readiness)
	if target.Ready || !target.AvailabilityReady || target.ExecutionReady || len(target.ExecutionBlockers) == 0 {
		t.Fatalf("external availability may remain ready while unsafe execution is blocked: %#v", target)
	}
	wrongAdapter := buildSchedulingTargetReadiness("another-provider", readiness)
	if wrongAdapter.Ready || len(wrongAdapter.Blockers) == 0 || wrongAdapter.Blockers[0].Code != "EXTERNAL_PROVIDER_SELECT_POS_ADAPTER" {
		t.Fatalf("external target readiness must require the selected adapter: %#v", wrongAdapter)
	}
}

func TestExternalTargetReadinessSeparatesAvailabilityFromBookingExecution(t *testing.T) {
	now := time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)
	connection, services, staff, periods := squareReadyPrerequisites(now)
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, nil, &pos.POSErrorRecord{
		ErrorCode: pos.ErrorPermissionDenied, ErrorMessage: "provider write permission denied", CreatedAt: now,
	}, nil)
	target := buildSchedulingTargetReadiness(pos.ProviderSquare, readiness)
	if !target.AvailabilityReady || target.ExecutionReady || target.Ready || len(target.AvailabilityBlockers) != 0 {
		t.Fatalf("capability readiness = %#v", target)
	}
	if len(target.ExecutionBlockers) == 0 || target.ExecutionBlockers[0].Code != "EXTERNAL_PROVIDER_BOOKING_WRITES" {
		t.Fatalf("execution blockers = %#v", target.ExecutionBlockers)
	}
}

func TestTestBookingRequestMappersForwardSafeRetryLineage(t *testing.T) {
	const createAttemptID = "00000000-0000-4000-8000-000000000041"
	createRequest := normalizeTestBookingRequest("salon_1", TestBookingRequest{
		OperationKey:     " create-retry ",
		RetryOfAttemptID: " " + createAttemptID + " ",
	})
	create := testBookingCreateRequest(createRequest)
	if create.RetryOfAttemptID != createAttemptID {
		t.Fatalf("create retry_of_attempt_id = %q, want %q", create.RetryOfAttemptID, createAttemptID)
	}
	if create.Source != booking.SourceSquareTestBooking {
		t.Fatalf("create source = %q, want %q", create.Source, booking.SourceSquareTestBooking)
	}

	const cancelAttemptID = "00000000-0000-4000-8000-000000000051"
	cancelRequest := normalizeCancelTestBookingRequest("salon_1", CancelTestBookingRequest{
		OperationKey:     " cancel-retry ",
		RetryOfAttemptID: " " + cancelAttemptID + " ",
	})
	cancel := testBookingCancelRequest(cancelRequest)
	if cancel.RetryOfAttemptID != cancelAttemptID {
		t.Fatalf("cancel retry_of_attempt_id = %q, want %q", cancel.RetryOfAttemptID, cancelAttemptID)
	}
	if cancel.Source != booking.SourceSquareTestBooking {
		t.Fatalf("cancel source = %q, want %q", cancel.Source, booking.SourceSquareTestBooking)
	}
}

func TestCreateTestBookingReplaysBeforeReadinessGate(t *testing.T) {
	startTime := time.Date(2026, 7, 15, 16, 0, 0, 0, time.UTC)
	replayed := &booking.BookingAttempt{
		ID:                 "attempt_1",
		Status:             booking.StatusConfirmed,
		OperationType:      booking.BookingActionBook,
		POSBookingID:       "square_booking_1",
		Appointment:        &booking.Appointment{ID: "appointment_1", Status: booking.StatusConfirmed},
		OperationKey:       "square-test-create-replay",
		CustomerName:       "Linh Tran",
		CustomerPhone:      "+13125550101",
		ServiceID:          "service_1",
		StaffID:            "staff_1",
		RequestedStartTime: startTime,
	}
	operations := &fakeBookingOperationService{
		currentAuthority:    booking.SchedulingAuthorityOwnerManual,
		replayCreateAttempt: replayed,
		replayCreateFound:   true,
	}
	readinessCalls := 0
	service := &Service{
		bookingService: operations,
		readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
			readinessCalls++
			return &ReadinessStatus{CanTestBooking: false}, nil
		},
	}

	response, err := service.CreateTestBooking(context.Background(), "salon_1", "owner_1", TestBookingRequest{
		OperationKey:        "square-test-create-replay",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000040",
		SlotFingerprint:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CustomerName:        "Linh Tran",
		CustomerPhone:       "+13125550101",
		ServiceID:           "service_1",
		StaffID:             "staff_1",
		StartTime:           startTime,
	})
	if err != nil {
		t.Fatalf("CreateTestBooking replay returned error: %v", err)
	}
	if response == nil || response.BookingAttempt != replayed || response.Appointment != replayed.Appointment {
		t.Fatalf("CreateTestBooking response = %#v, want hydrated replay", response)
	}
	if operations.replayCreateCalls != 1 || operations.createDispatchAuthorityCalls != 0 || operations.createCalls != 0 || readinessCalls != 1 {
		t.Fatalf("calls replay=%d dispatch_authority=%d create=%d readiness=%d, want 1/0/0/1", operations.replayCreateCalls, operations.createDispatchAuthorityCalls, operations.createCalls, readinessCalls)
	}
}

func TestCreateTestBookingNewOperationStillHonorsReadinessGate(t *testing.T) {
	operations := &fakeBookingOperationService{currentAuthority: booking.SchedulingAuthorityExternalProvider}
	service := &Service{
		bookingService: operations,
		readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
			return &ReadinessStatus{CanTestBooking: false}, nil
		},
	}

	response, err := service.CreateTestBooking(context.Background(), "salon_1", "owner_1", TestBookingRequest{
		OperationKey:        "square-test-create-new",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000040",
		SlotFingerprint:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ServiceID:           "service_1",
		StaffID:             "staff_1",
		StartTime:           time.Date(2026, 7, 15, 16, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrReadinessGate) || response != nil {
		t.Fatalf("CreateTestBooking response=%#v err=%v, want readiness gate", response, err)
	}
	if operations.replayCreateCalls != 1 || operations.createDispatchAuthorityCalls != 1 || operations.createCalls != 0 {
		t.Fatalf("calls replay=%d dispatch_authority=%d create=%d, want 1/1/0", operations.replayCreateCalls, operations.createDispatchAuthorityCalls, operations.createCalls)
	}
}

func TestCreateTestBookingRejectsNewOperationUnderInternalAuthorityBeforeReadinessOrCreate(t *testing.T) {
	for _, authority := range []string{
		booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
	} {
		t.Run(authority, func(t *testing.T) {
			operations := &fakeBookingOperationService{currentAuthority: authority}
			readinessCalls := 0
			service := &Service{
				bookingService: operations,
				readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
					readinessCalls++
					return &ReadinessStatus{CanTestBooking: true}, nil
				},
			}

			response, err := service.CreateTestBooking(context.Background(), "salon_1", "owner_1", TestBookingRequest{
				OperationKey:        "square-test-create-internal",
				AvailabilityQuoteID: "00000000-0000-4000-8000-000000000042",
				SlotFingerprint:     "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				ServiceID:           "service_3",
				StaffID:             "staff_3",
				StartTime:           time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC),
			})
			if response != nil || !errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
				t.Fatalf("CreateTestBooking response=%#v err=%v, want authority-not-ready", response, err)
			}
			if operations.replayCreateCalls != 1 || operations.createDispatchAuthorityCalls != 1 || operations.createCalls != 0 || readinessCalls != 0 {
				t.Fatalf("calls replay=%d dispatch_authority=%d create=%d readiness=%d, want 1/1/0/0", operations.replayCreateCalls, operations.createDispatchAuthorityCalls, operations.createCalls, readinessCalls)
			}
		})
	}
}

func TestCreateTestBookingAllowsExternalOriginRetryAfterCurrentAuthoritySwitch(t *testing.T) {
	const retryAttemptID = "00000000-0000-4000-8000-000000000043"
	attempt := &booking.BookingAttempt{ID: "attempt-retry-external", Status: booking.StatusConfirmed}
	operations := &fakeBookingOperationService{
		currentAuthority:        booking.SchedulingAuthorityOwnerManual,
		createDispatchAuthority: booking.SchedulingAuthorityExternalProvider,
		createAttempt:           attempt,
	}
	readinessCalls := 0
	service := &Service{
		bookingService: operations,
		readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
			readinessCalls++
			return &ReadinessStatus{
				SchedulingAuthority:    booking.SchedulingAuthorityOwnerManual,
				CanTestBooking:         false,
				providerCanTestBooking: true,
			}, nil
		},
	}

	response, err := service.CreateTestBooking(context.Background(), "salon_1", "owner_1", TestBookingRequest{
		OperationKey:        "square-test-create-safe-retry",
		RetryOfAttemptID:    retryAttemptID,
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000044",
		SlotFingerprint:     "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ServiceID:           "service_external",
		StaffID:             "staff_external",
		StartTime:           time.Date(2026, 7, 18, 14, 0, 0, 0, time.UTC),
	})
	if err != nil || response == nil || response.BookingAttempt != attempt {
		t.Fatalf("CreateTestBooking response=%#v err=%v, want external-origin retry", response, err)
	}
	if operations.replayCreateCalls != 1 || operations.createDispatchAuthorityCalls != 1 || operations.createCalls != 1 || readinessCalls != 2 {
		t.Fatalf("calls replay=%d dispatch_authority=%d create=%d readiness=%d, want 1/1/1/2", operations.replayCreateCalls, operations.createDispatchAuthorityCalls, operations.createCalls, readinessCalls)
	}
	if operations.createDispatchOperationKey != "square-test-create-safe-retry" || operations.createDispatchRetryAttemptID != retryAttemptID {
		t.Fatalf("dispatch lineage operation=%q retry=%q", operations.createDispatchOperationKey, operations.createDispatchRetryAttemptID)
	}
	if operations.createRequest.OperationKey != "square-test-create-safe-retry" || operations.createRequest.RetryOfAttemptID != retryAttemptID {
		t.Fatalf("create lineage = %#v", operations.createRequest)
	}
}

func TestCreateTestBookingRejectsInvalidPersistedLineageBeforeReadinessOrCreate(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{name: "conflicting operation and retry origins", wantErr: booking.ErrOperationConflict},
		{name: "cross tenant retry", wantErr: pos.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operations := &fakeBookingOperationService{
				currentAuthority:           booking.SchedulingAuthorityOwnerManual,
				createDispatchAuthorityErr: test.wantErr,
			}
			readinessCalls := 0
			service := &Service{
				bookingService: operations,
				readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
					readinessCalls++
					return &ReadinessStatus{providerCanTestBooking: true}, nil
				},
			}

			response, err := service.CreateTestBooking(context.Background(), "salon_1", "owner_1", TestBookingRequest{
				OperationKey:        "square-test-create-invalid-lineage",
				RetryOfAttemptID:    "00000000-0000-4000-8000-000000000045",
				AvailabilityQuoteID: "00000000-0000-4000-8000-000000000046",
				SlotFingerprint:     "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				ServiceID:           "service_invalid",
				StaffID:             "staff_invalid",
				StartTime:           time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC),
			})
			if response != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateTestBooking response=%#v err=%v, want %v", response, err, test.wantErr)
			}
			if operations.replayCreateCalls != 1 || operations.createDispatchAuthorityCalls != 1 || operations.createCalls != 0 || readinessCalls != 0 {
				t.Fatalf("calls replay=%d dispatch_authority=%d create=%d readiness=%d, want 1/1/0/0", operations.replayCreateCalls, operations.createDispatchAuthorityCalls, operations.createCalls, readinessCalls)
			}
		})
	}
}

func TestNewServiceAcceptsBookingOperationFacadeAndPropagatesAuthorityError(t *testing.T) {
	operations := &fakeBookingOperationService{
		currentAuthority: booking.SchedulingAuthorityExternalProvider,
		createErr:        booking.ErrSchedulingAuthorityNotReady,
	}
	service := NewService(nil, nil, "state-secret", operations)
	service.readinessLoader = func(context.Context, string, string) (*ReadinessStatus, error) {
		return &ReadinessStatus{CanTestBooking: true}, nil
	}

	response, err := service.CreateTestBooking(context.Background(), "salon_1", "owner_1", TestBookingRequest{
		OperationKey:        "authority-facade-create",
		AvailabilityQuoteID: "00000000-0000-4000-8000-000000000041",
		SlotFingerprint:     "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ServiceID:           "service_2",
		StaffID:             "staff_2",
		StartTime:           time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC),
	})
	if response != nil || !errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
		t.Fatalf("CreateTestBooking response=%#v err=%v, want authority-not-ready", response, err)
	}
	if operations.replayCreateCalls != 1 || operations.createDispatchAuthorityCalls != 1 || operations.createCalls != 1 {
		t.Fatalf("calls replay=%d dispatch_authority=%d create=%d, want 1/1/1", operations.replayCreateCalls, operations.createDispatchAuthorityCalls, operations.createCalls)
	}
}

func TestCancelTestBookingUsesBookingOperationFacade(t *testing.T) {
	operations := &fakeBookingOperationService{
		latest: &booking.TestBookingRecord{
			AppointmentID:     "appointment_2",
			AppointmentStatus: booking.StatusConfirmed,
		},
		cancelErr: booking.ErrSchedulingAuthorityNotReady,
	}
	service := NewService(nil, nil, "state-secret", operations)

	response, err := service.CancelTestBooking(context.Background(), "salon_2", "owner_2", CancelTestBookingRequest{
		OperationKey:  "authority-facade-cancel",
		AppointmentID: "appointment_2",
	})
	if response != nil || !errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
		t.Fatalf("CancelTestBooking response=%#v err=%v, want authority-not-ready", response, err)
	}
	if operations.replayCancelCalls != 1 || operations.latestCalls != 1 || operations.cancelCalls != 1 {
		t.Fatalf("calls replay=%d latest=%d cancel=%d, want 1/1/1", operations.replayCancelCalls, operations.latestCalls, operations.cancelCalls)
	}
}

func TestCancelTestBookingReplaysBeforeLatestCancelledGate(t *testing.T) {
	appointment := &booking.Appointment{ID: "appointment_1", Status: booking.StatusCancelled}
	attempt := &booking.BookingAttempt{ID: "attempt_cancel_1", Status: booking.StatusCancelled, OperationType: booking.BookingActionCancel}
	operations := &fakeBookingOperationService{
		latest: &booking.TestBookingRecord{
			AppointmentID:     appointment.ID,
			AppointmentStatus: booking.StatusCancelled,
		},
		replayCancelAppointment: appointment,
		replayCancelAttempt:     attempt,
		replayCancelFound:       true,
	}
	service := &Service{
		bookingService: operations,
		readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
			return &ReadinessStatus{LatestTestBooking: operations.latest}, nil
		},
	}

	response, err := service.CancelTestBooking(context.Background(), "salon_1", "owner_1", CancelTestBookingRequest{
		OperationKey: "square-test-cancel-replay",
		Reason:       "AI booking readiness test cleanup",
	})
	if err != nil {
		t.Fatalf("CancelTestBooking replay returned error: %v", err)
	}
	if response == nil || response.Appointment != appointment || response.BookingAttempt != attempt {
		t.Fatalf("CancelTestBooking response = %#v, want hydrated replay", response)
	}
	if operations.latestCalls != 1 || operations.replayCancelCalls != 1 || operations.cancelCalls != 0 {
		t.Fatalf("calls latest=%d replay=%d cancel=%d, want 1/1/0", operations.latestCalls, operations.replayCancelCalls, operations.cancelCalls)
	}
}

func TestCancelTestBookingExplicitTargetReplaysBeforeLatestRead(t *testing.T) {
	appointment := &booking.Appointment{ID: "appointment_1", Status: booking.StatusCancelled}
	operations := &fakeBookingOperationService{
		latestErr:               errors.New("latest state unavailable"),
		replayCancelAppointment: appointment,
		replayCancelFound:       true,
	}
	service := &Service{
		bookingService: operations,
		readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
			return &ReadinessStatus{}, nil
		},
	}

	response, err := service.CancelTestBooking(context.Background(), "salon_1", "owner_1", CancelTestBookingRequest{
		OperationKey:  "square-test-cancel-explicit-replay",
		AppointmentID: appointment.ID,
		Reason:        "AI booking readiness test cleanup",
	})
	if err != nil || response == nil || response.Appointment != appointment {
		t.Fatalf("CancelTestBooking response=%#v err=%v, want explicit replay", response, err)
	}
	if operations.latestCalls != 0 || operations.replayCancelCalls != 1 || operations.cancelCalls != 0 {
		t.Fatalf("calls latest=%d replay=%d cancel=%d, want 0/1/0", operations.latestCalls, operations.replayCancelCalls, operations.cancelCalls)
	}
}

func TestCancelTestBookingAllowsExistingExternalOriginAfterCurrentAuthoritySwitch(t *testing.T) {
	appointment := &booking.Appointment{ID: "appointment_external", Status: booking.StatusCancelled}
	operations := &fakeBookingOperationService{
		currentAuthority: booking.SchedulingAuthorityOwnerManual,
		latest: &booking.TestBookingRecord{
			AppointmentID:     appointment.ID,
			AppointmentStatus: booking.StatusConfirmed,
		},
		cancelAppointment: appointment,
	}
	service := &Service{
		bookingService: operations,
		readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
			return &ReadinessStatus{SchedulingAuthority: booking.SchedulingAuthorityOwnerManual}, nil
		},
	}

	response, err := service.CancelTestBooking(context.Background(), "salon_1", "owner_1", CancelTestBookingRequest{
		OperationKey:  "square-test-cancel-external-origin",
		AppointmentID: appointment.ID,
		Reason:        "AI booking readiness test cleanup",
	})
	if err != nil || response == nil || response.Appointment != appointment {
		t.Fatalf("CancelTestBooking response=%#v err=%v, want origin-routed cancellation", response, err)
	}
	if operations.replayCancelCalls != 1 || operations.latestCalls != 1 || operations.cancelCalls != 1 || operations.currentAuthorityCalls != 0 {
		t.Fatalf("calls replay=%d latest=%d cancel=%d authority=%d, want 1/1/1/0", operations.replayCancelCalls, operations.latestCalls, operations.cancelCalls, operations.currentAuthorityCalls)
	}
}

func TestEnableAIBookingRejectsInternalAuthority(t *testing.T) {
	for _, authority := range []string{
		booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
	} {
		t.Run(authority, func(t *testing.T) {
			service := &Service{
				bookingService: &fakeBookingOperationService{currentAuthority: authority},
				readinessLoader: func(context.Context, string, string) (*ReadinessStatus, error) {
					return &ReadinessStatus{SchedulingAuthority: authority}, nil
				},
			}

			response, err := service.EnableAIBooking(context.Background(), "salon_1", "owner_1")
			if response != nil || !errors.Is(err, booking.ErrSchedulingAuthorityNotReady) {
				t.Fatalf("EnableAIBooking response=%#v err=%v, want authority-not-ready", response, err)
			}
		})
	}
}

func TestBuildReadinessBlocksEnableWhenCreateBookingPermissionDenied(t *testing.T) {
	now := time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC)
	connection, services, staff, periods := squareReadyPrerequisites(now)
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, nil, &pos.POSErrorRecord{
		ErrorCode:    pos.ErrorPermissionDenied,
		ErrorMessage: "square INSUFFICIENT_SCOPES: The application is not allowed to update this field once written since it does not have all the required permissions: APPOINTMENTS_ALL_READ, APPOINTMENTS_ALL_WRITE.",
		CreatedAt:    now,
	}, nil)

	if readiness.CanTestBooking {
		t.Fatalf("test booking must remain blocked by atomic slot safety")
	}
	if readiness.CanEnableAIBooking {
		t.Fatalf("enable should be blocked while Square create-booking writes are rejected")
	}
	if !readiness.BookingWriteBlocked {
		t.Fatalf("booking write blocker was not surfaced")
	}
	if readiness.BookingWriteBlockedCode != pos.ErrorPermissionDenied {
		t.Fatalf("blocker code = %s, want %s", readiness.BookingWriteBlockedCode, pos.ErrorPermissionDenied)
	}
	if readiness.BookingWriteBlockedReason != pos.SafeErrorMessage(pos.ErrorPermissionDenied) {
		t.Fatalf("blocker reason = %q, want sanitized stable copy", readiness.BookingWriteBlockedReason)
	}
	if readiness.BookingWriteBlockedAt == nil || !readiness.BookingWriteBlockedAt.Equal(now) {
		t.Fatalf("blocker timestamp = %#v, want %s", readiness.BookingWriteBlockedAt, now)
	}
	writeCheck := findReadinessCheck(readiness.Checks, "booking_writes")
	if writeCheck == nil || writeCheck.Complete {
		t.Fatalf("booking_writes check = %#v, want incomplete", writeCheck)
	}
}

func TestBuildReadinessClearsCreateBookingBlockerAfterLaterSuccessfulTest(t *testing.T) {
	now := time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC)
	connection, services, staff, periods := squareReadyPrerequisites(now)
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, connection, services, staff, periods, &booking.TestBookingRecord{
		AppointmentID:     "appointment_1",
		Status:            booking.StatusConfirmed,
		AppointmentStatus: booking.StatusConfirmed,
		POSBookingID:      "booking_1",
		CreatedAt:         now.Add(time.Hour),
	}, &pos.POSErrorRecord{
		ErrorCode:    pos.ErrorWriteUnsupported,
		ErrorMessage: "untrusted provider detail must not be exposed",
		CreatedAt:    now,
	}, nil)

	if readiness.BookingWriteBlocked {
		t.Fatalf("later successful test booking should clear stale create-booking blocker")
	}
	if readiness.CanEnableAIBooking {
		t.Fatalf("successful permission test must not bypass atomic slot safety")
	}
	if check := findReadinessCheck(readiness.Checks, "atomic_slot_commit"); check == nil || check.Complete {
		t.Fatalf("atomic slot commit check=%#v, want incomplete", check)
	}
}

func TestBuildReadinessSurfacesSquareAppointmentChangeSubscriptionBlocker(t *testing.T) {
	now := time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC)
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, nil, nil, nil, nil, nil, nil, &pos.POSErrorRecord{
		ErrorCode:    pos.ErrorPermissionDenied,
		ErrorMessage: "square FORBIDDEN: Merchant subscription does not support write operations.",
		CreatedAt:    now,
	})

	if !readiness.AppointmentChangeWriteBlocked {
		t.Fatalf("appointment change blocker was not surfaced")
	}
	if readiness.AppointmentChangeWriteBlockedCode != pos.ErrorWriteUnsupported {
		t.Fatalf("blocker code = %s, want %s", readiness.AppointmentChangeWriteBlockedCode, pos.ErrorWriteUnsupported)
	}
	if readiness.AppointmentChangeWriteBlockedReason != pos.SafeErrorMessage(pos.ErrorWriteUnsupported) {
		t.Fatalf("blocker reason = %q, want sanitized stable copy", readiness.AppointmentChangeWriteBlockedReason)
	}
	if readiness.AppointmentChangeWriteBlockedAt == nil || !readiness.AppointmentChangeWriteBlockedAt.Equal(now) {
		t.Fatalf("blocker timestamp = %#v, want %s", readiness.AppointmentChangeWriteBlockedAt, now)
	}
}

func TestBuildReadinessDoesNotTreatInsufficientScopesAsSubscriptionBlocker(t *testing.T) {
	readiness := buildReadiness(false, booking.SchedulingAuthorityExternalProvider, nil, nil, nil, nil, nil, nil, &pos.POSErrorRecord{
		ErrorCode:    pos.ErrorPermissionDenied,
		ErrorMessage: "square INSUFFICIENT_SCOPES: The application is not allowed to update this field once written since it does not have all the required permissions: APPOINTMENTS_ALL_READ, APPOINTMENTS_ALL_WRITE.",
		CreatedAt:    time.Date(2026, 7, 6, 17, 0, 0, 0, time.UTC),
	})

	if readiness.AppointmentChangeWriteBlocked {
		t.Fatalf("insufficient scopes should not be treated as Square subscription blocker")
	}
}

func squareReadyPrerequisites(now time.Time) (*pos.Connection, []pos.Service, []pos.StaffMember, []pos.BusinessHourPeriod) {
	connection := &pos.Connection{
		ID:         "connection_1",
		Status:     pos.StatusActive,
		LocationID: "loc_1",
		LastSyncAt: &now,
	}
	services := []pos.Service{
		{
			POSServiceID:      "svc_1",
			POSServiceVersion: 123,
			DurationMinutes:   45,
			AIBookable:        true,
			Active:            true,
			SyncStatus:        pos.SyncStatusSynced,
			POSLinked:         true,
		},
	}
	staff := []pos.StaffMember{
		{POSStaffID: "staff_1", AIBookable: true, Active: true, SyncStatus: pos.SyncStatusSynced, POSLinked: true},
	}
	periods := []pos.BusinessHourPeriod{
		{DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "17:00:00", Source: pos.BusinessHourSourceImported, Provider: pos.ProviderSquare},
	}
	return connection, services, staff, periods
}

func findReadinessCheck(checks []ReadinessCheck, key string) *ReadinessCheck {
	for i := range checks {
		if checks[i].Key == key {
			return &checks[i]
		}
	}
	return nil
}

type fakeBookingOperationService struct {
	currentAuthority      string
	currentAuthorityErr   error
	currentAuthorityCalls int

	createDispatchAuthority      string
	createDispatchAuthorityErr   error
	createDispatchAuthorityCalls int
	createDispatchOperationKey   string
	createDispatchRetryAttemptID string

	replayCreateAttempt *booking.BookingAttempt
	replayCreateFound   bool
	replayCreateErr     error
	replayCreateCalls   int
	createAttempt       *booking.BookingAttempt
	createErr           error
	createCalls         int
	createRequest       booking.CreateBookingRequest

	latest      *booking.TestBookingRecord
	latestErr   error
	latestCalls int

	replayCancelAppointment *booking.Appointment
	replayCancelAttempt     *booking.BookingAttempt
	replayCancelFound       bool
	replayCancelErr         error
	replayCancelCalls       int
	replayCancelTarget      string
	cancelAppointment       *booking.Appointment
	cancelAttempt           *booking.BookingAttempt
	cancelErr               error
	cancelCalls             int
}

func (f *fakeBookingOperationService) CurrentSchedulingAuthority(context.Context, string, string) (string, error) {
	f.currentAuthorityCalls++
	authority := f.currentAuthority
	if authority == "" {
		authority = booking.SchedulingAuthorityExternalProvider
	}
	return authority, f.currentAuthorityErr
}

func (f *fakeBookingOperationService) ResolveCreateSchedulingAuthority(_ context.Context, _ string, _ string, operationKey string, retryOfAttemptID string) (string, error) {
	f.createDispatchAuthorityCalls++
	f.createDispatchOperationKey = operationKey
	f.createDispatchRetryAttemptID = retryOfAttemptID
	authority := f.createDispatchAuthority
	if authority == "" {
		authority = f.currentAuthority
	}
	if authority == "" {
		authority = booking.SchedulingAuthorityExternalProvider
	}
	return authority, f.createDispatchAuthorityErr
}

func (f *fakeBookingOperationService) Create(_ context.Context, _ string, _ string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.createCalls++
	f.createRequest = req
	return f.createAttempt, f.createErr
}

func (f *fakeBookingOperationService) ReplayCreate(context.Context, string, string, booking.CreateBookingRequest) (*booking.BookingAttempt, bool, error) {
	f.replayCreateCalls++
	return f.replayCreateAttempt, f.replayCreateFound, f.replayCreateErr
}

func (f *fakeBookingOperationService) Cancel(context.Context, string, string, string, booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, error) {
	f.cancelCalls++
	return f.cancelAppointment, f.cancelAttempt, f.cancelErr
}

func (f *fakeBookingOperationService) ReplayCancel(_ context.Context, _ string, _ string, appointmentID string, _ booking.CancelRequest) (*booking.Appointment, *booking.BookingAttempt, bool, error) {
	f.replayCancelCalls++
	f.replayCancelTarget = appointmentID
	return f.replayCancelAppointment, f.replayCancelAttempt, f.replayCancelFound, f.replayCancelErr
}

func (f *fakeBookingOperationService) LatestTestBooking(context.Context, string, string) (*booking.TestBookingRecord, error) {
	f.latestCalls++
	return f.latest, f.latestErr
}
