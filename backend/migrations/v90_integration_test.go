package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

const v90ClaimWorkers = 12

func TestV90WorkerClaimFunctionsHaveOneWinnerUnderContention(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(v90ClaimWorkers + 4)
	ctx := context.Background()

	t.Run("pos sync", func(t *testing.T) {
		_, salonID := seedV90Salon(t, ctx, db, "pos")
		var jobID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO pos_sync_jobs(salon_id,provider,entity_type,entity_id,operation)
			VALUES($1,'square','service',$2,'upsert_service') RETURNING id::text
		`, salonID, uuid.NewString()).Scan(&jobID); err != nil {
			t.Fatalf("insert POS sync job: %v", err)
		}
		claims := runV90ConcurrentClaims(t, db, `SELECT job_id::text FROM public.app_worker_claim_pos_sync_jobs(1)`)
		assertV90SingleClaim(t, claims, jobID)
		var status string
		var attempts int
		if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM pos_sync_jobs WHERE id=$1`, jobID).Scan(&status, &attempts); err != nil || status != "running" || attempts != 1 {
			t.Fatalf("POS sync state=%q/%d err=%v", status, attempts, err)
		}
	})

	t.Run("square booking webhook", func(t *testing.T) {
		_, salonID := seedV90Salon(t, ctx, db, "square-webhook")
		var webhookID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO square_booking_webhook_events(
				salon_id,event_id,event_type,merchant_id,location_id,pos_booking_id
			) VALUES($1,$2,'booking.updated',$3,$4,$5) RETURNING id::text
		`, salonID, "EVENT_"+uuid.NewString(), "merchant-"+uuid.NewString(), "location-"+uuid.NewString(), "booking-"+uuid.NewString()).Scan(&webhookID); err != nil {
			t.Fatalf("insert Square webhook: %v", err)
		}
		claims := runV90ConcurrentClaims(t, db, `SELECT webhook_id::text FROM public.app_worker_claim_square_booking_webhooks(1,10)`)
		assertV90SingleClaim(t, claims, webhookID)
		var status string
		var attempts int
		if err := db.QueryRowContext(ctx, `SELECT processing_status,processing_attempts FROM square_booking_webhook_events WHERE id=$1`, webhookID).Scan(&status, &attempts); err != nil || status != "processing" || attempts != 1 {
			t.Fatalf("Square webhook state=%q/%d err=%v", status, attempts, err)
		}
	})

	t.Run("owner notification", func(t *testing.T) {
		_, salonID := seedV90Salon(t, ctx, db, "owner-notification")
		var notificationID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO owner_notifications(
				salon_id,type,title,message,dedupe_key,payload,delivery_status,next_delivery_at
			) VALUES($1,'v90_claim_test','Owner review','Owner review is waiting.',$2,'{}'::jsonb,'queued',now())
			RETURNING id::text
		`, salonID, "v90-owner-"+uuid.NewString()).Scan(&notificationID); err != nil {
			t.Fatalf("insert owner notification: %v", err)
		}
		claims := runV90ConcurrentClaims(t, db, `SELECT notification_id::text FROM public.app_worker_claim_owner_notifications(5,1,300000)`)
		assertV90SingleClaim(t, claims, notificationID)
		assertV90DeliveryEvidence(t, ctx, db,
			`SELECT delivery_status,delivery_attempts FROM owner_notifications WHERE id=$1`,
			`SELECT count(*) FROM owner_notification_delivery_attempts WHERE owner_notification_id=$1`,
			`SELECT count(*) FROM owner_notification_delivery_events WHERE owner_notification_id=$1 AND event_type='claimed'`,
			notificationID,
		)
	})

	t.Run("customer notification", func(t *testing.T) {
		ownerID, salonID := seedV90Salon(t, ctx, db, "customer-notification")
		destination := newV90CustomerDestination()
		var consentID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO customer_sms_consents(
				salon_id,normalized_destination,destination_masked,status,source,evidence_type,
				evidence_reference,actor_user_id,consented_at
			) VALUES($1,$2,'••••' || right($2,4),'consented','owner_attested','v90_test',$3,$4,now())
			RETURNING id::text
		`, salonID, destination, "v90-consent-"+uuid.NewString(), ownerID).Scan(&consentID); err != nil {
			t.Fatalf("insert customer consent: %v", err)
		}
		var requestID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO scheduling_requests(
				salon_id,scheduling_authority,operation_key,request_fingerprint,operation_type,source,
				status,version,customer_name,customer_phone,requested_timezone,party_size,
				requested_start_time,requested_end_time
			) VALUES($1,'owner_manual',$2,$3,'book','v90_claim_test','pending',1,
			         'Customer',$4,'America/Chicago',1,now()+interval '1 day',now()+interval '1 day 1 hour')
			RETURNING id::text
		`, salonID, "v90-request-"+uuid.NewString(), strings.Repeat("9", 64), destination).Scan(&requestID); err != nil {
			t.Fatalf("insert scheduling request: %v", err)
		}
		var deliveryID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO customer_notification_deliveries(
				salon_id,customer_sms_consent_id,scheduling_request_id,notification_type,source_version,
				dedupe_key,message_body,destination_e164,destination_masked,destination_hash,
				consent_version,policy_version,delivery_status,next_delivery_at
			) VALUES($1,$2,$3,'request_received',1,$4,'Request received.',$5,'••••' || right($5,4),$6,1,1,'queued',now())
			RETURNING id::text
		`, salonID, consentID, requestID, "v90-customer-"+uuid.NewString(), destination, strings.Repeat("a", 64)).Scan(&deliveryID); err != nil {
			t.Fatalf("insert customer notification: %v", err)
		}
		claims := runV90ConcurrentClaims(t, db, `SELECT delivery_id::text FROM public.app_worker_claim_customer_notifications(5,1,300000)`)
		assertV90SingleClaim(t, claims, deliveryID)
		assertV90DeliveryEvidence(t, ctx, db,
			`SELECT delivery_status,delivery_attempts FROM customer_notification_deliveries WHERE id=$1`,
			`SELECT count(*) FROM customer_notification_delivery_attempts WHERE customer_notification_delivery_id=$1`,
			`SELECT count(*) FROM customer_notification_delivery_events WHERE customer_notification_delivery_id=$1 AND event_type='claimed'`,
			deliveryID,
		)
	})

	t.Run("openai runtime verification", func(t *testing.T) {
		ownerID, salonID := seedV90Salon(t, ctx, db, "openai-verification")
		var configID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO salon_integration_configs(
				salon_id,provider,enabled,settings,credential_fingerprint_hmac,credential_revision,destination_profile
			) VALUES($1,'openai',true,'{}'::jsonb,$2,1,'openai_public') RETURNING id::text
		`, salonID, strings.Repeat("b", 63)+"c").Scan(&configID); err != nil {
			t.Fatalf("insert OpenAI config: %v", err)
		}
		var runID string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO openai_runtime_verification_runs(
				salon_id,integration_config_id,actor_user_id,action_key,request_fingerprint,
				config_version,credential_revision,destination_policy_version,verification_contract_version
			) VALUES($1,$2,$3,$4,$5,1,1,'openai-public-v1','openai-voice-v1') RETURNING id::text
		`, salonID, configID, ownerID, "v90-openai-"+uuid.NewString(), strings.Repeat("c", 64)).Scan(&runID); err != nil {
			t.Fatalf("insert OpenAI verification: %v", err)
		}
		claims := runV90ConcurrentClaims(t, db, `SELECT run_id::text FROM public.app_worker_claim_openai_runtime_verifications(1,300000)`)
		assertV90SingleClaim(t, claims, runID)
		var status string
		var attempts int
		if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM openai_runtime_verification_runs WHERE id=$1`, runID).Scan(&status, &attempts); err != nil || status != "claimed" || attempts != 1 {
			t.Fatalf("OpenAI verification state=%q/%d err=%v", status, attempts, err)
		}
	})
}

func TestV90CustomerDestinationFixtureIsAlwaysE164(t *testing.T) {
	e164 := regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	destinations := make(map[string]struct{})
	for index := 0; index < 512; index++ {
		destination := newV90CustomerDestination()
		if !e164.MatchString(destination) {
			t.Fatalf("generated destination %q is not E.164", destination)
		}
		destinations[destination] = struct{}{}
	}
	if len(destinations) < 2 {
		t.Fatal("generated destination fixture did not vary")
	}
}

func newV90CustomerDestination() string {
	id := uuid.New()
	numericSuffix := (int(id[0])<<8 | int(id[1])) % 10000
	return fmt.Sprintf("+1312555%04d", numericSuffix)
}

func seedV90Salon(t *testing.T, ctx context.Context, db *sql.DB, label string) (ownerID, salonID string) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'v90-test','V90 claim test') RETURNING id::text
	`, "v90-"+label+"-"+suffix+"@example.test").Scan(&ownerID); err != nil {
		t.Fatalf("insert %s owner: %v", label, err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,owner_user_id)
		VALUES($1,$2,$3) RETURNING id::text
	`, "V90 "+label, "+1312"+suffix[0:7], ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert %s salon: %v", label, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM openai_runtime_verification_runs WHERE salon_id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salon_integration_configs WHERE salon_id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	})
	return ownerID, salonID
}

func runV90ConcurrentClaims(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(v90ClaimWorkers)
	workers := sync.WaitGroup{}
	workers.Add(v90ClaimWorkers)
	claims := make(chan string, v90ClaimWorkers)
	errorsCh := make(chan error, v90ClaimWorkers)
	for range v90ClaimWorkers {
		go func() {
			defer workers.Done()
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				errorsCh <- err
				ready.Done()
				return
			}
			defer tx.Rollback()
			if _, err := tx.Exec(`
				SELECT set_config('app.database_scope','worker',true),
				       set_config('app.system_salon_id','',true),
				       set_config('app.actor_user_id','',true)
			`); err != nil {
				errorsCh <- err
				ready.Done()
				return
			}
			ready.Done()
			<-start
			rows, err := tx.Query(query)
			if err != nil {
				errorsCh <- err
				return
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					errorsCh <- err
					return
				}
				claims <- id
			}
			if err := rows.Close(); err != nil {
				errorsCh <- err
				return
			}
			if err := tx.Commit(); err != nil {
				errorsCh <- err
			}
		}()
	}
	ready.Wait()
	close(start)
	workers.Wait()
	close(claims)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent claim: %v", err)
		}
	}
	var result []string
	for id := range claims {
		result = append(result, id)
	}
	return result
}

func assertV90SingleClaim(t *testing.T, claims []string, expectedID string) {
	t.Helper()
	if len(claims) != 1 || claims[0] != expectedID {
		t.Fatalf("claims=%v, want exactly %s", claims, expectedID)
	}
}

func assertV90DeliveryEvidence(t *testing.T, ctx context.Context, db *sql.DB, stateQuery, attemptQuery, eventQuery, deliveryID string) {
	t.Helper()
	var status string
	var attempts, attemptRows, eventRows int
	if err := db.QueryRowContext(ctx, stateQuery, deliveryID).Scan(&status, &attempts); err != nil {
		t.Fatalf("load delivery state: %v", err)
	}
	if err := db.QueryRowContext(ctx, attemptQuery, deliveryID).Scan(&attemptRows); err != nil {
		t.Fatalf("load delivery attempts: %v", err)
	}
	if err := db.QueryRowContext(ctx, eventQuery, deliveryID).Scan(&eventRows); err != nil {
		t.Fatalf("load delivery events: %v", err)
	}
	if status != "delivering" || attempts != 1 || attemptRows != 1 || eventRows != 1 {
		t.Fatalf("delivery state=%q attempts=%d attempt_rows=%d event_rows=%d", status, attempts, attemptRows, eventRows)
	}
}
