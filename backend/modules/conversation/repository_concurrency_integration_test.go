package conversation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestSessionTurnSlotLimitPreservesPoolHeadroom(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://unused")
	if err != nil {
		t.Fatalf("open database handle: %v", err)
	}
	defer db.Close()

	tests := []struct {
		name    string
		maxOpen int
		want    int
	}{
		{name: "unlimited pool uses bounded default", maxOpen: 0, want: defaultSessionTurnSlots},
		{name: "single connection remains bounded", maxOpen: 1, want: 1},
		{name: "two connections reserve one", maxOpen: 2, want: 1},
		{name: "production pool reserves half", maxOpen: 20, want: 10},
		{name: "odd pool rounds down", maxOpen: 21, want: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db.SetMaxOpenConns(test.maxOpen)
			if got := sessionTurnSlotLimit(db); got != test.want {
				t.Fatalf("session turn slot limit = %d, want %d", got, test.want)
			}
		})
	}

	db.SetMaxOpenConns(1)
	repo := NewRepository(db)
	called := false
	if _, err := repo.WithSessionTurnSerialization(context.Background(), "salon_1", "owner_1", "session_1", func(context.Context) (*Session, error) {
		called = true
		return nil, nil
	}); err == nil {
		t.Fatal("single-connection pool serializer error = nil")
	}
	if called {
		t.Fatal("single-connection pool must fail before invoking the turn callback")
	}
}

func TestRepositorySessionTurnSerializationBlocksAnotherRepository(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ownerID, salonID := seedConversationConcurrencyTestData(t, ctx, db)
	defer cleanupConversationConcurrencyTestData(t, db, ownerID, salonID)
	repoA := NewRepository(db)
	repoB := NewRepository(db)
	session, err := repoA.CreateSession(ctx, NewSessionRecord{
		SalonID:      salonID,
		OwnerUserID:  ownerID,
		Channel:      ChannelSimulator,
		InitialReply: "How can I help today?",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstReleased := false
	defer func() {
		if !firstReleased {
			close(releaseFirst)
		}
	}()
	firstDone := make(chan error, 1)
	go func() {
		_, err := repoA.WithSessionTurnSerialization(ctx, salonID, ownerID, session.ID, func(context.Context) (*Session, error) {
			close(firstEntered)
			<-releaseFirst
			return session, nil
		})
		firstDone <- err
	}()
	select {
	case <-firstEntered:
	case err := <-firstDone:
		t.Fatalf("first serializer exited before callback: %v", err)
	case <-ctx.Done():
		t.Fatalf("first serializer did not enter callback: %v", ctx.Err())
	}

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := repoB.WithSessionTurnSerialization(ctx, salonID, ownerID, session.ID, func(context.Context) (*Session, error) {
			close(secondEntered)
			return session, nil
		})
		secondDone <- err
	}()
	select {
	case <-secondEntered:
		t.Fatal("second repository entered the same session while the first lock was held")
	case err := <-secondDone:
		t.Fatalf("second serializer exited before the first lock was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	firstReleased = true
	select {
	case <-secondEntered:
	case err := <-secondDone:
		t.Fatalf("second serializer exited without entering callback: %v", err)
	case <-ctx.Done():
		t.Fatalf("second serializer did not enter after release: %v", ctx.Err())
	}
	if err := receiveRepositorySerializationError(t, ctx, firstDone); err != nil {
		t.Fatalf("first serializer: %v", err)
	}
	if err := receiveRepositorySerializationError(t, ctx, secondDone); err != nil {
		t.Fatalf("second serializer: %v", err)
	}
}

func TestRepositoryConcurrentPhoneSessionCreateIsIdempotentAndTenantBound(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ownerA, salonA := seedConversationConcurrencyTestData(t, ctx, db)
	defer cleanupConversationConcurrencyTestData(t, db, ownerA, salonA)
	ownerB, salonB := seedConversationConcurrencyTestData(t, ctx, db)
	defer cleanupConversationConcurrencyTestData(t, db, ownerB, salonB)
	record := NewSessionRecord{
		SalonID: salonA, OwnerUserID: ownerA, Channel: ChannelPhone, Provider: "twilio",
		ProviderCallID: "CA" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		InboundPhone:   "+13125550991", OutboundPhone: "+13125550199", InitialReply: "Welcome to this salon.",
	}

	results := make([]*Session, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for index := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = NewRepository(db).CreateSession(ctx, record)
		}(index)
	}
	wg.Wait()
	for index, createErr := range errs {
		if createErr != nil {
			t.Fatalf("concurrent create %d: %v", index, createErr)
		}
	}
	if results[0] == nil || results[1] == nil || results[0].ID != results[1].ID {
		t.Fatalf("concurrent sessions=%#v/%#v, want one durable identity", results[0], results[1])
	}
	var sessionCount, greetingCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM call_sessions WHERE provider='twilio' AND provider_call_id=$1`, record.ProviderCallID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM call_transcript_messages WHERE session_id=$1 AND speaker='ai'`, results[0].ID).Scan(&greetingCount); err != nil {
		t.Fatalf("count initial transcript: %v", err)
	}
	if sessionCount != 1 || greetingCount != 1 {
		t.Fatalf("durable session/greeting counts=%d/%d, want 1/1", sessionCount, greetingCount)
	}
	changedIdentity := record
	changedIdentity.InboundPhone = "+13125550992"
	if _, err := NewRepository(db).CreateSession(ctx, changedIdentity); !errors.Is(err, ErrSessionIdentityConflict) {
		t.Fatalf("changed-payload CallSid reuse error=%v, want identity conflict", err)
	}

	conflicting := record
	conflicting.SalonID = salonB
	conflicting.OwnerUserID = ownerB
	conflicting.OutboundPhone = "+13125550198"
	if _, err := NewRepository(db).CreateSession(ctx, conflicting); !errors.Is(err, ErrSessionIdentityConflict) {
		t.Fatalf("cross-tenant CallSid reuse error=%v, want identity conflict", err)
	}
	var crossTenantCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM call_sessions WHERE salon_id=$1 AND provider_call_id=$2`, salonB, record.ProviderCallID).Scan(&crossTenantCount); err != nil {
		t.Fatalf("count cross-tenant sessions: %v", err)
	}
	if crossTenantCount != 0 {
		t.Fatalf("cross-tenant session side effects=%d, want 0", crossTenantCount)
	}
}

func TestRepositoryPhoneSessionInsertFailsClosedWhenTwilioRouteFenceChanges(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ownerID, salonID := seedConversationConcurrencyTestData(t, ctx, db)
	defer cleanupConversationConcurrencyTestData(t, db, ownerID, salonID)

	routeID := uuid.NewString()
	var routeUpdatedAt time.Time
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings)
		VALUES($1,$2,'twilio',true,'{"voice_routing_enabled":"true","voice_inbound_number":"+13125550199"}'::jsonb)
		RETURNING updated_at
	`, routeID, salonID).Scan(&routeUpdatedAt); err != nil {
		t.Fatalf("insert Twilio route: %v", err)
	}
	record := NewSessionRecord{
		SalonID: salonID, OwnerUserID: ownerID, Channel: ChannelPhone, Provider: "twilio",
		ProviderCallID: "CA" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		InboundPhone:   "+13125550991", OutboundPhone: "+13125550199", InitialReply: "Welcome.",
		VoiceRouteID: routeID, VoiceRouteUpdatedAt: routeUpdatedAt,
	}
	if _, err := NewRepository(db).CreateSession(ctx, record); err != nil {
		t.Fatalf("create session with current route fence: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_integration_configs
		SET settings=jsonb_set(settings,'{voice_routing_enabled}','"false"'::jsonb),updated_at=now()
		WHERE id=$1
	`, routeID); err != nil {
		t.Fatalf("disable Twilio route: %v", err)
	}
	stale := record
	stale.ProviderCallID = "CA" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := NewRepository(db).CreateSession(ctx, stale); !errors.Is(err, ErrSessionRouteFenceConflict) {
		t.Fatalf("stale route fence error=%v", err)
	}
	var staleCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM call_sessions WHERE provider_call_id=$1`, stale.ProviderCallID).Scan(&staleCount); err != nil {
		t.Fatalf("count stale sessions: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("stale route created %d sessions", staleCount)
	}
}

func TestRepositorySessionTurnSerializationReleasesAfterRequestCancellation(t *testing.T) {
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

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer setupCancel()
	ownerID, salonID := seedConversationConcurrencyTestData(t, setupCtx, db)
	defer cleanupConversationConcurrencyTestData(t, db, ownerID, salonID)
	repoA := NewRepository(db)
	repoB := NewRepository(db)
	session, err := repoA.CreateSession(setupCtx, NewSessionRecord{
		SalonID:      salonID,
		OwnerUserID:  ownerID,
		Channel:      ChannelSimulator,
		InitialReply: "How can I help today?",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	_, err = repoA.WithSessionTurnSerialization(requestCtx, salonID, ownerID, session.ID, func(ctx context.Context) (*Session, error) {
		cancelRequest()
		return nil, ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled serializer error = %v, want context.Canceled", err)
	}

	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), time.Second)
	defer verifyCancel()
	entered := false
	_, err = repoB.WithSessionTurnSerialization(verifyCtx, salonID, ownerID, session.ID, func(context.Context) (*Session, error) {
		entered = true
		return session, nil
	})
	if err != nil {
		t.Fatalf("serializer after cancelled request: %v", err)
	}
	if !entered {
		t.Fatal("serializer callback did not run after cancelled request cleanup")
	}
}

func TestRepositoryConcurrentTurnsRejectStaleRevisionBeforeTranscriptWrite(t *testing.T) {
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
	ownerID, salonID := seedConversationConcurrencyTestData(t, ctx, db)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
			t.Errorf("cleanup test salon: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
			t.Errorf("cleanup test owner: %v", err)
		}
	}()

	repo := NewRepository(db)
	session, err := repo.CreateSession(ctx, NewSessionRecord{
		SalonID:      salonID,
		OwnerUserID:  ownerID,
		Channel:      ChannelSimulator,
		InitialReply: "How can I help today?",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turns := []TurnRecord{
		conversationConcurrencyTurn(*session, ownerID, "event-name", "My name is Linh Tran.", "Linh Tran", ""),
		conversationConcurrencyTurn(*session, ownerID, "event-phone", "My phone number is 312-555-0199.", "", "3125550199"),
	}

	results := make([]*Session, len(turns))
	errs := make([]error, len(turns))
	var wg sync.WaitGroup
	for index := range turns {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = repo.SaveTurn(ctx, turns[index])
		}(index)
	}
	wg.Wait()

	successIndex := -1
	conflictIndex := -1
	for index, err := range errs {
		switch {
		case err == nil:
			successIndex = index
		case errors.Is(err, ErrSessionStateConflict):
			conflictIndex = index
		default:
			t.Fatalf("turn %d: %v", index, err)
		}
	}
	if successIndex < 0 || conflictIndex < 0 {
		t.Fatalf("concurrent outcomes = %#v, want one success and one state conflict", errs)
	}
	committed := results[successIndex]
	if committed == nil || committed.StateRevision != 1 {
		t.Fatalf("first committed session = %#v", committed)
	}

	retry := turns[conflictIndex]
	retry.Session = *committed
	retry.ExpectedStateRevision = committed.StateRevision
	retry.Update.CustomerName = committed.CustomerName
	retry.Update.CustomerPhone = committed.CustomerPhone
	if conflictIndex == 0 {
		retry.Update.CustomerName = "Linh Tran"
	} else {
		retry.Update.CustomerPhone = "3125550199"
	}
	final, err := repo.SaveTurn(ctx, retry)
	if err != nil {
		t.Fatalf("retry stale turn from current revision: %v", err)
	}
	if final.StateRevision != 2 || final.CustomerName != "Linh Tran" || final.CustomerPhone != "3125550199" {
		t.Fatalf("final session state = revision %d, name %q, phone %q", final.StateRevision, final.CustomerName, final.CustomerPhone)
	}
	if got := countTranscriptSpeaker(final.Transcript, SpeakerCustomer); got != 2 {
		t.Fatalf("customer transcript count = %d, want 2 committed events", got)
	}
	replayed, ok, err := repo.GetSessionByTurnEventKey(ctx, salonID, ownerID, final.ID, turns[successIndex].EventKey)
	if err != nil {
		t.Fatalf("load exact replay for first committed event: %v", err)
	}
	if !ok || replayed.ReplayEventKey != turns[successIndex].EventKey || replayed.ReplayAIMessage != "Reply for "+turns[successIndex].EventKey {
		t.Fatalf("first event replay = ok=%t event=%q reply=%q", ok, replayed.ReplayEventKey, replayed.ReplayAIMessage)
	}
	if replayed.StateRevision != final.StateRevision || latestAITranscriptMessage(replayed) != "Reply for "+turns[conflictIndex].EventKey {
		t.Fatalf("replay changed current session state: replayed=%#v final=%#v", replayed, final)
	}
}

func conversationConcurrencyTurn(session Session, ownerID string, eventKey string, message string, customerName string, customerPhone string) TurnRecord {
	return TurnRecord{
		SalonID:               session.SalonID,
		OwnerUserID:           ownerID,
		Session:               session,
		ExpectedStateRevision: session.StateRevision,
		CustomerMessage:       message,
		AIMessage:             "Reply for " + eventKey,
		EventKey:              eventKey,
		CustomerMetadata:      map[string]any{"event_key": eventKey},
		Update: SessionUpdate{
			Status:        StatusActive,
			Intent:        session.Intent,
			Outcome:       OutcomeCollecting,
			BookingAction: BookingActionBook,
			CustomerName:  customerName,
			CustomerPhone: customerPhone,
			DialogState:   cloneDialogState(session.DialogState),
		},
	}
}

func receiveRepositorySerializationError(t *testing.T, ctx context.Context, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-ctx.Done():
		t.Fatalf("timed out waiting for repository serializer: %v", ctx.Err())
		return ctx.Err()
	}
}

func cleanupConversationConcurrencyTestData(t *testing.T, db *sql.DB, ownerID string, salonID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID); err != nil {
		t.Errorf("cleanup test salon: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
		t.Errorf("cleanup test owner: %v", err)
	}
}

func seedConversationConcurrencyTestData(t *testing.T, ctx context.Context, db *sql.DB) (string, string) {
	t.Helper()
	suffix := uuid.NewString()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name)
		VALUES ($1, 'integration-test', 'Conversation Concurrency Test')
		RETURNING id::text
	`, "conversation-concurrency-"+suffix+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id)
		VALUES ('Conversation Concurrency Test Salon', '+13125550199', $1)
		RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		if _, cleanupErr := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, ownerID); cleanupErr != nil {
			t.Errorf("cleanup owner after salon seed failure: %v", cleanupErr)
		}
		t.Fatalf("insert salon: %v", err)
	}
	return ownerID, salonID
}
