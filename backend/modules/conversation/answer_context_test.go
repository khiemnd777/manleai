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
		SnapshotGeneration: 2, Ready: false,
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
		SnapshotGeneration: 2, LastSyncAtRFC3339: "2026-06-02T12:00:00Z", Ready: true,
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

func TestLoadAnswerContextUsesActivatedInternalPoliciesAndLocalOverrides(t *testing.T) {
	store := newFakeConversationStore()
	store.answerContextFence = AnswerContextFence{
		SchedulingAuthority:        booking.SchedulingAuthorityManleAICalendar,
		SchedulingAuthorityVersion: 7,
		CalendarConfigVersion:      12,
		CalendarActivatedVersion:   12,
		Ready:                      true,
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
		Ready:                      true,
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

	fence := AnswerContextFence{Ready: true}
	applyManleAICalendarCapabilityFence(&fence, aggregate)
	if fence.Ready {
		t.Fatal("answer-context fence became ready while authoritative configuration readiness is false")
	}

	aggregate.ServicePolicies = aggregate.ServicePolicies[:1]
	aggregate.StaffProfiles[0].EligibleServices = []calendar.ServiceRef{readyService}
	applyManleAICalendarCapabilityFence(&fence, aggregate)
	if !fence.Ready || fence.CalendarConfigVersion != version || fence.CalendarActivatedVersion != version || fence.SchedulingAuthorityVersion != aggregate.AuthorityVersion {
		t.Fatalf("authoritative staff-only capability was not projected: %#v", fence)
	}
}
