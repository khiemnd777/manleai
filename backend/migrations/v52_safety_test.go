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

func readV52(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V52__scheduling_authority_switch_runs.sql")
	if err != nil {
		t.Fatalf("read V52 migration: %v", err)
	}
	return string(source)
}

func TestV52DefinesAuthoritySwitchPreviewEvidenceWithoutOperationalWrites(t *testing.T) {
	source := readV52(t)
	for _, fragment := range []string{
		"CREATE TABLE scheduling_authority_switch_runs",
		"CREATE TABLE scheduling_authority_switch_events",
		"expected_source_authority_version BIGINT NOT NULL",
		"UNIQUE (salon_id, operation_key)",
		"readiness_snapshot JSONB NOT NULL",
		"blockers JSONB NOT NULL",
		"status IN ('preview_ready', 'preview_blocked', 'committed', 'failed')",
		"rollback_of_switch_run_id UUID",
		"scheduling_authority_switch_runs_rollback_tenant_fk",
		"scheduling_authority_switch_runs_immutable_core_guard",
		"scheduling_authority_switch_runs_immutable_guard",
		"scheduling_authority_switch_runs_status_transition_guard",
		"scheduling_authority_switch_runs_rollback_state_guard",
		"scheduling_authority_switch_events_actor_owner_guard",
		"scheduling_authority_switch_events_immutable_guard",
		"event_type IN ('preview', 'commit', 'fail')",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V52 is missing switch-run safety contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"access_token",
		"refresh_token",
		"client_secret",
		"encrypted_secret",
		"UPDATE salon_settings",
		"UPDATE appointments",
		"UPDATE booking_attempts",
		"UPDATE availability_quotes",
	} {
		if strings.Contains(strings.ToLower(source), forbidden) {
			t.Fatalf("V52 must retain sanitized preview evidence without sensitive or operational writes; found %q", forbidden)
		}
	}
}

func TestV52AppliesAfterV51(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v52_upgrade_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 51)
	source := readV52(t)
	if _, err := tx.ExecContext(ctx, source); err != nil {
		t.Fatalf("apply V52 after V51: %v", err)
	}
}

func TestV52SwitchRunIntegrityAndPreviewIsolation(t *testing.T) {
	if os.Getenv("MIGRATION_TEST_DATABASE_URL") == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, ctx, tx := beginV49PostgresTest(t, "v52_switch_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 52)

	fixture := seedV52Fixture(t, ctx, tx)
	runID := insertV52Run(t, ctx, tx, fixture, "preview-one", strings.Repeat("a", 64), "external_provider", "manleai_calendar", "preview_ready", uuid.Nil)
	insertV52Event(t, ctx, tx, fixture.salonID, runID, fixture.ownerID, "preview-one-event", "preview")

	var authority string
	var authorityVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT scheduling_authority, scheduling_authority_version
		FROM salon_settings WHERE salon_id=$1
	`, fixture.salonID).Scan(&authority, &authorityVersion); err != nil {
		t.Fatalf("read source authority after preview: %v", err)
	}
	if authority != "external_provider" || authorityVersion != 1 {
		t.Fatalf("preview changed current authority/version to %q/%d", authority, authorityVersion)
	}
	for _, table := range []string{"appointments", "booking_attempts", "availability_quotes"} {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE salon_id=$1", fixture.salonID).Scan(&count); err != nil {
			t.Fatalf("count %s after preview: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("preview wrote operational %s rows=%d", table, count)
		}
	}

	expectV49PostgresError(t, ctx, tx, "23505", "scheduling_authority_switch_runs_salon_operation_key", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status
		) VALUES ($1,$2,'external_provider','manleai_calendar',1,$3,$4,'{}'::jsonb,'[]'::jsonb,$5,'preview_ready')
	`, uuid.New(), fixture.salonID, "preview-one", strings.Repeat("a", 64), fixture.ownerID)
	expectV49PostgresError(t, ctx, tx, "23505", "scheduling_authority_switch_runs_salon_operation_key", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status
		) VALUES ($1,$2,'external_provider','manleai_calendar',1,$3,$4,'{}'::jsonb,'[]'::jsonb,$5,'preview_ready')
	`, uuid.New(), fixture.salonID, "preview-one", strings.Repeat("b", 64), fixture.ownerID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_status_timestamps_check", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status
		) VALUES ($1,$2,'external_provider','manleai_calendar',1,'ready-with-blocker',$3,'{}'::jsonb,'[{"code":"not-ready"}]'::jsonb,$4,'preview_ready')
	`, uuid.New(), fixture.salonID, strings.Repeat("c", 64), fixture.ownerID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_operation_key_nonempty_check", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status
		) VALUES ($1,$2,'external_provider','manleai_calendar',1,$3,$4,'{}'::jsonb,'[]'::jsonb,$5,'preview_ready')
	`, uuid.New(), fixture.salonID, strings.Repeat("x", 257), strings.Repeat("d", 64), fixture.ownerID)

	var runCount int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM scheduling_authority_switch_runs WHERE salon_id=$1 AND operation_key='preview-one'`, fixture.salonID).Scan(&runCount); err != nil {
		t.Fatalf("count operation-key replay rows: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("operation-key replay rows=%d, want 1", runCount)
	}

	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_actor_owner_guard", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status
		) VALUES ($1,$2,'external_provider','owner_manual',1,'wrong-owner',$3,'{}'::jsonb,'[]'::jsonb,$4,'preview_ready')
	`, uuid.New(), fixture.salonID, strings.Repeat("e", 64), fixture.otherOwnerID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_events_actor_owner_guard", `
		INSERT INTO scheduling_authority_switch_events (
			id,salon_id,switch_run_id,action_key,action_fingerprint,event_type,actor_user_id,payload
		) VALUES ($1,$2,$3,'wrong-event-actor',$4,'preview',$5,'{}'::jsonb)
	`, uuid.New(), fixture.salonID, runID, strings.Repeat("f", 64), fixture.otherOwnerID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_events_state_guard", `
		INSERT INTO scheduling_authority_switch_events (
			id,salon_id,switch_run_id,action_key,action_fingerprint,event_type,actor_user_id,payload
		) VALUES ($1,$2,$3,'cross-tenant-event',$4,'preview',$5,'{}'::jsonb)
	`, uuid.New(), fixture.otherSalonID, runID, strings.Repeat("0", 64), fixture.otherOwnerID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_events_action_key_nonempty_check", `
		INSERT INTO scheduling_authority_switch_events (
			id,salon_id,switch_run_id,action_key,action_fingerprint,event_type,actor_user_id,payload
		) VALUES ($1,$2,$3,$4,$5,'preview',$6,'{}'::jsonb)
	`, uuid.New(), fixture.salonID, runID, strings.Repeat("x", 257), strings.Repeat("1", 64), fixture.ownerID)

	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_immutable_core_guard", `
		UPDATE scheduling_authority_switch_runs SET payload_fingerprint=$1 WHERE id=$2
	`, strings.Repeat("f", 64), runID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_immutable_guard", `
		DELETE FROM scheduling_authority_switch_runs WHERE id=$1
	`, runID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_events_immutable_guard", `
		UPDATE scheduling_authority_switch_events SET payload='{"rewritten":true}'::jsonb WHERE switch_run_id=$1
	`, runID)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_events_immutable_guard", `
		DELETE FROM scheduling_authority_switch_events WHERE switch_run_id=$1
	`, runID)

	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_status_timestamps_check", `
		UPDATE scheduling_authority_switch_runs SET status='committed' WHERE id=$1
	`, runID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE scheduling_authority_switch_runs
		SET status='committed', committed_at=now(), updated_at=now()
		WHERE id=$1
	`, runID); err != nil {
		t.Fatalf("commit preview-ready run: %v", err)
	}
	insertV52Event(t, ctx, tx, fixture.salonID, runID, fixture.ownerID, "commit-one-event", "commit")
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_status_transition_guard", `
		UPDATE scheduling_authority_switch_runs SET status='failed', failed_at=now() WHERE id=$1
	`, runID)

	blockedRunID := insertV52Run(t, ctx, tx, fixture, "blocked-switch", strings.Repeat("1", 64), "owner_manual", "external_provider", "preview_blocked", uuid.Nil)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_rollback_state_guard", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status,rollback_of_switch_run_id
		) VALUES ($1,$2,'external_provider','owner_manual',1,'blocked-rollback',$3,'{}'::jsonb,'[]'::jsonb,$4,'preview_ready',$5)
	`, uuid.New(), fixture.salonID, strings.Repeat("2", 64), fixture.ownerID, blockedRunID)
	previousRunID := insertV52Run(t, ctx, tx, fixture, "previous-switch", strings.Repeat("3", 64), "owner_manual", "external_provider", "preview_ready", uuid.Nil)
	if _, err := tx.ExecContext(ctx, `UPDATE scheduling_authority_switch_runs SET status='committed', committed_at=now() WHERE id=$1`, previousRunID); err != nil {
		t.Fatalf("commit prior switch for rollback evidence: %v", err)
	}
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_rollback_state_guard", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status,rollback_of_switch_run_id
		) VALUES ($1,$2,'owner_manual','external_provider',1,'wrong-direction-rollback',$3,'{}'::jsonb,'[]'::jsonb,$4,'preview_ready',$5)
	`, uuid.New(), fixture.salonID, strings.Repeat("4", 64), fixture.ownerID, previousRunID)
	rollbackRunID := insertV52Run(t, ctx, tx, fixture, "rollback-switch", strings.Repeat("5", 64), "external_provider", "owner_manual", "preview_ready", previousRunID)
	if rollbackRunID == uuid.Nil {
		t.Fatal("valid same-salon rollback run was not persisted")
	}
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_rollback_not_self_check", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status,rollback_of_switch_run_id
		) VALUES ($1,$2,'external_provider','owner_manual',1,'self-rollback',$3,'{}'::jsonb,'[]'::jsonb,$4,'preview_ready',$1)
	`, uuid.New(), fixture.salonID, strings.Repeat("6", 64), fixture.ownerID)
	expectV49PostgresError(t, ctx, tx, "23503", "scheduling_authority_switch_runs_rollback_tenant_fk", `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status,rollback_of_switch_run_id
		) VALUES ($1,$2,'external_provider','owner_manual',1,'cross-salon-rollback',$3,'{}'::jsonb,'[]'::jsonb,$4,'preview_ready',$5)
	`, uuid.New(), fixture.salonID, strings.Repeat("7", 64), fixture.ownerID, fixture.otherRunID)
}

type v52Fixture struct {
	salonID      uuid.UUID
	otherSalonID uuid.UUID
	ownerID      uuid.UUID
	otherOwnerID uuid.UUID
	otherRunID   uuid.UUID
}

func seedV52Fixture(t *testing.T, ctx context.Context, tx *sql.Tx) v52Fixture {
	t.Helper()
	fixture := v52Fixture{
		salonID: uuid.New(), otherSalonID: uuid.New(), ownerID: uuid.New(), otherOwnerID: uuid.New(),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id,email,password_hash,full_name) VALUES
			($1,$2,'hash','Switch Owner'),($3,$4,'hash','Other Switch Owner')
	`, fixture.ownerID, "v52-owner-"+uuid.NewString()+"@example.com", fixture.otherOwnerID, "v52-other-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V52 owners: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (id,name,phone,owner_user_id) VALUES
			($1,'Switch salon','5555200',$2),($3,'Other switch salon','5555201',$4)
	`, fixture.salonID, fixture.ownerID, fixture.otherSalonID, fixture.otherOwnerID); err != nil {
		t.Fatalf("seed V52 salons: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id,scheduling_authority) VALUES
			($1,'external_provider'),($2,'owner_manual')
	`, fixture.salonID, fixture.otherSalonID); err != nil {
		t.Fatalf("seed V52 salon settings: %v", err)
	}
	fixture.otherRunID = insertV52Run(t, ctx, tx, fixture, "other-salon-preview", strings.Repeat("9", 64), "owner_manual", "external_provider", "preview_ready", uuid.Nil, fixture.otherSalonID, fixture.otherOwnerID)
	return fixture
}

func insertV52Run(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	fixture v52Fixture,
	operationKey, fingerprint, sourceAuthority, targetAuthority, status string,
	rollbackOf uuid.UUID,
	overrides ...uuid.UUID,
) uuid.UUID {
	t.Helper()
	salonID, actorID := fixture.salonID, fixture.ownerID
	if len(overrides) == 2 {
		salonID, actorID = overrides[0], overrides[1]
	}
	runID := uuid.New()
	var rollback any
	if rollbackOf != uuid.Nil {
		rollback = rollbackOf
	}
	query := `
		INSERT INTO scheduling_authority_switch_runs (
			id,salon_id,source_scheduling_authority,target_scheduling_authority,
			expected_source_authority_version,operation_key,payload_fingerprint,
			readiness_snapshot,blockers,actor_user_id,status,blocked_at,rollback_of_switch_run_id
		) VALUES ($1,$2,$3,$4,1,$5,$6,'{"authority_ready":true}'::jsonb,
			CASE WHEN $8='preview_blocked' THEN '[{"code":"preview_blocked"}]'::jsonb ELSE '[]'::jsonb END,$7,$8,
			CASE WHEN $8='preview_blocked' THEN now() ELSE NULL END,$9)
	`
	if _, err := tx.ExecContext(ctx, query, runID, salonID, sourceAuthority, targetAuthority, operationKey, fingerprint, actorID, status, rollback); err != nil {
		t.Fatalf("insert V52 switch run %q: %v", operationKey, err)
	}
	return runID
}

func insertV52Event(t *testing.T, ctx context.Context, tx *sql.Tx, salonID, runID, actorID uuid.UUID, actionKey, eventType string) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_authority_switch_events (
			id,salon_id,switch_run_id,action_key,action_fingerprint,event_type,actor_user_id,payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'{"sanitized":true}'::jsonb)
	`, uuid.New(), salonID, runID, actionKey, strings.Repeat("8", 64), eventType, actorID); err != nil {
		t.Fatalf("insert V52 %s event: %v", eventType, err)
	}
}

func TestV52StatusTimestampOrder(t *testing.T) {
	if os.Getenv("MIGRATION_TEST_DATABASE_URL") == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, ctx, tx := beginV49PostgresTest(t, "v52_timestamp_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 52)
	fixture := seedV52Fixture(t, ctx, tx)
	runID := insertV52Run(t, ctx, tx, fixture, "timestamp-order", strings.Repeat("7", 64), "external_provider", "owner_manual", "preview_ready", uuid.Nil)
	past := time.Now().UTC().Add(-time.Hour)
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_authority_switch_runs_status_timestamps_check", `
		UPDATE scheduling_authority_switch_runs
		SET status='failed', failed_at=$1
		WHERE id=$2
	`, past, runID)
}
