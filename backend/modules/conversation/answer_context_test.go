package conversation

import (
	"context"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	calendar "github.com/manleai/ai-receptionist/modules/scheduling_manleai_calendar"
)

func TestLoadAnswerContextValidatesDatabaseReadinessFenceOnEveryTurn(t *testing.T) {
	store := newFakeConversationStore()
	store.serviceAliases = []ServiceAlias{{ID: "alias_a", ServiceID: "service_1", Alias: "A manicure"}}
	store.categoryAliases = []ServiceCategoryAlias{{ID: "category_alias_a", CategoryID: "category_a", Alias: "A nails"}}
	store.activeStaff = []StaffOption{{ID: "staff_1", Name: "Location A Artist", AIBookable: true}}
	store.businessHours = []BusinessHourPeriod{{ID: "hours_a", DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "17:00:00"}}
	store.knowledge = []KnowledgeSnippet{{ID: "knowledge_1", Title: "Parking", Body: "Parking is behind the salon."}}
	service := NewService(store, &fakeBookingTool{})

	first, err := service.loadAnswerContext(context.Background(), "salon_1")
	if err != nil {
		t.Fatalf("load location A answer context: %v", err)
	}
	if len(first.Services) != 1 || first.Services[0].ID != "service_1" || first.CacheHit {
		t.Fatalf("location A answer context = %#v", first)
	}

	// Simulate another replica beginning a new provider snapshot. Canonical
	// active-provider service guidance remains usable, but provider-backed
	// availability data and booking eligibility must fail closed.
	store.answerContextFence = AnswerContextFence{
		ActiveProvider: "square", ConnectionStatus: "syncing", LocationID: "location_2",
		SnapshotGeneration: 2,
	}
	notReady, err := service.loadAnswerContext(context.Background(), "salon_1")
	if err != nil {
		t.Fatalf("load syncing answer context: %v", err)
	}
	if len(notReady.Services) != 1 || notReady.Services[0].ID != "service_1" || notReady.Services[0].BookingReady ||
		len(notReady.ServiceAliases) != 1 || len(notReady.CategoryAliases) != 1 ||
		len(notReady.Staff) != 0 || len(notReady.ActiveStaff) != 0 || len(notReady.BusinessHours) != 0 {
		t.Fatalf("syncing context did not separate guidance from booking data: %#v", notReady)
	}
	if len(notReady.Knowledge) != 1 || notReady.Knowledge[0].ID != "knowledge_1" {
		t.Fatalf("salon-authored knowledge should remain available: %#v", notReady.Knowledge)
	}

	cachedNotReady, err := service.loadAnswerContext(context.Background(), "salon_1")
	if err != nil {
		t.Fatalf("reload stable syncing answer context: %v", err)
	}
	if !cachedNotReady.CacheHit || len(cachedNotReady.Services) != 1 || cachedNotReady.Services[0].BookingReady {
		t.Fatalf("stable fail-closed context was not cached safely: %#v", cachedNotReady)
	}

	store.services = []ServiceOption{{ID: "service_b", Name: "Location B Pedicure", DurationMinutes: 50}}
	store.serviceAliases = []ServiceAlias{{ID: "alias_b", ServiceID: "service_b", Alias: "B pedicure"}}
	store.categoryAliases = []ServiceCategoryAlias{{ID: "category_alias_b", CategoryID: "category_b", Alias: "B toes"}}
	store.staff = []StaffOption{{ID: "staff_b", Name: "Location B Artist", AIBookable: true}}
	store.activeStaff = append([]StaffOption(nil), store.staff...)
	store.businessHours = []BusinessHourPeriod{{ID: "hours_b", DayOfWeek: 2, StartLocalTime: "10:00:00", EndLocalTime: "18:00:00"}}
	store.answerContextFence = AnswerContextFence{
		ActiveProvider: "square", ConnectionStatus: "active", LocationID: "location_2",
		SnapshotGeneration: 2, LastSyncAtRFC3339: "2026-06-02T12:00:00Z",
	}
	locationB, err := service.loadAnswerContext(context.Background(), "salon_1")
	if err != nil {
		t.Fatalf("load completed location B answer context: %v", err)
	}
	if len(locationB.Services) != 1 || locationB.Services[0].ID != "service_b" || !locationB.Services[0].BookingReady ||
		len(locationB.Staff) != 1 || locationB.Staff[0].ID != "staff_b" ||
		len(locationB.ServiceAliases) != 1 || locationB.ServiceAliases[0].ID != "alias_b" ||
		len(locationB.BusinessHours) != 1 || locationB.BusinessHours[0].ID != "hours_b" {
		t.Fatalf("location B context did not replace location A: %#v", locationB)
	}
	if store.answerFenceCalls != 7 {
		t.Fatalf("database fence calls = %d, want every load plus miss verification", store.answerFenceCalls)
	}
	if store.serviceListCalls != 3 {
		t.Fatalf("service list calls = %d, want one per cache miss", store.serviceListCalls)
	}
}

func TestLoadAnswerContextUsesOwnerManagedHoursWithoutProviderHours(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityOwnerManual,
		SchedulingAuthorityVersion: 3,
		LocalBusinessHoursVersion:  8,
	}
	store.ownerHours = []BusinessHourPeriod{{
		ID: "owner_hours", DayOfWeek: 4, StartLocalTime: "09:15:00", EndLocalTime: "18:45:00", Source: "local_override",
	}}
	store.businessHours = []BusinessHourPeriod{{
		ID: "provider_hours", DayOfWeek: 4, StartLocalTime: "08:00:00", EndLocalTime: "20:00:00", Source: "imported", Provider: "square",
	}}

	service := NewService(store, &fakeBookingTool{})
	answer, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load owner-managed answer context: %v", err)
	}
	if answer.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || len(answer.BusinessHours) != 1 || answer.BusinessHours[0].ID != "owner_hours" {
		t.Fatalf("owner-managed hours projection = %#v", answer)
	}
	if store.ownerHoursListCalls != 1 || store.hoursListCalls != 0 || store.internalHoursCalls != 0 {
		t.Fatalf("hours source calls owner/external/internal = %d/%d/%d", store.ownerHoursListCalls, store.hoursListCalls, store.internalHoursCalls)
	}
}

func TestLoadAnswerContextExternalProviderIgnoresOwnerManagedHours(t *testing.T) {
	store := newFakeConversationStore()
	store.ownerHours = []BusinessHourPeriod{{ID: "owner_hours", Source: "local_override"}}
	store.businessHours = []BusinessHourPeriod{{ID: "provider_hours", Source: "imported", Provider: "square"}}
	service := NewService(store, &fakeBookingTool{})

	answer, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load external-provider answer context: %v", err)
	}
	if answer.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || len(answer.BusinessHours) != 1 || answer.BusinessHours[0].ID != "provider_hours" {
		t.Fatalf("external-provider hours projection = %#v", answer)
	}
	if store.hoursListCalls != 1 || store.ownerHoursListCalls != 0 || store.internalHoursCalls != 0 {
		t.Fatalf("hours source calls external/owner/internal = %d/%d/%d", store.hoursListCalls, store.ownerHoursListCalls, store.internalHoursCalls)
	}
}

func TestLoadAnswerContextInvalidatesOwnerHoursCacheOnResourceVersionChange(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityOwnerManual,
		SchedulingAuthorityVersion: 2,
		LocalBusinessHoursVersion:  4,
	}
	store.ownerHours = []BusinessHourPeriod{{ID: "hours_v4", Source: "local_override"}}
	service := NewService(store, &fakeBookingTool{})

	first, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load owner hours version 4: %v", err)
	}
	if first.CacheHit || len(first.BusinessHours) != 1 || first.BusinessHours[0].ID != "hours_v4" {
		t.Fatalf("owner hours version 4 context = %#v", first)
	}

	store.answerContextFence.LocalBusinessHoursVersion = 5
	store.ownerHours = []BusinessHourPeriod{{ID: "hours_v5", Source: "local_override"}}
	second, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load owner hours version 5: %v", err)
	}
	if second.CacheHit || len(second.BusinessHours) != 1 || second.BusinessHours[0].ID != "hours_v5" {
		t.Fatalf("owner hours version did not replace cached context: %#v", second)
	}
	if store.ownerHoursListCalls != 2 {
		t.Fatalf("owner hours loads = %d, want one per resource version", store.ownerHoursListCalls)
	}
}

func TestLoadAnswerContextInvalidatesCachedCommonResourcesOnCollectionVersionChange(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeConversationStore, *AnswerContextFence)
		assert func(*testing.T, *AIAnswerContext)
	}{
		{
			name: "service catalog",
			mutate: func(store *fakeConversationStore, fence *AnswerContextFence) {
				fence.ServiceCatalogVersion++
				store.guidanceServices = []ServiceOption{{ID: "service_new", Name: "Builder Gel Overlay", DurationMinutes: 70}}
			},
			assert: func(t *testing.T, answer *AIAnswerContext) {
				t.Helper()
				if len(answer.Services) != 1 || answer.Services[0].ID != "service_new" {
					t.Fatalf("service catalog was not refreshed: %#v", answer.Services)
				}
			},
		},
		{
			name: "service aliases",
			mutate: func(store *fakeConversationStore, fence *AnswerContextFence) {
				fence.ServiceAliasesVersion++
				store.serviceAliases = []ServiceAlias{{ID: "alias_new", ServiceID: "service_1", Alias: "Hard gel refill"}}
			},
			assert: func(t *testing.T, answer *AIAnswerContext) {
				t.Helper()
				if len(answer.ServiceAliases) != 1 || answer.ServiceAliases[0].ID != "alias_new" {
					t.Fatalf("service aliases were not refreshed: %#v", answer.ServiceAliases)
				}
			},
		},
		{
			name: "service categories",
			mutate: func(store *fakeConversationStore, fence *AnswerContextFence) {
				fence.ServiceCategoriesVersion++
				store.categoryAliases = []ServiceCategoryAlias{{ID: "category_alias_new", CategoryID: "category_new", Alias: "Nail enhancements"}}
			},
			assert: func(t *testing.T, answer *AIAnswerContext) {
				t.Helper()
				if len(answer.CategoryAliases) != 1 || answer.CategoryAliases[0].ID != "category_alias_new" {
					t.Fatalf("service category aliases were not refreshed: %#v", answer.CategoryAliases)
				}
			},
		},
		{
			name: "consultation profiles",
			mutate: func(store *fakeConversationStore, fence *AnswerContextFence) {
				fence.ConsultationProfilesVersion++
				store.guidanceServices = []ServiceOption{{
					ID: "service_1", Name: "Classic Manicure", DurationMinutes: 45,
					ConsultationProfile: &ServiceConsultationProfile{Status: "ready", OwnerApprovedSummary: "A durable overlay for flexible natural nails.", Revision: 2},
				}}
			},
			assert: func(t *testing.T, answer *AIAnswerContext) {
				t.Helper()
				if len(answer.Services) != 1 || answer.Services[0].ConsultationProfile == nil || answer.Services[0].ConsultationProfile.Revision != 2 {
					t.Fatalf("consultation profiles were not refreshed: %#v", answer.Services)
				}
			},
		},
		{
			name: "staff catalog",
			mutate: func(store *fakeConversationStore, fence *AnswerContextFence) {
				fence.StaffCatalogVersion++
				store.activeStaff = []StaffOption{{ID: "staff_new", Name: "Anh Le", AIBookable: true}}
			},
			assert: func(t *testing.T, answer *AIAnswerContext) {
				t.Helper()
				if len(answer.ActiveStaff) != 1 || answer.ActiveStaff[0].ID != "staff_new" || len(answer.Staff) != 1 || answer.Staff[0].ID != "staff_new" {
					t.Fatalf("staff catalog was not refreshed: staff=%#v active=%#v", answer.Staff, answer.ActiveStaff)
				}
			},
		},
		{
			name: "knowledge base",
			mutate: func(store *fakeConversationStore, fence *AnswerContextFence) {
				fence.KnowledgeBaseVersion++
				store.knowledge = []KnowledgeSnippet{{ID: "knowledge_new", Title: "Gift cards", Body: "Digital gift cards are available."}}
			},
			assert: func(t *testing.T, answer *AIAnswerContext) {
				t.Helper()
				if len(answer.Knowledge) != 1 || answer.Knowledge[0].ID != "knowledge_new" {
					t.Fatalf("knowledge base was not refreshed: %#v", answer.Knowledge)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.answerContextFence = AnswerContextFence{
				SchedulingAuthority:         booking.SchedulingAuthorityOwnerManual,
				SchedulingAuthorityVersion:  4,
				ServiceCatalogVersion:       1,
				ServiceAliasesVersion:       1,
				ServiceCategoriesVersion:    1,
				ConsultationProfilesVersion: 1,
				StaffCatalogVersion:         1,
				KnowledgeBaseVersion:        1,
				LocalBusinessHoursVersion:   1,
			}
			store.guidanceServices = []ServiceOption{{ID: "service_1", Name: "Classic Manicure", DurationMinutes: 45}}
			store.serviceAliases = []ServiceAlias{{ID: "alias_old", ServiceID: "service_1", Alias: "Regular manicure"}}
			store.categoryAliases = []ServiceCategoryAlias{{ID: "category_alias_old", CategoryID: "category_old", Alias: "Natural nails"}}
			store.activeStaff = []StaffOption{{ID: "staff_old", Name: "Mai Nguyen", AIBookable: true}}
			store.knowledge = []KnowledgeSnippet{{ID: "knowledge_old", Title: "Parking", Body: "Parking is behind the salon."}}
			store.ownerHours = []BusinessHourPeriod{{ID: "owner_hours", Source: "local_override"}}
			service := NewService(store, &fakeBookingTool{})

			first, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
			if err != nil {
				t.Fatalf("load initial answer context: %v", err)
			}
			if first.CacheHit {
				t.Fatal("initial answer context unexpectedly came from cache")
			}
			cached, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
			if err != nil {
				t.Fatalf("load cached answer context: %v", err)
			}
			if !cached.CacheHit {
				t.Fatal("stable answer context did not use the cache")
			}

			updatedFence := store.answerContextFence
			test.mutate(store, &updatedFence)
			store.answerContextFence = updatedFence
			refreshed, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
			if err != nil {
				t.Fatalf("load refreshed answer context: %v", err)
			}
			if refreshed.CacheHit {
				t.Fatal("collection version change reused stale cached answer context")
			}
			test.assert(t, refreshed)
			if store.answerFenceCalls != 5 {
				t.Fatalf("answer-context fence calls = %d, want initial verification, cache validation, and refresh verification", store.answerFenceCalls)
			}
		})
	}
}

type changingAnswerContextFenceStore struct {
	*fakeConversationStore
	fences       []AnswerContextFence
	calls        int
	mutateAtCall int
	mutate       func()
}

func (s *changingAnswerContextFenceStore) GetAnswerContextFence(ctx context.Context, salonID string) (AnswerContextFence, error) {
	s.calls++
	if s.calls == s.mutateAtCall && s.mutate != nil {
		s.mutate()
	}
	index := s.calls - 1
	if index >= len(s.fences) {
		index = len(s.fences) - 1
	}
	return s.fences[index], nil
}

func TestLoadAnswerContextRetriesWhenCollectionChangesBetweenFenceReads(t *testing.T) {
	baseStore := newFakeConversationStore()
	baseStore.guidanceServices = []ServiceOption{{ID: "service_old", Name: "Express Manicure", DurationMinutes: 30}}
	baseStore.activeStaff = []StaffOption{{ID: "staff_1", Name: "Mai Nguyen", AIBookable: true}}
	baseStore.ownerHours = []BusinessHourPeriod{{ID: "owner_hours", Source: "local_override"}}
	versionOne := AnswerContextFence{
		SchedulingAuthority:   booking.SchedulingAuthorityOwnerManual,
		ServiceCatalogVersion: 1, StaffCatalogVersion: 1, KnowledgeBaseVersion: 1,
		LocalBusinessHoursVersion: 1,
	}
	versionTwo := versionOne
	versionTwo.ServiceCatalogVersion = 2
	store := &changingAnswerContextFenceStore{
		fakeConversationStore: baseStore,
		fences:                []AnswerContextFence{versionOne, versionTwo, versionTwo, versionTwo},
		mutateAtCall:          2,
	}
	store.mutate = func() {
		baseStore.guidanceServices = []ServiceOption{{ID: "service_new", Name: "Dry E-File Manicure", DurationMinutes: 60}}
	}
	service := NewService(store, &fakeBookingTool{})

	answer, err := service.loadAnswerContext(context.Background(), baseStore.session.SalonID)
	if err != nil {
		t.Fatalf("load answer context across concurrent collection change: %v", err)
	}
	if answer.CacheHit || len(answer.Services) != 1 || answer.Services[0].ID != "service_new" {
		t.Fatalf("answer context did not retry onto the stable collection version: %#v", answer)
	}
	if store.calls != 4 {
		t.Fatalf("answer-context fence calls = %d, want two reads for each of two load attempts", store.calls)
	}
	if baseStore.knowledgeListCalls != 2 {
		t.Fatalf("fresh answer-context loads = %d, want stale attempt discarded and reloaded", baseStore.knowledgeListCalls)
	}
}

func TestLoadAnswerContextSwitchesHoursSourceWithSchedulingAuthority(t *testing.T) {
	store := newFakeConversationStore()
	store.businessHours = []BusinessHourPeriod{{ID: "provider_hours", Source: "imported", Provider: "square"}}
	store.ownerHours = []BusinessHourPeriod{{ID: "owner_hours", Source: "local_override"}}
	service := NewService(store, &fakeBookingTool{})

	external, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load external context before authority switch: %v", err)
	}
	if len(external.BusinessHours) != 1 || external.BusinessHours[0].ID != "provider_hours" {
		t.Fatalf("external context before switch = %#v", external)
	}

	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityOwnerManual,
		SchedulingAuthorityVersion: 2,
		LocalBusinessHoursVersion:  6,
	}
	owner, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load owner context after authority switch: %v", err)
	}
	if owner.CacheHit || owner.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || len(owner.BusinessHours) != 1 || owner.BusinessHours[0].ID != "owner_hours" {
		t.Fatalf("owner context after authority switch = %#v", owner)
	}
	if store.hoursListCalls != 1 || store.ownerHoursListCalls != 1 {
		t.Fatalf("hours source calls external/owner = %d/%d", store.hoursListCalls, store.ownerHoursListCalls)
	}
}

func TestLoadAnswerContextUsesActivatedInternalPoliciesAndLocalOverrides(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
		SchedulingAuthorityVersion: 7,
		CalendarConfigVersion:      12,
		CalendarActivatedVersion:   12,
	}
	store.services = nil
	store.guidanceServices = []ServiceOption{
		{ID: "service_internal", Name: "Structured Gel Manicure", DurationMinutes: 55},
		{ID: "service_not_enabled", Name: "Seasonal Nail Art", DurationMinutes: 30},
	}
	store.internalServices = []ServiceOption{{ID: "service_internal", Name: "Structured Gel Manicure", DurationMinutes: 55, BookingReady: true}}
	store.staff = nil
	store.activeStaff = []StaffOption{
		{ID: "staff_internal", Name: "Thao", AIBookable: true},
		{ID: "staff_not_scheduled", Name: "Lan", AIBookable: true},
	}
	store.internalStaff = []StaffOption{{ID: "staff_internal", Name: "Thao", AIBookable: true}}
	store.businessHours = []BusinessHourPeriod{{ID: "provider_hours", Source: "imported", Provider: "square"}}
	store.internalHours = []BusinessHourPeriod{{ID: "local_hours", DayOfWeek: 3, StartLocalTime: "10:00:00", EndLocalTime: "24:00:00", Source: "local_override"}}

	service := NewService(store, &fakeBookingTool{})
	answer, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("loadAnswerContext returned error: %v", err)
	}
	if len(answer.Services) != 2 || !answer.Services[0].BookingReady || answer.Services[1].BookingReady {
		t.Fatalf("internal service policy projection = %#v", answer.Services)
	}
	if len(answer.Staff) != 1 || answer.Staff[0].ID != "staff_internal" || len(answer.ActiveStaff) != 2 {
		t.Fatalf("internal staff projection = staff %#v active %#v", answer.Staff, answer.ActiveStaff)
	}
	if len(answer.BusinessHours) != 1 || answer.BusinessHours[0].ID != "local_hours" || answer.BusinessHours[0].Source != "local_override" || answer.BusinessHours[0].Provider != "" {
		t.Fatalf("internal hours projection = %#v", answer.BusinessHours)
	}
	if got := formatBusinessHourRanges(answer.BusinessHours); got != "10:00 AM to 12:00 AM" {
		t.Fatalf("midnight business-hours rendering = %q", got)
	}
}

func TestLoadAnswerContextInvalidatesInternalCacheOnConfigVersionChange(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
		SchedulingAuthorityVersion: 2,
		CalendarConfigVersion:      4,
		CalendarActivatedVersion:   4,
	}
	store.internalServices = append([]ServiceOption(nil), store.services...)
	store.internalStaff = append([]StaffOption(nil), store.staff...)
	store.internalHours = []BusinessHourPeriod{{ID: "hours_v4", Source: "local_override"}}
	service := NewService(store, &fakeBookingTool{})

	first, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load version 4 context: %v", err)
	}
	if first.CacheHit || len(first.BusinessHours) != 1 || first.BusinessHours[0].ID != "hours_v4" {
		t.Fatalf("version 4 context = %#v", first)
	}

	store.answerContextFence.CalendarConfigVersion = 5
	store.answerContextFence.CalendarActivatedVersion = 5
	store.internalHours = []BusinessHourPeriod{{ID: "hours_v5", Source: "local_override"}}
	second, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load version 5 context: %v", err)
	}
	if second.CacheHit || len(second.BusinessHours) != 1 || second.BusinessHours[0].ID != "hours_v5" {
		t.Fatalf("version 5 context did not replace cache: %#v", second)
	}
}

func TestLoadAnswerContextStableManleAICalendarCacheHitSkipsAggregateEvidenceLoad(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
		SchedulingAuthorityVersion: 9,
		ServiceCatalogVersion:      3,
		StaffCatalogVersion:        4,
		CalendarConfigVersion:      17,
		CalendarActivatedVersion:   17,
	}
	store.guidanceServices = []ServiceOption{{ID: "service_shellac", Name: "Shellac Manicure", DurationMinutes: 50}}
	store.internalServices = append([]ServiceOption(nil), store.guidanceServices...)
	store.activeStaff = []StaffOption{{ID: "staff_linh", Name: "Linh", AIBookable: true}}
	store.internalStaff = append([]StaffOption(nil), store.activeStaff...)
	store.internalHours = []BusinessHourPeriod{{ID: "hours_saturday", DayOfWeek: 6, StartLocalTime: "10:00:00", EndLocalTime: "19:00:00", Source: "local_override"}}
	service := NewService(store, &fakeBookingTool{})

	first, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load initial calendar answer context: %v", err)
	}
	second, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("load cached calendar answer context: %v", err)
	}
	if first.CacheHit || !second.CacheHit {
		t.Fatalf("calendar cache states first/second = %t/%t", first.CacheHit, second.CacheHit)
	}
	if store.calendarEvidenceCalls != 1 {
		t.Fatalf("calendar aggregate evidence loads = %d, want one cache-miss load", store.calendarEvidenceCalls)
	}
	if store.answerFenceCalls != 3 {
		t.Fatalf("database fence loads = %d, want two for miss and one for hit", store.answerFenceCalls)
	}
	if store.internalHoursCalls != 1 || store.knowledgeListCalls != 1 {
		t.Fatalf("fresh calendar context loads hours/knowledge = %d/%d, want one each", store.internalHoursCalls, store.knowledgeListCalls)
	}
}

type changingManleAICalendarEvidenceStore struct {
	*fakeConversationStore
	firstEvidenceVersion int64
}

func (s *changingManleAICalendarEvidenceStore) GetManleAICalendarAnswerContextEvidence(ctx context.Context, salonID string) (manleAICalendarAnswerContextEvidence, error) {
	s.calendarEvidenceCalls++
	if s.calendarEvidenceCalls == 1 {
		updated := s.answerContextFence
		updated.CalendarConfigVersion = s.firstEvidenceVersion
		updated.CalendarActivatedVersion = s.firstEvidenceVersion
		s.answerContextFence = updated
	}
	return manleAICalendarAnswerContextEvidence{
		SchedulingAuthority:        s.answerContextFence.SchedulingAuthority,
		SchedulingAuthorityVersion: s.answerContextFence.SchedulingAuthorityVersion,
		CalendarConfigVersion:      s.answerContextFence.CalendarConfigVersion,
		CalendarActivatedVersion:   s.answerContextFence.CalendarActivatedVersion,
		Ready:                      true,
	}, nil
}

func TestLoadAnswerContextRetriesBeforeFreshLoadWhenCalendarEvidenceVersionChanged(t *testing.T) {
	base := newFakeConversationStore()
	base.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
		SchedulingAuthorityVersion: 2,
		CalendarConfigVersion:      10,
		CalendarActivatedVersion:   10,
	}
	base.guidanceServices = []ServiceOption{{ID: "service_old", Name: "Express Pedicure", DurationMinutes: 35}}
	base.internalServices = append([]ServiceOption(nil), base.guidanceServices...)
	base.internalStaff = []StaffOption{{ID: "staff_mai", Name: "Mai", AIBookable: true}}
	base.internalHours = []BusinessHourPeriod{{ID: "hours_v11", Source: "local_override"}}
	store := &changingManleAICalendarEvidenceStore{fakeConversationStore: base, firstEvidenceVersion: 11}
	service := NewService(store, &fakeBookingTool{})

	answer, err := service.loadAnswerContext(context.Background(), base.session.SalonID)
	if err != nil {
		t.Fatalf("load calendar context across evidence change: %v", err)
	}
	if answer.CacheHit || len(answer.BusinessHours) != 1 || answer.BusinessHours[0].ID != "hours_v11" {
		t.Fatalf("calendar retry result = %#v", answer)
	}
	if store.calendarEvidenceCalls != 2 {
		t.Fatalf("calendar evidence loads = %d, want changed attempt plus stable attempt", store.calendarEvidenceCalls)
	}
	if store.knowledgeListCalls != 1 {
		t.Fatalf("fresh context loads = %d, stale evidence attempt should stop before hydration", store.knowledgeListCalls)
	}
}

func TestLoadAnswerContextInvalidatesCalendarCacheWhenActivationBecomesStale(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
		SchedulingAuthorityVersion: 3,
		CalendarConfigVersion:      21,
		CalendarActivatedVersion:   21,
	}
	store.guidanceServices = []ServiceOption{{ID: "service_builder", Name: "Builder Gel Balance", DurationMinutes: 75}}
	store.internalServices = append([]ServiceOption(nil), store.guidanceServices...)
	store.activeStaff = []StaffOption{{ID: "staff_thao", Name: "Thao", AIBookable: true}}
	store.internalStaff = append([]StaffOption(nil), store.activeStaff...)
	store.internalHours = []BusinessHourPeriod{{ID: "hours_current", Source: "local_override"}}
	service := NewService(store, &fakeBookingTool{})

	if _, err := service.loadAnswerContext(context.Background(), store.session.SalonID); err != nil {
		t.Fatalf("prewarm activated calendar context: %v", err)
	}
	store.answerContextFence.CalendarConfigVersion = 22
	store.calendarReady = false
	stale, err := service.loadAnswerContext(context.Background(), store.session.SalonID)
	if err != nil {
		t.Fatalf("reload stale activation context: %v", err)
	}
	if stale.CacheHit || len(stale.Staff) != 0 || len(stale.ActiveStaff) != 0 || len(stale.BusinessHours) != 0 || stale.Services[0].BookingReady {
		t.Fatalf("stale activation context did not fail closed: %#v", stale)
	}
}

func TestManleAICalendarAnswerFenceUsesAuthoritativeCapability(t *testing.T) {
	version := int64(8)
	now := time.Now().UTC()
	mode := calendar.CapacityModeStaffOnly
	staff := calendar.StaffRef{ID: "staff_ready", Name: "Thao", Active: true, AIBookable: true}
	readyService := calendar.ServiceRef{ID: "service_ready", Name: "Structured Gel", DurationMinutes: 55, Active: true, AIBookable: true}
	missingPolicyService := calendar.ServiceRef{ID: "service_missing_policy", Name: "Spa Pedicure", DurationMinutes: 65, Active: true, AIBookable: true}
	aggregate := &calendar.Aggregate{
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		AuthorityVersion:    3,
		ConfigVersion:       version,
		Config: &calendar.CalendarConfig{
			Version: version, ActivatedAt: &now, ActivatedVersion: &version,
		},
		Hours: []calendar.BusinessHourPeriod{{ID: "hours", DayOfWeek: 2, StartMinute: 600, EndMinute: 1080}},
		StaffProfiles: []calendar.StaffProfile{{
			Staff:            staff,
			WeeklyPeriods:    []calendar.WeeklyPeriod{{ID: "weekly", StaffID: staff.ID, DayOfWeek: 2, StartMinute: 600, EndMinute: 1080}},
			EligibleServices: []calendar.ServiceRef{readyService, missingPolicyService},
		}},
		ServicePolicies: []calendar.ServicePolicy{
			{Service: readyService, Configured: true, Enabled: true, CapacityMode: &mode, EligibleStaff: []calendar.StaffRef{staff}},
			// This row mirrors the aggregate loader's unconfigured projection for
			// another eligible canonical service. The removed EXISTS predicate
			// would have seen the first valid policy and incorrectly returned ready.
			{Service: missingPolicyService, Configured: false},
		},
	}

	evidence := projectManleAICalendarAnswerContextEvidence(aggregate)
	if evidence.Ready {
		t.Fatal("answer-context fence became ready while authoritative configuration readiness is false")
	}

	aggregate.ServicePolicies = aggregate.ServicePolicies[:1]
	aggregate.StaffProfiles[0].EligibleServices = []calendar.ServiceRef{readyService}
	evidence = projectManleAICalendarAnswerContextEvidence(aggregate)
	if !evidence.Ready || evidence.CalendarConfigVersion != version || evidence.CalendarActivatedVersion != version || evidence.SchedulingAuthorityVersion != aggregate.AuthorityVersion {
		t.Fatalf("authoritative staff-only capability was not projected: %#v", evidence)
	}
}
