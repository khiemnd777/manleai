package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	registration "github.com/manleai/ai-receptionist/modules/tenant_registration"
)

func TestV85RegistrationRuntimeRoleAndMigrationContract(t *testing.T) {
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
		t.Fatalf("first migration run: %v", err)
	}
	if err := Migrate(context.Background(), adminDB); err != nil {
		t.Fatalf("second migration run: %v", err)
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(suffix) > 16 {
		suffix = suffix[len(suffix)-16:]
	}
	runtimeRole := "registration_rls_" + suffix
	runtimePassword := "RegistrationRLS_" + suffix
	quotedRole := pq.QuoteIdentifier(runtimeRole)
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS", quotedRole, pq.QuoteLiteral(runtimePassword))); err != nil {
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

	adminID := insertV85ScopedActor(t, adminDB, suffix+"-admin", "platform", "platform_admin")
	opsID := insertV85ScopedActor(t, adminDB, suffix+"-ops", "platform", "platform_ops")
	tenantID := insertV85ScopedActor(t, adminDB, suffix+"-tenant", "tenant", "")
	runtimeURL, err := runtimeRoleURL(adminURL, runtimeRole, runtimePassword)
	if err != nil {
		t.Fatalf("runtime role URL: %v", err)
	}
	runtimeDB, err := Open(context.Background(), runtimeURL)
	if err != nil {
		t.Fatalf("open runtime database: %v", err)
	}
	defer runtimeDB.Close()
	if err := VerifyRuntimeRLS(context.Background(), runtimeDB, runtimeRole); err != nil {
		t.Fatalf("verify runtime RLS: %v", err)
	}

	repository := registration.NewRepository(runtimeDB)
	service := registration.NewService(repository, nil)
	request := registration.PublicSubmissionRequest{SubmissionKey: uuid.NewString(), ContactFullName: "RLS Applicant", ContactEmail: "rls-" + suffix + "@example.test", ContactPhone: "312-555-0148", SalonName: "RLS Registration Salon", SalonPhone: "773-555-0180", City: "Chicago", State: "IL", ZipCode: "60614", LocationCount: 1, PreferredContactLanguage: "en", Locale: "en", SourcePage: "home", ConsentVersion: registration.ConsentVersion, ContactConsent: true}
	publicCtx := databasecontext.WithScope(context.Background(), databasecontext.ScopePublic)
	created, err := service.Submit(publicCtx, request)
	if err != nil || created.RequestReference == "" {
		t.Fatalf("public scoped submit=%#v error=%v", created, err)
	}
	request.SubmissionKey = uuid.NewString()
	if _, err := service.Submit(context.Background(), request); err == nil {
		t.Fatal("unscoped public create unexpectedly succeeded")
	}

	assertCount := func(ctx context.Context, want int) {
		t.Helper()
		var count int
		if err := runtimeDB.QueryRowContext(ctx, `SELECT count(*) FROM tenant_registration_requests WHERE public_reference=$1`, created.RequestReference).Scan(&count); err != nil {
			t.Fatalf("query registration visibility: %v", err)
		}
		if count != want {
			t.Fatalf("registration visibility=%d, want %d", count, want)
		}
	}
	assertCount(context.Background(), 0)
	assertCount(publicCtx, 0)
	assertCount(databasecontext.WithActor(context.Background(), tenantID), 0)
	assertCount(databasecontext.WithActor(context.Background(), adminID), 1)
	assertCount(databasecontext.WithActor(context.Background(), opsID), 1)

	assertUpdateCount := func(ctx context.Context, want int64) {
		t.Helper()
		result, err := runtimeDB.ExecContext(ctx, `UPDATE tenant_registration_requests SET updated_at=updated_at WHERE public_reference=$1`, created.RequestReference)
		if err != nil {
			t.Fatalf("update registration: %v", err)
		}
		count, err := result.RowsAffected()
		if err != nil || count != want {
			t.Fatalf("update rows=%d error=%v, want %d", count, err, want)
		}
	}
	assertUpdateCount(publicCtx, 0)
	assertUpdateCount(databasecontext.WithActor(context.Background(), tenantID), 0)
	assertUpdateCount(databasecontext.WithActor(context.Background(), opsID), 1)

	assertCapability := func(ctx context.Context, capability string, want bool) {
		t.Helper()
		var allowed bool
		if err := runtimeDB.QueryRowContext(ctx, `SELECT public.app_global_platform_capability($1)`, capability).Scan(&allowed); err != nil {
			t.Fatalf("query capability %s: %v", capability, err)
		}
		if allowed != want {
			t.Fatalf("capability %s allowed=%t, want %t", capability, allowed, want)
		}
	}
	adminCtx := databasecontext.WithActor(context.Background(), adminID)
	opsCtx := databasecontext.WithActor(context.Background(), opsID)
	assertCapability(adminCtx, "platform.registration_requests.read", true)
	assertCapability(adminCtx, "platform.registration_requests.manage", true)
	assertCapability(adminCtx, "platform.tenants.provision", true)
	assertCapability(opsCtx, "platform.registration_requests.read", true)
	assertCapability(opsCtx, "platform.registration_requests.manage", true)
	assertCapability(opsCtx, "platform.tenants.provision", false)
	assertCapability(databasecontext.WithActor(context.Background(), tenantID), "platform.registration_requests.read", false)
}

func insertV85ScopedActor(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, suffix, scope, role string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), `INSERT INTO users(email,password_hash,full_name,status,principal_scope) VALUES($1,'integration-test','V85 RLS Actor','active',$2) RETURNING id::text`, suffix+"@example.test", scope).Scan(&id); err != nil {
		t.Fatalf("insert actor: %v", err)
	}
	if role != "" {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO platform_role_assignments(user_id,role_id,created_by_user_id,updated_by_user_id) SELECT $1,id,$1,$1 FROM roles WHERE name=$2`, id, role); err != nil {
			t.Fatalf("assign role %s: %v", role, err)
		}
	}
	return id
}
