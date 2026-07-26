package conversation

import (
	"fmt"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func internalLifecycleOptionsFromAvailability(result *booking.AvailabilityResult, session Session, requestedDate string, loc *time.Location) []PartySplitOption {
	candidate, ok := selectedInternalLifecycleCandidate(session)
	if !ok || result == nil || strings.TrimSpace(result.QuoteID) == "" {
		return nil
	}
	options := make([]PartySplitOption, 0, len(result.Slots))
	for _, slot := range result.Slots {
		fingerprint := strings.TrimSpace(slot.Fingerprint)
		if len(fingerprint) != 64 || slot.StartTime.IsZero() || !slot.EndTime.After(slot.StartTime) || len(slot.Segments) != len(candidate.Segments) {
			continue
		}
		blocks := make([]PartySplitBlock, 0, len(slot.Segments))
		valid := true
		for index, raw := range slot.Segments {
			target := candidate.Segments[index]
			serviceID := strings.TrimSpace(raw.ServiceID)
			staffID := strings.TrimSpace(raw.StaffID)
			guest := strings.TrimSpace(raw.GuestReference)
			mode := normalizeConversationStaffSelectionMode(raw.StaffSelectionMode)
			if serviceID != strings.TrimSpace(target.ServiceID) || guest != strings.TrimSpace(target.GuestReference) ||
				staffID == "" || mode != booking.StaffSelectionSpecific || raw.Quantity != 1 || target.Quantity != 1 ||
				raw.DurationMinutes <= 0 || raw.ScheduledStartTime.IsZero() || !raw.ScheduledEndTime.After(raw.ScheduledStartTime) ||
				int(raw.ScheduledEndTime.Sub(raw.ScheduledStartTime).Minutes()) != raw.DurationMinutes ||
				raw.OccupiedStartTime.IsZero() || raw.OccupiedEndTime.IsZero() || raw.OccupiedStartTime.After(raw.ScheduledStartTime) || raw.OccupiedEndTime.Before(raw.ScheduledEndTime) {
				valid = false
				break
			}
			for _, allocation := range raw.ResourceAllocations {
				if strings.TrimSpace(allocation.ResourcePoolID) == "" || allocation.UnitsAllocated <= 0 {
					valid = false
					break
				}
			}
			if !valid {
				break
			}
			blockIndex := len(blocks) - 1
			if blockIndex < 0 || !blocks[blockIndex].StartTime.Equal(raw.ScheduledStartTime) {
				blocks = append(blocks, PartySplitBlock{StartTime: raw.ScheduledStartTime, EndTime: raw.ScheduledEndTime})
				blockIndex = len(blocks) - 1
			} else if raw.ScheduledEndTime.After(blocks[blockIndex].EndTime) {
				blocks[blockIndex].EndTime = raw.ScheduledEndTime
			}
			segment := booking.BookingSegmentRequest{ServiceID: serviceID, StaffID: staffID, StaffSelectionMode: mode, GuestReference: guest, Quantity: 1}
			blocks[blockIndex].Segments = append(blocks[blockIndex].Segments, segment)
			blocks[blockIndex].QuoteRefs = append(blocks[blockIndex].QuoteRefs, PartySplitQuoteRef{
				ServiceID: serviceID, GuestReference: guest, Quantity: 1,
				RequestedStartTime: raw.ScheduledStartTime, RequestedEndTime: raw.ScheduledEndTime,
				AvailabilityQuoteID: strings.TrimSpace(result.QuoteID), SlotFingerprint: fingerprint,
			})
		}
		if !valid || len(blocks) == 0 || !slot.StartTime.Equal(blocks[0].StartTime) {
			continue
		}
		latest := blocks[0].EndTime
		lastStart := blocks[0].StartTime
		for _, block := range blocks[1:] {
			lastStart = block.StartTime
			if block.EndTime.After(latest) {
				latest = block.EndTime
			}
		}
		if !slot.EndTime.Equal(latest) {
			continue
		}
		option := PartySplitOption{
			ID: partySplitOptionID(blocks), Blocks: blocks,
			SpanMinutes:         int(latest.Sub(blocks[0].StartTime).Minutes()),
			FinishSpreadMinutes: int(latest.Sub(lastStart).Minutes()),
		}
		option = applyPartySplitDatePolicy(option, requestedDate, loc)
		options = append(options, option)
	}
	return rankPartySplitOptions(dedupePartySplitOptions(options), splitPartyOptionLimit)
}

func applyInternalLifecycleOffer(turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, options []PartySplitOption, unavailableRequestedTime bool) {
	if turn == nil || session == nil {
		return
	}
	candidate, ok := selectedInternalLifecycleCandidate(*session)
	if !ok {
		return
	}
	session.PartyPlan = &PartyPlan{PartySize: candidate.PartySize, SplitOptions: append([]PartySplitOption(nil), options...)}
	session.RequestedStartTime = nil
	clearSelectedAvailabilityQuote(session)
	session.OfferedSlots = nil
	turn.ToolMessage = fmt.Sprintf("Target-aware internal availability returned %d whole-appointment replacement option(s).", len(options))
	if len(options) == 0 {
		turn.AIMessage = "I do not see a complete replacement time for that appointment. What other day works?"
	} else {
		parts := make([]string, 0, len(options))
		for index, option := range options {
			parts = append(parts, ordinalSpeechLabel(index+1)+" "+internalLifecycleOptionDescription(option, services, staff, cfg))
		}
		turn.AIMessage = "I found these complete replacement options: " + strings.Join(parts, "; ") + ". Which one should I review with you?"
		if unavailableRequestedTime {
			turn.AIMessage = "That complete replacement is not available. " + turn.AIMessage
		}
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"availability_policy": "internal_whole_root_replacement", "replacement_option_count": len(options),
		"target_authority_appointment_version": candidate.AuthorityAppointmentVersion,
	})
	syncTurnUpdate(turn, *session, services, staff, cfg)
}

func internalLifecycleOptionDescription(option PartySplitOption, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	parts := make([]string, 0)
	for _, block := range option.Blocks {
		for _, segment := range block.Segments {
			detail := serviceName(segment.ServiceID, services, "service") + " with " + staffName(segment.StaffID, staff, "technician")
			if guest := strings.TrimSpace(segment.GuestReference); guest != "" {
				detail += " for " + strings.ReplaceAll(guest, "-", " ")
			}
			parts = append(parts, detail+" on "+block.StartTime.In(loc).Format("Monday, January 2 at 3:04 PM"))
		}
	}
	return joinHumanList(parts)
}

func internalLifecycleConfirmationPrompt(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	option, ok := selectedPartySplitOption(session.PartyPlan)
	if !ok {
		return "Please choose a complete replacement option first."
	}
	return "This will replace the whole appointment with " + internalLifecycleOptionDescription(option, services, staff, cfg) + ". Should I reschedule the entire appointment?"
}

func prepareSelectedInternalLifecycleOption(session *Session, option PartySplitOption) bool {
	if session == nil {
		return false
	}
	plan := clonePartyPlan(session.PartyPlan)
	if plan == nil {
		plan = &PartyPlan{}
	}
	found := false
	for _, existing := range plan.SplitOptions {
		if strings.TrimSpace(existing.ID) == strings.TrimSpace(option.ID) {
			found = true
			break
		}
	}
	if !found {
		plan.SplitOptions = append(plan.SplitOptions, clonePartySplitOption(option))
	}
	session.PartyPlan = plan
	applySelectedPartySplitOption(session, option, true)
	start := partySplitFirstStart(option)
	if start.IsZero() {
		return false
	}
	session.RequestedStartTime = &start
	if _, _, ok := partySplitAggregateProof(option); !ok {
		// A one-child lifecycle replacement is still an aggregate quote, even
		// though the Phase 4B party helper deliberately requires >1 children.
		if len(option.Blocks) != 1 || len(option.Blocks[0].QuoteRefs) != 1 {
			return false
		}
		ref := option.Blocks[0].QuoteRefs[0]
		session.AvailabilityQuoteID = strings.TrimSpace(ref.AvailabilityQuoteID)
		session.SlotFingerprint = strings.TrimSpace(ref.SlotFingerprint)
	}
	_, _, reviewed := reviewedInternalLifecycleAction(*session)
	return reviewed
}

func setInternalLifecyclePending(session *Session, promptKey string, value string) {
	if session == nil {
		return
	}
	state := normalizedDialogState(session.DialogState)
	state.Pending = &PendingConversationAct{Kind: "scheduling_lifecycle", Entity: "appointment", PromptKey: promptKey, Value: strings.TrimSpace(value)}
	state.LastPromptKey = promptKey
	session.DialogState = state
}

func internalLifecyclePending(session Session, promptKey string) bool {
	return session.DialogState.Pending != nil && session.DialogState.Pending.PromptKey == promptKey
}

func clearInternalLifecyclePending(session *Session) {
	if session == nil || session.DialogState.Pending == nil {
		return
	}
	switch session.DialogState.Pending.PromptKey {
	case PendingInternalRescheduleConfirmation, PendingInternalCancelReason, PendingInternalCancelConfirmation:
		session.DialogState.Pending = nil
	}
}

func selectedInternalLifecycleCancelReason(session Session) string {
	if !internalLifecyclePending(session, PendingInternalCancelConfirmation) {
		return ""
	}
	return strings.TrimSpace(session.DialogState.Pending.Value)
}

func internalLifecycleCancelReasonPrompt(candidate RescheduleCandidate, loc *time.Location) string {
	when := candidate.StartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	return "What is the reason for cancelling the whole appointment on " + when + "?"
}

func internalLifecycleCancelConfirmationPrompt(candidate RescheduleCandidate, reason string, loc *time.Location) string {
	when := candidate.StartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	return "This will cancel the entire appointment on " + when + " for " + strings.TrimSpace(reason) + ". Should I cancel the whole appointment?"
}

func selectedInternalLifecycleCandidate(session Session) (RescheduleCandidate, bool) {
	candidate := selectedRescheduleCandidate(session)
	if candidate == nil || !validInternalLifecycleCandidate(*candidate) {
		return RescheduleCandidate{}, false
	}
	return *candidate, true
}

func validInternalLifecycleCandidate(candidate RescheduleCandidate) bool {
	if strings.TrimSpace(candidate.AppointmentID) == "" ||
		candidate.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar ||
		candidate.AuthorityAppointmentVersion < 1 || candidate.PartySize < 1 ||
		candidate.ActiveChildCount != len(candidate.Segments) || len(candidate.Segments) == 0 ||
		(candidate.Status != booking.StatusConfirmed && candidate.Status != booking.StatusRescheduled) {
		return false
	}
	guests := map[string]bool{}
	allGuestsEmpty := true
	for _, segment := range candidate.Segments {
		guest := strings.TrimSpace(segment.GuestReference)
		if strings.TrimSpace(segment.ServiceID) == "" || strings.TrimSpace(segment.StaffID) == "" ||
			normalizeConversationStaffSelectionMode(segment.StaffSelectionMode) != booking.StaffSelectionSpecific || segment.Quantity != 1 {
			return false
		}
		if guest != "" {
			allGuestsEmpty = false
			guests[guest] = true
		}
	}
	if candidate.PartySize == 1 {
		return allGuestsEmpty || len(guests) == 1
	}
	return !allGuestsEmpty && len(guests) == candidate.PartySize
}

func reviewedInternalLifecycleAction(session Session) (reviewedPartyAction, RescheduleCandidate, bool) {
	candidate, ok := selectedInternalLifecycleCandidate(session)
	if !ok || bookingActionForSession(session) != BookingActionReschedule {
		return reviewedPartyAction{}, RescheduleCandidate{}, false
	}
	option, ok := selectedPartySplitOption(session.PartyPlan)
	if !ok || len(option.Blocks) == 0 {
		return reviewedPartyAction{}, RescheduleCandidate{}, false
	}
	result := reviewedPartyAction{Option: option}
	var previousBlockStart time.Time
	for _, block := range option.Blocks {
		if block.StartTime.IsZero() || (!previousBlockStart.IsZero() && !block.StartTime.After(previousBlockStart)) ||
			!block.EndTime.After(block.StartTime) || len(block.Segments) == 0 || len(block.Segments) != len(block.QuoteRefs) {
			return reviewedPartyAction{}, RescheduleCandidate{}, false
		}
		previousBlockStart = block.StartTime
		var latestBlockEnd time.Time
		for index, raw := range block.Segments {
			ref := block.QuoteRefs[index]
			segment := scheduling.ActionSegment{
				ServiceID: strings.TrimSpace(raw.ServiceID), StaffID: strings.TrimSpace(raw.StaffID),
				StaffSelectionMode: normalizeConversationStaffSelectionMode(raw.StaffSelectionMode),
				GuestReference:     strings.TrimSpace(ref.GuestReference), Quantity: ref.Quantity,
				RequestedStartTime: ref.RequestedStartTime, RequestedEndTime: ref.RequestedEndTime,
			}
			if segment.ServiceID == "" || segment.StaffID == "" || segment.StaffSelectionMode != booking.StaffSelectionSpecific ||
				strings.TrimSpace(ref.ServiceID) != segment.ServiceID || segment.Quantity != 1 ||
				segment.RequestedStartTime.IsZero() || !segment.RequestedEndTime.After(segment.RequestedStartTime) ||
				!segment.RequestedStartTime.Equal(block.StartTime) ||
				segment.RequestedEndTime.After(block.EndTime) {
				return reviewedPartyAction{}, RescheduleCandidate{}, false
			}
			quoteID := strings.TrimSpace(ref.AvailabilityQuoteID)
			fingerprint := strings.TrimSpace(ref.SlotFingerprint)
			if quoteID == "" || len(fingerprint) != 64 {
				return reviewedPartyAction{}, RescheduleCandidate{}, false
			}
			if result.AvailabilityQuoteID == "" {
				result.AvailabilityQuoteID, result.SlotFingerprint = quoteID, fingerprint
			} else if result.AvailabilityQuoteID != quoteID || result.SlotFingerprint != fingerprint {
				return reviewedPartyAction{}, RescheduleCandidate{}, false
			}
			result.Segments = append(result.Segments, segment)
			if result.StartTime.IsZero() || segment.RequestedStartTime.Before(result.StartTime) {
				result.StartTime = segment.RequestedStartTime
			}
			if result.EndTime.IsZero() || segment.RequestedEndTime.After(result.EndTime) {
				result.EndTime = segment.RequestedEndTime
			}
			if latestBlockEnd.IsZero() || segment.RequestedEndTime.After(latestBlockEnd) {
				latestBlockEnd = segment.RequestedEndTime
			}
		}
		if !latestBlockEnd.Equal(block.EndTime) {
			return reviewedPartyAction{}, RescheduleCandidate{}, false
		}
	}
	if len(result.Segments) != len(candidate.Segments) || result.AvailabilityQuoteID != strings.TrimSpace(session.AvailabilityQuoteID) ||
		result.SlotFingerprint != strings.TrimSpace(session.SlotFingerprint) {
		return reviewedPartyAction{}, RescheduleCandidate{}, false
	}
	for index, segment := range result.Segments {
		target := candidate.Segments[index]
		if segment.ServiceID != strings.TrimSpace(target.ServiceID) ||
			segment.GuestReference != strings.TrimSpace(target.GuestReference) || target.Quantity != 1 {
			return reviewedPartyAction{}, RescheduleCandidate{}, false
		}
	}
	return result, candidate, true
}

func internalLifecycleRequestTimes(action reviewedPartyAction) (time.Time, time.Time, bool) {
	if action.StartTime.IsZero() || !action.EndTime.After(action.StartTime) {
		return time.Time{}, time.Time{}, false
	}
	return action.StartTime, action.EndTime, true
}
