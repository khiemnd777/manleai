package migrations

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestV87SquareScopeBoundCapabilitySafety(t *testing.T) {
	raw, err := Files.ReadFile("V87__square_scope_bound_booking_capability.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"booking_write_capability_version",
		"square_oauth_scope_fingerprint",
		"connection_capability_version",
		"provider_api_version",
		"oauth_scope_fingerprint",
		"buyer_write",
		"seller_write",
		"square-buyer-single-create-v1",
		"external_provider_scheduling_capability_actions",
		"external_provider_capability_history_immutable_guard",
		"technical.write",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V87 missing safety contract %q", token)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE", "DROP COLUMN", "customer_phone", "customer_name",
		"redis", "manicure", "pedicure",
	} {
		if strings.Contains(strings.ToLower(source), strings.ToLower(forbidden)) {
			t.Fatalf("V87 contains forbidden destructive, secret, or product-specific token %q", forbidden)
		}
	}
}

func TestV87ScopeFingerprintVersionFenceAndTenantEvidence(t *testing.T) {
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
	salonID := uuid.New()
	otherSalonID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users(id,email,password_hash,full_name) VALUES($1,$2,'v87-test','V87 owner')
	`, ownerID, "v87-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM salons WHERE id IN ($1,$2)`, salonID, otherSalonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, ownerID)
	}()
	for _, id := range []uuid.UUID{salonID, otherSalonID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO salons(id,name,phone,owner_user_id) VALUES($1,'V87 test',$2,$3)`, id, "+1312"+strings.ReplaceAll(uuid.NewString(), "-", "")[:7], ownerID); err != nil {
			t.Fatal(err)
		}
	}

	var firstFingerprint, reorderedFingerprint string
	if err := db.QueryRowContext(ctx, `SELECT public.square_oauth_scope_fingerprint(ARRAY[' appointments_write ','CUSTOMERS_READ','APPOINTMENTS_WRITE'])`).Scan(&firstFingerprint); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT public.square_oauth_scope_fingerprint(ARRAY['customers_read','APPOINTMENTS_WRITE'])`).Scan(&reorderedFingerprint); err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != reorderedFingerprint || len(firstFingerprint) != 64 {
		t.Fatalf("normalized fingerprints differ: %q %q", firstFingerprint, reorderedFingerprint)
	}

	connectionID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections(id,salon_id,provider,status,location_id,scopes)
		VALUES($1,$2,'square','active','location-v87',ARRAY['APPOINTMENTS_WRITE'])
	`, connectionID, salonID); err != nil {
		t.Fatal(err)
	}
	var versionBefore, versionAfter int64
	if err := db.QueryRowContext(ctx, `SELECT booking_write_capability_version FROM pos_connections WHERE id=$1`, connectionID).Scan(&versionBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE pos_connections SET scopes=ARRAY['APPOINTMENTS_ALL_WRITE'] WHERE id=$1`, connectionID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT booking_write_capability_version FROM pos_connections WHERE id=$1`, connectionID).Scan(&versionAfter); err != nil {
		t.Fatal(err)
	}
	if versionAfter != versionBefore+1 {
		t.Fatalf("connection capability version %d -> %d, want +1", versionBefore, versionAfter)
	}

	configID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings) VALUES($1,$2,'square',true,'{"api_version":"2026-05-20"}')`, configID, salonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version) VALUES($1,'integration_config','square',1) ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET version=EXCLUDED.version`, salonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO external_provider_scheduling_capability_evidence(
			salon_id,integration_config_id,provider,provider_location_id,config_version,
			verification_contract_version,verification_source,verified_at,expires_at,
			connection_id,connection_capability_version,provider_api_version,
			oauth_scope_fingerprint,write_permission_mode,reviewer_user_id,action_key
		) VALUES($1,$2,'square','location-v87',1,'square-buyer-single-create-v1','provider_contract',now(),now()+interval '1 hour',$3,$4,'2026-05-20',$5,'seller_write',$6,$7)
	`, otherSalonID, configID, connectionID, versionAfter, reorderedFingerprint, ownerID, "cross-tenant-"+uuid.NewString()); err == nil {
		t.Fatal("cross-tenant capability evidence unexpectedly succeeded")
	}
}
