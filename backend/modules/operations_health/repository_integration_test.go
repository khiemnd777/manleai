package operationshealth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestRepositoryPostgresRunFencingAndTenantMetrics(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	jobName := "health_test_" + uuid.NewString()[:8]
	workerOne, workerTwo := uuid.NewString(), uuid.NewString()
	repository := NewRepository(db)
	base := time.Date(2026, time.July, 24, 10, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return base }
	runOne, err := repository.StartRun(ctx, StartRunInput{JobName: jobName, WorkerInstanceID: workerOne, Interval: 30 * time.Second, StaleAfter: 2 * time.Minute, LeaseDuration: 90 * time.Second})
	if err != nil {
		t.Fatalf("start first run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM worker_job_runs WHERE job_name=$1`, jobName)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM worker_job_heartbeats WHERE job_name=$1`, jobName)
	})
	if _, err := repository.StartRun(ctx, StartRunInput{JobName: jobName, WorkerInstanceID: workerTwo, Interval: 30 * time.Second, StaleAfter: 2 * time.Minute, LeaseDuration: 90 * time.Second}); !errors.Is(err, ErrJobLeaseHeld) {
		t.Fatalf("concurrent start error=%v, want lease held", err)
	}
	repository.now = func() time.Time { return base.Add(2 * time.Minute) }
	runTwo, err := repository.StartRun(ctx, StartRunInput{JobName: jobName, WorkerInstanceID: workerTwo, Interval: 30 * time.Second, StaleAfter: 2 * time.Minute, LeaseDuration: 90 * time.Second})
	if err != nil {
		t.Fatalf("replace expired run: %v", err)
	}
	if err := repository.HeartbeatRun(ctx, jobName, runOne, workerOne, 90*time.Second); !errors.Is(err, ErrRunFenced) {
		t.Fatalf("old heartbeat error=%v, want fenced", err)
	}
	if err := repository.FinishRun(ctx, FinishRunInput{JobName: jobName, RunID: runTwo, WorkerInstanceID: workerTwo, Status: RunStatusSucceeded, ProcessedCount: 7}); err != nil {
		t.Fatalf("finish replacement: %v", err)
	}
	if _, err := repository.StartRun(ctx, StartRunInput{JobName: jobName, WorkerInstanceID: workerOne, Interval: 30 * time.Second, StaleAfter: 2 * time.Minute, LeaseDuration: 90 * time.Second}); !errors.Is(err, ErrJobLeaseHeld) {
		t.Fatalf("same-cadence start error=%v, want lease held", err)
	}
	var abandoned, succeeded int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status='abandoned'), count(*) FILTER (WHERE status='succeeded') FROM worker_job_runs WHERE job_name=$1`, jobName).Scan(&abandoned, &succeeded); err != nil {
		t.Fatalf("load run statuses: %v", err)
	}
	if abandoned != 1 || succeeded != 1 {
		t.Fatalf("run statuses abandoned=%d succeeded=%d", abandoned, succeeded)
	}

	ownerOne, ownerTwo := insertHealthTestUser(t, ctx, db), insertHealthTestUser(t, ctx, db)
	salonOne, salonTwo := insertHealthTestSalon(t, ctx, db, ownerOne), insertHealthTestSalon(t, ctx, db, ownerTwo)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonOne, salonTwo)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerOne, ownerTwo)
	})
	for _, salonID := range []string{salonOne, salonTwo} {
		if _, err := db.ExecContext(ctx, `INSERT INTO call_sessions (salon_id, channel, status, lifecycle_status, retention_expires_at) VALUES ($1,'simulator','completed','archived',now()-interval '1 day')`, salonID); err != nil {
			t.Fatalf("seed retention metric: %v", err)
		}
	}
	_, queues, err := repository.LoadStatus(ctx, salonOne, ownerOne)
	if err != nil {
		t.Fatalf("load owned status: %v", err)
	}
	found := false
	for _, queue := range queues {
		if queue.Key == "conversation_retention" {
			found = true
			if queue.BacklogCount != 1 {
				t.Fatalf("tenant retention backlog=%d, want 1", queue.BacklogCount)
			}
		}
	}
	if !found {
		t.Fatal("conversation retention metric missing")
	}
	if _, _, err := repository.LoadStatus(ctx, salonOne, ownerTwo); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner status error=%v, want not found", err)
	}
}

func insertHealthTestUser(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var id string
	email := fmt.Sprintf("health-%s@example.com", uuid.NewString())
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email,password_hash,full_name) VALUES ($1,'test','Health Test') RETURNING id::text`, email).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func insertHealthTestSalon(t *testing.T, ctx context.Context, db *sql.DB, ownerID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `INSERT INTO salons (name,phone,owner_user_id) VALUES ('Health Test',$1,$2) RETURNING id::text`, "+1"+uuid.NewString()[:10], ownerID).Scan(&id); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	return id
}
