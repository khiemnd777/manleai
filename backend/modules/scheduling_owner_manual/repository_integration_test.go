package scheduling_owner_manual

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
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type ownerManualRepositoryFixture struct {
	ownerID      string
	salonID      string
	serviceID    string
	staffID      string
	otherOwnerID string
	otherSalonID string
	otherService string
}

func TestRepositorySchedulingTargetReadinessUsesOwnerScopedCanonicalServices(t *testing.T) {
	db := openOwnerManualTestDatabase(t)
	fixture := seedOwnerManualRepositoryFixture(t, db)
	ctx := context.Background()
	repository := NewRepository(db)

	version, count, err := repository.SchedulingTargetReadinessFacts(ctx, fixture.salonID, fixture.ownerID)
	if err != nil || version != 1 || count != 1 {
		t.Fatalf("readiness facts version=%d count=%d err=%v", version, count, err)
	}
	if _, _, err := repository.SchedulingTargetReadinessFacts(ctx, fixture.salonID, fixture.otherOwnerID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-owner readiness error = %v, want not found", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE services SET archived_at=now() WHERE id=$1`, fixture.serviceID); err != nil {
		t.Fatalf("archive service: %v", err)
	}
	version, count, err = repository.SchedulingTargetReadinessFacts(ctx, fixture.salonID, fixture.ownerID)
	if err != nil || version != 1 || count != 0 {
		t.Fatalf("blocked readiness facts version=%d count=%d err=%v", version, count, err)
	}
}

func TestRepositoryOwnerManualLifecycleReplayTenantAndSideEffectSafety(t *testing.T) {
	db := openOwnerManualTestDatabase(t)
	fixture := seedOwnerManualRepositoryFixture(t, db)
	ctx := context.Background()
	repository := NewRepository(db)
	service := NewService(repository)
	start := time.Date(2026, time.August, 12, 20, 0, 0, 0, time.UTC)
	request := scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "owner-manual-lifecycle-" + uuid.NewString(),
		Source:             booking.SourceAIVoiceCall,
		CustomerName:       "Lan Nguyen",
		CustomerPhone:      "+13125550111",
		CustomerEmail:      "lan@example.com",
		RequestedStartTime: start,
		RequestedTimezone:  "America/Chicago",
		Notes:              "Prefers a quiet station.",
		Segments: []scheduling.ActionSegment{{
			ServiceID:          fixture.serviceID,
			StaffID:            fixture.staffID,
			StaffSelectionMode: booking.StaffSelectionSpecific,
			GuestReference:     "guest-1",
			Quantity:           1,
		}},
	}

	created, replayed, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, request)
	if err != nil || replayed {
		t.Fatalf("create request = %#v replayed=%t err=%v", created, replayed, err)
	}
	if created.ID == "" || created.Status != scheduling.SchedulingRequestStatusPending || created.Version != 1 || created.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual {
		t.Fatalf("created aggregate = %#v", created)
	}
	if created.RequestedStartTime == nil || !created.RequestedStartTime.Equal(start) || created.RequestedEndTime == nil || !created.RequestedEndTime.Equal(start.Add(45*time.Minute)) {
		t.Fatalf("request times = start:%v end:%v", created.RequestedStartTime, created.RequestedEndTime)
	}
	if len(created.Segments) != 1 || created.Segments[0].ServiceName != "Integration Manicure" || created.Segments[0].StaffName != "Integration Staff" || created.Segments[0].DurationMinutes != 45 {
		t.Fatalf("segment snapshot = %#v", created.Segments)
	}
	if len(created.Events) != 1 || created.Events[0].ActorUserID != "" || created.Events[0].EventType != scheduling.SchedulingRequestEventCreated {
		t.Fatalf("voice creation event = %#v, want NULL actor system event", created.Events)
	}
	assertOperationAggregateCounts(t, db, fixture.salonID, request.OperationKey, 1, 1, 1, 1)
	assertNoBookingOrProviderSideEffects(t, db, fixture.salonID)

	for _, update := range []struct {
		query string
		id    string
	}{
		{query: `UPDATE services SET active = false WHERE id = $1`, id: fixture.serviceID},
		{query: `UPDATE staff SET active = false WHERE id = $1`, id: fixture.staffID},
		{query: `UPDATE salons SET timezone = 'America/New_York' WHERE id = $1`, id: fixture.salonID},
	} {
		if _, err := db.ExecContext(ctx, update.query, update.id); err != nil {
			t.Fatalf("change current catalog/timezone after create: %v", err)
		}
	}
	replayedRequest, replayed, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, request)
	if err != nil || !replayed || replayedRequest.ID != created.ID {
		t.Fatalf("durable replay after source changes = %#v replayed=%t err=%v", replayedRequest, replayed, err)
	}
	assertOperationAggregateCounts(t, db, fixture.salonID, request.OperationKey, 1, 1, 1, 1)

	changed := request
	changed.Notes = "Changed payload must conflict."
	if result, replayed, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, changed); !errors.Is(err, booking.ErrOperationConflict) || replayed || result != nil {
		t.Fatalf("changed replay = %#v replayed=%t err=%v", result, replayed, err)
	}
	assertOperationAggregateCounts(t, db, fixture.salonID, request.OperationKey, 1, 1, 1, 1)

	if _, err := repository.Get(ctx, fixture.salonID, fixture.otherOwnerID, created.ID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-owner get error = %v", err)
	}
	if _, err := repository.Get(ctx, fixture.otherSalonID, fixture.otherOwnerID, created.ID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-salon get error = %v", err)
	}
	if _, _, err := service.Transition(ctx, fixture.salonID, fixture.otherOwnerID, created.ID, scheduling.TransitionSchedulingRequest{ActionKey: "cross-owner", ExpectedVersion: 1, Status: scheduling.SchedulingRequestStatusContacted}); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-owner transition error = %v", err)
	}

	contact := scheduling.TransitionSchedulingRequest{
		ActionKey:       "contact-" + uuid.NewString(),
		ExpectedVersion: 1,
		Status:          scheduling.SchedulingRequestStatusContacted,
		Note:            "Owner called the customer.",
	}
	contacted, replayed, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, contact)
	if err != nil || replayed || contacted.Version != 2 || contacted.Status != scheduling.SchedulingRequestStatusContacted || contacted.ContactedAt == nil {
		t.Fatalf("contact transition = %#v replayed=%t err=%v", contacted, replayed, err)
	}
	contactReplay, replayed, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, contact)
	if err != nil || !replayed || contactReplay.Version != 2 || len(contactReplay.Events) != 2 {
		t.Fatalf("contact replay = %#v replayed=%t err=%v", contactReplay, replayed, err)
	}
	contactConflict := contact
	contactConflict.Note = "Same action key with different content."
	if _, _, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, contactConflict); !errors.Is(err, booking.ErrOperationConflict) {
		t.Fatalf("changed transition replay error = %v", err)
	}
	if _, _, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, scheduling.TransitionSchedulingRequest{ActionKey: "stale-" + uuid.NewString(), ExpectedVersion: 1, Status: scheduling.SchedulingRequestStatusResolved, ResolutionReason: "Handled outside the system."}); !errors.Is(err, scheduling.ErrSchedulingRequestVersion) {
		t.Fatalf("stale transition error = %v", err)
	}

	resolve := scheduling.TransitionSchedulingRequest{
		ActionKey:        "resolve-" + uuid.NewString(),
		ExpectedVersion:  2,
		Status:           scheduling.SchedulingRequestStatusResolved,
		ResolutionReason: "Owner completed the request outside automated scheduling.",
	}
	resolved, replayed, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, resolve)
	if err != nil || replayed || resolved.Version != 3 || resolved.ResolvedAt == nil || resolved.ResolutionReason == "" {
		t.Fatalf("resolve transition = %#v replayed=%t err=%v", resolved, replayed, err)
	}
	if _, _, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, scheduling.TransitionSchedulingRequest{ActionKey: "terminal-" + uuid.NewString(), ExpectedVersion: 3, Status: scheduling.SchedulingRequestStatusDismissed, ResolutionReason: "Invalid second closure."}); !errors.Is(err, scheduling.ErrSchedulingRequestTerminal) {
		t.Fatalf("terminal transition error = %v", err)
	}
	resolveReplay, replayed, err := service.Transition(ctx, fixture.salonID, fixture.ownerID, created.ID, resolve)
	if err != nil || !replayed || resolveReplay.Version != 3 || len(resolveReplay.Events) != 3 {
		t.Fatalf("terminal exact replay = %#v replayed=%t err=%v", resolveReplay, replayed, err)
	}
	assertNoBookingOrProviderSideEffects(t, db, fixture.salonID)
}

func TestRepositoryPendingExternalRequestPreservesTargetAndReplaysAfterModeChange(t *testing.T) {
	db := openOwnerManualTestDatabase(t)
	fixture := seedOwnerManualRepositoryFixture(t, db)
	ctx := context.Background()
	service := NewService(NewRepository(db))
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'external_provider', booking_mode = 'pending_approval'
		WHERE salon_id = $1
	`, fixture.salonID); err != nil {
		t.Fatalf("select pending external policy: %v", err)
	}
	request := scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "pending-external-" + uuid.NewString(),
		Source:             booking.SourceAIVoiceCall,
		CustomerName:       "Thao Nguyen",
		CustomerPhone:      "+13125550333",
		RequestedStartTime: time.Date(2026, time.September, 3, 19, 30, 0, 0, time.UTC),
		RequestedTimezone:  "America/Chicago",
		TargetAuthority:    booking.SchedulingAuthorityExternalProvider,
		Segments: []scheduling.ActionSegment{{
			ServiceID: fixture.serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1,
		}},
	}

	created, replayed, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, request)
	if err != nil || replayed || created.TargetAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("pending external create = %#v replayed=%t err=%v", created, replayed, err)
	}
	var eventTarget string
	var notificationTarget string
	if err := db.QueryRowContext(ctx, `
		SELECT event.payload->>'target_scheduling_authority', notification.payload->>'target_scheduling_authority'
		FROM scheduling_request_events event
		JOIN owner_notifications notification ON notification.scheduling_request_id = event.scheduling_request_id
		WHERE event.scheduling_request_id = $1 AND event.event_type = $2
	`, created.ID, scheduling.SchedulingRequestEventCreated).Scan(&eventTarget, &notificationTarget); err != nil {
		t.Fatalf("load target authority payload evidence: %v", err)
	}
	if eventTarget != booking.SchedulingAuthorityExternalProvider || notificationTarget != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("target authority event/notification = %q/%q", eventTarget, notificationTarget)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET booking_mode = 'confirmed_booking'
		WHERE salon_id = $1
	`, fixture.salonID); err != nil {
		t.Fatalf("change current booking mode: %v", err)
	}
	replay, replayed, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, request)
	if err != nil || !replayed || replay.ID != created.ID || replay.TargetAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("durable replay after mode change = %#v replayed=%t err=%v", replay, replayed, err)
	}
	changed := request
	changed.CustomerName = "Different payload"
	if result, replayed, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, changed); !errors.Is(err, booking.ErrOperationConflict) || replayed || result != nil {
		t.Fatalf("changed replay after mode change = %#v replayed=%t err=%v", result, replayed, err)
	}
	assertOperationAggregateCounts(t, db, fixture.salonID, request.OperationKey, 1, 1, 1, 1)
	assertNoBookingOrProviderSideEffects(t, db, fixture.salonID)
}

func TestRepositoryCreateRollbackConcurrencyAndTransitionCAS(t *testing.T) {
	db := openOwnerManualTestDatabase(t)
	fixture := seedOwnerManualRepositoryFixture(t, db)
	ctx := context.Background()
	service := NewService(NewRepository(db))
	base := scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "owner-manual-concurrent-" + uuid.NewString(),
		Source:             booking.SourceOwnerDashboard,
		CustomerName:       "Minh Pham",
		CustomerPhone:      "+13125550222",
		RequestedStartTime: time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC),
		RequestedTimezone:  "America/Chicago",
		Segments: []scheduling.ActionSegment{{
			ServiceID:          fixture.serviceID,
			StaffSelectionMode: booking.StaffSelectionAnyone,
			Quantity:           1,
		}},
	}

	invalid := base
	invalid.OperationKey = "owner-manual-invalid-" + uuid.NewString()
	invalid.Segments = []scheduling.ActionSegment{{ServiceID: fixture.otherService, StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1}}
	if _, _, err := service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, invalid); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-salon catalog error = %v", err)
	}
	assertOperationAggregateCounts(t, db, fixture.salonID, invalid.OperationKey, 0, 0, 0, 0)

	results := make([]*scheduling.SchedulingRequest, 2)
	replays := make([]bool, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], replays[index], errs[index] = service.CreateRequest(ctx, fixture.salonID, fixture.ownerID, base)
		}(i)
	}
	close(start)
	wait.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent create %d error = %v", i, err)
		}
	}
	if results[0].ID == "" || results[0].ID != results[1].ID || replays[0] == replays[1] {
		t.Fatalf("concurrent create results=%#v replays=%#v", results, replays)
	}
	assertOperationAggregateCounts(t, db, fixture.salonID, base.OperationKey, 1, 1, 1, 1)
	var createdActorID string
	if err := db.QueryRowContext(ctx, `
		SELECT event.actor_user_id::text
		FROM scheduling_request_events event
		WHERE event.scheduling_request_id = $1 AND event.event_type = $2
	`, results[0].ID, scheduling.SchedulingRequestEventCreated).Scan(&createdActorID); err != nil || createdActorID != fixture.ownerID {
		t.Fatalf("owner-dashboard creation actor = %q/%v, want owner %q", createdActorID, err, fixture.ownerID)
	}

	transitions := []scheduling.TransitionSchedulingRequest{
		{ActionKey: "concurrent-contact-" + uuid.NewString(), ExpectedVersion: 1, Status: scheduling.SchedulingRequestStatusContacted},
		{ActionKey: "concurrent-dismiss-" + uuid.NewString(), ExpectedVersion: 1, Status: scheduling.SchedulingRequestStatusDismissed, ResolutionReason: "Owner declined the request."},
	}
	transitionErrs := make([]error, len(transitions))
	start = make(chan struct{})
	for i := range transitions {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, _, transitionErrs[index] = service.Transition(ctx, fixture.salonID, fixture.ownerID, results[0].ID, transitions[index])
		}(i)
	}
	close(start)
	wait.Wait()
	var successCount int
	var staleCount int
	for _, err := range transitionErrs {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, scheduling.ErrSchedulingRequestVersion):
			staleCount++
		default:
			t.Fatalf("unexpected concurrent transition error: %v", err)
		}
	}
	if successCount != 1 || staleCount != 1 {
		t.Fatalf("concurrent transition outcomes = success:%d stale:%d errors:%#v", successCount, staleCount, transitionErrs)
	}
	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduling_request_events WHERE scheduling_request_id = $1`, results[0].ID).Scan(&eventCount); err != nil || eventCount != 2 {
		t.Fatalf("event count after CAS = %d/%v, want 2", eventCount, err)
	}
	assertNoBookingOrProviderSideEffects(t, db, fixture.salonID)
}

func openOwnerManualTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return db
}

func seedOwnerManualRepositoryFixture(t *testing.T, db *sql.DB) ownerManualRepositoryFixture {
	t.Helper()
	ctx := context.Background()
	fixture := ownerManualRepositoryFixture{}
	insertOwner := func(label string) string {
		var id string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash, full_name)
			VALUES ($1, 'integration-test', $2)
			RETURNING id::text
		`, label+"-"+uuid.NewString()+"@example.com", label).Scan(&id); err != nil {
			t.Fatalf("insert %s owner: %v", label, err)
		}
		return id
	}
	insertSalon := func(ownerID string, label string) string {
		var id string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO salons (name, phone, timezone, owner_user_id)
			VALUES ($1, '+13125550999', 'America/Chicago', $2)
			RETURNING id::text
		`, label, ownerID).Scan(&id); err != nil {
			t.Fatalf("insert %s salon: %v", label, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings (salon_id,scheduling_authority,booking_mode) VALUES ($1,'owner_manual','pending_approval')`, id); err != nil {
			t.Fatalf("insert %s owner-manual settings: %v", label, err)
		}
		return id
	}
	insertService := func(salonID string, label string) string {
		var id string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO services (salon_id, pos_provider, pos_service_id, pos_service_version, name, duration_minutes, active, ai_bookable)
			VALUES ($1, 'square', $2, 1, $3, 45, true, true)
			RETURNING id::text
		`, salonID, "owner-manual-service-"+uuid.NewString(), label).Scan(&id); err != nil {
			t.Fatalf("insert %s service: %v", label, err)
		}
		return id
	}
	fixture.ownerID = insertOwner("Owner Manual")
	fixture.salonID = insertSalon(fixture.ownerID, "Owner Manual Salon")
	fixture.serviceID = insertService(fixture.salonID, "Integration Manicure")
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, active, ai_bookable)
		VALUES ($1, 'square', $2, 'Integration Staff', true, true)
		RETURNING id::text
	`, fixture.salonID, "owner-manual-staff-"+uuid.NewString()).Scan(&fixture.staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	fixture.otherOwnerID = insertOwner("Other Owner")
	fixture.otherSalonID = insertSalon(fixture.otherOwnerID, "Other Owner Salon")
	fixture.otherService = insertService(fixture.otherSalonID, "Other Salon Service")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1, $2)`, fixture.salonID, fixture.otherSalonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, fixture.ownerID, fixture.otherOwnerID)
	})
	return fixture
}

func assertOperationAggregateCounts(t *testing.T, db *sql.DB, salonID string, operationKey string, requests int, segments int, events int, notifications int) {
	t.Helper()
	queries := []struct {
		name string
		sql  string
		want int
	}{
		{name: "requests", sql: `SELECT COUNT(*) FROM scheduling_requests WHERE salon_id = $1 AND operation_key = $2`, want: requests},
		{name: "segments", sql: `SELECT COUNT(*) FROM scheduling_request_segments segment JOIN scheduling_requests request ON request.id = segment.scheduling_request_id WHERE request.salon_id = $1 AND request.operation_key = $2`, want: segments},
		{name: "events", sql: `SELECT COUNT(*) FROM scheduling_request_events event JOIN scheduling_requests request ON request.id = event.scheduling_request_id WHERE request.salon_id = $1 AND request.operation_key = $2`, want: events},
		{name: "notifications", sql: `SELECT COUNT(*) FROM owner_notifications notification JOIN scheduling_requests request ON request.id = notification.scheduling_request_id WHERE request.salon_id = $1 AND request.operation_key = $2`, want: notifications},
	}
	for _, query := range queries {
		var got int
		if err := db.QueryRow(query.sql, salonID, operationKey).Scan(&got); err != nil || got != query.want {
			t.Fatalf("%s count = %d/%v, want %d", query.name, got, err, query.want)
		}
	}
	if notifications == 1 {
		var notificationType string
		var dedupeKey string
		if err := db.QueryRow(`
			SELECT notification.type, notification.dedupe_key
			FROM owner_notifications notification
			JOIN scheduling_requests request ON request.id = notification.scheduling_request_id
			WHERE request.salon_id = $1 AND request.operation_key = $2
		`, salonID, operationKey).Scan(&notificationType, &dedupeKey); err != nil {
			t.Fatalf("load notification contract: %v", err)
		}
		if notificationType != ownerManualNotificationType || len(dedupeKey) <= len("owner-manual-request-pending:") || dedupeKey[:len("owner-manual-request-pending:")] != "owner-manual-request-pending:" {
			t.Fatalf("notification contract = type:%q dedupe:%q", notificationType, dedupeKey)
		}
	}
}

func assertNoBookingOrProviderSideEffects(t *testing.T, db *sql.DB, salonID string) {
	t.Helper()
	queries := []struct {
		name string
		sql  string
	}{
		{name: "booking attempts", sql: `SELECT COUNT(*) FROM booking_attempts WHERE salon_id = $1`},
		{name: "appointments", sql: `SELECT COUNT(*) FROM appointments WHERE salon_id = $1`},
		{name: "POS errors", sql: `SELECT COUNT(*) FROM pos_errors WHERE salon_id = $1`},
		{name: "reconciliation tasks", sql: `SELECT COUNT(*) FROM booking_reconciliation_tasks WHERE salon_id = $1`},
	}
	for _, query := range queries {
		var count int
		if err := db.QueryRow(query.sql, salonID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count = %d/%v, want zero", query.name, count, err)
		}
	}
}
