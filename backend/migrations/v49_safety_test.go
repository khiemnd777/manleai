package migrations

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func readV49(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V49__manleai_calendar_execution_foundation.sql")
	if err != nil {
		t.Fatalf("read V49 migration: %v", err)
	}
	return string(source)
}

func TestV49DefinesManleAICalendarExecutionPersistence(t *testing.T) {
	source := readV49(t)
	for _, fragment := range []string{
		"ALTER COLUMN pos_provider DROP NOT NULL",
		"ALTER COLUMN pos_appointment_id DROP NOT NULL",
		"ALTER COLUMN pos_service_id DROP NOT NULL",
		"ALTER COLUMN provider_snapshot_generation DROP NOT NULL",
		"ADD COLUMN scheduling_authority_version BIGINT",
		"ADD COLUMN authority_config_version BIGINT",
		"CREATE TABLE availability_quote_slot_segments",
		"CREATE TABLE availability_quote_slot_resource_allocations",
		"CREATE TABLE booking_attempt_segment_resource_allocations",
		"CREATE TABLE manleai_calendar_appointment_resource_allocations",
		"CREATE TABLE manleai_calendar_execution_events",
		"appointment_services_manleai_staff_no_overlap",
		"tstzrange(occupied_start_time, occupied_end_time, '[)')",
		"DEFERRABLE INITIALLY IMMEDIATE",
		"booking_attempts_manleai_calendar_quote_guard",
		"appointments_manleai_calendar_plan_guard",
		"manleai_calendar_execution_events_immutable_guard",
		"event_type IN ('appointment_confirmed', 'appointment_rescheduled', 'appointment_cancelled')",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V49 is missing execution persistence contract %q", fragment)
		}
	}

	for _, forbidden := range []string{
		"CREATE TABLE manleai_calendar_appointments",
		"INSERT INTO pos_errors",
		"INSERT INTO booking_reconciliation_tasks",
		"UPDATE salon_settings\nSET scheduling_authority",
		"execution_failed",
		"fallback_pending'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V49 exceeds committed-evidence persistence scope; found %q", forbidden)
		}
	}
}

func TestV49KeepsExecutionEvidenceDataDriven(t *testing.T) {
	source := strings.ToLower(readV49(t))
	for _, forbidden := range []string{
		"square",
		"pedicure",
		"manicure",
		"technician",
		"service_name =",
		"staff_name =",
		"resource_pool_id = '",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V49 must not hardcode salon, provider, service, staff, or resource data; found %q", forbidden)
		}
	}
}

func TestV49UpgradesCompleteV48SchemaWithoutRewritingExternalEvidence(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v49_upgrade_")
	defer db.Close()
	defer tx.Rollback()

	applyV49MigrationChain(t, ctx, tx, 48)

	ownerID := uuid.New()
	salonID := uuid.New()
	attemptID := uuid.New()
	appointmentID := uuid.New()
	quoteID := uuid.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, full_name) VALUES ($1,$2,'hash','Owner')`, ownerID, "v49-upgrade-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V48 owner: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO salons (id, name, phone, owner_user_id) VALUES ($1,'Upgrade salon','5551000',$2)`, salonID, ownerID); err != nil {
		t.Fatalf("seed V48 salon: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO salon_settings (salon_id) VALUES ($1)`, salonID); err != nil {
		t.Fatalf("seed V48 salon settings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			id, salon_id, status, pos_provider, pos_booking_id, customer_name,
			customer_phone, requested_start_time, requested_end_time,
			operation_key, request_fingerprint, provider_outcome
		) VALUES ($1,$2,'confirmed','legacy-provider','provider-booking','Customer','5552000',now()+interval '1 day',now()+interval '1 day 1 hour',$3,$4,'succeeded')
	`, attemptID, salonID, "upgrade-operation-"+uuid.NewString(), strings.Repeat("a", 64)); err != nil {
		t.Fatalf("seed V48 attempt: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			booking_attempt_id, pos_service_id, name, duration_minutes, sort_order
		) VALUES ($1,'provider-service','Legacy service',60,1)
	`, attemptID); err != nil {
		t.Fatalf("seed V48 attempt segment: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id, salon_id, booking_attempt_id, pos_provider, pos_appointment_id,
			status, customer_name, customer_phone, start_time, end_time
		) VALUES ($1,$2,$3,'legacy-provider','provider-appointment','confirmed','Customer','5552000',now()+interval '1 day',now()+interval '1 day 1 hour')
	`, appointmentID, salonID, attemptID); err != nil {
		t.Fatalf("seed V48 appointment: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointment_services (
			appointment_id, pos_service_id, name, duration_minutes, sort_order
		) VALUES ($1,'provider-service','Legacy service',60,1)
	`, appointmentID); err != nil {
		t.Fatalf("seed V48 appointment segment: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id, salon_id, provider, provider_location_id, provider_snapshot_generation,
			request_fingerprint, expires_at
		) VALUES ($1,$2,'legacy-provider','legacy-location',9,$3,now()+interval '1 hour')
	`, quoteID, salonID, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("seed V48 quote: %v", err)
	}

	if _, err := tx.ExecContext(ctx, readV49(t)); err != nil {
		t.Fatalf("upgrade complete V48 schema with V49: %v", err)
	}

	var provider, providerAppointment, providerService, quoteProvider string
	var attemptFence, appointmentFence sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt.pos_provider, appointment.pos_appointment_id, segment.pos_service_id,
		       quote.provider, attempt.scheduling_authority_version,
		       appointment.scheduling_authority_version
		FROM booking_attempts attempt
		JOIN appointments appointment ON appointment.id = $2
		JOIN appointment_services segment ON segment.appointment_id = appointment.id
		JOIN availability_quotes quote ON quote.id = $3
		WHERE attempt.id = $1
	`, attemptID, appointmentID, quoteID).Scan(
		&provider, &providerAppointment, &providerService, &quoteProvider,
		&attemptFence, &appointmentFence,
	); err != nil {
		t.Fatalf("load upgraded external evidence: %v", err)
	}
	if provider != "legacy-provider" || providerAppointment != "provider-appointment" || providerService != "provider-service" || quoteProvider != "legacy-provider" {
		t.Fatalf("V49 rewrote external evidence: provider=%q appointment=%q service=%q quote=%q", provider, providerAppointment, providerService, quoteProvider)
	}
	if attemptFence.Valid || appointmentFence.Valid {
		t.Fatalf("V49 invented internal fences for external rows: attempt=%v appointment=%v", attemptFence, appointmentFence)
	}
}

func TestV49InternalPartyLifecycleConstraintsAndAtomicConflictRollback(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v49_execution_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 49)

	fixture := seedV49ExecutionFixture(t, ctx, tx)
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	end := start.Add(time.Hour)

	createQuoteID := uuid.New()
	createSlotID := uuid.New()
	createQuoteSegment1 := uuid.New()
	createQuoteSegment2 := uuid.New()
	insertV49PartyQuote(t, ctx, tx, fixture, createQuoteID, createSlotID, createQuoteSegment1, createQuoteSegment2, "book", uuid.Nil, 0, start)
	setV49ConstraintsImmediate(t, ctx, tx)
	setV49ConstraintsDeferred(t, ctx, tx)

	createAttemptID := uuid.New()
	appointmentID := uuid.New()
	createAttemptSegment1 := uuid.New()
	createAttemptSegment2 := uuid.New()
	plan1Segment1 := uuid.New()
	plan1Segment2 := uuid.New()
	insertV49PartyAttempt(t, ctx, tx, fixture, createAttemptID, appointmentID, createQuoteID, createSlotID, "book", "confirmed", uuid.Nil, 0, 1, start)
	insertV49AttemptSegments(t, ctx, tx, fixture, createAttemptID, createAttemptSegment1, createAttemptSegment2, start)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segment_resource_allocations (
			id, salon_id, booking_attempt_segment_id, resource_pool_id, units_allocated
		) VALUES (gen_random_uuid(),$1,$2,$3,1)
	`, fixture.salonID, createAttemptSegment1, fixture.resourcePoolID); err != nil {
		t.Fatalf("insert create attempt resource evidence: %v", err)
	}
	insertV49AppointmentRoot(t, ctx, tx, fixture, appointmentID, createAttemptID, "confirmed", 1, start)
	insertV49AppointmentPlan(t, ctx, tx, fixture, appointmentID, plan1Segment1, plan1Segment2, 1, start)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_appointment_resource_allocations (
			id, salon_id, appointment_service_id, resource_pool_id, units_allocated,
			plan_version, occupied_start_time, occupied_end_time
		) VALUES (gen_random_uuid(),$1,$2,$3,1,1,$4,$5)
	`, fixture.salonID, plan1Segment1, fixture.resourcePoolID, start.Add(-5*time.Minute), end.Add(10*time.Minute)); err != nil {
		t.Fatalf("insert create appointment resource allocation: %v", err)
	}
	insertV49ExecutionEvent(t, ctx, tx, fixture.salonID, createAttemptID, appointmentID, "appointment_confirmed", 1, fixture.configVersion)
	consumeV49Quote(t, ctx, tx, fixture.salonID, createQuoteID, createAttemptID)
	setV49ConstraintsImmediate(t, ctx, tx)
	setV49ConstraintsDeferred(t, ctx, tx)

	conflictQuoteID := uuid.New()
	conflictSlotID := uuid.New()
	insertV49SingleQuote(t, ctx, tx, fixture, conflictQuoteID, conflictSlotID, "book", start, fixture.service1ID, fixture.staff1ID)
	setV49ConstraintsImmediate(t, ctx, tx)
	setV49ConstraintsDeferred(t, ctx, tx)

	conflictAttemptID := uuid.New()
	conflictAppointmentID := uuid.New()
	conflictSegmentID := uuid.New()
	savepoint := pq.QuoteIdentifier("v49_conflict_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create conflict savepoint: %v", err)
	}
	insertV49SingleAttempt(t, ctx, tx, fixture, conflictAttemptID, conflictAppointmentID, conflictQuoteID, conflictSlotID, start)
	insertV49SingleAttemptSegment(t, ctx, tx, fixture, conflictAttemptID, conflictSegmentID, start)
	insertV49AppointmentRoot(t, ctx, tx, fixture, conflictAppointmentID, conflictAttemptID, "confirmed", 1, start)
	_, conflictErr := tx.ExecContext(ctx, `
		INSERT INTO appointment_services (
			id, salon_id, appointment_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, scheduling_authority, authority_service_id, authority_staff_id,
			name, duration_minutes, sort_order, plan_version, scheduled_start_time,
			scheduled_end_time, buffer_before_minutes, buffer_after_minutes,
			occupied_start_time, occupied_end_time
		) VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$10,$11,
		          'Dynamic service',60,1,1,$6,$7,5,10,$8,$9)
	`, uuid.New(), fixture.salonID, conflictAppointmentID, fixture.service1ID, fixture.staff1ID, start, end, start.Add(-5*time.Minute), end.Add(10*time.Minute), fixture.service1ID.String(), fixture.staff1ID.String())
	if conflictErr == nil {
		_, conflictErr = tx.ExecContext(ctx, "SET CONSTRAINTS appointment_services_manleai_staff_no_overlap IMMEDIATE")
	}
	assertV49PostgresError(t, conflictErr, "23P01", "appointment_services_manleai_staff_no_overlap")
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("roll back conflict operation: %v", err)
	}
	assertV49OperationAbsent(t, ctx, tx, conflictAttemptID, conflictAppointmentID)
	var conflictConsumed bool
	if err := tx.QueryRowContext(ctx, `SELECT consumed_at IS NOT NULL FROM availability_quotes WHERE id=$1`, conflictQuoteID).Scan(&conflictConsumed); err != nil {
		t.Fatalf("load conflict quote consumption: %v", err)
	}
	if conflictConsumed {
		t.Fatal("overlap rollback consumed the offered quote")
	}

	rescheduleStart := start.Add(3 * time.Hour)
	rescheduleQuoteID := uuid.New()
	rescheduleSlotID := uuid.New()
	rescheduleQuoteSegment1 := uuid.New()
	rescheduleQuoteSegment2 := uuid.New()
	insertV49PartyQuote(t, ctx, tx, fixture, rescheduleQuoteID, rescheduleSlotID, rescheduleQuoteSegment1, rescheduleQuoteSegment2, "reschedule", appointmentID, 1, rescheduleStart)
	setV49ConstraintsImmediate(t, ctx, tx)
	setV49ConstraintsDeferred(t, ctx, tx)

	rescheduleAttemptID := uuid.New()
	rescheduleAttemptSegment1 := uuid.New()
	rescheduleAttemptSegment2 := uuid.New()
	plan2Segment1 := uuid.New()
	plan2Segment2 := uuid.New()
	insertV49PartyAttempt(t, ctx, tx, fixture, rescheduleAttemptID, appointmentID, rescheduleQuoteID, rescheduleSlotID, "reschedule", "rescheduled", appointmentID, 1, 2, rescheduleStart)
	insertV49AttemptSegments(t, ctx, tx, fixture, rescheduleAttemptID, rescheduleAttemptSegment1, rescheduleAttemptSegment2, rescheduleStart)
	insertV49AttemptResourceAllocation(t, ctx, tx, fixture, rescheduleAttemptSegment1)
	releasedPlan1At := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE manleai_calendar_appointment_resource_allocations SET released_at=$1 WHERE appointment_service_id=$2`, releasedPlan1At, plan1Segment1); err != nil {
		t.Fatalf("release plan one resource allocation: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE appointment_services SET released_at=$1 WHERE appointment_id=$2 AND released_at IS NULL`, releasedPlan1At, appointmentID); err != nil {
		t.Fatalf("release plan one segments: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments SET booking_attempt_id=$1, status='rescheduled', authority_appointment_version=2,
		       authority_config_version=$2, start_time=$3, end_time=$4, updated_at=now()
		WHERE id=$5
	`, rescheduleAttemptID, fixture.configVersion, rescheduleStart, rescheduleStart.Add(time.Hour), appointmentID); err != nil {
		t.Fatalf("advance appointment to plan two: %v", err)
	}
	insertV49AppointmentPlan(t, ctx, tx, fixture, appointmentID, plan2Segment1, plan2Segment2, 2, rescheduleStart)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_appointment_resource_allocations (
			id, salon_id, appointment_service_id, resource_pool_id, units_allocated,
			plan_version, occupied_start_time, occupied_end_time
		) VALUES (gen_random_uuid(),$1,$2,$3,1,2,$4,$5)
	`, fixture.salonID, plan2Segment1, fixture.resourcePoolID, rescheduleStart.Add(-5*time.Minute), rescheduleStart.Add(70*time.Minute)); err != nil {
		t.Fatalf("insert plan two resource allocation: %v", err)
	}
	insertV49ExecutionEvent(t, ctx, tx, fixture.salonID, rescheduleAttemptID, appointmentID, "appointment_rescheduled", 2, fixture.configVersion)
	consumeV49Quote(t, ctx, tx, fixture.salonID, rescheduleQuoteID, rescheduleAttemptID)
	setV49ConstraintsImmediate(t, ctx, tx)
	setV49ConstraintsDeferred(t, ctx, tx)

	cancelAttemptID := uuid.New()
	cancelAttemptSegment1 := uuid.New()
	cancelAttemptSegment2 := uuid.New()
	insertV49PartyAttempt(t, ctx, tx, fixture, cancelAttemptID, appointmentID, uuid.Nil, uuid.Nil, "cancel", "cancelled", appointmentID, 2, 3, rescheduleStart)
	insertV49AttemptSegments(t, ctx, tx, fixture, cancelAttemptID, cancelAttemptSegment1, cancelAttemptSegment2, rescheduleStart)
	insertV49AttemptResourceAllocation(t, ctx, tx, fixture, cancelAttemptSegment1)
	releasedPlan2At := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE manleai_calendar_appointment_resource_allocations SET released_at=$1 WHERE appointment_service_id=$2`, releasedPlan2At, plan2Segment1); err != nil {
		t.Fatalf("release plan two resource allocation: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE appointment_services SET released_at=$1 WHERE appointment_id=$2 AND released_at IS NULL`, releasedPlan2At, appointmentID); err != nil {
		t.Fatalf("release plan two segments: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments SET booking_attempt_id=$1, status='cancelled', authority_appointment_version=3,
		       updated_at=now() WHERE id=$2
	`, cancelAttemptID, appointmentID); err != nil {
		t.Fatalf("advance appointment to cancelled plan: %v", err)
	}
	insertV49ExecutionEvent(t, ctx, tx, fixture.salonID, cancelAttemptID, appointmentID, "appointment_cancelled", 3, fixture.configVersion)
	setV49ConstraintsImmediate(t, ctx, tx)

	var currentAttempt uuid.UUID
	var appointmentVersion, planCount, activePlanCount, eventCount, allocationCount, activeAllocationCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT booking_attempt_id, authority_appointment_version FROM appointments WHERE id=$1
	`, appointmentID).Scan(&currentAttempt, &appointmentVersion); err != nil {
		t.Fatalf("load final appointment: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE released_at IS NULL) FROM appointment_services WHERE appointment_id=$1
	`, appointmentID).Scan(&planCount, &activePlanCount); err != nil {
		t.Fatalf("load appointment plan history: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_execution_events WHERE appointment_id=$1`, appointmentID).Scan(&eventCount); err != nil {
		t.Fatalf("load execution event history: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE allocation.released_at IS NULL)
		FROM manleai_calendar_appointment_resource_allocations allocation
		JOIN appointment_services segment ON segment.id=allocation.appointment_service_id
		WHERE segment.appointment_id=$1
	`, appointmentID).Scan(&allocationCount, &activeAllocationCount); err != nil {
		t.Fatalf("load resource history: %v", err)
	}
	if currentAttempt != cancelAttemptID || appointmentVersion != 3 || planCount != 4 || activePlanCount != 0 || eventCount != 3 || allocationCount != 2 || activeAllocationCount != 0 {
		t.Fatalf("final lifecycle currentAttempt=%s version=%d plans=%d activePlans=%d events=%d allocations=%d activeAllocations=%d", currentAttempt, appointmentVersion, planCount, activePlanCount, eventCount, allocationCount, activeAllocationCount)
	}

	expectV49PostgresError(t, ctx, tx, "23514", "manleai_calendar_execution_events_immutable_guard", `
		UPDATE manleai_calendar_execution_events SET payload='{"changed":true}'::jsonb WHERE booking_attempt_id=$1
	`, createAttemptID)
	expectV49PostgresError(t, ctx, tx, "23514", "booking_attempts_manleai_calendar_shape_check", `
		INSERT INTO booking_attempts (
			id, salon_id, source, status, pos_provider, target_pos_booking_version,
			operation_key, request_fingerprint, availability_quote_id,
			availability_slot_fingerprint, operation_type, provider_outcome,
			retry_policy, reconciliation_status, customer_name, customer_phone,
			requested_start_time, requested_end_time, scheduling_authority,
			authority_appointment_id, authority_appointment_version,
			authority_idempotency_key, scheduling_authority_version,
			authority_config_version, party_size
		) VALUES (gen_random_uuid(),$1,'test','confirmed','fake-provider',NULL,$2,$3,$4,$5,
		          'book','not_started','none','not_required','Customer','5553000',$6,$7,
		          'manleai_calendar',gen_random_uuid()::text,1,$2,1,$8,1)
	`, fixture.salonID, "fake-pos-"+uuid.NewString(), strings.Repeat("f", 64), conflictQuoteID, strings.Repeat("1", 64), start, end, fixture.configVersion)
	expectV49PostgresError(t, ctx, tx, "23514", "availability_quotes_execution_authority_check", `
		INSERT INTO availability_quotes (
			id, salon_id, scheduling_authority, request_fingerprint, expires_at, party_size
		) VALUES (gen_random_uuid(),$1,'owner_manual',$2,now()+interval '1 hour',1)
	`, fixture.salonID, strings.Repeat("e", 64))
}

type v49Fixture struct {
	salonID        uuid.UUID
	otherSalonID   uuid.UUID
	service1ID     uuid.UUID
	service2ID     uuid.UUID
	staff1ID       uuid.UUID
	staff2ID       uuid.UUID
	resourcePoolID uuid.UUID
	configVersion  int64
}

func beginV49PostgresTest(t *testing.T, prefix string) (*sql.DB, context.Context, *sql.Tx) {
	t.Helper()
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		t.Fatalf("begin transaction: %v", err)
	}
	schemaName := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("set test search path: %v", err)
	}
	return db, ctx, tx
}

func applyV49MigrationChain(t *testing.T, ctx context.Context, tx *sql.Tx, maximumVersion int) {
	t.Helper()
	paths, err := fs.Glob(Files, "V*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Slice(paths, func(i, j int) bool { return v49MigrationVersion(t, paths[i]) < v49MigrationVersion(t, paths[j]) })
	for _, migrationPath := range paths {
		if v49MigrationVersion(t, migrationPath) > maximumVersion {
			continue
		}
		source, err := Files.ReadFile(migrationPath)
		if err != nil {
			t.Fatalf("read %s: %v", migrationPath, err)
		}
		if _, err := tx.ExecContext(ctx, string(source)); err != nil {
			t.Fatalf("apply migration %s: %v", migrationPath, err)
		}
	}
}

func v49MigrationVersion(t *testing.T, migrationPath string) int {
	t.Helper()
	name := strings.TrimPrefix(migrationPath, "V")
	separator := strings.Index(name, "__")
	if separator <= 0 {
		t.Fatalf("invalid migration path %q", migrationPath)
	}
	version, err := strconv.Atoi(name[:separator])
	if err != nil {
		t.Fatalf("parse migration path %q: %v", migrationPath, err)
	}
	return version
}

func seedV49ExecutionFixture(t *testing.T, ctx context.Context, tx *sql.Tx) v49Fixture {
	t.Helper()
	ownerID := uuid.New()
	otherOwnerID := uuid.New()
	fixture := v49Fixture{
		salonID: uuid.New(), otherSalonID: uuid.New(), service1ID: uuid.New(), service2ID: uuid.New(),
		staff1ID: uuid.New(), staff2ID: uuid.New(), resourcePoolID: uuid.New(),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id,email,password_hash,full_name) VALUES
			($1,$2,'hash','Owner One'),($3,$4,'hash','Owner Two')
	`, ownerID, "v49-owner-"+uuid.NewString()+"@example.com", otherOwnerID, "v49-other-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed execution owners: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (id,name,phone,owner_user_id) VALUES
			($1,'Salon One','5551000',$2),($3,'Salon Two','5551001',$4)
	`, fixture.salonID, ownerID, fixture.otherSalonID, otherOwnerID); err != nil {
		t.Fatalf("seed execution salons: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id,scheduling_authority) VALUES
			($1,'manleai_calendar'),($2,'manleai_calendar')
	`, fixture.salonID, fixture.otherSalonID); err != nil {
		t.Fatalf("seed execution salon settings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO services (id,salon_id,name,duration_minutes,ai_bookable,active) VALUES
			($1,$2,'Dynamic service one',60,true,true),($3,$2,'Dynamic service two',60,true,true),
			(gen_random_uuid(),$4,'Other tenant service',60,true,true)
	`, fixture.service1ID, fixture.salonID, fixture.service2ID, fixture.otherSalonID); err != nil {
		t.Fatalf("seed execution services: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staff (id,salon_id,name,ai_bookable,active) VALUES
			($1,$2,'Dynamic staff one',true,true),($3,$2,'Dynamic staff two',true,true),
			(gen_random_uuid(),$4,'Other tenant staff',true,true)
	`, fixture.staff1ID, fixture.salonID, fixture.staff2ID, fixture.otherSalonID); err != nil {
		t.Fatalf("seed execution staff: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_configs (
			salon_id,slot_step_minutes,minimum_booking_notice_minutes,booking_horizon_days,
			max_party_size,default_buffer_before_minutes,default_buffer_after_minutes
		) VALUES ($1,15,0,90,10,5,10)
	`, fixture.salonID); err != nil {
		t.Fatalf("seed execution config: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_resource_pools (id,salon_id,name,capacity)
		VALUES ($1,$2,'Dynamic pool',4)
	`, fixture.resourcePoolID, fixture.salonID); err != nil {
		t.Fatalf("seed execution resource pool: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT version FROM manleai_calendar_configs WHERE salon_id=$1`, fixture.salonID).Scan(&fixture.configVersion); err != nil {
		t.Fatalf("load execution config version: %v", err)
	}
	return fixture
}

func insertV49PartyQuote(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, quoteID, slotID, segment1ID, segment2ID uuid.UUID, operation string, targetID uuid.UUID, targetVersion int, start time.Time) {
	t.Helper()
	var target any
	var version any
	if targetID != uuid.Nil {
		target = targetID
		version = targetVersion
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id,salon_id,provider,provider_location_id,provider_snapshot_generation,
			scheduling_authority,request_fingerprint,expires_at,scheduling_authority_version,
			authority_config_version,operation_type,target_appointment_id,
			target_authority_appointment_version,party_size
		) VALUES ($1,$2,NULL,NULL,NULL,'manleai_calendar',$3,now()+interval '1 hour',1,$4,$5,$6,$7,2)
	`, quoteID, fixture.salonID, strings.Repeat("a", 64), fixture.configVersion, operation, target, version); err != nil {
		t.Fatalf("insert %s party quote: %v", operation, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slots (id,salon_id,quote_id,slot_fingerprint,start_time,end_time,segments)
		VALUES ($1,$2,$3,$4,$5,$6,'[]'::jsonb)
	`, slotID, fixture.salonID, quoteID, strings.Repeat("1", 64), start, start.Add(time.Hour)); err != nil {
		t.Fatalf("insert %s party quote slot: %v", operation, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slot_segments (
			id,salon_id,quote_slot_id,service_id,staff_id,staff_selection_mode,guest_reference,
			duration_minutes,sort_order,scheduled_start_time,scheduled_end_time,
			buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time
		) VALUES
			($1,$2,$3,$4,$5,'specific','guest-one',60,1,$6,$7,5,10,$8,$9),
			($10,$2,$3,$11,$12,'specific','guest-two',60,2,$6,$7,5,10,$8,$9)
	`, segment1ID, fixture.salonID, slotID, fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour),
		start.Add(-5*time.Minute), start.Add(70*time.Minute), segment2ID, fixture.service2ID, fixture.staff2ID); err != nil {
		t.Fatalf("insert %s party quote segments: %v", operation, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slot_resource_allocations (
			id,salon_id,quote_slot_segment_id,resource_pool_id,units_allocated
		) VALUES (gen_random_uuid(),$1,$2,$3,1)
	`, fixture.salonID, segment1ID, fixture.resourcePoolID); err != nil {
		t.Fatalf("insert %s party quote resource allocation: %v", operation, err)
	}
}

func insertV49SingleQuote(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, quoteID, slotID uuid.UUID, operation string, start time.Time, serviceID, staffID uuid.UUID) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id,salon_id,provider,provider_location_id,provider_snapshot_generation,
			scheduling_authority,request_fingerprint,expires_at,scheduling_authority_version,
			authority_config_version,operation_type,party_size
		) VALUES ($1,$2,NULL,NULL,NULL,'manleai_calendar',$3,now()+interval '1 hour',1,$4,$5,1)
	`, quoteID, fixture.salonID, strings.Repeat("c", 64), fixture.configVersion, operation); err != nil {
		t.Fatalf("insert single quote: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slots (id,salon_id,quote_id,slot_fingerprint,start_time,end_time,segments)
		VALUES ($1,$2,$3,$4,$5,$6,'[]'::jsonb)
	`, slotID, fixture.salonID, quoteID, strings.Repeat("1", 64), start, start.Add(time.Hour)); err != nil {
		t.Fatalf("insert single quote slot: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slot_segments (
			id,salon_id,quote_slot_id,service_id,staff_id,staff_selection_mode,duration_minutes,
			sort_order,scheduled_start_time,scheduled_end_time,buffer_before_minutes,
			buffer_after_minutes,occupied_start_time,occupied_end_time
		) VALUES (gen_random_uuid(),$1,$2,$3,$4,'specific',60,1,$5,$6,5,10,$7,$8)
	`, fixture.salonID, slotID, serviceID, staffID, start, start.Add(time.Hour), start.Add(-5*time.Minute), start.Add(70*time.Minute)); err != nil {
		t.Fatalf("insert single quote segment: %v", err)
	}
}

func insertV49PartyAttempt(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, attemptID, appointmentID, quoteID, slotID uuid.UUID, operation, status string, targetID uuid.UUID, targetVersion, resultVersion int, start time.Time) {
	t.Helper()
	var quote, slot, target, targetVersionValue any
	if quoteID != uuid.Nil {
		quote = quoteID
		slot = strings.Repeat("1", 64)
	}
	if targetID != uuid.Nil {
		target = targetID
		targetVersionValue = targetVersion
	}
	operationKey := operation + "-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			id,salon_id,source,status,pos_provider,target_pos_booking_version,operation_key,
			request_fingerprint,availability_quote_id,availability_slot_fingerprint,
			operation_type,target_appointment_id,target_authority_appointment_version,
			provider_outcome,retry_policy,reconciliation_status,customer_name,customer_phone,
			service_id,staff_id,staff_selection_mode,requested_start_time,requested_end_time,
			scheduling_authority,authority_appointment_id,authority_appointment_version,
			authority_idempotency_key,scheduling_authority_version,authority_config_version,party_size
		) VALUES ($1,$2,'test',$3,NULL,NULL,$4,$5,$6,$7,$8,$9,$10,'not_started','none',
		          'not_required','Customer','5553000',$11,$12,'specific',$13,$14,
		          'manleai_calendar',$15,$16,$4,1,$17,2)
	`, attemptID, fixture.salonID, status, operationKey, strings.Repeat("d", 64), quote, slot, operation, target, targetVersionValue,
		fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), appointmentID.String(), resultVersion, fixture.configVersion); err != nil {
		t.Fatalf("insert %s party attempt: %v", operation, err)
	}
}

func insertV49SingleAttempt(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, attemptID, appointmentID, quoteID, slotID uuid.UUID, start time.Time) {
	t.Helper()
	operationKey := "conflict-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			id,salon_id,source,status,pos_provider,target_pos_booking_version,operation_key,
			request_fingerprint,availability_quote_id,availability_slot_fingerprint,
			operation_type,provider_outcome,retry_policy,reconciliation_status,
			customer_name,customer_phone,service_id,staff_id,staff_selection_mode,
			requested_start_time,requested_end_time,scheduling_authority,
			authority_appointment_id,authority_appointment_version,authority_idempotency_key,
			scheduling_authority_version,authority_config_version,party_size
		) VALUES ($1,$2,'test','confirmed',NULL,NULL,$3,$4,$5,$6,'book','not_started','none',
		          'not_required','Customer','5553000',$7,$8,'specific',$9,$10,
		          'manleai_calendar',$11,1,$3,1,$12,1)
	`, attemptID, fixture.salonID, operationKey, strings.Repeat("7", 64), quoteID, strings.Repeat("1", 64), fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), appointmentID.String(), fixture.configVersion); err != nil {
		t.Fatalf("insert conflict attempt: %v", err)
	}
}

func insertV49AttemptSegments(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, attemptID, segment1ID, segment2ID uuid.UUID, start time.Time) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			id,salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,pos_service_id,
			scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,
			sort_order,guest_reference,scheduled_start_time,scheduled_end_time,
			buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time
		) VALUES
			($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$13,$14,'Dynamic service one',60,1,'guest-one',$6,$7,5,10,$8,$9),
			($10,$2,$3,$11,$12,'specific',NULL,'manleai_calendar',$15,$16,'Dynamic service two',60,2,'guest-two',$6,$7,5,10,$8,$9)
	`, segment1ID, fixture.salonID, attemptID, fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), start.Add(-5*time.Minute), start.Add(70*time.Minute), segment2ID, fixture.service2ID, fixture.staff2ID,
		fixture.service1ID.String(), fixture.staff1ID.String(), fixture.service2ID.String(), fixture.staff2ID.String()); err != nil {
		t.Fatalf("insert attempt segments: %v", err)
	}
}

func insertV49AttemptResourceAllocation(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, segmentID uuid.UUID) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segment_resource_allocations (
			id,salon_id,booking_attempt_segment_id,resource_pool_id,units_allocated
		) VALUES (gen_random_uuid(),$1,$2,$3,1)
	`, fixture.salonID, segmentID, fixture.resourcePoolID); err != nil {
		t.Fatalf("insert attempt resource allocation: %v", err)
	}
}

func insertV49SingleAttemptSegment(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, attemptID, segmentID uuid.UUID, start time.Time) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			id,salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,pos_service_id,
			scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,
			sort_order,scheduled_start_time,scheduled_end_time,buffer_before_minutes,
			buffer_after_minutes,occupied_start_time,occupied_end_time
		) VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$10,$11,
		          'Dynamic service',60,1,$6,$7,5,10,$8,$9)
	`, segmentID, fixture.salonID, attemptID, fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), start.Add(-5*time.Minute), start.Add(70*time.Minute), fixture.service1ID.String(), fixture.staff1ID.String()); err != nil {
		t.Fatalf("insert single attempt segment: %v", err)
	}
}

func insertV49AppointmentRoot(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, appointmentID, attemptID uuid.UUID, status string, version int, start time.Time) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id,salon_id,booking_attempt_id,pos_provider,pos_appointment_id,pos_appointment_version,
			pos_sync_status,status,customer_name,customer_phone,service_id,staff_id,
			staff_selection_mode,start_time,end_time,scheduling_authority,authority_appointment_id,
			authority_appointment_version,confirmed_at,confirmation_source,
			scheduling_authority_version,authority_config_version,party_size
		) VALUES ($1,$2,$3,NULL,NULL,NULL,NULL,$4,'Customer','5553000',$5,$6,'specific',$7,$8,
		          'manleai_calendar',$11,$9,now(),'manleai_calendar',1,$10,2)
	`, appointmentID, fixture.salonID, attemptID, status, fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), version, fixture.configVersion, appointmentID.String()); err != nil {
		t.Fatalf("insert appointment root: %v", err)
	}
}

func insertV49AppointmentPlan(t *testing.T, ctx context.Context, tx *sql.Tx, fixture v49Fixture, appointmentID, segment1ID, segment2ID uuid.UUID, version int, start time.Time) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointment_services (
			id,salon_id,appointment_id,service_id,staff_id,staff_selection_mode,pos_service_id,
			scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,
			sort_order,plan_version,guest_reference,scheduled_start_time,scheduled_end_time,
			buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time
		) VALUES
			($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$14,$15,'Dynamic service one',60,1,$6,'guest-one',$7,$8,5,10,$9,$10),
			($11,$2,$3,$12,$13,'specific',NULL,'manleai_calendar',$16,$17,'Dynamic service two',60,2,$6,'guest-two',$7,$8,5,10,$9,$10)
	`, segment1ID, fixture.salonID, appointmentID, fixture.service1ID, fixture.staff1ID, version, start, start.Add(time.Hour), start.Add(-5*time.Minute), start.Add(70*time.Minute), segment2ID, fixture.service2ID, fixture.staff2ID,
		fixture.service1ID.String(), fixture.staff1ID.String(), fixture.service2ID.String(), fixture.staff2ID.String()); err != nil {
		t.Fatalf("insert appointment plan %d: %v", version, err)
	}
}

func insertV49ExecutionEvent(t *testing.T, ctx context.Context, tx *sql.Tx, salonID, attemptID, appointmentID uuid.UUID, eventType string, version int, configVersion int64) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_execution_events (
			id,salon_id,booking_attempt_id,appointment_id,event_type,
			scheduling_authority_version,authority_config_version,authority_appointment_version
		) VALUES (gen_random_uuid(),$1,$2,$3,$4,1,$5,$6)
	`, salonID, attemptID, appointmentID, eventType, configVersion, version); err != nil {
		t.Fatalf("insert execution event %s: %v", eventType, err)
	}
}

func consumeV49Quote(t *testing.T, ctx context.Context, tx *sql.Tx, salonID, quoteID, attemptID uuid.UUID) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		UPDATE availability_quotes SET consumed_at=now(), consumed_by_attempt_id=$1 WHERE id=$2 AND salon_id=$3
	`, attemptID, quoteID, salonID); err != nil {
		t.Fatalf("consume quote: %v", err)
	}
}

func setV49ConstraintsImmediate(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatalf("validate V49 deferred constraints: %v", err)
	}
}

func setV49ConstraintsDeferred(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		t.Fatalf("defer V49 constraints: %v", err)
	}
}

func assertV49OperationAbsent(t *testing.T, ctx context.Context, tx *sql.Tx, attemptID, appointmentID uuid.UUID) {
	t.Helper()
	var attempts, appointments, segments, events int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM booking_attempts WHERE id=$1`, attemptID).Scan(&attempts); err != nil {
		t.Fatalf("count rolled-back attempts: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM appointments WHERE id=$1`, appointmentID).Scan(&appointments); err != nil {
		t.Fatalf("count rolled-back appointments: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM appointment_services WHERE appointment_id=$1`, appointmentID).Scan(&segments); err != nil {
		t.Fatalf("count rolled-back reservations: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_execution_events WHERE booking_attempt_id=$1`, attemptID).Scan(&events); err != nil {
		t.Fatalf("count rolled-back events: %v", err)
	}
	if attempts != 0 || appointments != 0 || segments != 0 || events != 0 {
		t.Fatalf("conflict rollback left attempts=%d appointments=%d reservations=%d events=%d", attempts, appointments, segments, events)
	}
}

func expectV49PostgresError(t *testing.T, ctx context.Context, tx *sql.Tx, code, constraint, query string, args ...any) {
	t.Helper()
	savepoint := pq.QuoteIdentifier("v49_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create V49 savepoint: %v", err)
	}
	_, execErr := tx.ExecContext(ctx, query, args...)
	if execErr == nil {
		_, execErr = tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	}
	assertV49PostgresError(t, execErr, code, constraint)
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("roll back V49 savepoint: %v", err)
	}
}

func assertV49PostgresError(t *testing.T, err error, code, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected PostgreSQL error %s/%s", code, constraint)
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("error=%v, want PostgreSQL error %s/%s", err, code, constraint)
	}
	if string(pqErr.Code) != code || pqErr.Constraint != constraint {
		t.Fatalf("PostgreSQL error=%s/%s, want %s/%s: %v", pqErr.Code, pqErr.Constraint, code, constraint, err)
	}
}
