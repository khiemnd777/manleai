package migrations

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
	"github.com/lib/pq"
)

func readV50(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V50__manleai_calendar_pooled_capacity_guards.sql")
	if err != nil {
		t.Fatalf("read V50 migration: %v", err)
	}
	return string(source)
}

func TestV50DefinesPooledCapacityAndExactBookEvidenceGuards(t *testing.T) {
	source := readV50(t)
	for _, fragment := range []string{
		"CREATE FUNCTION lock_manleai_calendar_resource_pools",
		"pg_advisory_xact_lock",
		"booking-calendar-reconciliation:",
		"ORDER BY pool.id",
		"FOR UPDATE OF pool",
		"CREATE FUNCTION validate_manleai_calendar_resource_capacity",
		"candidate_windows",
		"relevant_allocations",
		"relevant_overrides",
		"probe_points",
		"tstzrange(allocation.occupied_start_time, allocation.occupied_end_time, '[)')",
		"COALESCE(override_item.capacity_override, pool.capacity)",
		"manleai_calendar_appointment_resource_capacity_guard",
		"CREATE FUNCTION validate_manleai_calendar_execution_graph",
		"attempt_item.operation_type <> 'book'",
		"quote, attempt, and appointment segments must match exactly",
		"quote, attempt, and appointment resources must match exactly",
		"availability_quotes_manleai_calendar_immutable_guard",
		"booking_attempts_manleai_calendar_immutable_guard",
		"manleai_calendar_appointment_history_immutable_guard",
		"CREATE INDEX idx_manleai_calendar_resource_capacity_exceptions",
		"AND quote_item.party_size <> 1",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V50 is missing persistence safety contract %q", fragment)
		}
	}

	for _, forbidden := range []string{
		"CREATE TABLE manleai_calendar_resource_reservations",
		"ALTER TABLE manleai_calendar_resource_pools ADD COLUMN",
		"pedicure",
		"manicure",
		"square",
		"resource_pool_id = '",
		"service_id = '",
		"staff_id = '",
	} {
		if strings.Contains(strings.ToLower(source), forbidden) {
			t.Fatalf("V50 must reuse V48/V49 owners without hardcoded product data; found %q", forbidden)
		}
	}
}

func TestV50AppliesAfterCompleteV49Schema(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v50_upgrade_")
	defer db.Close()
	defer tx.Rollback()

	applyV49MigrationChain(t, ctx, tx, 49)
	if _, err := tx.ExecContext(ctx, readV50(t)); err != nil {
		t.Fatalf("apply V50 after V49: %v", err)
	}
	if _, err := tx.ExecContext(ctx, readV50(t)); err == nil {
		t.Fatal("directly applying V50 twice unexpectedly succeeded")
	}
}

func TestV50CapacityOverridesExactGraphRollbackAndHistory(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v50_capacity_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 50)

	fixture := seedV50Fixture(t, ctx, tx, 2, false)
	start := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Minute)

	first, err := insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff1ID, "", start}})
	if err != nil {
		t.Fatalf("insert first pooled book: %v", err)
	}
	if _, err := insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff2ID, "", start}}); err != nil {
		t.Fatalf("insert second pooled book: %v", err)
	}
	withV50Savepoint(t, ctx, tx, func() error {
		_, execErr := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segment_resource_allocations (
				id,salon_id,booking_attempt_segment_id,resource_pool_id,units_allocated
			) VALUES (gen_random_uuid(),$1,$2,$3,1)
		`, fixture.salonID, first.attemptSegmentIDs[0], fixture.resourcePool2ID)
		return execErr
	}, "23514", "booking_attempts_manleai_calendar_book_graph_guard")
	withV50Savepoint(t, ctx, tx, func() error {
		_, execErr := tx.ExecContext(ctx, `
			SELECT lock_manleai_calendar_resource_pools($1,ARRAY[$2]::UUID[])
		`, fixture.otherSalonID, fixture.resourcePool1ID)
		return execErr
	}, "23514", "manleai_calendar_resource_pool_lock_guard")

	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_resource_pools SET capacity=1 WHERE id=$1
	`, fixture.resourcePool1ID); err != nil {
		t.Fatalf("reduce first pool capacity: %v", err)
	}
	activateV50Fixture(t, ctx, tx, &fixture)
	var grandfathered int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM manleai_calendar_appointment_resource_allocations allocation
		WHERE allocation.salon_id=$1 AND allocation.resource_pool_id=$2
		  AND allocation.released_at IS NULL
	`, fixture.salonID, fixture.resourcePool1ID).Scan(&grandfathered); err != nil {
		t.Fatalf("count grandfathered allocations: %v", err)
	}
	if grandfathered != 2 {
		t.Fatalf("grandfathered active allocations=%d, want 2", grandfathered)
	}

	overCapacity := newV50BookIDs()
	withV50Savepoint(t, ctx, tx, func() error {
		var insertErr error
		overCapacity, insertErr = insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff3ID, "", start}})
		return insertErr
	}, "23514", "manleai_calendar_appointment_resource_capacity_guard")
	assertV50OperationAbsent(t, ctx, tx, overCapacity)

	partyOverCapacity := newV50BookIDs()
	withV50Savepoint(t, ctx, tx, func() error {
		var insertErr error
		partyOverCapacity, insertErr = insertV50Book(ctx, tx, fixture, []v50BookSegment{
			{fixture.service1ID, fixture.staff1ID, "guest-a", start.Add(3 * time.Hour)},
			{fixture.service1ID, fixture.staff2ID, "guest-b", start.Add(3 * time.Hour)},
			{fixture.service1ID, fixture.staff3ID, "guest-c", start.Add(3 * time.Hour)},
		})
		return insertErr
	}, "23514", "manleai_calendar_appointment_resource_capacity_guard")
	assertV50OperationAbsent(t, ctx, tx, partyOverCapacity)

	mixedGuestReferences := newV50BookIDs()
	withV50Savepoint(t, ctx, tx, func() error {
		var insertErr error
		mixedGuestReferences, insertErr = insertV50Book(ctx, tx, fixture, []v50BookSegment{
			{fixture.service1ID, fixture.staff1ID, "guest-a", start.Add(5 * time.Hour)},
			{fixture.service1ID, fixture.staff2ID, "", start.Add(5 * time.Hour)},
		})
		return insertErr
	}, "23514", "availability_quotes_manleai_calendar_party_guard")
	assertV50OperationAbsent(t, ctx, tx, mixedGuestReferences)

	withV50Savepoint(t, ctx, tx, func() error {
		_, execErr := tx.ExecContext(ctx, `
			UPDATE booking_attempt_segment_resource_allocations
			SET units_allocated=2
			WHERE booking_attempt_segment_id=$1
		`, first.attemptSegmentIDs[0])
		return execErr
	}, "23514", "booking_attempts_manleai_calendar_immutable_guard")

	boundary := start.Add(8 * time.Hour)
	ownerID := v50OwnerID(t, ctx, tx, fixture.salonID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_exceptions (
			id,salon_id,scope_type,resource_pool_id,effect,starts_at,ends_at,
			capacity_override,created_by_user_id
		) VALUES (gen_random_uuid(),$1,'resource',$2,'capacity_override',$3,$4,0,$5)
	`, fixture.salonID, fixture.resourcePool1ID, boundary, boundary.Add(time.Hour), ownerID); err != nil {
		t.Fatalf("insert zero-capacity override: %v", err)
	}
	activateV50Fixture(t, ctx, tx, &fixture)

	if _, err := insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff1ID, "", boundary.Add(-time.Hour)}}); err != nil {
		t.Fatalf("adjacent book ending at override start: %v", err)
	}
	insideOverride := newV50BookIDs()
	withV50Savepoint(t, ctx, tx, func() error {
		var insertErr error
		insideOverride, insertErr = insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff2ID, "", boundary}})
		return insertErr
	}, "23514", "manleai_calendar_appointment_resource_capacity_guard")
	assertV50OperationAbsent(t, ctx, tx, insideOverride)
	if _, err := insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, fixture.staff2ID, "", boundary.Add(time.Hour)}}); err != nil {
		t.Fatalf("adjacent book starting at override end: %v", err)
	}

	withV50Savepoint(t, ctx, tx, func() error {
		_, execErr := tx.ExecContext(ctx, `
			UPDATE manleai_calendar_appointment_resource_allocations
			SET units_allocated=2
			WHERE id=$1
		`, first.appointmentResourceIDs[0])
		return execErr
	}, "23514", "manleai_calendar_appointment_history_immutable_guard")

	if _, err := tx.ExecContext(ctx, `
		UPDATE salon_settings SET scheduling_authority='external_provider' WHERE salon_id=$1
	`, fixture.salonID); err != nil {
		t.Fatalf("switch selected authority before target-origin cancellation: %v", err)
	}
	if err := cancelV50Book(ctx, tx, fixture, first, start); err != nil {
		t.Fatalf("release first book through cancellation: %v", err)
	}
	withV50Savepoint(t, ctx, tx, func() error {
		_, execErr := tx.ExecContext(ctx, `
			UPDATE manleai_calendar_appointment_resource_allocations
			SET released_at=NULL
			WHERE id=$1
		`, first.appointmentResourceIDs[0])
		return execErr
	}, "23514", "manleai_calendar_appointment_history_immutable_guard")
}

func TestV50ConcurrentReversePoolOrdersSerializeWithoutOvercapacity(t *testing.T) {
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
	schemaName := "v50_concurrent_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create concurrent schema: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP SCHEMA "+quotedSchema+" CASCADE") }()

	setupTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin concurrent setup: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("set concurrent setup search path: %v", err)
	}
	applyV49MigrationChain(t, ctx, setupTx, 50)
	fixture := seedV50Fixture(t, ctx, setupTx, 1, true)
	if err := setupTx.Commit(); err != nil {
		t.Fatalf("commit concurrent setup: %v", err)
	}

	start := time.Now().UTC().Add(96 * time.Hour).Truncate(time.Minute)
	type result struct{ err error }
	results := make(chan result, 2)
	orders := [][]uuid.UUID{
		{fixture.resourcePool1ID, fixture.resourcePool2ID},
		{fixture.resourcePool2ID, fixture.resourcePool1ID},
	}
	staffIDs := []uuid.UUID{fixture.staff1ID, fixture.staff2ID}
	var ready sync.WaitGroup
	ready.Add(2)
	startGate := make(chan struct{})
	for i := range orders {
		go func(index int) {
			tx, beginErr := db.BeginTx(ctx, nil)
			if beginErr != nil {
				results <- result{beginErr}
				return
			}
			defer tx.Rollback()
			if _, setErr := tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+", public"); setErr != nil {
				results <- result{setErr}
				return
			}
			ready.Done()
			<-startGate
			if _, lockErr := tx.ExecContext(ctx, `SELECT lock_manleai_calendar_resource_pools($1,$2)`, fixture.salonID, pq.Array(orders[index])); lockErr != nil {
				results <- result{lockErr}
				return
			}
			if _, insertErr := insertV50Book(ctx, tx, fixture, []v50BookSegment{{fixture.service1ID, staffIDs[index], "", start}}); insertErr != nil {
				results <- result{insertErr}
				return
			}
			results <- result{tx.Commit()}
		}(i)
	}
	ready.Wait()
	close(startGate)

	successes := 0
	capacityFailures := 0
	for range 2 {
		item := <-results
		if item.err == nil {
			successes++
			continue
		}
		var pqErr *pq.Error
		if errors.As(item.err, &pqErr) && string(pqErr.Code) == "23514" && pqErr.Constraint == "manleai_calendar_appointment_resource_capacity_guard" {
			capacityFailures++
			continue
		}
		t.Fatalf("unexpected concurrent result: %v", item.err)
	}
	if successes != 1 || capacityFailures != 1 {
		t.Fatalf("concurrent outcomes success=%d capacity_failure=%d, want 1/1", successes, capacityFailures)
	}
}

type v50Fixture struct {
	v49Fixture
	staff3ID        uuid.UUID
	resourcePool1ID uuid.UUID
	resourcePool2ID uuid.UUID
}

type v50BookSegment struct {
	serviceID uuid.UUID
	staffID   uuid.UUID
	guest     string
	start     time.Time
}

type v50BookIDs struct {
	quoteID                uuid.UUID
	attemptID              uuid.UUID
	appointmentID          uuid.UUID
	attemptSegmentIDs      []uuid.UUID
	appointmentResourceIDs []uuid.UUID
}

func newV50BookIDs() v50BookIDs {
	return v50BookIDs{quoteID: uuid.New(), attemptID: uuid.New(), appointmentID: uuid.New()}
}

func seedV50Fixture(t *testing.T, ctx context.Context, tx *sql.Tx, capacity int, secondPool bool) v50Fixture {
	t.Helper()
	base := seedV49ExecutionFixture(t, ctx, tx)
	fixture := v50Fixture{
		v49Fixture:      base,
		staff3ID:        uuid.New(),
		resourcePool1ID: base.resourcePoolID,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staff (id,salon_id,name,ai_bookable,active)
		VALUES ($1,$2,'Dynamic staff three',true,true)
	`, fixture.staff3ID, fixture.salonID); err != nil {
		t.Fatalf("seed third staff: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_resource_pools SET capacity=$1 WHERE id=$2
	`, capacity, fixture.resourcePool1ID); err != nil {
		t.Fatalf("set first pool capacity: %v", err)
	}
	fixture.resourcePool2ID = uuid.New()
	if _, err := tx.ExecContext(ctx, `
			INSERT INTO manleai_calendar_resource_pools (id,salon_id,name,capacity)
			VALUES ($1,$2,'Dynamic pool two',$3)
		`, fixture.resourcePool2ID, fixture.salonID, capacity); err != nil {
		t.Fatalf("seed second resource pool: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_policies (salon_id,service_id,enabled,capacity_mode)
		VALUES ($1,$2,true,'pooled'),($1,$3,true,'staff_only')
	`, fixture.salonID, fixture.service1ID, fixture.service2ID); err != nil {
		t.Fatalf("seed service policies: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_staff (salon_id,service_id,staff_id)
		VALUES ($1,$2,$3),($1,$2,$4),($1,$2,$5),($1,$6,$4)
	`, fixture.salonID, fixture.service1ID, fixture.staff1ID, fixture.staff2ID, fixture.staff3ID, fixture.service2ID); err != nil {
		t.Fatalf("seed service staff links: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_resources (
			salon_id,service_id,resource_pool_id,units_required
		) VALUES ($1,$2,$3,1)
	`, fixture.salonID, fixture.service1ID, fixture.resourcePool1ID); err != nil {
		t.Fatalf("seed first service resource: %v", err)
	}
	if secondPool {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manleai_calendar_service_resources (
				salon_id,service_id,resource_pool_id,units_required
			) VALUES ($1,$2,$3,1)
		`, fixture.salonID, fixture.service1ID, fixture.resourcePool2ID); err != nil {
			t.Fatalf("seed second service resource: %v", err)
		}
	}
	activateV50Fixture(t, ctx, tx, &fixture)
	return fixture
}

func activateV50Fixture(t *testing.T, ctx context.Context, tx *sql.Tx, fixture *v50Fixture) {
	t.Helper()
	ownerID := v50OwnerID(t, ctx, tx, fixture.salonID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_configs
		SET activated_at=clock_timestamp(), activated_by_user_id=$1
		WHERE salon_id=$2
	`, ownerID, fixture.salonID); err != nil {
		t.Fatalf("activate V50 config: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT version FROM manleai_calendar_configs WHERE salon_id=$1`, fixture.salonID).Scan(&fixture.configVersion); err != nil {
		t.Fatalf("load activated V50 config version: %v", err)
	}
}

func v50OwnerID(t *testing.T, ctx context.Context, tx *sql.Tx, salonID uuid.UUID) uuid.UUID {
	t.Helper()
	var ownerID uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT owner_user_id FROM salons WHERE id=$1`, salonID).Scan(&ownerID); err != nil {
		t.Fatalf("load V50 owner: %v", err)
	}
	return ownerID
}

func insertV50Book(ctx context.Context, tx *sql.Tx, fixture v50Fixture, segments []v50BookSegment) (v50BookIDs, error) {
	ids := newV50BookIDs()
	if len(segments) == 0 {
		return ids, fmt.Errorf("book segments are empty")
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return ids, err
	}
	partyGuests := make(map[string]struct{})
	start := segments[0].start
	end := start.Add(time.Hour)
	for _, segment := range segments {
		if segment.start.Before(start) {
			start = segment.start
		}
		if segment.start.Add(time.Hour).After(end) {
			end = segment.start.Add(time.Hour)
		}
		if segment.guest != "" {
			partyGuests[segment.guest] = struct{}{}
		}
	}
	partySize := 1
	if len(partyGuests) > 0 {
		partySize = len(partyGuests)
	}
	slotID := uuid.New()
	slotFingerprint := strings.Repeat("1", 64)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id,salon_id,provider,provider_location_id,provider_snapshot_generation,
			scheduling_authority,request_fingerprint,expires_at,scheduling_authority_version,
			authority_config_version,operation_type,party_size
		) VALUES ($1,$2,NULL,NULL,NULL,'manleai_calendar',$3,now()+interval '1 hour',1,$4,'book',$5)
	`, ids.quoteID, fixture.salonID, strings.Repeat("a", 64), fixture.configVersion, partySize); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quote_slots (id,salon_id,quote_id,slot_fingerprint,start_time,end_time,segments)
		VALUES ($1,$2,$3,$4,$5,$6,'[]'::jsonb)
	`, slotID, fixture.salonID, ids.quoteID, slotFingerprint, start, end); err != nil {
		return ids, err
	}

	quoteSegmentIDs := make([]uuid.UUID, len(segments))
	ids.attemptSegmentIDs = make([]uuid.UUID, len(segments))
	appointmentSegmentIDs := make([]uuid.UUID, len(segments))
	for i, segment := range segments {
		quoteSegmentIDs[i] = uuid.New()
		ids.attemptSegmentIDs[i] = uuid.New()
		appointmentSegmentIDs[i] = uuid.New()
		var guest any
		if segment.guest != "" {
			guest = segment.guest
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO availability_quote_slot_segments (
				id,salon_id,quote_slot_id,service_id,staff_id,staff_selection_mode,guest_reference,
				duration_minutes,sort_order,scheduled_start_time,scheduled_end_time,
				buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time
			) VALUES ($1,$2,$3,$4,$5,'specific',$6,60,$7,$8,$9,0,0,$8,$9)
		`, quoteSegmentIDs[i], fixture.salonID, slotID, segment.serviceID, segment.staffID, guest, i+1, segment.start, segment.start.Add(time.Hour)); err != nil {
			return ids, err
		}
		if err := insertV50QuoteResources(ctx, tx, fixture, quoteSegmentIDs[i], segment.serviceID); err != nil {
			return ids, err
		}
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return ids, err
	}

	operationKey := "v50-book-" + uuid.NewString()
	first := segments[0]
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
		          'manleai_calendar',$11,1,$3,1,$12,$13)
	`, ids.attemptID, fixture.salonID, operationKey, strings.Repeat("b", 64), ids.quoteID, slotFingerprint,
		first.serviceID, first.staffID, start, end, ids.appointmentID.String(), fixture.configVersion, partySize); err != nil {
		return ids, err
	}
	for i, segment := range segments {
		var guest any
		if segment.guest != "" {
			guest = segment.guest
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segments (
				id,salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,pos_service_id,
				scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,
				sort_order,guest_reference,scheduled_start_time,scheduled_end_time,
				buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time
			) VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$6,$7,
			          'Dynamic service',60,$8,$9,$10,$11,0,0,$10,$11)
		`, ids.attemptSegmentIDs[i], fixture.salonID, ids.attemptID, segment.serviceID, segment.staffID,
			segment.serviceID.String(), segment.staffID.String(), i+1, guest, segment.start, segment.start.Add(time.Hour)); err != nil {
			return ids, err
		}
		if err := insertV50AttemptResources(ctx, tx, fixture, ids.attemptSegmentIDs[i], segment.serviceID); err != nil {
			return ids, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id,salon_id,booking_attempt_id,pos_provider,pos_appointment_id,pos_appointment_version,
			pos_sync_status,status,customer_name,customer_phone,service_id,staff_id,
			staff_selection_mode,start_time,end_time,scheduling_authority,authority_appointment_id,
			authority_appointment_version,confirmed_at,confirmation_source,
			scheduling_authority_version,authority_config_version,party_size
		) VALUES ($1,$2,$3,NULL,NULL,NULL,NULL,'confirmed','Customer','5553000',$4,$5,'specific',$6,$7,
		          'manleai_calendar',$8,1,now(),'manleai_calendar',1,$9,$10)
	`, ids.appointmentID, fixture.salonID, ids.attemptID, first.serviceID, first.staffID, start, end,
		ids.appointmentID.String(), fixture.configVersion, partySize); err != nil {
		return ids, err
	}
	for i, segment := range segments {
		var guest any
		if segment.guest != "" {
			guest = segment.guest
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO appointment_services (
				id,salon_id,appointment_id,service_id,staff_id,staff_selection_mode,pos_service_id,
				scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,
				sort_order,plan_version,guest_reference,scheduled_start_time,scheduled_end_time,
				buffer_before_minutes,buffer_after_minutes,occupied_start_time,occupied_end_time
			) VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$6,$7,
			          'Dynamic service',60,$8,1,$9,$10,$11,0,0,$10,$11)
		`, appointmentSegmentIDs[i], fixture.salonID, ids.appointmentID, segment.serviceID, segment.staffID,
			segment.serviceID.String(), segment.staffID.String(), i+1, guest, segment.start, segment.start.Add(time.Hour)); err != nil {
			return ids, err
		}
		resourceIDs, err := insertV50AppointmentResources(ctx, tx, fixture, appointmentSegmentIDs[i], segment.serviceID, segment.start)
		if err != nil {
			return ids, err
		}
		ids.appointmentResourceIDs = append(ids.appointmentResourceIDs, resourceIDs...)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_execution_events (
			id,salon_id,booking_attempt_id,appointment_id,event_type,
			scheduling_authority_version,authority_config_version,authority_appointment_version
		) VALUES (gen_random_uuid(),$1,$2,$3,'appointment_confirmed',1,$4,1)
	`, fixture.salonID, ids.attemptID, ids.appointmentID, fixture.configVersion); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE availability_quotes SET consumed_at=now(), consumed_by_attempt_id=$1
		WHERE id=$2 AND salon_id=$3
	`, ids.attemptID, ids.quoteID, fixture.salonID); err != nil {
		return ids, err
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		return ids, err
	}
	return ids, nil
}

func cancelV50Book(ctx context.Context, tx *sql.Tx, fixture v50Fixture, book v50BookIDs, start time.Time) error {
	if len(book.attemptSegmentIDs) != 1 || len(book.appointmentResourceIDs) == 0 {
		return fmt.Errorf("cancel fixture requires one booked segment with resources")
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return err
	}
	cancelAttemptID := uuid.New()
	cancelSegmentID := uuid.New()
	operationKey := "v50-cancel-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			id,salon_id,source,status,pos_provider,target_pos_booking_version,operation_key,
			request_fingerprint,operation_type,target_appointment_id,
			target_authority_appointment_version,provider_outcome,retry_policy,
			reconciliation_status,customer_name,customer_phone,service_id,staff_id,
			staff_selection_mode,requested_start_time,requested_end_time,scheduling_authority,
			authority_appointment_id,authority_appointment_version,authority_idempotency_key,
			scheduling_authority_version,authority_config_version,party_size
		) VALUES ($1,$2,'test','cancelled',NULL,NULL,$3,$4,'cancel',$5,1,
		          'not_started','none','not_required','Customer','5553000',$6,$7,
		          'specific',$8,$9,'manleai_calendar',$10,2,$3,1,$11,1)
	`, cancelAttemptID, fixture.salonID, operationKey, strings.Repeat("c", 64), book.appointmentID,
		fixture.service1ID, fixture.staff1ID, start, start.Add(time.Hour), book.appointmentID.String(), fixture.configVersion); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			id,salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,pos_service_id,
			scheduling_authority,authority_service_id,authority_staff_id,name,duration_minutes,
			sort_order,scheduled_start_time,scheduled_end_time,buffer_before_minutes,
			buffer_after_minutes,occupied_start_time,occupied_end_time
		) VALUES ($1,$2,$3,$4,$5,'specific',NULL,'manleai_calendar',$6,$7,
		          'Dynamic service',60,1,$8,$9,0,0,$8,$9)
	`, cancelSegmentID, fixture.salonID, cancelAttemptID, fixture.service1ID, fixture.staff1ID,
		fixture.service1ID.String(), fixture.staff1ID.String(), start, start.Add(time.Hour)); err != nil {
		return err
	}
	if err := insertV50AttemptResources(ctx, tx, fixture, cancelSegmentID, fixture.service1ID); err != nil {
		return err
	}
	releasedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_appointment_resource_allocations
		SET released_at=$1
		WHERE salon_id=$2 AND appointment_service_id IN (
			SELECT id FROM appointment_services WHERE appointment_id=$3 AND released_at IS NULL
		)
	`, releasedAt, fixture.salonID, book.appointmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointment_services SET released_at=$1
		WHERE salon_id=$2 AND appointment_id=$3 AND released_at IS NULL
	`, releasedAt, fixture.salonID, book.appointmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET booking_attempt_id=$1,status='cancelled',authority_appointment_version=2,updated_at=now()
		WHERE salon_id=$2 AND id=$3
	`, cancelAttemptID, fixture.salonID, book.appointmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_execution_events (
			id,salon_id,booking_attempt_id,appointment_id,event_type,
			scheduling_authority_version,authority_config_version,authority_appointment_version
		) VALUES (gen_random_uuid(),$1,$2,$3,'appointment_cancelled',1,$4,2)
	`, fixture.salonID, cancelAttemptID, book.appointmentID, fixture.configVersion); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	return err
}

func insertV50QuoteResources(ctx context.Context, tx *sql.Tx, fixture v50Fixture, segmentID, serviceID uuid.UUID) error {
	requirements, err := loadV50ResourceRequirements(ctx, tx, fixture.salonID, serviceID)
	if err != nil {
		return err
	}
	for _, requirement := range requirements {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO availability_quote_slot_resource_allocations (
				id,salon_id,quote_slot_segment_id,resource_pool_id,units_allocated
			) VALUES (gen_random_uuid(),$1,$2,$3,$4)
		`, fixture.salonID, segmentID, requirement.poolID, requirement.units); err != nil {
			return err
		}
	}
	return nil
}

func insertV50AttemptResources(ctx context.Context, tx *sql.Tx, fixture v50Fixture, segmentID, serviceID uuid.UUID) error {
	requirements, err := loadV50ResourceRequirements(ctx, tx, fixture.salonID, serviceID)
	if err != nil {
		return err
	}
	for _, requirement := range requirements {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segment_resource_allocations (
				id,salon_id,booking_attempt_segment_id,resource_pool_id,units_allocated
			) VALUES (gen_random_uuid(),$1,$2,$3,$4)
		`, fixture.salonID, segmentID, requirement.poolID, requirement.units); err != nil {
			return err
		}
	}
	return nil
}

func insertV50AppointmentResources(ctx context.Context, tx *sql.Tx, fixture v50Fixture, segmentID, serviceID uuid.UUID, start time.Time) ([]uuid.UUID, error) {
	requirements, err := loadV50ResourceRequirements(ctx, tx, fixture.salonID, serviceID)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for _, requirement := range requirements {
		allocationID := uuid.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manleai_calendar_appointment_resource_allocations (
				id,salon_id,appointment_service_id,resource_pool_id,units_allocated,
				plan_version,occupied_start_time,occupied_end_time
			) VALUES ($1,$2,$3,$4,$5,1,$6,$7)
		`, allocationID, fixture.salonID, segmentID, requirement.poolID, requirement.units, start, start.Add(time.Hour)); err != nil {
			return nil, err
		}
		ids = append(ids, allocationID)
	}
	return ids, nil
}

type v50ResourceRequirement struct {
	poolID uuid.UUID
	units  int
}

func loadV50ResourceRequirements(ctx context.Context, tx *sql.Tx, salonID, serviceID uuid.UUID) ([]v50ResourceRequirement, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT resource_pool_id,units_required
		FROM manleai_calendar_service_resources
		WHERE salon_id=$1 AND service_id=$2
		ORDER BY resource_pool_id
	`, salonID, serviceID)
	if err != nil {
		return nil, err
	}
	var requirements []v50ResourceRequirement
	for rows.Next() {
		var requirement v50ResourceRequirement
		if err := rows.Scan(&requirement.poolID, &requirement.units); err != nil {
			rows.Close()
			return nil, err
		}
		requirements = append(requirements, requirement)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return requirements, nil
}

func withV50Savepoint(t *testing.T, ctx context.Context, tx *sql.Tx, action func() error, code, constraint string) {
	t.Helper()
	savepoint := pq.QuoteIdentifier("v50_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create V50 savepoint: %v", err)
	}
	err := action()
	assertV50PostgresError(t, err, code, constraint)
	if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); rollbackErr != nil {
		t.Fatalf("roll back V50 savepoint: %v", rollbackErr)
	}
	if _, err := tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatalf("restore V50 immediate constraints: %v", err)
	}
}

func assertV50PostgresError(t *testing.T, err error, code, constraint string) {
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

func assertV50OperationAbsent(t *testing.T, ctx context.Context, tx *sql.Tx, ids v50BookIDs) {
	t.Helper()
	var attempts, appointments, quotesConsumed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM booking_attempts WHERE id=$1`, ids.attemptID).Scan(&attempts); err != nil {
		t.Fatalf("count rolled-back V50 attempts: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM appointments WHERE id=$1`, ids.appointmentID).Scan(&appointments); err != nil {
		t.Fatalf("count rolled-back V50 appointments: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM availability_quotes WHERE id=$1 AND consumed_at IS NOT NULL`, ids.quoteID).Scan(&quotesConsumed); err != nil {
		t.Fatalf("count rolled-back V50 quote consumption: %v", err)
	}
	if attempts != 0 || appointments != 0 || quotesConsumed != 0 {
		t.Fatalf("V50 rollback left attempts=%d appointments=%d consumed_quotes=%d", attempts, appointments, quotesConsumed)
	}
}
