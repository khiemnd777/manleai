package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func readV54(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V54__owner_first_onboarding.sql")
	if err != nil {
		t.Fatalf("read V54 migration: %v", err)
	}
	return string(source)
}

func TestV54DefinesOwnerFirstOnboardingSafetyContract(t *testing.T) {
	source := readV54(t)
	for _, fragment := range []string{
		"creation_operation_key TEXT",
		"creation_payload_fingerprint TEXT",
		"salons_creation_proof_pair_check",
		"salons_creation_operation_key_check",
		"salons_creation_payload_fingerprint_check",
		"CREATE UNIQUE INDEX salons_owner_creation_operation_key",
		"WHERE creation_operation_key IS NOT NULL",
		"salon_settings_owner_manual_booking_mode_guard",
		"scheduling_authority <> 'owner_manual'",
		"booking_mode <> 'confirmed_booking'",
		"ERRCODE = '23514'",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V54 is missing onboarding safety contract %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"UPDATE salon_settings",
		"UPDATE salons",
		"SET scheduling_authority = 'owner_manual'",
		"SET scheduling_authority = 'external_provider'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V54 must not rewrite legacy salon state; found %q", forbidden)
		}
	}
}

func TestV54PreflightRejectsIncompatibleLegacyRowsWithoutPartialDDL(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v54_preflight_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 53)

	ownerID := uuid.New()
	salonID := uuid.New()
	seedV54OwnerAndSalon(t, ctx, tx, ownerID, salonID, "V54 incompatible")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, scheduling_authority, booking_mode)
		VALUES ($1, 'owner_manual', 'confirmed_booking')
	`, salonID); err != nil {
		t.Fatalf("seed incompatible V53 settings: %v", err)
	}

	expectV49PostgresError(t, ctx, tx, "23514", "salon_settings_owner_manual_booking_mode_guard", readV54(t))

	var v54ColumnCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'salons'
		  AND column_name IN ('creation_operation_key', 'creation_payload_fingerprint')
	`).Scan(&v54ColumnCount); err != nil {
		t.Fatalf("inspect V54 columns after failed preflight: %v", err)
	}
	if v54ColumnCount != 0 {
		t.Fatalf("failed V54 preflight left %d onboarding columns, want 0", v54ColumnCount)
	}
	var authority, bookingMode string
	if err := tx.QueryRowContext(ctx, `
		SELECT scheduling_authority, booking_mode FROM salon_settings WHERE salon_id=$1
	`, salonID).Scan(&authority, &bookingMode); err != nil {
		t.Fatalf("read incompatible legacy state after failed preflight: %v", err)
	}
	if authority != "owner_manual" || bookingMode != "confirmed_booking" {
		t.Fatalf("failed preflight rewrote legacy state to %q/%q", authority, bookingMode)
	}
}

func TestV54PreservesLegacyAuthorityAndEnforcesCreationAndModeInvariants(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v54_invariants_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 53)

	ownerID := uuid.New()
	otherOwnerID := uuid.New()
	legacySalonID := uuid.New()
	seedV54OwnerAndSalon(t, ctx, tx, ownerID, legacySalonID, "V54 legacy external")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id, scheduling_authority, booking_mode)
		VALUES ($1, 'external_provider', 'confirmed_booking')
	`, legacySalonID); err != nil {
		t.Fatalf("seed compatible legacy settings: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id,email,password_hash,full_name)
		VALUES ($1,$2,'hash','V54 Other Owner')
	`, otherOwnerID, "v54-other-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V54 second owner: %v", err)
	}

	if _, err := tx.ExecContext(ctx, readV54(t)); err != nil {
		t.Fatalf("apply V54: %v", err)
	}

	var authority, bookingMode string
	var operationKey, fingerprint sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority, settings.booking_mode,
		       salon.creation_operation_key, salon.creation_payload_fingerprint
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id=salon.id
		WHERE salon.id=$1
	`, legacySalonID).Scan(&authority, &bookingMode, &operationKey, &fingerprint); err != nil {
		t.Fatalf("read legacy salon after V54: %v", err)
	}
	if authority != "external_provider" || bookingMode != "confirmed_booking" || operationKey.Valid || fingerprint.Valid {
		t.Fatalf("V54 rewrote legacy state authority=%q mode=%q key=%v fingerprint=%v", authority, bookingMode, operationKey, fingerprint)
	}

	expectV49PostgresError(t, ctx, tx, "23514", "salon_settings_owner_manual_booking_mode_guard", `
		UPDATE salon_settings SET scheduling_authority='owner_manual' WHERE salon_id=$1
	`, legacySalonID)

	validFingerprint := strings.Repeat("a", 64)
	createdSalonID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (
			id,name,phone,owner_user_id,creation_operation_key,creation_payload_fingerprint
		) VALUES ($1,'V54 created','5555401',$2,'create-one',$3)
	`, createdSalonID, ownerID, validFingerprint); err != nil {
		t.Fatalf("insert valid V54 creation proof: %v", err)
	}
	expectV49PostgresError(t, ctx, tx, "23505", "salons_owner_creation_operation_key", `
		INSERT INTO salons (
			id,name,phone,owner_user_id,creation_operation_key,creation_payload_fingerprint
		) VALUES ($1,'V54 duplicate','5555402',$2,'create-one',$3)
	`, uuid.New(), ownerID, strings.Repeat("b", 64))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (
			id,name,phone,owner_user_id,creation_operation_key,creation_payload_fingerprint
		) VALUES ($1,'V54 other tenant','5555403',$2,'create-one',$3)
	`, uuid.New(), otherOwnerID, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("same operation key for another owner: %v", err)
	}
	expectV49PostgresError(t, ctx, tx, "23514", "salons_creation_proof_pair_check", `
		INSERT INTO salons (id,name,phone,owner_user_id,creation_operation_key)
		VALUES ($1,'V54 missing proof','5555404',$2,'missing-proof')
	`, uuid.New(), ownerID)
	expectV49PostgresError(t, ctx, tx, "23514", "salons_creation_operation_key_check", `
		INSERT INTO salons (
			id,name,phone,owner_user_id,creation_operation_key,creation_payload_fingerprint
		) VALUES ($1,'V54 spaced key','5555405',$2,' spaced-key ',$3)
	`, uuid.New(), ownerID, strings.Repeat("d", 64))
	expectV49PostgresError(t, ctx, tx, "23514", "salons_creation_payload_fingerprint_check", `
		INSERT INTO salons (
			id,name,phone,owner_user_id,creation_operation_key,creation_payload_fingerprint
		) VALUES ($1,'V54 bad proof','5555406',$2,'bad-proof','not-a-sha256')
	`, uuid.New(), ownerID)
}

func seedV54OwnerAndSalon(t *testing.T, ctx context.Context, tx *sql.Tx, ownerID uuid.UUID, salonID uuid.UUID, salonName string) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id,email,password_hash,full_name)
		VALUES ($1,$2,'hash','V54 Owner')
	`, ownerID, "v54-owner-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V54 owner: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (id,name,phone,owner_user_id)
		VALUES ($1,$2,'5555400',$3)
	`, salonID, salonName, ownerID); err != nil {
		t.Fatalf("seed V54 salon: %v", err)
	}
}
