package migrations

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func readV51(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V51__manleai_calendar_lifecycle_guards.sql")
	if err != nil {
		t.Fatalf("read V51 migration: %v", err)
	}
	return string(source)
}

func TestV51DefinesTenantFencedLifecycleGraphWithoutProductHardcoding(t *testing.T) {
	source := readV51(t)
	for _, fragment := range []string{
		"ADD COLUMN released_by_attempt_id UUID",
		"appointment_services_released_by_attempt_tenant_fk",
		"appointment_services_release_owner_pair_check",
		"manleai_calendar_execution_events_appointment_version_key",
		"UNIQUE (salon_id, appointment_id, authority_appointment_version)",
		"validate_manleai_calendar_lifecycle_graph",
		"enforce_manleai_calendar_lifecycle_graph",
		"manleai_calendar_lifecycle_root_immutable_guard",
		"manleai_calendar_lifecycle_root_transition_guard",
		"ManleAI Calendar cancellation requires its exact prior-plan snapshot",
		"ManleAI Calendar reschedule quote, attempt, and new plan must match exactly",
		"validate_manleai_calendar_quote_resource_integrity",
		"generate_series(1, root_item.authority_appointment_version)",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V51 is missing lifecycle safety contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"pedicure",
		"manicure",
		"square",
		"service_id = '",
		"staff_id = '",
		"resource_pool_id = '",
	} {
		if strings.Contains(strings.ToLower(source), forbidden) {
			t.Fatalf("V51 must derive lifecycle evidence from persisted rows; found %q", forbidden)
		}
	}
}

func TestV51AppliesAfterV50(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v51_upgrade_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 51)
}

func TestV51LifecycleAtomicHistoryAndAuthoritySwitch(t *testing.T) {
	if os.Getenv("MIGRATION_TEST_DATABASE_URL") == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, ctx, tx := beginV49PostgresTest(t, "v51_lifecycle_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 51)

	fixture := seedV50Fixture(t, ctx, tx, 3, false)
	start := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Minute)
	book, err := insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff1ID, "", start}})
	if err != nil {
		t.Fatalf("insert initial V51 book: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='external_provider' WHERE salon_id=$1`, fixture.salonID); err != nil {
		t.Fatalf("switch current authority before historical internal lifecycle: %v", err)
	}

	rescheduled, err := insertV51Reschedule(ctx, tx, fixture, book, start.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("reschedule historical internal appointment after authority switch: %v", err)
	}

	withV50Savepoint(t, ctx, tx, func() error {
		_, err := tx.ExecContext(ctx, `UPDATE appointments SET customer_name='rewritten' WHERE id=$1`, book.appointmentID)
		return err
	}, "23514", "manleai_calendar_lifecycle_root_immutable_guard")
	withV50Savepoint(t, ctx, tx, func() error {
		return insertV51Cancel(ctx, tx, fixture, book.appointmentID, rescheduled, start.Add(3*time.Hour), true, false, false)
	}, "23514", "manleai_calendar_lifecycle_graph_guard")
	withV50Savepoint(t, ctx, tx, func() error {
		return insertV51Cancel(ctx, tx, fixture, book.appointmentID, rescheduled, start.Add(3*time.Hour), false, true, false)
	}, "23514", "manleai_calendar_lifecycle_graph_guard")
	withV50Savepoint(t, ctx, tx, func() error {
		return insertV51Cancel(ctx, tx, fixture, book.appointmentID, rescheduled, start.Add(3*time.Hour), false, false, true)
	}, "23514", "manleai_calendar_lifecycle_graph_guard")

	if err := insertV51Cancel(ctx, tx, fixture, book.appointmentID, rescheduled, start.Add(3*time.Hour), false, false, false); err != nil {
		t.Fatalf("cancel exact current plan after invalid rollback: %v", err)
	}

	var version int
	var status string
	var activePlans, unownedReleases, activeResources, eventCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT authority_appointment_version,status FROM appointments WHERE id=$1
	`, book.appointmentID).Scan(&version, &status); err != nil {
		t.Fatalf("load final lifecycle root: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE released_at IS NULL),
		       count(*) FILTER (WHERE released_at IS NOT NULL AND released_by_attempt_id IS NULL)
		FROM appointment_services WHERE appointment_id=$1
	`, book.appointmentID).Scan(&activePlans, &unownedReleases); err != nil {
		t.Fatalf("load final plan history: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE allocation.released_at IS NULL)
		FROM manleai_calendar_appointment_resource_allocations allocation
		JOIN appointment_services segment ON segment.id=allocation.appointment_service_id
		WHERE segment.appointment_id=$1
	`, book.appointmentID).Scan(&activeResources); err != nil {
		t.Fatalf("load final resource history: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_execution_events WHERE appointment_id=$1`, book.appointmentID).Scan(&eventCount); err != nil {
		t.Fatalf("load final event history: %v", err)
	}
	if version != 3 || status != "cancelled" || activePlans != 0 || unownedReleases != 0 || activeResources != 0 || eventCount != 3 {
		t.Fatalf("final lifecycle version=%d status=%s activePlans=%d unownedReleases=%d activeResources=%d events=%d", version, status, activePlans, unownedReleases, activeResources, eventCount)
	}
}

type v51LifecycleIDs struct {
	attemptID uuid.UUID
	segmentID uuid.UUID
}

func insertV51Reschedule(ctx context.Context, tx *sql.Tx, fixture v50Fixture, book v50BookIDs, start time.Time) (v51LifecycleIDs, error) {
	ids := v51LifecycleIDs{attemptID: uuid.New(), segmentID: uuid.New()}
	quoteID, slotID, quoteSegmentID, planSegmentID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (id,salon_id,scheduling_authority,request_fingerprint,expires_at,
			scheduling_authority_version,authority_config_version,operation_type,target_appointment_id,
			target_authority_appointment_version,party_size)
		VALUES ($1,$2,'manleai_calendar',$3,now()+interval '1 hour',1,$4,'reschedule',$5,1,1)
	`, quoteID, fixture.salonID, strings.Repeat("a", 64), fixture.configVersion, book.appointmentID); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO availability_quote_slots (id,salon_id,quote_id,slot_fingerprint,start_time,end_time,segments) VALUES ($1,$2,$3,$4,$5,$6,'[]'::jsonb)`, slotID, fixture.salonID, quoteID, strings.Repeat("2", 64), start, start.Add(time.Hour)); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slot_segments (id,salon_id,quote_slot_id,service_id,staff_id,staff_selection_mode,duration_minutes,sort_order,scheduled_start_time,scheduled_end_time,buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time)
		VALUES ($1,$2,$3,$4,$5,'specific',60,1,$6,$7,0,0,$6,$7)
	`, quoteSegmentID, fixture.salonID, slotID, fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour)); err != nil {
		return ids, err
	}
	if err := insertV50QuoteResources(ctx, tx, fixture, quoteSegmentID, fixture.service1ID); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (id,salon_id,source,status,pos_provider,target_pos_booking_version,operation_key,request_fingerprint,availability_quote_id,availability_slot_fingerprint,operation_type,target_appointment_id,target_authority_appointment_version,provider_outcome,retry_policy,reconciliation_status,customer_name,customer_phone,service_id,staff_id,staff_selection_mode,requested_start_time,requested_end_time,scheduling_authority,authority_appointment_id,authority_appointment_version,authority_idempotency_key,scheduling_authority_version,authority_config_version,party_size)
		VALUES ($1,$2,'test','rescheduled',NULL,NULL,$3,$4,$5,$6,'reschedule',$7,1,'not_started','none','not_required','Customer','5553000',$8,$9,'specific',$10,$11,'manleai_calendar',$12,2,$3,1,$13,1)
	`, ids.attemptID, fixture.salonID, "v51-reschedule-"+uuid.NewString(), strings.Repeat("b", 64), quoteID, strings.Repeat("2", 64), book.appointmentID, fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), book.appointmentID.String(), fixture.configVersion); err != nil {
		return ids, err
	}
	if err := insertV51AttemptSegment(ctx, tx, fixture, ids.attemptID, ids.segmentID, start); err != nil {
		return ids, err
	}
	if err := insertV50AttemptResources(ctx, tx, fixture, ids.segmentID, fixture.service1ID); err != nil {
		return ids, err
	}
	releasedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE manleai_calendar_appointment_resource_allocations SET released_at=$1 WHERE appointment_service_id IN (SELECT id FROM appointment_services WHERE appointment_id=$2 AND released_at IS NULL)`, releasedAt, book.appointmentID); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE appointment_services SET released_at=$1,released_by_attempt_id=$2 WHERE appointment_id=$3 AND released_at IS NULL`, releasedAt, ids.attemptID, book.appointmentID); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE appointments SET booking_attempt_id=$1,status='rescheduled',authority_appointment_version=2,start_time=$2,end_time=$3,updated_at=now() WHERE id=$4`, ids.attemptID, start, start.Add(time.Hour), book.appointmentID); err != nil {
		return ids, err
	}
	if err := insertV51AppointmentSegment(ctx, tx, fixture, book.appointmentID, planSegmentID, 2, start); err != nil {
		return ids, err
	}
	if err := insertV51AppointmentResources(ctx, tx, fixture, planSegmentID, fixture.service1ID, 2, start); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO manleai_calendar_execution_events (id,salon_id,booking_attempt_id,appointment_id,event_type,scheduling_authority_version,authority_config_version,authority_appointment_version) VALUES (gen_random_uuid(),$1,$2,$3,'appointment_rescheduled',1,$4,2)`, fixture.salonID, ids.attemptID, book.appointmentID, fixture.configVersion); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE availability_quotes SET consumed_at=now(),consumed_by_attempt_id=$1 WHERE id=$2`, ids.attemptID, quoteID); err != nil {
		return ids, err
	}
	_, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	return ids, err
}

func insertV51Cancel(ctx context.Context, tx *sql.Tx, fixture v50Fixture, appointmentID uuid.UUID, prior v51LifecycleIDs, start time.Time, wrongSnapshot, wrongConfig, cancelledVersionPlan bool) error {
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return err
	}
	attemptID, segmentID := uuid.New(), uuid.New()
	snapshotStart := start
	if wrongSnapshot {
		snapshotStart = snapshotStart.Add(time.Minute)
	}
	configVersion := fixture.configVersion
	if wrongConfig {
		configVersion++
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (id,salon_id,source,status,pos_provider,target_pos_booking_version,operation_key,request_fingerprint,operation_type,target_appointment_id,target_authority_appointment_version,provider_outcome,retry_policy,reconciliation_status,customer_name,customer_phone,service_id,staff_id,staff_selection_mode,requested_start_time,requested_end_time,scheduling_authority,authority_appointment_id,authority_appointment_version,authority_idempotency_key,scheduling_authority_version,authority_config_version,party_size)
		VALUES ($1,$2,'test','cancelled',NULL,NULL,$3,$4,'cancel',$5,2,'not_started','none','not_required','Customer','5553000',$6,$7,'specific',$8,$9,'manleai_calendar',$10,3,$3,1,$11,1)
	`, attemptID, fixture.salonID, "v51-cancel-"+uuid.NewString(), strings.Repeat("c", 64), appointmentID, fixture.service1ID, fixture.staff1ID, snapshotStart, snapshotStart.Add(time.Hour), appointmentID.String(), configVersion); err != nil {
		return err
	}
	if err := insertV51AttemptSegment(ctx, tx, fixture, attemptID, segmentID, snapshotStart); err != nil {
		return err
	}
	if err := insertV50AttemptResources(ctx, tx, fixture, segmentID, fixture.service1ID); err != nil {
		return err
	}
	releasedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE manleai_calendar_appointment_resource_allocations SET released_at=$1 WHERE appointment_service_id IN (SELECT id FROM appointment_services WHERE appointment_id=$2 AND released_at IS NULL)`, releasedAt, appointmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE appointment_services SET released_at=$1,released_by_attempt_id=$2 WHERE appointment_id=$3 AND released_at IS NULL`, releasedAt, attemptID, appointmentID); err != nil {
		return err
	}
	if cancelledVersionPlan {
		segmentID := uuid.New()
		if err := insertV51AppointmentSegment(ctx, tx, fixture, appointmentID, segmentID, 3, start.Add(2*time.Hour)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE appointment_services SET released_at=$1,released_by_attempt_id=$2 WHERE id=$3`, releasedAt, attemptID, segmentID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE appointments SET booking_attempt_id=$1,status='cancelled',authority_appointment_version=3,authority_config_version=$2,updated_at=now() WHERE id=$3`, attemptID, configVersion, appointmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO manleai_calendar_execution_events (id,salon_id,booking_attempt_id,appointment_id,event_type,scheduling_authority_version,authority_config_version,authority_appointment_version) VALUES (gen_random_uuid(),$1,$2,$3,'appointment_cancelled',1,$4,3)`, fixture.salonID, attemptID, appointmentID, configVersion); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	return err
}

func insertV51AttemptSegment(ctx context.Context, tx *sql.Tx, fixture v50Fixture, attemptID, segmentID uuid.UUID, start time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (id,salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,pos_service_id,scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,sort_order,scheduled_start_time,scheduled_end_time,buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time)
		VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$6,$7,'Dynamic service',60,1,$8,$9,0,0,$8,$9)
	`, segmentID, fixture.salonID, attemptID, fixture.service1ID, fixture.staff1ID, fixture.service1ID.String(), fixture.staff1ID.String(), start, start.Add(time.Hour))
	return err
}

func insertV51AppointmentSegment(ctx context.Context, tx *sql.Tx, fixture v50Fixture, appointmentID, segmentID uuid.UUID, version int, start time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO appointment_services (id,salon_id,appointment_id,service_id,staff_id,staff_selection_mode,pos_service_id,scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,sort_order,plan_version,scheduled_start_time,scheduled_end_time,buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time)
		VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$6,$7,'Dynamic service',60,1,$8,$9,$10,0,0,$9,$10)
	`, segmentID, fixture.salonID, appointmentID, fixture.service1ID, fixture.staff1ID, fixture.service1ID.String(), fixture.staff1ID.String(), version, start, start.Add(time.Hour))
	return err
}

func insertV51AppointmentResources(ctx context.Context, tx *sql.Tx, fixture v50Fixture, segmentID, serviceID uuid.UUID, version int, start time.Time) error {
	requirements, err := loadV50ResourceRequirements(ctx, tx, fixture.salonID, serviceID)
	if err != nil {
		return err
	}
	for _, requirement := range requirements {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manleai_calendar_appointment_resource_allocations (
				id,salon_id,appointment_service_id,resource_pool_id,units_allocated,
				plan_version,occupied_start_time,occupied_end_time
			) VALUES (gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7)
		`, fixture.salonID, segmentID, requirement.poolID, requirement.units, version, start, start.Add(time.Hour)); err != nil {
			return err
		}
	}
	return nil
}
