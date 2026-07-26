package tenantruntime

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestConsumeQuotaIsAtomicAndSalonScoped(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	repo := NewRepository(db)
	ctx := context.Background()
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	var ownerID, salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email,password_hash,full_name)
		VALUES ($1,'test-hash','Quota Owner') RETURNING id::text
	`, "quota-"+suffix+"@example.test").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name,phone,owner_user_id)
		VALUES ('Quota Salon','555-6900',$1) RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	})
	if _, err := db.ExecContext(ctx, `UPDATE tenant_runtime_limits SET voice_starts_per_minute=3 WHERE salon_id=$1`, salonID); err != nil {
		t.Fatalf("set quota: %v", err)
	}

	var allowed atomic.Int64
	var rejected atomic.Int64
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			decision, consumeErr := repo.Consume(ctx, salonID, MetricVoiceStart, 1)
			if consumeErr != nil {
				t.Errorf("consume quota: %v", consumeErr)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			} else {
				rejected.Add(1)
			}
		}()
	}
	group.Wait()
	if allowed.Load() != 3 || rejected.Load() != 9 {
		t.Fatalf("allowed=%d rejected=%d, want 3/9", allowed.Load(), rejected.Load())
	}
}
