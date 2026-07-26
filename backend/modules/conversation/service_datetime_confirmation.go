package conversation

import (
	"context"
	"strings"
	"time"
)

const pendingOfferedSlotDateTimeCorrection = "offered_slot_datetime_correction"

func (s *Service) guardOfferedSlotDateTimeCorrection(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	session Session,
	message string,
	eventKey string,
	services []ServiceOption,
	aliases []ServiceAlias,
	categoryAliases []ServiceCategoryAlias,
	staff []StaffOption,
	cfg *RuntimeConfig,
) (bool, *Session, error) {
	if bookingActionForSession(session) != BookingActionBook || len(session.OfferedSlots) == 0 {
		return false, nil, nil
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	if selected := selectOfferedSlot(message, session.OfferedSlots, loc); selected != nil {
		return false, nil, nil
	}
	proposed := cloneSessionForTurn(session)
	applyExtraction(&proposed, message, services, aliases, categoryAliases, staff, loc, s.now)
	if !sameStringSlices(selectedServiceIDs(session), selectedServiceIDs(proposed)) || !dateTimeSelectionChanged(session, proposed) {
		return false, nil, nil
	}
	next := cloneSessionForTurn(session)
	state := normalizedDialogState(next.DialogState)
	state.Pending = &PendingConversationAct{
		Kind:             ConversationActSet,
		ProposedDate:     strings.TrimSpace(proposed.RequestedDate),
		ProposedStartISO: formatOptionalTime(proposed.RequestedStartTime),
		PromptKey:        pendingOfferedSlotDateTimeCorrection,
	}
	state.LastPromptKey = pendingOfferedSlotDateTimeCorrection
	next.DialogState = state
	turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	turn.AIMessage = offeredSlotDateTimeCorrectionPrompt(*state.Pending, cfg)
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_datetime_correction": true,
		"proposed_requested_date":     state.Pending.ProposedDate,
		"proposed_start_iso":          state.Pending.ProposedStartISO,
	})
	finalizeTurnMetadata(&turn, session, next, "requested_time", "requested_time", pendingOfferedSlotDateTimeCorrection)
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func (s *Service) handlePendingOfferedSlotDateTimeCorrection(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	session Session,
	message string,
	eventKey string,
	services []ServiceOption,
	staff []StaffOption,
	cfg *RuntimeConfig,
	knowledge []KnowledgeSnippet,
) (bool, *Session, error) {
	state := normalizedDialogState(session.DialogState)
	if state.Pending == nil || state.Pending.PromptKey != pendingOfferedSlotDateTimeCorrection {
		return false, nil, nil
	}
	pending := *state.Pending
	if isNegativeOnly(message) || asksAvailabilityQuestion(message) {
		next := cloneSessionForTurn(session)
		nextState := normalizedDialogState(next.DialogState)
		nextState.Pending = nil
		nextState.LastPromptKey = ""
		next.DialogState = nextState
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = formatSlotOfferForSession(next.OfferedSlots, timezoneLocation(timezoneFromConfig(cfg)), false, next, services)
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{"datetime_correction_rejected": true})
		finalizeTurnMetadata(&turn, session, next, "requested_time", "requested_time", "datetime_correction_rejected")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if !isAffirmativeOnly(message) {
		next := cloneSessionForTurn(session)
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = offeredSlotDateTimeCorrectionPrompt(pending, cfg) + " Please say yes to change it, or no to keep the previous openings."
		finalizeTurnMetadata(&turn, session, next, "requested_time", "requested_time", "datetime_correction_confirmation_repeated")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}

	next := cloneSessionForTurn(session)
	next.RequestedDate = strings.TrimSpace(pending.ProposedDate)
	next.RequestedStartTime = parsePendingProposedStart(pending.ProposedStartISO)
	next.OfferedSlots = nil
	next.Intent = IntentBooking
	nextState := normalizedDialogState(next.DialogState)
	nextState.Pending = nil
	nextState.LastPromptKey = ""
	next.DialogState = nextState
	advanceDraftRevision(session, &next)
	turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{"datetime_correction_confirmed": true})

	if next.RequestedStartTime != nil {
		available, _, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			updated, handoffErr := s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
			return true, updated, handoffErr
		}
		if !available {
			finalizeTurnMetadata(&turn, session, next, "requested_time", "requested_time", "datetime_correction_availability_alternative")
			updated, saveErr := s.store.SaveTurn(ctx, turn)
			return true, updated, saveErr
		}
		missing := missingBookingField(next)
		turn.AIMessage = selectedRequestedTimeReply(next, services, staff, cfg, missing)
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, session, next, missing, missing, "datetime_correction_confirmed_available")
		updated, saveErr := s.store.SaveTurn(ctx, turn)
		return true, updated, saveErr
	}

	if next.RequestedDate != "" && next.ServiceID != "" {
		if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, next.RequestedDate, false, cfg); err != nil {
			updated, handoffErr := s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
			return true, updated, handoffErr
		}
		finalizeTurnMetadata(&turn, session, next, "requested_time", "requested_time", "datetime_correction_confirmed_offer")
		updated, saveErr := s.store.SaveTurn(ctx, turn)
		return true, updated, saveErr
	}

	turn.AIMessage = promptForMissingField(missingBookingField(next))
	finalizeTurnMetadata(&turn, session, next, missingBookingField(next), missingBookingField(next), "datetime_correction_confirmed")
	updated, err := s.store.SaveTurn(ctx, turn)
	return true, updated, err
}

func dateTimeSelectionChanged(before Session, after Session) bool {
	return strings.TrimSpace(before.RequestedDate) != strings.TrimSpace(after.RequestedDate) ||
		!sameOptionalTime(before.RequestedStartTime, after.RequestedStartTime)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parsePendingProposedStart(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}

func offeredSlotDateTimeCorrectionPrompt(pending PendingConversationAct, cfg *RuntimeConfig) string {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	if start := parsePendingProposedStart(pending.ProposedStartISO); start != nil {
		return "Did you want to change the appointment to " + start.In(loc).Format("Monday, January 2 at 3:04 PM") + "?"
	}
	if date := strings.TrimSpace(pending.ProposedDate); date != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", date, loc); err == nil {
			return "Did you want to change the appointment date to " + parsed.Format("Monday, January 2") + "?"
		}
	}
	return "Did you want to change the appointment date and time?"
}
