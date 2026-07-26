package conversation

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func (s *Service) tryBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	// Disabled is a conversation policy, so it must stop the scheduling flow
	// before the planner asks for more booking data or any scheduling tool is
	// consulted. Durable operation replay remains replay-first inside the
	// scheduling boundary for callers that already hold an operation key.
	if cfg == nil || configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this request, and no appointment is confirmed.", services, staff, cfg)
	}
	if nextAction := planNextConversationAction(session, missingBookingField(session), cfg); nextAction.Kind != AssistantActionExecuteBooking {
		return s.continueAfterDraftReady(ctx, ownerUserID, turn, turn.Session, session, services, staff, cfg, knowledge)
	}
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	if option, ok := selectedPartySplitOption(session.PartyPlan); ok {
		if configuredConversationBookingMode(cfg) == scheduling.BookingModePendingApproval && s.schedulingTool != nil {
			return s.tryNeutralBooking(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
		}
		if s.schedulingTool != nil {
			authority, err := s.schedulingTool.CurrentSchedulingAuthority(ctx, turn.SalonID, ownerUserID)
			if err != nil {
				return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
			}
			if authority == booking.SchedulingAuthorityManleAICalendar {
				return s.tryNeutralBooking(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
			}
			if authority != booking.SchedulingAuthorityExternalProvider {
				return s.tryNeutralBooking(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
			}
		}
		return s.tryPartySplitBooking(ctx, ownerUserID, turn, session, option, services, staff, cfg, knowledge)
	}
	if s.schedulingTool != nil {
		return s.tryNeutralBooking(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
	}
	if !bookingServiceSelectionConsistent(session) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	availabilityResult, fresh, err := s.refreshSelectedAvailabilityProof(ctx, ownerUserID, turn.SalonID, &session, services, cfg)
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	if !fresh {
		session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
		applyAvailabilityOffer(&turn, &session, services, staff, cfg, availabilityResult, true)
		turn.AIMessage = "That opening changed before I could book it. " + turn.AIMessage
		finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "availability_changed_before_booking")
		return s.store.SaveTurn(ctx, turn)
	}
	syncTurnUpdate(&turn, session, services, staff, cfg)
	startedAt := time.Now()
	attempt, err := s.bookingTool.Create(ctx, turn.SalonID, ownerUserID, booking.CreateBookingRequest{
		OperationKey:        conversationOperationKey(session, booking.BookingActionBook),
		AvailabilityQuoteID: strings.TrimSpace(session.AvailabilityQuoteID),
		SlotFingerprint:     strings.TrimSpace(session.SlotFingerprint),
		Source:              bookingSourceForSession(session),
		CustomerName:        session.CustomerName,
		CustomerPhone:       session.CustomerPhone,
		CustomerEmail:       session.CustomerEmail,
		ServiceID:           session.ServiceID,
		StaffID:             session.StaffID,
		StaffSelectionMode:  staffSelectionModeForSession(session),
		Segments:            bookingSegmentsForCreate(session),
		StartTime:           *session.RequestedStartTime,
		Notes:               bookingNotesForSession(session),
	})
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}

	toolMessage := "Booking service returned fallback pending."
	outcome := OutcomeBookingFallbackPending
	status := StatusCompleted
	aiMessage := bookingFallbackReply()
	bookingAttemptID := ""
	appointmentID := ""
	if attempt != nil {
		bookingAttemptID = attempt.ID
	}
	if attempt != nil && attempt.Status == booking.StatusConfirmed && attempt.Appointment != nil && attempt.POSBookingID != "" {
		toolMessage = "Booking service confirmed appointment through POS."
		outcome = OutcomeBookingConfirmed
		aiMessage = confirmedMessage(session, services, staff, cfg)
		appointmentID = attempt.Appointment.ID
	} else if attempt == nil {
		toolMessage = "Booking service returned no booking attempt."
	}

	turn.ToolMessage = toolMessage
	turn.AIMessage = aiMessage
	turn.Update.Status = status
	turn.Update.Outcome = outcome
	turn.Update.BookingAttemptID = bookingAttemptID
	turn.Update.AppointmentID = appointmentID
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "", "", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "booking_result")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) tryNeutralBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if cfg == nil || configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this request, and no appointment is confirmed.", services, staff, cfg)
	}
	authority, err := s.schedulingTool.CurrentSchedulingAuthority(ctx, turn.SalonID, ownerUserID)
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	bookingMode := configuredConversationBookingMode(cfg)
	_, reviewedInternalParty := reviewedInternalPartyAction(session, services)
	_, reviewedPendingParty := reviewedPendingPartyAction(session, services)
	_, hasSelectedPartyOption := selectedPartySplitOption(session.PartyPlan)
	if session.RequestedStartTime == nil && !(authority == booking.SchedulingAuthorityManleAICalendar && reviewedInternalParty) && !(bookingMode == scheduling.BookingModePendingApproval && reviewedPendingParty) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	if bookingMode == scheduling.BookingModeConfirmedBooking && authority == booking.SchedulingAuthorityManleAICalendar && !reviewedInternalParty && (hasSelectedPartyOption || len(bookingSegmentsForCreate(session)) != 1) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(0, len(bookingSegmentsForCreate(session))), services, staff, cfg)
	}
	// An internal quote was already reviewed by the caller. Execute it directly:
	// the internal transaction is the final conflict check, and an exact
	// operation-key replay must reach the executor even after its quote was
	// consumed by a commit whose response was lost. Older drafts without proof
	// still receive the legacy pre-action refresh.
	if !(bookingMode == scheduling.BookingModePendingApproval && reviewedPendingParty) && (authority != booking.SchedulingAuthorityManleAICalendar || strings.TrimSpace(session.AvailabilityQuoteID) == "" || strings.TrimSpace(session.SlotFingerprint) == "") {
		availabilityResult, fresh, refreshErr := s.refreshSelectedAvailabilityProof(ctx, ownerUserID, turn.SalonID, &session, services, cfg)
		if refreshErr != nil {
			return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
		}
		if !fresh {
			session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
			applyAvailabilityOffer(&turn, &session, services, staff, cfg, availabilityResult, true)
			turn.AIMessage = "That opening changed before I could book it. " + turn.AIMessage
			finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "availability_changed_before_booking")
			return s.store.SaveTurn(ctx, turn)
		}
	}

	req, ok := schedulingActionRequest(session, services, cfg, scheduling.OperationKindBook)
	if !ok {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	syncTurnUpdate(&turn, session, services, staff, cfg)
	startedAt := time.Now()
	result, err := s.schedulingTool.ExecuteConversationAction(ctx, turn.SalonID, ownerUserID, conversationPolicyFenceForExecution(session, cfg), req)
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil {
		if bookingMode == scheduling.BookingModeConfirmedBooking && authority == booking.SchedulingAuthorityManleAICalendar && schedulingAvailabilityRefreshRequired(err) {
			if reviewedInternalParty {
				return s.reofferInternalPartyBookingAfterConflict(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
			}
			return s.reofferInternalBookingAfterConflict(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
		}
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	if !applyNeutralActionResult(&turn, session, result, services, staff, cfg) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "", "", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "booking_result")
	return s.store.SaveTurn(ctx, turn)
}

type availabilityRefreshRequiredError interface {
	AvailabilityRefreshRequired() bool
}

func schedulingAvailabilityRefreshRequired(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, booking.ErrAvailabilityQuoteStale) {
		return true
	}
	var typed availabilityRefreshRequiredError
	return errors.As(err, &typed) && typed.AvailabilityRefreshRequired()
}

func (s *Service) reofferInternalBookingAfterConflict(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	preferredDate := strings.TrimSpace(session.RequestedDate)
	if preferredDate == "" && session.RequestedStartTime != nil {
		preferredDate = session.RequestedStartTime.In(timezoneLocation(timezoneFromConfig(cfg))).Format("2006-01-02")
	}
	session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
	session.RequestedStartTime = nil
	clearSelectedAvailabilityQuote(&session)
	session.OfferedSlots = nil
	turn.Update.Status = StatusActive
	turn.Update.Outcome = OutcomeCollecting
	turn.Update.AppointmentID = ""
	turn.Update.BookingAttemptID = ""
	turn.Update.EndSession = false

	refreshErr := s.offerAvailableSlots(ctx, ownerUserID, &turn, &session, services, staff, preferredDate, true, cfg)
	if refreshErr != nil {
		turn.ToolMessage = "Internal scheduling evidence changed before commit; availability must be checked again."
		turn.AIMessage = "I couldn't verify that opening, so I did not book it. What other day or time works?"
		syncTurnUpdate(&turn, session, services, staff, cfg)
	} else {
		turn.AIMessage = "That opening changed before I could book it, so nothing was booked. " + turn.AIMessage
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"scheduling_authority": booking.SchedulingAuthorityManleAICalendar,
		"booking_result":       "availability_refresh_required",
		"appointment_created":  false,
	})
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "requested_time", "requested_time", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "internal_availability_changed_before_commit")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) reofferInternalPartyBookingAfterConflict(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	preferredDate := strings.TrimSpace(session.RequestedDate)
	if preferredDate == "" {
		if option, ok := selectedPartySplitOption(session.PartyPlan); ok {
			if first := partySplitFirstStart(option); !first.IsZero() {
				preferredDate = first.In(timezoneLocation(timezoneFromConfig(cfg))).Format("2006-01-02")
			}
		}
	}
	plan := clonePartyPlan(session.PartyPlan)
	if plan == nil {
		plan = &PartyPlan{}
	}
	plan.SplitOptions = nil
	plan.SelectedSplitOptionID = ""
	plan.SplitBookingAttemptIDs = nil
	plan.SplitAppointmentIDs = nil
	session.PartyPlan = plan
	session.BookingSegments = partyPlanSegments(plan, session)
	session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
	session.RequestedStartTime = nil
	clearSelectedAvailabilityQuote(&session)
	session.OfferedSlots = nil
	turn.Update.Status = StatusActive
	turn.Update.Outcome = OutcomeCollecting
	turn.Update.AppointmentID = ""
	turn.Update.BookingAttemptID = ""
	turn.Update.EndSession = false

	refreshErr := s.offerAvailableSlots(ctx, ownerUserID, &turn, &session, services, staff, preferredDate, true, cfg)
	if refreshErr != nil {
		turn.ToolMessage = "The reviewed aggregate party allocation changed before commit; no appointment was created."
		turn.AIMessage = "I couldn't verify all of those group openings together, so nothing was booked. What other day or time works?"
		syncTurnUpdate(&turn, session, services, staff, cfg)
	} else {
		turn.AIMessage = "Those group openings changed before booking, so nothing was booked. " + turn.AIMessage
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"scheduling_authority": booking.SchedulingAuthorityManleAICalendar,
		"booking_result":       "availability_refresh_required",
		"appointment_created":  false,
	})
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "requested_time", "requested_time", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "internal_party_availability_changed_before_commit")
	return s.store.SaveTurn(ctx, turn)
}

func schedulingActionRequest(session Session, services []ServiceOption, cfg *RuntimeConfig, operation scheduling.OperationKind) (scheduling.ActionRequest, bool) {
	segments := schedulingActionSegments(session, services)
	partyAction, reviewedParty := reviewedInternalPartyAction(session, services)
	pendingPartyAction, reviewedPendingParty := reviewedPendingPartyAction(session, services)
	lifecycleAction, lifecycleTarget, reviewedLifecycle := reviewedInternalLifecycleAction(session)
	internalTarget, internalLifecycleTarget := selectedInternalLifecycleCandidate(session)
	if operation == scheduling.OperationKindBook && reviewedParty {
		segments = partyAction.Segments
	}
	if operation == scheduling.OperationKindBook && configuredConversationBookingMode(cfg) == scheduling.BookingModePendingApproval && reviewedPendingParty {
		segments = pendingPartyAction.Segments
	}
	if operation == scheduling.OperationKindReschedule && reviewedLifecycle {
		segments = lifecycleAction.Segments
	}
	if operation == scheduling.OperationKindReschedule && internalLifecycleTarget && !reviewedLifecycle {
		return scheduling.ActionRequest{}, false
	}
	if (operation == scheduling.OperationKindBook || operation == scheduling.OperationKindReschedule) && len(segments) == 0 {
		return scheduling.ActionRequest{}, false
	}
	req := scheduling.ActionRequest{
		OperationType:       operation,
		OperationKey:        conversationOperationKey(session, string(operation), operationTargetKey(session)),
		AvailabilityQuoteID: strings.TrimSpace(session.AvailabilityQuoteID),
		SlotFingerprint:     strings.TrimSpace(session.SlotFingerprint),
		Source:              bookingSourceForSession(session),
		CallSessionID:       strings.TrimSpace(session.ID),
		CustomerName:        strings.TrimSpace(session.CustomerName),
		CustomerPhone:       strings.TrimSpace(session.CustomerPhone),
		CustomerEmail:       strings.TrimSpace(session.CustomerEmail),
		Segments:            segments,
		RequestedTimezone:   timezoneFromConfig(cfg),
		PartySize:           schedulingPartySize(session, segments),
		Notes:               bookingNotesForSession(session),
		TargetAppointmentID: strings.TrimSpace(session.TargetAppointmentID),
	}
	if operation == scheduling.OperationKindBook && reviewedParty {
		req.AvailabilityQuoteID = partyAction.AvailabilityQuoteID
		req.SlotFingerprint = partyAction.SlotFingerprint
		req.RequestedStartTime = partyAction.StartTime
		req.RequestedEndTime = partyAction.EndTime
	}
	if operation == scheduling.OperationKindBook && configuredConversationBookingMode(cfg) == scheduling.BookingModePendingApproval && reviewedPendingParty {
		req.AvailabilityQuoteID = ""
		req.SlotFingerprint = ""
		req.RequestedStartTime = pendingPartyAction.StartTime
		req.RequestedEndTime = pendingPartyAction.EndTime
	}
	if operation == scheduling.OperationKindReschedule && reviewedLifecycle {
		req.AvailabilityQuoteID = lifecycleAction.AvailabilityQuoteID
		req.SlotFingerprint = lifecycleAction.SlotFingerprint
		req.RequestedStartTime = lifecycleAction.StartTime
		req.RequestedEndTime = lifecycleAction.EndTime
		req.PartySize = lifecycleTarget.PartySize
		req.TargetAuthority = lifecycleTarget.SchedulingAuthority
		req.ExpectedTargetAuthorityAppointmentVersion = lifecycleTarget.AuthorityAppointmentVersion
	}
	if operation == scheduling.OperationKindCancel && internalLifecycleTarget && configuredConversationBookingMode(cfg) == scheduling.BookingModeConfirmedBooking {
		req.AvailabilityQuoteID = ""
		req.SlotFingerprint = ""
		req.Segments = nil
		req.RequestedStartTime = time.Time{}
		req.RequestedEndTime = time.Time{}
		req.RequestedTimezone = ""
		req.PartySize = 0
		req.TargetAuthority = internalTarget.SchedulingAuthority
		req.ExpectedTargetAuthorityAppointmentVersion = internalTarget.AuthorityAppointmentVersion
		req.Notes = selectedInternalLifecycleCancelReason(session)
	}
	if session.DialogState.ManualTarget != nil && req.TargetAppointmentID == "" {
		req.TargetDescription = strings.TrimSpace(session.DialogState.ManualTarget.Description)
	}
	if session.RequestedStartTime != nil && !reviewedParty && !(configuredConversationBookingMode(cfg) == scheduling.BookingModePendingApproval && reviewedPendingParty) && !reviewedLifecycle && !(operation == scheduling.OperationKindCancel && internalLifecycleTarget && configuredConversationBookingMode(cfg) == scheduling.BookingModeConfirmedBooking) {
		req.RequestedStartTime = *session.RequestedStartTime
		if end, ok := expectedAvailabilityEnd(session, services); ok {
			req.RequestedEndTime = end
		}
	}
	if operation == scheduling.OperationKindCancel && internalLifecycleTarget && configuredConversationBookingMode(cfg) == scheduling.BookingModeConfirmedBooking {
		return req, req.OperationKey != "" && req.Source != "" && req.TargetAppointmentID != "" &&
			req.TargetAuthority == booking.SchedulingAuthorityManleAICalendar && req.ExpectedTargetAuthorityAppointmentVersion > 0 && req.Notes != ""
	}
	if req.OperationKey == "" || req.CustomerName == "" || req.CustomerPhone == "" || req.RequestedTimezone == "" {
		return scheduling.ActionRequest{}, false
	}
	return req, true
}

func conversationPolicyFenceForExecution(session Session, cfg *RuntimeConfig) scheduling.ConversationPolicyFence {
	state := normalizedDialogState(session.DialogState)
	mode := state.ReviewedBookingMode
	if mode == "" && cfg != nil {
		mode = cfg.BookingMode
	}
	authority := strings.TrimSpace(state.SelectedSchedulingAuthority)
	if authority == "" {
		authority = selectedSchedulingAuthorityForReview(session, cfg)
	}
	return scheduling.ConversationPolicyFence{BookingMode: mode, SchedulingAuthority: authority}
}

func configuredConversationBookingMode(cfg *RuntimeConfig) scheduling.BookingMode {
	if cfg == nil {
		return scheduling.BookingModeDisabled
	}
	if cfg.BookingMode == "" {
		return scheduling.BookingModeConfirmedBooking
	}
	return cfg.BookingMode
}

type reviewedPartyAction struct {
	Option              PartySplitOption
	AvailabilityQuoteID string
	SlotFingerprint     string
	Segments            []scheduling.ActionSegment
	StartTime           time.Time
	EndTime             time.Time
}

// reviewedInternalPartyAction accepts only one aggregate proof whose concrete
// allocation still matches the structured party plan. It never fills missing
// guest, staff, or timing data by inference.
func reviewedInternalPartyAction(session Session, services []ServiceOption) (reviewedPartyAction, bool) {
	plan := session.PartyPlan
	option, ok := selectedPartySplitOption(plan)
	if !ok || !partyPlanComplete(plan) || len(plan.SplitAppointmentIDs) != 0 || len(plan.SplitBookingAttemptIDs) != 0 || (option.RequiresDateConsent && !option.DateConsentConfirmed) {
		return reviewedPartyAction{}, false
	}
	quoteID := strings.TrimSpace(session.AvailabilityQuoteID)
	fingerprint := strings.TrimSpace(session.SlotFingerprint)
	if quoteID == "" || len(fingerprint) != 64 {
		return reviewedPartyAction{}, false
	}
	planSegments := partyPlanSegments(plan, session)
	guestReferences, ok := partyPlanSegmentGuestReferences(plan, planSegments)
	if !ok {
		return reviewedPartyAction{}, false
	}
	expected := make(map[string]int, len(planSegments))
	for index, segment := range planSegments {
		key := guestReferences[index] + "\x00" + strings.TrimSpace(segment.ServiceID)
		expected[key]++
	}
	result := reviewedPartyAction{Option: option, AvailabilityQuoteID: quoteID, SlotFingerprint: fingerprint}
	guests := map[string]bool{}
	for _, block := range option.Blocks {
		if block.StartTime.IsZero() || block.EndTime.IsZero() || !block.EndTime.After(block.StartTime) || len(block.Segments) == 0 || len(block.QuoteRefs) != len(block.Segments) {
			return reviewedPartyAction{}, false
		}
		var latestSegmentEnd time.Time
		for index, segment := range block.Segments {
			ref := block.QuoteRefs[index]
			serviceID := strings.TrimSpace(segment.ServiceID)
			staffID := strings.TrimSpace(segment.StaffID)
			guestReference := strings.TrimSpace(ref.GuestReference)
			mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
			service := serviceByID(services, serviceID)
			if serviceID == "" || staffID == "" || mode != booking.StaffSelectionSpecific || service == nil || service.DurationMinutes <= 0 || strings.TrimSpace(ref.ServiceID) != serviceID || guestReference == "" || ref.Quantity != 1 || ref.RequestedStartTime.IsZero() || ref.RequestedEndTime.IsZero() || !ref.RequestedEndTime.After(ref.RequestedStartTime) || !ref.RequestedStartTime.Equal(block.StartTime) || ref.RequestedEndTime.After(block.EndTime) || strings.TrimSpace(ref.AvailabilityQuoteID) != quoteID || strings.TrimSpace(ref.SlotFingerprint) != fingerprint {
				return reviewedPartyAction{}, false
			}
			key := guestReference + "\x00" + serviceID
			if expected[key] <= 0 {
				return reviewedPartyAction{}, false
			}
			expected[key]--
			result.Segments = append(result.Segments, scheduling.ActionSegment{
				ServiceID:          serviceID,
				StaffID:            staffID,
				StaffSelectionMode: mode,
				GuestReference:     guestReference,
				Quantity:           ref.Quantity,
				RequestedStartTime: ref.RequestedStartTime,
				RequestedEndTime:   ref.RequestedEndTime,
			})
			guests[guestReference] = true
			if result.StartTime.IsZero() || ref.RequestedStartTime.Before(result.StartTime) {
				result.StartTime = ref.RequestedStartTime
			}
			if result.EndTime.IsZero() || ref.RequestedEndTime.After(result.EndTime) {
				result.EndTime = ref.RequestedEndTime
			}
			if latestSegmentEnd.IsZero() || ref.RequestedEndTime.After(latestSegmentEnd) {
				latestSegmentEnd = ref.RequestedEndTime
			}
		}
		if !latestSegmentEnd.Equal(block.EndTime) {
			return reviewedPartyAction{}, false
		}
	}
	for _, remaining := range expected {
		if remaining != 0 {
			return reviewedPartyAction{}, false
		}
	}
	if len(result.Segments) < 2 || len(guests) != plan.PartySize || result.StartTime.IsZero() || !result.EndTime.After(result.StartTime) {
		return reviewedPartyAction{}, false
	}
	return result, true
}

// reviewedPendingPartyAction validates the selected per-guest party allocation
// even when an external provider issued one quote per segment. Quote proof is
// selection evidence only; ExecuteConversationAction removes it before the
// non-reserving owner-review request is persisted.
func reviewedPendingPartyAction(session Session, services []ServiceOption) (reviewedPartyAction, bool) {
	plan := session.PartyPlan
	option, ok := selectedPartySplitOption(plan)
	if !ok || !partyPlanComplete(plan) || len(plan.SplitAppointmentIDs) != 0 || len(plan.SplitBookingAttemptIDs) != 0 || (option.RequiresDateConsent && !option.DateConsentConfirmed) {
		return reviewedPartyAction{}, false
	}
	planSegments := partyPlanSegments(plan, session)
	guestReferences, ok := partyPlanSegmentGuestReferences(plan, planSegments)
	if !ok {
		return reviewedPartyAction{}, false
	}
	expected := make(map[string]int, len(planSegments))
	for index, segment := range planSegments {
		expected[guestReferences[index]+"\x00"+strings.TrimSpace(segment.ServiceID)]++
	}
	result := reviewedPartyAction{Option: option}
	guests := make(map[string]struct{}, plan.PartySize)
	for _, block := range option.Blocks {
		if block.StartTime.IsZero() || !block.EndTime.After(block.StartTime) || len(block.Segments) == 0 || len(block.QuoteRefs) != len(block.Segments) {
			return reviewedPartyAction{}, false
		}
		var latestEnd time.Time
		for index, segment := range block.Segments {
			ref := block.QuoteRefs[index]
			serviceID := strings.TrimSpace(segment.ServiceID)
			staffID := strings.TrimSpace(segment.StaffID)
			guestReference := strings.TrimSpace(ref.GuestReference)
			mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
			service := serviceByID(services, serviceID)
			if service == nil || service.DurationMinutes <= 0 || serviceID == "" || staffID == "" || mode != booking.StaffSelectionSpecific ||
				strings.TrimSpace(ref.ServiceID) != serviceID || guestReference == "" || ref.Quantity != 1 ||
				strings.TrimSpace(ref.AvailabilityQuoteID) == "" || len(strings.TrimSpace(ref.SlotFingerprint)) != 64 ||
				ref.RequestedStartTime.IsZero() || !ref.RequestedEndTime.After(ref.RequestedStartTime) ||
				!ref.RequestedStartTime.Equal(block.StartTime) || ref.RequestedEndTime.After(block.EndTime) {
				return reviewedPartyAction{}, false
			}
			key := guestReference + "\x00" + serviceID
			if expected[key] <= 0 {
				return reviewedPartyAction{}, false
			}
			expected[key]--
			result.Segments = append(result.Segments, scheduling.ActionSegment{
				ServiceID: serviceID, StaffID: staffID, StaffSelectionMode: mode,
				GuestReference: guestReference, Quantity: 1,
				RequestedStartTime: ref.RequestedStartTime, RequestedEndTime: ref.RequestedEndTime,
			})
			guests[guestReference] = struct{}{}
			if result.StartTime.IsZero() || ref.RequestedStartTime.Before(result.StartTime) {
				result.StartTime = ref.RequestedStartTime
			}
			if result.EndTime.IsZero() || ref.RequestedEndTime.After(result.EndTime) {
				result.EndTime = ref.RequestedEndTime
			}
			if latestEnd.IsZero() || ref.RequestedEndTime.After(latestEnd) {
				latestEnd = ref.RequestedEndTime
			}
		}
		if !latestEnd.Equal(block.EndTime) {
			return reviewedPartyAction{}, false
		}
	}
	for _, remaining := range expected {
		if remaining != 0 {
			return reviewedPartyAction{}, false
		}
	}
	if len(result.Segments) < 2 || len(guests) != plan.PartySize || result.StartTime.IsZero() || !result.EndTime.After(result.StartTime) {
		return reviewedPartyAction{}, false
	}
	return result, true
}

func operationTargetKey(session Session) string {
	if targetID := strings.TrimSpace(session.TargetAppointmentID); targetID != "" {
		return targetID
	}
	if session.DialogState.ManualTarget != nil {
		return "manual_target"
	}
	return ""
}

func schedulingActionSegments(session Session, services []ServiceOption) []scheduling.ActionSegment {
	bookingSegments := bookingSegmentsForCreate(session)
	if len(bookingSegments) == 0 {
		return nil
	}
	guestReferences, guestReferencesOK := partyPlanSegmentGuestReferences(session.PartyPlan, bookingSegments)
	if activePartyPlan(session.PartyPlan) && !guestReferencesOK {
		return nil
	}
	segments := make([]scheduling.ActionSegment, 0, len(bookingSegments))
	for index, segment := range bookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
		if mode == "" {
			mode = staffSelectionModeForSession(session)
		}
		item := scheduling.ActionSegment{
			ServiceID:          serviceID,
			StaffID:            strings.TrimSpace(segment.StaffID),
			StaffSelectionMode: mode,
			Quantity:           1,
		}
		if guestReferencesOK && index < len(guestReferences) {
			item.GuestReference = guestReferences[index]
		}
		if session.RequestedStartTime != nil {
			item.RequestedStartTime = *session.RequestedStartTime
			if service := serviceByID(services, serviceID); service != nil && service.DurationMinutes > 0 {
				item.RequestedEndTime = session.RequestedStartTime.Add(time.Duration(service.DurationMinutes) * time.Minute)
			}
		}
		segments = append(segments, item)
	}
	return segments
}

func schedulingPartySize(session Session, segments []scheduling.ActionSegment) int {
	if session.PartyPlan != nil && session.PartyPlan.PartySize > 0 {
		return session.PartyPlan.PartySize
	}
	if len(segments) > 0 {
		return 1
	}
	return 0
}

func applyNeutralActionResult(turn *TurnRecord, session Session, result *scheduling.ActionResult, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) bool {
	if turn == nil || result == nil {
		return false
	}
	expectedOperation := schedulingOperationForSession(session)
	if result.OperationType != expectedOperation {
		return false
	}
	turn.Update.Status = StatusCompleted
	turn.Update.BookingAction = bookingActionForSession(session)
	turn.Update.TargetAppointmentID = strings.TrimSpace(session.TargetAppointmentID)
	turn.Update.RescheduleCandidates = nil
	switch result.Kind {
	case scheduling.ActionKindPendingOwnerReview:
		if result.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual || result.PendingOwnerReview == nil || result.ConfirmedAppointment != nil || result.ExternalFallbackPending != nil || strings.TrimSpace(result.PendingOwnerReview.SchedulingRequestID) == "" {
			return false
		}
		turn.ToolMessage = "Scheduling request recorded for owner review."
		turn.Update.Outcome = OutcomeOwnerReviewPending
		turn.Update.SchedulingRequestID = strings.TrimSpace(result.PendingOwnerReview.SchedulingRequestID)
		turn.Update.BookingAttemptID = ""
		turn.Update.AppointmentID = ""
		turn.AIMessage = ownerReviewPendingReply(session, result.OperationType)
	case scheduling.ActionKindConfirmedAppointment:
		if result.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider && result.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar || result.ConfirmedAppointment == nil || result.PendingOwnerReview != nil || result.ExternalFallbackPending != nil || strings.TrimSpace(result.ConfirmedAppointment.AppointmentID) == "" {
			return false
		}
		if result.SchedulingAuthority == booking.SchedulingAuthorityManleAICalendar {
			confirmed := result.ConfirmedAppointment
			if strings.TrimSpace(confirmed.BookingAttemptID) == "" || strings.TrimSpace(confirmed.ExternalAttemptID) != "" || confirmed.ExternalAttempt != nil {
				return false
			}
			if result.OperationType == scheduling.OperationKindReschedule || result.OperationType == scheduling.OperationKindCancel {
				if !confirmedInternalLifecycleResult(session, result) {
					return false
				}
			}
			if result.OperationType == scheduling.OperationKindBook &&
				!confirmedInternalBookResult(session, result, services, cfg) {
				return false
			}
			if confirmed.Appointment != nil {
				appointment := confirmed.Appointment
				if strings.TrimSpace(appointment.ID) != strings.TrimSpace(confirmed.AppointmentID) ||
					(strings.TrimSpace(appointment.SchedulingAuthority) != "" && appointment.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar) ||
					(strings.TrimSpace(appointment.BookingAttemptID) != "" && strings.TrimSpace(appointment.BookingAttemptID) != strings.TrimSpace(confirmed.BookingAttemptID)) ||
					strings.TrimSpace(appointment.AuthorityProvider) != "" || strings.TrimSpace(appointment.POSProvider) != "" || strings.TrimSpace(appointment.POSAppointmentID) != "" {
					return false
				}
			}
		}
		turn.ToolMessage = "Scheduling authority confirmed the appointment operation."
		turn.Update.AppointmentID = strings.TrimSpace(result.ConfirmedAppointment.AppointmentID)
		if result.SchedulingAuthority == booking.SchedulingAuthorityManleAICalendar {
			turn.Update.BookingAttemptID = strings.TrimSpace(result.ConfirmedAppointment.BookingAttemptID)
		} else {
			turn.Update.BookingAttemptID = strings.TrimSpace(result.ConfirmedAppointment.ExternalAttemptID)
		}
		switch result.OperationType {
		case scheduling.OperationKindBook:
			turn.Update.Outcome = OutcomeBookingConfirmed
			if result.Replayed {
				turn.ToolMessage = "Scheduling authority recovered a historical booking result."
				turn.AIMessage = historicalBookingReplayMessage()
			} else if result.SchedulingAuthority == booking.SchedulingAuthorityManleAICalendar {
				if partyMessage, ok := confirmedInternalPartyMessage(session, services, staff, cfg); ok {
					turn.AIMessage = partyMessage
				} else {
					turn.AIMessage = confirmedMessage(session, services, staff, cfg)
				}
			} else {
				turn.AIMessage = confirmedMessage(session, services, staff, cfg)
			}
		case scheduling.OperationKindReschedule:
			turn.Update.Outcome = OutcomeBookingRescheduled
			if result.Replayed {
				turn.ToolMessage = "Scheduling authority recovered a historical reschedule result."
				turn.AIMessage = historicalRescheduleReplayMessage()
			} else {
				turn.AIMessage = rescheduledMessage(session, cfg)
			}
		case scheduling.OperationKindCancel:
			turn.Update.Outcome = OutcomeBookingCancelled
			turn.AIMessage = cancelledMessage(session, cfg)
		default:
			return false
		}
	case scheduling.ActionKindExternalFallbackPending:
		if result.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider || result.ExternalFallbackPending == nil || result.PendingOwnerReview != nil || result.ConfirmedAppointment != nil || strings.TrimSpace(result.ExternalFallbackPending.ExternalAttemptID) == "" {
			return false
		}
		turn.ToolMessage = "External scheduling provider returned fallback pending."
		turn.Update.Outcome = OutcomeBookingFallbackPending
		turn.Update.BookingAttemptID = strings.TrimSpace(result.ExternalFallbackPending.ExternalAttemptID)
		turn.Update.AppointmentID = ""
		switch result.OperationType {
		case scheduling.OperationKindBook:
			turn.AIMessage = bookingFallbackReply()
		case scheduling.OperationKindReschedule:
			turn.AIMessage = rescheduleFallbackReply()
		case scheduling.OperationKindCancel:
			turn.AIMessage = cancelFallbackReply()
		default:
			return false
		}
	default:
		return false
	}
	return true
}

func confirmedInternalBookResult(
	session Session,
	result *scheduling.ActionResult,
	services []ServiceOption,
	cfg *RuntimeConfig,
) bool {
	if result == nil || result.ConfirmedAppointment == nil ||
		result.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar ||
		result.OperationType != scheduling.OperationKindBook ||
		result.TargetAuthorityAppointmentVersion != 0 ||
		result.AuthorityAppointmentVersion != 1 {
		return false
	}
	confirmed := result.ConfirmedAppointment
	if confirmed.AppointmentStatus != booking.StatusConfirmed ||
		confirmed.ActiveChildCount < 1 ||
		confirmed.ActiveChildCount != len(confirmed.Children) {
		return false
	}
	action, ok := schedulingActionRequest(session, services, cfg, scheduling.OperationKindBook)
	if !ok || len(action.Segments) != len(confirmed.Children) {
		return false
	}
	for index, expected := range action.Segments {
		if !confirmedLifecycleChildMatchesAction(confirmed.Children[index], expected) {
			return false
		}
	}
	return true
}

func confirmedInternalLifecycleResult(session Session, result *scheduling.ActionResult) bool {
	if result == nil || result.ConfirmedAppointment == nil || result.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar {
		return false
	}
	candidate, ok := selectedInternalLifecycleCandidate(session)
	if !ok || result.TargetAuthorityAppointmentVersion != candidate.AuthorityAppointmentVersion ||
		result.AuthorityAppointmentVersion != candidate.AuthorityAppointmentVersion+1 ||
		strings.TrimSpace(result.ConfirmedAppointment.AppointmentID) != candidate.AppointmentID ||
		strings.TrimSpace(result.ConfirmedAppointment.BookingAttemptID) == "" ||
		strings.TrimSpace(result.ConfirmedAppointment.ExternalAttemptID) != "" || result.ConfirmedAppointment.ExternalAttempt != nil {
		return false
	}
	confirmed := result.ConfirmedAppointment
	switch result.OperationType {
	case scheduling.OperationKindReschedule:
		action, _, reviewed := reviewedInternalLifecycleAction(session)
		if !reviewed || confirmed.AppointmentStatus != booking.StatusRescheduled || confirmed.ActiveChildCount != len(action.Segments) || len(confirmed.Children) != len(action.Segments) {
			return false
		}
		for index, expected := range action.Segments {
			if !confirmedLifecycleChildMatchesAction(confirmed.Children[index], expected) {
				return false
			}
		}
	case scheduling.OperationKindCancel:
		if confirmed.AppointmentStatus != booking.StatusCancelled || confirmed.ActiveChildCount != 0 || len(confirmed.Children) != len(candidate.Segments) {
			return false
		}
		for index, child := range confirmed.Children {
			target := candidate.Segments[index]
			if strings.TrimSpace(child.AppointmentServiceID) == "" || strings.TrimSpace(child.ServiceID) != strings.TrimSpace(target.ServiceID) ||
				strings.TrimSpace(child.GuestReference) != strings.TrimSpace(target.GuestReference) || child.Quantity != target.Quantity ||
				strings.TrimSpace(child.StaffID) == "" || normalizeConversationStaffSelectionMode(child.StaffSelectionMode) != booking.StaffSelectionSpecific ||
				child.ScheduledStartTime.IsZero() || !child.ScheduledEndTime.After(child.ScheduledStartTime) ||
				child.OccupiedStartTime.IsZero() || child.OccupiedEndTime.IsZero() || child.OccupiedStartTime.After(child.ScheduledStartTime) || child.OccupiedEndTime.Before(child.ScheduledEndTime) {
				return false
			}
		}
	default:
		return false
	}
	return true
}

func confirmedLifecycleChildMatchesAction(child scheduling.ConfirmedAppointmentSegment, expected scheduling.ActionSegment) bool {
	if strings.TrimSpace(child.AppointmentServiceID) == "" || strings.TrimSpace(child.ServiceID) != expected.ServiceID ||
		strings.TrimSpace(child.StaffID) != expected.StaffID || normalizeConversationStaffSelectionMode(child.StaffSelectionMode) != expected.StaffSelectionMode ||
		strings.TrimSpace(child.GuestReference) != expected.GuestReference || child.Quantity != expected.Quantity ||
		!child.ScheduledStartTime.Equal(expected.RequestedStartTime) || !child.ScheduledEndTime.Equal(expected.RequestedEndTime) ||
		child.OccupiedStartTime.IsZero() || child.OccupiedEndTime.IsZero() || child.OccupiedStartTime.After(child.ScheduledStartTime) || child.OccupiedEndTime.Before(child.ScheduledEndTime) {
		return false
	}
	for _, allocation := range child.ResourceAllocations {
		if strings.TrimSpace(allocation.ResourcePoolID) == "" || allocation.UnitsAllocated <= 0 {
			return false
		}
	}
	return true
}

func schedulingOperationForSession(session Session) scheduling.OperationKind {
	switch bookingActionForSession(session) {
	case BookingActionReschedule:
		return scheduling.OperationKindReschedule
	case BookingActionCancel:
		return scheduling.OperationKindCancel
	default:
		return scheduling.OperationKindBook
	}
}

func ownerReviewPendingReply(session Session, operation scheduling.OperationKind) string {
	switch operation {
	case scheduling.OperationKindBook:
		if session.PartyPlan != nil && session.PartyPlan.PartySize > 1 {
			return "I recorded one group appointment request with all of the group details for the owner to review. This is not a confirmed group appointment. Thank you, goodbye."
		}
		return "I recorded your appointment request for the owner to review. This is not a confirmed appointment. Thank you, goodbye."
	case scheduling.OperationKindReschedule:
		return "I recorded your reschedule request for the owner to review. The original appointment has not been changed. Thank you, goodbye."
	case scheduling.OperationKindCancel:
		return "I recorded your cancellation request for the owner to review. The original appointment has not been changed. Thank you, goodbye."
	default:
		return "I recorded your request for the owner to review. No appointment change is confirmed. Thank you, goodbye."
	}
}

func (s *Service) tryPartySplitBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, option PartySplitOption, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if len(option.Blocks) == 0 {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(0, 0), services, staff, cfg)
	}
	totalSegments := len(partySplitOptionSegments(option))
	if totalSegments == 0 {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(0, 0), services, staff, cfg)
	}
	if option.RequiresDateConsent && !option.DateConsentConfirmed {
		turn.AIMessage = partySplitDateConsentPrompt(session, services, cfg)
		turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
			"booking_policy": "party_split_date_consent_required",
		})
		s.applyReplyGenerator(ctx, &turn, session, services, cfg, "party_split_date_consent", "party_split_date_consent", knowledge)
		finalizeTurnMetadata(&turn, turn.Session, session, "party_split_date_consent", "party_split_date_consent", "party_split_date_consent")
		return s.store.SaveTurn(ctx, turn)
	}
	if strings.TrimSpace(session.CustomerName) == "" || strings.TrimSpace(session.CustomerPhone) == "" {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonCustomerDetailsUnavailable, partySplitBookingFailureReply(0, totalSegments), services, staff, cfg)
	}
	freshOption, fresh, err := s.refreshPartySplitOptionProofs(ctx, ownerUserID, turn.SalonID, session, option, services, cfg)
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(0, totalSegments), services, staff, cfg)
	}
	if !fresh {
		return s.reofferChangedPartySplitAvailability(ctx, ownerUserID, turn, session, services, staff, cfg, knowledge)
	}
	option = freshOption

	successfulAttempts := []*booking.BookingAttempt{}
	successfulAppointmentIDs := []string{}
	for blockIndex, block := range option.Blocks {
		if block.StartTime.IsZero() || len(block.Segments) == 0 {
			rollback := s.rollbackPartySplitBookings(ctx, ownerUserID, turn.SalonID, session, successfulAppointmentIDs)
			turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, partySplitBookingFailureMetadata(len(successfulAttempts), totalSegments, rollback))
			return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(len(successfulAttempts), totalSegments), services, staff, cfg)
		}
		for segmentIndex, segment := range block.Segments {
			if strings.TrimSpace(segment.ServiceID) == "" {
				rollback := s.rollbackPartySplitBookings(ctx, ownerUserID, turn.SalonID, session, successfulAppointmentIDs)
				turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, partySplitBookingFailureMetadata(len(successfulAttempts), totalSegments, rollback))
				return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(len(successfulAttempts), totalSegments), services, staff, cfg)
			}
			quoteRef, ok := partySplitQuoteRef(block, segmentIndex, segment)
			if !ok {
				rollback := s.rollbackPartySplitBookings(ctx, ownerUserID, turn.SalonID, session, successfulAppointmentIDs)
				turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, partySplitBookingFailureMetadata(len(successfulAttempts), totalSegments, rollback))
				return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(len(successfulAttempts), totalSegments), services, staff, cfg)
			}
			mode := firstNonEmpty(segment.StaffSelectionMode, booking.StaffSelectionSpecific)
			req := booking.CreateBookingRequest{
				OperationKey:        conversationOperationKey(session, "split", strconv.Itoa(blockIndex), strconv.Itoa(segmentIndex)),
				AvailabilityQuoteID: quoteRef.AvailabilityQuoteID,
				SlotFingerprint:     quoteRef.SlotFingerprint,
				Source:              bookingSourceForSession(session),
				CustomerName:        session.CustomerName,
				CustomerPhone:       session.CustomerPhone,
				CustomerEmail:       session.CustomerEmail,
				ServiceID:           strings.TrimSpace(segment.ServiceID),
				StaffID:             strings.TrimSpace(segment.StaffID),
				StaffSelectionMode:  mode,
				Segments: []booking.BookingSegmentRequest{{
					ServiceID:          strings.TrimSpace(segment.ServiceID),
					StaffID:            strings.TrimSpace(segment.StaffID),
					StaffSelectionMode: mode,
				}},
				StartTime: block.StartTime,
				Notes:     bookingNotesForSplitParty(session),
			}
			startedAt := time.Now()
			attempt, err := s.bookingTool.Create(ctx, turn.SalonID, ownerUserID, req)
			recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
			if err != nil || !bookingAttemptConfirmed(attempt) {
				rollback := s.rollbackPartySplitBookings(ctx, ownerUserID, turn.SalonID, session, successfulAppointmentIDs)
				turn.ToolMessage = "Booking service did not confirm every split party appointment through POS."
				turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, partySplitBookingFailureMetadata(len(successfulAttempts), totalSegments, rollback))
				return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(len(successfulAttempts), totalSegments), services, staff, cfg)
			}
			successfulAttempts = append(successfulAttempts, attempt)
			successfulAppointmentIDs = append(successfulAppointmentIDs, attempt.Appointment.ID)
		}
	}

	bookingAttemptIDs := make([]string, 0, len(successfulAttempts))
	for _, attempt := range successfulAttempts {
		bookingAttemptIDs = append(bookingAttemptIDs, attempt.ID)
	}
	plan := clonePartyPlan(session.PartyPlan)
	if plan == nil {
		plan = &PartyPlan{}
	}
	plan.SelectedSplitOptionID = option.ID
	plan.SplitBookingAttemptIDs = append([]string(nil), bookingAttemptIDs...)
	plan.SplitAppointmentIDs = append([]string(nil), successfulAppointmentIDs...)
	session.PartyPlan = plan
	session.BookingSegments = partySplitOptionSegments(option)
	session.OfferedSlots = nil
	session.RequestedStartTime = nil

	turn.ToolMessage = "Booking service confirmed every split party appointment through POS."
	turn.AIMessage = confirmedPartySplitMessage(session, option, services, cfg)
	turn.Update.Status = StatusCompleted
	turn.Update.Outcome = OutcomeBookingConfirmed
	if len(bookingAttemptIDs) > 0 {
		turn.Update.BookingAttemptID = bookingAttemptIDs[0]
	}
	if len(successfulAppointmentIDs) > 0 {
		turn.Update.AppointmentID = successfulAppointmentIDs[0]
	}
	turn.Update.OfferedSlots = nil
	turn.Update.PartyPlan = clonePartyPlan(plan)
	turn.Update.BookingSegments = session.BookingSegments
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"booking_policy":            "party_split_pos_confirmed",
		"split_booking_count":       len(successfulAttempts),
		"split_booking_attempt_ids": bookingAttemptIDs,
		"split_appointment_ids":     successfulAppointmentIDs,
	})
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "", "", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "party_split_booking_result")
	return s.store.SaveTurn(ctx, turn)
}

func partySplitQuoteRef(block PartySplitBlock, segmentIndex int, segment booking.BookingSegmentRequest) (PartySplitQuoteRef, bool) {
	if segmentIndex < 0 || segmentIndex >= len(block.QuoteRefs) {
		return PartySplitQuoteRef{}, false
	}
	ref := block.QuoteRefs[segmentIndex]
	ref.ServiceID = strings.TrimSpace(ref.ServiceID)
	ref.AvailabilityQuoteID = strings.TrimSpace(ref.AvailabilityQuoteID)
	ref.SlotFingerprint = strings.TrimSpace(ref.SlotFingerprint)
	if ref.ServiceID == "" || ref.ServiceID != strings.TrimSpace(segment.ServiceID) || ref.AvailabilityQuoteID == "" || len(ref.SlotFingerprint) != 64 {
		return PartySplitQuoteRef{}, false
	}
	return ref, true
}

func (s *Service) reofferChangedPartySplitAvailability(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	preferredDate := strings.TrimSpace(session.RequestedDate)
	if preferredDate == "" {
		if option, ok := selectedPartySplitOption(session.PartyPlan); ok {
			if first := partySplitFirstStart(option); !first.IsZero() {
				preferredDate = first.In(timezoneLocation(timezoneFromConfig(cfg))).Format("2006-01-02")
			}
		}
	}
	options, err := s.planPartySplitOptions(ctx, ownerUserID, turn.SalonID, session, services, preferredDate, cfg)
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonGroupBooking, partySplitBookingFailureReply(0, len(bookingSegmentsForCreate(session))), services, staff, cfg)
	}
	session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
	turn.Update.Status = StatusActive
	turn.Update.Outcome = OutcomeCollecting
	turn.Update.AppointmentID = ""
	turn.Update.BookingAttemptID = ""
	turn.Update.EndSession = false
	if len(options) > 0 {
		applyPartySplitOffer(&turn, &session, services, staff, cfg, options, true)
		turn.AIMessage = "Those group openings changed before booking. " + partySplitOfferMessage(session, services, cfg)
	} else {
		plan := clonePartyPlan(session.PartyPlan)
		if plan == nil {
			plan = &PartyPlan{}
		}
		plan.SplitOptions = nil
		plan.SelectedSplitOptionID = ""
		plan.SplitBookingAttemptIDs = nil
		plan.SplitAppointmentIDs = nil
		session.PartyPlan = plan
		session.RequestedStartTime = nil
		clearSelectedAvailabilityQuote(&session)
		session.OfferedSlots = nil
		syncTurnUpdate(&turn, session, services, staff, cfg)
		turn.ToolMessage = "Fresh split availability no longer contained every selected child slot."
		turn.AIMessage = "Those group openings changed before booking, and I do not see another safe split option. What other time or day works?"
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"booking_result":      "availability_refresh_required",
		"appointment_created": false,
	})
	turn.ReplyPolicy = ReplyPolicyOperationalFact
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "requested_time", "requested_time", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "party_split_availability_changed_before_booking")
	return s.store.SaveTurn(ctx, turn)
}

type partySplitRollbackResult struct {
	Attempted int
	Cancelled int
	Failed    int
}

func (s *Service) rollbackPartySplitBookings(ctx context.Context, ownerUserID string, salonID string, session Session, appointmentIDs []string) partySplitRollbackResult {
	result := partySplitRollbackResult{Attempted: len(appointmentIDs)}
	if s.bookingTool == nil {
		result.Failed = len(appointmentIDs)
		return result
	}
	for rollbackIndex, appointmentID := range appointmentIDs {
		appointmentID = strings.TrimSpace(appointmentID)
		if appointmentID == "" {
			result.Failed++
			continue
		}
		startedAt := time.Now()
		_, fallback, err := s.bookingTool.Cancel(ctx, salonID, ownerUserID, appointmentID, booking.CancelRequest{
			OperationKey: conversationOperationKey(session, "split_rollback", strconv.Itoa(rollbackIndex), appointmentID),
			Reason:       "Split party booking rollback after partial POS failure.",
			Source:       bookingSourceForSession(session),
		})
		recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
		if err != nil || fallback != nil {
			result.Failed++
			continue
		}
		result.Cancelled++
	}
	return result
}

func bookingAttemptConfirmed(attempt *booking.BookingAttempt) bool {
	return attempt != nil &&
		attempt.Status == booking.StatusConfirmed &&
		strings.TrimSpace(attempt.POSBookingID) != "" &&
		attempt.Appointment != nil &&
		strings.TrimSpace(attempt.Appointment.ID) != ""
}

func partySplitBookingFailureMetadata(successful int, total int, rollback partySplitRollbackResult) map[string]any {
	return map[string]any{
		"booking_policy":              "party_split_partial_failure",
		"split_booking_success_count": successful,
		"split_booking_total_count":   total,
		"rollback_attempted_count":    rollback.Attempted,
		"rollback_cancelled_count":    rollback.Cancelled,
		"rollback_failed_count":       rollback.Failed,
	}
}

func partySplitBookingFailureReply(successful int, total int) string {
	if successful > 0 {
		return "I could not complete every appointment for the group through the booking system. The owner needs to review this request. This is not a confirmed group appointment."
	}
	if total > 0 {
		return "I could not complete the group appointment through the booking system. The owner needs to review this request. This is not a confirmed group appointment."
	}
	return "I could not complete the group appointment safely. The owner needs to review this request. This is not a confirmed group appointment."
}

func bookingNotesForSplitParty(session Session) string {
	base := bookingNotesForSession(session)
	if strings.TrimSpace(base) == "" {
		return "Split party booking request."
	}
	return strings.TrimSpace(base) + " Split party booking request."
}

func confirmedPartySplitMessage(session Session, option PartySplitOption, services []ServiceOption, cfg *RuntimeConfig) string {
	salon := salonName(cfg)
	loc := timezoneLocation(timezoneFromConfig(cfg))
	parts := []string{}
	if salon != "" {
		parts = append(parts, "You're confirmed with "+salon+" for the group")
	} else {
		parts = append(parts, "You're confirmed for the group")
	}
	date, singleDate := partySplitOptionSingleDate(option, loc)
	if !singleDate {
		date = ""
	}
	if date != "" {
		parts[0] += " on " + date
	}
	details := make([]string, 0, len(option.Blocks))
	for _, block := range option.Blocks {
		details = append(details, partySplitBlockSpeech(block, services, loc, !singleDate))
	}
	message := strings.Join(parts, " ") + ": " + joinHumanList(details) + "."
	if name := strings.TrimSpace(session.CustomerName); name != "" {
		message += " The appointments are under " + name + "."
	}
	message += " Thank you, goodbye."
	return message
}

func confirmedInternalPartyMessage(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (string, bool) {
	action, ok := reviewedInternalPartyAction(session, services)
	if !ok {
		return "", false
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	details := make([]string, 0, len(action.Segments))
	for _, segment := range action.Segments {
		serviceLabel := serviceName(segment.ServiceID, services, "service")
		staffLabel := staffName(segment.StaffID, staff, "technician")
		when := segment.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
		details = append(details, serviceLabel+" with "+staffLabel+" on "+when)
	}
	prefix := "You're confirmed for one group appointment"
	if salon := salonName(cfg); salon != "" {
		prefix = "You're confirmed with " + salon + " for one group appointment"
	}
	message := prefix + ": " + joinHumanList(details) + "."
	if name := strings.TrimSpace(session.CustomerName); name != "" {
		message += " The group appointment is under " + name + "."
	}
	message += " Thank you, goodbye."
	return message, true
}

func (s *Service) tryReschedule(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	if s.schedulingTool != nil {
		return s.tryNeutralReschedule(ctx, ownerUserID, turn, session, services, staff, cfg)
	}
	if s.bookingTool == nil || strings.TrimSpace(session.TargetAppointmentID) == "" || session.RequestedStartTime == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	availabilityResult, fresh, err := s.refreshSelectedAvailabilityProof(ctx, ownerUserID, turn.SalonID, &session, services, cfg)
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	if !fresh {
		session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
		applyAvailabilityOffer(&turn, &session, services, staff, cfg, availabilityResult, true)
		turn.AIMessage = "That opening changed before I could reschedule it. " + turn.AIMessage
		finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "availability_changed_before_reschedule")
		return s.store.SaveTurn(ctx, turn)
	}
	syncTurnUpdate(&turn, session, services, staff, cfg)
	startedAt := time.Now()
	appointment, fallback, err := s.bookingTool.Reschedule(ctx, turn.SalonID, ownerUserID, session.TargetAppointmentID, booking.RescheduleRequest{
		OperationKey:        conversationOperationKey(session, booking.BookingActionReschedule, session.TargetAppointmentID),
		AvailabilityQuoteID: strings.TrimSpace(session.AvailabilityQuoteID),
		SlotFingerprint:     strings.TrimSpace(session.SlotFingerprint),
		Source:              bookingSourceForSession(session),
		StartTime:           *session.RequestedStartTime,
		StaffID:             session.StaffID,
		Notes:               "AI receptionist reschedule request.",
	})
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}

	toolMessage := "Booking service returned reschedule fallback pending."
	outcome := OutcomeBookingFallbackPending
	status := StatusCompleted
	aiMessage := rescheduleFallbackReply()
	bookingAttemptID := ""
	appointmentID := ""
	if fallback != nil {
		bookingAttemptID = fallback.ID
	}
	if appointment != nil && appointment.Status == booking.StatusRescheduled && appointment.POSAppointmentID != "" {
		toolMessage = "Booking service rescheduled appointment through POS."
		outcome = OutcomeBookingRescheduled
		aiMessage = rescheduledMessage(session, cfg)
		appointmentID = appointment.ID
	} else if fallback == nil {
		toolMessage = "Booking service returned no reschedule result."
	}

	turn.ToolMessage = toolMessage
	turn.AIMessage = aiMessage
	turn.Update.Status = status
	turn.Update.Outcome = outcome
	turn.Update.BookingAction = BookingActionReschedule
	turn.Update.TargetAppointmentID = session.TargetAppointmentID
	turn.Update.RescheduleCandidates = nil
	turn.Update.BookingAttemptID = bookingAttemptID
	turn.Update.AppointmentID = appointmentID
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "reschedule_result")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) tryCancel(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	if s.schedulingTool != nil {
		return s.tryNeutralCancel(ctx, ownerUserID, turn, session, services, staff, cfg)
	}
	if s.bookingTool == nil || strings.TrimSpace(session.TargetAppointmentID) == "" {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}
	startedAt := time.Now()
	appointment, fallback, err := s.bookingTool.Cancel(ctx, turn.SalonID, ownerUserID, session.TargetAppointmentID, booking.CancelRequest{
		OperationKey: conversationOperationKey(session, booking.BookingActionCancel, session.TargetAppointmentID),
		Source:       bookingSourceForSession(session),
		Reason:       "AI receptionist cancellation request.",
	})
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}

	toolMessage := "Booking service returned cancellation fallback pending."
	outcome := OutcomeBookingFallbackPending
	status := StatusCompleted
	aiMessage := cancelFallbackReply()
	bookingAttemptID := ""
	appointmentID := ""
	if fallback != nil {
		bookingAttemptID = fallback.ID
	}
	if appointment != nil && appointment.Status == booking.StatusCancelled && strings.TrimSpace(appointment.POSAppointmentID) != "" {
		toolMessage = "Booking service cancelled appointment through POS."
		outcome = OutcomeBookingCancelled
		aiMessage = cancelledMessage(session, cfg)
		appointmentID = appointment.ID
	} else if fallback == nil {
		toolMessage = "Booking service returned no cancellation result."
	}

	turn.ToolMessage = toolMessage
	turn.AIMessage = aiMessage
	turn.Update.Status = status
	turn.Update.Outcome = outcome
	turn.Update.BookingAction = BookingActionCancel
	turn.Update.TargetAppointmentID = session.TargetAppointmentID
	turn.Update.RescheduleCandidates = nil
	turn.Update.BookingAttemptID = bookingAttemptID
	turn.Update.AppointmentID = appointmentID
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "cancel_result")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) tryNeutralReschedule(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	if cfg == nil || configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this reschedule request, and the appointment has not changed.", services, staff, cfg)
	}
	if session.RequestedStartTime == nil || !sessionHasSchedulingTarget(session) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	_, _, reviewedLifecycle := reviewedInternalLifecycleAction(session)
	if !reviewedLifecycle {
		availabilityResult, fresh, err := s.refreshSelectedAvailabilityProof(ctx, ownerUserID, turn.SalonID, &session, services, cfg)
		if err != nil {
			return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
		}
		if !fresh {
			session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
			applyAvailabilityOffer(&turn, &session, services, staff, cfg, availabilityResult, true)
			turn.AIMessage = "That opening changed before I could reschedule it. " + turn.AIMessage
			finalizeTurnMetadata(&turn, turn.Session, session, "requested_time", "requested_time", "availability_changed_before_reschedule")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	req, ok := schedulingActionRequest(session, services, cfg, scheduling.OperationKindReschedule)
	if !ok {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	syncTurnUpdate(&turn, session, services, staff, cfg)
	startedAt := time.Now()
	stampSchedulingReviewFence(&session, cfg)
	result, err := s.schedulingTool.ExecuteConversationAction(ctx, turn.SalonID, ownerUserID, conversationPolicyFenceForExecution(session, cfg), req)
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil {
		if configuredConversationBookingMode(cfg) == scheduling.BookingModeConfirmedBooking && reviewedLifecycle && (errors.Is(err, booking.ErrAvailabilityQuoteStale) || errors.Is(err, booking.ErrOperationConflict)) {
			return s.reofferInternalLifecycleAfterConflict(ctx, ownerUserID, turn, session, services, staff, cfg, scheduling.OperationKindReschedule)
		}
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	if !applyNeutralActionResult(&turn, session, result, services, staff, cfg) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	clearInternalLifecyclePending(&session)
	turn.Update.DialogState = cloneDialogState(session.DialogState)
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "reschedule_result")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) tryNeutralCancel(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	if cfg == nil || configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this cancellation request, and the appointment is not cancelled.", services, staff, cfg)
	}
	if !sessionHasSchedulingTarget(session) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}
	req, ok := schedulingActionRequest(session, services, cfg, scheduling.OperationKindCancel)
	if !ok {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}
	startedAt := time.Now()
	stampSchedulingReviewFence(&session, cfg)
	result, err := s.schedulingTool.ExecuteConversationAction(ctx, turn.SalonID, ownerUserID, conversationPolicyFenceForExecution(session, cfg), req)
	recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
	if err != nil {
		if _, internalLifecycle := selectedInternalLifecycleCandidate(session); configuredConversationBookingMode(cfg) == scheduling.BookingModeConfirmedBooking && internalLifecycle && errors.Is(err, booking.ErrOperationConflict) {
			return s.reofferInternalLifecycleAfterConflict(ctx, ownerUserID, turn, session, services, staff, cfg, scheduling.OperationKindCancel)
		}
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}
	if !applyNeutralActionResult(&turn, session, result, services, staff, cfg) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}
	clearInternalLifecyclePending(&session)
	turn.Update.DialogState = cloneDialogState(session.DialogState)
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "cancel_result")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) reofferInternalLifecycleAfterConflict(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, operation scheduling.OperationKind) (*Session, error) {
	targetID := strings.TrimSpace(session.TargetAppointmentID)
	preferredDate := strings.TrimSpace(session.RequestedDate)
	if option, ok := selectedPartySplitOption(session.PartyPlan); ok {
		if first := partySplitFirstStart(option); !first.IsZero() {
			preferredDate = first.In(timezoneLocation(timezoneFromConfig(cfg))).Format("2006-01-02")
		}
	}
	items, err := s.bookingTool.RescheduleCandidates(ctx, turn.SalonID, ownerUserID, booking.RescheduleLookupRequest{
		CustomerName: session.CustomerName, CustomerPhone: session.CustomerPhone, Limit: 10,
	})
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, "I could not safely refresh that appointment, so I did not change it. The owner needs to review it.", services, staff, cfg)
	}
	candidates := rescheduleCandidatesFromAppointments(items)
	var refreshed *RescheduleCandidate
	for index := range candidates {
		if candidates[index].AppointmentID == targetID && validInternalLifecycleCandidate(candidates[index]) {
			refreshed = &candidates[index]
			break
		}
	}
	if refreshed == nil {
		session.RescheduleCandidates = candidates
		clearInternalLifecyclePending(&session)
		clearSelectedAvailabilityQuote(&session)
		turn.ToolMessage = "The internal appointment is no longer an active lifecycle target."
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, "That appointment is no longer active for this change, so I did not confirm any update. The owner needs to review it.", services, staff, cfg)
	}
	session.RescheduleCandidates = candidates
	applyRescheduleCandidate(&session, *refreshed)
	clearInternalLifecyclePending(&session)
	clearSelectedAvailabilityQuote(&session)
	turn.Update.Status = StatusActive
	turn.Update.Outcome = OutcomeCollecting
	turn.Update.EndSession = false
	turn.Update.AppointmentID = ""
	turn.Update.BookingAttemptID = ""
	if operation == scheduling.OperationKindCancel {
		session.BookingAction = BookingActionCancel
		applyCancelCandidate(&session, *refreshed, timezoneLocation(timezoneFromConfig(cfg)))
		setInternalLifecyclePending(&session, PendingInternalCancelReason, "")
		syncTurnUpdate(&turn, session, services, staff, cfg)
		turn.ToolMessage = "The appointment version changed before cancellation; no cancellation was committed."
		turn.AIMessage = "That appointment changed before I could cancel it, so it is still active. " + internalLifecycleCancelReasonPrompt(*refreshed, timezoneLocation(timezoneFromConfig(cfg)))
		finalizeTurnMetadata(&turn, turn.Session, session, ExpectedInputCancellationReason, ExpectedInputCancellationReason, "internal_cancel_target_changed")
		return s.store.SaveTurn(ctx, turn)
	}
	session.BookingAction = BookingActionReschedule
	if preferredDate == "" {
		preferredDate = refreshed.StartTime.In(timezoneLocation(timezoneFromConfig(cfg))).Format("2006-01-02")
	}
	if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &session, services, staff, preferredDate, true, cfg); err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, "The appointment changed and I could not verify a new complete replacement, so I did not reschedule it. The owner needs to review it.", services, staff, cfg)
	}
	turn.AIMessage = "The appointment changed before I could reschedule it, so nothing was changed. " + turn.AIMessage
	finalizeTurnMetadata(&turn, turn.Session, session, ExpectedInputOfferedSlot, ExpectedInputOfferedSlot, "internal_reschedule_target_changed")
	return s.store.SaveTurn(ctx, turn)
}

func sessionHasSchedulingTarget(session Session) bool {
	if strings.TrimSpace(session.TargetAppointmentID) != "" {
		return true
	}
	return session.DialogState.ManualTarget != nil && strings.TrimSpace(session.DialogState.ManualTarget.Description) != ""
}

func conversationOperationKey(session Session, operation string, suffixes ...string) string {
	parts := []string{"conversation", strings.TrimSpace(session.ID), strings.TrimSpace(operation)}
	for _, suffix := range suffixes {
		if value := strings.TrimSpace(suffix); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ":")
}

func bookingFallbackReply() string {
	return "I couldn't confirm the appointment. The owner needs to review the request. This is not a confirmed appointment."
}

func bookingErrorReply() string {
	return "I couldn't complete the booking right now, so the owner needs to review it. This is not a confirmed appointment."
}

func rescheduleFallbackReply() string {
	return "I couldn't reschedule the appointment. The owner needs to review the request. The original appointment has not been changed."
}

func rescheduleErrorReply() string {
	return "I couldn't complete the reschedule right now, so the owner needs to review it. The original appointment has not been changed."
}

func cancelFallbackReply() string {
	return "I couldn't cancel the appointment. The owner needs to review the request. The original appointment has not been changed."
}

func cancelErrorReply() string {
	return "I couldn't complete the cancellation right now, so the owner needs to review it. The original appointment has not been changed."
}

func rescheduledMessage(session Session, cfg *RuntimeConfig) string {
	loc := timezoneLocation("")
	if cfg != nil {
		loc = timezoneLocation(cfg.Timezone)
	}
	when := ""
	if session.RequestedStartTime != nil {
		when = session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	}
	salon := salonName(cfg)
	prefix := "Your appointment has been rescheduled"
	if salon != "" {
		prefix += " with " + salon
	}
	if when != "" {
		return prefix + " to " + when + ". Thank you, goodbye."
	}
	return prefix + ". Thank you, goodbye."
}

func historicalRescheduleReplayMessage() string {
	return "I recovered the prior result: that reschedule succeeded at that time. The appointment's current status may have changed since then, so this is not confirmation of its current status."
}

func historicalBookingReplayMessage() string {
	return "I recovered the prior result: that booking succeeded at that time. The appointment's current status may have changed since then, so this is not confirmation of its current status."
}

func bookingServiceSelectionConsistent(session Session) bool {
	if len(session.BookingSegments) == 0 {
		return false
	}
	primaryServiceID := strings.TrimSpace(session.ServiceID)
	if primaryServiceID == "" || primaryServiceID != strings.TrimSpace(session.BookingSegments[0].ServiceID) {
		return false
	}
	for _, segment := range session.BookingSegments {
		segmentServiceID := strings.TrimSpace(segment.ServiceID)
		if segmentServiceID == "" || strings.TrimSpace(segment.StaffID) == "" {
			return false
		}
		mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
		if mode != booking.StaffSelectionSpecific && mode != booking.StaffSelectionAnyone {
			return false
		}
	}
	return true
}

func (s *Service) applyReplyGenerator(ctx context.Context, turn *TurnRecord, session Session, services []ServiceOption, cfg *RuntimeConfig, missing string, nextRequired string, knowledge []KnowledgeSnippet) {
	if s.replyGenerator == nil || turn == nil || strings.TrimSpace(turn.AIMessage) == "" {
		return
	}
	safeReply := strings.TrimSpace(turn.AIMessage)
	if turn.Update.EndSession {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"safe_reply":          safeReply,
			"llm_guardrail":       "skipped_terminal_reply",
			"reply_source":        "safe_reply",
			"next_required_field": nextRequired,
		})
		return
	}
	if isRealtimePhoneTurn(turn, session) {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"safe_reply":          safeReply,
			"llm_guardrail":       "skipped_realtime_latency_budget",
			"reply_source":        "safe_reply",
			"next_required_field": nextRequired,
		})
		return
	}
	if turn.ReplyPolicy != ReplyPolicyStyleOnly {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"safe_reply":          safeReply,
			"llm_guardrail":       "skipped_operational_fact",
			"reply_policy":        ReplyPolicyOperationalFact,
			"reply_source":        "safe_reply",
			"next_required_field": nextRequired,
		})
		return
	}
	result, err := s.replyGenerator.GenerateReply(ctx, ReplyGenerationRequest{
		SalonID:              turn.SalonID,
		SessionID:            session.ID,
		Channel:              session.Channel,
		Intent:               turn.Update.Intent,
		Outcome:              turn.Update.Outcome,
		CustomerMessage:      turn.CustomerMessage,
		SafeReply:            turn.AIMessage,
		SalonName:            salonName(cfg),
		AITone:               aiTone(cfg),
		BookingConfirmed:     turn.Update.Outcome == OutcomeBookingConfirmed && turn.Update.BookingAttemptID != "" && turn.Update.AppointmentID != "",
		FallbackOrHandoff:    turn.Update.Outcome == OutcomeBookingFallbackPending || turn.Update.Outcome == OutcomeAIDisabled || turn.Update.Outcome == OutcomeHandoffRequested,
		MissingBookingField:  missing,
		KnownBookingFields:   knownBookingFields(session),
		NextRequiredField:    nextRequired,
		SelectedServiceNames: selectedServiceNames(session, services),
		Summary:              turn.Update.Summary,
		KnowledgeContext:     formatKnowledgeContext(knowledge),
		ReplyPolicy:          turn.ReplyPolicy,
	})
	if err != nil {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"safe_reply":          safeReply,
			"llm_guardrail":       "fallback_to_safe_reply",
			"llm_error":           "reply generation failed",
			"reply_source":        "safe_reply",
			"next_required_field": nextRequired,
		})
		return
	}
	if message := strings.TrimSpace(result.Message); message != "" {
		if rejectUnavailableRequestedTimeRewrite(safeReply, message) {
			turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
				"safe_reply":          safeReply,
				"llm_reply":           message,
				"llm_confidence":      result.Confidence,
				"llm_handoff":         result.Handoff,
				"llm_reason":          result.Reason,
				"llm_guardrail":       "rejected_unavailable_time_omission",
				"reply_source":        "safe_reply",
				"next_required_field": nextRequired,
			})
			return
		}
		if rejectRescheduleReplyRewrite(turn, nextRequired, message) {
			turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
				"safe_reply":          safeReply,
				"llm_reply":           message,
				"llm_confidence":      result.Confidence,
				"llm_handoff":         result.Handoff,
				"llm_reason":          result.Reason,
				"llm_guardrail":       "rejected_reschedule_stage_flip",
				"reply_source":        "safe_reply",
				"next_required_field": nextRequired,
			})
			return
		}
		turn.AIMessage = message
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"safe_reply":          safeReply,
		"llm_reply":           strings.TrimSpace(result.Message),
		"llm_confidence":      result.Confidence,
		"llm_handoff":         result.Handoff,
		"llm_reason":          result.Reason,
		"llm_guardrail":       "accepted",
		"reply_source":        "llm_rewrite",
		"next_required_field": nextRequired,
	})
}

func isRealtimePhoneTurn(turn *TurnRecord, session Session) bool {
	if turn == nil || session.Channel != ChannelPhone {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(turn.EventKey)), ":realtimetranscriptid:")
}

func rejectUnavailableRequestedTimeRewrite(safeReply string, message string) bool {
	safe := normalizeLooseText(safeReply)
	if !strings.Contains(safe, "time is not available") && !strings.Contains(safe, "not available at") {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return true
	}
	signals := []string{
		"not available",
		"unavailable",
		"no opening at that time",
		"no openings at that time",
		"do not have that time",
		"don t have that time",
		"that time is taken",
		"that time is booked",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return false
		}
	}
	return true
}

func rejectRescheduleReplyRewrite(turn *TurnRecord, nextRequired string, message string) bool {
	if turn == nil || nextRequired != "target_appointment" || turn.Update.BookingAction != BookingActionReschedule {
		return false
	}
	if strings.TrimSpace(turn.Update.TargetAppointmentID) != "" {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	stageFlipSignals := []string{
		"new time",
		"new day",
		"new date",
		"what time",
		"what day",
		"schedule your",
		"reschedule your",
	}
	for _, signal := range stageFlipSignals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
}

func (s *Service) saveHandoffTurn(ctx context.Context, turn TurnRecord, session Session, reason string, reply string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	closeConsultationForWorkflow(&session, reason, true)
	turn.Update.DialogState = cloneDialogState(session.DialogState)
	summary := summaryFor(session, services, staff, cfg)
	turn.AIMessage = reply
	turn.Update.Status = StatusHandoff
	turn.Update.Outcome = OutcomeHandoffRequested
	if reason == HandoffReasonAIBookingDisabled {
		turn.Update.Outcome = OutcomeAIDisabled
	}
	turn.Update.EndSession = true
	turn.Update.Summary = summary
	turn.Handoff = &HandoffRecord{
		Reason:        reason,
		CustomerName:  session.CustomerName,
		CustomerPhone: session.CustomerPhone,
		Summary:       summary,
	}
	if reason == HandoffReasonGroupBooking {
		turn.PartyRequest = partyRequestRecordFromSession(turn, session, services, cfg, summary)
	}
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "handoff")
	return s.store.SaveTurn(ctx, turn)
}

func partyRequestRecordFromSession(turn TurnRecord, session Session, services []ServiceOption, cfg *RuntimeConfig, summary string) *PartyRequestRecord {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	requestedTimeWindow := ""
	if session.RequestedStartTime != nil {
		requestedTimeWindow = session.RequestedStartTime.In(loc).Format("3:04 PM")
	}
	partySize := 0
	if session.PartyPlan != nil && session.PartyPlan.PartySize > 0 {
		partySize = session.PartyPlan.PartySize
	}
	return &PartyRequestRecord{
		EventKey:             normalizeEventKey(turn.EventKey),
		PartySize:            partySize,
		RepresentativeName:   session.CustomerName,
		RepresentativePhone:  session.CustomerPhone,
		RequestedDate:        session.RequestedDate,
		RequestedTimeWindow:  requestedTimeWindow,
		GuestServiceRequests: partyGuestServicesFromSession(session, services),
		Summary:              summary,
	}
}

func partyGuestServicesFromSession(session Session, services []ServiceOption) []PartyGuestService {
	byID := map[string]ServiceOption{}
	for _, service := range services {
		byID[strings.TrimSpace(service.ID)] = service
	}
	if session.PartyPlan != nil {
		segments := partyPlanSegments(session.PartyPlan, session)
		guestReferences, ok := partyPlanSegmentGuestReferences(session.PartyPlan, segments)
		if ok {
			items := make([]PartyGuestService, 0, len(segments))
			for index, segment := range segments {
				items = append(items, partyGuestServiceFromSegment(segment.ServiceID, guestReferences[index], 1, index+1, byID))
			}
			return items
		}
	}

	items := make([]PartyGuestService, 0, len(session.BookingSegments))
	for index, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		quantity := segment.Quantity
		if quantity < 1 {
			quantity = 1
		}
		items = append(items, partyGuestServiceFromSegment(serviceID, segment.GuestReference, quantity, index+1, byID))
	}
	if len(items) == 0 && strings.TrimSpace(session.ServiceID) != "" {
		serviceID := strings.TrimSpace(session.ServiceID)
		item := partyGuestServiceFromSegment(serviceID, "", 1, 1, byID)
		if item.ServiceName == "" {
			item.ServiceName = strings.TrimSpace(session.ServiceName)
		}
		items = append(items, item)
	}
	return items
}

func partyGuestServiceFromSegment(serviceID, guestReference string, quantity, sortOrder int, servicesByID map[string]ServiceOption) PartyGuestService {
	item := PartyGuestService{
		GuestReference: strings.TrimSpace(guestReference),
		ServiceID:      strings.TrimSpace(serviceID),
		Quantity:       quantity,
		SortOrder:      sortOrder,
	}
	if service, ok := servicesByID[item.ServiceID]; ok {
		item.ServiceName = strings.TrimSpace(service.Name)
	}
	return item
}
