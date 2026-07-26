package schedulingretention

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
	_ "github.com/lib/pq"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
	"github.com/manleai/ai-receptionist/modules/pos"
	ownerreview "github.com/manleai/ai-receptionist/modules/scheduling_owner_manual"
)

type retentionFixture struct {
	ownerID   string
	salonID   string
	serviceID string
}

func TestRepositoryPostgresRetentionBoundaryConcurrencyAndIrreversibility(t *testing.T) {
	db := openRetentionTestDatabase(t)
	ctx := context.Background()
	fixture := seedRetentionFixture(t, ctx, db, "primary")
	other := seedRetentionFixture(t, ctx, db, "other")
	repo := NewRepository(db)
	processor := NewProcessor(repo)

	terminalAt := time.Now().UTC().Add(-91 * 24 * time.Hour).Truncate(time.Microsecond)
	requestID := seedSchedulingRequest(t, ctx, db, fixture, "resolved", terminalAt, "eligible", "guest-Lan", `{
		"customer_name":"Lan Nguyen",
		"note":"allergic to one product",
		"status":"resolved",
		"scheduling_authority":"owner_manual",
		"provider_booking_id":"provider-booking-safe-1"
	}`)
	ownerNotificationID := seedTerminalOwnerNotification(t, ctx, db, fixture.salonID, requestID, terminalAt)
	consentID, customerDeliveryID := seedTerminalCustomerNotification(t, ctx, db, fixture, requestID, terminalAt)

	var retentionDelta float64
	if err := db.QueryRowContext(ctx, `
		SELECT extract(epoch FROM (retention_expires_at-resolved_at))
		FROM scheduling_requests WHERE id=$1
	`, requestID).Scan(&retentionDelta); err != nil || retentionDelta != 90*24*60*60 {
		t.Fatalf("request retention boundary=%v err=%v", retentionDelta, err)
	}

	futureTerminal := time.Now().UTC().Add(-90*24*time.Hour + time.Hour).Truncate(time.Microsecond)
	futureRequestID := seedSchedulingRequest(t, ctx, db, fixture, "resolved", futureTerminal, "future", "guest-future", `{"customer_name":"Future Guest"}`)
	pendingRequestID := seedSchedulingRequest(t, ctx, db, fixture, "pending", time.Time{}, "pending", "guest-pending", `{"customer_name":"Pending Guest"}`)
	liveRequestID := seedSchedulingRequest(t, ctx, db, fixture, "resolved", terminalAt.Add(-time.Hour), "live-lease", "guest-live", `{"customer_name":"Live Guest"}`)
	seedLiveOwnerNotification(t, ctx, db, fixture.salonID, liveRequestID)
	otherPendingID := seedSchedulingRequest(t, ctx, db, other, "pending", time.Time{}, "other-pending", "guest-other", `{"customer_name":"Other Tenant Guest"}`)

	dueAudioID := seedRetentionAudio(t, ctx, db, fixture.salonID, terminalAt, []byte("expired customer speech"))
	futureAudioID := seedRetentionAudio(t, ctx, db, fixture.salonID, time.Now().UTC().Add(time.Hour), []byte("not expired"))

	var wg sync.WaitGroup
	processed := make(chan int, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, err := processor.ProcessOnce(ctx, 50)
			processed <- count
			errs <- err
		}()
	}
	wg.Wait()
	close(processed)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent processor: %v", err)
		}
	}
	totalProcessed := 0
	for count := range processed {
		totalProcessed += count
	}
	if totalProcessed < 4 {
		t.Fatalf("processed=%d, want request + owner + customer + audio", totalProcessed)
	}

	assertSchedulingAggregateRedacted(t, ctx, db, requestID)
	requestRead, err := ownerreview.NewRepository(db).Get(ctx, fixture.salonID, fixture.ownerID, requestID)
	if err != nil {
		t.Fatalf("read redacted scheduling request: %v", err)
	}
	if !requestRead.Redacted || requestRead.RedactedAt == nil || requestRead.RedactionVersion != PolicyVersion ||
		len(requestRead.Segments) != 1 || !requestRead.Segments[0].Redacted ||
		len(requestRead.Events) != 1 || !requestRead.Events[0].Redacted {
		t.Fatalf("redacted request DTO markers=%#v", requestRead)
	}
	replayedRead, err := ownerreview.NewRepository(db).Get(ctx, fixture.salonID, fixture.ownerID, requestID)
	if err != nil || string(replayedRead.Events[0].Payload) != string(requestRead.Events[0].Payload) {
		t.Fatalf("redacted read replay=%#v err=%v", replayedRead, err)
	}
	if _, err := ownerreview.NewRepository(db).Get(ctx, fixture.salonID, other.ownerID, requestID); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("cross-owner redacted request read error=%v", err)
	}

	ownerRead, err := notificationdelivery.NewRepository(db).GetForOwner(ctx, fixture.salonID, fixture.ownerID, ownerNotificationID)
	if err != nil || !ownerRead.Redacted || ownerRead.DestinationMasked != "" || ownerRead.CanRequeue {
		t.Fatalf("redacted owner delivery DTO=%#v err=%v", ownerRead, err)
	}
	if _, _, err := notificationdelivery.NewRepository(db).RequeueForOwner(
		ctx, fixture.salonID, fixture.ownerID, ownerNotificationID,
		"retention-requeue-"+uuid.NewString(), notificationdelivery.RequeueFingerprint(ownerNotificationID),
	); !errors.Is(err, notificationdelivery.ErrRequeueBlocked) {
		t.Fatalf("redacted owner delivery requeue error=%v", err)
	}

	var message string
	var destination, destinationHash sql.NullString
	var customerRedactedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT message_body,destination_e164,destination_hash,redacted_at
		FROM customer_notification_deliveries WHERE id=$1
	`, customerDeliveryID).Scan(&message, &destination, &destinationHash, &customerRedactedAt); err != nil {
		t.Fatalf("load redacted customer delivery: %v", err)
	}
	if message != "[redacted]" || destination.Valid || destinationHash.Valid || !customerRedactedAt.Valid {
		t.Fatalf("customer delivery redaction=%q/%v/%v/%v", message, destination, destinationHash, customerRedactedAt)
	}
	var consentStatus, consentDestination string
	if err := db.QueryRowContext(ctx, `SELECT status,normalized_destination FROM customer_sms_consents WHERE id=$1`, consentID).Scan(&consentStatus, &consentDestination); err != nil {
		t.Fatalf("load retained opt-out routing key: %v", err)
	}
	if consentStatus != "opted_out" || consentDestination != "+13125550111" {
		t.Fatalf("opt-out routing evidence=%q/%q", consentStatus, consentDestination)
	}

	assertRequestNotRedacted(t, ctx, db, futureRequestID, true)
	assertRequestNotRedacted(t, ctx, db, pendingRequestID, false)
	assertRequestNotRedacted(t, ctx, db, liveRequestID, true)
	assertRequestNotRedacted(t, ctx, db, otherPendingID, false)

	var dueBytes, futureBytes []byte
	var dueRedactedAt, futureRedactedAt sql.NullTime
	var provider, contentType string
	if err := db.QueryRowContext(ctx, `SELECT audio_data,redacted_at,provider,content_type FROM voice_audio_outputs WHERE id=$1`, dueAudioID).Scan(&dueBytes, &dueRedactedAt, &provider, &contentType); err != nil {
		t.Fatalf("load expired audio: %v", err)
	}
	if len(dueBytes) != 0 || !dueRedactedAt.Valid || provider != "openai" || contentType != "audio/mpeg" {
		t.Fatalf("expired audio redaction bytes=%d redacted=%v provider=%q content=%q", len(dueBytes), dueRedactedAt.Valid, provider, contentType)
	}
	if err := db.QueryRowContext(ctx, `SELECT audio_data,redacted_at FROM voice_audio_outputs WHERE id=$1`, futureAudioID).Scan(&futureBytes, &futureRedactedAt); err != nil {
		t.Fatalf("load future audio: %v", err)
	}
	if string(futureBytes) != "not expired" || futureRedactedAt.Valid {
		t.Fatalf("future audio changed bytes=%q redacted=%v", futureBytes, futureRedactedAt.Valid)
	}

	if count, err := processor.ProcessOnce(ctx, 50); err != nil {
		t.Fatalf("idempotent processor replay: %v", err)
	} else if count != 0 {
		t.Fatalf("idempotent processor replay processed=%d", count)
	}

	assertIrreversibleRedaction(t, ctx, db, requestID, ownerNotificationID, customerDeliveryID, dueAudioID)
	testAggregateRollback(t, ctx, db, repo, fixture, terminalAt.Add(-2*time.Hour))
}

func openRetentionTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return db
}

func seedRetentionFixture(t *testing.T, ctx context.Context, db *sql.DB, label string) retentionFixture {
	t.Helper()
	var fixture retentionFixture
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'retention-test',$2) RETURNING id::text
	`, "retention-"+label+"-"+uuid.NewString()+"@example.test", "Retention "+label).Scan(&fixture.ownerID); err != nil {
		t.Fatalf("insert retention owner: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,timezone,owner_user_id)
		VALUES($1,$2,'America/Chicago',$3) RETURNING id::text
	`, "Retention "+label, "+1312555"+time.Now().UTC().Format("0405"), fixture.ownerID).Scan(&fixture.salonID); err != nil {
		t.Fatalf("insert retention salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings(salon_id,scheduling_authority,booking_mode) VALUES($1,'owner_manual','pending_approval')`, fixture.salonID); err != nil {
		t.Fatalf("insert retention settings: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services(salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes,active,ai_bookable)
		VALUES($1,'square',$2,1,'Retention Manicure',45,true,true) RETURNING id::text
	`, fixture.salonID, "retention-service-"+uuid.NewString()).Scan(&fixture.serviceID); err != nil {
		t.Fatalf("insert retention service: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, fixture.salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, fixture.ownerID)
	})
	return fixture
}

func seedSchedulingRequest(t *testing.T, ctx context.Context, db *sql.DB, fixture retentionFixture, status string, terminalAt time.Time, suffix, guestReference, payload string) string {
	t.Helper()
	var resolvedAt any
	createdAt := time.Now().UTC().Add(-100 * 24 * time.Hour)
	updatedAt := createdAt
	if status == "resolved" {
		resolvedAt = terminalAt
		updatedAt = terminalAt
	}
	var requestID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO scheduling_requests(
			salon_id,scheduling_authority,operation_key,request_fingerprint,operation_type,source,
			status,version,customer_name,customer_phone,customer_email,requested_timezone,party_size,
			requested_start_time,requested_end_time,notes,resolution_reason,resolved_at,created_at,updated_at
		) VALUES(
			$1,'owner_manual',$2,$3,'book','owner_dashboard',$4,1,
			'Lan Nguyen','+13125550111','lan@example.test','America/Chicago',2,
			now()+interval '30 days',now()+interval '30 days 45 minutes','private party note',
			CASE WHEN $4='resolved' THEN 'owner completed follow-up' ELSE NULL END,$5,$6,$7
		) RETURNING id::text
	`, fixture.salonID, "retention-"+suffix+"-"+uuid.NewString(), strings.Repeat("a", 64), status, resolvedAt, createdAt, updatedAt).Scan(&requestID); err != nil {
		t.Fatalf("insert %s request: %v", suffix, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scheduling_request_segments(
			salon_id,scheduling_request_id,service_id,service_name,guest_reference,quantity,
			staff_selection_mode,duration_minutes,sort_order
		) VALUES($1,$2,$3,'Retention Manicure',$4,1,'anyone',45,1)
	`, fixture.salonID, requestID, fixture.serviceID, guestReference); err != nil {
		t.Fatalf("insert %s request segment: %v", suffix, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scheduling_request_events(
			salon_id,scheduling_request_id,action_key,action_fingerprint,event_type,request_version,payload,created_at
		) VALUES($1,$2,$3,$4,'request_created',1,$5::jsonb,$6)
	`, fixture.salonID, requestID, "create-"+suffix, strings.Repeat("b", 64), payload, createdAt); err != nil {
		t.Fatalf("insert %s request event: %v", suffix, err)
	}
	return requestID
}

func seedTerminalOwnerNotification(t *testing.T, ctx context.Context, db *sql.DB, salonID, requestID string, terminalAt time.Time) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO owner_notifications(
			salon_id,scheduling_request_id,type,title,message,dedupe_key,payload,delivery_status,
			next_delivery_at,dead_lettered_at,last_delivery_error_code,last_delivery_error,created_at
		) VALUES(
			$1,$2::uuid,'owner_manual_request_pending','Owner review required','Call Lan at +1 312 555 0111',
			'owner-manual-request-pending:'||$2::text,
			jsonb_build_object('scheduling_request_id',$2::text,'customer_name','Lan Nguyen','status','resolved'),
			'dead_letter',$3,$3,'TWILIO_30003','Destination +1 312 555 0111 failed',$3
		) RETURNING id::text
	`, salonID, requestID, terminalAt).Scan(&id); err != nil {
		t.Fatalf("insert terminal owner notification: %v", err)
	}
	return id
}

func seedLiveOwnerNotification(t *testing.T, ctx context.Context, db *sql.DB, salonID, requestID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO owner_notifications(
			salon_id,scheduling_request_id,type,title,message,dedupe_key,payload,delivery_status,
			next_delivery_at,delivery_provider,delivery_claim_token,delivery_claimed_at,delivery_lease_expires_at
		) VALUES(
			$1,$2::uuid,'owner_manual_request_pending','Owner review required','Live delivery body',
			'owner-manual-request-pending:'||$2::text,'{"status":"resolved"}'::jsonb,
			'delivering',now(),'twilio',$3::uuid,now(),now()+interval '10 minutes'
		)
	`, salonID, requestID, uuid.NewString()); err != nil {
		t.Fatalf("insert live owner notification: %v", err)
	}
}

func seedTerminalCustomerNotification(t *testing.T, ctx context.Context, db *sql.DB, fixture retentionFixture, requestID string, terminalAt time.Time) (string, string) {
	t.Helper()
	var consentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer_sms_consents(
			salon_id,normalized_destination,destination_masked,status,version,source,evidence_type,
			evidence_reference,actor_user_id,consented_at
		) VALUES($1,'+13125550111','••••0111','consented',1,'owner_attested','owner_attestation',$2,$3,now())
		RETURNING id::text
	`, fixture.salonID, "retention-test-"+uuid.NewString(), fixture.ownerID).Scan(&consentID); err != nil {
		t.Fatalf("insert customer consent: %v", err)
	}
	var deliveryID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer_notification_deliveries(
			salon_id,customer_sms_consent_id,scheduling_request_id,notification_type,source_version,
			dedupe_key,message_body,destination_e164,destination_masked,destination_hash,
			consent_version,policy_version,delivery_status,next_delivery_at,suppressed_at,created_at,updated_at
		) VALUES(
			$1,$2,$3,'request_received',1,$4,'Lan appointment request body',
			'+13125550111','••••0111',$5,1,1,'suppressed',$6,$6,$6,$6
		) RETURNING id::text
	`, fixture.salonID, consentID, requestID, "retention-customer-"+uuid.NewString(), strings.Repeat("c", 64), terminalAt).Scan(&deliveryID); err != nil {
		t.Fatalf("insert customer delivery: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE customer_sms_consents
		SET status='opted_out',version=2,source='twilio_advanced_opt_out',
		    evidence_type='twilio_opt_out_type',evidence_reference='STOP',
		    actor_user_id=NULL,opted_out_at=now(),updated_at=now()
		WHERE id=$1
	`, consentID); err != nil {
		t.Fatalf("set retained opt-out: %v", err)
	}
	return consentID, deliveryID
}

func seedRetentionAudio(t *testing.T, ctx context.Context, db *sql.DB, salonID string, expiresAt time.Time, body []byte) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO voice_audio_outputs(salon_id,provider,provider_call_id,content_type,audio_data,expires_at)
		VALUES($1,'openai',$2,'audio/mpeg',$3,$4) RETURNING id::text
	`, salonID, "retention-call-"+uuid.NewString(), body, expiresAt).Scan(&id); err != nil {
		t.Fatalf("insert retention audio: %v", err)
	}
	return id
}

func assertSchedulingAggregateRedacted(t *testing.T, ctx context.Context, db *sql.DB, requestID string) {
	t.Helper()
	var name, phone string
	var email, notes, reason sql.NullString
	var redactedAt sql.NullTime
	var version int
	if err := db.QueryRowContext(ctx, `
		SELECT customer_name,customer_phone,customer_email,notes,resolution_reason,redacted_at,redaction_version
		FROM scheduling_requests WHERE id=$1
	`, requestID).Scan(&name, &phone, &email, &notes, &reason, &redactedAt, &version); err != nil {
		t.Fatalf("load redacted request: %v", err)
	}
	if name != "[redacted]" || phone != "[redacted]" || email.Valid || notes.Valid || reason.Valid || !redactedAt.Valid || version != PolicyVersion {
		t.Fatalf("request redaction=%q/%q/%v/%v/%v/%v/v%d", name, phone, email, notes, reason, redactedAt.Valid, version)
	}
	var guest sql.NullString
	var segmentRedacted sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT guest_reference,redacted_at FROM scheduling_request_segments WHERE scheduling_request_id=$1`, requestID).Scan(&guest, &segmentRedacted); err != nil {
		t.Fatalf("load redacted segment: %v", err)
	}
	if guest.Valid || !segmentRedacted.Valid {
		t.Fatalf("segment redaction guest=%v redacted=%v", guest, segmentRedacted.Valid)
	}
	var hasCustomerName, redacted bool
	var status, providerBookingID string
	if err := db.QueryRowContext(ctx, `
		SELECT payload ? 'customer_name',COALESCE((payload->>'redacted')::boolean,false),
		       COALESCE(payload->>'status',''),COALESCE(payload->>'provider_booking_id','')
		FROM scheduling_request_events WHERE scheduling_request_id=$1
	`, requestID).Scan(&hasCustomerName, &redacted, &status, &providerBookingID); err != nil {
		t.Fatalf("load redacted event payload: %v", err)
	}
	if hasCustomerName || !redacted || status != "resolved" || providerBookingID != "provider-booking-safe-1" {
		t.Fatalf("event safe payload customer=%t redacted=%t status=%q provider=%q", hasCustomerName, redacted, status, providerBookingID)
	}
}

func assertRequestNotRedacted(t *testing.T, ctx context.Context, db *sql.DB, requestID string, expectRetention bool) {
	t.Helper()
	var name string
	var retentionAt, redactedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT customer_name,retention_expires_at,redacted_at FROM scheduling_requests WHERE id=$1`, requestID).Scan(&name, &retentionAt, &redactedAt); err != nil {
		t.Fatalf("load preserved request: %v", err)
	}
	if name == "[redacted]" || redactedAt.Valid || retentionAt.Valid != expectRetention {
		t.Fatalf("preserved request name=%q retention=%v redacted=%v", name, retentionAt.Valid, redactedAt.Valid)
	}
}

func assertIrreversibleRedaction(t *testing.T, ctx context.Context, db *sql.DB, requestID, ownerID, customerID, audioID string) {
	t.Helper()
	for name, statement := range map[string]string{
		"request":  `UPDATE scheduling_requests SET customer_name='restored',redacted_at=NULL,redaction_version=NULL WHERE id='` + requestID + `'`,
		"owner":    `UPDATE owner_notifications SET message='restored',redacted_at=NULL,redaction_version=NULL WHERE id='` + ownerID + `'`,
		"customer": `UPDATE customer_notification_deliveries SET message_body='restored',redacted_at=NULL,redaction_version=NULL WHERE id='` + customerID + `'`,
		"audio":    `UPDATE voice_audio_outputs SET audio_data='restored'::bytea,redacted_at=NULL,redaction_version=NULL WHERE id='` + audioID + `'`,
	} {
		if _, err := db.ExecContext(ctx, statement); err == nil {
			t.Fatalf("%s redaction was reversible", name)
		}
	}
}

func testAggregateRollback(t *testing.T, ctx context.Context, db *sql.DB, repo *Repository, fixture retentionFixture, terminalAt time.Time) {
	t.Helper()
	requestID := seedSchedulingRequest(t, ctx, db, fixture, "resolved", terminalAt, "rollback", "guest-rollback", `{
		"customer_name":"Rollback Guest",
		"status":"resolved",
		"provider_booking_id":"provider-booking-safe-1"
	}`)
	if _, err := db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION fail_retention_test_root_update() RETURNS TRIGGER AS $$
		BEGIN
			IF OLD.operation_key LIKE 'retention-rollback-%' AND NEW.redacted_at IS NOT NULL THEN
				RAISE EXCEPTION 'forced retention rollback';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		DROP TRIGGER IF EXISTS a_fail_retention_test_root_update ON scheduling_requests;
		CREATE TRIGGER a_fail_retention_test_root_update
		BEFORE UPDATE ON scheduling_requests FOR EACH ROW
		EXECUTE FUNCTION fail_retention_test_root_update();
	`); err != nil {
		t.Fatalf("install rollback trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS a_fail_retention_test_root_update ON scheduling_requests`)
		_, _ = db.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS fail_retention_test_root_update()`)
	})
	if processed, err := repo.RedactNext(ctx, KindSchedulingRequest); err == nil || processed {
		t.Fatalf("forced rollback processed=%t err=%v", processed, err)
	}
	var rootRedacted, segmentRedacted, eventRedacted sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT request.redacted_at,segment.redacted_at,event.redacted_at
		FROM scheduling_requests request
		JOIN scheduling_request_segments segment ON segment.scheduling_request_id=request.id
		JOIN scheduling_request_events event ON event.scheduling_request_id=request.id
		WHERE request.id=$1
	`, requestID).Scan(&rootRedacted, &segmentRedacted, &eventRedacted); err != nil {
		t.Fatalf("load rolled-back aggregate: %v", err)
	}
	if rootRedacted.Valid || segmentRedacted.Valid || eventRedacted.Valid {
		t.Fatalf("partial aggregate redaction survived rollback=%v/%v/%v", rootRedacted.Valid, segmentRedacted.Valid, eventRedacted.Valid)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER a_fail_retention_test_root_update ON scheduling_requests`); err != nil {
		t.Fatalf("drop rollback trigger: %v", err)
	}
	if processed, err := repo.RedactNext(ctx, KindSchedulingRequest); err != nil || !processed {
		t.Fatalf("redact aggregate after rollback recovery processed=%t err=%v", processed, err)
	}
	assertSchedulingAggregateRedacted(t, ctx, db, requestID)
}
