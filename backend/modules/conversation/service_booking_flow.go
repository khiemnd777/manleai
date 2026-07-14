package conversation

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

func (s *Service) tryBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if nextAction := planNextConversationAction(session, missingBookingField(session)); nextAction.Kind != AssistantActionExecuteBooking {
		return s.continueAfterDraftReady(ctx, ownerUserID, turn, turn.Session, session, services, staff, cfg, knowledge)
	}
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	if option, ok := selectedPartySplitOption(session.PartyPlan); ok {
		return s.tryPartySplitBooking(ctx, ownerUserID, turn, session, option, services, staff, cfg, knowledge)
	}
	if !bookingServiceSelectionConsistent(session) {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}
	startedAt := time.Now()
	attempt, err := s.bookingTool.Create(ctx, turn.SalonID, ownerUserID, booking.CreateBookingRequest{
		OperationKey:       conversationOperationKey(session, booking.BookingActionBook),
		Source:             bookingSourceForSession(session),
		CustomerName:       session.CustomerName,
		CustomerPhone:      session.CustomerPhone,
		CustomerEmail:      session.CustomerEmail,
		ServiceID:          session.ServiceID,
		StaffID:            session.StaffID,
		StaffSelectionMode: staffSelectionModeForSession(session),
		Segments:           bookingSegmentsForCreate(session),
		StartTime:          *session.RequestedStartTime,
		Notes:              bookingNotesForSession(session),
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
			mode := firstNonEmpty(segment.StaffSelectionMode, booking.StaffSelectionSpecific)
			req := booking.CreateBookingRequest{
				OperationKey:       conversationOperationKey(session, "split", strconv.Itoa(blockIndex), strconv.Itoa(segmentIndex)),
				Source:             bookingSourceForSession(session),
				CustomerName:       session.CustomerName,
				CustomerPhone:      session.CustomerPhone,
				CustomerEmail:      session.CustomerEmail,
				ServiceID:          strings.TrimSpace(segment.ServiceID),
				StaffID:            strings.TrimSpace(segment.StaffID),
				StaffSelectionMode: mode,
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
		return "I could not complete every appointment for the group through the booking system, so I will send this to the owner to review. This is not a confirmed group appointment."
	}
	if total > 0 {
		return "I could not complete the group appointment through the booking system, so I will send this to the owner to review. This is not a confirmed group appointment."
	}
	return "I could not complete the group appointment safely, so I will send this to the owner to review. This is not a confirmed group appointment."
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

func (s *Service) tryReschedule(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	if s.bookingTool == nil || strings.TrimSpace(session.TargetAppointmentID) == "" || session.RequestedStartTime == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	startedAt := time.Now()
	appointment, fallback, err := s.bookingTool.Reschedule(ctx, turn.SalonID, ownerUserID, session.TargetAppointmentID, booking.RescheduleRequest{
		OperationKey: conversationOperationKey(session, booking.BookingActionReschedule, session.TargetAppointmentID),
		Source:       bookingSourceForSession(session),
		StartTime:    *session.RequestedStartTime,
		StaffID:      session.StaffID,
		Notes:        "AI receptionist reschedule request.",
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
	return "I couldn't confirm the appointment, so I sent the request to the owner to review. This is not a confirmed appointment."
}

func bookingErrorReply() string {
	return "I couldn't complete the booking right now, so the owner needs to review it. This is not a confirmed appointment."
}

func rescheduleFallbackReply() string {
	return "I couldn't reschedule the appointment, so I sent the request to the owner to review. The original appointment has not been changed."
}

func rescheduleErrorReply() string {
	return "I couldn't complete the reschedule right now, so the owner needs to review it. The original appointment has not been changed."
}

func cancelFallbackReply() string {
	return "I couldn't cancel the appointment, so I sent the request to the owner to review. The original appointment has not been changed."
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
			"llm_error":           err.Error(),
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
	return &PartyRequestRecord{
		EventKey:             normalizeEventKey(turn.EventKey),
		PartySize:            partySizeFromMessage(turn.CustomerMessage),
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
	items := make([]PartyGuestService, 0)
	seen := map[string]bool{}
	for _, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" || seen[serviceID] {
			continue
		}
		seen[serviceID] = true
		item := PartyGuestService{ServiceID: serviceID}
		if service, ok := byID[serviceID]; ok {
			item.ServiceName = service.Name
		}
		items = append(items, item)
	}
	if len(items) == 0 && strings.TrimSpace(session.ServiceID) != "" {
		serviceID := strings.TrimSpace(session.ServiceID)
		item := PartyGuestService{ServiceID: serviceID}
		if service, ok := byID[serviceID]; ok {
			item.ServiceName = service.Name
		} else if strings.TrimSpace(session.ServiceName) != "" {
			item.ServiceName = session.ServiceName
		}
		items = append(items, item)
	}
	return items
}
