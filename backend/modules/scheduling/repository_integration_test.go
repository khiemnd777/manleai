package scheduling

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
)

type operationQueryErrorResolver struct {
	repository   *Repository
	currentCalls int
}

func (r *operationQueryErrorResolver) ResolveSchedulingAuthority(context.Context, string, string) (string, error) {
	r.currentCalls++
	return booking.SchedulingAuthorityExternalProvider, nil
}

func (r *operationQueryErrorResolver) ResolveConversationSchedulingPolicy(context.Context, string, string) (ConversationPolicyFence, error) {
	r.currentCalls++
	return ConversationPolicyFence{BookingMode: BookingModePendingApproval, SchedulingAuthority: booking.SchedulingAuthorityExternalProvider}, nil
}

func (r *operationQueryErrorResolver) ResolveAvailabilityQuoteSchedulingAuthority(context.Context, string, string, string) (string, error) {
	return "", booking.ErrAvailabilityQuoteStale
}

func (r *operationQueryErrorResolver) FindOperationSchedulingAuthority(ctx context.Context, salonID string, ownerUserID string, operationKey string) (string, bool, error) {
	return r.repository.FindOperationSchedulingAuthority(ctx, salonID, ownerUserID, operationKey)
}

func (r *operationQueryErrorResolver) FindOperationSchedulingOrigin(ctx context.Context, salonID string, ownerUserID string, operationKey string) (PersistedOperationOrigin, bool, error) {
	return r.repository.FindOperationSchedulingOrigin(ctx, salonID, ownerUserID, operationKey)
}

func (r *operationQueryErrorResolver) ResolveAttemptSchedulingAuthority(context.Context, string, string, string) (string, error) {
	return booking.SchedulingAuthorityExternalProvider, nil
}

func (r *operationQueryErrorResolver) ResolveAppointmentSchedulingAuthority(context.Context, string, string, string) (string, error) {
	return booking.SchedulingAuthorityExternalProvider, nil
}

func TestRepositorySchedulingAuthorityIsPersistedAndOwnerScoped(t *testing.T) {
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
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Scheduling Owner')
		RETURNING id::text
	`, "scheduling-owner-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var otherOwnerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Other Scheduling Owner')
		RETURNING id::text
	`, "scheduling-other-"+uuid.NewString()+"@example.com").Scan(&otherOwnerID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
		t.Fatalf("insert other owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Scheduling Authority Test Salon', '+13125550991', $1)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, otherOwnerID)
		t.Fatalf("insert salon: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, otherOwnerID)
	}()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, scheduling_authority)
		VALUES ($1, $2)
	`, salonID, booking.SchedulingAuthorityOwnerManual); err != nil {
		t.Fatalf("insert salon settings: %v", err)
	}
	var attemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id,
			operation_key, customer_name, customer_phone,
			requested_start_time, requested_end_time,
			scheduling_authority, authority_provider, authority_appointment_id
		)
		VALUES ($1, 'owner_dashboard', 'confirmed', 'square', $2,
		        $3, 'Origin Caller', '+13125550991', now() + interval '1 day', now() + interval '1 day 45 minutes',
		        $4, 'square', $2)
		RETURNING id::text
	`, salonID, "origin-booking-"+uuid.NewString(), "origin-operation-"+uuid.NewString(), booking.SchedulingAuthorityExternalProvider).Scan(&attemptID); err != nil {
		t.Fatalf("insert origin booking attempt: %v", err)
	}
	var operationKey string
	if err := db.QueryRowContext(ctx, `SELECT operation_key FROM booking_attempts WHERE id = $1`, attemptID).Scan(&operationKey); err != nil {
		t.Fatalf("load origin operation key: %v", err)
	}
	var appointmentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id,
			status, customer_name, customer_phone, start_time, end_time,
			scheduling_authority, authority_provider, authority_appointment_id
		)
		SELECT salon_id, id, pos_provider, pos_booking_id,
		       'confirmed', customer_name, customer_phone, requested_start_time, requested_end_time,
		       scheduling_authority, authority_provider, authority_appointment_id
		FROM booking_attempts
		WHERE id = $1
		RETURNING id::text
	`, attemptID).Scan(&appointmentID); err != nil {
		t.Fatalf("insert origin appointment: %v", err)
	}

	repository := NewRepository(db)
	authority, err := repository.ResolveSchedulingAuthority(ctx, salonID, ownerID)
	if err != nil || authority != booking.SchedulingAuthorityOwnerManual {
		t.Fatalf("owner authority = %q/%v", authority, err)
	}
	if _, err := repository.ResolveSchedulingAuthority(ctx, salonID, otherOwnerID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-tenant resolution error = %v, want pos.ErrNotFound", err)
	}
	operationAuthority, found, err := repository.FindOperationSchedulingAuthority(ctx, salonID, ownerID, operationKey)
	if err != nil || !found || operationAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("operation authority = %q/%t/%v", operationAuthority, found, err)
	}
	if _, found, err := repository.FindOperationSchedulingAuthority(ctx, salonID, otherOwnerID, operationKey); err != nil || found {
		t.Fatalf("cross-tenant operation authority = found:%t error:%v", found, err)
	}
	ownerOperationKey := "owner-operation-" + uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scheduling_requests (
			salon_id, scheduling_authority, operation_key, request_fingerprint,
			operation_type, source, customer_name, customer_phone,
			requested_timezone, party_size
		)
		VALUES ($1, $2, $3, $4, 'book', 'owner_dashboard', 'Owner Request Caller', '+13125550992', 'America/Chicago', 1)
	`, salonID, booking.SchedulingAuthorityOwnerManual, ownerOperationKey, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("insert owner-manual origin: %v", err)
	}
	ownerAuthority, found, err := repository.FindOperationSchedulingAuthority(ctx, salonID, ownerID, ownerOperationKey)
	if err != nil || !found || ownerAuthority != booking.SchedulingAuthorityOwnerManual {
		t.Fatalf("owner operation authority = %q/%t/%v", ownerAuthority, found, err)
	}
	if _, found, err := repository.FindOperationSchedulingAuthority(ctx, salonID, otherOwnerID, ownerOperationKey); err != nil || found {
		t.Fatalf("cross-tenant owner operation authority = found:%t error:%v", found, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scheduling_requests (
			salon_id, scheduling_authority, operation_key, request_fingerprint,
			operation_type, source, customer_name, customer_phone,
			requested_timezone, party_size
		)
		VALUES ($1, $2, $3, $4, 'book', 'owner_dashboard', 'Conflicting Origin Caller', '+13125550993', 'America/Chicago', 1)
	`, salonID, booking.SchedulingAuthorityOwnerManual, operationKey, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("insert conflicting owner-manual origin: %v", err)
	}
	if _, found, err := repository.FindOperationSchedulingAuthority(ctx, salonID, ownerID, operationKey); !errors.Is(err, booking.ErrOperationConflict) || found {
		t.Fatalf("cross-source operation origin = found:%t error:%v", found, err)
	}
	attemptAuthority, err := repository.ResolveAttemptSchedulingAuthority(ctx, salonID, ownerID, attemptID)
	if err != nil || attemptAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("attempt authority = %q/%v", attemptAuthority, err)
	}
	if _, err := repository.ResolveAttemptSchedulingAuthority(ctx, salonID, otherOwnerID, attemptID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-tenant attempt authority error = %v", err)
	}
	appointmentAuthority, err := repository.ResolveAppointmentSchedulingAuthority(ctx, salonID, ownerID, appointmentID)
	if err != nil || appointmentAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("appointment authority = %q/%v", appointmentAuthority, err)
	}
	if _, err := repository.ResolveAppointmentSchedulingAuthority(ctx, salonID, otherOwnerID, appointmentID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-tenant appointment authority error = %v", err)
	}
}

func TestRepositoryOperationQueryErrorFailsClosedBeforeCurrentAuthorityOrExternalDispatch(t *testing.T) {
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

	resolver := &operationQueryErrorResolver{repository: NewRepository(db)}
	external := &fakeExecutor{
		authority:    booking.SchedulingAuthorityExternalProvider,
		createResult: validExternalConfirmedAttempt(),
	}
	service := NewService(resolver, nil, external)
	result, err := service.ExecuteAction(context.Background(), "not-a-uuid", uuid.NewString(), ActionRequest{
		OperationType: OperationKindBook,
		OperationKey:  "query-error-must-not-fallback",
		PartySize:     1,
		Segments: []ActionSegment{{
			ServiceID:          "service-1",
			StaffSelectionMode: booking.StaffSelectionAnyone,
			Quantity:           1,
		}},
	})
	if err == nil || result != nil {
		t.Fatalf("query error result = %#v/%v, want fail closed", result, err)
	}
	if resolver.currentCalls != 0 {
		t.Fatalf("current scheduling authority calls = %d, want zero", resolver.currentCalls)
	}
	if len(external.calls) != 0 {
		t.Fatalf("external executor calls = %#v, want zero", external.calls)
	}
}
