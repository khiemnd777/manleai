package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type internalPartyGoldenFixture struct {
	name          string
	callerMessage string
	services      []ServiceOption
	staff         []StaffOption
	plan          *PartyPlan
	option        PartySplitOption
	wantSegments  []scheduling.ActionSegment
	wantRootStart time.Time
	wantRootEnd   time.Time
	wantReply     string
}

func TestManleAICalendarPartyGoldenActionPreservesReviewedAssignments(t *testing.T) {
	quoteID := "quote-aggregate-party"
	fingerprint := strings.Repeat("9", 64)
	parallelStart := time.Date(2026, 8, 12, 19, 0, 0, 0, time.UTC)
	sequentialStart := time.Date(2026, 8, 15, 19, 0, 0, 0, time.UTC)
	staggeredStart := time.Date(2026, 9, 20, 18, 30, 0, 0, time.UTC)

	fixtures := []internalPartyGoldenFixture{
		{
			name:          "parallel guests keep distinct staff assignments",
			callerMessage: "Yes, book the two manicure openings together for us.",
			services: []ServiceOption{
				{ID: "classic_mani", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true},
			},
			staff: []StaffOption{
				{ID: "staff_mina", Name: "Mina", AIBookable: true},
				{ID: "staff_lan", Name: "Lan", AIBookable: true},
			},
			plan: &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
				{Label: "manicure", Count: 2, ResolvedServiceIDs: []string{"classic_mani", "classic_mani"}},
			}},
			option: PartySplitOption{ID: "parallel-two-guests", Blocks: []PartySplitBlock{{
				StartTime: parallelStart,
				EndTime:   parallelStart.Add(45 * time.Minute),
				Segments: []booking.BookingSegmentRequest{
					{ServiceID: "classic_mani", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific},
					{ServiceID: "classic_mani", StaffID: "staff_lan", StaffSelectionMode: booking.StaffSelectionSpecific},
				},
				QuoteRefs: []PartySplitQuoteRef{
					{ServiceID: "classic_mani", GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: parallelStart, RequestedEndTime: parallelStart.Add(45 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
					{ServiceID: "classic_mani", GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: parallelStart, RequestedEndTime: parallelStart.Add(45 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
				},
			}}},
			wantSegments: []scheduling.ActionSegment{
				{ServiceID: "classic_mani", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: parallelStart, RequestedEndTime: parallelStart.Add(45 * time.Minute)},
				{ServiceID: "classic_mani", StaffID: "staff_lan", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: parallelStart, RequestedEndTime: parallelStart.Add(45 * time.Minute)},
			},
			wantRootStart: parallelStart,
			wantRootEnd:   parallelStart.Add(45 * time.Minute),
			wantReply:     "You're confirmed with Lotus Nails for one group appointment: Classic Manicure with Mina on Wednesday, August 12 at 2:00 PM and Classic Manicure with Lan on Wednesday, August 12 at 2:00 PM. The group appointment is under Jade Nguyen. Thank you, goodbye.",
		},
		{
			name:          "sequential services for one guest share the guest reference",
			callerMessage: "Please reserve my manicure then gel add-on, while my sister gets the pedicure.",
			services: []ServiceOption{
				{ID: "classic_mani", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true},
				{ID: "gel_addon", Name: "Gel Add-on", DurationMinutes: 30, BookingReady: true},
				{ID: "spa_pedi", Name: "Spa Pedicure", DurationMinutes: 60, BookingReady: true},
			},
			staff: []StaffOption{
				{ID: "staff_mina", Name: "Mina", AIBookable: true},
				{ID: "staff_lan", Name: "Lan", AIBookable: true},
			},
			plan: &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
				{Label: "caller", Count: 1, ResolvedServiceIDs: []string{"classic_mani", "gel_addon"}},
				{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{"spa_pedi"}},
			}},
			option: PartySplitOption{ID: "sequential-caller", Blocks: []PartySplitBlock{
				{
					StartTime: sequentialStart,
					EndTime:   sequentialStart.Add(60 * time.Minute),
					Segments: []booking.BookingSegmentRequest{
						{ServiceID: "classic_mani", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific},
						{ServiceID: "spa_pedi", StaffID: "staff_lan", StaffSelectionMode: booking.StaffSelectionSpecific},
					},
					QuoteRefs: []PartySplitQuoteRef{
						{ServiceID: "classic_mani", GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: sequentialStart, RequestedEndTime: sequentialStart.Add(45 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
						{ServiceID: "spa_pedi", GuestReference: "group-2-guest-1", Quantity: 1, RequestedStartTime: sequentialStart, RequestedEndTime: sequentialStart.Add(60 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
					},
				},
				{
					StartTime: sequentialStart.Add(45 * time.Minute),
					EndTime:   sequentialStart.Add(75 * time.Minute),
					Segments: []booking.BookingSegmentRequest{
						{ServiceID: "gel_addon", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific},
					},
					QuoteRefs: []PartySplitQuoteRef{
						{ServiceID: "gel_addon", GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: sequentialStart.Add(45 * time.Minute), RequestedEndTime: sequentialStart.Add(75 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
					},
				},
			}},
			wantSegments: []scheduling.ActionSegment{
				{ServiceID: "classic_mani", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: sequentialStart, RequestedEndTime: sequentialStart.Add(45 * time.Minute)},
				{ServiceID: "spa_pedi", StaffID: "staff_lan", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-2-guest-1", Quantity: 1, RequestedStartTime: sequentialStart, RequestedEndTime: sequentialStart.Add(60 * time.Minute)},
				{ServiceID: "gel_addon", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: sequentialStart.Add(45 * time.Minute), RequestedEndTime: sequentialStart.Add(75 * time.Minute)},
			},
			wantRootStart: sequentialStart,
			wantRootEnd:   sequentialStart.Add(75 * time.Minute),
			wantReply:     "You're confirmed with Lotus Nails for one group appointment: Classic Manicure with Mina on Saturday, August 15 at 2:00 PM, Spa Pedicure with Lan on Saturday, August 15 at 2:00 PM, and Gel Add-on with Mina on Saturday, August 15 at 2:45 PM. The group appointment is under Jade Nguyen. Thank you, goodbye.",
		},
		{
			name:          "staggered assignments keep reviewed starts and technicians",
			callerMessage: "The staggered builder gel and dry pedicure times are fine; reserve the reviewed team.",
			services: []ServiceOption{
				{ID: "builder_gel", Name: "Builder Gel Refill", DurationMinutes: 50, BookingReady: true},
				{ID: "dry_pedi", Name: "Dry Pedicure", DurationMinutes: 65, BookingReady: true},
			},
			staff: []StaffOption{
				{ID: "staff_nhi", Name: "Nhi", AIBookable: true},
				{ID: "staff_uyen", Name: "Uyen", AIBookable: true},
			},
			plan: &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
				{Label: "caller", Count: 1, ResolvedServiceIDs: []string{"builder_gel"}},
				{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{"dry_pedi"}},
			}},
			option: PartySplitOption{ID: "staggered-two-guests", Blocks: []PartySplitBlock{
				{StartTime: staggeredStart, EndTime: staggeredStart.Add(50 * time.Minute), Segments: []booking.BookingSegmentRequest{{ServiceID: "builder_gel", StaffID: "staff_nhi", StaffSelectionMode: booking.StaffSelectionSpecific}}, QuoteRefs: []PartySplitQuoteRef{{ServiceID: "builder_gel", GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: staggeredStart, RequestedEndTime: staggeredStart.Add(50 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint}}},
				{StartTime: staggeredStart.Add(30 * time.Minute), EndTime: staggeredStart.Add(95 * time.Minute), Segments: []booking.BookingSegmentRequest{{ServiceID: "dry_pedi", StaffID: "staff_uyen", StaffSelectionMode: booking.StaffSelectionSpecific}}, QuoteRefs: []PartySplitQuoteRef{{ServiceID: "dry_pedi", GuestReference: "group-2-guest-1", Quantity: 1, RequestedStartTime: staggeredStart.Add(30 * time.Minute), RequestedEndTime: staggeredStart.Add(95 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint}}},
			}},
			wantSegments: []scheduling.ActionSegment{
				{ServiceID: "builder_gel", StaffID: "staff_nhi", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: staggeredStart, RequestedEndTime: staggeredStart.Add(50 * time.Minute)},
				{ServiceID: "dry_pedi", StaffID: "staff_uyen", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-2-guest-1", Quantity: 1, RequestedStartTime: staggeredStart.Add(30 * time.Minute), RequestedEndTime: staggeredStart.Add(95 * time.Minute)},
			},
			wantRootStart: staggeredStart,
			wantRootEnd:   staggeredStart.Add(95 * time.Minute),
			wantReply:     "You're confirmed with Lotus Nails for one group appointment: Builder Gel Refill with Nhi on Sunday, September 20 at 1:30 PM and Dry Pedicure with Uyen on Sunday, September 20 at 2:00 PM. The group appointment is under Jade Nguyen. Thank you, goodbye.",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = fixture.services
			store.staff = fixture.staff
			tool := newQueuedManleAICalendarSchedulingTool()
			tool.actionResults = []*scheduling.ActionResult{internalConfirmedPartyAction("appointment-party-root", fixture.wantSegments)}
			service := NewService(store, tool)
			session := reviewedInternalPartySession(store, fixture.plan, fixture.option, quoteID, fingerprint)
			turn := newTurnRecord(session.SalonID, "owner_1", session, session, fixture.callerMessage, "party-golden", store.services, store.staff, &store.cfg)

			got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
			if err != nil {
				t.Fatalf("tryBooking returned error: %v", err)
			}
			if got.Outcome != OutcomeBookingConfirmed || got.AppointmentID != "appointment-party-root" || got.BookingAttemptID != "attempt-for-appointment-party-root" {
				t.Fatalf("root confirmation was not preserved: %#v", got)
			}
			if got.PartyPlan == nil || len(got.PartyPlan.SplitAppointmentIDs) != 0 || len(got.PartyPlan.SplitBookingAttemptIDs) != 0 {
				t.Fatalf("internal aggregate result recorded child confirmations: %#v", got.PartyPlan)
			}
			if tool.actionCalls != 1 || len(tool.actionRequests) != 1 || tool.fakeBookingTool.calls != 0 {
				t.Fatalf("party operation was not one neutral action: actions=%d legacy=%d", tool.actionCalls, tool.fakeBookingTool.calls)
			}
			req := tool.actionRequests[0]
			if req.AvailabilityQuoteID != quoteID || req.SlotFingerprint != fingerprint || req.PartySize != fixture.plan.PartySize || !req.RequestedStartTime.Equal(fixture.wantRootStart) || !req.RequestedEndTime.Equal(fixture.wantRootEnd) {
				t.Fatalf("aggregate request root mismatch: %#v", req)
			}
			assertSchedulingSegmentsEqual(t, req.Segments, fixture.wantSegments)
			if store.lastTurn.AIMessage != fixture.wantReply {
				t.Fatalf("golden reply = %q, want %q", store.lastTurn.AIMessage, fixture.wantReply)
			}
		})
	}
}

func TestManleAICalendarPartyRejectsPartialConfirmedChildren(t *testing.T) {
	start := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	serviceOption := ServiceOption{ID: "dip_mani", Name: "Dip Manicure", DurationMinutes: 50, BookingReady: true}
	staff := []StaffOption{{ID: "staff_ly", Name: "Ly", AIBookable: true}, {ID: "staff_van", Name: "Van", AIBookable: true}}
	plan := &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{{Label: "dip manicure", Count: 2, ResolvedServiceIDs: []string{serviceOption.ID, serviceOption.ID}}}}
	fingerprint := strings.Repeat("a", 64)
	option := PartySplitOption{ID: "partial-result", Blocks: []PartySplitBlock{{
		StartTime: start,
		EndTime:   start.Add(50 * time.Minute),
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: serviceOption.ID, StaffID: staff[0].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
			{ServiceID: serviceOption.ID, StaffID: staff[1].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
		},
		QuoteRefs: []PartySplitQuoteRef{
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(50 * time.Minute), AvailabilityQuoteID: "quote-partial-result", SlotFingerprint: fingerprint},
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(50 * time.Minute), AvailabilityQuoteID: "quote-partial-result", SlotFingerprint: fingerprint},
		},
	}}}
	store := newFakeConversationStore()
	store.services = []ServiceOption{serviceOption}
	store.staff = staff
	session := reviewedInternalPartySession(store, plan, option, "quote-partial-result", fingerprint)
	action, ok := reviewedInternalPartyAction(session, store.services)
	if !ok {
		t.Fatal("test fixture did not produce a reviewed internal party action")
	}
	partialResult := internalConfirmedPartyAction("appointment-partial-root", action.Segments)
	partialResult.ConfirmedAppointment.Children = partialResult.ConfirmedAppointment.Children[:1]
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.actionResults = []*scheduling.ActionResult{partialResult}
	service := NewService(store, tool)
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Please reserve both dip manicure assignments.", "party-partial-result", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if tool.actionCalls != 1 || got.Outcome != OutcomeHandoffRequested || got.AppointmentID != "" || got.BookingAttemptID != "" || got.Handoff == nil {
		t.Fatalf("partial confirmed children were exposed as a durable group confirmation: result=%#v actions=%d", got, tool.actionCalls)
	}
	lower := strings.ToLower(store.lastTurn.AIMessage)
	for _, forbidden := range []string{"you're confirmed", "has been booked", "one group appointment:"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("partial confirmed children produced confirmation wording %q: %q", forbidden, store.lastTurn.AIMessage)
		}
	}
}

func TestManleAICalendarPartyAvailabilityUsesOneAggregateRequestAndPreservesAllocation(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true},
		{ID: "gel_addon", Name: "Gel Add-on", DurationMinutes: 30, BookingReady: true},
		{ID: "spa_pedi", Name: "Spa Pedicure", DurationMinutes: 60, BookingReady: true},
	}
	store.staff = []StaffOption{{ID: "staff_mina", Name: "Mina", AIBookable: true}, {ID: "staff_lan", Name: "Lan", AIBookable: true}}
	start := time.Date(2026, 8, 15, 19, 0, 0, 0, time.UTC)
	quoteID := "quote-aggregate-availability"
	fingerprint := strings.Repeat("8", 64)
	plan := &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{
		{Label: "caller", Count: 1, ResolvedServiceIDs: []string{"classic_mani", "gel_addon"}},
		{Label: "guest 2", Count: 1, ResolvedServiceIDs: []string{"spa_pedi"}},
	}}
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.availabilityResults = []*scheduling.AvailabilityResult{{
		Kind:                scheduling.AvailabilityKindVerifiedSlots,
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		VerifiedSlots: &booking.AvailabilityResult{
			QuoteID:            quoteID,
			RequestFingerprint: strings.Repeat("3", 64),
			PreferredDate:      "2026-08-15",
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				Fingerprint: fingerprint,
				StartTime:   start,
				EndTime:     start.Add(75 * time.Minute),
				Segments: []booking.AvailabilitySegment{
					{ServiceID: "classic_mani", ServiceName: "Classic Manicure", StaffID: "staff_mina", StaffName: "Mina", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, DurationMinutes: 45, ScheduledStartTime: start, ScheduledEndTime: start.Add(45 * time.Minute), OccupiedStartTime: start.Add(-5 * time.Minute), OccupiedEndTime: start.Add(50 * time.Minute), BufferBeforeMinutes: 5, BufferAfterMinutes: 5},
					{ServiceID: "spa_pedi", ServiceName: "Spa Pedicure", StaffID: "staff_lan", StaffName: "Lan", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-2-guest-1", Quantity: 1, DurationMinutes: 60, ScheduledStartTime: start, ScheduledEndTime: start.Add(60 * time.Minute), OccupiedStartTime: start, OccupiedEndTime: start.Add(60 * time.Minute), ResourceAllocations: []booking.AvailabilityResourceAllocation{{ResourcePoolID: "pool-chair", ResourceName: "Pedicure chair", UnitsAllocated: 1}}},
					{ServiceID: "gel_addon", ServiceName: "Gel Add-on", StaffID: "staff_mina", StaffName: "Mina", StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, DurationMinutes: 30, ScheduledStartTime: start.Add(45 * time.Minute), ScheduledEndTime: start.Add(75 * time.Minute), OccupiedStartTime: start.Add(45 * time.Minute), OccupiedEndTime: start.Add(75 * time.Minute)},
				},
			}},
		},
	}}
	service := NewService(store, tool)
	session := store.session
	session.Intent = IntentBooking
	session.ServiceID = "classic_mani"
	session.ServiceName = "Classic Manicure"
	session.StaffSelectionMode = booking.StaffSelectionAnyone
	session.RequestedDate = "2026-08-15"
	session.PartyPlan = plan
	session.BookingSegments = partyPlanSegments(plan, session)
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Saturday afternoon works for both of us.", "aggregate-availability", store.services, store.staff, &store.cfg)

	if err := service.offerAvailableSlots(context.Background(), "owner_1", &turn, &session, store.services, store.staff, session.RequestedDate, false, &store.cfg); err != nil {
		t.Fatalf("offerAvailableSlots returned error: %v", err)
	}
	if tool.availabilityCalls != 1 || len(tool.availabilityRequests) != 1 {
		t.Fatalf("internal party availability fanned out: calls=%d requests=%#v", tool.availabilityCalls, tool.availabilityRequests)
	}
	req := tool.availabilityRequests[0]
	if req.PartySize != 2 || len(req.Segments) != 3 {
		t.Fatalf("aggregate request lost party shape: %#v", req)
	}
	wantGuestRefs := []string{"group-1-guest-1", "group-1-guest-1", "group-2-guest-1"}
	for index, segment := range req.Segments {
		if segment.GuestReference != wantGuestRefs[index] || segment.Quantity != 1 {
			t.Fatalf("request segment %d lost structured guest/quantity: %#v", index, segment)
		}
	}
	if session.PartyPlan == nil || len(session.PartyPlan.SplitOptions) != 1 || session.PartyPlan.SelectedSplitOptionID != "" {
		t.Fatalf("aggregate allocation was not offered for review: %#v", session.PartyPlan)
	}
	option := session.PartyPlan.SplitOptions[0]
	if len(option.Blocks) != 2 || len(option.Blocks[0].Segments) != 2 || len(option.Blocks[1].Segments) != 1 {
		t.Fatalf("ordered aggregate allocation was flattened: %#v", option)
	}
	gotRefs := append(append([]PartySplitQuoteRef(nil), option.Blocks[0].QuoteRefs...), option.Blocks[1].QuoteRefs...)
	if len(gotRefs) != 3 {
		t.Fatalf("aggregate proof refs = %#v", gotRefs)
	}
	wantStarts := []time.Time{start, start, start.Add(45 * time.Minute)}
	wantEnds := []time.Time{start.Add(45 * time.Minute), start.Add(60 * time.Minute), start.Add(75 * time.Minute)}
	for index, ref := range gotRefs {
		if ref.AvailabilityQuoteID != quoteID || ref.SlotFingerprint != fingerprint || ref.GuestReference == "" || ref.Quantity != 1 || !ref.RequestedStartTime.Equal(wantStarts[index]) || !ref.RequestedEndTime.Equal(wantEnds[index]) {
			t.Fatalf("reviewed aggregate ref %d changed: %#v", index, ref)
		}
	}
	applySelectedPartySplitOption(&session, option, true)
	action, ok := reviewedInternalPartyAction(session, store.services)
	if !ok || action.AvailabilityQuoteID != quoteID || action.SlotFingerprint != fingerprint || len(action.Segments) != 3 {
		t.Fatalf("reviewed aggregate allocation did not become one action: %#v ok=%v", action, ok)
	}
	wantServices := []string{"classic_mani", "spa_pedi", "gel_addon"}
	for index, segment := range action.Segments {
		if segment.ServiceID != wantServices[index] || segment.StaffID == "" || segment.GuestReference == "" || segment.Quantity != 1 || !segment.RequestedStartTime.Equal(wantStarts[index]) || !segment.RequestedEndTime.Equal(wantEnds[index]) {
			t.Fatalf("aggregate action segment %d changed reviewed allocation: %#v", index, segment)
		}
	}
	malformed := *tool.availabilityResults[0].VerifiedSlots
	malformed.Slots = append([]booking.AvailabilitySlot(nil), malformed.Slots...)
	malformed.Slots[0].Segments = append([]booking.AvailabilitySegment(nil), malformed.Slots[0].Segments...)
	malformed.Slots[0].Segments[1].ResourceAllocations = []booking.AvailabilityResourceAllocation{{ResourcePoolID: "", UnitsAllocated: 1}}
	if options := partySplitOptionsFromAggregateAvailability(&malformed, session, session.RequestedDate, timezoneLocation(store.cfg.Timezone)); len(options) != 0 {
		t.Fatalf("malformed pooled allocation became a reviewable option: %#v", options)
	}
}

func TestExternalProviderPartyAvailabilityKeepsPerChildContract(t *testing.T) {
	store := newFakeConversationStore()
	store.services = []ServiceOption{{ID: "classic_mani", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true}}
	plan := &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{{Label: "manicure", Count: 2, ResolvedServiceIDs: []string{"classic_mani", "classic_mani"}}}}
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.authority = booking.SchedulingAuthorityExternalProvider
	tool.availabilityResults = []*scheduling.AvailabilityResult{{
		Kind:                scheduling.AvailabilityKindVerifiedSlots,
		SchedulingAuthority: booking.SchedulingAuthorityExternalProvider,
		VerifiedSlots:       &booking.AvailabilityResult{PreferredDate: "2026-09-09", Timezone: "America/Chicago", Slots: []booking.AvailabilitySlot{}},
	}}
	service := NewService(store, tool)
	session := store.session
	session.Intent = IntentBooking
	session.ServiceID = "classic_mani"
	session.ServiceName = "Classic Manicure"
	session.StaffSelectionMode = booking.StaffSelectionAnyone
	session.RequestedDate = "2026-09-09"
	session.PartyPlan = plan
	session.BookingSegments = partyPlanSegments(plan, session)
	for index := range session.BookingSegments {
		session.BookingSegments[index].GuestReference = "stale-internal-guest"
		session.BookingSegments[index].Quantity = 1
	}
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Check Wednesday for both manicures.", "external-party-availability", store.services, store.staff, &store.cfg)

	if err := service.offerAvailableSlots(context.Background(), "owner_1", &turn, &session, store.services, store.staff, session.RequestedDate, false, &store.cfg); err != nil {
		t.Fatalf("offerAvailableSlots returned error: %v", err)
	}
	if tool.availabilityCalls < 2 {
		t.Fatalf("external provider no longer used its existing per-child fallback: calls=%d", tool.availabilityCalls)
	}
	first := tool.availabilityRequests[0]
	if first.PartySize != 0 {
		t.Fatalf("external provider received internal party contract: %#v", first)
	}
	for _, segment := range first.Segments {
		if segment.GuestReference != "" || segment.Quantity != 0 {
			t.Fatalf("external provider segment was rewritten with internal fields: %#v", segment)
		}
	}
}

func TestManleAICalendarPartyResourceConflictReopensDraftWithZeroConfirmation(t *testing.T) {
	start := time.Date(2026, 10, 7, 19, 0, 0, 0, time.UTC)
	serviceOption := ServiceOption{ID: "classic_mani", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true}
	staff := []StaffOption{{ID: "staff_nhi", Name: "Nhi", AIBookable: true}, {ID: "staff_uyen", Name: "Uyen", AIBookable: true}}
	plan := &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{{Label: "manicure", Count: 2, ResolvedServiceIDs: []string{serviceOption.ID, serviceOption.ID}}}}
	quoteID := "quote-resource-conflict"
	fingerprint := strings.Repeat("7", 64)
	option := PartySplitOption{ID: "resource-conflict", Blocks: []PartySplitBlock{{
		StartTime: start,
		EndTime:   start.Add(45 * time.Minute),
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: serviceOption.ID, StaffID: staff[0].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
			{ServiceID: serviceOption.ID, StaffID: staff[1].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
		},
		QuoteRefs: []PartySplitQuoteRef{
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute), AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint},
		},
	}}}
	store := newFakeConversationStore()
	store.services = []ServiceOption{serviceOption}
	store.staff = staff
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.actionErrors = []error{booking.ErrAvailabilityQuoteStale}
	replacementStart := start.Add(90 * time.Minute)
	tool.availabilityResults = []*scheduling.AvailabilityResult{{
		Kind:                scheduling.AvailabilityKindVerifiedSlots,
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		VerifiedSlots: &booking.AvailabilityResult{
			QuoteID:       "quote-resource-replacement",
			PreferredDate: "2026-10-07",
			Timezone:      "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				Fingerprint: strings.Repeat("2", 64),
				StartTime:   replacementStart,
				EndTime:     replacementStart.Add(45 * time.Minute),
				Segments: []booking.AvailabilitySegment{
					{ServiceID: serviceOption.ID, ServiceName: serviceOption.Name, StaffID: staff[0].ID, StaffName: staff[0].Name, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-1", Quantity: 1, DurationMinutes: 45, ScheduledStartTime: replacementStart, ScheduledEndTime: replacementStart.Add(45 * time.Minute), OccupiedStartTime: replacementStart, OccupiedEndTime: replacementStart.Add(45 * time.Minute)},
					{ServiceID: serviceOption.ID, ServiceName: serviceOption.Name, StaffID: staff[1].ID, StaffName: staff[1].Name, StaffSelectionMode: booking.StaffSelectionSpecific, GuestReference: "group-1-guest-2", Quantity: 1, DurationMinutes: 45, ScheduledStartTime: replacementStart, ScheduledEndTime: replacementStart.Add(45 * time.Minute), OccupiedStartTime: replacementStart, OccupiedEndTime: replacementStart.Add(45 * time.Minute)},
				},
			}},
		},
	}}
	service := NewService(store, tool)
	service.SetReplyGenerator(&fakeReplyGenerator{message: "Everything is confirmed."})
	session := reviewedInternalPartySession(store, plan, option, quoteID, fingerprint)
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Go ahead with those two assignments.", "party-resource-conflict", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if got.Status != StatusActive || got.Outcome != OutcomeCollecting || got.AppointmentID != "" || got.BookingAttemptID != "" || got.Handoff != nil {
		t.Fatalf("resource conflict did not leave one open draft: %#v", got)
	}
	if got.PartyPlan == nil || got.PartyPlan.SelectedSplitOptionID != "" || len(got.PartyPlan.SplitOptions) != 1 || len(got.PartyPlan.SplitAppointmentIDs) != 0 || len(got.PartyPlan.SplitBookingAttemptIDs) != 0 {
		t.Fatalf("resource conflict preserved partial success or selected stale proof: %#v", got.PartyPlan)
	}
	if tool.actionCalls != 1 || tool.availabilityCalls != 1 || tool.fakeBookingTool.calls != 0 {
		t.Fatalf("resource conflict performed partial child writes: actions=%d legacy=%d", tool.actionCalls, tool.fakeBookingTool.calls)
	}
	lower := strings.ToLower(store.lastTurn.AIMessage)
	for _, forbidden := range []string{"everything is confirmed", "you're confirmed", "has been booked", "one of the appointments"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("resource conflict claimed partial or full success %q: %q", forbidden, store.lastTurn.AIMessage)
		}
	}
}

func TestManleAICalendarPartyCounterexamplesFailClosedBeforeAction(t *testing.T) {
	start := time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC)
	serviceOption := ServiceOption{ID: "natural_nails", Name: "Natural Nail Care", DurationMinutes: 40, BookingReady: true}
	staff := []StaffOption{{ID: "staff_lien", Name: "Lien", AIBookable: true}, {ID: "staff_thao", Name: "Thao", AIBookable: true}}
	basePlan := &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{{Label: "natural nails", Count: 2, ResolvedServiceIDs: []string{serviceOption.ID, serviceOption.ID}}}}
	baseOption := PartySplitOption{ID: "counterexample", Blocks: []PartySplitBlock{{
		StartTime: start,
		EndTime:   start.Add(40 * time.Minute),
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: serviceOption.ID, StaffID: staff[0].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
			{ServiceID: serviceOption.ID, StaffID: staff[1].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
		},
		QuoteRefs: []PartySplitQuoteRef{
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(40 * time.Minute), AvailabilityQuoteID: "quote-one", SlotFingerprint: strings.Repeat("1", 64)},
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(40 * time.Minute), AvailabilityQuoteID: "quote-one", SlotFingerprint: strings.Repeat("1", 64)},
		},
	}}}

	fixtures := []struct {
		name   string
		mutate func(*PartyPlan, *PartySplitOption)
	}{
		{
			name: "missing guest reference",
			mutate: func(_ *PartyPlan, option *PartySplitOption) {
				option.Blocks[0].QuoteRefs[1].GuestReference = ""
			},
		},
		{
			name: "child proof is not one aggregate proof",
			mutate: func(_ *PartyPlan, option *PartySplitOption) {
				option.Blocks[0].QuoteRefs[1].AvailabilityQuoteID = "quote-two"
			},
		},
		{
			name: "missing exact per-segment end",
			mutate: func(_ *PartyPlan, option *PartySplitOption) {
				option.Blocks[0].QuoteRefs[1].RequestedEndTime = time.Time{}
			},
		},
		{
			name: "legacy partial child success is present",
			mutate: func(plan *PartyPlan, _ *PartySplitOption) {
				plan.SplitAppointmentIDs = []string{"legacy-child-appointment"}
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = []ServiceOption{serviceOption}
			store.staff = staff
			tool := newQueuedManleAICalendarSchedulingTool()
			tool.actionResults = []*scheduling.ActionResult{internalConfirmedAction("must-not-confirm")}
			service := NewService(store, tool)
			plan := clonePartyPlan(basePlan)
			option := clonePartySplitOption(baseOption)
			fixture.mutate(plan, &option)
			session := reviewedInternalPartySession(store, plan, option, "quote-one", strings.Repeat("1", 64))
			turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Please book it.", "party-counterexample", store.services, store.staff, &store.cfg)

			got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
			if err != nil {
				t.Fatalf("tryBooking returned error: %v", err)
			}
			if tool.actionCalls != 0 || tool.fakeBookingTool.calls != 0 || got.Outcome == OutcomeBookingConfirmed || got.AppointmentID != "" {
				t.Fatalf("unsafe aggregate state reached an action: result=%#v actions=%d legacy=%d", got, tool.actionCalls, tool.fakeBookingTool.calls)
			}
		})
	}
}

func TestApplySelectedPartySplitOptionCarriesOnlyAggregateReviewedProof(t *testing.T) {
	start := time.Date(2026, 12, 2, 20, 0, 0, 0, time.UTC)
	fingerprint := strings.Repeat("6", 64)
	option := PartySplitOption{ID: "aggregate-proof", Blocks: []PartySplitBlock{{
		StartTime: start,
		EndTime:   start.Add(45 * time.Minute),
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: "classic_mani", StaffID: "staff_mina", StaffSelectionMode: booking.StaffSelectionSpecific},
			{ServiceID: "classic_mani", StaffID: "staff_lan", StaffSelectionMode: booking.StaffSelectionSpecific},
		},
		QuoteRefs: []PartySplitQuoteRef{
			{ServiceID: "classic_mani", GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute), AvailabilityQuoteID: "quote-aggregate", SlotFingerprint: fingerprint},
			{ServiceID: "classic_mani", GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(45 * time.Minute), AvailabilityQuoteID: "quote-aggregate", SlotFingerprint: fingerprint},
		},
	}}}
	session := Session{AvailabilityQuoteID: "stale-quote", SlotFingerprint: strings.Repeat("5", 64), PartyPlan: &PartyPlan{
		PartySize:    2,
		Groups:       []PartyPlanGroup{{Label: "manicure", Count: 2, ResolvedServiceIDs: []string{"classic_mani", "classic_mani"}}},
		SplitOptions: []PartySplitOption{option},
	}}

	applySelectedPartySplitOption(&session, option, true)
	if session.AvailabilityQuoteID != "quote-aggregate" || session.SlotFingerprint != fingerprint {
		t.Fatalf("aggregate proof was not carried into the selected root: %#v", session)
	}

	nonAggregate := clonePartySplitOption(option)
	nonAggregate.ID = "per-child-proofs"
	nonAggregate.Blocks[0].QuoteRefs[1].AvailabilityQuoteID = "different-child-quote"
	session.PartyPlan.SplitOptions = append(session.PartyPlan.SplitOptions, nonAggregate)
	applySelectedPartySplitOption(&session, nonAggregate, true)
	if session.AvailabilityQuoteID != "" || session.SlotFingerprint != "" {
		t.Fatalf("per-child proofs were promoted to an aggregate proof: quote=%q fingerprint=%q", session.AvailabilityQuoteID, session.SlotFingerprint)
	}
}

func TestManleAICalendarPartyExactReplayUsesSameRootOperation(t *testing.T) {
	start := time.Date(2026, 12, 5, 19, 0, 0, 0, time.UTC)
	serviceOption := ServiceOption{ID: "soft_gel", Name: "Soft Gel Manicure", DurationMinutes: 50, BookingReady: true}
	staff := []StaffOption{{ID: "staff_mai", Name: "Mai", AIBookable: true}, {ID: "staff_anh", Name: "Anh", AIBookable: true}}
	plan := &PartyPlan{PartySize: 2, Groups: []PartyPlanGroup{{Label: "soft gel", Count: 2, ResolvedServiceIDs: []string{serviceOption.ID, serviceOption.ID}}}}
	fingerprint := strings.Repeat("4", 64)
	option := PartySplitOption{ID: "party-replay", Blocks: []PartySplitBlock{{
		StartTime: start,
		EndTime:   start.Add(50 * time.Minute),
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: serviceOption.ID, StaffID: staff[0].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
			{ServiceID: serviceOption.ID, StaffID: staff[1].ID, StaffSelectionMode: booking.StaffSelectionSpecific},
		},
		QuoteRefs: []PartySplitQuoteRef{
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-1", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(50 * time.Minute), AvailabilityQuoteID: "quote-party-replay", SlotFingerprint: fingerprint},
			{ServiceID: serviceOption.ID, GuestReference: "group-1-guest-2", Quantity: 1, RequestedStartTime: start, RequestedEndTime: start.Add(50 * time.Minute), AvailabilityQuoteID: "quote-party-replay", SlotFingerprint: fingerprint},
		},
	}}}
	store := newFakeConversationStore()
	store.services = []ServiceOption{serviceOption}
	store.staff = staff
	tool := newQueuedManleAICalendarSchedulingTool()
	session := reviewedInternalPartySession(store, plan, option, "quote-party-replay", fingerprint)
	action, ok := reviewedInternalPartyAction(session, store.services)
	if !ok {
		t.Fatal("test fixture did not produce a reviewed internal party action")
	}
	tool.actionResults = []*scheduling.ActionResult{internalConfirmedPartyAction("appointment-party-replayed", action.Segments)}
	service := NewService(store, tool)

	for index, wording := range []string{"Reserve both soft gel appointments.", "The call dropped; verify that same group booking."} {
		turn := newTurnRecord(session.SalonID, "owner_1", session, session, wording, "party-replay-"+string(rune('1'+index)), store.services, store.staff, &store.cfg)
		got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
		if err != nil {
			t.Fatalf("replay %d returned error: %v", index+1, err)
		}
		if got.Outcome != OutcomeBookingConfirmed || got.AppointmentID != "appointment-party-replayed" {
			t.Fatalf("replay %d did not return the durable root: %#v", index+1, got)
		}
	}
	if tool.actionCalls != 2 || len(tool.actionRequests) != 2 || tool.actionRequests[0].OperationKey != tool.actionRequests[1].OperationKey {
		t.Fatalf("party replay changed root operation: %#v", tool.actionRequests)
	}
	assertSchedulingSegmentsEqual(t, tool.actionRequests[1].Segments, tool.actionRequests[0].Segments)
}

func internalConfirmedPartyAction(appointmentID string, segments []scheduling.ActionSegment) *scheduling.ActionResult {
	return internalConfirmedAction(appointmentID, segments...)
}

func reviewedInternalPartySession(store *fakeConversationStore, plan *PartyPlan, option PartySplitOption, quoteID string, fingerprint string) Session {
	session := store.session
	session.Intent = IntentBooking
	session.BookingAction = BookingActionBook
	session.CustomerName = "Jade Nguyen"
	session.CustomerPhone = "+13125550172"
	if segments := partySplitOptionSegments(option); len(segments) > 0 {
		session.ServiceID = segments[0].ServiceID
		if service := serviceByID(store.services, session.ServiceID); service != nil {
			session.ServiceName = service.Name
		}
	}
	session.RequestedDate = partySplitFirstStart(option).In(timezoneLocation(store.cfg.Timezone)).Format("2006-01-02")
	session.RequestedStartTime = nil
	session.AvailabilityQuoteID = quoteID
	session.SlotFingerprint = fingerprint
	session.BookingSegments = partySplitOptionSegments(option)
	session.PartyPlan = clonePartyPlan(plan)
	session.PartyPlan.SplitOptions = []PartySplitOption{clonePartySplitOption(option)}
	session.PartyPlan.SelectedSplitOptionID = option.ID
	session.DialogState = normalizedDialogState(session.DialogState)
	session.DialogState.ReviewRequired = false
	return session
}

func assertSchedulingSegmentsEqual(t *testing.T, got []scheduling.ActionSegment, want []scheduling.ActionSegment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("segment count = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		actual := got[index]
		expected := want[index]
		if actual.ServiceID != expected.ServiceID || actual.StaffID != expected.StaffID || actual.StaffSelectionMode != expected.StaffSelectionMode || actual.GuestReference != expected.GuestReference || actual.Quantity != expected.Quantity || !actual.RequestedStartTime.Equal(expected.RequestedStartTime) || !actual.RequestedEndTime.Equal(expected.RequestedEndTime) {
			t.Fatalf("segment %d = %#v, want %#v", index, actual, expected)
		}
	}
}
