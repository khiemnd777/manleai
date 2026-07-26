package migrations

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestV55ConversationPendingApprovalTargetShape(t *testing.T) {
	payload, err := os.ReadFile("V55__conversation_pending_approval_targets.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(payload)
	for _, required := range []string{
		"DROP CONSTRAINT scheduling_requests_operation_target_check",
		"ADD CONSTRAINT scheduling_requests_operation_target_check",
		"operation_type = 'book'",
		"target_appointment_id IS NULL",
		"target_description IS NULL",
		"target_scheduling_authority IS NOT NULL",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("V55 missing %q", required)
		}
	}
	if strings.Contains(sql, "UPDATE scheduling_requests") {
		t.Fatal("V55 must not backfill immutable legacy request targets")
	}
}

func TestV55AppliesOnPostgresAndPreservesLegacyNullBookTarget(t *testing.T) {
	db, ctx, tx := beginV49PostgresTest(t, "v55_pending_target_")
	defer db.Close()
	defer tx.Rollback()
	applyV49MigrationChain(t, ctx, tx, 54)

	ownerID := uuid.New()
	salonID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id,email,password_hash,full_name)
		VALUES ($1,$2,'hash','V55 Owner')
	`, ownerID, "v55-owner-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("seed V55 owner: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (id,name,phone,timezone,owner_user_id)
		VALUES ($1,'V55 Salon','5555500','America/Chicago',$2)
	`, salonID, ownerID); err != nil {
		t.Fatalf("seed V55 salon: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salon_settings (salon_id,scheduling_authority,booking_mode)
		VALUES ($1,'external_provider','pending_approval')
	`, salonID); err != nil {
		t.Fatalf("seed V55 settings: %v", err)
	}

	insertBook := `
		INSERT INTO scheduling_requests (
			salon_id,scheduling_authority,operation_key,request_fingerprint,
			operation_type,source,status,version,target_scheduling_authority,
			customer_name,customer_phone,requested_timezone,party_size
		) VALUES ($1,'owner_manual',$2,$3,'book','ai_voice_call','pending',1,$4,
		          'V55 Customer','5555501','America/Chicago',1)
		RETURNING id
	`
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_requests_operation_target_check", insertBook,
		salonID, "pre-v55-target", strings.Repeat("a", 64), "external_provider")

	var legacyID uuid.UUID
	if err := tx.QueryRowContext(ctx, insertBook, salonID, "legacy-null-target", strings.Repeat("b", 64), nil).Scan(&legacyID); err != nil {
		t.Fatalf("insert V54 legacy NULL target: %v", err)
	}
	if _, err := tx.ExecContext(ctx, readV55(t)); err != nil {
		t.Fatalf("apply V55: %v", err)
	}

	var targetedID uuid.UUID
	if err := tx.QueryRowContext(ctx, insertBook, salonID, "v55-external-target", strings.Repeat("c", 64), "external_provider").Scan(&targetedID); err != nil {
		t.Fatalf("insert V55 targeted pending book request: %v", err)
	}
	var legacyTarget sql.NullString
	var target string
	if err := tx.QueryRowContext(ctx, `SELECT target_scheduling_authority FROM scheduling_requests WHERE id=$1`, legacyID).Scan(&legacyTarget); err != nil {
		t.Fatalf("load V55 legacy target: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT target_scheduling_authority FROM scheduling_requests WHERE id=$1`, targetedID).Scan(&target); err != nil {
		t.Fatalf("load V55 pending target: %v", err)
	}
	if legacyTarget.Valid || target != "external_provider" {
		t.Fatalf("V55 legacy/target authority = %v/%q", legacyTarget, target)
	}
	expectV49PostgresError(t, ctx, tx, "23514", "scheduling_requests_operation_target_check", `
		INSERT INTO scheduling_requests (
			salon_id,scheduling_authority,operation_key,request_fingerprint,
			operation_type,source,status,version,target_appointment_id,target_scheduling_authority,
			customer_name,customer_phone,requested_timezone,party_size
		) VALUES ($1,'owner_manual','v55-book-with-appointment',$2,'book','ai_voice_call','pending',1,
		          gen_random_uuid(),'external_provider','V55 Customer','5555501','America/Chicago',1)
	`, salonID, strings.Repeat("d", 64))
}

func readV55(t *testing.T) string {
	t.Helper()
	source, err := Files.ReadFile("V55__conversation_pending_approval_targets.sql")
	if err != nil {
		t.Fatalf("read V55 migration: %v", err)
	}
	return string(source)
}
