package booking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestReconciliationCandidateMatchesRescheduleExactRequestedRange(t *testing.T) {
	requestedStart := time.Date(2026, time.July, 20, 15, 0, 0, 0, time.UTC)
	requestedEnd := requestedStart.Add(45 * time.Minute)
	attempt := reconciliationAttemptMatchRecord{
		OperationType:           BookingActionReschedule,
		TargetAppointmentID:     "appointment-1",
		TargetPOSBookingVersion: 4,
		StartTime:               requestedStart,
		EndTime:                 requestedEnd,
	}
	candidate := verifiedReconciliationCandidate{
		ReconciliationCandidate: ReconciliationCandidate{
			AppointmentID:              "appointment-1",
			ProviderAppointmentVersion: 5,
			StartTime:                  requestedStart,
			EndTime:                    requestedEnd,
		},
		AppointmentStatus: StatusRescheduled,
	}

	if !reconciliationCandidateMatchesAttempt(attempt, candidate) {
		t.Fatal("expected exact reschedule mirror to match")
	}
	candidate.ProviderAppointmentVersion = attempt.TargetPOSBookingVersion
	if reconciliationCandidateMatchesAttempt(attempt, candidate) {
		t.Fatal("provider mirror at the pre-mutation version must not prove a reschedule")
	}
	candidate.ProviderAppointmentVersion++
	candidate.StartTime = requestedStart.Add(time.Hour)
	candidate.EndTime = requestedEnd.Add(time.Hour)
	if reconciliationCandidateMatchesAttempt(attempt, candidate) {
		t.Fatal("different provider time must not resolve the original reschedule request")
	}
}

func TestReconciliationNotCreatedTreatsTargetBookingIDByOperation(t *testing.T) {
	if !reconciliationNotCreatedBlockedByAttempt(StatusFallbackPending, BookingActionBook, "new-provider-booking") {
		t.Fatal("a provider booking ID must block not_created for a create operation")
	}
	for _, operation := range []string{BookingActionReschedule, BookingActionCancel} {
		if reconciliationNotCreatedBlockedByAttempt(StatusFallbackPending, operation, "existing-target-booking") {
			t.Fatalf("the existing target booking ID must not block not_created for %s", operation)
		}
	}
	if !reconciliationNotCreatedBlockedByAttempt(StatusProviderPending, BookingActionCancel, "existing-target-booking") {
		t.Fatal("provider_pending must remain blocked until provider state is known")
	}
}

func TestBookingLeaseExpiryPolicyDistinguishesCrashWindows(t *testing.T) {
	t.Run("claim persisted before provider dispatch", func(t *testing.T) {
		policy := bookingLeaseExpiryPolicyFor(ProviderOutcomeNotStarted)
		if policy.ProviderOutcome != ProviderOutcomeFailed || policy.RetryPolicy != RetryPolicySafe || policy.Reconciliation != ReconciliationNotRequired {
			t.Fatalf("pre-dispatch policy = %#v, want failed/safe/not_required", policy)
		}
		if policy.NotificationDedupePrefix != "booking-result:" || policy.FailurePhase != "pre_dispatch" {
			t.Fatalf("pre-dispatch notification policy = %#v, want booking-result/pre_dispatch", policy)
		}
	})

	t.Run("provider dispatch may have started", func(t *testing.T) {
		policy := bookingLeaseExpiryPolicyFor(ProviderOutcomeInFlight)
		if policy.ProviderOutcome != ProviderOutcomeUnknown || policy.RetryPolicy != RetryPolicyBlocked || policy.Reconciliation != ReconciliationRequired {
			t.Fatalf("in-flight policy = %#v, want unknown/blocked/required", policy)
		}
		if policy.NotificationDedupePrefix != "booking-reconciliation:" || policy.FailurePhase != "in_flight" {
			t.Fatalf("in-flight notification policy = %#v, want booking-reconciliation/in_flight", policy)
		}
	})
}

func TestAnnotateBookingAttemptPolicyRequiresAuthoritativePrerequisites(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		attempt    BookingAttempt
		wantRetry  bool
		wantReason string
	}{
		{
			name: "authoritative prerequisites current",
			attempt: BookingAttempt{Status: StatusFallbackPending, RetryPolicy: RetryPolicySafe,
				retryPrerequisitesChecked: true, retryPrerequisitesCurrent: true},
			wantRetry: true,
		},
		{
			name: "stale catalog blocks retry",
			attempt: BookingAttempt{Status: StatusFallbackPending, RetryPolicy: RetryPolicySafe,
				retryPrerequisitesChecked: true, retryPrerequisitesReason: "catalog changed"},
			wantReason: "catalog changed",
		},
		{
			name: "superseded attempt never retries",
			attempt: BookingAttempt{Status: StatusFallbackPending, RetryPolicy: RetryPolicySafe,
				SupersededAt: &now, retryPrerequisitesChecked: true, retryPrerequisitesCurrent: true},
			wantReason: "superseded",
		},
		{
			name: "unknown provider outcome keeps reconciliation priority",
			attempt: BookingAttempt{Status: StatusFallbackPending, RetryPolicy: RetryPolicyBlocked,
				retryPrerequisitesChecked: true, retryPrerequisitesCurrent: true},
			wantReason: "Reconcile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annotateBookingAttemptPolicy(&tt.attempt)
			if tt.attempt.CanRetry != tt.wantRetry {
				t.Fatalf("CanRetry = %t, want %t", tt.attempt.CanRetry, tt.wantRetry)
			}
			if tt.wantReason != "" && !strings.Contains(tt.attempt.RetryBlockedReason, tt.wantReason) {
				t.Fatalf("RetryBlockedReason = %q, want substring %q", tt.attempt.RetryBlockedReason, tt.wantReason)
			}
		})
	}
}

func TestRepositorySafetyQueriesKeepBoundedLeaseAndAttemptBeforeTaskLockOrder(t *testing.T) {
	sourceBytes, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository source: %v", err)
	}
	source := string(sourceBytes)
	if strings.Contains(source, `"booking-reconciliation:"+attemptID`) {
		t.Fatal("per-attempt reconciliation advisory lock must not precede row locks")
	}
	leaseStart := strings.Index(source, "func (r *Repository) expireBookingOperationLeases")
	leaseEnd := strings.Index(source[leaseStart:], "func (r *Repository) expireBookingOperationLeaseCandidate")
	if leaseStart < 0 || leaseEnd < 0 {
		t.Fatal("could not locate lease expiry implementation")
	}
	leaseSource := source[leaseStart : leaseStart+leaseEnd]
	for _, fragment := range []string{"r.db.QueryContext", "LIMIT $5", "expireBookingOperationLeaseCandidate"} {
		if !strings.Contains(leaseSource, fragment) {
			t.Fatalf("lease sweep is missing bounded candidate fragment %q", fragment)
		}
	}
	if strings.Contains(leaseSource, "FOR UPDATE") {
		t.Fatal("lease candidate selection must not lock attempts before the salon advisory lock")
	}
	leaseCandidateStart := strings.Index(source, "func (r *Repository) expireBookingOperationLeaseCandidate")
	leaseCandidateEnd := strings.Index(source[leaseCandidateStart:], "func (r *Repository) SweepExpiredBookingOperationLeases")
	if leaseCandidateStart < 0 || leaseCandidateEnd < 0 {
		t.Fatal("could not locate per-candidate lease expiry implementation")
	}
	leaseCandidateSource := source[leaseCandidateStart : leaseCandidateStart+leaseCandidateEnd]
	advisoryLock := strings.Index(leaseCandidateSource, "lockBookingCalendarReconciliationTx")
	appointmentLock := strings.Index(leaseCandidateSource, "FROM appointments")
	attemptLock := strings.Index(leaseCandidateSource, "FROM booking_attempts attempt")
	if advisoryLock < 0 || appointmentLock < 0 || attemptLock < 0 || advisoryLock > appointmentLock || appointmentLock > attemptLock {
		t.Fatal("lease expiry must lock advisory key, then appointment mirror, then attempt")
	}
	assertCalendarMutationLockOrder := func(functionName string, nextFunctionName string, appointmentFragment string, attemptFragment string) {
		t.Helper()
		start := strings.Index(source, functionName)
		if start < 0 {
			t.Fatalf("could not locate %s", functionName)
		}
		end := len(source)
		if nextFunctionName != "" {
			relativeEnd := strings.Index(source[start+len(functionName):], nextFunctionName)
			if relativeEnd >= 0 {
				end = start + len(functionName) + relativeEnd
			}
		}
		body := source[start:end]
		advisory := strings.Index(body, "lockBookingCalendarReconciliationTx")
		appointment := strings.Index(body, appointmentFragment)
		attempt := strings.Index(body, attemptFragment)
		if advisory < 0 || appointment < 0 || attempt < 0 || advisory > appointment || appointment > attempt {
			t.Fatalf("%s must lock advisory key, then appointment/mirror, then attempt", functionName)
		}
	}
	assertCalendarMutationLockOrder("func (r *Repository) SaveConfirmedBooking", "func (r *Repository) SaveFallbackBooking", "FROM appointments", "FROM booking_attempts")
	assertCalendarMutationLockOrder("func (r *Repository) SaveFallbackBooking", "func (r *Repository) GetAppointmentForOwner", "FROM appointments", "FROM booking_attempts")
	assertCalendarMutationLockOrder("func (r *Repository) SaveRescheduledAppointment", "func (r *Repository) SaveCancelledAppointment", "loadDirectMutationAppointmentTx", "UPDATE booking_attempts")
	assertCalendarMutationLockOrder("func (r *Repository) SaveCancelledAppointment", "func (r *Repository) SaveAppointmentActionFallback", "loadDirectMutationAppointmentTx", "UPDATE booking_attempts")
	assertCalendarMutationLockOrder("func (r *Repository) SaveAppointmentActionFallback", "func (r *Repository) LatestTestBooking", "loadDirectMutationAppointmentTx", "FROM booking_attempts")

	assertAttemptBeforeTask := func(functionName string, nextFunctionName string) {
		t.Helper()
		start := strings.Index(source, functionName)
		if start < 0 {
			t.Fatalf("could not locate %s", functionName)
		}
		end := len(source)
		if nextFunctionName != "" {
			relativeEnd := strings.Index(source[start+len(functionName):], nextFunctionName)
			if relativeEnd >= 0 {
				end = start + len(functionName) + relativeEnd
			}
		}
		body := source[start:end]
		attemptLock := strings.Index(body, "booking_attempts")
		taskLock := strings.Index(body, "booking_reconciliation_tasks")
		if attemptLock < 0 || taskLock < 0 || attemptLock > taskLock {
			t.Fatalf("%s must access/lock booking_attempts before booking_reconciliation_tasks", functionName)
		}
	}
	assertAttemptBeforeTask("func ensureReconciliationTaskTx", "func closeSupersededReconciliationTaskTx")
	assertAttemptBeforeTask("func closeSupersededReconciliationTaskTx", "func loadAvailabilityQuoteProviderFenceTx")
	assertAttemptBeforeTask("func (r *Repository) ResolveReconciliationTask", "func (r *Repository) ListCalendarAppointments")
	assertAttemptBeforeTask("func resolveCalendarReconciliationTaskTx", "func mergeCalendarCustomerDetails")
}

func TestRepositorySweepExpiredNotStartedLeaseCreatesOneRetrySafeFallback(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, serviceID, staffID := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	startTime := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	record := PendingBookingRecord{
		SalonID:            salonID,
		Source:             SourceAIVoiceCall,
		Provider:           pos.ProviderSquare,
		POSIdempotencyKey:  uuid.NewString(),
		OperationKey:       "integration-pre-dispatch-crash",
		RequestFingerprint: "integration-pre-dispatch-crash-fingerprint",
		ProcessingToken:    uuid.NewString(),
		LeaseExpiresAt:     time.Now().UTC().Add(-time.Minute),
		CustomerName:       "Integration Caller",
		CustomerPhone:      "+13125550199",
		Service: ServiceRef{
			ID: serviceID, POSProvider: pos.ProviderSquare, POSServiceID: "integration-service",
			POSServiceVersion: 1, Name: "Integration Manicure", DurationMinutes: 45,
		},
		Staff: StaffRef{
			ID: staffID, POSProvider: pos.ProviderSquare, POSStaffID: "integration-staff", Name: "Integration Staff",
		},
		StaffSelectionMode: StaffSelectionSpecific,
		StartTime:          startTime,
		EndTime:            startTime.Add(45 * time.Minute),
	}
	record.Segments = singleBookingSegment(record.Service, record.Staff, record.StaffSelectionMode)
	claim, err := repo.ClaimPendingBookingAttempt(ctx, record)
	if err != nil {
		t.Fatalf("claim pre-dispatch operation: %v", err)
	}
	if claim == nil || !claim.Acquired || claim.Attempt.ProviderOutcome != ProviderOutcomeNotStarted {
		t.Fatalf("pre-dispatch claim = %#v, want acquired not_started", claim)
	}

	processed, err := repo.SweepExpiredBookingOperationLeases(ctx, 50)
	if err != nil {
		t.Fatalf("sweep pre-dispatch lease: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed lease salons = %d, want 1", processed)
	}
	processed, err = repo.SweepExpiredBookingOperationLeases(ctx, 50)
	if err != nil {
		t.Fatalf("repeat pre-dispatch lease sweep: %v", err)
	}
	if processed != 0 {
		t.Fatalf("repeated processed lease salons = %d, want 0", processed)
	}

	var status, outcome, retryPolicy, reconciliation, errorCode, errorMessage string
	var hasProcessingToken, hasProcessingLease bool
	if err := db.QueryRowContext(ctx, `
		SELECT status, provider_outcome, retry_policy, reconciliation_status,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       processing_token IS NOT NULL, processing_lease_expires_at IS NOT NULL
		FROM booking_attempts
		WHERE id = $1
	`, claim.Attempt.ID).Scan(
		&status,
		&outcome,
		&retryPolicy,
		&reconciliation,
		&errorCode,
		&errorMessage,
		&hasProcessingToken,
		&hasProcessingLease,
	); err != nil {
		t.Fatalf("load pre-dispatch fallback: %v", err)
	}
	preDispatchPolicy := bookingLeaseExpiryPolicyFor(ProviderOutcomeNotStarted)
	if status != StatusFallbackPending || outcome != preDispatchPolicy.ProviderOutcome || retryPolicy != preDispatchPolicy.RetryPolicy || reconciliation != preDispatchPolicy.Reconciliation {
		t.Fatalf("pre-dispatch fallback = %s/%s/%s/%s, want fallback_pending/failed/safe/not_required", status, outcome, retryPolicy, reconciliation)
	}
	if errorCode != preDispatchPolicy.ErrorCode || errorMessage != preDispatchPolicy.ErrorMessage || hasProcessingToken || hasProcessingLease {
		t.Fatalf("pre-dispatch error/lease = %s/%q token=%t lease=%t", errorCode, errorMessage, hasProcessingToken, hasProcessingLease)
	}

	var notificationCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM owner_notifications
		WHERE salon_id = $1 AND booking_attempt_id = $2 AND dedupe_key = $3
	`, salonID, claim.Attempt.ID, preDispatchPolicy.NotificationDedupePrefix+claim.Attempt.ID).Scan(&notificationCount); err != nil {
		t.Fatalf("count pre-dispatch notifications: %v", err)
	}
	if notificationCount != 1 {
		t.Fatalf("pre-dispatch notifications = %d, want 1", notificationCount)
	}
	var payloadOutcome, payloadRetryPolicy, payloadReconciliation, payloadPhase, deliveryStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT payload->>'provider_outcome', payload->>'retry_policy', payload->>'reconciliation_status',
		       payload->>'failure_phase', delivery_status
		FROM owner_notifications
		WHERE salon_id = $1 AND booking_attempt_id = $2 AND dedupe_key = $3
	`, salonID, claim.Attempt.ID, preDispatchPolicy.NotificationDedupePrefix+claim.Attempt.ID).Scan(
		&payloadOutcome,
		&payloadRetryPolicy,
		&payloadReconciliation,
		&payloadPhase,
		&deliveryStatus,
	); err != nil {
		t.Fatalf("load pre-dispatch notification payload: %v", err)
	}
	if payloadOutcome != ProviderOutcomeFailed || payloadRetryPolicy != RetryPolicySafe || payloadReconciliation != ReconciliationNotRequired || payloadPhase != "pre_dispatch" || deliveryStatus != "queued" {
		t.Fatalf("pre-dispatch outbox = %s/%s/%s/%s/%s", payloadOutcome, payloadRetryPolicy, payloadReconciliation, payloadPhase, deliveryStatus)
	}

	var posErrorCount, reconciliationTaskCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pos_errors
		WHERE salon_id = $1 AND operation = $2 AND error_code = $3
	`, salonID, BookingActionBook+preDispatchPolicy.OperationSuffix, preDispatchPolicy.ErrorCode).Scan(&posErrorCount); err != nil {
		t.Fatalf("count pre-dispatch POS errors: %v", err)
	}
	if posErrorCount != 1 {
		t.Fatalf("pre-dispatch POS errors = %d, want 1", posErrorCount)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM booking_reconciliation_tasks
		WHERE salon_id = $1 AND booking_attempt_id = $2
	`, salonID, claim.Attempt.ID).Scan(&reconciliationTaskCount); err != nil {
		t.Fatalf("count pre-dispatch reconciliation tasks: %v", err)
	}
	if reconciliationTaskCount != 0 {
		t.Fatalf("pre-dispatch reconciliation tasks = %d, want 0", reconciliationTaskCount)
	}

	retry := record
	retry.OperationKey = "integration-pre-dispatch-crash-retry"
	retry.RetryOfAttemptID = claim.Attempt.ID
	retry.POSIdempotencyKey = uuid.NewString()
	retry.ProcessingToken = uuid.NewString()
	retry.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
	retryClaim, err := repo.ClaimPendingBookingAttempt(ctx, retry)
	if err != nil {
		t.Fatalf("claim safe pre-dispatch retry: %v", err)
	}
	if retryClaim == nil || !retryClaim.Acquired || retryClaim.Attempt.RetryOfAttemptID != claim.Attempt.ID || retryClaim.Attempt.RetrySequence != 1 {
		t.Fatalf("pre-dispatch retry claim = %#v, want acquired retry sequence 1", retryClaim)
	}
}

func TestRepositoryLeaseSweepLimitBoundsAttemptsNotSalons(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
	}()
	for index := 0; index < 3; index++ {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO booking_attempts (
				salon_id, source, status, pos_provider, operation_key, request_fingerprint,
				processing_token, processing_lease_expires_at, provider_outcome, retry_policy,
				reconciliation_status, operation_type, customer_name, customer_phone,
				requested_start_time, requested_end_time
			)
			VALUES ($1, $2, $3, 'square', $4, $5, $6, now() - interval '1 minute',
			        $7, $8, $9, $10, 'Lease Caller', '+13125550199', now() + interval '1 day', now() + interval '1 day 45 minutes')
		`, salonID, SourceAIVoiceCall, StatusPOSPending, fmt.Sprintf("bounded-lease-%d-%s", index, uuid.NewString()), strings.Repeat(fmt.Sprintf("%x", index+1), 64)[:64], uuid.NewString(), ProviderOutcomeNotStarted, RetryPolicyNone, ReconciliationNotRequired, BookingActionBook); err != nil {
			t.Fatalf("insert expired attempt %d: %v", index, err)
		}
	}
	repo := NewRepository(db)
	processed, err := repo.SweepExpiredBookingOperationLeases(ctx, 2)
	if err != nil {
		t.Fatalf("first bounded sweep: %v", err)
	}
	if processed != 2 {
		t.Fatalf("first bounded sweep processed = %d, want 2 attempts", processed)
	}
	processed, err = repo.SweepExpiredBookingOperationLeases(ctx, 2)
	if err != nil {
		t.Fatalf("second bounded sweep: %v", err)
	}
	if processed != 1 {
		t.Fatalf("second bounded sweep processed = %d, want remaining 1 attempt", processed)
	}
}

func TestRepositoryBookingCalendarReconciliationLockSerializesTransactions(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	salonID := uuid.NewString()
	first, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin first transaction: %v", err)
	}
	defer first.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, first, salonID); err != nil {
		t.Fatalf("acquire first reconciliation lock: %v", err)
	}

	contender, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin contender transaction: %v", err)
	}
	if _, err := contender.ExecContext(ctx, `SET LOCAL lock_timeout = '150ms'`); err != nil {
		contender.Rollback()
		t.Fatalf("set contender lock timeout: %v", err)
	}
	if err := lockBookingCalendarReconciliationTx(ctx, contender, salonID); err == nil {
		contender.Rollback()
		t.Fatal("concurrent calendar/reconciliation transaction acquired the same salon lock")
	}
	if err := contender.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("rollback contender transaction: %v", err)
	}

	if err := first.Commit(); err != nil {
		t.Fatalf("commit first transaction: %v", err)
	}
	afterRelease, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin post-release transaction: %v", err)
	}
	defer afterRelease.Rollback()
	if err := lockBookingCalendarReconciliationTx(ctx, afterRelease, salonID); err != nil {
		t.Fatalf("acquire reconciliation lock after release: %v", err)
	}
}

func TestRepositoryConcurrentOperationClaimHasOneWriter(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, serviceID, staffID := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	base := PendingBookingRecord{
		SalonID:            salonID,
		Source:             SourceAIVoiceCall,
		Provider:           "square",
		OperationKey:       "integration-concurrent-operation",
		RequestFingerprint: "same-fingerprint",
		CustomerName:       "Integration Caller",
		CustomerPhone:      "+13125550199",
		Service: ServiceRef{
			ID: serviceID, POSProvider: "square", POSServiceID: "integration-service",
			POSServiceVersion: 1, Name: "Integration Manicure", DurationMinutes: 45,
		},
		Staff: StaffRef{
			ID: staffID, POSProvider: "square", POSStaffID: "integration-staff", Name: "Integration Staff",
		},
		StaffSelectionMode: StaffSelectionSpecific,
		StartTime:          time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second),
		EndTime:            time.Now().UTC().Add(24*time.Hour + 45*time.Minute).Truncate(time.Second),
	}
	base.Segments = singleBookingSegment(base.Service, base.Staff, base.StaffSelectionMode)

	claims := make([]*BookingOperationClaim, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for index := range claims {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			record := base
			record.POSIdempotencyKey = uuid.NewString()
			record.ProcessingToken = uuid.NewString()
			record.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
			claims[index], errs[index] = repo.ClaimPendingBookingAttempt(ctx, record)
		}(index)
	}
	wg.Wait()

	acquired := 0
	for index, err := range errs {
		if err != nil {
			t.Fatalf("claim %d: %v", index, err)
		}
		if claims[index] == nil || claims[index].Attempt == nil {
			t.Fatalf("claim %d returned no attempt", index)
		}
		if claims[index].Acquired {
			acquired++
		}
	}
	if acquired != 1 {
		t.Fatalf("acquired claims = %d, want 1", acquired)
	}
	if claims[0].Attempt.ID != claims[1].Attempt.ID {
		t.Fatalf("attempt ids = %s/%s, want one durable attempt", claims[0].Attempt.ID, claims[1].Attempt.ID)
	}
	if claims[0].Attempt.POSIdempotencyKey != claims[1].Attempt.POSIdempotencyKey {
		t.Fatalf("POS idempotency keys differ: %s/%s", claims[0].Attempt.POSIdempotencyKey, claims[1].Attempt.POSIdempotencyKey)
	}
	var acquiredClaim *BookingOperationClaim
	for _, claim := range claims {
		if claim.Acquired {
			acquiredClaim = claim
			break
		}
	}
	if acquiredClaim == nil {
		t.Fatal("no acquired claim found")
	}
	if err := repo.MarkBookingOperationStarted(ctx, salonID, acquiredClaim.Attempt.ID, acquiredClaim.Attempt.ProcessingToken, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatalf("mark operation started: %v", err)
	}
	if err := repo.ExpireBookingOperationLeases(ctx, salonID); err != nil {
		t.Fatalf("expire operation lease: %v", err)
	}
	if err := repo.ExpireBookingOperationLeases(ctx, salonID); err != nil {
		t.Fatalf("repeat expired operation reconciliation: %v", err)
	}
	var status, outcome, retryPolicy, reconciliation string
	if err := db.QueryRowContext(ctx, `
		SELECT status, provider_outcome, retry_policy, reconciliation_status
		FROM booking_attempts
		WHERE id = $1
	`, acquiredClaim.Attempt.ID).Scan(&status, &outcome, &retryPolicy, &reconciliation); err != nil {
		t.Fatalf("load expired operation: %v", err)
	}
	if status != StatusFallbackPending || outcome != ProviderOutcomeUnknown || retryPolicy != RetryPolicyBlocked || reconciliation != ReconciliationRequired {
		t.Fatalf("expired operation state = %s/%s/%s/%s, want fallback_pending/unknown/blocked/required", status, outcome, retryPolicy, reconciliation)
	}
	var notificationCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM owner_notifications WHERE booking_attempt_id = $1 AND type = $2
	`, acquiredClaim.Attempt.ID, NotificationTypeBookingFallback).Scan(&notificationCount); err != nil {
		t.Fatalf("count reconciliation notifications: %v", err)
	}
	if notificationCount != 1 {
		t.Fatalf("reconciliation notifications = %d, want 1", notificationCount)
	}
	var taskCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM booking_reconciliation_tasks WHERE booking_attempt_id = $1 AND status = 'open'
	`, acquiredClaim.Attempt.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count reconciliation tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("open reconciliation tasks = %d, want 1", taskCount)
	}
	var payloadEventType, payloadOutcome, payloadRetryPolicy, payloadReconciliation, payloadPhase string
	var deliveryStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT payload->>'event_type', payload->>'provider_outcome', payload->>'retry_policy',
		       payload->>'reconciliation_status', payload->>'failure_phase', delivery_status
		FROM owner_notifications
		WHERE booking_attempt_id = $1 AND type = $2
	`, acquiredClaim.Attempt.ID, NotificationTypeBookingFallback).Scan(
		&payloadEventType,
		&payloadOutcome,
		&payloadRetryPolicy,
		&payloadReconciliation,
		&payloadPhase,
		&deliveryStatus,
	); err != nil {
		t.Fatalf("load reconciliation outbox payload: %v", err)
	}
	if payloadEventType != NotificationTypeBookingFallback || payloadOutcome != ProviderOutcomeUnknown || payloadRetryPolicy != RetryPolicyBlocked || payloadReconciliation != ReconciliationRequired || payloadPhase != "in_flight" || deliveryStatus != "queued" {
		t.Fatalf("in-flight outbox = %s/%s/%s/%s/%s/%s", payloadEventType, payloadOutcome, payloadRetryPolicy, payloadReconciliation, payloadPhase, deliveryStatus)
	}
	var posErrorCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pos_errors
		WHERE salon_id = $1 AND operation = $2 AND error_code = $3
	`, salonID, BookingActionBook+bookingLeaseExpiryPolicyFor(ProviderOutcomeInFlight).OperationSuffix, pos.ErrorTimeout).Scan(&posErrorCount); err != nil {
		t.Fatalf("count in-flight lease POS errors: %v", err)
	}
	if posErrorCount != 1 {
		t.Fatalf("in-flight lease POS errors = %d, want 1", posErrorCount)
	}

	sameIntent := base
	sameIntent.OperationKey = "integration-same-intent-different-key"
	sameIntent.POSIdempotencyKey = uuid.NewString()
	sameIntent.ProcessingToken = uuid.NewString()
	sameIntent.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
	sameIntentClaim, err := repo.ClaimPendingBookingAttempt(ctx, sameIntent)
	if err != nil {
		t.Fatalf("cross-key same-intent claim: %v", err)
	}
	if sameIntentClaim.Acquired || sameIntentClaim.Attempt == nil || sameIntentClaim.Attempt.ID != acquiredClaim.Attempt.ID {
		t.Fatalf("cross-key same-intent claim = %#v, want existing non-acquired attempt %s", sameIntentClaim, acquiredClaim.Attempt.ID)
	}
	if _, err := NewService(repo, nil).ResolveReconciliation(ctx, salonID, ownerID, acquiredClaim.Attempt.ID, ResolveReconciliationRequest{
		ActionKey: "integration-not-created",
		Action:    ReconciliationActionNotCreated,
		Note:      "Verified no appointment exists in the provider.",
	}); err != nil {
		t.Fatalf("resolve expired attempt as not created: %v", err)
	}
	retry := base
	retry.OperationKey = "integration-explicit-retry"
	retry.RetryOfAttemptID = acquiredClaim.Attempt.ID
	retry.POSIdempotencyKey = uuid.NewString()
	retry.ProcessingToken = uuid.NewString()
	retry.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
	firstRetry, err := repo.ClaimPendingBookingAttempt(ctx, retry)
	if err != nil {
		t.Fatalf("explicit retry claim: %v", err)
	}
	if firstRetry == nil || !firstRetry.Acquired || firstRetry.Attempt.RetrySequence != 1 {
		t.Fatalf("explicit retry claim = %#v, want acquired sequence 1", firstRetry)
	}
	retry.POSIdempotencyKey = uuid.NewString()
	retry.ProcessingToken = uuid.NewString()
	replayedRetry, err := repo.ClaimPendingBookingAttempt(ctx, retry)
	if err != nil {
		t.Fatalf("replayed explicit retry claim: %v", err)
	}
	if replayedRetry == nil || replayedRetry.Acquired || replayedRetry.Attempt.ID != firstRetry.Attempt.ID {
		t.Fatalf("replayed explicit retry = %#v, want same non-acquired attempt %s", replayedRetry, firstRetry.Attempt.ID)
	}
	var supersededBy, supersededAttemptReconciliation, supersededAttemptResolution string
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(superseded_by_attempt_id::text, ''), reconciliation_status,
		       COALESCE(reconciliation_resolution, '')
		FROM booking_attempts
		WHERE id = $1
	`, acquiredClaim.Attempt.ID).Scan(&supersededBy, &supersededAttemptReconciliation, &supersededAttemptResolution); err != nil {
		t.Fatalf("load retry lineage: %v", err)
	}
	if supersededBy != firstRetry.Attempt.ID {
		t.Fatalf("retry lineage superseded_by = %s, want %s", supersededBy, firstRetry.Attempt.ID)
	}
	if supersededAttemptReconciliation != ReconciliationResolved || supersededAttemptResolution != ReconciliationResolutionSuperseded {
		t.Fatalf("retry source reconciliation = %s/%s, want resolved/superseded", supersededAttemptReconciliation, supersededAttemptResolution)
	}
	var supersededTaskStatus, supersededTaskResolution string
	var supersededTaskResolved bool
	if err := db.QueryRowContext(ctx, `
		SELECT status, COALESCE(resolution, ''), resolved_at IS NOT NULL
		FROM booking_reconciliation_tasks
		WHERE booking_attempt_id = $1
	`, acquiredClaim.Attempt.ID).Scan(&supersededTaskStatus, &supersededTaskResolution, &supersededTaskResolved); err != nil {
		t.Fatalf("load superseded retry task: %v", err)
	}
	if supersededTaskStatus != "resolved" || supersededTaskResolution != ReconciliationResolutionSuperseded || !supersededTaskResolved {
		t.Fatalf("superseded retry task = %s/%s/%t, want resolved/superseded/true", supersededTaskStatus, supersededTaskResolution, supersededTaskResolved)
	}
	var supersededEventCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM booking_reconciliation_events
		WHERE booking_attempt_id = $1
		  AND action = $2
		  AND action_key = $2 || ':' || $3
	`, acquiredClaim.Attempt.ID, ReconciliationResolutionSuperseded, firstRetry.Attempt.ID).Scan(&supersededEventCount); err != nil {
		t.Fatalf("count superseded retry events: %v", err)
	}
	if supersededEventCount != 1 {
		t.Fatalf("superseded retry events = %d, want 1", supersededEventCount)
	}

	conflicting := base
	conflicting.RequestFingerprint = "different-fingerprint"
	conflicting.POSIdempotencyKey = uuid.NewString()
	conflicting.ProcessingToken = uuid.NewString()
	conflicting.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
	if _, err := repo.ClaimPendingBookingAttempt(ctx, conflicting); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("conflicting claim error = %v, want ErrOperationConflict", err)
	}
}

func TestRepositoryAvailabilityQuoteRejectsPositionalSegmentMismatchAtomically(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, serviceAID, staffID := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()
	var serviceBID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id, pos_provider, pos_service_id, pos_service_version, name, duration_minutes)
		VALUES ($1, 'square', 'integration-service-b', 1, 'Integration Pedicure', 45)
		RETURNING id::text
	`, salonID).Scan(&serviceBID); err != nil {
		t.Fatalf("insert second service: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id, snapshot_generation, last_sync_at)
		VALUES ($1, 'square', 'active', 'integration-location', 1, now())
	`, salonID); err != nil {
		t.Fatalf("insert ready POS connection: %v", err)
	}

	repo := NewRepository(db)
	startTime := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	endTime := startTime.Add(90 * time.Minute)
	fence := pos.ProviderFence{LocationID: "integration-location", SnapshotGeneration: 1}
	serviceA := ServiceRef{ID: serviceAID, POSProvider: "square", POSServiceID: "integration-service", POSServiceVersion: 1, Name: "Integration Manicure", DurationMinutes: 45, ProviderFence: fence}
	serviceB := ServiceRef{ID: serviceBID, POSProvider: "square", POSServiceID: "integration-service-b", POSServiceVersion: 1, Name: "Integration Pedicure", DurationMinutes: 45, ProviderFence: fence}
	staff := StaffRef{ID: staffID, POSProvider: "square", POSStaffID: "integration-staff", Name: "Integration Staff", ProviderFence: fence}
	slotFingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	quote, err := repo.CreateAvailabilityQuote(ctx, AvailabilityQuoteRecord{
		SalonID:            salonID,
		Provider:           "square",
		ProviderFence:      fence,
		RequestFingerprint: "integration-quote-request",
		ExpiresAt:          time.Now().UTC().Add(5 * time.Minute),
		Slots: []AvailabilitySlot{{
			Fingerprint: slotFingerprint,
			StartTime:   startTime,
			EndTime:     endTime,
			Segments: []AvailabilitySegment{
				{ServiceID: serviceAID, StaffID: staffID, StaffSelectionMode: StaffSelectionSpecific, DurationMinutes: 45},
				{ServiceID: serviceAID, StaffID: staffID, StaffSelectionMode: StaffSelectionSpecific, DurationMinutes: 45},
			},
		}},
	})
	if err != nil {
		t.Fatalf("create availability quote: %v", err)
	}
	record := PendingBookingRecord{
		SalonID:             salonID,
		Source:              SourceSquareTestBooking,
		Provider:            "square",
		POSIdempotencyKey:   uuid.NewString(),
		OperationKey:        "integration-positional-quote",
		RequestFingerprint:  "integration-positional-booking",
		AvailabilityQuoteID: quote.ID,
		SlotFingerprint:     slotFingerprint,
		ProviderFence:       fence,
		ProcessingToken:     uuid.NewString(),
		LeaseExpiresAt:      time.Now().UTC().Add(bookingOperationLeaseDuration),
		CustomerName:        "Integration Caller",
		CustomerPhone:       "+13125550199",
		Service:             serviceA,
		Staff:               staff,
		StaffSelectionMode:  StaffSelectionSpecific,
		StartTime:           startTime,
		EndTime:             endTime,
		Segments: []BookingSegmentRecord{
			{Service: serviceA, Staff: staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1},
			{Service: serviceB, Staff: staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 2},
		},
	}
	if _, err := repo.ClaimPendingBookingAttempt(ctx, record); !errors.Is(err, ErrAvailabilityQuoteStale) {
		t.Fatalf("mismatched positional quote error = %v, want ErrAvailabilityQuoteStale", err)
	}
	var consumed bool
	if err := db.QueryRowContext(ctx, `SELECT consumed_at IS NOT NULL FROM availability_quotes WHERE id = $1`, quote.ID).Scan(&consumed); err != nil {
		t.Fatalf("load quote consumption: %v", err)
	}
	if consumed {
		t.Fatal("mismatched quote was consumed despite transaction rollback")
	}
	var attemptCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM booking_attempts WHERE salon_id = $1 AND operation_key = $2`, salonID, record.OperationKey).Scan(&attemptCount); err != nil {
		t.Fatalf("count rolled-back attempt: %v", err)
	}
	if attemptCount != 0 {
		t.Fatalf("rolled-back quote attempt count = %d, want 0", attemptCount)
	}

	record.Segments[1].Service = serviceA
	record.POSIdempotencyKey = uuid.NewString()
	record.ProcessingToken = uuid.NewString()
	claim, err := repo.ClaimPendingBookingAttempt(ctx, record)
	if err != nil {
		t.Fatalf("matching positional quote claim: %v", err)
	}
	if claim == nil || !claim.Acquired {
		t.Fatalf("matching positional quote claim = %#v, want acquired", claim)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE booking_attempts
		SET status = $1, provider_outcome = $2, retry_policy = $3,
		    reconciliation_status = $4, processing_lease_expires_at = NULL
		WHERE id = $5
	`, StatusFallbackPending, ProviderOutcomeFailed, RetryPolicySafe, ReconciliationNotRequired, claim.Attempt.ID); err != nil {
		t.Fatalf("mark quote attempt safe to retry: %v", err)
	}
	retry := record
	retry.OperationKey = "integration-positional-quote-retry"
	retry.RetryOfAttemptID = claim.Attempt.ID
	retry.POSIdempotencyKey = uuid.NewString()
	retry.ProcessingToken = uuid.NewString()
	retry.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
	retryClaim, err := repo.ClaimPendingBookingAttempt(ctx, retry)
	if err != nil {
		t.Fatalf("safe retry with lineage-owned quote: %v", err)
	}
	if retryClaim == nil || !retryClaim.Acquired || retryClaim.Attempt.RetryOfAttemptID != claim.Attempt.ID {
		t.Fatalf("safe quote retry claim = %#v, want acquired lineage from %s", retryClaim, claim.Attempt.ID)
	}
	var quoteConsumer string
	if err := db.QueryRowContext(ctx, `SELECT consumed_by_attempt_id::text FROM availability_quotes WHERE id = $1`, quote.ID).Scan(&quoteConsumer); err != nil {
		t.Fatalf("load retried quote consumer: %v", err)
	}
	if quoteConsumer != retryClaim.Attempt.ID {
		t.Fatalf("retried quote consumer = %s, want %s", quoteConsumer, retryClaim.Attempt.ID)
	}
	latest, err := repo.LatestTestBooking(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("load latest test booking after retry: %v", err)
	}
	if latest.BookingAttemptID != retryClaim.Attempt.ID || latest.OperationType != BookingActionBook {
		t.Fatalf("latest test booking = %#v, want retry %s with operation book", latest, retryClaim.Attempt.ID)
	}
}

func TestRepositoryAvailabilityQuoteRejectsFenceChangedBeforePersistence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id, snapshot_generation, last_sync_at)
		VALUES ($1, 'square', 'active', 'location-b', 2, now())
	`, salonID); err != nil {
		t.Fatalf("insert current POS connection: %v", err)
	}

	repo := NewRepository(db)
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	_, err = repo.CreateAvailabilityQuote(ctx, AvailabilityQuoteRecord{
		SalonID:            salonID,
		Provider:           pos.ProviderSquare,
		ProviderFence:      pos.ProviderFence{LocationID: "location-a", SnapshotGeneration: 1},
		RequestFingerprint: "stale-fence-race",
		ExpiresAt:          time.Now().UTC().Add(5 * time.Minute),
		Slots: []AvailabilitySlot{{
			Fingerprint: strings.Repeat("f", 64),
			StartTime:   start,
			EndTime:     start.Add(45 * time.Minute),
		}},
	})
	if !errors.Is(err, ErrAvailabilityQuoteStale) {
		t.Fatalf("stale fence quote error = %v, want ErrAvailabilityQuoteStale", err)
	}
	var quoteCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM availability_quotes WHERE salon_id = $1`, salonID).Scan(&quoteCount); err != nil {
		t.Fatalf("count availability quotes: %v", err)
	}
	if quoteCount != 0 {
		t.Fatalf("stale fence quote count = %d, want 0", quoteCount)
	}
}

func TestRepositorySupersededReconciliationAttemptIsClosedNotListedOrResolvable(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, serviceID, staffID := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	startTime := time.Now().UTC().Add(60 * time.Hour).Truncate(time.Second)
	endTime := startTime.Add(45 * time.Minute)
	insertAttempt := func(operationKey string, reconciliationStatus string, supersededBy string) string {
		t.Helper()
		var attemptID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO booking_attempts (
				salon_id, source, status, pos_provider, operation_key, request_fingerprint,
				operation_type, provider_outcome, retry_policy, reconciliation_status,
				reconciliation_resolution, reconciliation_resolved_at,
				superseded_by_attempt_id, superseded_at,
				customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
				requested_start_time, requested_end_time, error_code, error_message
			)
			VALUES ($1, $2, $3, 'square', $4, 'integration-historical-duplicate',
			        $5, $6, $7, $8, NULLIF($9, ''),
			        CASE WHEN $9 = '' THEN NULL ELSE now() END,
			        NULLIF($10, '')::uuid, CASE WHEN $10 = '' THEN NULL ELSE now() END,
			        'Integration Caller', '+13125550199', $11, $12, $13,
			        $14, $15, 'POS_TIMEOUT', 'Provider response was unknown')
			RETURNING id::text
		`, salonID, SourceAIVoiceCall, StatusFallbackPending, operationKey,
			BookingActionBook, ProviderOutcomeUnknown, RetryPolicyBlocked, reconciliationStatus,
			func() string {
				if supersededBy == "" {
					return ""
				}
				return ReconciliationResolutionSuperseded
			}(), supersededBy, serviceID, staffID, StaffSelectionSpecific, startTime, endTime).Scan(&attemptID); err != nil {
			t.Fatalf("insert reconciliation attempt %s: %v", operationKey, err)
		}
		return attemptID
	}

	canonicalAttemptID := insertAttempt("integration-canonical-attempt", ReconciliationRequired, "")
	supersededAttemptID := insertAttempt("integration-superseded-attempt", ReconciliationResolved, canonicalAttemptID)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_tasks (salon_id, booking_attempt_id, status)
		VALUES ($1, $2, 'open')
	`, salonID, canonicalAttemptID); err != nil {
		t.Fatalf("insert canonical reconciliation task: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_tasks (
			salon_id, booking_attempt_id, status, resolution, resolution_note, resolved_at
		)
		VALUES ($1, $2, 'resolved', $3, 'Booking attempt was superseded by a canonical attempt.', now())
	`, salonID, supersededAttemptID, ReconciliationResolutionSuperseded); err != nil {
		t.Fatalf("insert closed superseded reconciliation task: %v", err)
	}

	repo := NewRepository(db)
	service := NewService(repo, nil)
	listed, err := service.ReconciliationTasks(ctx, salonID, ownerID, "open", 100, 0)
	if err != nil {
		t.Fatalf("list open reconciliation tasks: %v", err)
	}
	if len(listed.Tasks) != 1 || listed.Tasks[0].BookingAttemptID != canonicalAttemptID {
		t.Fatalf("listed reconciliation tasks = %#v, want only canonical attempt %s", listed.Tasks, canonicalAttemptID)
	}
	if _, err := service.ReconciliationCandidates(ctx, salonID, ownerID, supersededAttemptID); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("superseded candidate lookup error = %v, want ErrOperationConflict", err)
	}
	if _, err := service.ResolveReconciliation(ctx, salonID, ownerID, supersededAttemptID, ResolveReconciliationRequest{
		ActionKey: "integration-resolve-superseded",
		Action:    ReconciliationActionNotCreated,
		Note:      "This superseded ledger row must remain non-resolvable.",
	}); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("superseded resolution error = %v, want ErrOperationConflict", err)
	}

	var taskStatus, taskResolution string
	if err := db.QueryRowContext(ctx, `
		SELECT status, COALESCE(resolution, '')
		FROM booking_reconciliation_tasks
		WHERE booking_attempt_id = $1
	`, supersededAttemptID).Scan(&taskStatus, &taskResolution); err != nil {
		t.Fatalf("load superseded task status: %v", err)
	}
	if taskStatus != "resolved" || taskResolution != ReconciliationResolutionSuperseded {
		t.Fatalf("superseded task changed to %q/%q during rejected resolution", taskStatus, taskResolution)
	}

	originalProcessingToken := uuid.NewString()
	var originalLease time.Time
	if err := db.QueryRowContext(ctx, `
		UPDATE booking_attempts
		SET status = $1,
		    provider_outcome = $2,
		    retry_policy = $3,
		    processing_token = $4,
		    processing_lease_expires_at = now() - interval '5 minutes',
		    updated_at = now()
		WHERE id = $5
		RETURNING processing_lease_expires_at
	`, StatusPOSPending, ProviderOutcomeNotStarted, RetryPolicyNone, originalProcessingToken, supersededAttemptID).Scan(&originalLease); err != nil {
		t.Fatalf("prepare superseded expired claim fixture: %v", err)
	}
	if err := repo.MarkBookingOperationStarted(ctx, salonID, supersededAttemptID, originalProcessingToken, time.Now().UTC().Add(bookingOperationLeaseDuration)); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("mark superseded operation started error = %v, want ErrOperationInProgress", err)
	}
	if _, err := repo.SaveFallbackBooking(ctx, FallbackBookingRecord{
		AttemptID:          supersededAttemptID,
		SalonID:            salonID,
		Source:             SourceAIVoiceCall,
		Provider:           pos.ProviderSquare,
		Operation:          BookingActionBook,
		CustomerName:       "Integration Caller",
		CustomerPhone:      "+13125550199",
		Service:            ServiceRef{ID: serviceID, POSProvider: pos.ProviderSquare, POSServiceID: "integration-service", POSServiceVersion: 1, Name: "Integration Manicure", DurationMinutes: 45},
		Staff:              StaffRef{ID: staffID, POSProvider: pos.ProviderSquare, POSStaffID: "integration-staff", Name: "Integration Staff"},
		StaffSelectionMode: StaffSelectionSpecific,
		StartTime:          startTime,
		EndTime:            endTime,
		ErrorCode:          pos.ErrorTimeout,
		ErrorMessage:       "An old worker must not finalize a superseded attempt.",
		ProcessingToken:    originalProcessingToken,
		ProviderOutcome:    ProviderOutcomeFailed,
		RetryPolicy:        RetryPolicySafe,
		Reconciliation:     ReconciliationNotRequired,
		Status:             StatusFallbackPending,
	}); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("finalize superseded fallback error = %v, want pos.ErrNotFound", err)
	}
	replayedClaim, err := repo.ClaimPendingBookingAttempt(ctx, PendingBookingRecord{
		SalonID:            salonID,
		Source:             SourceAIVoiceCall,
		Provider:           "square",
		POSIdempotencyKey:  uuid.NewString(),
		OperationKey:       "integration-superseded-attempt",
		RequestFingerprint: "integration-historical-duplicate",
		ProcessingToken:    uuid.NewString(),
		LeaseExpiresAt:     time.Now().UTC().Add(bookingOperationLeaseDuration),
		CustomerName:       "Integration Caller",
		CustomerPhone:      "+13125550199",
		Service:            ServiceRef{ID: serviceID, POSProvider: "square", POSServiceID: "integration-service", POSServiceVersion: 1, Name: "Integration Manicure", DurationMinutes: 45},
		Staff:              StaffRef{ID: staffID, POSProvider: "square", POSStaffID: "integration-staff", Name: "Integration Staff"},
		StaffSelectionMode: StaffSelectionSpecific,
		StartTime:          startTime,
		EndTime:            endTime,
	})
	if err != nil {
		t.Fatalf("replay superseded expired claim: %v", err)
	}
	if replayedClaim == nil || replayedClaim.Acquired || replayedClaim.Attempt == nil || replayedClaim.Attempt.ID != supersededAttemptID {
		t.Fatalf("replayed superseded claim = %#v, want same non-acquired attempt %s", replayedClaim, supersededAttemptID)
	}
	if err := repo.ExpireBookingOperationLeases(ctx, salonID); err != nil {
		t.Fatalf("sweep superseded expired claim: %v", err)
	}
	guardTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin superseded ensure guard transaction: %v", err)
	}
	if err := ensureReconciliationTaskTx(ctx, guardTx, salonID, supersededAttemptID, "This terminal task must not reopen."); err != nil {
		guardTx.Rollback()
		t.Fatalf("guard superseded reconciliation task: %v", err)
	}
	if err := guardTx.Commit(); err != nil {
		t.Fatalf("commit superseded ensure guard transaction: %v", err)
	}
	var persistedStatus, persistedOutcome, persistedProcessingToken, persistedReconciliation, persistedResolution string
	var persistedLease time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT status, provider_outcome, COALESCE(processing_token, ''), processing_lease_expires_at,
		       reconciliation_status, COALESCE(reconciliation_resolution, '')
		FROM booking_attempts
		WHERE id = $1
	`, supersededAttemptID).Scan(&persistedStatus, &persistedOutcome, &persistedProcessingToken, &persistedLease, &persistedReconciliation, &persistedResolution); err != nil {
		t.Fatalf("load replayed superseded attempt: %v", err)
	}
	if persistedStatus != StatusPOSPending || persistedOutcome != ProviderOutcomeNotStarted {
		t.Fatalf("superseded operation state = %s/%s, want pos_pending/not_started", persistedStatus, persistedOutcome)
	}
	if persistedProcessingToken != originalProcessingToken || !persistedLease.Equal(originalLease) {
		t.Fatalf("superseded lease changed to %q/%s, want %q/%s", persistedProcessingToken, persistedLease, originalProcessingToken, originalLease)
	}
	if persistedReconciliation != ReconciliationResolved || persistedResolution != ReconciliationResolutionSuperseded {
		t.Fatalf("superseded attempt reconciliation = %s/%s, want resolved/superseded", persistedReconciliation, persistedResolution)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT status, COALESCE(resolution, '')
		FROM booking_reconciliation_tasks
		WHERE booking_attempt_id = $1
	`, supersededAttemptID).Scan(&taskStatus, &taskResolution); err != nil {
		t.Fatalf("reload guarded superseded task: %v", err)
	}
	if taskStatus != "resolved" || taskResolution != ReconciliationResolutionSuperseded {
		t.Fatalf("guarded superseded task = %s/%s, want resolved/superseded", taskStatus, taskResolution)
	}
}

func TestRepositoryAvailabilityQuoteCleanupIsBoundedIdempotentAndPreservesAttemptAudit(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	now := time.Now().UTC().Truncate(time.Second)
	insertQuote := func(label string, createdAt time.Time, expiresAt time.Time, consumedAt *time.Time) string {
		t.Helper()
		var quoteID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO availability_quotes (
				salon_id, provider, provider_location_id, provider_snapshot_generation,
				request_fingerprint, expires_at, consumed_at, created_at
			)
			VALUES ($1, 'square', 'cleanup-location', 1, $2, $3, $4, $5)
			RETURNING id::text
		`, salonID, "cleanup-"+label+"-"+uuid.NewString(), expiresAt, consumedAt, createdAt).Scan(&quoteID); err != nil {
			t.Fatalf("insert %s quote: %v", label, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO availability_quote_slots (
				quote_id, slot_fingerprint, start_time, end_time, segments
			)
			VALUES ($1, $2, $3, $4, '[]'::jsonb)
		`, quoteID, strings.Repeat("a", 64), now.Add(time.Hour), now.Add(2*time.Hour)); err != nil {
			t.Fatalf("insert %s quote slot: %v", label, err)
		}
		return quoteID
	}

	oldConsumedAt := now.Add(-31 * 24 * time.Hour)
	recentConsumedAt := now.Add(-29 * 24 * time.Hour)
	orphanConsumedID := insertQuote("orphan-consumed", now.Add(-32*24*time.Hour), now.Add(-32*24*time.Hour+5*time.Minute), &oldConsumedAt)
	expiredID := insertQuote("expired", now.Add(-26*time.Hour), now.Add(-25*time.Hour), nil)
	graceProtectedID := insertQuote("grace-protected", now.Add(-24*time.Hour), now.Add(-23*time.Hour), nil)
	retentionProtectedID := insertQuote("retention-protected", now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour+5*time.Minute), &recentConsumedAt)
	attemptReferencedID := insertQuote("attempt-referenced", now.Add(-32*24*time.Hour), now.Add(-32*24*time.Hour+5*time.Minute), &oldConsumedAt)
	consumerReferencedID := insertQuote("consumer-referenced", now.Add(-32*24*time.Hour), now.Add(-32*24*time.Hour+5*time.Minute), &oldConsumedAt)

	var referencedAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, operation_key, request_fingerprint,
			availability_quote_id, availability_slot_fingerprint, operation_type,
			provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, staff_selection_mode,
			requested_start_time, requested_end_time
		)
		VALUES ($1, $2, $3, 'square', $4, $5, $6, $7, $8, $9, $10, $11,
		        'Retention Audit Caller', '+13125550222', $12, $13, $14)
		RETURNING id::text
	`, salonID, SourceOwnerDashboard, StatusFallbackPending, "cleanup-audit-"+uuid.NewString(), "cleanup-audit-"+uuid.NewString(),
		attemptReferencedID, strings.Repeat("a", 64), BookingActionBook, ProviderOutcomeFailed, RetryPolicySafe, ReconciliationNotRequired,
		StaffSelectionSpecific, now.Add(time.Hour), now.Add(2*time.Hour)).Scan(&referencedAttemptID); err != nil {
		t.Fatalf("insert quote-referencing attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE availability_quotes
		SET consumed_by_attempt_id = $1
		WHERE id = $2
	`, referencedAttemptID, consumerReferencedID); err != nil {
		t.Fatalf("link consumed quote to attempt: %v", err)
	}

	repo := NewRepository(db)
	unconsumedCutoff := now.Add(-availabilityQuoteUnconsumedGrace)
	consumedCutoff := now.Add(-availabilityQuoteConsumedAuditRetention)
	for run := 0; run < 2; run++ {
		deleted, err := repo.CleanupAvailabilityQuotes(ctx, unconsumedCutoff, consumedCutoff, 1)
		if err != nil {
			t.Fatalf("cleanup run %d: %v", run+1, err)
		}
		if deleted != 1 {
			t.Fatalf("cleanup run %d deleted = %d, want bounded 1", run+1, deleted)
		}
	}

	quoteExists := func(quoteID string) bool {
		t.Helper()
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM availability_quotes WHERE id = $1)`, quoteID).Scan(&exists); err != nil {
			t.Fatalf("check quote %s: %v", quoteID, err)
		}
		return exists
	}
	for _, deletedID := range []string{orphanConsumedID, expiredID} {
		if quoteExists(deletedID) {
			t.Fatalf("eligible quote %s still exists", deletedID)
		}
		var slots int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM availability_quote_slots WHERE quote_id = $1`, deletedID).Scan(&slots); err != nil {
			t.Fatalf("count deleted quote slots: %v", err)
		}
		if slots != 0 {
			t.Fatalf("deleted quote %s retained %d slots, want cascade cleanup", deletedID, slots)
		}
	}
	for _, protectedID := range []string{graceProtectedID, retentionProtectedID, attemptReferencedID, consumerReferencedID} {
		if !quoteExists(protectedID) {
			t.Fatalf("protected quote %s was deleted", protectedID)
		}
	}
	if deleted, err := repo.CleanupAvailabilityQuotes(ctx, unconsumedCutoff, consumedCutoff, 10); err != nil || deleted != 0 {
		t.Fatalf("idempotent cleanup = %d/%v, want 0/nil", deleted, err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM booking_attempts WHERE id = $1`, referencedAttemptID); err != nil {
		t.Fatalf("delete audit attempt: %v", err)
	}
	if deleted, err := repo.CleanupAvailabilityQuotes(ctx, unconsumedCutoff, consumedCutoff, 10); err != nil || deleted != 2 {
		t.Fatalf("orphaned consumed cleanup = %d/%v, want 2/nil", deleted, err)
	}
	for _, orphanedID := range []string{attemptReferencedID, consumerReferencedID} {
		if quoteExists(orphanedID) {
			t.Fatalf("orphaned consumed quote %s still exists after its audit reference was removed", orphanedID)
		}
	}
}

func TestRepositoryCreateReconciliationRequiresExactMirrorAndSupersedesImportedLedger(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, serviceID, staffID := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()
	startTime := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	endTime := startTime.Add(45 * time.Minute)
	var uncertainAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, operation_key, request_fingerprint,
			operation_type, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time, error_code, error_message
		)
		VALUES ($1, $2, $3, 'square', $4, $5, $6, $7, $8, $9,
		        'Integration Caller', '+13125550199', $10, $11, $12, $13, $14,
		        'POS_TIMEOUT', 'Provider response was unknown')
		RETURNING id::text
	`, salonID, SourceAIVoiceCall, StatusFallbackPending, "integration-reconcile-create", "integration-reconcile-fingerprint",
		BookingActionBook, ProviderOutcomeUnknown, RetryPolicyBlocked, ReconciliationRequired,
		serviceID, staffID, StaffSelectionSpecific, startTime, endTime).Scan(&uncertainAttemptID); err != nil {
		t.Fatalf("insert uncertain attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			booking_attempt_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, pos_service_version, name, duration_minutes, sort_order
		)
		VALUES ($1, $2, $3, $4, 'integration-service', 1, 'Integration Manicure', 45, 1)
	`, uncertainAttemptID, serviceID, staffID, StaffSelectionSpecific); err != nil {
		t.Fatalf("insert uncertain attempt segment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_tasks (salon_id, booking_attempt_id, status)
		VALUES ($1, $2, 'open')
	`, salonID, uncertainAttemptID); err != nil {
		t.Fatalf("insert reconciliation task: %v", err)
	}
	var importedAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			operation_type, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time,
			provider_location_id, provider_snapshot_generation
		)
		VALUES ($1, $2, $3, 'square', 'integration-provider-booking', 3,
		        $4, $5, $6, $7, 'Integration Caller', '+14155550199', $8, $9, $10, $11, $12,
		        'integration-location', 4)
		RETURNING id::text
	`, salonID, SourcePOSCalendarSync, StatusConfirmed, BookingActionBook, ProviderOutcomeSucceeded,
		RetryPolicyNone, ReconciliationNotRequired, serviceID, staffID, StaffSelectionSpecific, startTime, endTime).Scan(&importedAttemptID); err != nil {
		t.Fatalf("insert imported attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_tasks (salon_id, booking_attempt_id, status)
		VALUES ($1, $2, 'open')
	`, salonID, importedAttemptID); err != nil {
		t.Fatalf("insert imported reconciliation task: %v", err)
	}
	var appointmentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			status, customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
			start_time, end_time, pos_sync_status, last_pos_synced_at
		)
		VALUES ($1, $2, 'square', 'integration-provider-booking', 3,
		        $3, 'Integration Caller', '+14155550199', $4, $5, $6, $7, $8, 'synced', now())
		RETURNING id::text
	`, salonID, importedAttemptID, StatusConfirmed, serviceID, staffID, StaffSelectionSpecific, startTime, endTime).Scan(&appointmentID); err != nil {
		t.Fatalf("insert imported mirror: %v", err)
	}
	service := NewService(NewRepository(db), nil)
	req := ResolveReconciliationRequest{
		ActionKey:                  "integration-provider-attach",
		Action:                     ReconciliationActionProviderAttached,
		ProviderAppointmentID:      "integration-provider-booking",
		ProviderAppointmentVersion: 3,
		ProviderStatus:             "ACCEPTED",
		Note:                       "Verified against provider calendar.",
	}
	if _, err := service.ResolveReconciliation(ctx, salonID, ownerID, uncertainAttemptID, req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("conflicting customer mirror error = %v, want ErrOperationConflict", err)
	}
	conflictingCandidates, err := service.ReconciliationCandidates(ctx, salonID, ownerID, uncertainAttemptID)
	if err != nil {
		t.Fatalf("list conflicting reconciliation candidates: %v", err)
	}
	if len(conflictingCandidates.Candidates) != 0 {
		t.Fatalf("conflicting reconciliation candidates = %#v, want none", conflictingCandidates.Candidates)
	}
	if _, err := db.ExecContext(ctx, `UPDATE appointments SET customer_phone = '+13125550199' WHERE id = $1`, appointmentID); err != nil {
		t.Fatalf("align mirror customer phone: %v", err)
	}
	missingSegmentCandidates, err := service.ReconciliationCandidates(ctx, salonID, ownerID, uncertainAttemptID)
	if err != nil {
		t.Fatalf("list reconciliation candidates without provider segments: %v", err)
	}
	if len(missingSegmentCandidates.Candidates) != 0 {
		t.Fatalf("candidates without provider segments = %#v, want fail-closed empty result", missingSegmentCandidates.Candidates)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO appointment_services (
			appointment_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, pos_service_version, name, duration_minutes, sort_order
		)
		VALUES ($1, $2, $3, $4, 'integration-service', 1, 'Integration Manicure', 45, 1)
	`, appointmentID, serviceID, staffID, StaffSelectionSpecific); err != nil {
		t.Fatalf("insert imported appointment segment: %v", err)
	}
	exactCandidates, err := service.ReconciliationCandidates(ctx, salonID, ownerID, uncertainAttemptID)
	if err != nil {
		t.Fatalf("list exact reconciliation candidates: %v", err)
	}
	if len(exactCandidates.Candidates) != 1 || exactCandidates.Candidates[0].ProviderAppointmentID != req.ProviderAppointmentID || exactCandidates.Candidates[0].ProviderAppointmentVersion != req.ProviderAppointmentVersion || exactCandidates.Candidates[0].ProviderStatus != string(pos.AppointmentStatusAccepted) {
		t.Fatalf("exact reconciliation candidates = %#v, want provider-verified match", exactCandidates.Candidates)
	}
	if _, err := service.ResolveReconciliation(ctx, salonID, ownerID, uncertainAttemptID, ResolveReconciliationRequest{
		ActionKey: "integration-not-created-with-exact-provider-match",
		Action:    ReconciliationActionNotCreated,
		Note:      "This must be rejected because provider sync already found the exact booking.",
	}); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("not_created with exact provider match error = %v, want ErrOperationConflict", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE booking_attempts
		SET provider_location_id = 'different-location', provider_snapshot_generation = 1
		WHERE id = $1
	`, uncertainAttemptID); err != nil {
		t.Fatalf("set conflicting canonical provider fence: %v", err)
	}
	if _, err := service.ResolveReconciliation(ctx, salonID, ownerID, uncertainAttemptID, req); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("provider attach with conflicting origin fence error = %v, want ErrOperationConflict", err)
	}
	var conflictingLocation string
	if err := db.QueryRowContext(ctx, `SELECT provider_location_id FROM booking_attempts WHERE id = $1`, uncertainAttemptID).Scan(&conflictingLocation); err != nil {
		t.Fatalf("load conflicting canonical provider fence: %v", err)
	}
	if conflictingLocation != "different-location" {
		t.Fatalf("conflicting canonical provider location = %q, want unchanged", conflictingLocation)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE booking_attempts
		SET provider_location_id = NULL, provider_snapshot_generation = NULL
		WHERE id = $1
	`, uncertainAttemptID); err != nil {
		t.Fatalf("clear canonical provider fence for verified backfill: %v", err)
	}
	task, err := service.ResolveReconciliation(ctx, salonID, ownerID, uncertainAttemptID, req)
	if err != nil {
		t.Fatalf("resolve exact provider mirror: %v", err)
	}
	if task.Status != "resolved" || task.Resolution != ReconciliationActionProviderAttached {
		t.Fatalf("resolved task = %#v", task)
	}
	replayed, err := service.ResolveReconciliation(ctx, salonID, ownerID, uncertainAttemptID, req)
	if err != nil || replayed.Status != "resolved" {
		t.Fatalf("idempotent reconciliation replay = %#v, err=%v", replayed, err)
	}
	changed := req
	changed.Note = "A different payload under the same action key."
	if _, err := service.ResolveReconciliation(ctx, salonID, ownerID, uncertainAttemptID, changed); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("changed reconciliation replay error = %v, want ErrOperationConflict", err)
	}
	var linkedAttemptID string
	if err := db.QueryRowContext(ctx, `SELECT booking_attempt_id::text FROM appointments WHERE id = $1`, appointmentID).Scan(&linkedAttemptID); err != nil {
		t.Fatalf("load relinked appointment: %v", err)
	}
	if linkedAttemptID != uncertainAttemptID {
		t.Fatalf("appointment booking attempt = %s, want %s", linkedAttemptID, uncertainAttemptID)
	}
	var canonicalProviderLocationID string
	var canonicalProviderGeneration int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(provider_location_id, ''), COALESCE(provider_snapshot_generation, 0)
		FROM booking_attempts
		WHERE id = $1
	`, uncertainAttemptID).Scan(&canonicalProviderLocationID, &canonicalProviderGeneration); err != nil {
		t.Fatalf("load canonical attempt provider fence: %v", err)
	}
	if canonicalProviderLocationID != "integration-location" || canonicalProviderGeneration != 4 {
		t.Fatalf("canonical provider fence = %s/%d, want integration-location/4", canonicalProviderLocationID, canonicalProviderGeneration)
	}
	var supersededBy, importedAttemptReconciliation, importedAttemptResolution string
	var superseded bool
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(superseded_by_attempt_id::text, ''), superseded_at IS NOT NULL,
		       reconciliation_status, COALESCE(reconciliation_resolution, '')
		FROM booking_attempts WHERE id = $1
	`, importedAttemptID).Scan(&supersededBy, &superseded, &importedAttemptReconciliation, &importedAttemptResolution); err != nil {
		t.Fatalf("load imported attempt lineage: %v", err)
	}
	if supersededBy != uncertainAttemptID || !superseded {
		t.Fatalf("imported attempt lineage = %s/%t, want %s/true", supersededBy, superseded, uncertainAttemptID)
	}
	if importedAttemptReconciliation != ReconciliationResolved || importedAttemptResolution != ReconciliationResolutionSuperseded {
		t.Fatalf("imported attempt reconciliation = %s/%s, want resolved/superseded", importedAttemptReconciliation, importedAttemptResolution)
	}
	var importedTaskStatus, importedTaskResolution string
	var importedTaskResolved bool
	if err := db.QueryRowContext(ctx, `
		SELECT status, COALESCE(resolution, ''), resolved_at IS NOT NULL
		FROM booking_reconciliation_tasks
		WHERE booking_attempt_id = $1
	`, importedAttemptID).Scan(&importedTaskStatus, &importedTaskResolution, &importedTaskResolved); err != nil {
		t.Fatalf("load imported reconciliation task: %v", err)
	}
	if importedTaskStatus != "resolved" || importedTaskResolution != ReconciliationResolutionSuperseded || !importedTaskResolved {
		t.Fatalf("imported reconciliation task = %s/%s/%t, want resolved/superseded/true", importedTaskStatus, importedTaskResolution, importedTaskResolved)
	}
	var importedSupersededEventCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM booking_reconciliation_events
		WHERE booking_attempt_id = $1
		  AND action = $2
		  AND action_key = $2 || ':' || $3
	`, importedAttemptID, ReconciliationResolutionSuperseded, uncertainAttemptID).Scan(&importedSupersededEventCount); err != nil {
		t.Fatalf("count imported superseded events: %v", err)
	}
	if importedSupersededEventCount != 1 {
		t.Fatalf("imported superseded events = %d, want 1", importedSupersededEventCount)
	}
}

func TestRepositoryDirectAppointmentMutationIsIdempotentAtEqualVersionAndRejectsNewerCalendarTruth(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, serviceID, staffID := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	baseStart := time.Now().UTC().Add(144 * time.Hour).Truncate(time.Second)
	baseEnd := baseStart.Add(45 * time.Minute)
	providerBookingID := "direct-version-booking-" + uuid.NewString()
	var originalAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			operation_type, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time
		)
		VALUES ($1, $2, $3, 'square', $4, 7, $5, $6, $7, $8,
		        'Direct Version Caller', '+13125550444', $9, $10, $11, $12, $13)
		RETURNING id::text
	`, salonID, SourceOwnerDashboard, StatusConfirmed, providerBookingID, BookingActionBook,
		ProviderOutcomeSucceeded, RetryPolicyNone, ReconciliationNotRequired,
		serviceID, staffID, StaffSelectionSpecific, baseStart, baseEnd).Scan(&originalAttemptID); err != nil {
		t.Fatalf("insert original booking attempt: %v", err)
	}
	var appointmentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			status, customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
			start_time, end_time, pos_sync_status, last_pos_synced_at
		)
		VALUES ($1, $2, 'square', $3, 7, $4, 'Direct Version Caller', '+13125550444',
		        $5, $6, $7, $8, $9, 'synced', now())
		RETURNING id::text
	`, salonID, originalAttemptID, providerBookingID, StatusConfirmed, serviceID, staffID, StaffSelectionSpecific, baseStart, baseEnd).Scan(&appointmentID); err != nil {
		t.Fatalf("insert direct mutation appointment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO appointment_services (
			appointment_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, pos_service_version, name, duration_minutes, sort_order
		)
		VALUES ($1, $2, $3, $4, 'integration-service', 1, 'Integration Manicure', 45, 1)
	`, appointmentID, serviceID, staffID, StaffSelectionSpecific); err != nil {
		t.Fatalf("insert direct mutation segment: %v", err)
	}

	repo := NewRepository(db)
	serviceRef := ServiceRef{ID: serviceID, POSProvider: pos.ProviderSquare, POSServiceID: "integration-service", POSServiceVersion: 1, Name: "Integration Manicure", DurationMinutes: 45}
	staffRef := StaffRef{ID: staffID, POSProvider: pos.ProviderSquare, POSStaffID: "integration-staff", Name: "Integration Staff"}
	segment := BookingSegmentRecord{Service: serviceRef, Staff: staffRef, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1}
	appointmentRef := AppointmentActionRef{
		ID: appointmentID, SalonID: salonID, BookingAttemptID: originalAttemptID,
		POSProvider: pos.ProviderSquare, POSAppointmentID: providerBookingID, POSAppointmentVersion: 7,
		Status: StatusConfirmed, CustomerName: "Direct Version Caller", CustomerPhone: "+13125550444",
		Service: serviceRef, Staff: staffRef, StaffSelectionMode: StaffSelectionSpecific,
		Segments: []BookingSegmentRecord{segment}, StartTime: baseStart, EndTime: baseEnd,
	}
	rescheduledStart := baseStart.Add(24 * time.Hour)
	rescheduledEnd := rescheduledStart.Add(45 * time.Minute)
	rescheduleToken := uuid.NewString()
	rescheduleClaim, err := repo.ClaimPendingAppointmentAction(ctx, PendingAppointmentActionRecord{
		SalonID: salonID, Appointment: appointmentRef, Provider: pos.ProviderSquare,
		Source: SourceOwnerDashboard, Segments: []BookingSegmentRecord{segment},
		RequestedStartTime: rescheduledStart, RequestedEndTime: rescheduledEnd,
		POSIdempotencyKey: uuid.NewString(), OperationKey: uuid.NewString(),
		RequestFingerprint: strings.Repeat("a", 64), OperationType: BookingActionReschedule,
		ProcessingToken: rescheduleToken, LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil || rescheduleClaim == nil || !rescheduleClaim.Acquired {
		t.Fatalf("claim direct reschedule = %#v/%v", rescheduleClaim, err)
	}
	if err := repo.MarkBookingOperationStarted(ctx, salonID, rescheduleClaim.Attempt.ID, rescheduleToken, time.Now().UTC().Add(5*time.Minute)); err != nil {
		t.Fatalf("mark direct reschedule started: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE appointments
		SET pos_appointment_version = 8, status = $2, start_time = $3, end_time = $4
		WHERE id = $1
	`, appointmentID, StatusConfirmed, rescheduledStart, rescheduledEnd); err != nil {
		t.Fatalf("simulate equal-version calendar save: %v", err)
	}
	rescheduled, err := repo.SaveRescheduledAppointment(ctx, RescheduledAppointmentRecord{
		AttemptID: rescheduleClaim.Attempt.ID, Appointment: appointmentRef, Staff: staffRef,
		Source: SourceOwnerDashboard, Segments: []BookingSegmentRecord{segment},
		StartTime: rescheduledStart, EndTime: rescheduledEnd, POSBookingVersion: 8,
		ProcessingToken: rescheduleToken,
	})
	if err != nil {
		t.Fatalf("save equal-version reschedule: %v", err)
	}
	if rescheduled.POSAppointmentVersion != 8 || rescheduled.Status != StatusRescheduled {
		t.Fatalf("equal-version reschedule = %#v, want v8 rescheduled", rescheduled)
	}

	currentRef := appointmentRef
	currentRef.POSAppointmentVersion = 8
	currentRef.Status = StatusRescheduled
	currentRef.StartTime = rescheduledStart
	currentRef.EndTime = rescheduledEnd
	cancelToken := uuid.NewString()
	cancelClaim, err := repo.ClaimPendingAppointmentAction(ctx, PendingAppointmentActionRecord{
		SalonID: salonID, Appointment: currentRef, Provider: pos.ProviderSquare,
		Source: SourceOwnerDashboard, Segments: []BookingSegmentRecord{segment},
		RequestedStartTime: rescheduledStart, RequestedEndTime: rescheduledEnd,
		POSIdempotencyKey: uuid.NewString(), OperationKey: uuid.NewString(),
		RequestFingerprint: strings.Repeat("b", 64), OperationType: BookingActionCancel,
		ProcessingToken: cancelToken, LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil || cancelClaim == nil || !cancelClaim.Acquired {
		t.Fatalf("claim direct cancellation = %#v/%v", cancelClaim, err)
	}
	if err := repo.MarkBookingOperationStarted(ctx, salonID, cancelClaim.Attempt.ID, cancelToken, time.Now().UTC().Add(5*time.Minute)); err != nil {
		t.Fatalf("mark direct cancellation started: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE appointments SET pos_appointment_version = 10, status = $2 WHERE id = $1
	`, appointmentID, StatusCancelled); err != nil {
		t.Fatalf("simulate newer calendar truth: %v", err)
	}
	if _, err := repo.SaveCancelledAppointment(ctx, CancelledAppointmentRecord{
		AttemptID: cancelClaim.Attempt.ID, Appointment: currentRef, Source: SourceOwnerDashboard,
		Reason: "Direct version conflict", POSBookingVersion: 9, ProcessingToken: cancelToken,
	}); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("save cancellation behind newer calendar error = %v, want ErrOperationConflict", err)
	}
	var finalVersion int
	var finalStatus string
	if err := db.QueryRowContext(ctx, `SELECT pos_appointment_version, status FROM appointments WHERE id = $1`, appointmentID).Scan(&finalVersion, &finalStatus); err != nil {
		t.Fatalf("load final direct mutation appointment: %v", err)
	}
	if finalVersion != 10 || finalStatus != StatusCancelled {
		t.Fatalf("newer calendar truth = %d/%s, want 10/cancelled", finalVersion, finalStatus)
	}
}

func TestRepositoryCalendarImportVersionGuardRejectsStaleAndSerializesConcurrentUpdates(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	fence := seedReadyCalendarProviderFence(t, ctx, db, salonID)
	appointmentID := "integration-versioned-calendar-booking"
	baseStart := time.Now().UTC().Add(96 * time.Hour).Truncate(time.Second)
	item := func(version int, status string, start time.Time) CalendarAppointmentImport {
		return CalendarAppointmentImport{
			Provider:              pos.ProviderSquare,
			POSAppointmentID:      appointmentID,
			POSAppointmentVersion: version,
			Status:                status,
			CustomerName:          "Versioned Calendar Caller",
			CustomerPhone:         "+13125550200",
			StartTime:             start,
			EndTime:               start.Add(45 * time.Minute),
			Segments: []CalendarAppointmentSegmentImport{{
				POSServiceID:      "integration-service",
				POSServiceVersion: 1,
				POSStaffID:        "integration-staff",
				DurationMinutes:   45,
			}},
		}
	}

	if summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{item(2, StatusConfirmed, baseStart)}); err != nil || summary.Imported != 1 {
		t.Fatalf("initial version import summary/error = %#v/%v", summary, err)
	}
	latestStart := baseStart.Add(2 * time.Hour)
	if summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{item(3, StatusCancelled, latestStart)}); err != nil || summary.Updated != 1 {
		t.Fatalf("newer version import summary/error = %#v/%v", summary, err)
	}
	for _, stale := range []CalendarAppointmentImport{
		item(2, StatusConfirmed, baseStart.Add(4*time.Hour)),
		item(3, StatusConfirmed, baseStart.Add(6*time.Hour)),
	} {
		summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{stale})
		if err != nil || summary.Skipped != 1 || summary.Updated != 0 {
			t.Fatalf("stale/equal import summary/error = %#v/%v, want skipped", summary, err)
		}
	}

	assertCalendarVersion := func(wantVersion int, wantStatus string, wantStart time.Time) {
		t.Helper()
		var appointmentVersion, attemptVersion int
		var appointmentStatus, attemptStatus string
		var appointmentStart time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT appointment.pos_appointment_version, appointment.status, appointment.start_time,
			       attempt.pos_booking_version, attempt.status
			FROM appointments appointment
			JOIN booking_attempts attempt ON attempt.id = appointment.booking_attempt_id
			WHERE appointment.salon_id = $1
			  AND appointment.pos_provider = $2
			  AND appointment.pos_appointment_id = $3
		`, salonID, pos.ProviderSquare, appointmentID).Scan(
			&appointmentVersion, &appointmentStatus, &appointmentStart, &attemptVersion, &attemptStatus,
		); err != nil {
			t.Fatalf("load versioned calendar mirror: %v", err)
		}
		if appointmentVersion != wantVersion || attemptVersion != wantVersion || appointmentStatus != wantStatus || attemptStatus != wantStatus || !appointmentStart.Equal(wantStart) {
			t.Fatalf("calendar mirror = appointment %d/%s/%s attempt %d/%s, want %d/%s/%s", appointmentVersion, appointmentStatus, appointmentStart, attemptVersion, attemptStatus, wantVersion, wantStatus, wantStart)
		}
	}
	assertCalendarVersion(3, StatusCancelled, latestStart)

	concurrentItems := []CalendarAppointmentImport{
		item(4, StatusConfirmed, baseStart.Add(8*time.Hour)),
		item(5, StatusCancelled, baseStart.Add(10*time.Hour)),
	}
	errs := make([]error, len(concurrentItems))
	var wg sync.WaitGroup
	for index := range concurrentItems {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = repo.UpsertCalendarAppointments(context.Background(), salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{concurrentItems[index]})
		}(index)
	}
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent import %d: %v", index, err)
		}
	}
	assertCalendarVersion(5, StatusCancelled, baseStart.Add(10*time.Hour))
}

func TestRepositoryCalendarEqualVersionConflictDoesNotEnrichOrResolvePendingAction(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	fence := seedReadyCalendarProviderFence(t, ctx, db, salonID)
	suffix := uuid.NewString()
	providerAppointmentID := "equal-version-conflict-" + suffix
	providerServiceID := "equal-version-service-" + suffix
	providerStaffID := "equal-version-staff-" + suffix
	providerCustomerID := "equal-version-customer-" + suffix
	originalStart := time.Now().UTC().Add(168 * time.Hour).Truncate(time.Second)
	originalEnd := originalStart.Add(45 * time.Minute)
	initial := CalendarAppointmentImport{
		Provider:              pos.ProviderSquare,
		POSAppointmentID:      providerAppointmentID,
		POSAppointmentVersion: 5,
		Status:                StatusConfirmed,
		POSCustomerID:         providerCustomerID,
		StartTime:             originalStart,
		EndTime:               originalEnd,
		Segments: []CalendarAppointmentSegmentImport{{
			POSServiceID:      providerServiceID,
			POSServiceVersion: 7,
			POSStaffID:        providerStaffID,
			DurationMinutes:   45,
		}},
	}
	if summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{initial}); err != nil || summary.Imported != 1 {
		t.Fatalf("initial equal-version conflict import summary/error = %#v/%v", summary, err)
	}
	var appointmentID, mirrorAttemptID string
	if err := db.QueryRowContext(ctx, `
		SELECT id::text, booking_attempt_id::text
		FROM appointments
		WHERE salon_id = $1 AND pos_provider = $2 AND pos_appointment_id = $3
	`, salonID, pos.ProviderSquare, providerAppointmentID).Scan(&appointmentID, &mirrorAttemptID); err != nil {
		t.Fatalf("load initial calendar mirror: %v", err)
	}

	var serviceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, pos_service_version,
			name, duration_minutes, sync_status, source
		)
		VALUES ($1, $2, $3, 7, 'Equal Version Service', 45, 'synced', 'imported')
		RETURNING id::text
	`, salonID, pos.ProviderSquare, providerServiceID).Scan(&serviceID); err != nil {
		t.Fatalf("insert equal-version service mapping: %v", err)
	}
	var staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, sync_status, source)
		VALUES ($1, $2, $3, 'Equal Version Staff', 'synced', 'imported')
		RETURNING id::text
	`, salonID, pos.ProviderSquare, providerStaffID).Scan(&staffID); err != nil {
		t.Fatalf("insert equal-version staff mapping: %v", err)
	}
	var customerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customers (
			salon_id, name, phone, normalized_phone, email, normalized_email,
			sync_status, source
		)
		VALUES ($1, 'Equal Version Customer', '+13125550988', '+13125550988',
		        'equal-version@example.com', 'equal-version@example.com', 'synced', 'imported')
		RETURNING id::text
	`, salonID).Scan(&customerID); err != nil {
		t.Fatalf("insert equal-version customer mapping: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id,
			sync_status, last_synced_at
		)
		VALUES ($1, 'customer', $2, $3, $4, 'synced', now())
	`, salonID, customerID, pos.ProviderSquare, providerCustomerID); err != nil {
		t.Fatalf("insert equal-version customer link: %v", err)
	}

	requestedStart := originalStart.Add(2 * time.Hour)
	requestedEnd := requestedStart.Add(45 * time.Minute)
	var pendingAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			operation_type, target_appointment_id, target_pos_booking_version,
			provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time, error_code, error_message
		)
		VALUES ($1, $2, $3, $4, $5, 4, $6, $7, 4, $8, $9, $10,
		        'Equal Version Customer', '+13125550988', $11, $12, $13, $14, $15,
		        'POS_TIMEOUT', 'Provider result was unknown.')
		RETURNING id::text
	`, salonID, SourceOwnerDashboard, StatusFallbackPending, pos.ProviderSquare, providerAppointmentID,
		BookingActionReschedule, appointmentID, ProviderOutcomeUnknown, RetryPolicyBlocked, ReconciliationRequired,
		serviceID, staffID, StaffSelectionSpecific, requestedStart, requestedEnd).Scan(&pendingAttemptID); err != nil {
		t.Fatalf("insert pending equal-version action: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			booking_attempt_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, pos_service_version, name, duration_minutes, sort_order
		)
		VALUES ($1, $2, $3, $4, $5, 7, 'Equal Version Service', 45, 1)
	`, pendingAttemptID, serviceID, staffID, StaffSelectionSpecific, providerServiceID); err != nil {
		t.Fatalf("insert pending equal-version action segment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_reconciliation_tasks (salon_id, booking_attempt_id, status)
		VALUES ($1, $2, 'open')
	`, salonID, pendingAttemptID); err != nil {
		t.Fatalf("insert pending equal-version reconciliation task: %v", err)
	}

	conflicting := initial
	conflicting.Status = StatusRescheduled
	conflicting.StartTime = requestedStart
	conflicting.EndTime = requestedEnd
	summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{conflicting})
	if err != nil || summary.Skipped != 1 || summary.Updated != 0 {
		t.Fatalf("conflicting equal-version summary/error = %#v/%v, want skipped", summary, err)
	}
	var appointmentStatus, appointmentCustomerName, appointmentCustomerPhone, appointmentCustomerEmail string
	var appointmentServiceID, appointmentStaffID, segmentServiceID, segmentStaffID string
	var mirrorCustomerName, mirrorCustomerPhone, mirrorCustomerEmail, mirrorServiceID, mirrorStaffID string
	var appointmentStart, appointmentEnd time.Time
	var appointmentVersion int
	if err := db.QueryRowContext(ctx, `
		SELECT appointment.status, appointment.start_time, appointment.end_time,
		       COALESCE(appointment.pos_appointment_version, 0), appointment.customer_name,
		       appointment.customer_phone, COALESCE(appointment.customer_email, ''),
		       COALESCE(appointment.service_id::text, ''),
		       COALESCE(appointment.staff_id::text, ''), COALESCE(segment.service_id::text, ''),
		       COALESCE(segment.staff_id::text, ''), mirror.customer_name, mirror.customer_phone,
		       COALESCE(mirror.customer_email, ''), COALESCE(mirror.service_id::text, ''),
		       COALESCE(mirror.staff_id::text, '')
		FROM appointments appointment
		JOIN booking_attempts mirror ON mirror.id = appointment.booking_attempt_id
		JOIN appointment_services segment
		  ON segment.appointment_id = appointment.id AND segment.sort_order = 1
		WHERE appointment.id = $1
	`, appointmentID).Scan(
		&appointmentStatus,
		&appointmentStart,
		&appointmentEnd,
		&appointmentVersion,
		&appointmentCustomerName,
		&appointmentCustomerPhone,
		&appointmentCustomerEmail,
		&appointmentServiceID,
		&appointmentStaffID,
		&segmentServiceID,
		&segmentStaffID,
		&mirrorCustomerName,
		&mirrorCustomerPhone,
		&mirrorCustomerEmail,
		&mirrorServiceID,
		&mirrorStaffID,
	); err != nil {
		t.Fatalf("load conflicting equal-version mirror: %v", err)
	}
	if appointmentStatus != StatusConfirmed || appointmentVersion != initial.POSAppointmentVersion || !appointmentStart.Equal(originalStart) || !appointmentEnd.Equal(originalEnd) {
		t.Fatalf("conflicting equal-version mirror truth = %s/v%d/%s-%s, want confirmed/v5/%s-%s", appointmentStatus, appointmentVersion, appointmentStart, appointmentEnd, originalStart, originalEnd)
	}
	if appointmentCustomerName != "Square customer" || appointmentCustomerPhone != "" || appointmentCustomerEmail != "" || appointmentServiceID != "" || appointmentStaffID != "" || segmentServiceID != "" || segmentStaffID != "" || mirrorCustomerName != "Square customer" || mirrorCustomerPhone != "" || mirrorCustomerEmail != "" || mirrorServiceID != "" || mirrorStaffID != "" {
		t.Fatalf("conflicting equal-version payload enriched appointment=%q/%q/%q service=%s/%s staff=%s/%s mirror=%q/%q/%q/%s/%s", appointmentCustomerName, appointmentCustomerPhone, appointmentCustomerEmail, appointmentServiceID, segmentServiceID, appointmentStaffID, segmentStaffID, mirrorCustomerName, mirrorCustomerPhone, mirrorCustomerEmail, mirrorServiceID, mirrorStaffID)
	}
	var pendingStatus, pendingReconciliation, pendingResolution, taskStatus, taskResolution string
	if err := db.QueryRowContext(ctx, `
		SELECT attempt.status, attempt.reconciliation_status,
		       COALESCE(attempt.reconciliation_resolution, ''), task.status,
		       COALESCE(task.resolution, '')
		FROM booking_attempts attempt
		JOIN booking_reconciliation_tasks task ON task.booking_attempt_id = attempt.id
		WHERE attempt.id = $1
	`, pendingAttemptID).Scan(&pendingStatus, &pendingReconciliation, &pendingResolution, &taskStatus, &taskResolution); err != nil {
		t.Fatalf("load pending equal-version action: %v", err)
	}
	if pendingStatus != StatusFallbackPending || pendingReconciliation != ReconciliationRequired || pendingResolution != "" || taskStatus != "open" || taskResolution != "" {
		t.Fatalf("pending equal-version action changed to %s/%s/%s task=%s/%s", pendingStatus, pendingReconciliation, pendingResolution, taskStatus, taskResolution)
	}
	var resolutionEventCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM booking_reconciliation_events
		WHERE booking_attempt_id = $1 AND action = $2
	`, pendingAttemptID, ReconciliationActionProviderAttached).Scan(&resolutionEventCount); err != nil {
		t.Fatalf("count conflicting equal-version resolution events: %v", err)
	}
	if resolutionEventCount != 0 {
		t.Fatalf("conflicting equal-version resolution events = %d, want 0", resolutionEventCount)
	}
}

func TestEqualVersionCalendarSnapshotMatchesRequiresImmutableProviderTruth(t *testing.T) {
	start := time.Date(2026, time.July, 20, 15, 0, 0, 0, time.UTC)
	fence := pos.ProviderFence{LocationID: "provider-location", SnapshotGeneration: 8}
	item := CalendarAppointmentImport{
		Provider:              pos.ProviderSquare,
		POSAppointmentID:      "provider-appointment",
		POSAppointmentVersion: 8,
		POSCustomerID:         "provider-customer",
		Status:                StatusConfirmed,
		StartTime:             start,
		EndTime:               start.Add(45 * time.Minute),
	}
	segments := []calendarImportSegmentRef{{
		POSServiceID:      "provider-service",
		POSServiceVersion: 3,
		POSStaffID:        "provider-staff",
		DurationMinutes:   45,
		SortOrder:         1,
	}}
	snapshot := calendarAppointmentSnapshot{
		Provider:              item.Provider,
		ProviderLocationID:    fence.LocationID,
		ProviderGeneration:    fence.SnapshotGeneration,
		POSAppointmentID:      item.POSAppointmentID,
		POSAppointmentVersion: item.POSAppointmentVersion,
		POSCustomerID:         item.POSCustomerID,
		Status:                item.Status,
		StartTime:             item.StartTime,
		EndTime:               item.EndTime,
		Segments: []calendarAppointmentSegmentSnapshot{{
			POSServiceID:      segments[0].POSServiceID,
			POSServiceVersion: segments[0].POSServiceVersion,
			POSStaffID:        segments[0].POSStaffID,
			DurationMinutes:   segments[0].DurationMinutes,
			SortOrder:         segments[0].SortOrder,
		}},
	}
	if !equalVersionCalendarSnapshotMatches(snapshot, item, segments, fence) {
		t.Fatal("identical persisted and incoming provider snapshots did not match")
	}
	conflictingStatus := snapshot
	conflictingStatus.Status = StatusCancelled
	if equalVersionCalendarSnapshotMatches(conflictingStatus, item, segments, fence) {
		t.Fatal("conflicting persisted status matched equal-version payload")
	}
	conflictingTime := snapshot
	conflictingTime.StartTime = conflictingTime.StartTime.Add(time.Minute)
	if equalVersionCalendarSnapshotMatches(conflictingTime, item, segments, fence) {
		t.Fatal("conflicting persisted time matched equal-version payload")
	}
	conflictingSegments := snapshot
	conflictingSegments.Segments = append([]calendarAppointmentSegmentSnapshot(nil), snapshot.Segments...)
	conflictingSegments.Segments[0].POSServiceVersion++
	if equalVersionCalendarSnapshotMatches(conflictingSegments, item, segments, fence) {
		t.Fatal("conflicting persisted raw segment matched equal-version payload")
	}
	conflictingCustomer := snapshot
	conflictingCustomer.POSCustomerID = "other-provider-customer"
	if equalVersionCalendarSnapshotMatches(conflictingCustomer, item, segments, fence) {
		t.Fatal("conflicting persisted raw customer identity matched equal-version payload")
	}
	conflictingStaff := snapshot
	conflictingStaff.Segments = append([]calendarAppointmentSegmentSnapshot(nil), snapshot.Segments...)
	conflictingStaff.Segments[0].POSStaffID = "other-provider-staff"
	if equalVersionCalendarSnapshotMatches(conflictingStaff, item, segments, fence) {
		t.Fatal("conflicting persisted raw staff identity matched equal-version payload")
	}
	legacyUnknownIdentities := snapshot
	legacyUnknownIdentities.POSCustomerID = ""
	legacyUnknownIdentities.Segments = append([]calendarAppointmentSegmentSnapshot(nil), snapshot.Segments...)
	legacyUnknownIdentities.Segments[0].POSStaffID = ""
	if !equalVersionCalendarSnapshotMatches(legacyUnknownIdentities, item, segments, fence) {
		t.Fatal("unknown historical raw identities should not fabricate an equal-version conflict")
	}
	conflictingLocation := snapshot
	conflictingLocation.ProviderLocationID = "other-location"
	if equalVersionCalendarSnapshotMatches(conflictingLocation, item, segments, fence) {
		t.Fatal("conflicting persisted provider location matched equal-version payload")
	}
	legacyUnknownLocation := snapshot
	legacyUnknownLocation.ProviderLocationID = ""
	legacyUnknownLocation.ProviderGeneration = 0
	if !equalVersionCalendarSnapshotMatches(legacyUnknownLocation, item, segments, fence) {
		t.Fatal("legacy missing origin location should be eligible for exact equal-version enrichment")
	}
}

func TestConfirmedBookingMirrorMatchRequiresExactAuthoritativeSnapshot(t *testing.T) {
	start := time.Date(2026, time.July, 21, 15, 0, 0, 0, time.UTC)
	service := ServiceRef{ID: "service-1", POSServiceID: "provider-service-1", POSServiceVersion: 9, DurationMinutes: 45}
	staff := StaffRef{ID: "staff-1", POSStaffID: "provider-staff-1"}
	segments := []BookingSegmentRecord{{
		Service:            service,
		Staff:              staff,
		StaffSelectionMode: StaffSelectionSpecific,
		SortOrder:          1,
	}}
	record := ConfirmedBookingRecord{
		AttemptID:          "attempt-1",
		SalonID:            "salon-1",
		Provider:           pos.ProviderSquare,
		POSCustomerID:      "provider-customer-1",
		Service:            service,
		Staff:              staff,
		StaffSelectionMode: StaffSelectionSpecific,
		Segments:           segments,
		StartTime:          start,
		EndTime:            start.Add(45 * time.Minute),
		POSBookingID:       "provider-booking-1",
		POSBookingVersion:  4,
		ProviderFence:      pos.ProviderFence{LocationID: "location-1", SnapshotGeneration: 7},
	}
	snapshot := calendarAppointmentSnapshot{
		AppointmentID:         "appointment-1",
		BookingAttemptID:      "mirror-attempt-1",
		OriginSource:          SourcePOSCalendarSync,
		Provider:              pos.ProviderSquare,
		ProviderLocationID:    "location-1",
		ProviderGeneration:    8,
		POSAppointmentID:      "provider-booking-1",
		POSAppointmentVersion: 4,
		POSCustomerID:         "provider-customer-1",
		Status:                StatusConfirmed,
		ServiceID:             "service-1",
		StaffID:               "staff-1",
		StaffSelectionMode:    StaffSelectionSpecific,
		StartTime:             start,
		EndTime:               start.Add(45 * time.Minute),
		Segments: []calendarAppointmentSegmentSnapshot{{
			ServiceID:          "service-1",
			POSServiceID:       "provider-service-1",
			POSServiceVersion:  9,
			StaffID:            "staff-1",
			POSStaffID:         "provider-staff-1",
			StaffSelectionMode: StaffSelectionSpecific,
			DurationMinutes:    45,
			SortOrder:          1,
		}},
	}
	if !confirmedBookingMirrorMatches(snapshot, record, segments) {
		t.Fatal("exact synchronized mirror should converge with the claimed create attempt")
	}
	tests := []struct {
		name   string
		mutate func(*calendarAppointmentSnapshot)
	}{
		{name: "different location", mutate: func(item *calendarAppointmentSnapshot) { item.ProviderLocationID = "location-2" }},
		{name: "different version", mutate: func(item *calendarAppointmentSnapshot) { item.POSAppointmentVersion++ }},
		{name: "different status", mutate: func(item *calendarAppointmentSnapshot) { item.Status = StatusCancelled }},
		{name: "different range", mutate: func(item *calendarAppointmentSnapshot) { item.EndTime = item.EndTime.Add(5 * time.Minute) }},
		{name: "different raw service", mutate: func(item *calendarAppointmentSnapshot) { item.Segments[0].POSServiceID = "provider-service-2" }},
		{name: "different raw staff", mutate: func(item *calendarAppointmentSnapshot) { item.Segments[0].POSStaffID = "provider-staff-2" }},
		{name: "non calendar origin", mutate: func(item *calendarAppointmentSnapshot) { item.OriginSource = SourceAIVoiceCall }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := snapshot
			candidate.Segments = append([]calendarAppointmentSegmentSnapshot(nil), snapshot.Segments...)
			test.mutate(&candidate)
			if confirmedBookingMirrorMatches(candidate, record, segments) {
				t.Fatal("conflicting provider mirror must fail closed")
			}
		})
	}
}

func TestFallbackCreateMirrorMatchRequiresKnownExactAcceptedBooking(t *testing.T) {
	start := time.Date(2026, time.July, 22, 15, 0, 0, 0, time.UTC)
	service := ServiceRef{ID: "service-1", POSServiceID: "provider-service-1", POSServiceVersion: 5, DurationMinutes: 30}
	staff := StaffRef{ID: "staff-1", POSStaffID: "provider-staff-1"}
	segments := []BookingSegmentRecord{{Service: service, Staff: staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1}}
	record := FallbackBookingRecord{
		Provider:           pos.ProviderSquare,
		POSBookingID:       "provider-booking-1",
		POSBookingVersion:  3,
		Service:            service,
		Staff:              staff,
		StaffSelectionMode: StaffSelectionSpecific,
		Segments:           segments,
		StartTime:          start,
		EndTime:            start.Add(30 * time.Minute),
	}
	snapshot := calendarAppointmentSnapshot{
		OriginSource:          SourcePOSCalendarSync,
		Provider:              pos.ProviderSquare,
		ProviderLocationID:    "location-1",
		ProviderGeneration:    9,
		POSAppointmentID:      "provider-booking-1",
		POSAppointmentVersion: 4,
		Status:                StatusConfirmed,
		ServiceID:             service.ID,
		StaffID:               staff.ID,
		StaffSelectionMode:    StaffSelectionSpecific,
		StartTime:             start,
		EndTime:               start.Add(30 * time.Minute),
		Segments: []calendarAppointmentSegmentSnapshot{{
			ServiceID:          service.ID,
			POSServiceID:       service.POSServiceID,
			POSServiceVersion:  service.POSServiceVersion,
			StaffID:            staff.ID,
			POSStaffID:         staff.POSStaffID,
			StaffSelectionMode: StaffSelectionSpecific,
			DurationMinutes:    service.DurationMinutes,
			SortOrder:          1,
		}},
	}
	fence := pos.ProviderFence{LocationID: "location-1", SnapshotGeneration: 7}
	if !fallbackCreateMirrorMatches(snapshot, record, fence, segments) {
		t.Fatal("newer exact accepted mirror should finalize the known fallback booking")
	}
	missingID := record
	missingID.POSBookingID = ""
	if fallbackCreateMirrorMatches(snapshot, missingID, fence, segments) {
		t.Fatal("fallback create without a known provider booking ID must not converge")
	}
	conflictingRaw := snapshot
	conflictingRaw.Segments = append([]calendarAppointmentSegmentSnapshot(nil), snapshot.Segments...)
	conflictingRaw.Segments[0].POSStaffID = "provider-staff-2"
	if fallbackCreateMirrorMatches(conflictingRaw, record, fence, segments) {
		t.Fatal("fallback create with conflicting raw segment identity must not converge")
	}
}

func TestRepositoryCalendarEqualVersionEnrichesLateCustomerServiceAndStaffMappings(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	fence := seedReadyCalendarProviderFence(t, ctx, db, salonID)
	startTime := time.Now().UTC().Add(120 * time.Hour).Truncate(time.Second)
	item := CalendarAppointmentImport{
		Provider:              pos.ProviderSquare,
		POSAppointmentID:      "late-mapping-booking-" + uuid.NewString(),
		POSAppointmentVersion: 4,
		Status:                StatusConfirmed,
		POSCustomerID:         "late-provider-customer",
		StartTime:             startTime,
		EndTime:               startTime.Add(55 * time.Minute),
		Segments: []CalendarAppointmentSegmentImport{{
			POSServiceID:      "late-provider-service",
			POSServiceVersion: 12,
			POSStaffID:        "late-provider-staff",
			DurationMinutes:   55,
		}},
	}
	if summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{item}); err != nil || summary.Imported != 1 {
		t.Fatalf("initial unmapped import summary/error = %#v/%v", summary, err)
	}

	var serviceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id, pos_provider, pos_service_id, pos_service_version, name, duration_minutes, sync_status, source)
		VALUES ($1, 'square', $2, 12, 'Late Mapped Pedicure', 55, 'synced', 'imported')
		RETURNING id::text
	`, salonID, "late-provider-service").Scan(&serviceID); err != nil {
		t.Fatalf("insert late service mapping: %v", err)
	}
	var staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, sync_status, source)
		VALUES ($1, 'square', $2, 'Late Mapped Technician', 'synced', 'imported')
		RETURNING id::text
	`, salonID, "late-provider-staff").Scan(&staffID); err != nil {
		t.Fatalf("insert late staff mapping: %v", err)
	}
	var customerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customers (salon_id, name, phone, normalized_phone, email, normalized_email, sync_status, source)
		VALUES ($1, 'Late Mapped Customer', '+13125550333', '+13125550333', 'late@example.com', 'late@example.com', 'synced', 'imported')
		RETURNING id::text
	`, salonID).Scan(&customerID); err != nil {
		t.Fatalf("insert late customer mapping: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (salon_id, entity_type, entity_id, provider, provider_entity_id, sync_status, last_synced_at)
		VALUES ($1, 'customer', $2, 'square', $3, 'synced', now())
	`, salonID, customerID, item.POSCustomerID); err != nil {
		t.Fatalf("insert late provider customer link: %v", err)
	}

	summary, err := repo.UpsertCalendarAppointments(ctx, salonID, pos.ProviderSquare, fence, []CalendarAppointmentImport{item})
	if err != nil || summary.Updated != 1 || summary.Skipped != 0 {
		t.Fatalf("equal-version enrichment summary/error = %#v/%v, want one update", summary, err)
	}
	var gotName, gotPhone, gotEmail, gotServiceID, gotStaffID, segmentServiceID, segmentStaffID string
	var gotVersion int
	var gotStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT appointment.customer_name, appointment.customer_phone, COALESCE(appointment.customer_email, ''),
		       COALESCE(appointment.service_id::text, ''), COALESCE(appointment.staff_id::text, ''),
		       appointment.pos_appointment_version, appointment.status,
		       COALESCE(segment.service_id::text, ''), COALESCE(segment.staff_id::text, '')
		FROM appointments appointment
		JOIN appointment_services segment ON segment.appointment_id = appointment.id AND segment.sort_order = 1
		WHERE appointment.salon_id = $1 AND appointment.pos_provider = 'square' AND appointment.pos_appointment_id = $2
	`, salonID, item.POSAppointmentID).Scan(&gotName, &gotPhone, &gotEmail, &gotServiceID, &gotStaffID, &gotVersion, &gotStatus, &segmentServiceID, &segmentStaffID); err != nil {
		t.Fatalf("load enriched calendar mirror: %v", err)
	}
	if gotName != "Late Mapped Customer" || gotPhone != "+13125550333" || gotEmail != "late@example.com" || gotServiceID != serviceID || gotStaffID != staffID || segmentServiceID != serviceID || segmentStaffID != staffID {
		t.Fatalf("enriched mirror = %q/%q/%q service=%s/%s staff=%s/%s", gotName, gotPhone, gotEmail, gotServiceID, segmentServiceID, gotStaffID, segmentStaffID)
	}
	if gotVersion != item.POSAppointmentVersion || gotStatus != StatusConfirmed {
		t.Fatalf("provider truth changed during enrichment: version/status = %d/%s", gotVersion, gotStatus)
	}
}

func TestRepositoryCalendarImportRejectsStaleProviderFenceBeforeWrites(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	ctx := context.Background()
	ownerID, salonID, _, _ := seedBookingOperationTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	wantFence := seedReadyCalendarProviderFence(t, ctx, db, salonID)
	provider, capturedFence, err := repo.GetActiveProviderFence(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("capture active provider fence: %v", err)
	}
	if provider != pos.ProviderSquare || !sameProviderFence(capturedFence, wantFence) {
		t.Fatalf("captured provider fence = %s/%#v, want square/%#v", provider, capturedFence, wantFence)
	}
	if _, _, err := repo.GetActiveProviderFence(ctx, salonID, uuid.NewString()); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("other owner fence error = %v, want pos.ErrNotFound", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE pos_connections
		SET snapshot_generation = snapshot_generation + 1,
		    updated_at = now()
		WHERE salon_id = $1 AND provider = $2
	`, salonID, pos.ProviderSquare); err != nil {
		t.Fatalf("advance provider generation: %v", err)
	}

	providerAppointmentID := "stale-fence-calendar-" + uuid.NewString()
	start := time.Now().UTC().Add(144 * time.Hour).Truncate(time.Second)
	summary, err := repo.UpsertCalendarAppointments(ctx, salonID, provider, capturedFence, []CalendarAppointmentImport{{
		Provider:              provider,
		POSAppointmentID:      providerAppointmentID,
		POSAppointmentVersion: 1,
		Status:                StatusConfirmed,
		CustomerName:          "Stale Fence Caller",
		CustomerPhone:         "+13125550444",
		StartTime:             start,
		EndTime:               start.Add(45 * time.Minute),
		Segments: []CalendarAppointmentSegmentImport{{
			POSServiceID:      "stale-fence-service",
			POSServiceVersion: 1,
			POSStaffID:        "stale-fence-staff",
			DurationMinutes:   45,
		}},
	}})
	if !errors.Is(err, pos.ErrStaleProviderFence) {
		t.Fatalf("stale fence import summary/error = %#v/%v, want pos.ErrStaleProviderFence", summary, err)
	}
	var appointmentCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM appointments
		WHERE salon_id = $1 AND pos_provider = $2 AND pos_appointment_id = $3
	`, salonID, provider, providerAppointmentID).Scan(&appointmentCount); err != nil {
		t.Fatalf("count stale-fence appointments: %v", err)
	}
	var attemptCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM booking_attempts
		WHERE salon_id = $1 AND pos_provider = $2 AND pos_booking_id = $3
	`, salonID, provider, providerAppointmentID).Scan(&attemptCount); err != nil {
		t.Fatalf("count stale-fence attempts: %v", err)
	}
	if appointmentCount != 0 || attemptCount != 0 {
		t.Fatalf("stale fence writes = %d appointments/%d attempts, want zero", appointmentCount, attemptCount)
	}
}

func seedReadyCalendarProviderFence(t *testing.T, ctx context.Context, db *sql.DB, salonID string) pos.ProviderFence {
	t.Helper()
	fence := pos.ProviderFence{
		LocationID:         "calendar-location-" + uuid.NewString(),
		SnapshotGeneration: 1,
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (
			salon_id, provider, status, location_id, snapshot_generation, last_sync_at
		)
		VALUES ($1, $2, 'active', $3, $4, now())
	`, salonID, pos.ProviderSquare, fence.LocationID, fence.SnapshotGeneration); err != nil {
		t.Fatalf("insert ready calendar provider connection: %v", err)
	}
	return fence
}

func seedBookingOperationTestData(t *testing.T, ctx context.Context, db *sql.DB) (string, string, string, string) {
	t.Helper()
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Booking Operation Test')
		RETURNING id::text
	`, "booking-operation-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Booking Operation Test Salon', '+13125550199', $1)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	var serviceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id, pos_provider, pos_service_id, pos_service_version, name, duration_minutes)
		VALUES ($1, 'square', 'integration-service', 1, 'Integration Manicure', 45)
		RETURNING id::text
	`, salonID).Scan(&serviceID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	var staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name)
		VALUES ($1, 'square', 'integration-staff', 'Integration Staff')
		RETURNING id::text
	`, salonID).Scan(&staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	return ownerID, salonID, serviceID, staffID
}
