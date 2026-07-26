package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type queuedManleAICalendarSchedulingTool struct {
	*fakeBookingTool
	availabilityResults  []*scheduling.AvailabilityResult
	actionResults        []*scheduling.ActionResult
	actionErrors         []error
	availabilityCalls    int
	actionCalls          int
	authorityCalls       int
	authority            string
	availabilityRequests []booking.AvailabilityRequest
	actionRequests       []scheduling.ActionRequest
}

func newQueuedManleAICalendarSchedulingTool() *queuedManleAICalendarSchedulingTool {
	return &queuedManleAICalendarSchedulingTool{fakeBookingTool: &fakeBookingTool{}}
}

func (f *queuedManleAICalendarSchedulingTool) CheckAvailability(_ context.Context, _ string, _ string, req booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error) {
	f.availabilityCalls++
	f.availabilityRequests = append(f.availabilityRequests, req)
	if len(f.availabilityResults) == 0 {
		return nil, nil
	}
	index := f.availabilityCalls - 1
	if index >= len(f.availabilityResults) {
		index = len(f.availabilityResults) - 1
	}
	return f.availabilityResults[index], nil
}

func (f *queuedManleAICalendarSchedulingTool) CheckConversationAvailability(ctx context.Context, salonID string, ownerUserID string, _ scheduling.BookingMode, req booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error) {
	return f.CheckAvailability(ctx, salonID, ownerUserID, req)
}

func (f *queuedManleAICalendarSchedulingTool) ExecuteAction(_ context.Context, _ string, _ string, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	f.actionCalls++
	f.actionRequests = append(f.actionRequests, req)
	index := f.actionCalls - 1
	if index < len(f.actionErrors) && f.actionErrors[index] != nil {
		return nil, f.actionErrors[index]
	}
	if len(f.actionResults) == 0 {
		return nil, nil
	}
	if index >= len(f.actionResults) {
		index = len(f.actionResults) - 1
	}
	result := *f.actionResults[index]
	result.OperationType = req.OperationType
	return &result, nil
}

func (f *queuedManleAICalendarSchedulingTool) ExecuteConversationAction(ctx context.Context, salonID string, ownerUserID string, _ scheduling.ConversationPolicyFence, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	return f.ExecuteAction(ctx, salonID, ownerUserID, req)
}

func (f *queuedManleAICalendarSchedulingTool) CurrentSchedulingAuthority(context.Context, string, string) (string, error) {
	f.authorityCalls++
	if strings.TrimSpace(f.authority) != "" {
		return strings.TrimSpace(f.authority), nil
	}
	return booking.SchedulingAuthorityManleAICalendar, nil
}

func manleAICalendarReadySession(store *fakeConversationStore, service ServiceOption, staff StaffOption, start time.Time, quoteID string, fingerprint string) Session {
	session := store.session
	session.Intent = IntentBooking
	session.BookingAction = BookingActionBook
	session.CustomerName = "Jade Nguyen"
	session.CustomerPhone = "+13125550172"
	session.ServiceID = service.ID
	session.ServiceName = service.Name
	session.StaffID = staff.ID
	session.StaffName = staff.Name
	session.StaffSelectionMode = booking.StaffSelectionSpecific
	session.RequestedDate = start.In(timezoneLocation(store.cfg.Timezone)).Format("2006-01-02")
	session.RequestedStartTime = &start
	session.AvailabilityQuoteID = quoteID
	session.SlotFingerprint = fingerprint
	session.BookingSegments = []booking.BookingSegmentRequest{{
		ServiceID:          service.ID,
		StaffID:            staff.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific,
	}}
	session.DialogState = normalizedDialogState(session.DialogState)
	session.DialogState.ReviewRequired = false
	return session
}

func internalConfirmedAction(appointmentID string, segments ...scheduling.ActionSegment) *scheduling.ActionResult {
	result := &scheduling.ActionResult{
		Kind:                        scheduling.ActionKindConfirmedAppointment,
		SchedulingAuthority:         booking.SchedulingAuthorityManleAICalendar,
		AuthorityAppointmentVersion: 1,
		ConfirmedAppointment: &scheduling.ConfirmedAppointmentResult{
			AppointmentID:     appointmentID,
			BookingAttemptID:  "attempt-for-" + appointmentID,
			AppointmentStatus: booking.StatusConfirmed,
			ActiveChildCount:  len(segments),
		},
	}
	for index, segment := range segments {
		result.ConfirmedAppointment.Children = append(result.ConfirmedAppointment.Children, scheduling.ConfirmedAppointmentSegment{
			AppointmentServiceID: appointmentID + "-service-" + string(rune('1'+index)),
			GuestReference:       segment.GuestReference,
			ServiceID:            segment.ServiceID,
			StaffID:              segment.StaffID,
			StaffSelectionMode:   segment.StaffSelectionMode,
			Quantity:             segment.Quantity,
			ScheduledStartTime:   segment.RequestedStartTime,
			ScheduledEndTime:     segment.RequestedEndTime,
			OccupiedStartTime:    segment.RequestedStartTime,
			OccupiedEndTime:      segment.RequestedEndTime,
		})
	}
	return result
}

func internalVerifiedAvailability(service ServiceOption, staff StaffOption, start time.Time, quoteID string, fingerprint string) *scheduling.AvailabilityResult {
	return &scheduling.AvailabilityResult{
		Kind:                scheduling.AvailabilityKindVerifiedSlots,
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		VerifiedSlots: &booking.AvailabilityResult{
			QuoteID:            quoteID,
			RequestFingerprint: strings.Repeat("f", 64),
			ServiceID:          service.ID,
			ServiceName:        service.Name,
			StaffID:            staff.ID,
			StaffName:          staff.Name,
			StaffSelectionMode: booking.StaffSelectionSpecific,
			PreferredDate:      start.In(timezoneLocation("America/Chicago")).Format("2006-01-02"),
			DurationMinutes:    service.DurationMinutes,
			Timezone:           "America/Chicago",
			Slots: []booking.AvailabilitySlot{{
				Fingerprint:        fingerprint,
				StartTime:          start,
				EndTime:            start.Add(time.Duration(service.DurationMinutes) * time.Minute),
				StaffID:            staff.ID,
				StaffName:          staff.Name,
				StaffSelectionMode: booking.StaffSelectionSpecific,
				Segments: []booking.AvailabilitySegment{{
					ServiceID: service.ID, ServiceName: service.Name,
					StaffID: staff.ID, StaffName: staff.Name,
					StaffSelectionMode: booking.StaffSelectionSpecific,
					DurationMinutes:    service.DurationMinutes,
				}},
			}},
		},
	}
}

func TestManleAICalendarSingleSegmentGoldenConfirmationsUseDurableInternalID(t *testing.T) {
	fixtures := []struct {
		name          string
		callerMessage string
		customerName  string
		service       ServiceOption
		staff         StaffOption
		start         time.Time
		appointmentID string
		wantReply     string
	}{
		{
			name:          "structured gel with named technician",
			callerMessage: "Wednesday after lunch with Thao is perfect; please lock in the structured gel service.",
			customerName:  "Jade Nguyen",
			service:       ServiceOption{ID: "service_structured_gel", Name: "Structured Gel Manicure", DurationMinutes: 55, BookingReady: true},
			staff:         StaffOption{ID: "staff_thao", Name: "Thao", AIBookable: true},
			start:         time.Date(2026, 8, 12, 20, 30, 0, 0, time.UTC),
			appointmentID: "appointment-internal-gel",
			wantReply:     "You're confirmed with Lotus Nails for your Structured Gel Manicure on Wednesday, August 12 at 3:30 PM with Thao. The appointment is under Jade Nguyen. Thank you, goodbye.",
		},
		{
			name:          "spa pedicure on a different date",
			callerMessage: "The Friday morning opening for the spa pedicure with Kim works for me.",
			customerName:  "Avery Pham",
			service:       ServiceOption{ID: "service_spa_pedicure", Name: "Spa Pedicure", DurationMinutes: 70, BookingReady: true},
			staff:         StaffOption{ID: "staff_kim", Name: "Kim", AIBookable: true},
			start:         time.Date(2026, 9, 18, 15, 15, 0, 0, time.UTC),
			appointmentID: "appointment-internal-pedi",
			wantReply:     "You're confirmed with Lotus Nails for your Spa Pedicure on Friday, September 18 at 10:15 AM with Kim. The appointment is under Avery Pham. Thank you, goodbye.",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = []ServiceOption{fixture.service}
			store.staff = []StaffOption{fixture.staff}
			tool := newQueuedManleAICalendarSchedulingTool()
			tool.actionResults = []*scheduling.ActionResult{internalConfirmedAction(fixture.appointmentID, scheduling.ActionSegment{
				ServiceID: fixture.service.ID, StaffID: fixture.staff.ID,
				StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
				RequestedStartTime: fixture.start,
				RequestedEndTime:   fixture.start.Add(time.Duration(fixture.service.DurationMinutes) * time.Minute),
			})}
			service := NewService(store, tool)
			session := manleAICalendarReadySession(store, fixture.service, fixture.staff, fixture.start, "quote-reviewed", strings.Repeat("a", 64))
			session.CustomerName = fixture.customerName
			turn := newTurnRecord(session.SalonID, "owner_1", session, session, fixture.callerMessage, "internal-golden", store.services, store.staff, &store.cfg)

			got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
			if err != nil {
				t.Fatalf("tryBooking returned error: %v", err)
			}
			if got.Status != StatusCompleted || got.Outcome != OutcomeBookingConfirmed || got.AppointmentID != fixture.appointmentID || got.BookingAttemptID != "attempt-for-"+fixture.appointmentID {
				t.Fatalf("internal result = status %q outcome %q appointment %q attempt %q", got.Status, got.Outcome, got.AppointmentID, got.BookingAttemptID)
			}
			if store.lastTurn.AIMessage != fixture.wantReply {
				t.Fatalf("golden reply = %q, want %q", store.lastTurn.AIMessage, fixture.wantReply)
			}
			if tool.actionCalls != 1 || tool.availabilityCalls != 0 {
				t.Fatalf("executor calls = action %d availability %d, reviewed proof should go directly to atomic commit", tool.actionCalls, tool.availabilityCalls)
			}
			req := tool.actionRequests[0]
			if req.AvailabilityQuoteID != "quote-reviewed" || req.SlotFingerprint != strings.Repeat("a", 64) || len(req.Segments) != 1 || req.Segments[0].ServiceID != fixture.service.ID || req.Segments[0].StaffID != fixture.staff.ID {
				t.Fatalf("reviewed internal proof/segment was not preserved: %#v", req)
			}
		})
	}
}

func TestManleAICalendarStaleCommitReoffersAndNeverConfirms(t *testing.T) {
	store := newFakeConversationStore()
	serviceOption := ServiceOption{ID: "service_builder_gel", Name: "Builder Gel Refill", DurationMinutes: 50, BookingReady: true}
	staffOption := StaffOption{ID: "staff_nhi", Name: "Nhi", AIBookable: true}
	store.services = []ServiceOption{serviceOption}
	store.staff = []StaffOption{staffOption}
	selectedStart := time.Date(2026, 10, 7, 19, 0, 0, 0, time.UTC)
	replacementStart := selectedStart.Add(90 * time.Minute)
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.actionErrors = []error{booking.ErrAvailabilityQuoteStale}
	tool.availabilityResults = []*scheduling.AvailabilityResult{
		internalVerifiedAvailability(serviceOption, staffOption, replacementStart, "quote-replacement", strings.Repeat("b", 64)),
	}
	service := NewService(store, tool)
	service.SetReplyGenerator(&fakeReplyGenerator{message: "You're confirmed; see you then."})
	session := manleAICalendarReadySession(store, serviceOption, staffOption, selectedStart, "quote-stale", strings.Repeat("a", 64))
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Yes, use the one o'clock opening we reviewed.", "internal-stale", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if got.Status != StatusActive || got.Outcome != OutcomeCollecting || got.AppointmentID != "" || got.BookingAttemptID != "" || got.Handoff != nil {
		t.Fatalf("stale result closed or mutated booking state: %#v", got)
	}
	if got.RequestedStartTime != nil || len(got.OfferedSlots) != 1 || !got.OfferedSlots[0].StartTime.Equal(replacementStart) || got.OfferedSlots[0].AvailabilityQuoteID != "quote-replacement" {
		t.Fatalf("stale result did not preserve draft and reoffer fresh proof: %#v", got)
	}
	if tool.actionCalls != 1 || tool.availabilityCalls != 1 || tool.fakeBookingTool.calls != 0 {
		t.Fatalf("stale path calls = action %d availability %d legacy create %d", tool.actionCalls, tool.availabilityCalls, tool.fakeBookingTool.calls)
	}
	lower := strings.ToLower(store.lastTurn.AIMessage)
	for _, forbidden := range []string{"you're confirmed", "appointment is confirmed", "has been booked"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("stale reply contains confirmation claim %q: %q", forbidden, store.lastTurn.AIMessage)
		}
	}
	if store.lastTurn.ReplyPolicy != ReplyPolicyOperationalFact || store.lastTurn.AIMetadata["llm_guardrail"] != "skipped_operational_fact" {
		t.Fatalf("stale operational fact was exposed to an unsafe rewrite: policy %q metadata %#v", store.lastTurn.ReplyPolicy, store.lastTurn.AIMetadata)
	}
}

func TestManleAICalendarExactCommittedReplayAfterLaterLifecycleUsesHistoricalCopy(t *testing.T) {
	store := newFakeConversationStore()
	serviceOption := ServiceOption{ID: "service_dry_pedicure", Name: "Dry Pedicure", DurationMinutes: 65, BookingReady: true}
	staffOption := StaffOption{ID: "staff_uyen", Name: "Uyen", AIBookable: true}
	store.services = []ServiceOption{serviceOption}
	store.staff = []StaffOption{staffOption}
	start := time.Date(2026, 11, 5, 22, 0, 0, 0, time.UTC)
	tool := newQueuedManleAICalendarSchedulingTool()
	replayed := internalConfirmedAction("appointment-replayed", scheduling.ActionSegment{
		ServiceID: serviceOption.ID, StaffID: staffOption.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
		RequestedStartTime: start,
		RequestedEndTime:   start.Add(time.Duration(serviceOption.DurationMinutes) * time.Minute),
	})
	replayed.Replayed = true
	tool.actionResults = []*scheduling.ActionResult{replayed}
	service := NewService(store, tool)
	session := manleAICalendarReadySession(store, serviceOption, staffOption, start, "quote-before-response-loss", strings.Repeat("c", 64))

	for index, wording := range []string{"Please book that Thursday slot.", "The response dropped; please check the same booking."} {
		turn := newTurnRecord(session.SalonID, "owner_1", session, session, wording, "response-loss-"+string(rune('1'+index)), store.services, store.staff, &store.cfg)
		got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
		if err != nil {
			t.Fatalf("tryBooking replay %d returned error: %v", index+1, err)
		}
		if got.AppointmentID != "appointment-replayed" || got.Outcome != OutcomeBookingConfirmed {
			t.Fatalf("replay %d result = %#v", index+1, got)
		}
		reply := strings.ToLower(store.lastTurn.AIMessage)
		for _, forbidden := range []string{"you're confirmed", "appointment is confirmed", "has been booked", "is now confirmed"} {
			if strings.Contains(reply, forbidden) {
				t.Fatalf("historical booking replay %d used current-state copy %q: %q", index+1, forbidden, store.lastTurn.AIMessage)
			}
		}
		for _, required := range []string{"booking succeeded at that time", "current status may have changed", "not confirmation of its current status"} {
			if !strings.Contains(reply, required) {
				t.Fatalf("historical booking replay %d missing %q: %q", index+1, required, store.lastTurn.AIMessage)
			}
		}
	}
	if tool.actionCalls != 2 || tool.availabilityCalls != 0 || len(tool.actionRequests) != 2 || tool.actionRequests[0].OperationKey != tool.actionRequests[1].OperationKey {
		t.Fatalf("exact replay contract = actions %d availability %d requests %#v", tool.actionCalls, tool.availabilityCalls, tool.actionRequests)
	}
}

func TestManleAICalendarSelectedPartySplitStaysGatedWithoutPartialWrite(t *testing.T) {
	store := newFakeConversationStore()
	serviceOption := ServiceOption{ID: "service_manicure", Name: "Classic Manicure", DurationMinutes: 45, BookingReady: true}
	staffOption := StaffOption{ID: "staff_mina", Name: "Mina", AIBookable: true}
	store.services = []ServiceOption{serviceOption}
	store.staff = []StaffOption{staffOption}
	start := time.Date(2026, 12, 3, 19, 0, 0, 0, time.UTC)
	tool := newQueuedManleAICalendarSchedulingTool()
	service := NewService(store, tool)
	session := manleAICalendarReadySession(store, serviceOption, staffOption, start, "quote-party", strings.Repeat("d", 64))
	option := PartySplitOption{ID: "split-two-guests", Blocks: []PartySplitBlock{
		{StartTime: start, EndTime: start.Add(45 * time.Minute), Segments: []booking.BookingSegmentRequest{{ServiceID: serviceOption.ID, StaffID: staffOption.ID, StaffSelectionMode: booking.StaffSelectionSpecific}}},
		{StartTime: start.Add(45 * time.Minute), EndTime: start.Add(90 * time.Minute), Segments: []booking.BookingSegmentRequest{{ServiceID: serviceOption.ID, StaffID: staffOption.ID, StaffSelectionMode: booking.StaffSelectionSpecific}}},
	}}
	session.PartyPlan = &PartyPlan{PartySize: 2, SplitOptions: []PartySplitOption{option}, SelectedSplitOptionID: option.ID}
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Book both guests in those two openings.", "party-gated", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if got.Status != StatusHandoff || got.AppointmentID != "" || got.BookingAttemptID != "" || tool.actionCalls != 0 || tool.fakeBookingTool.calls != 0 {
		t.Fatalf("party gate allowed a partial internal write: session %#v action %d create %d", got, tool.actionCalls, tool.fakeBookingTool.calls)
	}
	if lower := strings.ToLower(store.lastTurn.AIMessage); strings.Contains(lower, "confirmed") && !strings.Contains(lower, "not a confirmed group appointment") {
		t.Fatalf("party gate claimed success: %q", store.lastTurn.AIMessage)
	}
}

func TestManleAICalendarRejectsProviderShapedConfirmationEvidence(t *testing.T) {
	store := newFakeConversationStore()
	serviceOption := ServiceOption{ID: "service_natural_nails", Name: "Natural Nail Care", DurationMinutes: 40, BookingReady: true}
	staffOption := StaffOption{ID: "staff_lien", Name: "Lien", AIBookable: true}
	store.services = []ServiceOption{serviceOption}
	store.staff = []StaffOption{staffOption}
	start := time.Date(2026, 12, 9, 21, 0, 0, 0, time.UTC)
	malformed := internalConfirmedAction("appointment-internal-malformed", scheduling.ActionSegment{
		ServiceID: serviceOption.ID, StaffID: staffOption.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
		RequestedStartTime: start,
		RequestedEndTime:   start.Add(time.Duration(serviceOption.DurationMinutes) * time.Minute),
	})
	malformed.ConfirmedAppointment.ExternalAttemptID = "fake-pos-attempt"
	tool := newQueuedManleAICalendarSchedulingTool()
	tool.actionResults = []*scheduling.ActionResult{malformed}
	service := NewService(store, tool)
	session := manleAICalendarReadySession(store, serviceOption, staffOption, start, "quote-malformed", strings.Repeat("e", 64))
	turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Use that opening.", "malformed-internal", store.services, store.staff, &store.cfg)

	got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
	if err != nil {
		t.Fatalf("tryBooking returned error: %v", err)
	}
	if got.Outcome == OutcomeBookingConfirmed || got.AppointmentID != "" || !strings.Contains(strings.ToLower(store.lastTurn.AIMessage), "not a confirmed appointment") {
		t.Fatalf("provider-shaped internal evidence was accepted: %#v reply %q", got, store.lastTurn.AIMessage)
	}
}

func TestManleAICalendarBookNeverConfirmsIDsWithoutAuthoritativeRootAndChildEvidence(t *testing.T) {
	serviceOption := ServiceOption{ID: "service_acrylic_overlay", Name: "Acrylic Overlay", DurationMinutes: 75, BookingReady: true}
	staffOption := StaffOption{ID: "staff_mai", Name: "Mai", AIBookable: true}
	start := time.Date(2027, 1, 14, 16, 45, 0, 0, time.UTC)
	segment := scheduling.ActionSegment{
		ServiceID: serviceOption.ID, StaffID: staffOption.ID,
		StaffSelectionMode: booking.StaffSelectionSpecific, Quantity: 1,
		RequestedStartTime: start,
		RequestedEndTime:   start.Add(time.Duration(serviceOption.DurationMinutes) * time.Minute),
	}
	valid := internalConfirmedAction("appointment-authoritative", segment)
	tests := []struct {
		name   string
		mutate func(*scheduling.ActionResult)
	}{
		{name: "ids only", mutate: func(result *scheduling.ActionResult) {
			result.AuthorityAppointmentVersion = 0
			result.ConfirmedAppointment.AppointmentStatus = ""
			result.ConfirmedAppointment.ActiveChildCount = 0
			result.ConfirmedAppointment.Children = nil
		}},
		{name: "wrong persisted status", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.AppointmentStatus = booking.StatusRescheduled
		}},
		{name: "wrong authority version", mutate: func(result *scheduling.ActionResult) {
			result.AuthorityAppointmentVersion = 2
		}},
		{name: "missing active child count", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.ActiveChildCount = 0
		}},
		{name: "mismatched active child count", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.ActiveChildCount = 2
		}},
		{name: "missing exact child graph", mutate: func(result *scheduling.ActionResult) {
			result.ConfirmedAppointment.Children = nil
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeConversationStore()
			store.services = []ServiceOption{serviceOption}
			store.staff = []StaffOption{staffOption}
			copyResult := *valid
			copyConfirmed := *valid.ConfirmedAppointment
			copyConfirmed.Children = append([]scheduling.ConfirmedAppointmentSegment{}, valid.ConfirmedAppointment.Children...)
			copyResult.ConfirmedAppointment = &copyConfirmed
			test.mutate(&copyResult)

			tool := newQueuedManleAICalendarSchedulingTool()
			tool.actionResults = []*scheduling.ActionResult{&copyResult}
			service := NewService(store, tool)
			session := manleAICalendarReadySession(store, serviceOption, staffOption, start, "quote-authoritative", strings.Repeat("a", 64))
			turn := newTurnRecord(session.SalonID, "owner_1", session, session, "Please reserve the reviewed opening.", "authoritative-book-"+test.name, store.services, store.staff, &store.cfg)

			got, err := service.tryBooking(context.Background(), "owner_1", turn, session, store.services, store.staff, &store.cfg, nil)
			if err != nil {
				t.Fatalf("tryBooking returned error: %v", err)
			}
			if got.Outcome == OutcomeBookingConfirmed || got.AppointmentID != "" || got.BookingAttemptID != "" {
				t.Fatalf("invalid internal evidence was confirmed: %#v", got)
			}
			lower := strings.ToLower(store.lastTurn.AIMessage)
			if strings.Contains(lower, "you're confirmed") || strings.Contains(lower, "appointment is confirmed") || strings.Contains(lower, "has been booked") {
				t.Fatalf("invalid internal evidence produced success copy: %q", store.lastTurn.AIMessage)
			}
		})
	}
}

func TestOwnerFirstCatalogQueriesDoNotBroadenExternalProviderGuidance(t *testing.T) {
	externalGuidance := serviceOptionsQuery(false)
	for _, required := range []string{"JOIN pos_entity_links", "link.provider = salon.active_pos_provider", "svc.pos_provider = salon.active_pos_provider", "svc.sync_status = 'synced'"} {
		if !strings.Contains(externalGuidance, required) {
			t.Fatalf("external guidance query lost provider fence %q: %s", required, externalGuidance)
		}
	}
	externalBookable := serviceOptionsQuery(true)
	for _, required := range []string{"JOIN pos_connections", "connection.snapshot_generation > 0", "connection.last_sync_at IS NOT NULL"} {
		if !strings.Contains(externalBookable, required) {
			t.Fatalf("external booking query lost readiness fence %q: %s", required, externalBookable)
		}
	}
	canonical := serviceOptionsQueryWithGuards("", "")
	for _, forbidden := range []string{"pos_entity_links", "pos_connections", "active_pos_provider", "sync_status = 'synced'"} {
		if strings.Contains(canonical, forbidden) {
			t.Fatalf("canonical owner-first query unexpectedly depends on provider evidence %q: %s", forbidden, canonical)
		}
	}
}
