package migrations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestV86ExternalAtomicSlotCommitSafety(t *testing.T) {
	raw, err := Files.ReadFile("V86__external_atomic_slot_commit.sql")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, token := range []string{
		"external_provider_scheduling_capability_evidence",
		"external_slot_claims",
		"external_slot_claim_intervals",
		"external_slot_claim_events",
		"claim_conflict",
		"external_slot_claim_required",
		"replaces_claim_id",
		"activation_pending",
		"external_slot_claim_intervals_no_overlap",
		"tstzrange(occupied_start_time, occupied_end_time, '[)')",
		"WHERE (released_at IS NULL AND activation_pending = false)",
		"external_slot_claim_release_graph_guard",
		"external_slot_claim_events_immutable_guard",
		"external_provider_capability_config_tenant_fk",
		"external_slot_claims_attempt_tenant_fk",
		"external-slot-commit-v1",
		"atomic_create_no_overlap",
		"atomic_reschedule_no_overlap",
		"atomic_party_create",
		"app_rls_salon_write_allowed(salon_id, ''appointments'')",
		"app_rls_feature_access(salon_id, 'technical.write', NULL)",
	} {
		if !strings.Contains(source, token) {
			t.Fatalf("V86 missing safety contract %q", token)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE",
		"DROP COLUMN",
		"CREATE TABLE external_calendar",
		"customer_phone",
		"customer_name",
		"transcript",
		"pedicure",
		"manicure",
	} {
		if strings.Contains(strings.ToLower(source), strings.ToLower(forbidden)) {
			t.Fatalf("V86 contains forbidden product or destructive token %q", forbidden)
		}
	}
}

func TestV86ConcurrentExternalClaimsAllowExactlyOneWinner(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED") == "1" {
			t.Fatal("TEST_DATABASE_URL is required in release-gate mode")
		}
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	setupTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin setup: %v", err)
	}
	ownerID := uuid.New()
	salonID := uuid.New()
	serviceID := uuid.New()
	staffID := uuid.New()
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO users(id,email,password_hash,full_name)
		VALUES($1,$2,'v86-test','V86 concurrent owner')
	`, ownerID, "v86-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO salons(id,name,phone,owner_user_id)
		VALUES($1,'V86 Concurrent Salon',$2,$3)
	`, salonID, "+1312555"+strings.ReplaceAll(uuid.NewString(), "-", "")[:4], ownerID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO services(id,salon_id,pos_provider,pos_service_id,name,duration_minutes,ai_bookable,active,source,sync_status)
		VALUES($1,$2,'square',$3,'V86 service',60,true,true,'local','local_only')
	`, serviceID, salonID, "v86-service-"+uuid.NewString()); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO staff(id,salon_id,pos_provider,pos_staff_id,name,ai_bookable,active,source,sync_status)
		VALUES($1,$2,'square',$3,'V86 staff',true,true,'local','local_only')
	`, staffID, salonID, "v86-staff-"+uuid.NewString()); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	configID := uuid.New()
	evidenceID := uuid.New()
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings)
		VALUES($1,$2,'square',true,'{}'::jsonb)
	`, configID, salonID); err != nil {
		t.Fatalf("insert config: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version)
		VALUES($1,'integration_config','square',1)
		ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET version=EXCLUDED.version
	`, salonID); err != nil {
		t.Fatalf("insert config version: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
		INSERT INTO external_provider_scheduling_capability_evidence(
			id,salon_id,integration_config_id,provider,provider_location_id,config_version,
			verification_contract_version,verification_source,atomic_create_no_overlap,
			atomic_reschedule_no_overlap,concrete_staff_assignment,verified_at,expires_at
		) VALUES($1,$2,$3,'square','location-v86',1,'external-slot-commit-v1',
		         'provider_contract',true,true,true,now()-interval '1 minute',now()+interval '1 hour')
	`, evidenceID, salonID, configID); err != nil {
		t.Fatalf("insert capability evidence: %v", err)
	}
	attemptIDs := []uuid.UUID{uuid.New(), uuid.New()}
	startTime := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	for index, attemptID := range attemptIDs {
		if _, err := setupTx.ExecContext(ctx, `
			INSERT INTO booking_attempts(
				id,salon_id,source,status,pos_provider,pos_idempotency_key,operation_key,
				request_fingerprint,operation_type,processing_token,processing_lease_expires_at,
				provider_outcome,retry_policy,reconciliation_status,customer_name,customer_phone,
				service_id,staff_id,staff_selection_mode,requested_start_time,requested_end_time,
				provider_location_id,provider_snapshot_generation,scheduling_authority,
				authority_provider,authority_idempotency_key,authority_location_id,
				authority_snapshot_generation,external_slot_claim_required
			) VALUES($1,$2,'test','pos_pending','square',$3,$4,$5,'book',$6,now()+interval '5 minutes',
			         'not_started','none','not_required','Concurrent caller','5558600',$7,$8,
			         'specific',$9,$10,'location-v86',1,'external_provider','square',$3,
			         'location-v86',1,true)
		`, attemptID, salonID, "provider-key-"+attemptID.String(),
			"operation-"+attemptID.String(), strings.Repeat(string(rune('a'+index)), 64),
			"token-"+attemptID.String(), serviceID, staffID,
			startTime, startTime.Add(time.Hour)); err != nil {
			t.Fatalf("insert attempt %d: %v", index, err)
		}
	}
	if err := setupTx.Commit(); err != nil {
		t.Fatalf("commit setup: %v", err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM external_slot_claim_intervals WHERE salon_id=$1`, salonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM external_slot_claim_events WHERE salon_id=$1`, salonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM external_slot_claims WHERE salon_id=$1`, salonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM external_provider_scheduling_capability_evidence WHERE salon_id=$1`, salonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, ownerID)
	}()

	type claimResult struct{ err error }
	results := make(chan claimResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	startGate := make(chan struct{})
	for _, attemptID := range attemptIDs {
		go func(attemptID uuid.UUID) {
			tx, beginErr := db.BeginTx(ctx, nil)
			if beginErr != nil {
				results <- claimResult{beginErr}
				return
			}
			defer tx.Rollback()
			claimID := uuid.New()
			if _, insertErr := tx.ExecContext(ctx, `
				INSERT INTO external_slot_claims(
					id,salon_id,booking_attempt_id,provider,provider_location_id,operation_type,
					state,provider_capability_evidence_id,provider_config_version,
					processing_token,lease_expires_at
				) VALUES($1,$2,$3,'square','location-v86','book','claimed_pre_dispatch',$4,1,$5,now()+interval '5 minutes')
			`, claimID, salonID, attemptID, evidenceID, "token-"+attemptID.String()); insertErr != nil {
				results <- claimResult{insertErr}
				return
			}
			ready.Done()
			<-startGate
			if _, lockErr := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "external-slot-claim:"+salonID.String()+":square:location-v86:staff:"+staffID.String()); lockErr != nil {
				results <- claimResult{lockErr}
				return
			}
			if _, insertErr := tx.ExecContext(ctx, `
				INSERT INTO external_slot_claim_intervals(
					salon_id,claim_id,provider,provider_location_id,resource_kind,resource_id,
					source_segment_indexes,occupied_start_time,occupied_end_time
				) VALUES($1,$2,'square','location-v86','staff',$3,ARRAY[1],$4,$5)
			`, salonID, claimID, staffID.String(), startTime, startTime.Add(time.Hour)); insertErr != nil {
				results <- claimResult{insertErr}
				return
			}
			results <- claimResult{tx.Commit()}
		}(attemptID)
	}
	ready.Wait()
	close(startGate)

	successes := 0
	conflicts := 0
	for range 2 {
		item := <-results
		if item.err == nil {
			successes++
			continue
		}
		var pqErr *pq.Error
		if errors.As(item.err, &pqErr) && string(pqErr.Code) == "23P01" && pqErr.Constraint == "external_slot_claim_intervals_no_overlap" {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent result: %v", item.err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes success=%d conflict=%d, want 1/1", successes, conflicts)
	}
}
