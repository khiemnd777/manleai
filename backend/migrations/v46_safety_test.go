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

func readV46(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V46__owner_first_scheduling_authority.sql")
	if err != nil {
		t.Fatalf("read V46 migration: %v", err)
	}
	return string(source)
}

func TestV46ExpandsAuthoritySchemaWithoutChangingLegacyContracts(t *testing.T) {
	sql := readV46(t)
	for _, fragment := range []string{
		"ALTER TABLE salon_settings",
		"ALTER TABLE booking_attempts",
		"ALTER TABLE booking_attempt_segments",
		"ALTER TABLE appointments",
		"ALTER TABLE appointment_services",
		"ALTER TABLE availability_quotes",
		"ADD COLUMN scheduling_authority TEXT NOT NULL DEFAULT 'external_provider'",
		"ADD COLUMN confirmed_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT",
		"authority_appointment_version IS NULL OR authority_appointment_version >= 0",
		"target_authority_appointment_version IS NULL OR target_authority_appointment_version >= 0",
		"authority_service_version IS NULL OR authority_service_version >= 0",
		"authority_snapshot_generation IS NULL OR authority_snapshot_generation >= 0",
		"booking_attempts_authority_fence_pair_check",
		"availability_quotes_authority_fence_pair_check",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V46 is missing expand-only authority contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"ADD COLUMN IF NOT EXISTS",
		"ALTER TABLE IF EXISTS",
		"CREATE TABLE IF NOT EXISTS",
		"CREATE FUNCTION IF NOT EXISTS",
		"CREATE TRIGGER IF NOT EXISTS",
		"DROP COLUMN",
		"DROP CONSTRAINT",
		"DROP TABLE",
		"DELETE FROM",
		"TRUNCATE",
		"ALTER COLUMN",
		"SET status =",
		"SET booking_mode =",
		"CREATE UNIQUE INDEX",
		"CHECK (authority_provider = pos_provider)",
		"CHECK (authority_appointment_id = pos_appointment_id)",
		"CHECK (authority_service_id = pos_service_id)",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V46 must preserve the legacy runtime contract; found %q", forbidden)
		}
	}
}

func TestV46UsesExactSchedulingAuthorityProtocol(t *testing.T) {
	sql := readV46(t)
	const values = "('owner_manual', 'manleai_calendar', 'external_provider')"
	if got := strings.Count(sql, "scheduling_authority IN "+values); got != 6 {
		t.Fatalf("V46 scheduling-authority enum checks=%d, want 6 exact checks", got)
	}
	if got := strings.Count(sql, "confirmation_source IN "+values); got != 1 {
		t.Fatalf("V46 confirmation-source enum checks=%d, want 1 exact check", got)
	}
	if got := strings.Count(sql, "scheduling_authority TEXT NOT NULL DEFAULT 'external_provider'"); got != 6 {
		t.Fatalf("V46 scheduling-authority defaults=%d, want 6", got)
	}
	if strings.Contains(strings.ToLower(sql), "square") {
		t.Fatal("V46 must derive historical providers and must not hardcode Square")
	}
	if strings.Contains(sql, "active_pos_provider") {
		t.Fatal("V46 must preserve each row's historical provider instead of using the salon's current provider")
	}
}

func TestV46BackfillsAuthorityFromEachHistoricalOwner(t *testing.T) {
	sql := readV46(t)
	for _, fragment := range []string{
		"authority_provider = pos_provider",
		"authority_appointment_id = pos_booking_id",
		"authority_appointment_version = pos_booking_version",
		"target_authority_appointment_version = target_pos_booking_version",
		"authority_idempotency_key = pos_idempotency_key",
		"authority_location_id = provider_location_id",
		"authority_snapshot_generation = provider_snapshot_generation",
		"authority_provider = attempt.authority_provider",
		"authority_service_id = segment.pos_service_id",
		"authority_service_version = segment.pos_service_version",
		"authority_staff_id = segment.pos_staff_id",
		"FROM booking_attempts attempt",
		"authority_appointment_id = pos_appointment_id",
		"authority_customer_id = pos_customer_id",
		"authority_provider = appointment.authority_provider",
		"FROM appointments appointment",
		"authority_provider = provider",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V46 is missing historical authority backfill %q", fragment)
		}
	}
}

func TestV46DoesNotFabricateHistoricalConfirmationProvenance(t *testing.T) {
	sql := readV46(t)
	for _, fragment := range []string{
		"ADD COLUMN confirmed_at TIMESTAMPTZ",
		"ADD COLUMN confirmed_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT",
		"ADD COLUMN confirmation_source TEXT",
		"appointments_confirmation_source_authority_check",
		"appointments_confirmation_provenance_pair_check",
		"appointments_confirmation_actor_mode_check",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V46 is missing confirmation provenance contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"SET confirmed_at",
		"SET confirmed_by_user_id",
		"SET confirmation_source",
		"confirmed_at TIMESTAMPTZ NOT NULL",
		"confirmed_at TIMESTAMPTZ DEFAULT",
		"confirmation_source TEXT NOT NULL",
		"confirmation_source TEXT DEFAULT",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V46 must not fabricate historical confirmation evidence; found %q", forbidden)
		}
	}
}

func TestV46GuardsOwnerConfirmationAtTheDatabaseBoundary(t *testing.T) {
	sql := readV46(t)
	for _, fragment := range []string{
		"confirmation_source = scheduling_authority",
		"confirmation_source = 'owner_manual'\n                AND confirmed_by_user_id IS NOT NULL",
		"confirmation_source IN ('external_provider', 'manleai_calendar')\n                AND confirmed_by_user_id IS NULL",
		"CREATE FUNCTION enforce_appointment_confirmation_provenance()",
		"IF TG_OP = 'UPDATE' AND OLD.confirmed_at IS NOT NULL THEN",
		"OLD.confirmed_at IS DISTINCT FROM NEW.confirmed_at",
		"OLD.confirmation_source IS DISTINCT FROM NEW.confirmation_source",
		"OLD.confirmed_by_user_id IS DISTINCT FROM NEW.confirmed_by_user_id",
		"OLD.scheduling_authority IS DISTINCT FROM NEW.scheduling_authority",
		"OLD.salon_id IS DISTINCT FROM NEW.salon_id",
		"CONSTRAINT = 'appointments_confirmation_provenance_immutable_guard'",
		"NEW.confirmation_source IS DISTINCT FROM 'owner_manual'",
		"FROM salons salon",
		"salon.id = NEW.salon_id",
		"salon.owner_user_id = NEW.confirmed_by_user_id",
		"CREATE TRIGGER appointments_confirmation_provenance_guard",
		"BEFORE INSERT OR UPDATE OF confirmed_at, confirmation_source, confirmed_by_user_id, scheduling_authority, salon_id",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("V46 is missing owner confirmation database guard %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"AFTER INSERT OR UPDATE",
		"UPDATE OF notes",
		"CREATE TRIGGER appointments_confirmation_provenance_guard\nBEFORE UPDATE ON",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("V46 owner guard is broader than the provenance write boundary: found %q", forbidden)
		}
	}
}

func TestV46ConfirmationProvenanceConstraintsInPostgres(t *testing.T) {
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

	schemaName := "v46_provenance_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
		CREATE TABLE salon_settings (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id)
		);
		CREATE TABLE booking_attempts (
			id UUID PRIMARY KEY,
			pos_provider TEXT NOT NULL,
			pos_booking_id TEXT,
			pos_booking_version INTEGER,
			target_pos_booking_version INTEGER NOT NULL DEFAULT 0,
			pos_idempotency_key TEXT,
			provider_location_id TEXT,
			provider_snapshot_generation BIGINT
		);
		CREATE TABLE booking_attempt_segments (
			id UUID PRIMARY KEY,
			booking_attempt_id UUID NOT NULL,
			pos_service_id TEXT NOT NULL,
			pos_service_version BIGINT,
			pos_staff_id TEXT
		);
		CREATE TABLE appointments (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id),
			pos_provider TEXT NOT NULL,
			pos_appointment_id TEXT NOT NULL,
			pos_appointment_version INTEGER NOT NULL DEFAULT 0,
			pos_customer_id TEXT,
			notes TEXT
		);
		CREATE TABLE appointment_services (
			id UUID PRIMARY KEY,
			appointment_id UUID NOT NULL,
			pos_service_id TEXT NOT NULL,
			pos_service_version BIGINT,
			pos_staff_id TEXT
		);
		CREATE TABLE availability_quotes (
			id UUID PRIMARY KEY,
			provider TEXT NOT NULL,
			provider_location_id TEXT NOT NULL,
			provider_snapshot_generation BIGINT NOT NULL
		);
	`); err != nil {
		t.Fatalf("create pre-V46 fixture schema: %v", err)
	}

	const (
		ownerID       = "00000000-0000-0000-0000-000000000001"
		foreignID     = "00000000-0000-0000-0000-000000000002"
		nextOwnerID   = "00000000-0000-0000-0000-000000000003"
		salonID       = "00000000-0000-0000-0000-000000000010"
		secondSalonID = "00000000-0000-0000-0000-000000000011"
		historicalID  = "00000000-0000-0000-0000-000000000100"
	)
	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id) VALUES ($1), ($2), ($3)`, ownerID, foreignID, nextOwnerID); err != nil {
		t.Fatalf("seed fixture users: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (id, owner_user_id)
		VALUES ($1, $2), ($3, $4)
	`, salonID, ownerID, secondSalonID, nextOwnerID); err != nil {
		t.Fatalf("seed fixture salon: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (id, salon_id)
		VALUES ('00000000-0000-0000-0000-000000000020', $1)
	`, salonID); err != nil {
		t.Fatalf("seed fixture salon settings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id, salon_id, pos_provider, pos_appointment_id, pos_appointment_version
		) VALUES ($1, $2, 'fixture_provider', 'historical-booking', 3)
	`, historicalID, salonID); err != nil {
		t.Fatalf("seed historical appointment: %v", err)
	}

	migrationSQL := readV46(t)
	if _, err := tx.ExecContext(ctx, migrationSQL); err != nil {
		t.Fatalf("apply V46 in test schema: %v", err)
	}

	var historicalAuthority string
	var historicalConfirmedAt sql.NullTime
	var historicalActor sql.NullString
	var historicalSource sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT scheduling_authority, confirmed_at, confirmed_by_user_id::text, confirmation_source
		FROM appointments
		WHERE id = $1
	`, historicalID).Scan(&historicalAuthority, &historicalConfirmedAt, &historicalActor, &historicalSource); err != nil {
		t.Fatalf("read historical appointment after V46: %v", err)
	}
	if historicalAuthority != "external_provider" || historicalConfirmedAt.Valid || historicalActor.Valid || historicalSource.Valid {
		t.Fatalf(
			"historical provenance = authority:%q confirmed:%v actor:%v source:%v, want external_provider with null provenance",
			historicalAuthority,
			historicalConfirmedAt.Valid,
			historicalActor.Valid,
			historicalSource.Valid,
		)
	}

	const insertConfirmedAppointment = `
		INSERT INTO appointments (
			id, salon_id, pos_provider, pos_appointment_id, pos_appointment_version,
			scheduling_authority, confirmed_at, confirmed_by_user_id, confirmation_source
		) VALUES ($1, $2, 'fixture_provider', $3, 1, $4, now(), $5::uuid, $6)
	`
	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"mismatched_source",
		"appointments_confirmation_source_authority_check",
		insertConfirmedAppointment,
		"00000000-0000-0000-0000-000000000201",
		salonID,
		"mismatched-source",
		"external_provider",
		ownerID,
		"owner_manual",
	)
	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"missing_owner_actor",
		"appointments_confirmation_actor_mode_check",
		insertConfirmedAppointment,
		"00000000-0000-0000-0000-000000000202",
		salonID,
		"missing-owner-actor",
		"owner_manual",
		nil,
		"owner_manual",
	)
	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"foreign_owner_actor",
		"appointments_owner_confirmation_actor_guard",
		insertConfirmedAppointment,
		"00000000-0000-0000-0000-000000000203",
		salonID,
		"foreign-owner-actor",
		"owner_manual",
		foreignID,
		"owner_manual",
	)

	const ownerManualAppointmentID = "00000000-0000-0000-0000-000000000204"
	if _, err := tx.ExecContext(
		ctx,
		insertConfirmedAppointment,
		ownerManualAppointmentID,
		salonID,
		"valid-owner-manual",
		"owner_manual",
		ownerID,
		"owner_manual",
	); err != nil {
		t.Fatalf("insert valid current-owner confirmation: %v", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		insertConfirmedAppointment,
		"00000000-0000-0000-0000-000000000205",
		salonID,
		"valid-external-provider",
		"external_provider",
		nil,
		"external_provider",
	); err != nil {
		t.Fatalf("insert valid external-provider confirmation with null actor: %v", err)
	}

	const transitionedExternalAppointmentID = "00000000-0000-0000-0000-000000000206"
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id, salon_id, pos_provider, pos_appointment_id, pos_appointment_version
		) VALUES ($1, $2, 'fixture_provider', 'transitioned-external-provider', 1)
	`, transitionedExternalAppointmentID, salonID); err != nil {
		t.Fatalf("insert unconfirmed external-provider appointment: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET confirmed_at = now(),
		    confirmation_source = 'external_provider'
		WHERE id = $1
	`, transitionedExternalAppointmentID); err != nil {
		t.Fatalf("transition unconfirmed appointment once: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET confirmed_at = COALESCE(confirmed_at, now()),
		    confirmation_source = COALESCE(confirmation_source, 'external_provider'),
		    notes = 'external reschedule retained provenance'
		WHERE id = $1
	`, transitionedExternalAppointmentID); err != nil {
		t.Fatalf("retain external-provider provenance during existing update path: %v", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE salons SET owner_user_id = $1 WHERE id = $2`, nextOwnerID, salonID); err != nil {
		t.Fatalf("transfer salon ownership: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET notes = 'ownership transferred'
		WHERE id = $1
	`, ownerManualAppointmentID); err != nil {
		t.Fatalf("update immutable owner confirmation history after ownership transfer: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET confirmation_source = confirmation_source
		WHERE id = $1
	`, ownerManualAppointmentID); err != nil {
		t.Fatalf("retain unchanged owner provenance after ownership transfer: %v", err)
	}

	var retainedActor string
	var retainedAuthority string
	var retainedSource string
	var retainedNotes string
	var retainedConfirmed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT confirmed_by_user_id::text, scheduling_authority, confirmation_source,
		       COALESCE(notes, ''), confirmed_at IS NOT NULL
		FROM appointments
		WHERE id = $1
	`, ownerManualAppointmentID).Scan(
		&retainedActor,
		&retainedAuthority,
		&retainedSource,
		&retainedNotes,
		&retainedConfirmed,
	); err != nil {
		t.Fatalf("read immutable provenance after ownership transfer: %v", err)
	}
	if retainedActor != ownerID || retainedAuthority != "owner_manual" || retainedSource != "owner_manual" ||
		retainedNotes != "ownership transferred" || !retainedConfirmed {
		t.Fatalf(
			"retained provenance = actor:%q authority:%q source:%q notes:%q confirmed:%v",
			retainedActor,
			retainedAuthority,
			retainedSource,
			retainedNotes,
			retainedConfirmed,
		)
	}

	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"replace_confirming_actor",
		"appointments_confirmation_provenance_immutable_guard",
		`UPDATE appointments SET confirmed_by_user_id = $1 WHERE id = $2`,
		nextOwnerID,
		ownerManualAppointmentID,
	)
	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"rewrite_confirmation_source",
		"appointments_confirmation_provenance_immutable_guard",
		`
			UPDATE appointments
			SET scheduling_authority = 'external_provider',
			    confirmation_source = 'external_provider',
			    confirmed_by_user_id = NULL
			WHERE id = $1
		`,
		ownerManualAppointmentID,
	)
	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"rewrite_confirmed_at",
		"appointments_confirmation_provenance_immutable_guard",
		`UPDATE appointments SET confirmed_at = confirmed_at + interval '1 second' WHERE id = $1`,
		ownerManualAppointmentID,
	)
	expectV46PostgresConstraint(
		t,
		ctx,
		tx,
		"move_confirmed_appointment",
		"appointments_confirmation_provenance_immutable_guard",
		`UPDATE appointments SET salon_id = $1 WHERE id = $2`,
		secondSalonID,
		ownerManualAppointmentID,
	)
}

func expectV46PostgresConstraint(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	savepointName string,
	wantConstraint string,
	query string,
	args ...any,
) {
	t.Helper()
	quotedSavepoint := pq.QuoteIdentifier("v46_" + savepointName)
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+quotedSavepoint); err != nil {
		t.Fatalf("create savepoint %s: %v", savepointName, err)
	}
	_, execErr := tx.ExecContext(ctx, query, args...)
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+quotedSavepoint); err != nil {
		t.Fatalf("rollback savepoint %s: %v", savepointName, err)
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+quotedSavepoint); err != nil {
		t.Fatalf("release savepoint %s: %v", savepointName, err)
	}
	if execErr == nil {
		t.Fatalf("statement unexpectedly satisfied %s", wantConstraint)
	}
	var postgresErr *pq.Error
	if !errors.As(execErr, &postgresErr) {
		t.Fatalf("statement error = %T %v, want PostgreSQL constraint %s", execErr, execErr, wantConstraint)
	}
	if postgresErr.Code != "23514" {
		t.Fatalf("PostgreSQL error code = %s, want 23514 for %s", postgresErr.Code, wantConstraint)
	}
	if postgresErr.Constraint != wantConstraint {
		t.Fatalf("PostgreSQL constraint = %q, want %q", postgresErr.Constraint, wantConstraint)
	}
}
