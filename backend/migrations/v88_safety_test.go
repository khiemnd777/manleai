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

func TestV88SquareStrictTenantBindingSafety(t *testing.T) {
	raw, err := Files.ReadFile("V88__square_strict_tenant_binding.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"pos_connections_provider_merchant_location_tenant_unique",
		"pos_connections_tenant_identity_normalized_check",
		"pos_adapter",
		"active_provider",
		"(NEW.id, 'service', 'collection', 1)",
		"(NEW.id, 'staff', 'collection', 1)",
		"btrim(salon.active_pos_provider) AS provider",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V88 missing tenant-binding contract %q", token)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "SQUARE_CLIENT_SECRET", "SQUARE_ACCESS_TOKEN"} {
		if strings.Contains(strings.ToUpper(source), forbidden) {
			t.Fatalf("V88 contains forbidden destructive or secret token %q", forbidden)
		}
	}
}

func TestV88ProviderIdentityIsUniqueAcrossTenantsAndBlankProviderIsExplicit(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED") == "1" {
			t.Fatal("TEST_DATABASE_URL is required in release-gate mode")
		}
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	ownerID := uuid.New()
	salonA := uuid.New()
	salonB := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,full_name) VALUES($1,$2,'v88-test','V88 owner')`, ownerID, "v88-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM salons WHERE id IN ($1,$2)`, salonA, salonB)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, ownerID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO salons(id,name,phone,owner_user_id,active_pos_provider) VALUES($1,'V88 A',$2,$3,'')`, salonA, "+1312"+strings.ReplaceAll(uuid.NewString(), "-", "")[:7], ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salons(id,name,phone,owner_user_id) VALUES($1,'V88 B',$2,$3)`, salonB, "+1312"+strings.ReplaceAll(uuid.NewString(), "-", "")[:7], ownerID); err != nil {
		t.Fatal(err)
	}
	var blankProvider, defaultProvider string
	if err := db.QueryRowContext(ctx, `SELECT active_pos_provider FROM salons WHERE id=$1`, salonA).Scan(&blankProvider); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT active_pos_provider FROM salons WHERE id=$1`, salonB).Scan(&defaultProvider); err != nil {
		t.Fatal(err)
	}
	if blankProvider != "" || defaultProvider != "square" {
		t.Fatalf("provider states blank=%q default=%q, want explicit blank and staged square default", blankProvider, defaultProvider)
	}
	for _, salonID := range []uuid.UUID{salonA, salonB} {
		var version int64
		if err := db.QueryRowContext(ctx, `SELECT version FROM technical_resource_versions WHERE salon_id=$1 AND resource_type='pos_adapter' AND resource_id='active_provider'`, salonID).Scan(&version); err != nil {
			t.Fatalf("active-provider version seed for %s: %v", salonID, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pos_connections(salon_id,provider,status,merchant_id,location_id) VALUES($1,'square','connected','merchant-v88','location-v88')`, salonA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pos_connections(salon_id,provider,status,merchant_id,location_id) VALUES($1,'square','connected','merchant-v88','location-v88')`, salonB); err == nil {
		t.Fatal("duplicate Square merchant/location unexpectedly crossed tenants")
	} else {
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Constraint != "pos_connections_provider_merchant_location_tenant_unique" {
			t.Fatalf("duplicate identity error=%v, want tenant unique constraint", err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO pos_connections(salon_id,provider,status,merchant_id,location_id) VALUES($1,'future_pos','connected',NULL,NULL)`, salonB); err != nil {
		t.Fatalf("incomplete provider identity should remain permitted: %v", err)
	}
}
