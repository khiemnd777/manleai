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

func readV48(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V48__manleai_calendar_configuration.sql")
	if err != nil {
		t.Fatalf("read V48 migration: %v", err)
	}
	return string(source)
}

func TestV48DefinesManleAICalendarConfigurationContract(t *testing.T) {
	source := readV48(t)
	for _, fragment := range []string{
		"ADD COLUMN scheduling_authority_version BIGINT NOT NULL DEFAULT 1",
		"ADD COLUMN end_at_midnight BOOLEAN NOT NULL DEFAULT false",
		"activated_version BIGINT",
		"source = 'local_override'",
		"CREATE TABLE manleai_calendar_configs",
		"CREATE TABLE manleai_calendar_service_policies",
		"CREATE TABLE manleai_calendar_service_staff",
		"CREATE TABLE manleai_calendar_staff_weekly_periods",
		"CREATE TABLE manleai_calendar_resource_pools",
		"CREATE TABLE manleai_calendar_service_resources",
		"CREATE TABLE manleai_calendar_exceptions",
		"CREATE TABLE manleai_calendar_config_events",
		"capacity_mode IN ('staff_only', 'pooled')",
		"scope_type = 'resource'",
		"effect = 'capacity_override'",
		"DEFERRABLE INITIALLY DEFERRED",
		"EXCLUDE USING gist",
		"UNIQUE (salon_id, action_key)",
		"manleai_calendar_config_events_result_version_guard",
		"manleai_calendar_exceptions_immutable_guard",
		"manleai_calendar_exceptions_delete_guard",
		"manleai_calendar_config_events_immutable_guard",
		"ELSIF OLD.updated_at IS DISTINCT FROM NEW.updated_at THEN",
		"AFTER UPDATE OF duration_minutes, active, ai_bookable, archived_at ON services",
		"AFTER UPDATE OF active, ai_bookable, archived_at ON staff",
		"AFTER UPDATE OF timezone ON salons",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V48 is missing calendar configuration contract %q", fragment)
		}
	}

	for _, forbidden := range []string{
		"ALTER TABLE appointments",
		"ALTER TABLE appointment_services",
		"ALTER TABLE booking_attempts",
		"ALTER TABLE booking_attempt_segments",
		"INSERT INTO manleai_calendar_configs",
		"INSERT INTO manleai_calendar_service_policies",
		"UPDATE salon_settings\nSET scheduling_authority",
		"UPDATE salon_business_hour_periods\nSET source",
		"DELETE FROM salon_business_hour_periods",
		"pos_appointment_id",
		"pos_booking_id",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V48 exceeds the approved configuration-only scope; found %q", forbidden)
		}
	}
}

func TestV48KeepsCalendarConfigurationDataDriven(t *testing.T) {
	source := strings.ToLower(readV48(t))
	for _, forbidden := range []string{
		"square",
		"pedicure",
		"manicure",
		"technician",
		"service_name =",
		"staff_name =",
		"capacity = 1",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V48 must not hardcode salon, provider, service, staff, or capacity data; found %q", forbidden)
		}
	}
}

func TestV48CalendarConfigurationConstraintsInPostgres(t *testing.T) {
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

	schemaName := "v48_safety_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
			owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
			timezone TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE salon_settings (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			salon_id UUID NOT NULL UNIQUE REFERENCES salons(id) ON DELETE CASCADE,
			scheduling_authority TEXT NOT NULL DEFAULT 'external_provider'
		);
		CREATE TABLE salon_business_hour_periods (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
			day_of_week SMALLINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
			start_local_time TIME NOT NULL,
			end_local_time TIME NOT NULL,
			source TEXT NOT NULL DEFAULT 'imported' CHECK (source IN ('imported', 'local_migrated', 'local_override')),
			provider TEXT NOT NULL DEFAULT '',
			provider_location_id TEXT NOT NULL DEFAULT '',
			provider_period_index INTEGER NOT NULL DEFAULT 0,
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT salon_business_hour_periods_check CHECK (end_local_time > start_local_time),
			UNIQUE (salon_id, provider, provider_location_id, day_of_week, provider_period_index)
		);
		CREATE TABLE services (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
			duration_minutes INTEGER NOT NULL DEFAULT 0,
			active BOOLEAN NOT NULL DEFAULT true,
			ai_bookable BOOLEAN NOT NULL DEFAULT true,
			archived_at TIMESTAMPTZ,
			UNIQUE (salon_id, id)
		);
		CREATE TABLE staff (
			id UUID PRIMARY KEY,
			salon_id UUID NOT NULL REFERENCES salons(id) ON DELETE CASCADE,
			active BOOLEAN NOT NULL DEFAULT true,
			ai_bookable BOOLEAN NOT NULL DEFAULT true,
			archived_at TIMESTAMPTZ,
			UNIQUE (salon_id, id)
		);
	`); err != nil {
		t.Fatalf("create pre-V48 fixture: %v", err)
	}

	ownerID := uuid.New()
	otherOwnerID := uuid.New()
	salonID := uuid.New()
	otherSalonID := uuid.New()
	serviceID := uuid.New()
	otherServiceID := uuid.New()
	staffID := uuid.New()
	otherStaffID := uuid.New()
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO users (id) VALUES ($1), ($2)", []any{ownerID, otherOwnerID}},
		{"INSERT INTO salons (id, owner_user_id, timezone) VALUES ($1, $2, 'America/Chicago'), ($3, $4, 'America/New_York')", []any{salonID, ownerID, otherSalonID, otherOwnerID}},
		{"INSERT INTO salon_settings (salon_id) VALUES ($1), ($2)", []any{salonID, otherSalonID}},
		{"INSERT INTO services (id, salon_id, duration_minutes) VALUES ($1, $2, 45), ($3, $4, 30)", []any{serviceID, salonID, otherServiceID, otherSalonID}},
		{"INSERT INTO staff (id, salon_id) VALUES ($1, $2), ($3, $4)", []any{staffID, salonID, otherStaffID, otherSalonID}},
		{`INSERT INTO salon_business_hour_periods
			(salon_id, day_of_week, start_local_time, end_local_time, source, provider_period_index)
			VALUES ($1, 1, '09:00', '17:00', 'local_migrated', 0)`, []any{salonID}},
		{`INSERT INTO salon_business_hour_periods
			(salon_id, day_of_week, start_local_time, end_local_time, source, provider, provider_location_id, provider_period_index, last_synced_at)
			VALUES ($1, 2, '09:00', '17:00', 'imported', 'provider', 'location', 0, now())`, []any{salonID}},
	} {
		if _, err := tx.ExecContext(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seed pre-V48 fixture: %v", err)
		}
	}

	if _, err := tx.ExecContext(ctx, readV48(t)); err != nil {
		t.Fatalf("apply V48 to pre-V48 fixture: %v", err)
	}

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_configs`).Scan(&count); err != nil {
		t.Fatalf("count V48 configs: %v", err)
	}
	if count != 0 {
		t.Fatalf("V48 backfilled ready configuration rows=%d, want 0", count)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM salon_business_hour_periods WHERE source = 'local_override'`).Scan(&count); err != nil {
		t.Fatalf("count local override periods: %v", err)
	}
	if count != 0 {
		t.Fatalf("V48 fabricated local override periods=%d, want 0", count)
	}

	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_configs_activation_version_guard", `
		INSERT INTO manleai_calendar_configs (
			salon_id, slot_step_minutes, minimum_booking_notice_minutes,
			booking_horizon_days, max_party_size, activated_at, activated_by_user_id
		) VALUES ($1, 15, 60, 90, 6, now(), $2)
	`, salonID, otherOwnerID)

	for _, id := range []uuid.UUID{salonID, otherSalonID} {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO manleai_calendar_configs (
				salon_id, slot_step_minutes, minimum_booking_notice_minutes,
				booking_horizon_days, max_party_size
			) VALUES ($1, 15, 60, 90, 6)
		`, id); err != nil {
			t.Fatalf("insert calendar config: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id)
		VALUES ($1, $2, $3)
	`, otherSalonID, otherServiceID, otherStaffID); err != nil {
		t.Fatalf("insert staff-first eligibility without service policy: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM manleai_calendar_service_policies
		WHERE salon_id = $1 AND service_id = $2
	`, otherSalonID, otherServiceID).Scan(&count); err != nil {
		t.Fatalf("count staff-first service policies: %v", err)
	}
	if count != 0 {
		t.Fatalf("staff-first eligibility fabricated service policies=%d, want 0", count)
	}

	expectV48PostgresError(t, ctx, tx, "23514", "salon_settings_scheduling_authority_version_guard", `
		UPDATE salon_settings SET scheduling_authority_version = 9 WHERE salon_id = $1
	`, salonID)
	if _, err := tx.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority = 'owner_manual' WHERE salon_id = $1`, salonID); err != nil {
		t.Fatalf("change scheduling authority: %v", err)
	}
	assertV48Int64(t, ctx, tx, `SELECT scheduling_authority_version FROM salon_settings WHERE salon_id = $1`, salonID, 2)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, end_at_midnight,
			source, provider_period_index
		) VALUES ($1, 1, '18:00', '00:00', true, 'local_override', 1)
	`, salonID); err != nil {
		t.Fatalf("insert midnight-ending local hours: %v", err)
	}
	assertV48ConfigVersion(t, ctx, tx, salonID, 2)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source,
			provider, provider_location_id, provider_period_index, last_synced_at
		) VALUES ($1, 3, '09:00', '17:00', 'imported', 'provider', 'location', 0, now())
	`, salonID); err != nil {
		t.Fatalf("insert imported hours: %v", err)
	}
	assertV48ConfigVersion(t, ctx, tx, salonID, 2)

	expectV48PostgresError(t, ctx, tx, "23P01", "salon_business_hour_periods_local_override_no_overlap", `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source, provider_period_index
		) VALUES ($1, 1, '19:00', '22:00', 'local_override', 2)
	`, salonID)
	expectV48PostgresError(t, ctx, tx, "23514", "salon_business_hour_periods_local_override_shape_check", `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source,
			provider, provider_period_index
		) VALUES ($1, 4, '09:00', '17:00', 'local_override', 'provider', 1)
	`, salonID)

	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_service_policies_enabled_mode_check", `
		INSERT INTO manleai_calendar_service_policies (salon_id, service_id, enabled)
		VALUES ($1, $2, true)
	`, salonID, serviceID)
	expectV48PostgresError(t, ctx, tx, "23503", "manleai_calendar_service_policies_service_tenant_fk", `
		INSERT INTO manleai_calendar_service_policies (salon_id, service_id, enabled, capacity_mode)
		VALUES ($1, $2, true, 'staff_only')
	`, salonID, otherServiceID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_policies (salon_id, service_id, enabled, capacity_mode)
		VALUES ($1, $2, true, 'staff_only')
	`, salonID, serviceID); err != nil {
		t.Fatalf("insert service policy: %v", err)
	}

	expectV48PostgresError(t, ctx, tx, "23503", "manleai_calendar_service_staff_staff_tenant_fk", `
		INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id)
		VALUES ($1, $2, $3)
	`, salonID, serviceID, otherStaffID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id)
		VALUES ($1, $2, $3)
	`, salonID, serviceID, staffID); err != nil {
		t.Fatalf("insert service staff eligibility: %v", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_staff_weekly_periods
			(salon_id, staff_id, day_of_week, start_minute, end_minute)
		VALUES ($1, $2, 1, 540, 1020)
	`, salonID, staffID); err != nil {
		t.Fatalf("insert staff weekly period: %v", err)
	}
	expectV48PostgresError(t, ctx, tx, "23P01", "manleai_calendar_staff_weekly_periods_no_overlap", `
		INSERT INTO manleai_calendar_staff_weekly_periods
			(salon_id, staff_id, day_of_week, start_minute, end_minute)
		VALUES ($1, $2, 1, 600, 1080)
	`, salonID, staffID)

	poolID := uuid.New()
	otherPoolID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_resource_pools (id, salon_id, name, capacity)
		VALUES ($1, $2, 'Shared stations', 4), ($3, $4, 'Other stations', 2)
	`, poolID, salonID, otherPoolID, otherSalonID); err != nil {
		t.Fatalf("insert resource pools: %v", err)
	}
	expectV48PostgresError(t, ctx, tx, "23505", "idx_manleai_calendar_resource_pools_active_name", `
		INSERT INTO manleai_calendar_resource_pools (salon_id, name, capacity)
		VALUES ($1, ' shared stations ', 3)
	`, salonID)
	expectV48PostgresError(t, ctx, tx, "23503", "manleai_calendar_service_resources_pool_tenant_fk", `
		INSERT INTO manleai_calendar_service_resources
			(salon_id, service_id, resource_pool_id, units_required)
		VALUES ($1, $2, $3, 1)
	`, salonID, serviceID, otherPoolID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_resources
			(salon_id, service_id, resource_pool_id, units_required)
		VALUES ($1, $2, $3, 1)
	`, salonID, serviceID, poolID); err != nil {
		t.Fatalf("insert service resource requirement: %v", err)
	}

	startsAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	endsAt := startsAt.Add(2 * time.Hour)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_exceptions_creator_owner_guard", `
		INSERT INTO manleai_calendar_exceptions (
			salon_id, scope_type, staff_id, effect, starts_at, ends_at, created_by_user_id
		) VALUES ($1, 'staff', $2, 'unavailable', $3, $4, $5)
	`, salonID, staffID, startsAt, endsAt, otherOwnerID)

	exceptionID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_exceptions (
			id, salon_id, scope_type, staff_id, effect, starts_at, ends_at,
			reason, created_by_user_id
		) VALUES ($1, $2, 'staff', $3, 'unavailable', $4, $5, 'Approved time off', $6)
	`, exceptionID, salonID, staffID, startsAt, endsAt, ownerID); err != nil {
		t.Fatalf("insert staff exception: %v", err)
	}
	expectV48PostgresError(t, ctx, tx, "23P01", "manleai_calendar_exceptions_no_overlap", `
		INSERT INTO manleai_calendar_exceptions (
			salon_id, scope_type, staff_id, effect, starts_at, ends_at, created_by_user_id
		) VALUES ($1, 'staff', $2, 'available', $3, $4, $5)
	`, salonID, staffID, startsAt.Add(time.Hour), endsAt.Add(time.Hour), ownerID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_exceptions_immutable_guard", `
		UPDATE manleai_calendar_exceptions SET reason = 'Changed' WHERE id = $1
	`, exceptionID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_exceptions_delete_guard", `
		DELETE FROM manleai_calendar_exceptions WHERE id = $1
	`, exceptionID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_exceptions_cancellation_actor_guard", `
		UPDATE manleai_calendar_exceptions
		SET cancelled_at = now(), cancelled_by_user_id = $2
		WHERE id = $1
	`, exceptionID, otherOwnerID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_exceptions
		SET cancelled_at = now(), cancelled_by_user_id = $2
		WHERE id = $1
	`, exceptionID, ownerID); err != nil {
		t.Fatalf("cancel exception: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_exceptions (
			salon_id, scope_type, staff_id, effect, starts_at, ends_at, created_by_user_id
		) VALUES ($1, 'staff', $2, 'available', $3, $4, $5)
	`, salonID, staffID, startsAt, endsAt, ownerID); err != nil {
		t.Fatalf("replace cancelled exception: %v", err)
	}

	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_configs_activation_actor_guard", `
		UPDATE manleai_calendar_configs
		SET activated_at = now(), activated_by_user_id = $2
		WHERE salon_id = $1
	`, salonID, otherOwnerID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_configs
		SET activated_at = now(), activated_by_user_id = $2
		WHERE salon_id = $1
	`, salonID, ownerID); err != nil {
		t.Fatalf("activate calendar config: %v", err)
	}
	activatedVersion := loadV48ActivatedVersion(t, ctx, tx, salonID)
	if activatedVersion != loadV48ConfigVersion(t, ctx, tx, salonID) {
		t.Fatalf("activated version=%d, want current config version", activatedVersion)
	}
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_configs_activation_version_guard", `
		UPDATE manleai_calendar_configs SET activated_version = activated_version + 1
		WHERE salon_id = $1
	`, salonID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_configs_version_guard", `
		UPDATE manleai_calendar_configs SET version = version + 1 WHERE salon_id = $1
	`, salonID)
	versionBeforeTouch := loadV48ConfigVersion(t, ctx, tx, salonID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_configs SET updated_at = updated_at + interval '1 second'
		WHERE salon_id = $1
	`, salonID); err != nil {
		t.Fatalf("touch calendar config: %v", err)
	}
	assertV48ConfigVersion(t, ctx, tx, salonID, versionBeforeTouch+1)

	versionBeforeOwners := loadV48ConfigVersion(t, ctx, tx, salonID)
	if _, err := tx.ExecContext(ctx, `UPDATE salons SET timezone = 'America/Denver' WHERE id = $1`, salonID); err != nil {
		t.Fatalf("update salon timezone: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE services SET duration_minutes = duration_minutes + 5 WHERE id = $1`, serviceID); err != nil {
		t.Fatalf("update service duration: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE staff SET ai_bookable = false WHERE id = $1`, staffID); err != nil {
		t.Fatalf("update staff bookability: %v", err)
	}
	assertV48ConfigVersion(t, ctx, tx, salonID, versionBeforeOwners+3)
	staleActivatedVersion := loadV48ActivatedVersion(t, ctx, tx, salonID)
	if staleActivatedVersion == loadV48ConfigVersion(t, ctx, tx, salonID) {
		t.Fatal("ordinary scheduling changes must make activation evidence stale")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_configs
		SET activated_at = clock_timestamp(), activated_by_user_id = $2
		WHERE salon_id = $1
	`, salonID, ownerID); err != nil {
		t.Fatalf("re-activate changed calendar config: %v", err)
	}
	if got, want := loadV48ActivatedVersion(t, ctx, tx, salonID), loadV48ConfigVersion(t, ctx, tx, salonID); got != want {
		t.Fatalf("re-activated version=%d, want current config version=%d", got, want)
	}

	currentVersion := loadV48ConfigVersion(t, ctx, tx, salonID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_config_events_actor_owner_guard", `
		INSERT INTO manleai_calendar_config_events (
			salon_id, action_key, action_fingerprint, event_type,
			previous_version, result_version, actor_user_id
		) VALUES ($1, 'action-wrong-owner', $2, 'config_updated', $3, $4, $5)
	`, salonID, strings.Repeat("a", 64), currentVersion-1, currentVersion, otherOwnerID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_config_events_result_version_guard", `
		INSERT INTO manleai_calendar_config_events (
			salon_id, action_key, action_fingerprint, event_type,
			previous_version, result_version, actor_user_id
		) VALUES ($1, 'action-stale-result', $2, 'config_updated', $3, $4, $5)
	`, salonID, strings.Repeat("b", 64), currentVersion, currentVersion+1, ownerID)

	eventID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_config_events (
			id, salon_id, action_key, action_fingerprint, event_type,
			previous_version, result_version, actor_user_id
		) VALUES ($1, $2, 'action-1', $3, 'config_updated', $4, $5, $6)
	`, eventID, salonID, strings.Repeat("c", 64), currentVersion-1, currentVersion, ownerID); err != nil {
		t.Fatalf("insert calendar config event: %v", err)
	}
	expectV48PostgresError(t, ctx, tx, "23505", "manleai_calendar_config_events_action_key", `
		INSERT INTO manleai_calendar_config_events (
			salon_id, action_key, action_fingerprint, event_type,
			previous_version, result_version, actor_user_id
		) VALUES ($1, 'action-1', $2, 'config_updated', $3, $4, $5)
	`, salonID, strings.Repeat("d", 64), currentVersion-1, currentVersion, ownerID)
	expectV48PostgresError(t, ctx, tx, "23514", "manleai_calendar_config_events_immutable_guard", `
		UPDATE manleai_calendar_config_events SET payload = '{"changed":true}'::jsonb WHERE id = $1
	`, eventID)

	if _, err := tx.ExecContext(ctx, `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
		t.Fatalf("cascade-delete salon with immutable calendar evidence: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatalf("validate deferred calendar tenant constraints after salon cascade: %v", err)
	}
	for _, table := range []string{
		"manleai_calendar_configs",
		"manleai_calendar_exceptions",
		"manleai_calendar_config_events",
	} {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+pq.QuoteIdentifier(table)+` WHERE salon_id = $1`, salonID).Scan(&count); err != nil {
			t.Fatalf("count %s after salon cascade: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after salon cascade=%d, want 0", table, count)
		}
	}
}

func TestV48UpgradesCompleteV47SchemaInPostgres(t *testing.T) {
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

	schemaName := "v48_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create upgrade schema: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("set upgrade search path: %v", err)
	}

	paths, err := fs.Glob(Files, "V*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Slice(paths, func(i, j int) bool {
		return v48MigrationVersion(t, paths[i]) < v48MigrationVersion(t, paths[j])
	})
	for _, migrationPath := range paths {
		version := v48MigrationVersion(t, migrationPath)
		if version >= 48 {
			continue
		}
		source, err := Files.ReadFile(migrationPath)
		if err != nil {
			t.Fatalf("read %s: %v", migrationPath, err)
		}
		if _, err := tx.ExecContext(ctx, string(source)); err != nil {
			t.Fatalf("apply pre-V48 migration %s: %v", migrationPath, err)
		}
	}

	var hasV47 bool
	if err := tx.QueryRowContext(ctx, `
		SELECT to_regclass('scheduling_requests') IS NOT NULL
	`).Scan(&hasV47); err != nil {
		t.Fatalf("check V47 schema: %v", err)
	}
	if !hasV47 {
		t.Fatal("complete pre-V48 chain did not create the V47 request aggregate")
	}

	if _, err := tx.ExecContext(ctx, readV48(t)); err != nil {
		t.Fatalf("upgrade complete V47 schema with V48: %v", err)
	}

	var hasConfig bool
	if err := tx.QueryRowContext(ctx, `
		SELECT to_regclass('manleai_calendar_configs') IS NOT NULL
	`).Scan(&hasConfig); err != nil {
		t.Fatalf("check V48 schema: %v", err)
	}
	if !hasConfig {
		t.Fatal("V48 upgrade did not create ManleAI calendar configuration")
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_configs`).Scan(&count); err != nil {
		t.Fatalf("count upgraded calendar configs: %v", err)
	}
	if count != 0 {
		t.Fatalf("V48 complete upgrade backfilled configs=%d, want 0", count)
	}
}

func v48MigrationVersion(t *testing.T, migrationPath string) int {
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

func expectV48PostgresError(
	t *testing.T,
	ctx context.Context,
	tx *sql.Tx,
	code string,
	constraint string,
	query string,
	args ...any,
) {
	t.Helper()
	savepoint := pq.QuoteIdentifier("v48_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create V48 savepoint: %v", err)
	}
	_, execErr := tx.ExecContext(ctx, query, args...)
	if execErr == nil {
		_, execErr = tx.ExecContext(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	}
	if execErr == nil {
		t.Fatalf("expected PostgreSQL error %s/%s", code, constraint)
	}
	var pqErr *pq.Error
	if !errors.As(execErr, &pqErr) {
		t.Fatalf("error=%v, want PostgreSQL error %s/%s", execErr, code, constraint)
	}
	if string(pqErr.Code) != code || pqErr.Constraint != constraint {
		t.Fatalf("PostgreSQL error=%s/%s, want %s/%s: %v", pqErr.Code, pqErr.Constraint, code, constraint, execErr)
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("roll back V48 savepoint: %v", err)
	}
}

func loadV48ConfigVersion(t *testing.T, ctx context.Context, tx *sql.Tx, salonID uuid.UUID) int64 {
	t.Helper()
	var version int64
	if err := tx.QueryRowContext(ctx,
		`SELECT version FROM manleai_calendar_configs WHERE salon_id = $1`, salonID,
	).Scan(&version); err != nil {
		t.Fatalf("load V48 config version: %v", err)
	}
	return version
}

func assertV48ConfigVersion(t *testing.T, ctx context.Context, tx *sql.Tx, salonID uuid.UUID, want int64) {
	t.Helper()
	if got := loadV48ConfigVersion(t, ctx, tx, salonID); got != want {
		t.Fatalf("calendar config version=%d, want %d", got, want)
	}
}

func loadV48ActivatedVersion(t *testing.T, ctx context.Context, tx *sql.Tx, salonID uuid.UUID) int64 {
	t.Helper()
	var version int64
	if err := tx.QueryRowContext(ctx,
		`SELECT activated_version FROM manleai_calendar_configs WHERE salon_id = $1`, salonID,
	).Scan(&version); err != nil {
		t.Fatalf("load V48 activated version: %v", err)
	}
	return version
}

func assertV48Int64(t *testing.T, ctx context.Context, tx *sql.Tx, query string, arg any, want int64) {
	t.Helper()
	var got int64
	if err := tx.QueryRowContext(ctx, query, arg).Scan(&got); err != nil {
		t.Fatalf("load integer assertion: %v", err)
	}
	if got != want {
		t.Fatalf("integer value=%d, want %d", got, want)
	}
}
