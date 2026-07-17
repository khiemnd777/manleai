package conversation

import (
	"context"
	"testing"
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
