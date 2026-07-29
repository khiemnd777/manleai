package migrations

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestV82ConversationAnswerContextCollectionCascadeSafety(t *testing.T) {
	raw, err := Files.ReadFile("V82__conversation_answer_context_collection_cascade_safety.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"CREATE OR REPLACE FUNCTION phase13_bump_answer_context_collection_version()",
		"EXISTS (SELECT 1 FROM salons WHERE id = old_salon_id)",
		"EXISTS (SELECT 1 FROM salons WHERE id = new_salon_id)",
		"ON CONFLICT (salon_id, resource_type, resource_id)",
		"version = business_resource_versions.version + 1",
		"IF TG_OP = 'DELETE'",
		"RETURN OLD",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V82 missing cascade-safety token %q", token)
		}
	}
	for _, forbidden := range []string{
		"DROP TRIGGER",
		"DROP FUNCTION",
		"ALTER TABLE business_resource_versions",
		"DISABLE TRIGGER",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("V82 contains destructive or fence-weakening token %q", forbidden)
		}
	}
}

func TestV82PostgresPreservesChildDeleteBumpsAndAllowsSalonCascade(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	ownerA := uuid.New()
	ownerB := uuid.New()
	salonA := uuid.New()
	salonB := uuid.New()
	suffix := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, full_name)
		VALUES
			($1, $3, 'v82-test', 'V82 Owner A'),
			($2, $4, 'v82-test', 'V82 Owner B')
	`, ownerA, ownerB, "v82-a-"+suffix+"@example.test", "v82-b-"+suffix+"@example.test"); err != nil {
		t.Fatalf("insert owners: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO salons (id, name, phone, owner_user_id)
		VALUES
			($1, 'V82 Cascade Salon A', $3, $5),
			($2, 'V82 Cascade Salon B', $4, $6)
	`, salonA, salonB, "+1312555"+suffix[:4]+"01", "+1312555"+suffix[:4]+"02", ownerA, ownerB); err != nil {
		t.Fatalf("insert salons: %v", err)
	}

	serviceA := uuid.New()
	staffA := uuid.New()
	serviceB := uuid.New()
	staffB := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO services (
			id, salon_id, pos_provider, pos_service_id, name,
			duration_minutes, ai_bookable, active, source, sync_status
		) VALUES
			($1, $3, 'square', $5, 'V82 Service A', 45, true, true, 'local', 'local_only'),
			($2, $4, 'square', $6, 'V82 Service B', 50, true, true, 'local', 'local_only')
	`, serviceA, serviceB, salonA, salonB,
		"v82-service-a-"+suffix, "v82-service-b-"+suffix); err != nil {
		t.Fatalf("insert canonical services: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staff (
			id, salon_id, pos_provider, pos_staff_id, name,
			ai_bookable, active, source, sync_status
		) VALUES
			($1, $3, 'square', $5, 'V82 Staff A', true, true, 'local', 'local_only'),
			($2, $4, 'square', $6, 'V82 Staff B', true, true, 'local', 'local_only')
	`, staffA, staffB, salonA, salonB,
		"v82-staff-a-"+suffix, "v82-staff-b-"+suffix); err != nil {
		t.Fatalf("insert canonical staff: %v", err)
	}

	serviceVersionBeforeDelete := v82CollectionVersion(t, ctx, tx, salonA, "service")
	staffVersionBeforeDelete := v82CollectionVersion(t, ctx, tx, salonA, "staff")
	if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE id = $1`, serviceA); err != nil {
		t.Fatalf("delete service explicitly: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM staff WHERE id = $1`, staffA); err != nil {
		t.Fatalf("delete staff explicitly: %v", err)
	}
	if got := v82CollectionVersion(t, ctx, tx, salonA, "service"); got != serviceVersionBeforeDelete+1 {
		t.Fatalf("service collection version after explicit delete=%d, want %d", got, serviceVersionBeforeDelete+1)
	}
	if got := v82CollectionVersion(t, ctx, tx, salonA, "staff"); got != staffVersionBeforeDelete+1 {
		t.Fatalf("staff collection version after explicit delete=%d, want %d", got, staffVersionBeforeDelete+1)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name,
			duration_minutes, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'V82 Cascade Service', 60, true, true, 'local', 'local_only')
	`, salonA, "v82-cascade-service-"+suffix); err != nil {
		t.Fatalf("insert cascade-owned service: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name,
			ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'V82 Cascade Staff', true, true, 'local', 'local_only')
	`, salonA, "v82-cascade-staff-"+suffix); err != nil {
		t.Fatalf("insert cascade-owned staff: %v", err)
	}
	tenantBServiceVersion := v82CollectionVersion(t, ctx, tx, salonB, "service")
	tenantBStaffVersion := v82CollectionVersion(t, ctx, tx, salonB, "staff")

	if _, err := tx.ExecContext(ctx, `DELETE FROM salons WHERE id = $1`, salonA); err != nil {
		t.Fatalf("delete salon with canonical children: %v", err)
	}
	var salonRows, versionRows int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM salons WHERE id = $1`, salonA).Scan(&salonRows); err != nil {
		t.Fatalf("count deleted salon: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM business_resource_versions WHERE salon_id = $1`, salonA).Scan(&versionRows); err != nil {
		t.Fatalf("count deleted salon resource versions: %v", err)
	}
	if salonRows != 0 || versionRows != 0 {
		t.Fatalf("cascade left salon/version rows=%d/%d, want 0/0", salonRows, versionRows)
	}
	if got := v82CollectionVersion(t, ctx, tx, salonB, "service"); got != tenantBServiceVersion {
		t.Fatalf("tenant B service version changed during tenant A cascade: before=%d after=%d", tenantBServiceVersion, got)
	}
	if got := v82CollectionVersion(t, ctx, tx, salonB, "staff"); got != tenantBStaffVersion {
		t.Fatalf("tenant B staff version changed during tenant A cascade: before=%d after=%d", tenantBStaffVersion, got)
	}
}

func v82CollectionVersion(t *testing.T, ctx context.Context, tx *sql.Tx, salonID uuid.UUID, resourceType string) int64 {
	t.Helper()
	var version int64
	if err := tx.QueryRowContext(ctx, `
		SELECT version
		FROM business_resource_versions
		WHERE salon_id = $1 AND resource_type = $2 AND resource_id = 'collection'
	`, salonID, resourceType).Scan(&version); err != nil {
		t.Fatalf("load %s collection version for salon %s: %v", resourceType, salonID, err)
	}
	return version
}
