package database

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

func TestV80RuntimeRoleEnforcesActorPublicAndSystemTenantBoundaries(t *testing.T) {
	adminURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
	}
	adminDB, err := Open(context.Background(), adminURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := Migrate(context.Background(), adminDB); err != nil {
		t.Fatalf("migrate RLS database: %v", err)
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(suffix) > 16 {
		suffix = suffix[len(suffix)-16:]
	}
	runtimeRole := "rls_runtime_" + suffix
	runtimePassword := "RlsRuntimeTest_" + suffix
	quotedRole := pq.QuoteIdentifier(runtimeRole)
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(
		"CREATE ROLE %s LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS",
		quotedRole,
		pq.QuoteLiteral(runtimePassword),
	)); err != nil {
		t.Fatalf("create runtime role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), "DROP OWNED BY "+quotedRole)
		_, _ = adminDB.ExecContext(context.Background(), "DROP ROLE "+quotedRole)
	})
	for _, statement := range []string{
		"GRANT USAGE ON SCHEMA public TO " + quotedRole,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + quotedRole,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + quotedRole,
		"GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO " + quotedRole,
	} {
		if _, err := adminDB.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("grant runtime role: %v", err)
		}
	}

	var ownerA, ownerB, platformAdmin, salonA, salonB string
	fixturePrefix := "rls-" + suffix
	if err := adminDB.QueryRowContext(context.Background(), `
		INSERT INTO users (email,password_hash,full_name)
		VALUES ($1,'test-hash','RLS Owner A') RETURNING id::text
	`, fixturePrefix+"-a@example.test").Scan(&ownerA); err != nil {
		t.Fatalf("insert owner A: %v", err)
	}
	if err := adminDB.QueryRowContext(context.Background(), `
		INSERT INTO users (email,password_hash,full_name)
		VALUES ($1,'test-hash','RLS Owner B') RETURNING id::text
	`, fixturePrefix+"-b@example.test").Scan(&ownerB); err != nil {
		t.Fatalf("insert owner B: %v", err)
	}
	if err := adminDB.QueryRowContext(context.Background(), `
		INSERT INTO users (email,password_hash,full_name,principal_scope)
		VALUES ($1,'test-hash','RLS Platform Admin','platform') RETURNING id::text
	`, fixturePrefix+"-platform@example.test").Scan(&platformAdmin); err != nil {
		t.Fatalf("insert platform admin: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO platform_role_assignments (user_id, role_id, created_by_user_id, updated_by_user_id)
		SELECT $1, id, $1, $1 FROM roles WHERE name='platform_admin'
	`, platformAdmin); err != nil {
		t.Fatalf("assign platform admin: %v", err)
	}
	if err := adminDB.QueryRowContext(context.Background(), `
		INSERT INTO salons (name,phone,owner_user_id,public_slug,public_catalog_enabled)
		VALUES ('RLS Salon A','555-6801',$1,'rls-a-' || $2,true) RETURNING id::text
	`, ownerA, suffix).Scan(&salonA); err != nil {
		t.Fatalf("insert salon A: %v", err)
	}
	if err := adminDB.QueryRowContext(context.Background(), `
		INSERT INTO salons (name,phone,owner_user_id,public_slug,public_catalog_enabled)
		VALUES ('RLS Salon B','555-6802',$1,'rls-b-' || $2,false) RETURNING id::text
	`, ownerB, suffix).Scan(&salonB); err != nil {
		t.Fatalf("insert salon B: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO salon_settings (salon_id,scheduling_authority,booking_mode)
		VALUES ($1,'owner_manual','pending_approval'),($2,'owner_manual','pending_approval')
	`, salonA, salonB); err != nil {
		t.Fatalf("insert salon settings fixtures: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonA, salonB)
		_, _ = adminDB.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2,$3)`, ownerA, ownerB, platformAdmin)
	})
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO services (salon_id,pos_provider,pos_service_id,name,duration_minutes,active,ai_bookable)
		VALUES ($1,'local',$3 || '-a','Tenant A Service',30,true,true),
		       ($2,'local',$3 || '-b','Tenant B Service',30,true,true)
	`, salonA, salonB, fixturePrefix); err != nil {
		t.Fatalf("insert services: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO customers (salon_id,name,phone,normalized_phone)
		VALUES ($1,'RLS Customer','555-6803','5556803')
	`, salonA); err != nil {
		t.Fatalf("insert customer fixture: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO call_sessions (salon_id,customer_name,customer_phone)
		VALUES ($1,'RLS Caller A','555-6804'),
		       ($2,'RLS Caller B','555-6806')
	`, salonA, salonB); err != nil {
		t.Fatalf("insert call fixture: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		WITH attempt AS (
			INSERT INTO booking_attempts (
				salon_id,status,customer_name,customer_phone,requested_start_time,requested_end_time
			) VALUES ($1,'started','RLS Appointment','555-6805',now() + interval '1 day',now() + interval '1 day 30 minutes')
			RETURNING id
		)
		INSERT INTO appointments (
			salon_id,booking_attempt_id,pos_appointment_id,status,customer_name,customer_phone,start_time,end_time
		)
		SELECT $1,id,$2 || '-appointment','confirmed','RLS Appointment','555-6805',now() + interval '1 day',now() + interval '1 day 30 minutes'
		FROM attempt
	`, salonA, fixturePrefix); err != nil {
		t.Fatalf("insert appointment fixture: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO owner_notifications (salon_id,type,title,message)
		VALUES ($1,'owner_review','RLS Notification','Sensitive owner notification')
	`, salonA); err != nil {
		t.Fatalf("insert notification fixture: %v", err)
	}

	runtimeURL, err := runtimeRoleURL(adminURL, runtimeRole, runtimePassword)
	if err != nil {
		t.Fatalf("build runtime URL: %v", err)
	}
	runtimeDB, err := Open(context.Background(), runtimeURL)
	if err != nil {
		t.Fatalf("open runtime database: %v", err)
	}
	defer runtimeDB.Close()
	if err := VerifyRuntimeRLS(context.Background(), runtimeDB, runtimeRole); err != nil {
		t.Fatalf("verify runtime RLS: %v", err)
	}

	assertServiceNames := func(ctx context.Context, want string) {
		t.Helper()
		rows, err := runtimeDB.QueryContext(ctx, `SELECT name FROM services WHERE salon_id IN ($1,$2) ORDER BY name`, salonA, salonB)
		if err != nil {
			t.Fatalf("query runtime services: %v", err)
		}
		defer rows.Close()
		names := make([]string, 0, 2)
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan service: %v", err)
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate services: %v", err)
		}
		if got := strings.Join(names, ","); got != want {
			t.Fatalf("service visibility=%q, want %q", got, want)
		}
	}

	assertServiceNames(context.Background(), "")
	assertServiceNames(databasecontext.WithActor(context.Background(), ownerA), "Tenant A Service")
	assertServiceNames(databasecontext.WithActor(context.Background(), ownerB), "Tenant B Service")
	assertServiceNames(databasecontext.WithScope(context.Background(), databasecontext.ScopePublic), "")
	assertServiceNames(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker), "")
	assertServiceNames(databasecontext.WithScope(context.Background(), databasecontext.ScopeProvider), "")
	assertServiceNames(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), "Tenant A Service")
	assertServiceNames(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonB), "Tenant B Service")
	assertServiceNames(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonA), "Tenant A Service")
	assertServiceNames(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonB), "Tenant B Service")

	assertSystemRowCount := func(ctx context.Context, table string, want int) {
		t.Helper()
		var got int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE salon_id IN ($1,$2)", pq.QuoteIdentifier(table))
		if err := runtimeDB.QueryRowContext(ctx, query, salonA, salonB).Scan(&got); err != nil {
			t.Fatalf("query %s system visibility: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s system visibility=%d, want %d", table, got, want)
		}
	}
	assertSystemRowCount(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker), "call_sessions", 0)
	assertSystemRowCount(databasecontext.WithScope(context.Background(), databasecontext.ScopeProvider), "call_sessions", 0)
	assertSystemRowCount(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), "call_sessions", 1)
	assertSystemRowCount(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonB), "call_sessions", 1)

	assertServiceUpdates := func(ctx context.Context, targetSalonID string, want int64) {
		t.Helper()
		result, err := runtimeDB.ExecContext(ctx, `UPDATE services SET name=name WHERE salon_id=$1`, targetSalonID)
		if err != nil {
			t.Fatalf("update runtime service: %v", err)
		}
		got, err := result.RowsAffected()
		if err != nil {
			t.Fatalf("read updated service count: %v", err)
		}
		if got != want {
			t.Fatalf("updated services=%d, want %d", got, want)
		}
	}
	assertServiceUpdates(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker), salonA, 0)
	assertServiceUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), salonA, 1)
	assertServiceUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), salonB, 0)
	assertServiceUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonB), salonB, 1)
	assertServiceUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonB), salonA, 0)

	var locatedSalonID string
	if err := runtimeDB.QueryRowContext(
		databasecontext.WithScope(context.Background(), databasecontext.ScopeProvider),
		`SELECT public.app_provider_voice_phone_salon($1)::text`,
		"555-6801",
	).Scan(&locatedSalonID); err != nil {
		t.Fatalf("locate provider voice salon: %v", err)
	}
	if locatedSalonID != salonA {
		t.Fatalf("located provider salon=%q, want %q", locatedSalonID, salonA)
	}

	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO pos_sync_jobs(salon_id,provider,entity_type,entity_id,operation)
		VALUES ($1,'square','service',gen_random_uuid(),'upsert_service'),
		       ($2,'square','service',gen_random_uuid(),'upsert_service')
	`, salonA, salonB); err != nil {
		t.Fatalf("insert worker discovery fixtures: %v", err)
	}
	claimWorkerJobs := func(ctx context.Context) int {
		t.Helper()
		rows, err := runtimeDB.QueryContext(ctx, `SELECT job_id FROM public.app_worker_claim_pos_sync_jobs(10)`)
		if err != nil {
			t.Fatalf("claim worker jobs: %v", err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate worker claims: %v", err)
		}
		return count
	}
	if got := claimWorkerJobs(context.Background()); got != 0 {
		t.Fatalf("unscoped runtime worker claims=%d, want 0", got)
	}
	if got := claimWorkerJobs(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA)); got != 0 {
		t.Fatalf("tenant-bound worker discovery claims=%d, want 0", got)
	}
	if got := claimWorkerJobs(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker)); got != 2 {
		t.Fatalf("unbound worker discovery claims=%d, want 2", got)
	}

	publicContext := databasecontext.WithScope(context.Background(), databasecontext.ScopePublic)
	var publicRaw string
	if err := runtimeDB.QueryRowContext(publicContext, `SELECT public.read_public_catalog($1)::text`, "rls-a-"+suffix).Scan(&publicRaw); err != nil {
		t.Fatalf("read safe public catalog: %v", err)
	}
	var publicCatalog map[string]any
	if err := json.Unmarshal([]byte(publicRaw), &publicCatalog); err != nil {
		t.Fatalf("decode safe public catalog: %v", err)
	}
	if !strings.Contains(publicRaw, "Tenant A Service") {
		t.Fatalf("safe public catalog omitted published service: %s", publicRaw)
	}
	for _, forbidden := range []string{"owner_user_id", "access_token", "provider_entity_id", "555-6804", "RLS Customer"} {
		if strings.Contains(publicRaw, forbidden) {
			t.Fatalf("safe public catalog leaked %q: %s", forbidden, publicRaw)
		}
	}

	platformContext := databasecontext.WithActor(context.Background(), platformAdmin)
	assertCount := func(ctx context.Context, table string, want int) {
		t.Helper()
		var got int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE salon_id=$1", pq.QuoteIdentifier(table))
		if err := runtimeDB.QueryRowContext(ctx, query, salonA).Scan(&got); err != nil {
			t.Fatalf("query %s visibility: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s visibility=%d, want %d", table, got, want)
		}
	}
	// V76 gives Platform Admin direct capability-backed control-plane access,
	// including the corresponding PII scope. Platform Ops remains grant-bound.
	for _, table := range []string{"customers", "call_sessions", "appointments", "owner_notifications"} {
		assertCount(platformContext, table, 1)
	}

	for _, scope := range []string{"customers", "calls", "appointments", "notifications"} {
		if _, err := adminDB.ExecContext(context.Background(), `
			INSERT INTO platform_pii_access_grants (
				salon_id,user_id,scope,reason,expires_at,created_by_user_id
			) VALUES ($1,$2,$3,'rls-test-grant',now() + interval '1 hour',$2)
		`, salonA, platformAdmin, scope); err != nil {
			t.Fatalf("grant Platform PII scope %s: %v", scope, err)
		}
	}
	assertCount(platformContext, "customers", 1)
	assertCount(platformContext, "call_sessions", 1)
	assertCount(platformContext, "appointments", 1)
	assertCount(platformContext, "owner_notifications", 1)
}

func runtimeRoleURL(adminURL, role, password string) (string, error) {
	parsed, err := url.Parse(adminURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("RLS integration requires a postgres URL")
	}
	parsed.User = url.UserPassword(role, password)
	return parsed.String(), nil
}
