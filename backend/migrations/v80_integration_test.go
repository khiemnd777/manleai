package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestV80SystemTenantMatcherAndPolicyCatalogFailClosed(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	salonA := uuid.NewString()
	salonB := uuid.NewString()
	assertAllowed := func(scope, systemSalonID, targetSalonID string, want bool) {
		t.Helper()
		if _, err := tx.Exec(`
			SELECT set_config('app.database_scope',$1,true),
			       set_config('app.system_salon_id',$2,true),
			       set_config('app.actor_user_id','',true)
		`, scope, systemSalonID); err != nil {
			t.Fatalf("set database context: %v", err)
		}
		var got bool
		if err := tx.QueryRow(`SELECT public.app_rls_system_salon_allowed($1)`, targetSalonID).Scan(&got); err != nil {
			t.Fatalf("evaluate system tenant matcher: %v", err)
		}
		if got != want {
			t.Fatalf("scope=%q system salon=%q target=%q allowed=%t, want %t", scope, systemSalonID, targetSalonID, got, want)
		}
	}

	assertAllowed("", "", salonA, false)
	assertAllowed("worker", "", salonA, false)
	assertAllowed("worker", "invalid-uuid", salonA, false)
	assertAllowed("worker", salonA, salonA, true)
	assertAllowed("worker", salonA, salonB, false)
	assertAllowed("provider", salonB, salonB, true)
	assertAllowed("provider", salonB, salonA, false)

	var unsafePolicyCount int
	if err := tx.QueryRow(`
		SELECT count(*)
		FROM pg_catalog.pg_policies policy
		CROSS JOIN LATERAL (
			VALUES (policy.qual), (policy.with_check)
		) AS expression(value)
		WHERE policy.schemaname = 'public'
		  AND expression.value IS NOT NULL
		  AND expression.value LIKE '%app_database_scope%'
		  AND (
		      expression.value LIKE '%worker%'
		      OR expression.value LIKE '%provider%'
		  )
		  AND expression.value NOT LIKE '%app_rls_system_salon_allowed%'
	`).Scan(&unsafePolicyCount); err != nil {
		t.Fatalf("audit runtime policy catalog: %v", err)
	}
	if unsafePolicyCount != 0 {
		t.Fatalf("unsafe provider/worker policy expressions=%d, want 0", unsafePolicyCount)
	}
}
