package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	bookingmodule "github.com/manleai/ai-receptionist/modules/booking"
	conversationmodule "github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	calendar "github.com/manleai/ai-receptionist/modules/scheduling_manleai_calendar"
	"github.com/manleai/ai-receptionist/modules/scheduling_owner_manual"
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
	phoneBase := time.Now().UnixNano() % 10000000
	phoneA := fmt.Sprintf("+1555%07d", phoneBase)
	phoneB := fmt.Sprintf("+1555%07d", (phoneBase+1)%10000000)
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
		VALUES ('RLS Salon A',$3,$1,'rls-a-' || $2,true) RETURNING id::text
	`, ownerA, suffix, phoneA).Scan(&salonA); err != nil {
		t.Fatalf("insert salon A: %v", err)
	}
	if err := adminDB.QueryRowContext(context.Background(), `
		INSERT INTO salons (name,phone,owner_user_id,public_slug,public_catalog_enabled)
		VALUES ('RLS Salon B',$3,$1,'rls-b-' || $2,false) RETURNING id::text
	`, ownerB, suffix, phoneB).Scan(&salonB); err != nil {
		t.Fatalf("insert salon B: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO salon_settings (salon_id,scheduling_authority,booking_mode)
		VALUES ($1,'owner_manual','pending_approval'),($2,'owner_manual','pending_approval')
	`, salonA, salonB); err != nil {
		t.Fatalf("insert salon settings fixtures: %v", err)
	}
	t.Cleanup(func() {
		cleanupTx, err := adminDB.BeginTx(context.Background(), &sql.TxOptions{})
		if err != nil {
			return
		}
		defer cleanupTx.Rollback()
		if _, err := cleanupTx.ExecContext(context.Background(), `SET LOCAL session_replication_role='replica'`); err != nil {
			return
		}
		for _, statement := range []string{
			`DELETE FROM openai_runtime_verification_events WHERE salon_id IN ($1,$2)`,
			`DELETE FROM openai_runtime_verification_capabilities WHERE salon_id IN ($1,$2)`,
			`DELETE FROM openai_runtime_verification_runs WHERE salon_id IN ($1,$2)`,
		} {
			if _, err := cleanupTx.ExecContext(context.Background(), statement, salonA, salonB); err != nil {
				return
			}
		}
		if _, err := cleanupTx.ExecContext(context.Background(), `SET LOCAL session_replication_role='origin'`); err != nil {
			return
		}
		if _, err := cleanupTx.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonA, salonB); err != nil {
			return
		}
		if _, err := cleanupTx.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2,$3)`, ownerA, ownerB, platformAdmin); err != nil {
			return
		}
		_ = cleanupTx.Commit()
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
	if _, err := adminDB.ExecContext(context.Background(), `
		WITH configs AS (
			INSERT INTO salon_integration_configs (
				salon_id,provider,enabled,settings,credential_fingerprint_hmac,credential_revision,destination_profile
			) VALUES
				($1,'openai',true,'{}'::jsonb,md5($3) || md5($3),1,'openai_public'),
				($2,'openai',true,'{}'::jsonb,md5($4) || md5($4),1,'openai_public')
			RETURNING id,salon_id
		)
		INSERT INTO openai_runtime_verification_runs (
			salon_id,integration_config_id,actor_user_id,action_key,request_fingerprint,
			config_version,credential_revision,destination_policy_version,verification_contract_version
		)
		SELECT config.salon_id,config.id,salon.owner_user_id,'rls-openai-' || config.salon_id::text,
		       repeat('c',64),1,1,'openai-public-v1','openai-voice-v1'
		FROM configs config JOIN salons salon ON salon.id=config.salon_id
	`, salonA, salonB, fixturePrefix+"-openai-a", fixturePrefix+"-openai-b"); err != nil {
		t.Fatalf("insert OpenAI verification fixtures: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO openai_runtime_verification_capabilities (salon_id,run_id,capability,required,status)
		SELECT salon_id,id,'reply',true,'pending' FROM openai_runtime_verification_runs
		WHERE salon_id IN ($1,$2)
	`, salonA, salonB); err != nil {
		t.Fatalf("insert OpenAI verification capability fixtures: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), `
		INSERT INTO openai_runtime_verification_events (
			salon_id,run_id,event_key,event_fingerprint,event_type,status
		)
		SELECT salon_id,id,'queued',repeat('d',64),'queued','queued'
		FROM openai_runtime_verification_runs WHERE salon_id IN ($1,$2)
	`, salonA, salonB); err != nil {
		t.Fatalf("insert OpenAI verification event fixtures: %v", err)
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
	for _, table := range []string{
		"openai_runtime_verification_runs",
		"openai_runtime_verification_capabilities",
		"openai_runtime_verification_events",
	} {
		assertSystemRowCount(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker), table, 0)
		assertSystemRowCount(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonA), table, 0)
		assertSystemRowCount(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), table, 1)
		assertSystemRowCount(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonB), table, 1)
	}
	assertVerificationRunUpdates := func(ctx context.Context, targetSalonID string, want int64) {
		t.Helper()
		result, err := runtimeDB.ExecContext(ctx, `
			UPDATE openai_runtime_verification_runs SET updated_at=updated_at WHERE salon_id=$1
		`, targetSalonID)
		if err != nil {
			t.Fatalf("update OpenAI verification run: %v", err)
		}
		got, err := result.RowsAffected()
		if err != nil {
			t.Fatalf("read updated OpenAI verification run count: %v", err)
		}
		if got != want {
			t.Fatalf("updated OpenAI verification runs=%d, want %d", got, want)
		}
	}
	assertVerificationRunUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonA), salonA, 0)
	assertVerificationRunUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), salonA, 1)
	assertVerificationRunUpdates(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA), salonB, 0)
	claimOpenAIVerifications := func(ctx context.Context) map[string]int {
		t.Helper()
		rows, err := runtimeDB.QueryContext(ctx, `
			SELECT run_id, salon_id::text FROM public.app_worker_claim_openai_runtime_verifications(10,300000)
		`)
		if err != nil {
			t.Fatalf("claim OpenAI verification runs: %v", err)
		}
		defer rows.Close()
		claimedBySalon := map[string]int{}
		for rows.Next() {
			var runID string
			var claimedSalonID string
			if err := rows.Scan(&runID, &claimedSalonID); err != nil {
				t.Fatalf("scan OpenAI verification claim: %v", err)
			}
			claimedBySalon[claimedSalonID]++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate OpenAI verification claims: %v", err)
		}
		return claimedBySalon
	}
	if got := claimOpenAIVerifications(context.Background()); len(got) != 0 {
		t.Fatalf("unscoped OpenAI verification claims=%v, want none", got)
	}
	if got := claimOpenAIVerifications(databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeWorker, salonA)); len(got) != 0 {
		t.Fatalf("tenant-bound discovery OpenAI verification claims=%v, want none", got)
	}
	if got := claimOpenAIVerifications(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker)); got[salonA] != 1 || got[salonB] != 1 {
		t.Fatalf("unbound worker OpenAI verification claims=%v, want one fixture claim per salon", got)
	}

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
		phoneA,
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

	// A provider-bound live call is a system runtime operation, not an
	// interactive action performed by the salon Owner. Revoking the Owner
	// must still fail interactive actor access closed without interrupting the
	// exact-salon provider runtime.
	if _, err := adminDB.ExecContext(context.Background(), `
		UPDATE salon_memberships
		SET status='revoked', version=version+1, updated_at=now()
		WHERE salon_id=$1 AND user_id=$2
	`, salonA, ownerA); err != nil {
		t.Fatalf("revoke owner membership for provider runtime regression: %v", err)
	}

	ownerContext := databasecontext.WithActor(context.Background(), ownerA)
	providerContext := databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonA)
	unboundProviderContext := databasecontext.WithScope(context.Background(), databasecontext.ScopeProvider)
	crossTenantProviderContext := databasecontext.WithSystemSalon(context.Background(), databasecontext.ScopeProvider, salonB)

	conversationRepo := conversationmodule.NewRepository(runtimeDB)
	if _, err := conversationRepo.GetRuntimeConfig(ownerContext, salonA, ownerA); !errors.Is(err, conversationmodule.ErrNotFound) {
		t.Fatalf("revoked owner runtime config error=%v, want conversation not found", err)
	}
	if _, err := conversationRepo.GetRuntimeConfig(unboundProviderContext, salonA, ownerA); !errors.Is(err, conversationmodule.ErrNotFound) {
		t.Fatalf("unbound provider runtime config error=%v, want conversation not found", err)
	}
	if _, err := conversationRepo.GetRuntimeConfig(crossTenantProviderContext, salonA, ownerA); !errors.Is(err, conversationmodule.ErrNotFound) {
		t.Fatalf("cross-tenant provider runtime config error=%v, want conversation not found", err)
	}
	if _, err := conversationRepo.GetRuntimeConfig(providerContext, salonA, ownerA); err != nil {
		t.Fatalf("exact-salon provider runtime config: %v", err)
	}

	phoneSession, err := conversationRepo.CreateSession(providerContext, conversationmodule.NewSessionRecord{
		SalonID:        salonA,
		OwnerUserID:    ownerA,
		Channel:        conversationmodule.ChannelPhone,
		Provider:       "twilio",
		ProviderCallID: "CA" + suffix,
		InboundPhone:   "+13125556810",
		OutboundPhone:  "+13125556811",
		InitialReply:   "How can I help today?",
	})
	if err != nil {
		t.Fatalf("create exact-salon provider phone session: %v", err)
	}
	phoneSession, err = conversationRepo.SaveTurn(providerContext, conversationmodule.TurnRecord{
		SalonID:               salonA,
		OwnerUserID:           ownerA,
		Session:               *phoneSession,
		ExpectedStateRevision: phoneSession.StateRevision,
		CustomerMessage:       "I need help choosing a service.",
		AIMessage:             "I can help with that.",
		EventKey:              "provider-runtime-turn-" + suffix,
		Update: conversationmodule.SessionUpdate{
			Status:      conversationmodule.StatusActive,
			Intent:      conversationmodule.IntentUnknown,
			Outcome:     conversationmodule.OutcomeCollecting,
			DialogState: phoneSession.DialogState,
		},
	})
	if err != nil {
		t.Fatalf("save exact-salon provider phone turn: %v", err)
	}
	if phoneSession.StateRevision != 1 {
		t.Fatalf("provider phone state revision=%d, want 1", phoneSession.StateRevision)
	}

	salonRepo := salon.NewRepository(runtimeDB)
	if _, err := salonRepo.GetSettings(ownerContext, salonA, ownerA); !errors.Is(err, salon.ErrNotFound) {
		t.Fatalf("revoked owner settings error=%v, want salon not found", err)
	}
	if _, err := salonRepo.GetSettings(providerContext, salonA, ownerA); err != nil {
		t.Fatalf("exact-salon provider settings: %v", err)
	}

	schedulingRepo := scheduling.NewRepository(runtimeDB)
	if _, err := schedulingRepo.ResolveSchedulingAuthority(ownerContext, salonA, ownerA); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("revoked owner scheduling authority error=%v, want POS not found", err)
	}
	if authority, err := schedulingRepo.ResolveSchedulingAuthority(providerContext, salonA, ownerA); err != nil {
		t.Fatalf("exact-salon provider scheduling authority: %v", err)
	} else if authority != bookingmodule.SchedulingAuthorityOwnerManual {
		t.Fatalf("provider scheduling authority=%q, want owner_manual", authority)
	}

	calendarRepo := calendar.NewRepository(runtimeDB)
	if _, err := calendarRepo.GetAggregate(ownerContext, salonA, ownerA); !errors.Is(err, calendar.ErrNotFound) {
		t.Fatalf("revoked owner internal-calendar aggregate error=%v, want calendar not found", err)
	}
	if aggregate, err := calendarRepo.GetAggregate(providerContext, salonA, ownerA); err != nil {
		t.Fatalf("exact-salon provider internal-calendar aggregate: %v", err)
	} else if aggregate.SchedulingAuthority != bookingmodule.SchedulingAuthorityOwnerManual {
		t.Fatalf("provider internal-calendar authority=%q, want owner_manual", aggregate.SchedulingAuthority)
	}

	bookingRepo := bookingmodule.NewRepository(runtimeDB)
	if err := bookingRepo.EnsureSalonOwner(ownerContext, salonA, ownerA); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("revoked owner booking access error=%v, want POS not found", err)
	}
	if err := bookingRepo.EnsureSalonOwner(providerContext, salonA, ownerA); err != nil {
		t.Fatalf("exact-salon provider booking access: %v", err)
	}

	ownerManualRepo := scheduling_owner_manual.NewRepository(runtimeDB)
	if _, _, err := ownerManualRepo.SchedulingTargetReadinessFacts(ownerContext, salonA, ownerA); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("revoked owner manual readiness error=%v, want POS not found", err)
	}
	if _, serviceCount, err := ownerManualRepo.SchedulingTargetReadinessFacts(providerContext, salonA, ownerA); err != nil {
		t.Fatalf("exact-salon provider owner-manual readiness: %v", err)
	} else if serviceCount != 1 {
		t.Fatalf("provider owner-manual eligible services=%d, want 1", serviceCount)
	}

	var serviceID string
	if err := adminDB.QueryRowContext(context.Background(), `
		SELECT id::text FROM services WHERE salon_id=$1 AND name='Tenant A Service'
	`, salonA).Scan(&serviceID); err != nil {
		t.Fatalf("load provider runtime service fixture: %v", err)
	}
	voiceRequest := scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "provider-runtime-owner-manual-" + suffix,
		Source:             bookingmodule.SourceAIVoiceCall,
		CallSessionID:      phoneSession.ID,
		CustomerName:       "Provider Runtime Caller",
		CustomerPhone:      "+13125556812",
		RequestedStartTime: time.Now().UTC().Add(48 * time.Hour),
		RequestedTimezone:  "America/Chicago",
		PartySize:          1,
		Segments: []scheduling.ActionSegment{{
			ServiceID:          serviceID,
			StaffSelectionMode: bookingmodule.StaffSelectionAnyone,
			GuestReference:     "guest-1",
			Quantity:           1,
		}},
	}
	if _, _, err := ownerManualRepo.CreateOrReplay(ownerContext, salonA, ownerA, voiceRequest, strings.Repeat("e", 64)); !errors.Is(err, pos.ErrNotFound) {
		t.Fatalf("revoked owner scheduling write error=%v, want POS not found", err)
	}
	createdRequest, replayed, err := ownerManualRepo.CreateOrReplay(providerContext, salonA, ownerA, voiceRequest, strings.Repeat("e", 64))
	if err != nil {
		t.Fatalf("create exact-salon provider owner-manual request: %v", err)
	}
	if replayed || createdRequest.Status != scheduling.SchedulingRequestStatusPending {
		t.Fatalf("provider owner-manual create replayed=%t status=%q, want new pending request", replayed, createdRequest.Status)
	}
	replayedRequest, replayed, err := ownerManualRepo.CreateOrReplay(providerContext, salonA, ownerA, voiceRequest, strings.Repeat("e", 64))
	if err != nil {
		t.Fatalf("replay exact-salon provider owner-manual request: %v", err)
	}
	if !replayed || replayedRequest.ID != createdRequest.ID {
		t.Fatalf("provider owner-manual replay=%t id=%q, want exact request %q", replayed, replayedRequest.ID, createdRequest.ID)
	}
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
