package booking

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

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

	conflicting := base
	conflicting.RequestFingerprint = "different-fingerprint"
	conflicting.POSIdempotencyKey = uuid.NewString()
	conflicting.ProcessingToken = uuid.NewString()
	conflicting.LeaseExpiresAt = time.Now().UTC().Add(bookingOperationLeaseDuration)
	if _, err := repo.ClaimPendingBookingAttempt(ctx, conflicting); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("conflicting claim error = %v, want ErrOperationConflict", err)
	}
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
