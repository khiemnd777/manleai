package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

type answerContextStoreCallCounts struct {
	Fence                    int
	CalendarEvidence         int
	GuidanceServices         int
	BookableServices         int
	ServiceAliases           int
	CategoryAliases          int
	ActiveStaff              int
	BookableStaff            int
	InternalBookableServices int
	InternalBookableStaff    int
	Knowledge                int
	ExternalHours            int
	OwnerHours               int
	InternalHours            int
}

type answerContextCountingStore struct {
	*fakeConversationStore
	calls answerContextStoreCallCounts
}

func newAnswerContextCountingStore(authority string) *answerContextCountingStore {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:         authority,
		SchedulingAuthorityVersion:  3,
		ServiceCatalogVersion:       5,
		ServiceAliasesVersion:       7,
		ServiceCategoriesVersion:    11,
		ConsultationProfilesVersion: 13,
		StaffCatalogVersion:         17,
		KnowledgeBaseVersion:        19,
	}
	store.guidanceServices = []ServiceOption{{ID: "service_guidance", Name: "Botanical Manicure", DurationMinutes: 50}}
	store.services = []ServiceOption{{ID: "service_guidance", Name: "Botanical Manicure", DurationMinutes: 50}}
	store.internalServices = []ServiceOption{{ID: "service_guidance", Name: "Botanical Manicure", DurationMinutes: 50}}
	store.activeStaff = []StaffOption{{ID: "staff_active", Name: "Samira Lane", AIBookable: true}}
	store.staff = append([]StaffOption(nil), store.activeStaff...)
	store.internalStaff = append([]StaffOption(nil), store.activeStaff...)
	store.serviceAliases = []ServiceAlias{{ID: "alias_1", ServiceID: "service_guidance", Alias: "botanical care"}}
	store.categoryAliases = []ServiceCategoryAlias{{ID: "category_alias_1", CategoryID: "category_1", Alias: "hand care"}}
	store.knowledge = []KnowledgeSnippet{{ID: "knowledge_1", Title: "Parking", Body: "Parking is available beside the studio."}}
	store.businessHours = []BusinessHourPeriod{{ID: "external_hours", Source: "imported", Provider: "square"}}
	store.ownerHours = []BusinessHourPeriod{{ID: "owner_hours", Source: "local_override"}}
	store.internalHours = []BusinessHourPeriod{{ID: "internal_hours", Source: "local_override"}}

	switch authority {
	case booking.SchedulingAuthorityOwnerManual:
		store.answerContextFence.LocalBusinessHoursVersion = 23
	case booking.SchedulingAuthorityManleAICalendar:
		store.answerContextFence.CalendarConfigVersion = 29
		store.answerContextFence.CalendarActivatedVersion = 29
	case booking.SchedulingAuthorityExternalProvider:
		store.answerContextFence.ActiveProvider = "square"
		store.answerContextFence.ConnectionStatus = "active"
		store.answerContextFence.LocationID = "location_perf"
		store.answerContextFence.SnapshotGeneration = 31
		store.answerContextFence.LastSyncAtRFC3339 = "2026-07-29T01:02:03Z"
	}
	return &answerContextCountingStore{fakeConversationStore: store}
}

func (s *answerContextCountingStore) resetCalls() {
	s.calls = answerContextStoreCallCounts{}
}

func (s *answerContextCountingStore) GetAnswerContextFence(ctx context.Context, salonID string) (AnswerContextFence, error) {
	s.calls.Fence++
	return s.fakeConversationStore.GetAnswerContextFence(ctx, salonID)
}

func (s *answerContextCountingStore) GetManleAICalendarAnswerContextEvidence(ctx context.Context, salonID string) (manleAICalendarAnswerContextEvidence, error) {
	s.calls.CalendarEvidence++
	return s.fakeConversationStore.GetManleAICalendarAnswerContextEvidence(ctx, salonID)
}

func (s *answerContextCountingStore) ListGuidanceServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	s.calls.GuidanceServices++
	return s.fakeConversationStore.ListGuidanceServices(ctx, salonID)
}

func (s *answerContextCountingStore) ListCanonicalGuidanceServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	return s.ListGuidanceServices(ctx, salonID)
}

func (s *answerContextCountingStore) ListBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	s.calls.BookableServices++
	return s.fakeConversationStore.ListBookableServices(ctx, salonID)
}

func (s *answerContextCountingStore) ListManleAICalendarBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error) {
	s.calls.InternalBookableServices++
	return s.fakeConversationStore.ListManleAICalendarBookableServices(ctx, salonID)
}

func (s *answerContextCountingStore) ListActiveServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error) {
	s.calls.ServiceAliases++
	return s.fakeConversationStore.ListActiveServiceAliases(ctx, salonID)
}

func (s *answerContextCountingStore) ListCanonicalServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error) {
	return s.ListActiveServiceAliases(ctx, salonID)
}

func (s *answerContextCountingStore) ListActiveServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error) {
	s.calls.CategoryAliases++
	return s.fakeConversationStore.ListActiveServiceCategoryAliases(ctx, salonID)
}

func (s *answerContextCountingStore) ListCanonicalServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error) {
	return s.ListActiveServiceCategoryAliases(ctx, salonID)
}

func (s *answerContextCountingStore) ListActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	s.calls.ActiveStaff++
	return s.fakeConversationStore.ListActiveStaff(ctx, salonID)
}

func (s *answerContextCountingStore) ListCanonicalActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	return s.ListActiveStaff(ctx, salonID)
}

func (s *answerContextCountingStore) ListBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	s.calls.BookableStaff++
	return s.fakeConversationStore.ListBookableStaff(ctx, salonID)
}

func (s *answerContextCountingStore) ListManleAICalendarBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error) {
	s.calls.InternalBookableStaff++
	return s.fakeConversationStore.ListManleAICalendarBookableStaff(ctx, salonID)
}

func (s *answerContextCountingStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	s.calls.Knowledge++
	return s.fakeConversationStore.ListActiveKnowledge(ctx, salonID)
}

func (s *answerContextCountingStore) ListExternalProviderBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	s.calls.ExternalHours++
	return s.fakeConversationStore.ListExternalProviderBusinessHourPeriods(ctx, salonID)
}

func (s *answerContextCountingStore) ListOwnerManagedBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	s.calls.OwnerHours++
	return s.fakeConversationStore.ListOwnerManagedBusinessHourPeriods(ctx, salonID)
}

func (s *answerContextCountingStore) ListManleAICalendarBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error) {
	s.calls.InternalHours++
	return s.fakeConversationStore.ListManleAICalendarBusinessHourPeriods(ctx, salonID)
}

func TestAnswerContextQueryTopologyByAuthority(t *testing.T) {
	tests := []struct {
		name      string
		authority string
		wantMiss  answerContextStoreCallCounts
	}{
		{
			name: "owner manual", authority: booking.SchedulingAuthorityOwnerManual,
			wantMiss: answerContextStoreCallCounts{
				Fence: 2, GuidanceServices: 1, ServiceAliases: 1, CategoryAliases: 1,
				ActiveStaff: 1, Knowledge: 1, OwnerHours: 1,
			},
		},
		{
			name: "external provider", authority: booking.SchedulingAuthorityExternalProvider,
			wantMiss: answerContextStoreCallCounts{
				Fence: 2, GuidanceServices: 1, BookableServices: 1, ServiceAliases: 1,
				CategoryAliases: 1, ActiveStaff: 1, BookableStaff: 1, Knowledge: 1, ExternalHours: 1,
			},
		},
		{
			name: "ManleAI Calendar", authority: booking.SchedulingAuthorityManleAICalendar,
			wantMiss: answerContextStoreCallCounts{
				Fence: 2, CalendarEvidence: 1, GuidanceServices: 1, InternalBookableServices: 1,
				ServiceAliases: 1, CategoryAliases: 1, ActiveStaff: 1, InternalBookableStaff: 1,
				Knowledge: 1, InternalHours: 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newAnswerContextCountingStore(test.authority)
			service := NewService(store, &fakeBookingTool{})

			first, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID)
			if err != nil {
				t.Fatalf("cold answer-context load: %v", err)
			}
			if first.CacheHit || diagnostics.cacheStatus != answerContextCacheStatusMiss || diagnostics.refreshReason != answerContextRefreshReasonCold ||
				diagnostics.retryReason != answerContextRetryReasonNone || diagnostics.attempts != 1 || diagnostics.outcome != answerContextLoadOutcomeRefreshed {
				t.Fatalf("cold diagnostics = %#v context=%#v", diagnostics, first)
			}
			if store.calls != test.wantMiss {
				t.Fatalf("cold repository-call topology = %#v, want %#v", store.calls, test.wantMiss)
			}

			store.resetCalls()
			cached, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID)
			if err != nil {
				t.Fatalf("stable cached answer-context load: %v", err)
			}
			if !cached.CacheHit || diagnostics.cacheStatus != answerContextCacheStatusHit || diagnostics.refreshReason != answerContextRefreshReasonNone ||
				diagnostics.attempts != 1 || diagnostics.outcome != answerContextLoadOutcomeCacheHit {
				t.Fatalf("cache-hit diagnostics = %#v context=%#v", diagnostics, cached)
			}
			if store.calls != (answerContextStoreCallCounts{Fence: 1}) {
				t.Fatalf("stable cache hit repository-call topology = %#v, want one lightweight fence read", store.calls)
			}
		})
	}
}

func TestAnswerContextDiagnosticsDistinguishTTLAndFenceMisses(t *testing.T) {
	t.Run("TTL expiry", func(t *testing.T) {
		store := newAnswerContextCountingStore(booking.SchedulingAuthorityOwnerManual)
		service := NewService(store, &fakeBookingTool{})
		if _, _, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID); err != nil {
			t.Fatalf("preload answer context: %v", err)
		}
		service.answerContextCache.mu.Lock()
		entry := service.answerContextCache.entries[store.session.SalonID]
		entry.expiresAt = time.Now().Add(-time.Second)
		service.answerContextCache.entries[store.session.SalonID] = entry
		service.answerContextCache.mu.Unlock()
		store.resetCalls()

		_, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID)
		if err != nil {
			t.Fatalf("reload expired answer context: %v", err)
		}
		if diagnostics.refreshReason != answerContextRefreshReasonTTLExpired || diagnostics.outcome != answerContextLoadOutcomeRefreshed || diagnostics.attempts != 1 {
			t.Fatalf("TTL diagnostics = %#v", diagnostics)
		}
		if store.calls.Fence != 2 || store.calls.OwnerHours != 1 {
			t.Fatalf("TTL miss topology = %#v", store.calls)
		}
	})

	t.Run("persisted fence mismatch", func(t *testing.T) {
		store := newAnswerContextCountingStore(booking.SchedulingAuthorityOwnerManual)
		service := NewService(store, &fakeBookingTool{})
		if _, _, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID); err != nil {
			t.Fatalf("preload answer context: %v", err)
		}
		store.answerContextFence.KnowledgeBaseVersion++
		store.knowledge = []KnowledgeSnippet{{ID: "knowledge_2", Title: "Entry", Body: "Use the garden entrance."}}
		store.resetCalls()

		answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID)
		if err != nil {
			t.Fatalf("reload changed answer context: %v", err)
		}
		if diagnostics.refreshReason != answerContextRefreshReasonFenceMismatch || diagnostics.outcome != answerContextLoadOutcomeRefreshed ||
			len(answer.Knowledge) != 1 || answer.Knowledge[0].ID != "knowledge_2" {
			t.Fatalf("fence-mismatch diagnostics/context = %#v %#v", diagnostics, answer)
		}
		if store.calls.Fence != 2 || store.calls.Knowledge != 1 {
			t.Fatalf("fence-mismatch topology = %#v", store.calls)
		}
	})
}

type readinessMismatchThenStableStore struct {
	*answerContextCountingStore
}

func (s *readinessMismatchThenStableStore) GetManleAICalendarAnswerContextEvidence(ctx context.Context, salonID string) (manleAICalendarAnswerContextEvidence, error) {
	s.calls.CalendarEvidence++
	evidence, err := s.fakeConversationStore.GetManleAICalendarAnswerContextEvidence(ctx, salonID)
	if err == nil && s.calls.CalendarEvidence == 1 {
		evidence.CalendarConfigVersion++
	}
	return evidence, err
}

func TestAnswerContextRetriesReadinessMismatchAndFailsClosedOnExhaustion(t *testing.T) {
	t.Run("readiness evidence stabilizes", func(t *testing.T) {
		base := newAnswerContextCountingStore(booking.SchedulingAuthorityManleAICalendar)
		store := &readinessMismatchThenStableStore{answerContextCountingStore: base}
		service := NewService(store, &fakeBookingTool{})

		answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), base.session.SalonID)
		if err != nil {
			t.Fatalf("load after readiness retry: %v", err)
		}
		if answer.CacheHit || diagnostics.retryReason != answerContextRetryReasonReadinessMismatch || diagnostics.attempts != 2 ||
			diagnostics.outcome != answerContextLoadOutcomeRefreshed {
			t.Fatalf("readiness-retry diagnostics/context = %#v %#v", diagnostics, answer)
		}
		if store.calls.Fence != 3 || store.calls.CalendarEvidence != 2 || store.calls.InternalHours != 1 {
			t.Fatalf("readiness-retry topology = %#v", store.calls)
		}
	})

	t.Run("bounded readiness retry exhaustion", func(t *testing.T) {
		store := newAnswerContextCountingStore(booking.SchedulingAuthorityManleAICalendar)
		store.calendarEvidence = &manleAICalendarAnswerContextEvidence{
			SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
			SchedulingAuthorityVersion: store.answerContextFence.SchedulingAuthorityVersion,
			CalendarConfigVersion:      store.answerContextFence.CalendarConfigVersion + 1,
			CalendarActivatedVersion:   store.answerContextFence.CalendarActivatedVersion,
			Ready:                      true,
		}
		service := NewService(store, &fakeBookingTool{})

		answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID)
		if err == nil || answer != nil {
			t.Fatalf("retry exhaustion answer=%#v err=%v", answer, err)
		}
		if diagnostics.retryReason != answerContextRetryReasonReadinessMismatch || diagnostics.attempts != answerContextFenceLoadAttempts ||
			diagnostics.outcome != answerContextLoadOutcomeRetryExhausted {
			t.Fatalf("retry-exhaustion diagnostics = %#v", diagnostics)
		}
		if store.calls.Fence != answerContextFenceLoadAttempts || store.calls.CalendarEvidence != answerContextFenceLoadAttempts ||
			store.calls.GuidanceServices != 0 || store.calls.InternalHours != 0 {
			t.Fatalf("retry-exhaustion topology = %#v", store.calls)
		}
		if _, ok := service.answerContextCache.get(store.session.SalonID, store.answerContextFence); ok {
			t.Fatal("retry exhaustion populated the process-local cache")
		}
	})
}

func TestAnswerContextDiagnosticsReportConcurrentFenceRetryAndAuthoritySwitch(t *testing.T) {
	t.Run("concurrent fence change", func(t *testing.T) {
		base := newFakeConversationStore()
		base.answerContextFence = AnswerContextFence{
			SchedulingAuthority: booking.SchedulingAuthorityOwnerManual, ServiceCatalogVersion: 1,
			LocalBusinessHoursVersion: 1,
		}
		first := base.answerContextFence
		second := first
		second.ServiceCatalogVersion++
		store := &changingAnswerContextFenceStore{
			fakeConversationStore: base,
			fences:                []AnswerContextFence{first, second, second, second},
		}
		service := NewService(store, &fakeBookingTool{})

		_, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), base.session.SalonID)
		if err != nil {
			t.Fatalf("load across concurrent fence change: %v", err)
		}
		if diagnostics.retryReason != answerContextRetryReasonFenceChanged || diagnostics.attempts != 2 ||
			diagnostics.outcome != answerContextLoadOutcomeRefreshed {
			t.Fatalf("concurrent-fence diagnostics = %#v", diagnostics)
		}
	})

	t.Run("authority switch", func(t *testing.T) {
		store := newAnswerContextCountingStore(booking.SchedulingAuthorityOwnerManual)
		service := NewService(store, &fakeBookingTool{})
		if _, _, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID); err != nil {
			t.Fatalf("preload owner authority: %v", err)
		}
		store.answerContextFence = AnswerContextFence{
			SchedulingAuthority:        booking.SchedulingAuthorityExternalProvider,
			SchedulingAuthorityVersion: 4,
			ServiceCatalogVersion:      5,
			StaffCatalogVersion:        17,
			ActiveProvider:             "square",
			ConnectionStatus:           "active",
			LocationID:                 "location_switched",
			SnapshotGeneration:         41,
			LastSyncAtRFC3339:          "2026-07-29T02:03:04Z",
		}
		store.resetCalls()

		answer, diagnostics, err := service.loadAnswerContextWithDiagnostics(context.Background(), store.session.SalonID)
		if err != nil {
			t.Fatalf("load after authority switch: %v", err)
		}
		if answer.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || diagnostics.authority != booking.SchedulingAuthorityExternalProvider ||
			diagnostics.refreshReason != answerContextRefreshReasonFenceMismatch || store.calls.ExternalHours != 1 || store.calls.OwnerHours != 0 {
			t.Fatalf("authority-switch diagnostics/context/calls = %#v %#v %#v", diagnostics, answer, store.calls)
		}
	})
}

func TestMessageRecordsLowCardinalityAnswerContextDiagnostics(t *testing.T) {
	store := newAnswerContextCountingStore(booking.SchedulingAuthorityOwnerManual)
	service := NewService(store, &fakeBookingTool{})
	service.SetTurnInterpreter(&fakeConversationActInterpreter{turn: testQuestionUnderstanding(ConversationQuestionPolicy, "")})
	var timings []TurnTiming

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Where should a guest park near the studio?",
		TimingRecorder: func(timing TurnTiming) {
			timings = append(timings, timing)
		},
	})
	if err != nil {
		t.Fatalf("Message: %v", err)
	}
	var answerTiming *TurnTiming
	for index := range timings {
		if timings[index].Stage == TurnTimingStageAnswerContext {
			answerTiming = &timings[index]
			break
		}
	}
	if answerTiming == nil {
		t.Fatalf("answer-context timing missing: %#v", timings)
	}
	want := map[string]string{
		answerContextTimingAuthority:     booking.SchedulingAuthorityOwnerManual,
		answerContextTimingCacheStatus:   answerContextCacheStatusMiss,
		answerContextTimingRefreshReason: answerContextRefreshReasonCold,
		answerContextTimingRetryReason:   answerContextRetryReasonNone,
		answerContextTimingAttempts:      "1",
		answerContextTimingOutcome:       answerContextLoadOutcomeRefreshed,
		answerContextTimingReady:         "true",
	}
	for key, value := range want {
		if answerTiming.Attributes[key] != value {
			t.Fatalf("answer-context timing %s = %q, want %q; timing=%#v", key, answerTiming.Attributes[key], value, answerTiming)
		}
	}
	for _, forbidden := range []string{"salon_1", "owner_1", "Where should a guest park near the studio?"} {
		for key, value := range answerTiming.Attributes {
			if key == forbidden || value == forbidden {
				t.Fatalf("private/high-cardinality value leaked into answer-context timing: %#v", answerTiming.Attributes)
			}
		}
	}
}

type answerContextFailingFenceStore struct {
	*answerContextCountingStore
}

func (s *answerContextFailingFenceStore) GetAnswerContextFence(context.Context, string) (AnswerContextFence, error) {
	s.calls.Fence++
	return AnswerContextFence{}, errors.New("synthetic fence read failure")
}

func TestMessageRecordsAnswerContextLoadFailureWithoutSensitiveErrorText(t *testing.T) {
	base := newAnswerContextCountingStore(booking.SchedulingAuthorityOwnerManual)
	store := &answerContextFailingFenceStore{answerContextCountingStore: base}
	service := NewService(store, &fakeBookingTool{})
	var timings []TurnTiming

	_, err := service.Message(context.Background(), "salon_1", "owner_1", "session_1", MessageRequest{
		Message: "Please explain the current guest policy.",
		TimingRecorder: func(timing TurnTiming) {
			timings = append(timings, timing)
		},
	})
	if err == nil {
		t.Fatal("Message unexpectedly succeeded after answer-context fence failure")
	}
	for _, timing := range timings {
		if timing.Stage != TurnTimingStageAnswerContext {
			continue
		}
		if timing.Result != TurnTimingResultError || timing.Attributes[answerContextTimingOutcome] != answerContextLoadOutcomeError ||
			timing.Attributes[answerContextTimingAttempts] != "1" || timing.Attributes[answerContextTimingCacheStatus] != answerContextCacheStatusMiss {
			t.Fatalf("answer-context failure timing = %#v", timing)
		}
		for key, value := range timing.Attributes {
			if key == err.Error() || value == err.Error() || key == "salon_1" || value == "salon_1" {
				t.Fatalf("failure timing leaked high-cardinality/error detail: %#v", timing.Attributes)
			}
		}
		return
	}
	t.Fatalf("answer-context failure timing missing: %#v", timings)
}

func BenchmarkAnswerContextStableCacheHitInMemory(b *testing.B) {
	for _, authority := range []string{
		booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
		booking.SchedulingAuthorityExternalProvider,
	} {
		b.Run(authority, func(b *testing.B) {
			store := newAnswerContextCountingStore(authority)
			service := NewService(store, &fakeBookingTool{})
			if _, err := service.loadAnswerContext(context.Background(), store.session.SalonID); err != nil {
				b.Fatalf("preload answer context: %v", err)
			}
			store.resetCalls()
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				answer, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
				if err != nil || !answer.CacheHit {
					b.Fatalf("stable cache hit answer=%#v err=%v", answer, err)
				}
			}
			b.StopTimer()
			if store.calls.Fence != b.N {
				b.Fatalf("fence reads = %d, want %d", store.calls.Fence, b.N)
			}
		})
	}
}
