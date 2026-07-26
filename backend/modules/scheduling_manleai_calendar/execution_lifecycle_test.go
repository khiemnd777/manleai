package scheduling_manleai_calendar

import (
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestLifecycleCutoffNullDisabledAndEqualityClosed(t *testing.T) {
	start := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	equality := start.Add(-2 * time.Hour)
	cutoff := 120
	if !lifecycleCutoffOpen(start.Add(time.Hour), start, nil) {
		t.Fatal("NULL cutoff must disable the cutoff even after appointment start")
	}
	if lifecycleCutoffOpen(equality, start, &cutoff) {
		t.Fatal("cutoff equality must be closed; contract requires now < start-cutoff")
	}
	if !lifecycleCutoffOpen(equality.Add(-time.Nanosecond), start, &cutoff) {
		t.Fatal("instant before cutoff must remain open")
	}
}

func TestReplacementPreservesPartyServiceUnitAndGuestMapping(t *testing.T) {
	target := lifecycleTarget{
		PartySize: 2,
		Segments: []quotedAggregateSegment{
			{ServiceID: "service-a", GuestReference: "guest-a", SortOrder: 1},
			{ServiceID: "service-b", GuestReference: "guest-b", SortOrder: 2},
		},
	}
	replacement := normalizedAvailabilityRequest{
		PartySize: 2,
		Segments: []normalizedAvailabilitySegment{
			{ServiceID: "service-a", StaffID: "new-staff-a", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-a", Quantity: 1},
			{ServiceID: "service-b", StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-b", Quantity: 1},
		},
	}
	if !replacementPreservesTargetShape(replacement, target) {
		t.Fatal("staff reassignment with exact party/service-unit/guest mapping must remain valid")
	}
	changedGuest := replacement
	changedGuest.Segments = append([]normalizedAvailabilitySegment{}, replacement.Segments...)
	changedGuest.Segments[1].GuestReference = "guest-a"
	if replacementPreservesTargetShape(changedGuest, target) {
		t.Fatal("guest remapping must reject the whole-root replacement")
	}
	partial := replacement
	partial.Segments = partial.Segments[:1]
	if replacementPreservesTargetShape(partial, target) {
		t.Fatal("partial replacement must reject the whole-root mutation")
	}
}

func TestHistoricalTargetPlannerDoesNotRequireCurrentAuthority(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	snapshot.Aggregate.SchedulingAuthority = booking.SchedulingAuthorityExternalProvider
	request := normalizedAvailabilityRequest{
		PartySize: 1,
		Segments: []normalizedAvailabilitySegment{{
			ServiceID:          snapshot.Aggregate.ServicePolicies[0].Service.ID,
			StaffID:            snapshot.Aggregate.StaffProfiles[0].Staff.ID,
			StaffSelectionMode: booking.StaffSelectionSpecific,
			Quantity:           1,
		}},
		PreferredDate: "2026-02-10",
		Limit:         1,
	}
	now := time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC)
	if _, err := planAggregateAvailability(snapshot, request, now); err != booking.ErrSchedulingAuthorityNotReady {
		t.Fatalf("origin-free planner error = %v", err)
	}
	snapshot.TargetOriginAuthorized = true
	slots, err := planAggregateAvailability(snapshot, request, now)
	if err != nil || len(slots) == 0 {
		t.Fatalf("target-origin planner = %d slots / %v", len(slots), err)
	}
}
