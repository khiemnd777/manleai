package conversation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestMessageSerializesConfirmedBookingBeforePlanningAnotherTurn(t *testing.T) {
	store := &serializingConversationStore{
		fakeConversationStore:  newFakeConversationStore(),
		serializationAttempted: make(chan struct{}, 3),
	}
	bookingTool := newBlockingConfirmedBookingTool()
	defer bookingTool.release()
	service := NewService(store, bookingTool)
	service.now = fixedNow
	if err := service.PrewarmAnswerContext(context.Background(), "salon_1"); err != nil {
		t.Fatalf("prewarm answer context: %v", err)
	}

	bookingResult := make(chan messageResult, 1)
	go func() {
		session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
			Message:  "My name is Linh Tran, phone 312-555-0101, classic manicure with Mai on 2026-06-10 at 3pm.",
			EventKey: "provider-event-book",
		})
		bookingResult <- messageResult{session: session, err: err}
	}()

	select {
	case <-bookingTool.entered:
	case <-time.After(time.Second):
		t.Fatal("booking side effect was not reached")
	}
	receiveSerializationAttempt(t, store.serializationAttempted)

	distinctResult := make(chan messageResult, 1)
	go func() {
		session, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
			Message:  "Actually, change that to a Gel Manicure.",
			EventKey: "provider-event-distinct",
		})
		distinctResult <- messageResult{session: session, err: err}
	}()
	voiceRecoveryResult := make(chan messageResult, 1)
	go func() {
		session, err := service.HandleUnintelligibleVoiceInput(context.Background(), "salon_1", "owner_1", "session_1", VoiceInputHandoffRequest{
			EventKey: "provider-event-voice-recovery",
		})
		voiceRecoveryResult <- messageResult{session: session, err: err}
	}()
	receiveSerializationAttempt(t, store.serializationAttempted)
	receiveSerializationAttempt(t, store.serializationAttempted)

	select {
	case result := <-distinctResult:
		t.Fatalf("distinct turn completed while booking side effect was active: session=%#v err=%v", result.session, result.err)
	case result := <-voiceRecoveryResult:
		t.Fatalf("voice recovery completed while booking side effect was active: session=%#v err=%v", result.session, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	if got := store.sessionLoads.Load(); got != 1 {
		t.Fatalf("session loads while booking side effect was active = %d, want 1", got)
	}
	if got := bookingTool.callCount(); got != 1 {
		t.Fatalf("booking calls while first side effect was active = %d, want 1", got)
	}

	bookingTool.release()
	confirmed := receiveMessageResult(t, bookingResult)
	if confirmed.err != nil {
		t.Fatalf("confirmed booking turn: %v", confirmed.err)
	}
	if confirmed.session == nil || confirmed.session.Outcome != OutcomeBookingConfirmed {
		t.Fatalf("confirmed booking session = %#v", confirmed.session)
	}
	distinct := receiveMessageResult(t, distinctResult)
	if !errors.Is(distinct.err, ErrSessionClosed) {
		t.Fatalf("distinct turn after confirmed booking error = %v, want ErrSessionClosed", distinct.err)
	}
	voiceRecovery := receiveMessageResult(t, voiceRecoveryResult)
	if !errors.Is(voiceRecovery.err, ErrSessionClosed) {
		t.Fatalf("voice recovery after confirmed booking error = %v, want ErrSessionClosed", voiceRecovery.err)
	}

	replayed, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message:  "This wording is ignored because the provider event is a replay.",
		EventKey: "provider-event-book",
	})
	if err != nil {
		t.Fatalf("same-event replay: %v", err)
	}
	if replayed.BookingAttemptID != "attempt_serialized" || replayed.AppointmentID != "appointment_serialized" {
		t.Fatalf("replayed booking linkage = %q/%q", replayed.BookingAttemptID, replayed.AppointmentID)
	}
	if got := bookingTool.callCount(); got != 1 {
		t.Fatalf("booking calls after same-event replay = %d, want 1", got)
	}
	final := store.snapshot()
	if final.BookingAttemptID != "attempt_serialized" || final.AppointmentID != "appointment_serialized" {
		t.Fatalf("final booking linkage = %q/%q", final.BookingAttemptID, final.AppointmentID)
	}
	if final.StateRevision != 1 {
		t.Fatalf("final state revision = %d, want one committed turn", final.StateRevision)
	}
	if got := countTranscriptSpeaker(final.Transcript, SpeakerCustomer); got != 1 {
		t.Fatalf("customer transcript count = %d, want one committed booking event", got)
	}
	if !store.processedEventKeys["provider-event-book"] || store.processedEventKeys["provider-event-distinct"] || store.processedEventKeys["provider-event-voice-recovery"] {
		t.Fatalf("processed event keys = %#v", store.processedEventKeys)
	}
}

func TestMessageConcurrentDifferentEventsReplanWithoutLosingState(t *testing.T) {
	store := newConcurrentCASConversationStore(newFakeConversationStore(), 2)
	service := NewService(store, &fakeBookingTool{})
	if err := service.PrewarmAnswerContext(context.Background(), "salon_1"); err != nil {
		t.Fatalf("prewarm answer context: %v", err)
	}

	requests := []MessageRequest{
		{Message: "I would like a Classic Manicure.", EventKey: "provider-event-service"},
		{Message: "My phone number is 312-555-0199.", EventKey: "provider-event-phone"},
	}
	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for index := range requests {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = service.Message(context.Background(), "salon_1", "owner_1", "session_1", requests[index])
		}(index)
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("message %d: %v", index, err)
		}
	}
	final := store.snapshot()
	if final.ServiceID != "service_1" {
		t.Fatalf("service id = %q, want service_1", final.ServiceID)
	}
	if want := validation.NormalizePhone("312-555-0199"); final.CustomerPhone != want {
		t.Fatalf("customer phone = %q, want %q", final.CustomerPhone, want)
	}
	if final.StateRevision != 2 {
		t.Fatalf("state revision = %d, want 2 committed turns", final.StateRevision)
	}
	if got := countTranscriptSpeaker(final.Transcript, SpeakerCustomer); got != 2 {
		t.Fatalf("customer transcript count = %d, want 2", got)
	}
	if !store.processed("provider-event-service") || !store.processed("provider-event-phone") {
		t.Fatalf("processed event keys = %#v", store.processedEventKeys)
	}
}

func TestMessageConcurrentSameEventRemainsIdempotent(t *testing.T) {
	store := newConcurrentCASConversationStore(newFakeConversationStore(), 2)
	service := NewService(store, &fakeBookingTool{})
	if err := service.PrewarmAnswerContext(context.Background(), "salon_1"); err != nil {
		t.Fatalf("prewarm answer context: %v", err)
	}
	req := MessageRequest{Message: "I would like a Classic Manicure.", EventKey: "provider-event-replayed"}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for index := range errs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = service.Message(context.Background(), "salon_1", "owner_1", "session_1", req)
		}(index)
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("message %d: %v", index, err)
		}
	}
	final := store.snapshot()
	if final.StateRevision != 1 {
		t.Fatalf("state revision = %d, want one committed event", final.StateRevision)
	}
	if got := countTranscriptSpeaker(final.Transcript, SpeakerCustomer); got != 1 {
		t.Fatalf("customer transcript count = %d, want one deduplicated event", got)
	}
}

func TestMessageReplaysExactHistoricalReplyForOlderEvent(t *testing.T) {
	store := newConcurrentCASConversationStore(newFakeConversationStore(), 0)
	bookingTool := &fakeBookingTool{}
	service := NewService(store, bookingTool)
	service.now = fixedNow

	first, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "I would like a Classic Manicure.", EventKey: "provider-event-e1",
	})
	if err != nil {
		t.Fatalf("event E1: %v", err)
	}
	firstReply := latestAITranscriptMessage(first)
	second, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "June 20, 2026.", EventKey: "provider-event-e2",
	})
	if err != nil {
		t.Fatalf("event E2: %v", err)
	}
	secondReply := latestAITranscriptMessage(second)
	if firstReply == "" || secondReply == "" || firstReply == secondReply {
		t.Fatalf("test requires distinct replies: E1=%q E2=%q", firstReply, secondReply)
	}

	beforeReplay := store.snapshot()
	bookingCallsBeforeReplay := bookingTool.calls
	availabilityCallsBeforeReplay := bookingTool.availabilityCalls
	replayed, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Provider retry payload must not be reinterpreted.", EventKey: "provider-event-e1",
	})
	if err != nil {
		t.Fatalf("replay E1: %v", err)
	}
	if replayed.ReplayEventKey != "provider-event-e1" || replayed.ReplayAIMessage != firstReply {
		t.Fatalf("replayed event/reply = %q/%q, want exact E1 reply %q", replayed.ReplayEventKey, replayed.ReplayAIMessage, firstReply)
	}
	if latest := latestAITranscriptMessage(replayed); latest != secondReply {
		t.Fatalf("current transcript changed during E1 replay: latest=%q want E2=%q", latest, secondReply)
	}
	afterReplay := store.snapshot()
	if afterReplay.StateRevision != beforeReplay.StateRevision || len(afterReplay.Transcript) != len(beforeReplay.Transcript) {
		t.Fatalf("E1 replay mutated current state: before=%#v after=%#v", beforeReplay, afterReplay)
	}
	if bookingTool.calls != bookingCallsBeforeReplay || bookingTool.availabilityCalls != availabilityCallsBeforeReplay {
		t.Fatalf("E1 replay invoked booking tools: booking=%d→%d availability=%d→%d",
			bookingCallsBeforeReplay, bookingTool.calls, availabilityCallsBeforeReplay, bookingTool.availabilityCalls)
	}
}

func latestAITranscriptMessage(session *Session) string {
	if session == nil {
		return ""
	}
	for index := len(session.Transcript) - 1; index >= 0; index-- {
		if session.Transcript[index].Speaker == SpeakerAI {
			return session.Transcript[index].Body
		}
	}
	return ""
}

func TestMessageStateConflictRetryIsBounded(t *testing.T) {
	store := &alwaysConflictConversationStore{fakeConversationStore: newFakeConversationStore()}
	service := NewService(store, &fakeBookingTool{})
	if err := service.PrewarmAnswerContext(context.Background(), "salon_1"); err != nil {
		t.Fatalf("prewarm answer context: %v", err)
	}

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message:  "I would like a Classic Manicure.",
		EventKey: "provider-event-conflict",
	})
	if !errors.Is(err, ErrSessionStateConflict) {
		t.Fatalf("error = %v, want ErrSessionStateConflict", err)
	}
	if store.saveCalls != maxStateConflictRetries+1 {
		t.Fatalf("save calls = %d, want %d", store.saveCalls, maxStateConflictRetries+1)
	}
}

type concurrentCASConversationStore struct {
	*fakeConversationStore

	mu                sync.Mutex
	initialLoadTarget int
	initialLoadCount  int
	initialLoadsReady chan struct{}
}

func newConcurrentCASConversationStore(base *fakeConversationStore, initialLoadTarget int) *concurrentCASConversationStore {
	return &concurrentCASConversationStore{
		fakeConversationStore: base,
		initialLoadTarget:     initialLoadTarget,
		initialLoadsReady:     make(chan struct{}),
	}
}

func (s *concurrentCASConversationStore) GetAnswerContextFence(ctx context.Context, salonID string) (AnswerContextFence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.GetAnswerContextFence(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListBookableServices(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListActiveServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListActiveServiceAliases(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListActiveServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListActiveServiceCategoryAliases(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListBookableStaff(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListActiveStaff(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListActiveKnowledge(ctx, salonID)
}

func (s *concurrentCASConversationStore) ListBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConversationStore.ListBusinessHourPeriods(ctx, salonID)
}

func (s *concurrentCASConversationStore) GetSessionForOwner(_ context.Context, _ string, _ string, _ string) (*Session, error) {
	s.mu.Lock()
	snapshot := cloneSessionForTurn(s.session)
	snapshot.Transcript = append([]TranscriptMessage(nil), s.session.Transcript...)
	waitForInitialLoads := s.initialLoadCount < s.initialLoadTarget
	if waitForInitialLoads {
		s.initialLoadCount++
		if s.initialLoadCount == s.initialLoadTarget {
			close(s.initialLoadsReady)
		}
	}
	s.mu.Unlock()
	if waitForInitialLoads {
		<-s.initialLoadsReady
	}
	return &snapshot, nil
}

func (s *concurrentCASConversationStore) GetSessionByTurnEventKey(_ context.Context, _ string, _ string, _ string, eventKey string) (*Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.processedEventKeys[eventKey] {
		return nil, false, nil
	}
	snapshot := cloneSessionForTurn(s.session)
	snapshot.Transcript = append([]TranscriptMessage(nil), s.session.Transcript...)
	snapshot.ReplayEventKey = eventKey
	snapshot.ReplayAIMessage = s.processedTurnReplies[eventKey]
	return &snapshot, true, nil
}

func (s *concurrentCASConversationStore) SaveTurn(ctx context.Context, record TurnRecord) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.EventKey != "" && s.processedEventKeys[record.EventKey] {
		snapshot := cloneSessionForTurn(s.session)
		snapshot.Transcript = append([]TranscriptMessage(nil), s.session.Transcript...)
		snapshot.ReplayEventKey = record.EventKey
		snapshot.ReplayAIMessage = s.processedTurnReplies[record.EventKey]
		return &snapshot, nil
	}
	if record.ExpectedStateRevision != s.session.StateRevision {
		return nil, ErrSessionStateConflict
	}
	updated, err := s.fakeConversationStore.SaveTurn(ctx, record)
	if err != nil {
		return nil, err
	}
	updated.StateRevision = record.ExpectedStateRevision + 1
	s.session.StateRevision = updated.StateRevision
	return updated, nil
}

func (s *concurrentCASConversationStore) snapshot() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := cloneSessionForTurn(s.session)
	snapshot.Transcript = append([]TranscriptMessage(nil), s.session.Transcript...)
	return snapshot
}

func (s *concurrentCASConversationStore) processed(eventKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processedEventKeys[eventKey]
}

type alwaysConflictConversationStore struct {
	*fakeConversationStore
	saveCalls int
}

func (s *alwaysConflictConversationStore) SaveTurn(context.Context, TurnRecord) (*Session, error) {
	s.saveCalls++
	return nil, ErrSessionStateConflict
}

type serializingConversationStore struct {
	*fakeConversationStore

	turnMu                 sync.Mutex
	sessionLoads           atomic.Int32
	serializationAttempted chan struct{}
}

func (s *serializingConversationStore) WithSessionTurnSerialization(
	ctx context.Context,
	_ string,
	_ string,
	_ string,
	operation func(context.Context) (*Session, error),
) (*Session, error) {
	if s.serializationAttempted != nil {
		s.serializationAttempted <- struct{}{}
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	return operation(ctx)
}

func (s *serializingConversationStore) GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	s.sessionLoads.Add(1)
	return s.fakeConversationStore.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
}

func (s *serializingConversationStore) SaveTurn(ctx context.Context, record TurnRecord) (*Session, error) {
	updated, err := s.fakeConversationStore.SaveTurn(ctx, record)
	if err != nil {
		return nil, err
	}
	updated.StateRevision = record.ExpectedStateRevision + 1
	s.session.StateRevision = updated.StateRevision
	return updated, nil
}

func (s *serializingConversationStore) snapshot() Session {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	snapshot := cloneSessionForTurn(s.session)
	snapshot.Transcript = append([]TranscriptMessage(nil), s.session.Transcript...)
	return snapshot
}

type blockingConfirmedBookingTool struct {
	*fakeBookingTool

	entered     chan struct{}
	releaseCh   chan struct{}
	releaseOnce sync.Once
	calls       atomic.Int32
}

func newBlockingConfirmedBookingTool() *blockingConfirmedBookingTool {
	return &blockingConfirmedBookingTool{
		fakeBookingTool: &fakeBookingTool{},
		entered:         make(chan struct{}),
		releaseCh:       make(chan struct{}),
	}
}

func (b *blockingConfirmedBookingTool) Create(_ context.Context, _ string, _ string, _ booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	if b.calls.Add(1) == 1 {
		close(b.entered)
	}
	<-b.releaseCh
	return &booking.BookingAttempt{
		ID:           "attempt_serialized",
		Status:       booking.StatusConfirmed,
		POSBookingID: "pos_booking_serialized",
		Appointment: &booking.Appointment{
			ID:               "appointment_serialized",
			Status:           booking.StatusConfirmed,
			POSAppointmentID: "pos_booking_serialized",
		},
	}, nil
}

func (b *blockingConfirmedBookingTool) release() {
	b.releaseOnce.Do(func() {
		close(b.releaseCh)
	})
}

func (b *blockingConfirmedBookingTool) callCount() int {
	return int(b.calls.Load())
}

type messageResult struct {
	session *Session
	err     error
}

func receiveMessageResult(t *testing.T, results <-chan messageResult) messageResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for conversation turn")
		return messageResult{}
	}
}

func receiveSerializationAttempt(t *testing.T, attempts <-chan struct{}) {
	t.Helper()
	select {
	case <-attempts:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session serialization attempt")
	}
}

func countTranscriptSpeaker(messages []TranscriptMessage, speaker string) int {
	count := 0
	for _, message := range messages {
		if message.Speaker == speaker {
			count++
		}
	}
	return count
}
