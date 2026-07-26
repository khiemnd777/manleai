package migrations

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestV56OwnerNotificationDeliverySafetyContract(t *testing.T) {
	raw, err := Files.ReadFile("V56__owner_notification_delivery.sql")
	if err != nil {
		t.Fatalf("read V56: %v", err)
	}
	source := string(raw)
	for _, fragment := range []string{
		"WHERE delivery_status IN ('queued', 'delivering', 'delivered', 'failed')",
		"delivery_dispatch_started_at",
		"owner_notification_delivery_attempts",
		"owner_notification_delivery_events",
		"owner_notification_delivery_actions",
		"FOREIGN KEY (salon_id, owner_notification_id)",
		"provider_message_id",
		"dead_letter",
		"UNIQUE (salon_id, action_key)",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("V56 missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToLower(source), "customer_sms") {
		t.Fatal("V56 must not introduce customer SMS delivery")
	}
}

func TestV56BackfillsLegacyRowsDisabledAndEnforcesDeliveryGuards(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	schemaName := "v56_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schemaName)
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema+", public"); err != nil {
		t.Fatalf("search path: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE users(id UUID PRIMARY KEY DEFAULT gen_random_uuid());
		CREATE TABLE owner_notifications(
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(), salon_id UUID NOT NULL,
			delivery_status TEXT NOT NULL, last_delivery_error TEXT,
			delivery_attempts INTEGER NOT NULL DEFAULT 0,
			next_delivery_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			delivered_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT owner_notifications_delivery_status_check
				CHECK (delivery_status IN ('queued','delivering','delivered','failed','disabled'))
		);
		CREATE INDEX idx_owner_notifications_delivery_queue ON owner_notifications(next_delivery_at);
		INSERT INTO owner_notifications(salon_id,delivery_status)
		SELECT gen_random_uuid(), status FROM unnest(ARRAY['queued','delivering','delivered','failed']) status;
	`); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	raw, _ := Files.ReadFile("V56__owner_notification_delivery.sql")
	if _, err := tx.ExecContext(ctx, string(raw)); err != nil {
		t.Fatalf("apply V56: %v", err)
	}
	var disabled int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM owner_notifications WHERE delivery_status='disabled'`).Scan(&disabled); err != nil || disabled != 4 {
		t.Fatalf("disabled=%d err=%v", disabled, err)
	}
	var notificationID, salonID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text,salon_id::text FROM owner_notifications LIMIT 1`).Scan(&notificationID, &salonID); err != nil {
		t.Fatalf("parent row: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT v56_deadletter_guard`); err != nil {
		t.Fatalf("dead-letter savepoint: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE owner_notifications SET delivery_status='dead_letter' WHERE id=(SELECT id FROM owner_notifications LIMIT 1)`); err == nil || !strings.Contains(err.Error(), "owner_notifications_dead_letter_shape_check") {
		t.Fatalf("dead-letter shape error=%v", err)
	}
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT v56_deadletter_guard`); err != nil {
		t.Fatalf("rollback dead-letter guard: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT v56_tenant_guard`); err != nil {
		t.Fatalf("tenant savepoint: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO owner_notification_delivery_attempts(salon_id,owner_notification_id,attempt_number,claim_token,provider,outcome)
		VALUES(gen_random_uuid(),$1,1,gen_random_uuid(),'twilio','leased')
	`, notificationID); err == nil || !strings.Contains(err.Error(), "owner_notification_delivery_attempts_tenant_fk") {
		t.Fatalf("tenant FK error=%v salon=%s", err, salonID)
	}
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT v56_tenant_guard`); err != nil {
		t.Fatalf("rollback tenant guard: %v", err)
	}
}
