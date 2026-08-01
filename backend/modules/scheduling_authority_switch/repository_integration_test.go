package scheduling_authority_switch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

func TestRepositoryPostgresPreviewReplayTenantFenceAndZeroOperationalMutation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	ctx := context.Background()
	ownerID := insertSwitchTestUser(t, ctx, db, "switch-owner")
	otherOwnerID := insertSwitchTestUser(t, ctx, db, "switch-other-owner")
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id, timezone, active_pos_provider)
		VALUES ('Authority Switch Test Salon', '+13125550991', $1, 'America/Chicago', 'square')
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, otherOwnerID)
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings (salon_id, scheduling_authority) VALUES ($1, 'external_provider')`, salonID); err != nil {
		t.Fatalf("insert salon settings: %v", err)
	}
	var serviceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id, pos_provider, pos_service_id, name, duration_minutes, active, ai_bookable)
		VALUES ($1, 'square', $2, 'Dynamic Eligible Service', 45, true, true)
		RETURNING id::text
	`, salonID, "switch-service-"+uuid.NewString()).Scan(&serviceID); err != nil {
		t.Fatalf("insert eligible service: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id, provider, status, location_id, last_sync_at)
		VALUES ($1, 'square', 'active', 'switch-location', now())
	`, salonID); err != nil {
		t.Fatalf("insert provider connection evidence: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (salon_id, entity_type, entity_id, provider, provider_entity_id, provider_version, sync_status, last_synced_at)
		VALUES ($1, 'service', $2, 'square', 'switch-provider-service', 7, 'synced', now())
	`, salonID, serviceID); err != nil {
		t.Fatalf("insert provider link evidence: %v", err)
	}
	var providerSwitchRunID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO pos_provider_switch_runs (salon_id, from_provider, to_provider, status, created_by_user_id)
		VALUES ($1, 'square', 'future-provider', 'needs_review', $2)
		RETURNING id::text
	`, salonID, ownerID).Scan(&providerSwitchRunID); err != nil {
		t.Fatalf("insert POS switch run evidence: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_provider_switch_matches (run_id, salon_id, entity_type, provider_entity_id, provider_name, match_status, match_confidence)
		VALUES ($1, $2, 'service', 'future-service', 'Future Service', 'unmatched', 0)
	`, providerSwitchRunID, salonID); err != nil {
		t.Fatalf("insert POS switch match evidence: %v", err)
	}

	before := switchOperationalState(t, ctx, db, salonID)
	service := NewService(NewRepository(db), nil, nil, true)
	request := PreviewRequest{
		OperationKey:                   "pg-preview-" + uuid.NewString(),
		SourceSchedulingAuthority:      TargetExternalProvider,
		TargetSchedulingAuthority:      TargetOwnerManual,
		ExpectedSourceAuthorityVersion: 1,
	}
	created, err := service.Preview(ctx, salonID, ownerID, request)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if created.Replayed || created.SwitchRun.Status != StatusPreviewReady || !created.SwitchRun.ReadinessSnapshot.Ready {
		t.Fatalf("created preview=%#v", created)
	}
	replayed, err := service.Preview(ctx, salonID, ownerID, request)
	if err != nil || !replayed.Replayed || replayed.SwitchRun.ID != created.SwitchRun.ID {
		t.Fatalf("exact replay=%#v err=%v", replayed, err)
	}
	changed := request
	changed.TargetSchedulingAuthority = TargetManleAICalendar
	if _, err := service.Preview(ctx, salonID, ownerID, changed); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("changed fingerprint error=%v, want ErrOperationConflict", err)
	}
	stale := request
	stale.OperationKey = "pg-stale-" + uuid.NewString()
	stale.ExpectedSourceAuthorityVersion = 2
	if _, err := service.Preview(ctx, salonID, ownerID, stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale version error=%v, want ErrVersionConflict", err)
	}
	if _, err := service.Get(ctx, salonID, otherOwnerID, created.SwitchRun.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get error=%v, want ErrNotFound", err)
	}
	latest, err := service.Latest(ctx, salonID, ownerID)
	if err != nil || latest.SwitchRun.ID != created.SwitchRun.ID {
		t.Fatalf("latest=%#v err=%v", latest, err)
	}

	after := switchOperationalState(t, ctx, db, salonID)
	if before != after {
		t.Fatalf("preview mutated operational state\nbefore=%s\nafter=%s", before, after)
	}
	var runCount, eventCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM scheduling_authority_switch_runs WHERE salon_id = $1`, salonID).Scan(&runCount); err != nil {
		t.Fatalf("count switch runs: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM scheduling_authority_switch_events WHERE salon_id = $1`, salonID).Scan(&eventCount); err != nil {
		t.Fatalf("count switch events: %v", err)
	}
	if runCount != 1 || eventCount != 1 {
		t.Fatalf("durable audit rows runs=%d events=%d, want 1/1", runCount, eventCount)
	}
}

func TestRepositoryPostgresCommitRollbackConcurrencyReplayAndLiveLeaseFence(t *testing.T) {
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
	ownerID := insertSwitchTestUser(t, ctx, db, "phase5b-owner")
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name,phone,owner_user_id,timezone,active_pos_provider)
		VALUES ('Phase 5B Switch Salon','+13125550992',$1,'America/Chicago','square') RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert Phase 5B salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings (salon_id,scheduling_authority,booking_mode) VALUES ($1,'external_provider','pending_approval')`, salonID); err != nil {
		t.Fatalf("insert Phase 5B settings: %v", err)
	}
	var serviceID, staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes,active,ai_bookable,sync_status)
		VALUES ($1,'square','phase5b-service',9,'Phase 5B Service',45,true,true,'synced') RETURNING id::text
	`, salonID).Scan(&serviceID); err != nil {
		t.Fatalf("insert Phase 5B service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id,pos_provider,pos_staff_id,name,active,ai_bookable,sync_status)
		VALUES ($1,'square','phase5b-staff','Phase 5B Staff',true,true,'synced') RETURNING id::text
	`, salonID).Scan(&staffID); err != nil {
		t.Fatalf("insert Phase 5B staff: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (salon_id,provider,status,location_id,snapshot_generation,scopes,last_sync_at)
		VALUES ($1,'square','active','phase5b-location',6,ARRAY['APPOINTMENTS_WRITE'],now())
	`, salonID); err != nil {
		t.Fatalf("insert Phase 5B connection: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_integration_configs(salon_id,provider,enabled,settings)
		VALUES($1,'square',true,'{"api_version":"2026-05-20"}'::jsonb)
	`, salonID); err != nil {
		t.Fatalf("insert Phase 5B Square config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version)
		VALUES($1,'integration_config','square',1)
		ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET version=1
	`, salonID); err != nil {
		t.Fatalf("insert Phase 5B Square config version: %v", err)
	}
	var connectionCapabilityVersion int64
	if err := db.QueryRowContext(ctx, `
		SELECT booking_write_capability_version
		FROM pos_connections
		WHERE salon_id=$1 AND provider='square'
	`, salonID).Scan(&connectionCapabilityVersion); err != nil {
		t.Fatalf("load Phase 5B connection capability version: %v", err)
	}
	if capability, replayed, err := pos.NewRepository(db).ReevaluateSquareSchedulingCapability(ctx, pos.SchedulingCapabilityEvaluationInput{
		SalonID: salonID, ActorUserID: ownerID, ActionKey: "phase5b-square-capability",
		RequestFingerprint: strings.Repeat("9", 64), ExpectedConnectionCapabilityVersion: connectionCapabilityVersion,
		ExpectedIntegrationConfigVersion: 1,
	}); err != nil || replayed || !capability.AutomaticSingleCreate || !capability.EvidenceCurrent {
		t.Fatalf("seed Phase 5B Square capability result/replay/error=%#v/%t/%v", capability, replayed, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (salon_id,entity_type,entity_id,provider,provider_entity_id,provider_version,sync_status,last_synced_at)
		VALUES ($1,'service',$2,'square','phase5b-service',9,'synced',now()),
		       ($1,'staff',$3,'square','phase5b-staff',NULL,'synced',now())
	`, salonID, serviceID, staffID); err != nil {
		t.Fatalf("insert Phase 5B provider links: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (salon_id,day_of_week,start_local_time,end_local_time,source,provider,provider_location_id,provider_period_index,last_synced_at)
		VALUES ($1,1,'09:00','17:00','imported','square','phase5b-location',1,now())
	`, salonID); err != nil {
		t.Fatalf("insert Phase 5B business hours: %v", err)
	}

	externalReadiness := pos_square.NewService(pos.NewRepository(db), nil, "", nil)
	service := NewService(NewRepository(db), nil, externalReadiness, true)
	preview := func(source, target string, version int64, rollbackID string) *SwitchRun {
		t.Helper()
		response, err := service.Preview(ctx, salonID, ownerID, PreviewRequest{
			OperationKey: "phase5b-preview-" + uuid.NewString(), SourceSchedulingAuthority: source,
			TargetSchedulingAuthority: target, ExpectedSourceAuthorityVersion: version, RollbackOfSwitchRunID: rollbackID,
		})
		if err != nil || response.SwitchRun.Status != StatusPreviewReady {
			t.Fatalf("preview %s->%s v%d response=%#v err=%v", source, target, version, response, err)
		}
		return response.SwitchRun
	}
	commit := func(run *SwitchRun, action string) *PreviewResponse {
		t.Helper()
		response, err := service.Commit(ctx, salonID, ownerID, run.ID, CommitRequest{ActionKey: action})
		if err != nil {
			t.Fatalf("commit run %s: %v", run.ID, err)
		}
		return response
	}

	forward := preview(TargetExternalProvider, TargetOwnerManual, 1, "")
	committedForward := commit(forward, "phase5b-commit-forward")
	if committedForward.Replayed || committedForward.SwitchRun.Status != "committed" {
		t.Fatalf("forward commit=%#v", committedForward)
	}
	if _, err := db.ExecContext(ctx, `UPDATE services SET ai_bookable=false WHERE id=$1`, serviceID); err != nil {
		t.Fatalf("drift owner readiness after commit: %v", err)
	}
	replayed, err := service.Commit(ctx, salonID, ownerID, forward.ID, CommitRequest{ActionKey: "phase5b-commit-forward"})
	if err != nil || !replayed.Replayed || replayed.SwitchRun.ID != forward.ID {
		t.Fatalf("exact commit replay after drift=%#v err=%v", replayed, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE services SET ai_bookable=true WHERE id=$1`, serviceID); err != nil {
		t.Fatalf("restore owner readiness: %v", err)
	}

	rollback := preview(TargetOwnerManual, TargetExternalProvider, 2, forward.ID)
	committedRollback := commit(rollback, "phase5b-commit-rollback")
	if committedRollback.SwitchRun.RollbackOfSwitchRunID != forward.ID {
		t.Fatalf("rollback reference=%q, want %q", committedRollback.SwitchRun.RollbackOfSwitchRunID, forward.ID)
	}

	concurrentRun := preview(TargetExternalProvider, TargetOwnerManual, 3, "")
	type commitResult struct {
		response *PreviewResponse
		err      error
		action   string
	}
	results := make(chan commitResult, 2)
	var wg sync.WaitGroup
	for _, action := range []string{"phase5b-concurrent-a", "phase5b-concurrent-b"} {
		wg.Add(1)
		go func(action string) {
			defer wg.Done()
			response, err := service.Commit(context.Background(), salonID, ownerID, concurrentRun.ID, CommitRequest{ActionKey: action})
			results <- commitResult{response: response, err: err, action: action}
		}(action)
	}
	wg.Wait()
	close(results)
	successes := 0
	winnerAction := ""
	stateConflicts := 0
	for result := range results {
		if result.err == nil {
			successes++
			winnerAction = result.action
		} else if errors.Is(result.err, ErrStateConflict) || errors.Is(result.err, ErrVersionConflict) {
			stateConflicts++
		} else {
			t.Fatalf("unexpected concurrent commit error: %v", result.err)
		}
	}
	if successes != 1 || stateConflicts != 1 {
		t.Fatalf("concurrent commits successes/conflicts=%d/%d", successes, stateConflicts)
	}
	var authority string
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT scheduling_authority,scheduling_authority_version FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&authority, &version); err != nil {
		t.Fatalf("read concurrent authority result: %v", err)
	}
	if authority != TargetOwnerManual || version != 4 {
		t.Fatalf("concurrent authority/version=%s/%d, want owner_manual/4", authority, version)
	}
	winnerReplay, err := service.Commit(ctx, salonID, ownerID, concurrentRun.ID, CommitRequest{ActionKey: winnerAction})
	if err != nil || !winnerReplay.Replayed {
		t.Fatalf("winner exact replay=%#v err=%v", winnerReplay, err)
	}

	backToExternal := preview(TargetOwnerManual, TargetExternalProvider, 4, concurrentRun.ID)
	commit(backToExternal, "phase5b-back-to-external")
	liveBlockedRun := preview(TargetExternalProvider, TargetOwnerManual, 5, "")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id,source,status,pos_provider,operation_key,request_fingerprint,operation_type,
			processing_token,processing_lease_expires_at,provider_outcome,retry_policy,reconciliation_status,
			customer_name,customer_phone,requested_start_time,requested_end_time,scheduling_authority,target_pos_booking_version
		) VALUES ($1,'owner_dashboard','pos_pending','square',$2,$3,'book',$4,now()-interval '1 hour',
		          'not_started','none','not_required','Live Caller','+13125550199',now()+interval '1 day',now()+interval '1 day 45 minutes','external_provider',0)
	`, salonID, "phase5b-live-"+uuid.NewString(), strings.Repeat("a", 64), uuid.NewString()); err != nil {
		t.Fatalf("insert expired live external lease: %v", err)
	}
	if _, err := service.Commit(ctx, salonID, ownerID, liveBlockedRun.ID, CommitRequest{ActionKey: "phase5b-live-blocked"}); !errors.Is(err, ErrLiveExecution) {
		t.Fatalf("live lease commit error=%v, want ErrLiveExecution", err)
	}
	var runStatus string
	var commitEvents int
	if err := db.QueryRowContext(ctx, `SELECT status FROM scheduling_authority_switch_runs WHERE id=$1`, liveBlockedRun.ID).Scan(&runStatus); err != nil {
		t.Fatalf("read live-blocked run: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM scheduling_authority_switch_events WHERE switch_run_id=$1 AND event_type='commit'`, liveBlockedRun.ID).Scan(&commitEvents); err != nil {
		t.Fatalf("count live-blocked commit events: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT scheduling_authority,scheduling_authority_version FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&authority, &version); err != nil {
		t.Fatalf("read live-blocked authority: %v", err)
	}
	if runStatus != StatusPreviewReady || commitEvents != 0 || authority != TargetExternalProvider || version != 5 {
		t.Fatalf("live-blocked partial state run/events/authority/version=%s/%d/%s/%d", runStatus, commitEvents, authority, version)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM booking_attempts WHERE salon_id=$1 AND operation_key LIKE 'phase5b-live-%'`, salonID); err != nil {
		t.Fatalf("clear live execution fixture: %v", err)
	}
	commit(liveBlockedRun, "phase5b-live-cleared")
	externalDriftRun := preview(TargetOwnerManual, TargetExternalProvider, 6, liveBlockedRun.ID)
	mutationTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin external readiness mutation: %v", err)
	}
	defer mutationTx.Rollback()
	if _, err := mutationTx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(salonID)); err != nil {
		t.Fatalf("lock external readiness mutation: %v", err)
	}
	if _, err := mutationTx.ExecContext(ctx, `
		INSERT INTO pos_errors (salon_id,provider,operation,error_code,error_message)
		VALUES ($1,'square','create_booking','POS_PERMISSION_DENIED','write permission removed after preview')
	`, salonID); err != nil {
		t.Fatalf("insert lock-scoped external readiness drift: %v", err)
	}
	driftResult := make(chan error, 1)
	go func() {
		_, commitErr := service.Commit(context.Background(), salonID, ownerID, externalDriftRun.ID, CommitRequest{ActionKey: "phase5b-external-drift"})
		driftResult <- commitErr
	}()
	select {
	case early := <-driftResult:
		t.Fatalf("external readiness commit returned before mutation fence release: %v", early)
	case <-time.After(75 * time.Millisecond):
	}
	if err := mutationTx.Commit(); err != nil {
		t.Fatalf("commit external readiness mutation: %v", err)
	}
	select {
	case commitErr := <-driftResult:
		if !errors.Is(commitErr, ErrReadinessConflict) {
			t.Fatalf("external write-readiness drift error=%v, want ErrReadinessConflict", commitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("external readiness commit did not resume after mutation fence release")
	}
	if err := db.QueryRowContext(ctx, `SELECT scheduling_authority,scheduling_authority_version FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&authority, &version); err != nil {
		t.Fatalf("read external-drift authority: %v", err)
	}
	if authority != TargetOwnerManual || version != 6 {
		t.Fatalf("external readiness drift partially switched authority/version=%s/%d", authority, version)
	}
}

func insertSwitchTestUser(t *testing.T, ctx context.Context, db *sql.DB, prefix string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Authority Switch Test Owner')
		RETURNING id::text
	`, prefix+"-"+uuid.NewString()+"@example.com").Scan(&id); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return id
}

func switchOperationalState(t *testing.T, ctx context.Context, db *sql.DB, salonID string) string {
	t.Helper()
	var authority, activeProvider string
	var version int64
	if err := db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority, settings.scheduling_authority_version,
		       COALESCE(salon.active_pos_provider, '')
		FROM salon_settings settings
		JOIN salons salon ON salon.id = settings.salon_id
		WHERE settings.salon_id = $1
	`, salonID).Scan(&authority, &version, &activeProvider); err != nil {
		t.Fatalf("load authority fence: %v", err)
	}
	tables := []string{
		"appointments", "booking_attempts", "availability_quotes", "scheduling_requests",
		"pos_connections", "pos_entity_links", "pos_provider_switch_runs", "pos_provider_switch_matches",
	}
	tableState := make([]string, 0, len(tables))
	for _, table := range tables {
		var count int64
		var digest string
		query := fmt.Sprintf(`
			SELECT count(*), md5(COALESCE(string_agg(to_jsonb(item)::text, '|' ORDER BY to_jsonb(item)::text), ''))
			FROM (SELECT * FROM %s WHERE salon_id = $1) item
		`, table)
		if err := db.QueryRowContext(ctx, query, salonID).Scan(&count, &digest); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		tableState = append(tableState, fmt.Sprintf("%s=%d:%s", table, count, digest))
	}
	return fmt.Sprintf("authority=%s version=%d active_provider=%s tables=%v", authority, version, activeProvider, tableState)
}
