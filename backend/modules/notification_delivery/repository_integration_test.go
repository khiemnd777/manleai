package notificationdelivery

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	appdatabase "github.com/manleai/ai-receptionist/internal/database"
)

func TestRepositoryPostgresClaimCallbackReplayTenantAndLeaseSafety(t *testing.T) {
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
	ownerID := seedDeliveryUser(t, ctx, db, "delivery-owner")
	otherOwnerID := seedDeliveryUser(t, ctx, db, "delivery-other")
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,owner_user_id) VALUES('Delivery Test Salon',$1,$2) RETURNING id::text
	`, "+1"+time.Now().UTC().Format("150405000"), ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerID, otherOwnerID)
	})
	notificationID := seedQueuedDelivery(t, ctx, db, salonID, "claim")
	repo := NewRepository(db)

	var wg sync.WaitGroup
	claimed := make(chan ClaimedNotification, 2)
	errorsCh := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, claimErr := repo.ClaimBatch(ctx, 1, DeliveryLeaseDuration)
			if claimErr != nil {
				errorsCh <- claimErr
				return
			}
			for _, item := range items {
				claimed <- item
			}
		}()
	}
	wg.Wait()
	close(claimed)
	close(errorsCh)
	for claimErr := range errorsCh {
		t.Fatalf("concurrent claim: %v", claimErr)
	}
	items := make([]ClaimedNotification, 0, 1)
	for item := range claimed {
		items = append(items, item)
	}
	if len(items) != 1 || items[0].ID != notificationID {
		t.Fatalf("claims=%#v, want exactly one", items)
	}
	item := items[0]
	if err := repo.MarkDispatchStarted(ctx, item, "••••0123"); err != nil {
		t.Fatalf("mark dispatch: %v", err)
	}
	if err := repo.RecordProviderResult(ctx, item, SendResult{ProviderMessageID: "SM" + uuid.NewString(), ProviderStatus: "queued", DeliveryStatus: StatusProviderAccepted, StatusRank: 10}); err != nil {
		t.Fatalf("provider result: %v", err)
	}
	var providerMessageID string
	if err := db.QueryRowContext(ctx, `SELECT provider_message_id FROM owner_notifications WHERE id=$1`, notificationID).Scan(&providerMessageID); err != nil {
		t.Fatalf("provider id: %v", err)
	}
	delivered := ProviderCallback{Provider: ProviderTwilio, ProviderMessageID: providerMessageID, ProviderStatus: "delivered", StatusRank: 50, DeliveryStatus: StatusDelivered, EventKey: "callback-delivered", EventFingerprint: fingerprintParts("delivered"), OccurredAt: time.Now().UTC()}
	if err := repo.ApplyProviderCallback(ctx, delivered); err != nil {
		t.Fatalf("delivered callback: %v", err)
	}
	if err := repo.ApplyProviderCallback(ctx, delivered); err != nil {
		t.Fatalf("exact callback replay: %v", err)
	}
	outOfOrder := ProviderCallback{Provider: ProviderTwilio, ProviderMessageID: providerMessageID, ProviderStatus: "sent", StatusRank: 30, DeliveryStatus: StatusSent, EventKey: "callback-sent-late", EventFingerprint: fingerprintParts("sent"), OccurredAt: time.Now().UTC().Add(time.Second)}
	if err := repo.ApplyProviderCallback(ctx, outOfOrder); err != nil {
		t.Fatalf("out-of-order callback: %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT delivery_status FROM owner_notifications WHERE id=$1`, notificationID).Scan(&status); err != nil || status != StatusDelivered {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if _, err := repo.GetForOwner(ctx, salonID, otherOwnerID, notificationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get error=%v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE owner_notifications SET delivery_status='dead_letter', delivery_attempts=$2, dead_lettered_at=now(), last_delivery_error_code='TWILIO_30003' WHERE id=$1`, notificationID, MaxSafeDeliveryAttempts); err != nil {
		t.Fatalf("prepare dead letter: %v", err)
	}
	actionKey := "requeue-" + uuid.NewString()
	fingerprint := RequeueFingerprint(notificationID)
	if _, replayed, err := repo.RequeueForOwner(ctx, salonID, ownerID, notificationID, actionKey, fingerprint); err != nil || replayed {
		t.Fatalf("requeue replayed=%t err=%v", replayed, err)
	}
	if _, replayed, err := repo.RequeueForOwner(ctx, salonID, ownerID, notificationID, actionKey, fingerprint); err != nil || !replayed {
		t.Fatalf("requeue replay replayed=%t err=%v", replayed, err)
	}
	requeuedItems, err := repo.ClaimBatch(ctx, 1, DeliveryLeaseDuration)
	if err != nil || len(requeuedItems) != 1 || requeuedItems[0].ID != notificationID || requeuedItems[0].AttemptNumber != MaxSafeDeliveryAttempts+1 || requeuedItems[0].RequeueCount != 1 {
		t.Fatalf("requeued claim=%#v err=%v", requeuedItems, err)
	}
	if err := repo.RecordDisabled(ctx, requeuedItems[0]); err != nil {
		t.Fatalf("finish requeued fixture: %v", err)
	}

	unknownID := seedQueuedDelivery(t, ctx, db, salonID, "unknown")
	unknownItems, err := repo.ClaimBatch(ctx, 1, DeliveryLeaseDuration)
	if err != nil || len(unknownItems) != 1 || unknownItems[0].ID != unknownID {
		t.Fatalf("unknown claim=%#v err=%v", unknownItems, err)
	}
	if err := repo.MarkDispatchStarted(ctx, unknownItems[0], "••••0456"); err != nil {
		t.Fatalf("unknown dispatch: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE owner_notifications SET delivery_claimed_at=now()-interval '2 minutes', delivery_lease_expires_at=now()-interval '1 second' WHERE id=$1`, unknownID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if recovered, err := repo.RecoverExpiredLeases(ctx, 10); err != nil || recovered < 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT delivery_status FROM owner_notifications WHERE id=$1`, unknownID).Scan(&status); err != nil || status != StatusDeadLetter {
		t.Fatalf("recovered unknown status=%q err=%v", status, err)
	}
	if _, _, err := repo.RequeueForOwner(ctx, salonID, ownerID, unknownID, "unsafe-"+uuid.NewString(), RequeueFingerprint(unknownID)); !errors.Is(err, ErrRequeueBlocked) {
		t.Fatalf("unknown requeue error=%v", err)
	}
}

func TestClaimBatchAppliesPerTenantFairnessBeforeGlobalLimit(t *testing.T) {
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
	ownerA := seedDeliveryUser(t, ctx, db, "fair-owner-a")
	ownerB := seedDeliveryUser(t, ctx, db, "fair-owner-b")
	var salonA, salonB string
	if err := db.QueryRowContext(ctx, `INSERT INTO salons(name,phone,owner_user_id) VALUES('Fair Salon A',$1,$2) RETURNING id::text`, "+1"+time.Now().UTC().Format("150405001"), ownerA).Scan(&salonA); err != nil {
		t.Fatalf("insert salon A: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO salons(name,phone,owner_user_id) VALUES('Fair Salon B',$1,$2) RETURNING id::text`, "+1"+time.Now().UTC().Format("150405002"), ownerB).Scan(&salonB); err != nil {
		t.Fatalf("insert salon B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonA, salonB)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerA, ownerB)
	})
	if _, err := db.ExecContext(ctx, `UPDATE tenant_runtime_limits SET worker_claims_per_batch=1 WHERE salon_id IN ($1,$2)`, salonA, salonB); err != nil {
		t.Fatalf("set fair claim limits: %v", err)
	}
	for index := range 3 {
		seedQueuedDelivery(t, ctx, db, salonA, "fair-a-"+strconv.Itoa(index))
	}
	seedQueuedDelivery(t, ctx, db, salonB, "fair-b")

	items, err := NewRepository(db).ClaimBatch(ctx, 4, DeliveryLeaseDuration)
	if err != nil {
		t.Fatalf("claim fair batch: %v", err)
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.SalonID]++
	}
	if len(items) != 2 || counts[salonA] != 1 || counts[salonB] != 1 {
		t.Fatalf("fair claims=%#v counts=%#v, want one per salon", items, counts)
	}
}

func seedDeliveryUser(t *testing.T, ctx context.Context, db *sql.DB, prefix string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name) VALUES($1,'hash','Delivery Test') RETURNING id::text`, prefix+"-"+uuid.NewString()+"@example.test").Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func seedQueuedDelivery(t *testing.T, ctx context.Context, db *sql.DB, salonID, suffix string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO owner_notifications(salon_id,type,title,message,dedupe_key,payload,delivery_status,next_delivery_at)
		VALUES($1,'delivery_integration_test','Owner review required','A new owner review request is waiting.',$2,'{"status":"pending"}'::jsonb,'queued',now())
		RETURNING id::text
	`, salonID, "delivery-test-"+suffix+"-"+uuid.NewString()).Scan(&id); err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	return id
}
