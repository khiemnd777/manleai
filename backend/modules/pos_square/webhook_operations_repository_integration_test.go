package pos_square

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	appdatabase "github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestWebhookRepositoryPostgresOperationsSafety(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := appdatabase.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := NewWebhookRepository(db)
	ownerOne := insertSquareWebhookTestUser(t, ctx, db)
	ownerTwo := insertSquareWebhookTestUser(t, ctx, db)
	salonOne := insertSquareWebhookTestSalon(t, ctx, db, ownerOne)
	salonTwo := insertSquareWebhookTestSalon(t, ctx, db, ownerTwo)
	merchantID := "MERCHANT_" + uuid.NewString()
	locationID := "LOCATION_" + uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id,provider,status,merchant_id,location_id,last_sync_at)
		VALUES ($1,'square','active',$2,$3,now())
	`, salonOne, merchantID, locationID); err != nil {
		t.Fatalf("seed Square connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonOne, salonTwo)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerOne, ownerTwo)
	})

	deadEvent := insertSquareWebhookTestEvent(t, ctx, db, salonOne, WebhookStatusDeadLetter, 10, 0, "SAFE_FAILURE", merchantID, locationID)
	record, err := repo.GetBookingWebhookForOwner(ctx, salonOne, ownerOne, deadEvent)
	if err != nil {
		t.Fatalf("load owner event: %v", err)
	}
	if !record.CanRequeue || record.LastErrorCode != "SAFE_FAILURE" {
		t.Fatalf("owner event replay state = %#v", record)
	}
	if _, err := repo.GetBookingWebhookForOwner(ctx, salonOne, ownerTwo, deadEvent); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("wrong-owner event error=%v, want salon not found", err)
	}
	if _, err := repo.GetBookingWebhookForOwner(ctx, salonTwo, ownerTwo, deadEvent); !errors.Is(err, ErrWebhookEventNotFound) {
		t.Fatalf("cross-salon event error=%v, want event not found", err)
	}

	actionKey := "requeue-" + uuid.NewString()
	fingerprint := webhookRequeueFingerprint(deadEvent)
	requeued, replayed, err := repo.RequeueBookingWebhookForOwner(ctx, salonOne, ownerOne, deadEvent, actionKey, fingerprint)
	if err != nil || replayed || requeued.ProcessingStatus != WebhookStatusPending || requeued.RequeueCount != 1 {
		t.Fatalf("initial requeue event/replayed/error = %#v/%t/%v", requeued, replayed, err)
	}
	replayedEvent, replayed, err := repo.RequeueBookingWebhookForOwner(ctx, salonOne, ownerOne, deadEvent, actionKey, fingerprint)
	if err != nil || !replayed || replayedEvent.ID != deadEvent || replayedEvent.RequeueCount != 1 {
		t.Fatalf("exact requeue replay event/replayed/error = %#v/%t/%v", replayedEvent, replayed, err)
	}
	changedTarget := insertSquareWebhookTestEvent(t, ctx, db, salonOne, WebhookStatusDeadLetter, 10, 0, "SAFE_FAILURE", merchantID, locationID)
	if _, _, err := repo.RequeueBookingWebhookForOwner(ctx, salonOne, ownerOne, changedTarget, actionKey, webhookRequeueFingerprint(changedTarget)); !errors.Is(err, ErrWebhookActionConflict) {
		t.Fatalf("changed action-key reuse error=%v, want conflict", err)
	}

	concurrentEvent := insertSquareWebhookTestEvent(t, ctx, db, salonOne, WebhookStatusDeadLetter, 10, 0, "SAFE_FAILURE", merchantID, locationID)
	start := make(chan struct{})
	var succeeded, blocked atomic.Int32
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, _, requeueErr := repo.RequeueBookingWebhookForOwner(
				context.Background(), salonOne, ownerOne, concurrentEvent,
				fmt.Sprintf("concurrent-%d-%s", index, uuid.NewString()), webhookRequeueFingerprint(concurrentEvent),
			)
			switch {
			case requeueErr == nil:
				succeeded.Add(1)
			case errors.Is(requeueErr, ErrWebhookRequeueBlocked):
				blocked.Add(1)
			default:
				t.Errorf("concurrent requeue %d: %v", index, requeueErr)
			}
		}()
	}
	close(start)
	workers.Wait()
	if succeeded.Load() != 1 || blocked.Load() != 1 {
		t.Fatalf("concurrent requeue succeeded/blocked=%d/%d, want 1/1", succeeded.Load(), blocked.Load())
	}

	exhaustedEvent := insertSquareWebhookTestEvent(t, ctx, db, salonOne, WebhookStatusFailed, 10, 0, "SAFE_FAILURE", merchantID, locationID)
	claimed, err := repo.ClaimBookingWebhooks(ctx, 100)
	if err != nil {
		t.Fatalf("claim with exhausted event: %v", err)
	}
	for _, item := range claimed {
		if item.ID == exhaustedEvent {
			t.Fatal("attempt-exhausted event must not be claimed")
		}
	}
	assertSquareWebhookDeadLetter(t, ctx, db, exhaustedEvent)

	ninthAttempt := insertSquareWebhookTestEvent(t, ctx, db, salonOne, WebhookStatusFailed, 9, 0, "SAFE_FAILURE", merchantID, locationID)
	claimed, err = repo.ClaimBookingWebhooks(ctx, 100)
	if err != nil {
		t.Fatalf("claim ninth-attempt event: %v", err)
	}
	var tenth SquareBookingWebhookEvent
	for _, item := range claimed {
		if item.ID == ninthAttempt {
			tenth = item
			break
		}
	}
	if tenth.ID == "" || tenth.ProcessingAttempts != 10 {
		t.Fatalf("claimed tenth attempt = %#v", tenth)
	}
	if err := repo.CompleteBookingWebhook(ctx, tenth.ID, tenth.ProcessingToken, tenth.ProcessingAttempts, errors.New("raw provider response BOOKING_SECRET")); err != nil {
		t.Fatalf("complete tenth attempt: %v", err)
	}
	assertSquareWebhookDeadLetter(t, ctx, db, ninthAttempt)
	var rawError sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT last_error FROM square_booking_webhook_events WHERE id=$1`, ninthAttempt).Scan(&rawError); err != nil || rawError.Valid {
		t.Fatalf("raw webhook error valid/error=%t/%v, want redacted NULL", rawError.Valid, err)
	}

	dedupeEventID := "EVENT_" + uuid.NewString()
	enqueue := SquareBookingWebhookEvent{SalonID: salonOne, EventID: dedupeEventID, EventType: "booking.updated", MerchantID: merchantID, LocationID: locationID, POSBookingID: "BOOKING_" + uuid.NewString()}
	inserted, err := repo.EnqueueBookingWebhook(ctx, enqueue)
	if err != nil || !inserted {
		t.Fatalf("first enqueue inserted/error=%t/%v", inserted, err)
	}
	inserted, err = repo.EnqueueBookingWebhook(ctx, enqueue)
	if err != nil || inserted {
		t.Fatalf("duplicate enqueue inserted/error=%t/%v, want false/nil", inserted, err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO square_calendar_repair_state (
			salon_id,next_repair_at,lease_expires_at,lease_token,repair_attempts
		) VALUES ($1,now(),now()+interval '5 minutes',$2,1)
		ON CONFLICT (salon_id) DO UPDATE
		SET next_repair_at=EXCLUDED.next_repair_at,
		    lease_expires_at=EXCLUDED.lease_expires_at,
		    lease_token=EXCLUDED.lease_token,
		    repair_attempts=EXCLUDED.repair_attempts
	`, salonOne, "repair-"+uuid.NewString()); err != nil {
		t.Fatalf("seed repair claim: %v", err)
	}
	var repairToken string
	if err := db.QueryRowContext(ctx, `SELECT lease_token FROM square_calendar_repair_state WHERE salon_id=$1`, salonOne).Scan(&repairToken); err != nil {
		t.Fatalf("load repair token: %v", err)
	}
	if err := repo.CompleteCalendarRepair(ctx, salonOne, repairToken, errors.New("raw repair response LOCATION_SECRET")); err != nil {
		t.Fatalf("complete repair failure: %v", err)
	}
	var repairRaw sql.NullString
	var repairClass, repairCode string
	if err := db.QueryRowContext(ctx, `
		SELECT last_error, COALESCE(last_error_class,''), COALESCE(last_error_code,'')
		FROM square_calendar_repair_state WHERE salon_id=$1
	`, salonOne).Scan(&repairRaw, &repairClass, &repairCode); err != nil {
		t.Fatalf("load repair diagnostics: %v", err)
	}
	if repairRaw.Valid || repairClass != "dependency" || repairCode != "SQUARE_CALENDAR_REPAIR_FAILED" {
		t.Fatalf("repair diagnostics raw/class/code=%v/%q/%q", repairRaw, repairClass, repairCode)
	}

	if _, err := db.ExecContext(ctx, `UPDATE square_booking_webhook_events SET pos_booking_id='MUTATED' WHERE id=$1`, changedTarget); err == nil {
		t.Fatal("immutable provider booking evidence update unexpectedly succeeded")
	}
	if _, err := db.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='owner_manual' WHERE salon_id=$1`, salonOne); err != nil {
		t.Fatalf("switch current scheduling authority for historical webhook test: %v", err)
	}
	target, err := repo.FindWebhookTarget(databasecontext.WithScope(ctx, databasecontext.ScopeProvider), merchantID, locationID)
	if err != nil || target == nil || target.SalonID != salonOne {
		t.Fatalf("historical Square target after authority switch = %#v/%v", target, err)
	}

	snapshot, err := repo.LoadTenantQueueMetrics(ctx, salonOne, ownerOne)
	if err != nil || !snapshot.Relevant {
		t.Fatalf("provider-owned tenant metrics = %#v/%v", snapshot, err)
	}
	if _, err := repo.LoadTenantQueueMetrics(ctx, salonOne, ownerTwo); !errors.Is(err, operationshealth.ErrNotFound) {
		t.Fatalf("cross-owner metrics error=%v, want not found", err)
	}
	var posErrorCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pos_errors WHERE salon_id=$1`, salonOne).Scan(&posErrorCount); err != nil {
		t.Fatalf("count POS errors: %v", err)
	}
	if posErrorCount != 0 {
		t.Fatalf("webhook operations created %d POS errors, want zero", posErrorCount)
	}

	list, _, repair, err := repo.ListBookingWebhooksForOwner(ctx, salonOne, ownerOne, "", 100, 0)
	if err != nil || !repair.Relevant || len(list) == 0 {
		t.Fatalf("owner operations list/repair/error = %d/%#v/%v", len(list), repair, err)
	}
	raw, _ := json.Marshal(list)
	for _, forbidden := range []string{merchantID, locationID, "BOOKING_SECRET", "LOCATION_SECRET", "pos_booking_id", "merchant_id", "processing_token"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("owner operations response leaked %q: %s", forbidden, raw)
		}
	}
}

func insertSquareWebhookTestUser(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var id string
	email := fmt.Sprintf("square-webhook-%s@example.com", uuid.NewString())
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email,password_hash,full_name)
		VALUES ($1,'test','Square Webhook Test') RETURNING id::text
	`, email).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func insertSquareWebhookTestSalon(t *testing.T, ctx context.Context, db *sql.DB, ownerID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name,phone,owner_user_id,active_pos_provider)
		VALUES ('Square Webhook Test',$1,$2,'square') RETURNING id::text
	`, "+1"+strings.ReplaceAll(uuid.NewString()[:10], "-", "0"), ownerID).Scan(&id); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings (salon_id) VALUES ($1)`, id); err != nil {
		t.Fatalf("insert salon settings: %v", err)
	}
	return id
}

func insertSquareWebhookTestEvent(t *testing.T, ctx context.Context, db *sql.DB, salonID, status string, attempts, requeues int, errorCode, merchantID, locationID string) string {
	t.Helper()
	var id string
	deadLetter := any(nil)
	if status == WebhookStatusDeadLetter {
		deadLetter = time.Now().UTC()
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO square_booking_webhook_events (
			salon_id,event_id,event_type,merchant_id,location_id,pos_booking_id,
			processing_status,processing_attempts,requeue_count,next_attempt_at,
			last_error_class,last_error_code,dead_lettered_at
		) VALUES ($1,$2,'booking.updated',$3,$4,$5,$6,$7,$8,now()-interval '1 minute',
		          CASE WHEN $9='' THEN NULL ELSE 'dependency' END,NULLIF($9,''),$10)
		RETURNING id::text
	`, salonID, "EVENT_"+uuid.NewString(), merchantID, locationID, "BOOKING_"+uuid.NewString(),
		status, attempts, requeues, errorCode, deadLetter).Scan(&id); err != nil {
		t.Fatalf("insert webhook event: %v", err)
	}
	return id
}

func assertSquareWebhookDeadLetter(t *testing.T, ctx context.Context, db *sql.DB, eventID string) {
	t.Helper()
	var status, code string
	var deadLetteredAt sql.NullTime
	if err := db.QueryRowContext(ctx, `
		SELECT processing_status, COALESCE(last_error_code,''), dead_lettered_at
		FROM square_booking_webhook_events WHERE id=$1
	`, eventID).Scan(&status, &code, &deadLetteredAt); err != nil {
		t.Fatalf("load webhook terminal state: %v", err)
	}
	if status != WebhookStatusDeadLetter || code != "SQUARE_WEBHOOK_ATTEMPTS_EXHAUSTED" || !deadLetteredAt.Valid {
		t.Fatalf("webhook status/code/dead-letter=%q/%q/%v", status, code, deadLetteredAt)
	}
}
