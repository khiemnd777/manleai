package scheduling_manleai_calendar

import (
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestStrictLocalInstantRejectsDSTGapAndOverlap(t *testing.T) {
	location, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}

	normal, ok := strictLocalInstant(2026, time.February, 10, 9, 30, location)
	if !ok || normal.Format(time.RFC3339) != "2026-02-10T15:30:00Z" {
		t.Fatalf("normal local instant = %s/%t", normal.Format(time.RFC3339), ok)
	}
	if value, ok := strictLocalInstant(2026, time.March, 8, 2, 30, location); ok || !value.IsZero() {
		t.Fatalf("spring gap resolved to %s/%t", value.Format(time.RFC3339), ok)
	}
	if value, ok := strictLocalInstant(2026, time.November, 1, 1, 30, location); ok || !value.IsZero() {
		t.Fatalf("fall overlap resolved to %s/%t", value.Format(time.RFC3339), ok)
	}
}

func TestPlanStaffOnlyAvailabilityAppliesBuffersExceptionsAndReservations(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	location, _ := time.LoadLocation(snapshot.Aggregate.Timezone)
	staffUnavailableStart, _ := strictLocalInstant(2026, time.February, 10, 10, 30, location)
	staffUnavailableEnd, _ := strictLocalInstant(2026, time.February, 10, 11, 30, location)
	snapshot.Aggregate.Exceptions = append(snapshot.Aggregate.Exceptions, CalendarException{
		ScopeType: ExceptionScopeStaff, StaffID: snapshot.Aggregate.StaffProfiles[0].Staff.ID,
		Effect: ExceptionEffectUnavailable, StartsAt: staffUnavailableStart, EndsAt: staffUnavailableEnd,
	})
	reservationStart, _ := strictLocalInstant(2026, time.February, 10, 12, 0, location)
	reservationEnd, _ := strictLocalInstant(2026, time.February, 10, 13, 0, location)
	snapshot.Conflicts = []StaffConflict{{StaffID: snapshot.Aggregate.StaffProfiles[0].Staff.ID, StartsAt: reservationStart, EndsAt: reservationEnd}}

	request := normalizedAvailabilityRequest{
		ServiceID:          snapshot.Aggregate.ServicePolicies[0].Service.ID,
		StaffID:            snapshot.Aggregate.StaffProfiles[0].Staff.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific,
		PreferredDate:      "2026-02-10",
		Limit:              50,
	}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planStaffOnlyAvailability(snapshot, request, now)
	if err != nil {
		t.Fatalf("plan availability: %v", err)
	}
	if len(slots) == 0 {
		t.Fatal("expected staff-only openings")
	}
	if got := slots[0].StartTime.Format(time.RFC3339); got != "2026-02-10T15:15:00Z" {
		t.Fatalf("first slot = %s, want buffer-fitted 09:15 local", got)
	}
	for _, slot := range slots {
		if rangesOverlap(slot.OccupiedStartTime, slot.OccupiedEndTime, staffUnavailableStart, staffUnavailableEnd) {
			t.Fatalf("slot overlaps staff exception: %#v", slot)
		}
		if rangesOverlap(slot.OccupiedStartTime, slot.OccupiedEndTime, reservationStart, reservationEnd) {
			t.Fatalf("slot overlaps active reservation: %#v", slot)
		}
	}
}

func TestPlanStaffOnlyAvailabilityRequiresBothSalonAndStaffAvailability(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	location, _ := time.LoadLocation(snapshot.Aggregate.Timezone)
	startsAt, _ := strictLocalInstant(2026, time.February, 10, 17, 0, location)
	endsAt, _ := strictLocalInstant(2026, time.February, 10, 19, 0, location)
	snapshot.Aggregate.Exceptions = []CalendarException{{
		ScopeType: ExceptionScopeSalon, Effect: ExceptionEffectAvailable, StartsAt: startsAt, EndsAt: endsAt,
	}}
	request := normalizedAvailabilityRequest{
		ServiceID:          snapshot.Aggregate.ServicePolicies[0].Service.ID,
		StaffID:            snapshot.Aggregate.StaffProfiles[0].Staff.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific,
		PreferredDate:      "2026-02-10", Limit: 50,
	}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planStaffOnlyAvailability(snapshot, request, now)
	if err != nil {
		t.Fatalf("plan with salon-only extension: %v", err)
	}
	assertNoSlotStartingAtOrAfter(t, slots, startsAt)

	snapshot.Aggregate.Exceptions = append(snapshot.Aggregate.Exceptions, CalendarException{
		ScopeType: ExceptionScopeStaff, StaffID: snapshot.Aggregate.StaffProfiles[0].Staff.ID,
		Effect: ExceptionEffectAvailable, StartsAt: startsAt, EndsAt: endsAt,
	})
	slots, err = planStaffOnlyAvailability(snapshot, request, now)
	if err != nil {
		t.Fatalf("plan with salon+staff extension: %v", err)
	}
	wantStart, _ := strictLocalInstant(2026, time.February, 10, 17, 15, location)
	if !containsSlotStart(slots, wantStart) {
		t.Fatalf("salon+staff exception did not expose buffer-fitted 17:15 slot: %#v", slots)
	}
}

func TestPhase4AOperationCapabilitiesRemainScopeHonest(t *testing.T) {
	aggregate := readyAggregateFixture()
	aggregate.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	aggregate.Config.SlotStepMinutes = 15
	aggregate.Config.MinimumBookingNoticeMinutes = 0
	aggregate.Config.BookingHorizonDays = 30
	readiness := EvaluateReadiness(aggregate)
	if !readiness.Capabilities.StaffOnlyAvailability || !readiness.Capabilities.StaffOnlyCreate {
		t.Fatalf("staff-only capabilities = %#v, want available/create", readiness.Capabilities)
	}
	if readiness.Capabilities.PooledCapacity || readiness.Capabilities.PartyCreate ||
		!readiness.Capabilities.Reschedule || !readiness.Capabilities.Cancel {
		t.Fatalf("Phase 4C lifecycle capabilities = %#v", readiness.Capabilities)
	}
	if readiness.ExecutionReady {
		t.Fatal("aggregate execution_ready must remain false until Phase 4A-C are complete")
	}
	if !DefaultConstraints().ExecutionEngineAvailable {
		t.Fatal("legacy execution engine signal must report the available staff-only slice")
	}
}

func TestPhase4BOperationCapabilitiesExposeOnlyReadyAggregateSlices(t *testing.T) {
	aggregate := readyAggregateFixture()
	aggregate.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	aggregate.Config.SlotStepMinutes = 15
	aggregate.Config.MinimumBookingNoticeMinutes = 0
	aggregate.Config.BookingHorizonDays = 30
	aggregate.Config.MaxPartySize = 4
	mode := CapacityModePooled
	aggregate.ServicePolicies[0].CapacityMode = &mode
	aggregate.ServicePolicies[0].ResourceRequirements = []ResourceRequirement{{
		ResourcePoolID: "00000000-0000-4000-8000-000000000081", ResourceName: "Chairs", UnitsRequired: 1, PoolCapacity: 2,
	}}
	aggregate.Resources = []ResourcePool{{ID: "00000000-0000-4000-8000-000000000081", Name: "Chairs", Capacity: 2}}
	readiness := EvaluateReadiness(aggregate)
	if !readiness.Capabilities.PooledCapacity || !readiness.Capabilities.PartyCreate {
		t.Fatalf("aggregate capabilities = %#v, want pooled+party", readiness.Capabilities)
	}
	if !readiness.Capabilities.Reschedule || !readiness.Capabilities.Cancel || readiness.ExecutionReady {
		t.Fatalf("pooled-only Phase 4A-C capability suite = %#v ready=%t", readiness.Capabilities, readiness.ExecutionReady)
	}
}

func TestPhase4CExecutionReadyRequiresEveryDeclaredCapability(t *testing.T) {
	aggregate := readyAggregateFixture()
	aggregate.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	aggregate.Config.MaxPartySize = 4
	pooled := aggregate.ServicePolicies[0]
	pooled.Service.ID = "00000000-0000-4000-8000-000000000091"
	mode := CapacityModePooled
	pooled.CapacityMode = &mode
	pooled.ResourceRequirements = []ResourceRequirement{{
		ResourcePoolID: "00000000-0000-4000-8000-000000000081", ResourceName: "Chairs", UnitsRequired: 1, PoolCapacity: 2,
	}}
	aggregate.ServicePolicies = append(aggregate.ServicePolicies, pooled)
	aggregate.Resources = []ResourcePool{{ID: "00000000-0000-4000-8000-000000000081", Name: "Chairs", Capacity: 2}}

	readiness := EvaluateReadiness(aggregate)
	if !readiness.ExecutionReady || !readiness.Capabilities.StaffOnlyAvailability ||
		!readiness.Capabilities.StaffOnlyCreate || !readiness.Capabilities.PooledCapacity ||
		!readiness.Capabilities.PartyCreate || !readiness.Capabilities.Reschedule ||
		!readiness.Capabilities.Cancel {
		t.Fatalf("complete readiness = %#v", readiness)
	}
}

func TestCreateTimePlannerScansBeyondPublicSlotLimit(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	snapshot.Aggregate.Config.SlotStepMinutes = 1
	snapshot.Aggregate.Config.DefaultBufferBeforeMinutes = 0
	snapshot.Aggregate.Config.DefaultBufferAfterMinutes = 0
	request := normalizedAvailabilityRequest{
		ServiceID:          snapshot.Aggregate.ServicePolicies[0].Service.ID,
		StaffID:            snapshot.Aggregate.StaffProfiles[0].Staff.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific,
		PreferredDate:      "2026-02-10", Limit: -1,
	}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planStaffOnlyAvailability(snapshot, request, now)
	if err != nil {
		t.Fatalf("plan complete day: %v", err)
	}
	if len(slots) <= 50 {
		t.Fatalf("complete create-time scan returned %d slots, want beyond public limit", len(slots))
	}
	location, err := time.LoadLocation(snapshot.Aggregate.Timezone)
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	wantLate, _ := strictLocalInstant(2026, time.February, 10, 15, 30, location)
	if !containsSlotStart(slots, wantLate) {
		t.Fatalf("complete scan omitted late quoted slot %s", wantLate)
	}
}

func TestPlanAggregateAvailabilityExpandsPartyUnitsAndAssignsAnyoneDeterministically(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	first := snapshot.Aggregate.StaffProfiles[0]
	second := first
	second.Staff.ID = "00000000-0000-4000-8000-000000000002"
	second.Staff.Name = "A name that must not control assignment"
	second.WeeklyPeriods[0].ID = "00000000-0000-4000-8000-000000000012"
	second.WeeklyPeriods[0].StaffID = second.Staff.ID
	snapshot.Aggregate.StaffProfiles = append(snapshot.Aggregate.StaffProfiles, second)
	snapshot.Aggregate.ServicePolicies[0].EligibleStaff = append(snapshot.Aggregate.ServicePolicies[0].EligibleStaff, second.Staff)
	// Force the original staff ID after the second ID. Assignment must be ID/state
	// deterministic and must never depend on staff display names.
	snapshot.Aggregate.StaffProfiles[0].Staff.ID = "00000000-0000-4000-8000-000000000003"
	snapshot.Aggregate.StaffProfiles[0].WeeklyPeriods[0].StaffID = snapshot.Aggregate.StaffProfiles[0].Staff.ID
	snapshot.Aggregate.ServicePolicies[0].EligibleStaff[0] = snapshot.Aggregate.StaffProfiles[0].Staff

	req := normalizedAvailabilityRequest{
		PartySize: 2, PreferredDate: "2026-02-10", Limit: 1,
		Segments: []normalizedAvailabilitySegment{
			{ServiceID: snapshot.Aggregate.ServicePolicies[0].Service.ID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-a", Quantity: 1},
			{ServiceID: snapshot.Aggregate.ServicePolicies[0].Service.ID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-b", Quantity: 1},
		},
	}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planAggregateAvailability(snapshot, req, now)
	if err != nil {
		t.Fatalf("plan aggregate: %v", err)
	}
	if len(slots) != 1 || len(slots[0].Segments) != 2 {
		t.Fatalf("slots = %#v, want one aggregate with two concrete units", slots)
	}
	left, right := slots[0].Segments[0], slots[0].Segments[1]
	if left.GuestReference != "guest-a" || right.GuestReference != "guest-b" || left.Quantity != 1 || right.Quantity != 1 {
		t.Fatalf("guest/unit evidence = %#v", slots[0].Segments)
	}
	if left.StaffID != "00000000-0000-4000-8000-000000000002" || right.StaffID != "00000000-0000-4000-8000-000000000003" {
		t.Fatalf("deterministic assignment = %s/%s", left.StaffID, right.StaffID)
	}
	if !left.StartTime.Equal(right.StartTime) || slots[0].StartTime != left.StartTime || slots[0].EndTime != left.EndTime {
		t.Fatalf("parallel aggregate bounds = %#v", slots[0])
	}
}

func TestNormalizeAggregateAvailabilityExpandsQuantityIntoOrderedConcreteUnits(t *testing.T) {
	serviceID := "00000000-0000-4000-8000-000000000091"
	normalized, err := normalizeAggregateAvailabilityRequest(booking.AvailabilityRequest{
		PartySize: 2, PreferredDate: "2026-02-10", Limit: 5,
		Segments: []booking.BookingSegmentRequest{{
			ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone,
			GuestReference: "party-unit", Quantity: 2,
		}},
	})
	if err != nil {
		t.Fatalf("normalize aggregate: %v", err)
	}
	if len(normalized.Segments) != 2 || normalized.Segments[0].Quantity != 1 || normalized.Segments[1].Quantity != 1 ||
		normalized.Segments[0].GuestReference != "party-unit-1" || normalized.Segments[1].GuestReference != "party-unit-2" {
		t.Fatalf("expanded units = %#v", normalized.Segments)
	}
	if _, err := normalizeAggregateAvailabilityRequest(booking.AvailabilityRequest{
		PartySize: 3, PreferredDate: "2026-02-10", Limit: 5,
		Segments: []booking.BookingSegmentRequest{{ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "party-unit", Quantity: 2}},
	}); !errors.Is(err, booking.ErrValidation) {
		t.Fatalf("party mismatch error = %v", err)
	}
	for _, test := range []struct {
		name      string
		partySize int
		guests    []string
	}{
		{name: "mixed references for one guest", partySize: 1, guests: []string{"guest-a", ""}},
		{name: "duplicate reference for two guests", partySize: 2, guests: []string{"guest-a", "guest-a"}},
		{name: "anonymous multi guest", partySize: 2, guests: []string{"", ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeAggregateAvailabilityRequest(booking.AvailabilityRequest{
				PartySize: test.partySize, PreferredDate: "2026-02-10", Limit: 5,
				Segments: []booking.BookingSegmentRequest{
					{ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: test.guests[0], Quantity: 1},
					{ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: test.guests[1], Quantity: 1},
				},
			})
			if !errors.Is(err, booking.ErrValidation) {
				t.Fatalf("normalize error = %v", err)
			}
		})
	}
}

func TestPlanAggregateAvailabilityPreservesSequentialServicesPerGuest(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	secondPolicy := snapshot.Aggregate.ServicePolicies[0]
	secondPolicy.Service.ID = "00000000-0000-4000-8000-000000000052"
	secondPolicy.Service.Name = "Different catalog service"
	secondPolicy.Service.DurationMinutes = 30
	snapshot.Aggregate.ServicePolicies = append(snapshot.Aggregate.ServicePolicies, secondPolicy)
	snapshot.Aggregate.StaffProfiles[0].EligibleServices = append(snapshot.Aggregate.StaffProfiles[0].EligibleServices, secondPolicy.Service)
	snapshot.Aggregate.Config.DefaultBufferBeforeMinutes = 0
	snapshot.Aggregate.Config.DefaultBufferAfterMinutes = 0
	req := normalizedAvailabilityRequest{PartySize: 1, PreferredDate: "2026-02-10", Limit: 1, Segments: []normalizedAvailabilitySegment{
		{ServiceID: snapshot.Aggregate.ServicePolicies[0].Service.ID, StaffID: snapshot.Aggregate.StaffProfiles[0].Staff.ID, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-a", Quantity: 1},
		{ServiceID: secondPolicy.Service.ID, StaffID: snapshot.Aggregate.StaffProfiles[0].Staff.ID, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-a", Quantity: 1},
	}}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planAggregateAvailability(snapshot, req, now)
	if err != nil || len(slots) != 1 {
		t.Fatalf("plan sequential = %#v/%v", slots, err)
	}
	if !slots[0].Segments[1].StartTime.Equal(slots[0].Segments[0].EndTime) || !slots[0].EndTime.Equal(slots[0].Segments[1].EndTime) {
		t.Fatalf("sequential timing = %#v", slots[0])
	}
}

func TestPlanAggregateAvailabilityBacktracksAnyoneForLaterSpecificSegment(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	snapshot.Aggregate.Config.DefaultBufferBeforeMinutes = 0
	snapshot.Aggregate.Config.DefaultBufferAfterMinutes = 0
	firstProfile := snapshot.Aggregate.StaffProfiles[0]
	firstProfile.Staff.ID = "00000000-0000-4000-8000-000000000102"
	firstProfile.WeeklyPeriods[0].StaffID = firstProfile.Staff.ID
	secondProfile := firstProfile
	secondProfile.Staff.ID = "00000000-0000-4000-8000-000000000103"
	secondProfile.WeeklyPeriods[0].StaffID = secondProfile.Staff.ID
	snapshot.Aggregate.StaffProfiles = []StaffProfile{firstProfile, secondProfile}
	snapshot.Aggregate.ServicePolicies[0].EligibleStaff = []StaffRef{firstProfile.Staff, secondProfile.Staff}
	req := normalizedAvailabilityRequest{PartySize: 2, PreferredDate: "2026-02-10", Limit: 1, Segments: []normalizedAvailabilitySegment{
		{ServiceID: snapshot.Aggregate.ServicePolicies[0].Service.ID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-a", Quantity: 1},
		{ServiceID: snapshot.Aggregate.ServicePolicies[0].Service.ID, StaffID: firstProfile.Staff.ID, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "guest-b", Quantity: 1},
	}}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planAggregateAvailability(snapshot, req, now)
	if err != nil || len(slots) != 1 {
		t.Fatalf("backtracking plan = %#v/%v", slots, err)
	}
	if slots[0].Segments[0].StaffID != secondProfile.Staff.ID || slots[0].Segments[1].StaffID != firstProfile.Staff.ID {
		t.Fatalf("backtracking assignment = %#v", slots[0].Segments)
	}
}

func TestPlanAggregatePooledCapacityRechecksOverrideBoundaries(t *testing.T) {
	snapshot := staffOnlyAvailabilityFixture(t)
	mode := CapacityModePooled
	poolID := "00000000-0000-4000-8000-000000000061"
	policy := &snapshot.Aggregate.ServicePolicies[0]
	policy.CapacityMode = &mode
	policy.ResourceRequirements = []ResourceRequirement{{ResourcePoolID: poolID, ResourceName: "Pedicure chairs", UnitsRequired: 1, PoolCapacity: 2}}
	snapshot.Aggregate.Resources = []ResourcePool{{ID: poolID, Name: "Pedicure chairs", Capacity: 2}}
	snapshot.Aggregate.Config.DefaultBufferBeforeMinutes = 0
	snapshot.Aggregate.Config.DefaultBufferAfterMinutes = 0
	loc, _ := time.LoadLocation(snapshot.Aggregate.Timezone)
	overrideStart, _ := strictLocalInstant(2026, time.February, 10, 9, 30, loc)
	overrideEnd, _ := strictLocalInstant(2026, time.February, 10, 10, 0, loc)
	zero := 0
	snapshot.Aggregate.Exceptions = []CalendarException{{ScopeType: ExceptionScopeResource, ResourcePoolID: poolID, Effect: ExceptionEffectCapacityOverride, CapacityOverride: &zero, StartsAt: overrideStart, EndsAt: overrideEnd}}
	req := normalizedAvailabilityRequest{PartySize: 1, PreferredDate: "2026-02-10", Limit: 50, Segments: []normalizedAvailabilitySegment{{
		ServiceID: policy.Service.ID, StaffID: snapshot.Aggregate.StaffProfiles[0].Staff.ID, StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
	}}}
	now, _ := time.Parse(time.RFC3339, "2026-02-09T12:00:00Z")
	slots, err := planAggregateAvailability(snapshot, req, now)
	if err != nil {
		t.Fatalf("plan pooled: %v", err)
	}
	for _, slot := range slots {
		for _, segment := range slot.Segments {
			if rangesOverlap(segment.OccupiedStartTime, segment.OccupiedEndTime, overrideStart, overrideEnd) {
				t.Fatalf("slot crossed zero-capacity boundary: %#v", segment)
			}
			if len(segment.ResourceAllocations) != 1 || segment.ResourceAllocations[0].ResourcePoolID != poolID || segment.ResourceAllocations[0].UnitsAllocated != 1 {
				t.Fatalf("resource evidence = %#v", segment.ResourceAllocations)
			}
		}
	}
}

func staffOnlyAvailabilityFixture(t *testing.T) AvailabilitySnapshot {
	t.Helper()
	aggregate := readyAggregateFixture()
	aggregate.Timezone = "America/Chicago"
	aggregate.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	aggregate.Config.SlotStepMinutes = 15
	aggregate.Config.MinimumBookingNoticeMinutes = 0
	aggregate.Config.BookingHorizonDays = 30
	aggregate.Config.DefaultBufferBeforeMinutes = 15
	aggregate.Config.DefaultBufferAfterMinutes = 15
	aggregate.Hours = []BusinessHourPeriod{{DayOfWeek: int(time.Tuesday), StartMinute: 9 * 60, EndMinute: 17 * 60}}
	aggregate.StaffProfiles[0].WeeklyPeriods = []WeeklyPeriod{{
		StaffID: aggregate.StaffProfiles[0].Staff.ID, DayOfWeek: int(time.Tuesday), StartMinute: 9 * 60, EndMinute: 17 * 60,
	}}
	return AvailabilitySnapshot{Aggregate: aggregate}
}

func assertNoSlotStartingAtOrAfter(t *testing.T, slots []InternalAvailabilitySlot, threshold time.Time) {
	t.Helper()
	for _, slot := range slots {
		if !slot.StartTime.Before(threshold) {
			t.Fatalf("unexpected slot at/after %s: %#v", threshold, slot)
		}
	}
}

func containsSlotStart(slots []InternalAvailabilitySlot, target time.Time) bool {
	for _, slot := range slots {
		if slot.StartTime.Equal(target) {
			return true
		}
	}
	return false
}
