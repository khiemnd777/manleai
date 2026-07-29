package conversation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/modules/booking"
)

type answerContextQueryCounter struct {
	queries atomic.Int64
}

func (c *answerContextQueryCounter) reset() {
	c.queries.Store(0)
}

func (c *answerContextQueryCounter) value() int64 {
	return c.queries.Load()
}

type answerContextCountingDriver struct {
	inner   driver.Driver
	counter *answerContextQueryCounter
}

func (d *answerContextCountingDriver) Open(name string) (driver.Conn, error) {
	connection, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &answerContextCountingConn{Conn: connection, counter: d.counter}, nil
}

type answerContextCountingConn struct {
	driver.Conn
	counter *answerContextQueryCounter
}

func (c *answerContextCountingConn) Prepare(query string) (driver.Stmt, error) {
	statement, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &answerContextCountingStmt{Stmt: statement, counter: c.counter}, nil
}

func (c *answerContextCountingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := c.Conn.(driver.ConnPrepareContext); ok {
		statement, err := preparer.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &answerContextCountingStmt{Stmt: statement, counter: c.counter}, nil
	}
	return c.Prepare(query)
}

func (c *answerContextCountingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != driver.ErrSkip {
		c.counter.queries.Add(1)
	}
	return rows, err
}

func (c *answerContextCountingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	executor, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return executor.ExecContext(ctx, query, args)
}

func (c *answerContextCountingConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *answerContextCountingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

type answerContextCountingStmt struct {
	driver.Stmt
	counter *answerContextQueryCounter
}

func (s *answerContextCountingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.counter.queries.Add(1)
	return s.Stmt.Query(args)
}

func (s *answerContextCountingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.Stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, args)
	if err != driver.ErrSkip {
		s.counter.queries.Add(1)
	}
	return rows, err
}

func openAnswerContextCountingDatabase(t *testing.T) (*sql.DB, *answerContextQueryCounter, *sql.DB) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	counter := &answerContextQueryCounter{}
	driverName := "answer-context-counting-" + uuid.NewString()
	sql.Register(driverName, &answerContextCountingDriver{inner: &pq.Driver{}, counter: counter})
	db, err := sql.Open(driverName, databaseURL)
	if err != nil {
		t.Fatalf("open counting PostgreSQL connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mutationDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open mutation PostgreSQL connection: %v", err)
	}
	t.Cleanup(func() { _ = mutationDB.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping counting PostgreSQL connection: %v", err)
	}
	if err := mutationDB.PingContext(ctx); err != nil {
		t.Fatalf("ping mutation PostgreSQL connection: %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	counter.reset()
	return db, counter, mutationDB
}

func TestPostgresAnswerContextDatabaseRoundTripsByAuthority(t *testing.T) {
	db, counter, mutationDB := openAnswerContextCountingDatabase(t)
	ctx := context.Background()

	tests := []struct {
		name             string
		authority        string
		wantRefreshReads int64
		setup            func(*testing.T, informationalContractTenant)
		mutateFence      func(*testing.T, informationalContractTenant)
	}{
		{
			name: "owner manual", authority: booking.SchedulingAuthorityOwnerManual, wantRefreshReads: 8,
			setup: func(t *testing.T, tenant informationalContractTenant) {
				insertInformationalCatalogFixture(t, ctx, mutationDB, tenant.salonID)
			},
			mutateFence: func(t *testing.T, tenant informationalContractTenant) {
				if _, err := mutationDB.ExecContext(ctx, `
					INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
					VALUES ($1, 'Access note', 'policy', 'Use the courtyard entrance.', 'active', 'owner')
				`, tenant.salonID); err != nil {
					t.Fatalf("mutate owner knowledge fence: %v", err)
				}
			},
		},
		{
			name: "external provider", authority: booking.SchedulingAuthorityExternalProvider, wantRefreshReads: 10,
			setup: func(t *testing.T, tenant informationalContractTenant) {
				insertExternalProviderFixture(t, ctx, mutationDB, tenant)
			},
			mutateFence: func(t *testing.T, tenant informationalContractTenant) {
				if _, err := mutationDB.ExecContext(ctx, `
					UPDATE pos_connections
					SET snapshot_generation = snapshot_generation + 1, last_sync_at = now()
					WHERE salon_id = $1 AND provider = 'square'
				`, tenant.salonID); err != nil {
					t.Fatalf("mutate provider snapshot fence: %v", err)
				}
			},
		},
		{
			name: "ManleAI Calendar", authority: booking.SchedulingAuthorityManleAICalendar, wantRefreshReads: 22,
			setup: func(t *testing.T, tenant informationalContractTenant) {
				insertReadyManleAICalendarFixture(t, ctx, mutationDB, tenant)
			},
			mutateFence: func(t *testing.T, tenant informationalContractTenant) {
				if _, err := mutationDB.ExecContext(ctx, `
					UPDATE manleai_calendar_configs
					SET slot_step_minutes = 20, updated_at = now()
					WHERE salon_id = $1
				`, tenant.salonID); err != nil {
					t.Fatalf("mutate calendar config fence: %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tenant := insertInformationalContractTenant(t, ctx, mutationDB, "query-topology-"+strings.ReplaceAll(test.name, " ", "-"))
			test.setup(t, tenant)
			repository := NewRepository(db)
			service := NewService(repository, &fakeBookingTool{})

			counter.reset()
			answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(ctx, tenant.salonID)
			if err != nil {
				t.Fatalf("cold load: %v", err)
			}
			if answer.SchedulingAuthority != test.authority || diagnostics.refreshReason != answerContextRefreshReasonCold {
				t.Fatalf("cold authority/diagnostics = %q %#v", answer.SchedulingAuthority, diagnostics)
			}
			if got := counter.value(); got != test.wantRefreshReads {
				t.Fatalf("cold database round trips = %d, want %d", got, test.wantRefreshReads)
			}

			counter.reset()
			cached, diagnostics, err := service.loadAnswerContextWithDiagnostics(ctx, tenant.salonID)
			if err != nil {
				t.Fatalf("stable cache hit: %v", err)
			}
			if !cached.CacheHit || diagnostics.outcome != answerContextLoadOutcomeCacheHit || counter.value() != 1 {
				t.Fatalf("stable cache hit context/diagnostics/round trips = %#v %#v %d", cached, diagnostics, counter.value())
			}

			test.mutateFence(t, tenant)
			counter.reset()
			refreshed, diagnostics, err := service.loadAnswerContextWithDiagnostics(ctx, tenant.salonID)
			if err != nil {
				t.Fatalf("stale-fence refresh: %v", err)
			}
			if refreshed.CacheHit || diagnostics.refreshReason != answerContextRefreshReasonFenceMismatch {
				t.Fatalf("stale-fence refresh context/diagnostics = %#v %#v", refreshed, diagnostics)
			}
			if got := counter.value(); got != test.wantRefreshReads {
				t.Fatalf("stale-fence database round trips = %d, want %d", got, test.wantRefreshReads)
			}
		})
	}
}

type answerContextReadinessMutationStore struct {
	*Repository
	mutationDB  *sql.DB
	salonID     string
	once        sync.Once
	mutationErr error
}

func (s *answerContextReadinessMutationStore) GetManleAICalendarAnswerContextEvidence(ctx context.Context, salonID string) (manleAICalendarAnswerContextEvidence, error) {
	s.once.Do(func() {
		_, s.mutationErr = s.mutationDB.ExecContext(ctx, `
			UPDATE manleai_calendar_configs
			SET slot_step_minutes = 20, updated_at = now()
			WHERE salon_id = $1
		`, s.salonID)
	})
	if s.mutationErr != nil {
		return manleAICalendarAnswerContextEvidence{}, s.mutationErr
	}
	return s.Repository.GetManleAICalendarAnswerContextEvidence(ctx, salonID)
}

func TestPostgresAnswerContextReadinessMismatchRoundTrips(t *testing.T) {
	db, counter, mutationDB := openAnswerContextCountingDatabase(t)
	ctx := context.Background()
	tenant := insertInformationalContractTenant(t, ctx, mutationDB, "readiness-round-trips")
	insertReadyManleAICalendarFixture(t, ctx, mutationDB, tenant)
	store := &answerContextReadinessMutationStore{Repository: NewRepository(db), mutationDB: mutationDB, salonID: tenant.salonID}
	service := NewService(store, &fakeBookingTool{})

	counter.reset()
	answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(ctx, tenant.salonID)
	if err != nil {
		t.Fatalf("load across readiness evidence mismatch: %v", err)
	}
	if diagnostics.retryReason != answerContextRetryReasonReadinessMismatch || diagnostics.attempts != 2 ||
		diagnostics.outcome != answerContextLoadOutcomeFailClosed {
		t.Fatalf("readiness-mismatch diagnostics/context = %#v %#v", diagnostics, answer)
	}
	// Each authoritative evidence read is 12 queries: owner lookup plus the
	// 11-query calendar aggregate. The stable second attempt adds three fence
	// reads and eight authority-specific projections: 2*12 + 3 + 8 = 35.
	if got := counter.value(); got != 35 {
		t.Fatalf("readiness-mismatch database round trips = %d, want 35", got)
	}
}

func TestPostgresAnswerContextAuthoritySwitchRoundTrips(t *testing.T) {
	db, counter, mutationDB := openAnswerContextCountingDatabase(t)
	ctx := context.Background()
	tenant := insertInformationalContractTenant(t, ctx, mutationDB, "authority-switch-round-trips")
	insertExternalProviderFixture(t, ctx, mutationDB, tenant)
	if _, err := mutationDB.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'owner_manual', booking_mode = 'pending_approval'
		WHERE salon_id = $1
	`, tenant.salonID); err != nil {
		t.Fatalf("restore owner authority before preload: %v", err)
	}
	service := NewService(NewRepository(db), &fakeBookingTool{})
	if _, _, err := service.loadAnswerContextWithDiagnostics(ctx, tenant.salonID); err != nil {
		t.Fatalf("preload owner authority: %v", err)
	}
	if _, err := mutationDB.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'external_provider', booking_mode = 'confirmed_booking'
		WHERE salon_id = $1
	`, tenant.salonID); err != nil {
		t.Fatalf("switch to external authority: %v", err)
	}

	counter.reset()
	answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(ctx, tenant.salonID)
	if err != nil {
		t.Fatalf("load after authority switch: %v", err)
	}
	if answer.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || diagnostics.refreshReason != answerContextRefreshReasonFenceMismatch {
		t.Fatalf("authority-switch context/diagnostics = %#v %#v", answer, diagnostics)
	}
	if got := counter.value(); got != 10 {
		t.Fatalf("authority-switch database round trips = %d, want external refresh topology 10", got)
	}
}

type answerContextBlockingKnowledgeStore struct {
	*Repository
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
	fenceReads atomic.Int64
}

func (s *answerContextBlockingKnowledgeStore) GetAnswerContextFence(ctx context.Context, salonID string) (AnswerContextFence, error) {
	s.fenceReads.Add(1)
	return s.Repository.GetAnswerContextFence(ctx, salonID)
}

func (s *answerContextBlockingKnowledgeStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	s.once.Do(func() {
		close(s.started)
		<-s.release
	})
	return s.Repository.ListActiveKnowledge(ctx, salonID)
}

type answerContextAlwaysMutatingKnowledgeStore struct {
	*Repository
	mutationDB *sql.DB
	salonID    string
	mutations  atomic.Int64
}

func (s *answerContextAlwaysMutatingKnowledgeStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	mutation := s.mutations.Add(1)
	if _, err := s.mutationDB.ExecContext(ctx, `
		UPDATE knowledge_items
		SET body = $2
		WHERE salon_id = $1 AND title = 'Replica policy'
	`, s.salonID, fmt.Sprintf("Concurrent revision %d.", mutation)); err != nil {
		return nil, err
	}
	return s.Repository.ListActiveKnowledge(ctx, salonID)
}

func TestPostgresAnswerContextMultiReplicaFreshnessTenantIsolationAndRetryExhaustion(t *testing.T) {
	db := openInformationalContractDatabase(t)
	ctx := context.Background()
	tenantA := insertInformationalContractTenant(t, ctx, db, "replica-a")
	tenantB := insertInformationalContractTenant(t, ctx, db, "replica-b")
	for _, tenant := range []informationalContractTenant{tenantA, tenantB} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
			VALUES ($1, 'Replica policy', 'policy', $2, 'active', 'owner')
		`, tenant.salonID, "Initial policy for "+tenant.salonID+"."); err != nil {
			t.Fatalf("insert replica policy: %v", err)
		}
	}

	replicaA := NewService(NewRepository(db), &fakeBookingTool{})
	replicaB := NewService(NewRepository(db), &fakeBookingTool{})
	if replicaA.answerContextCache == replicaB.answerContextCache {
		t.Fatal("replicas unexpectedly share a process-local answer-context cache")
	}
	for _, service := range []*Service{replicaA, replicaB} {
		if _, err := service.loadAnswerContext(ctx, tenantA.salonID); err != nil {
			t.Fatalf("prewarm tenant A replica: %v", err)
		}
	}
	if _, err := replicaB.loadAnswerContext(ctx, tenantB.salonID); err != nil {
		t.Fatalf("prewarm tenant B replica: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE knowledge_items SET body = 'Committed on the writer replica.'
		WHERE salon_id = $1 AND title = 'Replica policy'
	`, tenantA.salonID); err != nil {
		t.Fatalf("commit tenant A mutation: %v", err)
	}
	refreshed, diagnostics, err := replicaB.loadAnswerContextWithDiagnostics(ctx, tenantA.salonID)
	if err != nil {
		t.Fatalf("load tenant A on replica B: %v", err)
	}
	if diagnostics.refreshReason != answerContextRefreshReasonFenceMismatch || len(refreshed.Knowledge) != 1 ||
		refreshed.Knowledge[0].Body != "Committed on the writer replica." {
		t.Fatalf("replica B retained stale tenant A context: %#v %#v", diagnostics, refreshed.Knowledge)
	}
	isolated, diagnostics, err := replicaB.loadAnswerContextWithDiagnostics(ctx, tenantB.salonID)
	if err != nil {
		t.Fatalf("reload isolated tenant B: %v", err)
	}
	if !isolated.CacheHit || diagnostics.outcome != answerContextLoadOutcomeCacheHit || len(isolated.Knowledge) != 1 ||
		!strings.HasPrefix(isolated.Knowledge[0].Body, "Initial policy for ") {
		t.Fatalf("tenant A mutation affected tenant B cache: %#v %#v", diagnostics, isolated.Knowledge)
	}

	blockingStore := &answerContextBlockingKnowledgeStore{
		Repository: NewRepository(db), started: make(chan struct{}), release: make(chan struct{}),
	}
	concurrentReplica := NewService(blockingStore, &fakeBookingTool{})
	result := make(chan struct {
		answer      *AIAnswerContext
		diagnostics answerContextLoadDiagnostics
		err         error
	}, 1)
	go func() {
		answer, loadDiagnostics, loadErr := concurrentReplica.loadAnswerContextWithDiagnostics(ctx, tenantA.salonID)
		result <- struct {
			answer      *AIAnswerContext
			diagnostics answerContextLoadDiagnostics
			err         error
		}{answer: answer, diagnostics: loadDiagnostics, err: loadErr}
	}()
	select {
	case <-blockingStore.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent answer-context projection")
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE knowledge_items SET body = 'Committed while the reader was loading.'
		WHERE salon_id = $1 AND title = 'Replica policy'
	`, tenantA.salonID); err != nil {
		t.Fatalf("commit concurrent tenant A mutation: %v", err)
	}
	close(blockingStore.release)
	var concurrentResult struct {
		answer      *AIAnswerContext
		diagnostics answerContextLoadDiagnostics
		err         error
	}
	select {
	case concurrentResult = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent answer-context load")
	}
	if concurrentResult.err != nil {
		t.Fatalf("concurrent answer-context load: %v", concurrentResult.err)
	}
	if concurrentResult.diagnostics.retryReason != answerContextRetryReasonFenceChanged || concurrentResult.diagnostics.attempts != 2 ||
		len(concurrentResult.answer.Knowledge) != 1 || concurrentResult.answer.Knowledge[0].Body != "Committed while the reader was loading." ||
		blockingStore.fenceReads.Load() != 4 {
		t.Fatalf("concurrent load evidence = diagnostics %#v answer %#v fence_reads=%d", concurrentResult.diagnostics, concurrentResult.answer, blockingStore.fenceReads.Load())
	}

	mutatingStore := &answerContextAlwaysMutatingKnowledgeStore{Repository: NewRepository(db), mutationDB: db, salonID: tenantA.salonID}
	exhaustedReplica := NewService(mutatingStore, &fakeBookingTool{})
	answer, diagnostics, err := exhaustedReplica.loadAnswerContextWithDiagnostics(ctx, tenantA.salonID)
	if err == nil || answer != nil || diagnostics.outcome != answerContextLoadOutcomeRetryExhausted ||
		diagnostics.retryReason != answerContextRetryReasonFenceChanged || diagnostics.attempts != answerContextFenceLoadAttempts {
		t.Fatalf("retry exhaustion did not fail closed: answer=%#v diagnostics=%#v err=%v", answer, diagnostics, err)
	}
	if mutatingStore.mutations.Load() != answerContextFenceLoadAttempts {
		t.Fatalf("retry exhaustion mutations = %d, want %d", mutatingStore.mutations.Load(), answerContextFenceLoadAttempts)
	}
	if _, ok := exhaustedReplica.answerContextCache.get(tenantA.salonID, mustAnswerContextFence(t, ctx, NewRepository(db), tenantA.salonID)); ok {
		t.Fatal("retry exhaustion cached a partially loaded context")
	}
}

type answerContextExplainNode struct {
	NodeType         string                     `json:"Node Type"`
	RelationName     string                     `json:"Relation Name"`
	IndexName        string                     `json:"Index Name"`
	ActualRows       float64                    `json:"Actual Rows"`
	ActualLoops      float64                    `json:"Actual Loops"`
	SharedHitBlocks  int64                      `json:"Shared Hit Blocks"`
	SharedReadBlocks int64                      `json:"Shared Read Blocks"`
	Plans            []answerContextExplainNode `json:"Plans"`
}

type answerContextExplainResult struct {
	Plan          answerContextExplainNode `json:"Plan"`
	PlanningTime  float64                  `json:"Planning Time"`
	ExecutionTime float64                  `json:"Execution Time"`
}

func TestPostgresAnswerContextFenceExplainBoundedSyntheticCatalogs(t *testing.T) {
	db := openInformationalContractDatabase(t)
	ctx := context.Background()
	fixtures := []struct {
		name     string
		services int
		staff    int
	}{
		{name: "small", services: 5, staff: 3},
		{name: "medium", services: 100, staff: 40},
		{name: "high_bounded", services: 500, staff: 150},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			tenant := insertInformationalContractTenant(t, ctx, db, "explain-"+fixture.name)
			insertAnswerContextSyntheticCatalog(t, ctx, db, tenant.salonID, fixture.name, fixture.services, fixture.staff)
			var raw []byte
			if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+answerContextFenceQuery, tenant.salonID).Scan(&raw); err != nil {
				t.Fatalf("EXPLAIN answer-context fence: %v", err)
			}
			var explained []answerContextExplainResult
			if err := json.Unmarshal(raw, &explained); err != nil || len(explained) != 1 {
				t.Fatalf("decode EXPLAIN JSON: len=%d err=%v payload=%s", len(explained), err, string(raw))
			}
			plan := explained[0].Plan
			if plan.ActualRows != 1 || plan.ActualLoops != 1 {
				t.Fatalf("fence plan multiplied rows: actual_rows=%v loops=%v", plan.ActualRows, plan.ActualLoops)
			}
			indexes := map[string]struct{}{}
			collectAnswerContextExplainIndexes(plan, indexes)
			indexNames := make([]string, 0, len(indexes))
			for name := range indexes {
				indexNames = append(indexNames, name)
			}
			sort.Strings(indexNames)
			if len(indexNames) == 0 {
				t.Fatal("representative fence plan used no index-backed node")
			}
			t.Logf(
				"fixture=%s services=%d staff=%d planning_ms=%.3f execution_ms=%.3f shared_hit_blocks=%d shared_read_blocks=%d indexes=%s",
				fixture.name, fixture.services, fixture.staff, explained[0].PlanningTime, explained[0].ExecutionTime,
				plan.SharedHitBlocks, plan.SharedReadBlocks, strings.Join(indexNames, ","),
			)
		})
	}
}

func collectAnswerContextExplainIndexes(node answerContextExplainNode, indexes map[string]struct{}) {
	if name := strings.TrimSpace(node.IndexName); name != "" {
		indexes[name] = struct{}{}
	}
	for _, child := range node.Plans {
		collectAnswerContextExplainIndexes(child, indexes)
	}
}

func insertAnswerContextSyntheticCatalog(t *testing.T, ctx context.Context, db *sql.DB, salonID string, label string, services int, staff int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, duration_minutes,
			price_from, ai_bookable, active, source, sync_status
		)
		SELECT $1, 'square', $2 || '-service-' || item::text,
		       'Synthetic Service ' || item::text, 45, 40, true, true, 'local', 'local_only'
		FROM generate_series(1, $3) item
	`, salonID, label, services); err != nil {
		t.Fatalf("insert %s synthetic services: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, ai_bookable, active, source, sync_status
		)
		SELECT $1, 'square', $2 || '-staff-' || item::text,
		       'Synthetic Staff ' || item::text, true, true, 'local', 'local_only'
		FROM generate_series(1, $3) item
	`, salonID, label, staff); err != nil {
		t.Fatalf("insert %s synthetic staff: %v", label, err)
	}
}
