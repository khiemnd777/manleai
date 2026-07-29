package database

import (
	"context"
	"os"
	"testing"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

func TestContextConnectorAppliesAndClearsRequestDatabaseContext(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	db, err := Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open contextual database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	actorID := "00000000-0000-4000-8000-000000000068"
	var actor, scope, systemSalonID string
	if err := db.QueryRowContext(databasecontext.WithActor(context.Background(), actorID), `
		SELECT current_setting('app.actor_user_id', true), current_setting('app.database_scope', true), current_setting('app.system_salon_id', true)
	`).Scan(&actor, &scope, &systemSalonID); err != nil {
		t.Fatalf("read actor context: %v", err)
	}
	if actor != actorID || scope != "" || systemSalonID != "" {
		t.Fatalf("database actor=%q scope=%q system_salon_id=%q", actor, scope, systemSalonID)
	}

	if err := db.QueryRowContext(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker), `
		SELECT current_setting('app.actor_user_id', true), current_setting('app.database_scope', true), current_setting('app.system_salon_id', true)
	`).Scan(&actor, &scope, &systemSalonID); err != nil {
		t.Fatalf("read worker context: %v", err)
	}
	if actor != "" || scope != databasecontext.ScopeWorker || systemSalonID != "" {
		t.Fatalf("database worker actor=%q scope=%q system_salon_id=%q", actor, scope, systemSalonID)
	}

	if err := db.QueryRowContext(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, actorID), `
		SELECT current_setting('app.actor_user_id', true), current_setting('app.database_scope', true), current_setting('app.system_salon_id', true)
	`).Scan(&actor, &scope, &systemSalonID); err != nil {
		t.Fatalf("read bound worker context: %v", err)
	}
	if actor != "" || scope != databasecontext.ScopeWorker || systemSalonID != actorID {
		t.Fatalf("database bound worker actor=%q scope=%q system_salon_id=%q", actor, scope, systemSalonID)
	}

	if err := db.QueryRowContext(context.Background(), `
		SELECT current_setting('app.actor_user_id', true), current_setting('app.database_scope', true), current_setting('app.system_salon_id', true)
	`).Scan(&actor, &scope, &systemSalonID); err != nil {
		t.Fatalf("read cleared context: %v", err)
	}
	if actor != "" || scope != "" || systemSalonID != "" {
		t.Fatalf("database context leaked actor=%q scope=%q system_salon_id=%q", actor, scope, systemSalonID)
	}
}
