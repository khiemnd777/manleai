package conversation

import (
	"encoding/json"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestPartyRequestRecordUsesStructuredPartyPlanForSizeAndGuestServiceOrder(t *testing.T) {
	services := []ServiceOption{
		{ID: "builder-gel", Name: "Builder Gel"},
		{ID: "gel-removal", Name: "Gel Removal"},
	}
	session := Session{
		CustomerName:  "Linh",
		CustomerPhone: "555-0100",
		PartyPlan: &PartyPlan{
			PartySize: 2,
			Groups: []PartyPlanGroup{
				{Label: "first guest", Count: 1, ResolvedServiceIDs: []string{"builder-gel", "gel-removal"}},
				{Label: "second guest", Count: 1, ResolvedServiceIDs: []string{"builder-gel"}},
			},
		},
		// The session snapshot is deliberately incomplete. The reviewed PartyPlan,
		// not this compatibility snapshot or the latest wording, owns fidelity.
		BookingSegments: []booking.BookingSegmentRequest{{ServiceID: "gel-removal"}},
	}
	turn := TurnRecord{EventKey: "party-fidelity", CustomerMessage: "Please arrange this for seven people."}

	record := partyRequestRecordFromSession(turn, session, services, &RuntimeConfig{Timezone: "America/Chicago"}, "Owner review required")

	if record.PartySize != 2 {
		t.Fatalf("party size = %d, want structured plan size 2", record.PartySize)
	}
	want := []PartyGuestService{
		{GuestReference: "group-1-guest-1", ServiceID: "builder-gel", ServiceName: "Builder Gel", Quantity: 1, SortOrder: 1},
		{GuestReference: "group-1-guest-1", ServiceID: "gel-removal", ServiceName: "Gel Removal", Quantity: 1, SortOrder: 2},
		{GuestReference: "group-2-guest-1", ServiceID: "builder-gel", ServiceName: "Builder Gel", Quantity: 1, SortOrder: 3},
	}
	assertPartyGuestServices(t, record.GuestServiceRequests, want)
}

func TestPartyRequestRecordPreservesDifferentStructuredServicesWithoutPhraseInference(t *testing.T) {
	services := []ServiceOption{
		{ID: "classic-manicure", Name: "Classic Manicure"},
		{ID: "spa-pedicure", Name: "Spa Pedicure"},
		{ID: "nail-art", Name: "Nail Art"},
	}
	session := Session{PartyPlan: &PartyPlan{
		PartySize: 3,
		Groups: []PartyPlanGroup{
			{Label: "manicure guests", Count: 2, ResolvedServiceIDs: []string{"classic-manicure", "spa-pedicure"}},
			{Label: "art guest", Count: 1, ResolvedServiceIDs: []string{"nail-art"}},
		},
	}}
	turn := TurnRecord{CustomerMessage: "Those are the services we reviewed."}

	record := partyRequestRecordFromSession(turn, session, services, &RuntimeConfig{Timezone: "America/New_York"}, "Review")

	if record.PartySize != 3 {
		t.Fatalf("party size = %d, want 3", record.PartySize)
	}
	want := []PartyGuestService{
		{GuestReference: "group-1-guest-1", ServiceID: "classic-manicure", ServiceName: "Classic Manicure", Quantity: 1, SortOrder: 1},
		{GuestReference: "group-1-guest-2", ServiceID: "spa-pedicure", ServiceName: "Spa Pedicure", Quantity: 1, SortOrder: 2},
		{GuestReference: "group-2-guest-1", ServiceID: "nail-art", ServiceName: "Nail Art", Quantity: 1, SortOrder: 3},
	}
	assertPartyGuestServices(t, record.GuestServiceRequests, want)
}

func TestPartyRequestRecordLegacyFallbackPreservesDuplicateSegmentsWithoutInventingGuests(t *testing.T) {
	services := []ServiceOption{{ID: "dip-powder", Name: "Dip Powder"}}
	session := Session{BookingSegments: []booking.BookingSegmentRequest{
		{ServiceID: "dip-powder"},
		{ServiceID: "dip-powder"},
	}}
	turn := TurnRecord{CustomerMessage: "This is for nine guests."}

	record := partyRequestRecordFromSession(turn, session, services, &RuntimeConfig{Timezone: "UTC"}, "Legacy owner review")

	if record.PartySize != 0 {
		t.Fatalf("legacy party size = %d, want unknown rather than phrase-derived size", record.PartySize)
	}
	want := []PartyGuestService{
		{ServiceID: "dip-powder", ServiceName: "Dip Powder", Quantity: 1, SortOrder: 1},
		{ServiceID: "dip-powder", ServiceName: "Dip Powder", Quantity: 1, SortOrder: 2},
	}
	assertPartyGuestServices(t, record.GuestServiceRequests, want)
}

func TestPartyGuestServiceJSONRoundTripPreservesFidelityAndReadsLegacyRows(t *testing.T) {
	want := []PartyGuestService{
		{GuestReference: "group-1-guest-1", ServiceID: "builder-gel", ServiceName: "Builder Gel", Quantity: 1, SortOrder: 1},
		{GuestReference: "group-2-guest-1", ServiceID: "builder-gel", ServiceName: "Builder Gel", Quantity: 1, SortOrder: 2},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal guest services: %v", err)
	}
	var got []PartyGuestService
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal guest services: %v", err)
	}
	assertPartyGuestServices(t, got, want)

	var legacy []PartyGuestService
	if err := json.Unmarshal([]byte(`[{"service_id":"legacy-service","service_name":"Legacy Service"}]`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy guest services: %v", err)
	}
	if len(legacy) != 1 || legacy[0].ServiceID != "legacy-service" || legacy[0].GuestReference != "" || legacy[0].Quantity != 0 || legacy[0].SortOrder != 0 {
		t.Fatalf("legacy guest service = %#v", legacy)
	}
}

func assertPartyGuestServices(t *testing.T, got, want []PartyGuestService) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("guest service count = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("guest service %d = %#v, want %#v", index, got[index], want[index])
		}
	}
}
