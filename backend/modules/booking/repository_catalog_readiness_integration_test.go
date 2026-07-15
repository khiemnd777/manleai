package booking

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestRepositoryBookableCatalogRequiresCompletedCurrentProviderSnapshot(t *testing.T) {
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
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Booking Catalog Readiness Test')
		RETURNING id::text
	`, "booking-catalog-readiness-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Booking Catalog Readiness Test Salon', '+13125550301', $1)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert salon: %v", err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id)
		VALUES ($1, 'square', 'connected', 'location-a')
	`, salonID); err != nil {
		t.Fatalf("insert POS connection: %v", err)
	}

	posRepo := pos.NewRepository(db)
	bookingRepo := NewRepository(db)
	locationAGeneration, err := posRepo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "location-a")
	if err != nil {
		t.Fatalf("begin location A snapshot: %v", err)
	}
	if _, err := posRepo.ApplyProviderSnapshot(ctx, salonID, pos.ProviderSnapshot{
		Provider:   pos.ProviderSquare,
		LocationID: "location-a",
		Generation: locationAGeneration,
		Services: []pos.Service{{
			POSProvider: pos.ProviderSquare, POSServiceID: "location-a-service-" + suffix,
			POSServiceVersion: 1, Name: "Location A Manicure", DurationMinutes: 30, AIBookable: true, Active: true,
		}},
		Staff: []pos.StaffMember{{
			POSProvider: pos.ProviderSquare, POSStaffID: "location-a-staff-" + suffix,
			Name: "Location A Technician", AIBookable: true, Active: true,
		}},
	}); err != nil {
		t.Fatalf("apply location A snapshot: %v", err)
	}
	if err := posRepo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, locationAGeneration, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete location A snapshot: %v", err)
	}

	locationAServiceID := providerEntityLocalID(t, ctx, db, salonID, "service", "location-a-service-"+suffix)
	locationAStaffID := providerEntityLocalID(t, ctx, db, salonID, "staff", "location-a-staff-"+suffix)
	if _, err := bookingRepo.GetBookableService(ctx, salonID, pos.ProviderSquare, locationAServiceID); err != nil {
		t.Fatalf("load location A service after completed snapshot: %v", err)
	}
	if _, err := bookingRepo.GetBookableStaff(ctx, salonID, pos.ProviderSquare, locationAStaffID); err != nil {
		t.Fatalf("load location A staff after completed snapshot: %v", err)
	}
	staff, err := bookingRepo.ListBookableStaffRefs(ctx, salonID, pos.ProviderSquare)
	if err != nil || len(staff) != 1 || staff[0].POSStaffID != "location-a-staff-"+suffix {
		t.Fatalf("location A staff catalog = %#v, err=%v", staff, err)
	}

	if _, err := posRepo.UpdateLocation(ctx, salonID, pos.ProviderSquare, "location-b"); err != nil {
		t.Fatalf("switch to location B: %v", err)
	}
	assertBookingCatalogUnavailable(t, ctx, bookingRepo, salonID, locationAServiceID, locationAStaffID)

	locationBGeneration, err := posRepo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "location-b")
	if err != nil {
		t.Fatalf("begin location B snapshot: %v", err)
	}
	if _, err := posRepo.ApplyProviderSnapshot(ctx, salonID, pos.ProviderSnapshot{
		Provider:   pos.ProviderSquare,
		LocationID: "location-b",
		Generation: locationBGeneration,
		Services: []pos.Service{{
			POSProvider: pos.ProviderSquare, POSServiceID: "location-b-service-" + suffix,
			POSServiceVersion: 2, Name: "Location B Pedicure", DurationMinutes: 45, AIBookable: true, Active: true,
		}},
		Staff: []pos.StaffMember{{
			POSProvider: pos.ProviderSquare, POSStaffID: "location-b-staff-" + suffix,
			Name: "Location B Technician", AIBookable: true, Active: true,
		}},
	}); err != nil {
		t.Fatalf("apply location B snapshot: %v", err)
	}
	locationBServiceID := providerEntityLocalID(t, ctx, db, salonID, "service", "location-b-service-"+suffix)
	locationBStaffID := providerEntityLocalID(t, ctx, db, salonID, "staff", "location-b-staff-"+suffix)
	assertBookingCatalogUnavailable(t, ctx, bookingRepo, salonID, locationBServiceID, locationBStaffID)

	if err := posRepo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, locationBGeneration, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete location B snapshot: %v", err)
	}
	service, err := bookingRepo.GetBookableService(ctx, salonID, pos.ProviderSquare, locationBServiceID)
	if err != nil || service.POSServiceID != "location-b-service-"+suffix {
		t.Fatalf("location B service = %#v, err=%v", service, err)
	}
	if _, err := bookingRepo.GetBookableStaff(ctx, salonID, pos.ProviderSquare, locationBStaffID); err != nil {
		t.Fatalf("load location B staff after completed snapshot: %v", err)
	}
	staff, err = bookingRepo.ListBookableStaffRefs(ctx, salonID, pos.ProviderSquare)
	if err != nil || len(staff) != 1 || staff[0].POSStaffID != "location-b-staff-"+suffix {
		t.Fatalf("location B staff catalog = %#v, err=%v", staff, err)
	}
	if _, err := bookingRepo.GetBookableService(ctx, salonID, pos.ProviderSquare, locationAServiceID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("old location A service error = %v, want pos.ErrNotFound", err)
	}
	if _, err := bookingRepo.GetBookableStaff(ctx, salonID, pos.ProviderSquare, locationAStaffID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("old location A staff error = %v, want pos.ErrNotFound", err)
	}
}

func TestRepositoryRetryEligibilityUsesCurrentCatalogLocationAndTargetVersion(t *testing.T) {
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
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Retry Eligibility Test')
		RETURNING id::text
	`, "retry-eligibility-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Retry Eligibility Salon', '+13125550302', $1)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert salon: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
	}()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id)
		VALUES ($1, 'square', 'connected', 'retry-location')
	`, salonID); err != nil {
		t.Fatalf("insert POS connection: %v", err)
	}

	serviceProviderID := "retry-service-" + suffix
	staffProviderID := "retry-staff-" + suffix
	snapshot := func(generation int64) pos.ProviderSnapshot {
		return pos.ProviderSnapshot{
			Provider: pos.ProviderSquare, LocationID: "retry-location", Generation: generation,
			Services: []pos.Service{{
				POSProvider: pos.ProviderSquare, POSServiceID: serviceProviderID, POSServiceVersion: 7,
				Name: "Retry Manicure", DurationMinutes: 45, AIBookable: true, Active: true,
			}},
			Staff: []pos.StaffMember{{
				POSProvider: pos.ProviderSquare, POSStaffID: staffProviderID,
				Name: "Retry Technician", AIBookable: true, Active: true,
			}},
		}
	}
	posRepo := pos.NewRepository(db)
	bookingRepo := NewRepository(db)
	generation1, err := posRepo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "retry-location")
	if err != nil {
		t.Fatalf("begin first snapshot: %v", err)
	}
	if _, err := posRepo.ApplyProviderSnapshot(ctx, salonID, snapshot(generation1)); err != nil {
		t.Fatalf("apply first snapshot: %v", err)
	}
	if err := posRepo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, generation1, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete first snapshot: %v", err)
	}
	serviceID := providerEntityLocalID(t, ctx, db, salonID, pos.EntityTypeService, serviceProviderID)
	staffID := providerEntityLocalID(t, ctx, db, salonID, pos.EntityTypeStaff, staffProviderID)
	service, err := bookingRepo.GetBookableService(ctx, salonID, pos.ProviderSquare, serviceID)
	if err != nil {
		t.Fatalf("load bookable service: %v", err)
	}
	staff, err := bookingRepo.GetBookableStaff(ctx, salonID, pos.ProviderSquare, staffID)
	if err != nil {
		t.Fatalf("load bookable staff: %v", err)
	}

	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	slotFingerprint := strings.Repeat("a", 64)
	quote, err := bookingRepo.CreateAvailabilityQuote(ctx, AvailabilityQuoteRecord{
		SalonID: salonID, Provider: pos.ProviderSquare, ProviderFence: service.ProviderFence,
		RequestFingerprint: "retry-eligibility-quote", ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		Slots: []AvailabilitySlot{{
			Fingerprint: slotFingerprint, StartTime: start, EndTime: start.Add(45 * time.Minute),
			Segments: []AvailabilitySegment{{ServiceID: serviceID, StaffID: staffID, StaffSelectionMode: StaffSelectionSpecific, DurationMinutes: 45}},
		}},
	})
	if err != nil {
		t.Fatalf("create quote: %v", err)
	}
	claim, err := bookingRepo.ClaimPendingBookingAttempt(ctx, PendingBookingRecord{
		SalonID: salonID, Source: SourceSquareTestBooking, Provider: pos.ProviderSquare,
		POSIdempotencyKey: uuid.NewString(), OperationKey: "retry-eligibility-book",
		RequestFingerprint: "retry-eligibility-book-fingerprint", AvailabilityQuoteID: quote.ID,
		SlotFingerprint: slotFingerprint, ProviderFence: service.ProviderFence,
		ProcessingToken: uuid.NewString(), LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
		CustomerName: "Retry Caller", CustomerPhone: "+13125550302", Service: *service, Staff: *staff,
		StaffSelectionMode: StaffSelectionSpecific, StartTime: start, EndTime: start.Add(45 * time.Minute),
		Segments: []BookingSegmentRecord{{Service: *service, Staff: *staff, StaffSelectionMode: StaffSelectionSpecific, SortOrder: 1}},
	})
	if err != nil {
		t.Fatalf("claim booking attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE booking_attempts
		SET status = $1, provider_outcome = $2, retry_policy = $3,
		    reconciliation_status = $4, processing_token = NULL, processing_lease_expires_at = NULL
		WHERE id = $5
	`, StatusFallbackPending, ProviderOutcomeFailed, RetryPolicySafe, ReconciliationNotRequired, claim.Attempt.ID); err != nil {
		t.Fatalf("mark safe fallback: %v", err)
	}
	assertRetry := func(want bool, reasonContains string) {
		t.Helper()
		items, err := bookingRepo.ListBookingAttempts(ctx, salonID, ownerID, "", 50, 0)
		if err != nil {
			t.Fatalf("list booking attempts: %v", err)
		}
		for _, item := range items {
			if item.ID != claim.Attempt.ID {
				continue
			}
			if item.CanRetry != want {
				t.Fatalf("CanRetry = %t, want %t; reason=%q", item.CanRetry, want, item.RetryBlockedReason)
			}
			if reasonContains != "" && !strings.Contains(item.RetryBlockedReason, reasonContains) {
				t.Fatalf("retry reason = %q, want substring %q", item.RetryBlockedReason, reasonContains)
			}
			return
		}
		t.Fatalf("attempt %s not returned", claim.Attempt.ID)
	}
	assertRetry(true, "")

	generation2, err := posRepo.BeginProviderSnapshot(ctx, salonID, pos.ProviderSquare, "retry-location")
	if err != nil {
		t.Fatalf("begin second snapshot: %v", err)
	}
	if _, err := posRepo.ApplyProviderSnapshot(ctx, salonID, snapshot(generation2)); err != nil {
		t.Fatalf("apply second snapshot: %v", err)
	}
	if err := posRepo.MarkSyncCompleteForGeneration(ctx, salonID, pos.ProviderSquare, generation2, pos.StatusActive, ""); err != nil {
		t.Fatalf("complete second snapshot: %v", err)
	}
	assertRetry(true, "")

	if _, err := db.ExecContext(ctx, `
		UPDATE pos_entity_links
		SET provider_version = 8
		WHERE salon_id = $1 AND entity_type = $2 AND provider = 'square' AND entity_id = $3
	`, salonID, pos.EntityTypeService, serviceID); err != nil {
		t.Fatalf("change service mapping version: %v", err)
	}
	assertRetry(false, "service or staff mapping changed")
	if _, err := db.ExecContext(ctx, `
		UPDATE pos_entity_links
		SET provider_version = 7
		WHERE salon_id = $1 AND entity_type = $2 AND provider = 'square' AND entity_id = $3
	`, salonID, pos.EntityTypeService, serviceID); err != nil {
		t.Fatalf("restore service mapping version: %v", err)
	}
	if _, err := posRepo.UpdateLocation(ctx, salonID, pos.ProviderSquare, "other-location"); err != nil {
		t.Fatalf("change provider location: %v", err)
	}
	assertRetry(false, "catalog location changed")
	if _, err := db.ExecContext(ctx, `
		UPDATE booking_attempts
		SET provider_location_id = NULL, provider_snapshot_generation = NULL
		WHERE id = $1
	`, claim.Attempt.ID); err != nil {
		t.Fatalf("clear legacy provider fence: %v", err)
	}
	assertRetry(false, "legacy request")
	if _, err := db.ExecContext(ctx, `UPDATE booking_attempts SET superseded_at = now() WHERE id = $1`, claim.Attempt.ID); err != nil {
		t.Fatalf("supersede attempt: %v", err)
	}
	latest, err := bookingRepo.LatestTestBooking(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("load latest test booking: %v", err)
	}
	if latest.CanRetry || !strings.Contains(latest.RetryBlockedReason, "superseded") {
		t.Fatalf("latest superseded retry policy = can_retry=%t reason=%q", latest.CanRetry, latest.RetryBlockedReason)
	}

	var originalAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			customer_name, customer_phone, requested_start_time, requested_end_time,
			operation_type, provider_outcome, retry_policy, reconciliation_status
		)
		VALUES ($1, $2, $3, 'square', $4, 3, 'Cancel Caller', '+13125550303', $5, $6, 'book', $7, $8, $9)
		RETURNING id::text
	`, salonID, SourcePOSCalendarSync, StatusConfirmed, "cancel-booking-"+suffix, start, start.Add(45*time.Minute), ProviderOutcomeSucceeded, RetryPolicyNone, ReconciliationNotRequired).Scan(&originalAttemptID); err != nil {
		t.Fatalf("insert original attempt: %v", err)
	}
	var appointmentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			status, customer_name, customer_phone, start_time, end_time
		)
		VALUES ($1, $2, 'square', $3, 3, $4, 'Cancel Caller', '+13125550303', $5, $6)
		RETURNING id::text
	`, salonID, originalAttemptID, "cancel-booking-"+suffix, StatusConfirmed, start, start.Add(45*time.Minute)).Scan(&appointmentID); err != nil {
		t.Fatalf("insert target appointment: %v", err)
	}
	var cancelAttemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, customer_name, customer_phone,
			requested_start_time, requested_end_time, operation_type, target_appointment_id,
			target_pos_booking_version, provider_outcome, retry_policy, reconciliation_status
		)
		VALUES ($1, $2, $3, 'square', $4, 'Cancel Caller', '+13125550303', $5, $6, 'cancel', $7, 3, $8, $9, $10)
		RETURNING id::text
	`, salonID, SourceOwnerDashboard, StatusFallbackPending, "cancel-booking-"+suffix, start, start.Add(45*time.Minute), appointmentID, ProviderOutcomeFailed, RetryPolicySafe, ReconciliationNotRequired).Scan(&cancelAttemptID); err != nil {
		t.Fatalf("insert cancel fallback: %v", err)
	}
	items, err := bookingRepo.ListBookingAttempts(ctx, salonID, ownerID, "", 50, 0)
	if err != nil {
		t.Fatalf("list cancel attempt: %v", err)
	}
	if item := bookingAttemptByID(items, cancelAttemptID); item == nil || !item.CanRetry {
		t.Fatalf("current cancel attempt = %#v, want retryable", item)
	}
	if _, err := db.ExecContext(ctx, `UPDATE appointments SET pos_appointment_version = 4 WHERE id = $1`, appointmentID); err != nil {
		t.Fatalf("advance target version: %v", err)
	}
	items, err = bookingRepo.ListBookingAttempts(ctx, salonID, ownerID, "", 50, 0)
	if err != nil {
		t.Fatalf("list advanced cancel attempt: %v", err)
	}
	item := bookingAttemptByID(items, cancelAttemptID)
	if item == nil || item.CanRetry || !strings.Contains(item.RetryBlockedReason, "target appointment changed") {
		t.Fatalf("advanced cancel retry policy = %#v", item)
	}
}

func bookingAttemptByID(items []BookingAttempt, id string) *BookingAttempt {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}

func providerEntityLocalID(t *testing.T, ctx context.Context, db *sql.DB, salonID string, entityType string, providerEntityID string) string {
	t.Helper()
	var entityID string
	if err := db.QueryRowContext(ctx, `
		SELECT entity_id::text
		FROM pos_entity_links
		WHERE salon_id = $1
		  AND entity_type = $2
		  AND provider = 'square'
		  AND provider_entity_id = $3
	`, salonID, entityType, providerEntityID).Scan(&entityID); err != nil {
		t.Fatalf("load %s local ID for %s: %v", entityType, providerEntityID, err)
	}
	return entityID
}

func assertBookingCatalogUnavailable(t *testing.T, ctx context.Context, repo *Repository, salonID string, serviceID string, staffID string) {
	t.Helper()
	if _, err := repo.GetBookableService(ctx, salonID, pos.ProviderSquare, serviceID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("bookable service error = %v, want pos.ErrNotFound while catalog is not ready", err)
	}
	if _, err := repo.GetBookableStaff(ctx, salonID, pos.ProviderSquare, staffID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("bookable staff error = %v, want pos.ErrNotFound while catalog is not ready", err)
	}
	staff, err := repo.ListBookableStaffRefs(ctx, salonID, pos.ProviderSquare)
	if err != nil {
		t.Fatalf("list bookable staff while catalog is not ready: %v", err)
	}
	if len(staff) != 0 {
		t.Fatalf("bookable staff while catalog is not ready = %#v, want empty", staff)
	}
}
