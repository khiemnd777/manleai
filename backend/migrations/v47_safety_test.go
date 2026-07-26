package migrations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func readV47(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V47__owner_manual_scheduling_requests.sql")
	if err != nil {
		t.Fatalf("read V47 migration: %v", err)
	}
	return string(source)
}

func TestV47DefinesOwnerManualSchedulingRequestContract(t *testing.T) {
	source := readV47(t)
	for _, fragment := range []string{
		"CREATE TABLE scheduling_requests",
		"CREATE TABLE scheduling_request_segments",
		"CREATE TABLE scheduling_request_events",
		"CHECK (scheduling_authority = 'owner_manual')",
		"CHECK (operation_type IN ('book', 'reschedule', 'cancel'))",
		"CONSTRAINT scheduling_requests_operation_target_check",
		"CHECK (status IN ('pending', 'contacted', 'resolved', 'dismissed'))",
		"CHECK (version >= 1)",
		"source TEXT NOT NULL",
		"requested_timezone TEXT NOT NULL",
		"party_size INTEGER NOT NULL",
		"CHECK (party_size >= 1)",
		"resolution_reason TEXT",
		"contacted_at TIMESTAMPTZ",
		"resolved_at TIMESTAMPTZ",
		"dismissed_at TIMESTAMPTZ",
		"guest_reference TEXT",
		"quantity INTEGER NOT NULL DEFAULT 1",
		"CHECK (quantity >= 1)",
		"ON scheduling_requests(salon_id, scheduling_authority, operation_key)",
		"UNIQUE (scheduling_request_id, action_key)",
		"request_fingerprint TEXT NOT NULL",
		"action_fingerprint TEXT NOT NULL",
		"CONSTRAINT = 'scheduling_requests_immutable_core_guard'",
		"CONSTRAINT = 'scheduling_request_segments_immutable_guard'",
		"CONSTRAINT = 'scheduling_request_events_immutable_guard'",
		"CONSTRAINT = 'scheduling_request_events_actor_owner_guard'",
		"CONSTRAINT = 'call_sessions_scheduling_request_link_write_guard'",
		"pg_trigger_depth() > 1",
		"ADD COLUMN scheduling_request_id UUID",
		"type = 'owner_manual_request_pending'",
		"dedupe_key = 'owner-manual-request-pending:' || scheduling_request_id::text",
		"type <> 'owner_manual_request_pending'",
		"CREATE UNIQUE INDEX idx_call_sessions_scheduling_request",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V47 is missing owner-manual request contract %q", fragment)
		}
	}

	for _, forbidden := range []string{
		"ADD COLUMN IF NOT EXISTS",
		"ALTER TABLE IF EXISTS",
		"CREATE TABLE IF NOT EXISTS",
		"CREATE FUNCTION IF NOT EXISTS",
		"CREATE TRIGGER IF NOT EXISTS",
		"DROP TABLE",
		"DROP COLUMN",
		"TRUNCATE",
		"DELETE FROM scheduling_requests",
		"UPDATE owner_notifications",
		"operation_type IN ('create'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V47 must remain forward-only and preserve approved tokens; found %q", forbidden)
		}
	}
}

func TestV47KeepsProviderAndCustomerInputsDataDriven(t *testing.T) {
	source := strings.ToLower(readV47(t))
	for _, forbidden := range []string{
		"square",
		"customer_name =",
		"customer_phone =",
		"service_name =",
		"staff_name =",
		"source in (",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V47 must not hardcode provider or salon-specific request data; found %q", forbidden)
		}
	}
}

func TestV47OwnerManualRequestConstraintsInPostgres(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback()

	schemaName := "v47_safety_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("set test search path: %v", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE users (
			id UUID PRIMARY KEY
		);
		CREATE TABLE salons (
			id UUID PRIMARY KEY,
			owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT
		);
		CREATE TABLE call_sessions (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE appointments (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE
		);
		CREATE TABLE services (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE
		);
		CREATE TABLE staff (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE
		);
		CREATE TABLE owner_notifications (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
			type TEXT NOT NULL,
			dedupe_key TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		t.Fatalf("create pre-V47 fixture: %v", err)
	}

	if _, err := tx.ExecContext(ctx, readV47(t)); err != nil {
		t.Fatalf("apply V47 to pre-V47 fixture: %v", err)
	}

	ownerID := uuid.New()
	otherOwnerID := uuid.New()
	salonID := uuid.New()
	otherSalonID := uuid.New()
	callSessionID := uuid.New()
	otherCallSessionID := uuid.New()
	appointmentID := uuid.New()
	otherAppointmentID := uuid.New()
	serviceID := uuid.New()
	otherServiceID := uuid.New()
	staffID := uuid.New()
	otherStaffID := uuid.New()
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO users (id) VALUES ($1), ($2)", []any{ownerID, otherOwnerID}},
		{"INSERT INTO salons (id, owner_user_id) VALUES ($1, $2), ($3, $4)", []any{salonID, ownerID, otherSalonID, otherOwnerID}},
		{"INSERT INTO call_sessions (id, salon_id) VALUES ($1, $2), ($3, $4)", []any{callSessionID, salonID, otherCallSessionID, otherSalonID}},
		{"INSERT INTO appointments (id, salon_id) VALUES ($1, $2), ($3, $4)", []any{appointmentID, salonID, otherAppointmentID, otherSalonID}},
		{"INSERT INTO services (id, salon_id) VALUES ($1, $2), ($3, $4)", []any{serviceID, salonID, otherServiceID, otherSalonID}},
		{"INSERT INTO staff (id, salon_id) VALUES ($1, $2), ($3, $4)", []any{staffID, salonID, otherStaffID, otherSalonID}},
	} {
		if _, err := tx.ExecContext(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seed V47 fixture: %v", err)
		}
	}

	requestID := uuid.New()
	fingerprint := strings.Repeat("a", 64)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_requests (
			id, salon_id, operation_key, request_fingerprint, operation_type, source,
			call_session_id, target_description, customer_name, customer_phone,
			requested_timezone, party_size
		) VALUES ($1, $2, 'request-1', $3, 'reschedule', 'dashboard', $4,
			'appointment described by the owner', 'Customer', '+15551230000',
			'America/New_York', 2)
	`, requestID, salonID, fingerprint, callSessionID); err != nil {
		t.Fatalf("insert owner-manual request with free-text target: %v", err)
	}

	var linkedRequestID uuid.UUID
	if err := tx.QueryRowContext(ctx,
		"SELECT scheduling_request_id FROM call_sessions WHERE id = $1", callSessionID,
	).Scan(&linkedRequestID); err != nil {
		t.Fatalf("read reciprocal call-session link: %v", err)
	}
	if linkedRequestID != requestID {
		t.Fatalf("reciprocal call-session request=%s, want %s", linkedRequestID, requestID)
	}

	segmentID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_request_segments (
			id, salon_id, scheduling_request_id, service_id, service_name,
			guest_reference, quantity, staff_id, staff_name, duration_minutes, sort_order
		) VALUES ($1, $2, $3, $4, 'Gel Manicure', 'guest-a', 2, $5, 'Technician', 45, 1)
	`, segmentID, salonID, requestID, serviceID, staffID); err != nil {
		t.Fatalf("insert scheduling request segment: %v", err)
	}

	eventID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_request_events (
			id, salon_id, scheduling_request_id, action_key, action_fingerprint,
			event_type, request_version, actor_user_id
		) VALUES ($1, $2, $3, 'created', $4, 'created', 1, $5)
	`, eventID, salonID, requestID, strings.Repeat("b", 64), ownerID); err != nil {
		t.Fatalf("insert scheduling request event: %v", err)
	}

	notificationID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notifications (
			id, salon_id, type, dedupe_key, scheduling_request_id
		) VALUES ($1, $2, 'owner_manual_request_pending', $3, $4)
	`, notificationID, salonID, "owner-manual-request-pending:"+requestID.String(), requestID); err != nil {
		t.Fatalf("insert request notification: %v", err)
	}

	expectV47PostgresError(t, ctx, tx, "23505", "idx_scheduling_requests_operation_key", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			customer_name, customer_phone, requested_timezone, party_size
		) VALUES ($1, 'request-1', $2, 'book', 'dashboard', 'Other', '+15550000000',
			'America/New_York', 1)
	`, salonID, strings.Repeat("c", 64))

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_requests_authority_check", `
		INSERT INTO scheduling_requests (
			salon_id, scheduling_authority, operation_key, request_fingerprint,
			operation_type, source, customer_name, customer_phone, requested_timezone, party_size
		) VALUES ($1, 'external_provider', 'request-2', $2, 'book', 'dashboard',
			'Other', '+15550000000', 'America/New_York', 1)
	`, salonID, strings.Repeat("d", 64))

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_requests_lifecycle_check", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			customer_name, customer_phone, requested_timezone, party_size, contacted_at
		) VALUES ($1, 'request-3', $2, 'book', 'dashboard', 'Other', '+15550000000',
			'America/New_York', 1, now())
	`, salonID, strings.Repeat("e", 64))

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_requests_operation_target_check", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			target_description, customer_name, customer_phone, requested_timezone, party_size
		) VALUES ($1, 'request-book-with-target', $2, 'book', 'dashboard',
			'book must not target an existing appointment', 'Other', '+15550000000',
			'America/New_York', 1)
	`, salonID, strings.Repeat("4", 64))
	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_requests_operation_target_check", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			target_appointment_id, customer_name, customer_phone, requested_timezone, party_size
		) VALUES ($1, 'request-target-without-authority', $2, 'cancel', 'dashboard', $3,
			'Other', '+15550000000', 'America/New_York', 1)
	`, salonID, strings.Repeat("5", 64), appointmentID)
	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_requests_operation_target_check", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			customer_name, customer_phone, requested_timezone, party_size
		) VALUES ($1, 'request-reschedule-without-target', $2, 'reschedule', 'dashboard',
			'Other', '+15550000000', 'America/New_York', 1)
	`, salonID, strings.Repeat("6", 64))

	expectV47PostgresError(t, ctx, tx, "23505", "idx_scheduling_requests_call_session", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			call_session_id, target_description, customer_name, customer_phone,
			requested_timezone, party_size
		) VALUES ($1, 'request-duplicate-call', $2, 'reschedule', 'dashboard', $3,
			'another appointment', 'Other', '+15550000000', 'America/New_York', 1)
	`, salonID, strings.Repeat("7", 64), callSessionID)

	if _, err := tx.ExecContext(ctx,
		"SET CONSTRAINTS scheduling_requests_target_appointment_tenant_fk IMMEDIATE",
	); err != nil {
		t.Fatalf("make target appointment tenant constraint immediate: %v", err)
	}
	expectV47PostgresError(t, ctx, tx, "23503", "scheduling_requests_target_appointment_tenant_fk", `
		INSERT INTO scheduling_requests (
			salon_id, operation_key, request_fingerprint, operation_type, source,
			target_appointment_id, target_scheduling_authority, customer_name,
			customer_phone, requested_timezone, party_size
		) VALUES ($1, 'request-cross-tenant-target', $2, 'cancel', 'dashboard', $3,
			'external_provider', 'Other', '+15550000000', 'America/New_York', 1)
	`, salonID, strings.Repeat("8", 64), otherAppointmentID)

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_requests_immutable_core_guard",
		"UPDATE scheduling_requests SET notes = 'changed' WHERE id = $1", requestID)

	if _, err := tx.ExecContext(ctx,
		"SET CONSTRAINTS scheduling_request_segments_service_tenant_fk IMMEDIATE",
	); err != nil {
		t.Fatalf("make segment service tenant constraint immediate: %v", err)
	}
	expectV47PostgresError(t, ctx, tx, "23503", "scheduling_request_segments_service_tenant_fk", `
		INSERT INTO scheduling_request_segments (
			salon_id, scheduling_request_id, service_id, service_name,
			staff_selection_mode, duration_minutes, sort_order
		) VALUES ($1, $2, $3, 'Other Service', 'anyone', 30, 2)
	`, salonID, requestID, otherServiceID)

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_request_segments_immutable_guard",
		"UPDATE scheduling_request_segments SET quantity = 1 WHERE id = $1", segmentID)
	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_request_segments_immutable_guard",
		"DELETE FROM scheduling_request_segments WHERE id = $1", segmentID)

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_request_events_actor_owner_guard", `
		INSERT INTO scheduling_request_events (
			salon_id, scheduling_request_id, action_key, action_fingerprint,
			event_type, request_version, actor_user_id
		) VALUES ($1, $2, 'wrong-actor', $3, 'contacted', 2, $4)
	`, salonID, requestID, strings.Repeat("f", 64), otherOwnerID)

	expectV47PostgresError(t, ctx, tx, "23505", "scheduling_request_events_action_key", `
		INSERT INTO scheduling_request_events (
			salon_id, scheduling_request_id, action_key, action_fingerprint,
			event_type, request_version, actor_user_id
		) VALUES ($1, $2, 'created', $3, 'created', 1, $4)
	`, salonID, requestID, strings.Repeat("1", 64), ownerID)

	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_request_events_immutable_guard",
		"UPDATE scheduling_request_events SET event_type = 'changed' WHERE id = $1", eventID)
	expectV47PostgresError(t, ctx, tx, "23514", "scheduling_request_events_immutable_guard",
		"DELETE FROM scheduling_request_events WHERE id = $1", eventID)

	expectV47PostgresError(t, ctx, tx, "23514", "owner_notifications_owner_manual_request_check", `
		INSERT INTO owner_notifications (id, salon_id, type, dedupe_key)
		VALUES ($1, $2, 'owner_manual_request_pending', 'orphan')
	`, uuid.New(), salonID)
	expectV47PostgresError(t, ctx, tx, "23514", "owner_notifications_owner_manual_request_check", `
		INSERT INTO owner_notifications (id, salon_id, type, dedupe_key, scheduling_request_id)
		VALUES ($1, $2, 'other', 'other', $3)
	`, uuid.New(), salonID, requestID)
	expectV47PostgresError(t, ctx, tx, "23514", "owner_notifications_owner_manual_request_check", `
		INSERT INTO owner_notifications (id, salon_id, type, dedupe_key, scheduling_request_id)
		VALUES ($1, $2, 'owner_manual_request_pending', 'wrong', $3)
	`, uuid.New(), salonID, requestID)

	expectV47PostgresError(t, ctx, tx, "23514", "call_sessions_scheduling_request_link_guard", `
		INSERT INTO call_sessions (id, salon_id, scheduling_request_id)
		VALUES ($1, $2, $3)
	`, uuid.New(), otherSalonID, requestID)
	expectV47PostgresError(t, ctx, tx, "23514", "call_sessions_scheduling_request_link_write_guard",
		"UPDATE call_sessions SET scheduling_request_id = NULL WHERE id = $1", callSessionID)

	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduling_requests
		SET status = 'contacted', contacted_at = now(), version = 2, updated_at = now()
		WHERE id = $1
	`, requestID); err != nil {
		t.Fatalf("apply contacted lifecycle transition: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduling_requests
		SET status = 'resolved', resolved_at = now(), resolution_reason = 'owner completed request',
			version = 3, updated_at = now()
		WHERE id = $1
	`, requestID); err != nil {
		t.Fatalf("apply resolved lifecycle transition: %v", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM scheduling_requests WHERE id = $1", requestID); err != nil {
		t.Fatalf("delete request aggregate through child cascades: %v", err)
	}
	assertV47Count(t, ctx, tx, "scheduling_request_segments", "scheduling_request_id", requestID, 0)
	assertV47Count(t, ctx, tx, "scheduling_request_events", "scheduling_request_id", requestID, 0)
	assertV47Count(t, ctx, tx, "owner_notifications", "scheduling_request_id", requestID, 0)

	var reciprocalCleared bool
	if err := tx.QueryRowContext(ctx, `
		SELECT scheduling_request_id IS NULL FROM call_sessions WHERE id = $1
	`, callSessionID).Scan(&reciprocalCleared); err != nil {
		t.Fatalf("read call-session link after aggregate deletion: %v", err)
	}
	if !reciprocalCleared {
		t.Fatal("request deletion must clear the reciprocal call-session link")
	}

	deleteSalonID := uuid.New()
	deleteCallID := uuid.New()
	deleteAppointmentID := uuid.New()
	deleteServiceID := uuid.New()
	deleteStaffID := uuid.New()
	deleteRequestID := uuid.New()
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO salons (id, owner_user_id) VALUES ($1, $2)", []any{deleteSalonID, ownerID}},
		{"INSERT INTO call_sessions (id, salon_id) VALUES ($1, $2)", []any{deleteCallID, deleteSalonID}},
		{"INSERT INTO appointments (id, salon_id) VALUES ($1, $2)", []any{deleteAppointmentID, deleteSalonID}},
		{"INSERT INTO services (id, salon_id) VALUES ($1, $2)", []any{deleteServiceID, deleteSalonID}},
		{"INSERT INTO staff (id, salon_id) VALUES ($1, $2)", []any{deleteStaffID, deleteSalonID}},
		{`
			INSERT INTO scheduling_requests (
				id, salon_id, operation_key, request_fingerprint, operation_type, source,
				call_session_id, target_appointment_id, target_scheduling_authority,
				customer_name, customer_phone, requested_timezone, party_size
			) VALUES ($1, $2, 'cascade-request', $3, 'cancel', 'dashboard', $4, $5,
				'external_provider', 'Customer', '+15551230001', 'America/New_York', 1)
		`, []any{deleteRequestID, deleteSalonID, strings.Repeat("2", 64), deleteCallID, deleteAppointmentID}},
		{`
			INSERT INTO scheduling_request_segments (
				salon_id, scheduling_request_id, service_id, service_name, staff_id,
				staff_name, duration_minutes, sort_order
			) VALUES ($1, $2, $3, 'Service', $4, 'Staff', 30, 1)
		`, []any{deleteSalonID, deleteRequestID, deleteServiceID, deleteStaffID}},
		{`
			INSERT INTO scheduling_request_events (
				salon_id, scheduling_request_id, action_key, action_fingerprint,
				event_type, request_version, actor_user_id
			) VALUES ($1, $2, 'created', $3, 'created', 1, $4)
		`, []any{deleteSalonID, deleteRequestID, strings.Repeat("3", 64), ownerID}},
	} {
		if _, err := tx.ExecContext(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seed salon cascade fixture: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM salons WHERE id = $1", deleteSalonID); err != nil {
		t.Fatalf("delete salon through aggregate and reference cascades: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatalf("validate deferred constraints after salon cascade: %v", err)
	}
	assertV47Count(t, ctx, tx, "scheduling_requests", "id", deleteRequestID, 0)
}

func expectV47PostgresError(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	wantCode string,
	wantConstraint string,
	query string,
	args ...any,
) {
	t.Helper()
	savepoint := pq.QuoteIdentifier("v47_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create V47 savepoint: %v", err)
	}
	_, execErr := tx.ExecContext(ctx, query, args...)
	if execErr == nil {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("expected PostgreSQL error %s (%s)", wantCode, wantConstraint)
	}
	var pqErr *pq.Error
	if !errors.As(execErr, &pqErr) {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("expected *pq.Error, got %T: %v", execErr, execErr)
	}
	if string(pqErr.Code) != wantCode {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("PostgreSQL code=%s, want %s: %v", pqErr.Code, wantCode, execErr)
	}
	if pqErr.Constraint != wantConstraint {
		_, _ = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("PostgreSQL constraint=%q, want %q: %v", pqErr.Constraint, wantConstraint, execErr)
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("roll back V47 savepoint: %v", err)
	}
}

func assertV47Count(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	table string,
	column string,
	id uuid.UUID,
	want int,
) {
	t.Helper()
	query := "SELECT count(*) FROM " + pq.QuoteIdentifier(table) +
		" WHERE " + pq.QuoteIdentifier(column) + " = $1"
	var got int
	if err := tx.QueryRowContext(ctx, query, id).Scan(&got); err != nil {
		t.Fatalf("count %s by %s: %v", table, column, err)
	}
	if got != want {
		t.Fatalf("%s count=%d, want %d", table, got, want)
	}
}
