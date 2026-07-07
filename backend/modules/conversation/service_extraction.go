package conversation

import (
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
	"strings"
	"time"
)

func normalizeStartRequest(req StartSessionRequest) StartSessionRequest {
	req.Channel = strings.TrimSpace(req.Channel)
	if req.Channel == "" {
		req.Channel = ChannelSimulator
	}
	if req.Channel != ChannelSimulator {
		req.Channel = ChannelSimulator
	}
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.CustomerPhone = validation.NormalizePhone(req.CustomerPhone)
	req.CustomerEmail = strings.TrimSpace(req.CustomerEmail)
	return req
}

func normalizeEventKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 180 {
		value = value[:180]
	}
	return value
}

func repairReplyForMessage(message string, session Session, cfg *RuntimeConfig) string {
	if !isRepairOrUnclearUtterance(message) {
		return ""
	}
	last := lastAITranscriptMessage(session)
	if isConnectionCheck(message) {
		if hasBookingProgress(session) {
			return "I can hear you. " + promptForCurrentBookingState(session, cfg)
		}
		return connectionCheckOpenPrompt
	}
	if last != "" {
		return last
	}
	if session.Intent == IntentBooking || session.ServiceID != "" || session.RequestedDate != "" || session.RequestedStartTime != nil {
		return promptForCurrentBookingState(session, cfg)
	}
	return ""
}

func isRepairOrUnclearUtterance(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	cleaned := strings.Trim(lower, " .,!?:;-")
	if len([]rune(cleaned)) <= 2 {
		return true
	}
	exact := []string{"sorry", "what", "huh", "pardon", "hello", "hi", "hey"}
	for _, trigger := range exact {
		if cleaned == trigger {
			return true
		}
	}
	contains := []string{"repeat that", "say that again", "can you repeat", "i didn't hear", "i did not hear", "can't understand", "cannot understand", "can you hear me", "i can hear you"}
	for _, trigger := range contains {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

func isConnectionCheck(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	cleaned := strings.Trim(lower, " .,!?:;-")
	return cleaned == "hello" || cleaned == "hi" || cleaned == "hey" ||
		strings.Contains(lower, "can you hear") ||
		strings.Contains(lower, "i can hear")
}

func hasBookingProgress(session Session) bool {
	return session.Intent == IntentBooking ||
		strings.TrimSpace(session.ServiceID) != "" ||
		strings.TrimSpace(session.RequestedDate) != "" ||
		session.RequestedStartTime != nil ||
		len(session.OfferedSlots) > 0 ||
		activePartyPlan(session.PartyPlan) ||
		strings.TrimSpace(session.CustomerName) != "" ||
		strings.TrimSpace(session.CustomerPhone) != "" ||
		hasStaffAssignment(session)
}

func lastAITranscriptMessage(session Session) string {
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker == SpeakerAI {
			return strings.TrimSpace(msg.Body)
		}
	}
	return ""
}

func promptForCurrentBookingState(session Session, cfg *RuntimeConfig) string {
	switch missingBookingField(session) {
	case "service":
		return promptForMissingField("service")
	case "requested_date":
		return promptForMissingField("requested_date")
	case "requested_time":
		if session.RequestedDate != "" {
			return "For " + requestedDateLabel(session.RequestedDate, timezoneLocation(timezoneFromConfig(cfg))) + ", what time works best?"
		}
		return promptForMissingField("requested_time")
	case "customer_name":
		return promptForMissingField("customer_name")
	case "customer_phone":
		return promptForMissingField("customer_phone")
	case "staff":
		return promptForMissingField("staff")
	default:
		return "I have the appointment details. Let me check that for you."
	}
}

func salonIdentityReplyForMessage(message string, session Session, cfg *RuntimeConfig) string {
	if !isSalonIdentityCheck(message, cfg) {
		return ""
	}
	salon := salonName(cfg)
	if salon == "" {
		return ""
	}
	prefix := "Yes, this is " + salon + "."
	if !hasBookingProgress(session) {
		return prefix + " " + openEndedHelpPrompt
	}
	if len(session.OfferedSlots) > 0 && missingBookingField(session) == "requested_time" {
		return prefix + " " + formatSlotOffer(session.OfferedSlots, timezoneLocation(timezoneFromConfig(cfg)), false)
	}
	return prefix + " " + promptForCurrentBookingState(session, cfg)
}

func isSalonIdentityCheck(message string, cfg *RuntimeConfig) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if shouldHandoff(message) {
		return false
	}
	identityShape := strings.HasPrefix(normalized, "hi ") ||
		strings.HasPrefix(normalized, "hello ") ||
		strings.HasPrefix(normalized, "is this ") ||
		strings.HasPrefix(normalized, "is that ") ||
		strings.HasPrefix(normalized, "am i calling ") ||
		strings.HasPrefix(normalized, "did i call ") ||
		(strings.Contains(message, "?") && len(strings.Fields(normalized)) <= 3)
	if !identityShape {
		return false
	}
	for _, identifier := range salonIdentityIdentifiers(salonName(cfg)) {
		if identifier != "" && (normalized == identifier || strings.Contains(normalized, identifier)) {
			return true
		}
	}
	return false
}

func salonIdentityIdentifiers(salon string) []string {
	normalized := normalizeLooseText(salon)
	if normalized == "" {
		return nil
	}
	identifiers := []string{normalized}
	parts := strings.Fields(normalized)
	if len(parts) > 0 && len([]rune(parts[0])) >= 4 {
		identifiers = append(identifiers, parts[0])
	}
	if len(parts) > 1 {
		identifiers = append(identifiers, strings.Join(parts[:2], " "))
	}
	return identifiers
}

func timezoneFromConfig(cfg *RuntimeConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.Timezone
}

func requestedDateLabel(requestedDate string, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(requestedDate), loc)
	if err != nil {
		return strings.TrimSpace(requestedDate)
	}
	return parsed.Format("Monday")
}

func bookingSourceForSession(session Session) string {
	if session.Channel == ChannelPhone {
		return booking.SourceAIVoiceCall
	}
	return booking.SourceAIConversationSimulator
}

func bookingNotesForSession(session Session) string {
	if session.Channel == ChannelPhone {
		return "AI phone receptionist request."
	}
	return "AI conversation simulator request."
}

func initialReply(cfg *RuntimeConfig) string {
	greeting := defaultGreeting
	if cfg != nil && strings.TrimSpace(cfg.AIGreeting) != "" {
		greeting = strings.TrimSpace(cfg.AIGreeting)
	}
	return normalizeInitialGreeting(greeting, salonName(cfg), recordingDisclosureForConfig(cfg))
}

func initialPhoneReply(cfg *RuntimeConfig) string {
	greeting := defaultGreeting
	if cfg != nil && strings.TrimSpace(cfg.AIGreeting) != "" {
		greeting = strings.TrimSpace(cfg.AIGreeting)
	}
	return normalizeInitialGreeting(stripStandardRecordingDisclosure(greeting), salonName(cfg), "")
}

func aiTone(cfg *RuntimeConfig) string {
	if cfg == nil {
		return "professional_warm"
	}
	switch strings.TrimSpace(cfg.AITone) {
	case "natural_human", "friendly_young", "concise_calm":
		return strings.TrimSpace(cfg.AITone)
	default:
		return "professional_warm"
	}
}

func normalizeInitialGreeting(greeting string, salon string, disclosure string) string {
	greeting = strings.TrimSpace(greeting)
	if greeting == "" {
		greeting = defaultGreeting
	}
	greeting = ensureSalonInGreeting(greeting, salon)
	greeting = ensureRecordingDisclosure(greeting, disclosure)
	greeting = ensureOpenEndedHelpPrompt(greeting)
	return greeting
}

func ensureSalonInGreeting(greeting string, salon string) string {
	salon = strings.TrimSpace(salon)
	if salon == "" || containsFold(greeting, salon) {
		return greeting
	}
	if strings.HasPrefix(strings.ToLower(greeting), "thank you for calling.") {
		rest := strings.TrimSpace(greeting[len("Thank you for calling."):])
		if rest == "" {
			return "Thank you for calling " + salon + "."
		}
		return "Thank you for calling " + salon + ". " + rest
	}
	return "Thank you for calling " + salon + ". " + greeting
}

func ensureRecordingDisclosure(greeting string, disclosure string) string {
	disclosure = strings.TrimSpace(disclosure)
	if disclosure == "" {
		return greeting
	}
	if containsFold(greeting, "recorded") {
		return greeting
	}
	return insertAfterFirstSentence(greeting, disclosure)
}

func recordingDisclosureForConfig(cfg *RuntimeConfig) string {
	if cfg != nil && !cfg.RecordingEnabled {
		return ""
	}
	if cfg != nil && strings.TrimSpace(cfg.RecordingConsentMessage) != "" {
		return strings.TrimSpace(cfg.RecordingConsentMessage)
	}
	return recordingDisclosure
}

func stripStandardRecordingDisclosure(greeting string) string {
	greeting = strings.TrimSpace(greeting)
	if greeting == "" {
		return greeting
	}
	greeting = strings.ReplaceAll(greeting, " "+recordingDisclosure, "")
	greeting = strings.ReplaceAll(greeting, recordingDisclosure+" ", "")
	greeting = strings.ReplaceAll(greeting, recordingDisclosure, "")
	return strings.TrimSpace(greeting)
}

func ensureOpenEndedHelpPrompt(greeting string) string {
	lower := strings.ToLower(greeting)
	if strings.Contains(lower, "how can i help") || strings.Contains(lower, "how may i help") {
		return greeting
	}
	return appendSentence(greeting, openEndedHelpPrompt)
}

func insertAfterFirstSentence(text string, sentence string) string {
	text = strings.TrimSpace(text)
	sentence = strings.TrimSpace(sentence)
	if text == "" {
		return sentence
	}
	if sentence == "" {
		return text
	}
	index := strings.Index(text, ".")
	if index < 0 || index == len(text)-1 {
		return appendSentence(text, sentence)
	}
	return strings.TrimSpace(text[:index+1]) + " " + sentence + " " + strings.TrimSpace(text[index+1:])
}

func appendSentence(text string, sentence string) string {
	text = strings.TrimSpace(text)
	sentence = strings.TrimSpace(sentence)
	if text == "" {
		return sentence
	}
	if sentence == "" {
		return text
	}
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "?") || strings.HasSuffix(text, "!") {
		return text + " " + sentence
	}
	return text + ". " + sentence
}

func containsFold(value string, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(strings.TrimSpace(needle)))
}

func salonName(cfg *RuntimeConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.SalonName)
}

func bookingSafetyEnabled(aiEnabled bool) bool {
	return aiEnabled
}

func resolveIntent(current string, message string, session Session) string {
	if shouldHandoff(message) {
		return IntentHandoff
	}
	if current == IntentBooking || hasBookingSignal(message) || session.ServiceID != "" || session.RequestedDate != "" || session.RequestedStartTime != nil {
		return IntentBooking
	}
	return IntentUnknown
}

func applyExtraction(session *Session, message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, staff []StaffOption, loc *time.Location, now func() time.Time) {
	if session == nil {
		return
	}
	if requestedAt, ok := parseRequestedTime(message, loc, now); ok {
		applyRequestedStartTime(session, requestedAt, loc)
	} else {
		if requestedDate := preferredDateFromMessage(message, nil, loc, now); requestedDate != "" {
			applyRequestedDate(session, requestedDate)
		}
		if session.RequestedStartTime == nil && strings.TrimSpace(session.RequestedDate) != "" {
			if requestedAt, ok := parseTimeOnlyForDate(message, session.RequestedDate, loc); ok {
				applyRequestedStartTime(session, requestedAt, loc)
			}
		}
	}
	if session.CustomerEmail == "" {
		if email := extractEmail(message); email != "" {
			session.CustomerEmail = email
		}
	}
	if session.CustomerPhone == "" {
		if phone := extractPhone(message); phone != "" {
			session.CustomerPhone = phone
		}
	}
	requestedAnyone := customerRequestedAnyone(message)
	matchedStaff := matchStaff(message, staff)
	if requestedAnyone {
		session.StaffSelectionMode = booking.StaffSelectionAnyone
		session.StaffID = ""
		session.StaffName = ""
		clearBookingSegmentsStaffSelection(session)
	} else if matchedStaff != nil {
		session.StaffSelectionMode = booking.StaffSelectionSpecific
		session.StaffID = matchedStaff.ID
		session.StaffName = matchedStaff.Name
		applySpecificStaffToBookingSegments(session, *matchedStaff)
	}
	if session.CustomerName == "" {
		if name := spelledCustomerName(message); name != "" && missingBookingField(*session) == "customer_name" {
			session.CustomerName = name
		} else if name := extractName(message); name != "" {
			session.CustomerName = name
		} else if !looksLikeServiceInsteadOfName(message, services, aliases, categoryAliases) {
			if name := bareCustomerNameForSession(message, *session); name != "" {
				session.CustomerName = name
			}
		}
	}
}

func detectStaffChangeRequest(message string, session Session, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, staff []StaffOption, activeStaff []StaffOption) staffChangeRequest {
	if bookingActionForSession(session) != BookingActionBook || !hasBookingProgress(session) {
		return staffChangeRequest{}
	}
	if spelledCustomerName(message) != "" || extractName(message) != "" {
		return staffChangeRequest{}
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return staffChangeRequest{}
	}
	requestedAnyone := customerRequestedAnyone(message)
	bookableMatch := matchStaff(message, staff)
	activeMatch := matchStaff(message, activeStaff)
	target := staffChangeTargetName(message)
	hasSignal := hasStaffChangeSignal(normalized, target != "", requestedAnyone, bookableMatch != nil || activeMatch != nil)
	if requestedAnyone && hasSignal {
		return staffChangeRequest{Intent: true, RequestedAnyone: true, Source: "anyone_available"}
	}
	if bookableMatch != nil && hasSignal {
		return staffChangeRequest{Intent: true, HasMatchedStaff: true, MatchedStaff: *bookableMatch, Source: "bookable_staff"}
	}
	if activeMatch != nil && !activeMatch.AIBookable && hasSignal {
		return staffChangeRequest{Intent: true, HasNonBookable: true, NonBookableStaff: *activeMatch, Source: "non_bookable_staff"}
	}
	if target == "" || !hasSignal || looksLikeServiceInsteadOfName(target, services, aliases, categoryAliases) || looksLikeDateOrTimeInsteadOfName(target) {
		return staffChangeRequest{}
	}
	return staffChangeRequest{Intent: true, UnknownStaffName: target, Source: "unknown_staff"}
}

func hasStaffChangeSignal(normalized string, hasTarget bool, requestedAnyone bool, hasKnownStaff bool) bool {
	if normalized == "" {
		return false
	}
	if requestedAnyone {
		return true
	}
	signals := []string{
		"change to",
		"change with",
		"switch to",
		"switch with",
		"move to",
		"move with",
		"instead",
		"actually",
		"prefer",
		"with",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return hasKnownStaff || hasTarget || signal == "with"
		}
	}
	return false
}

func staffChangeTargetName(message string) string {
	for _, pattern := range staffChangeTargetPatterns {
		match := pattern.FindStringSubmatch(message)
		if len(match) < 2 {
			continue
		}
		if target := cleanStaffChangeTarget(match[1]); target != "" {
			return target
		}
	}
	return ""
}

func cleanStaffChangeTarget(raw string) string {
	value := strings.TrimSpace(strings.Trim(raw, " ,.;?!\"'"))
	if value == "" || customerRequestedAnyone(value) || looksLikeDateOrTimeInsteadOfName(value) {
		return ""
	}
	for {
		lower := strings.ToLower(value)
		next := value
		for _, suffix := range []string{" instead", " please", " pls", " for me"} {
			if strings.HasSuffix(lower, suffix) {
				next = strings.TrimSpace(value[:len(value)-len(suffix)])
				break
			}
		}
		if next == value {
			break
		}
		value = next
	}
	for _, marker := range []string{" at ", " on ", " for "} {
		if idx := strings.Index(strings.ToLower(value), marker); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
	}
	return cleanBareCustomerName(value)
}

func offeredSlotAllowedForStaffChange(slot OfferedSlot, change staffChangeRequest) bool {
	if !change.Intent {
		return true
	}
	if change.RequestedAnyone {
		return true
	}
	if !change.HasMatchedStaff {
		return false
	}
	return offeredSlotHasStaff(slot, change.MatchedStaff)
}

func offeredSlotHasStaff(slot OfferedSlot, member StaffOption) bool {
	staffID := strings.TrimSpace(member.ID)
	staffName := strings.TrimSpace(member.Name)
	if staffID != "" && strings.TrimSpace(slot.StaffID) == staffID {
		return true
	}
	if staffName != "" && staffNameMentioned(strings.ToLower(slot.StaffName), staffName) {
		return true
	}
	for _, segment := range slot.Segments {
		if staffID != "" && strings.TrimSpace(segment.StaffID) == staffID {
			return true
		}
		if staffName != "" && staffNameMentioned(strings.ToLower(segment.StaffName), staffName) {
			return true
		}
	}
	return false
}

func applyStaffChangeMetadata(turn *TurnRecord, change staffChangeRequest) {
	if turn == nil || !change.Intent {
		return
	}
	metadata := map[string]any{
		"staff_change_intent": true,
		"staff_change_source": strings.TrimSpace(change.Source),
	}
	switch {
	case change.RequestedAnyone:
		metadata["staff_change_selection_mode"] = booking.StaffSelectionAnyone
	case change.HasMatchedStaff:
		metadata["staff_change_selection_mode"] = booking.StaffSelectionSpecific
		metadata["requested_staff_id"] = change.MatchedStaff.ID
		metadata["requested_staff_name"] = change.MatchedStaff.Name
	case change.HasNonBookable:
		metadata["staff_change_selection_mode"] = booking.StaffSelectionSpecific
		metadata["requested_staff_id"] = change.NonBookableStaff.ID
		metadata["requested_staff_name"] = change.NonBookableStaff.Name
	case change.UnknownStaffName != "":
		metadata["staff_change_selection_mode"] = "unknown"
		metadata["requested_staff_name"] = change.UnknownStaffName
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, metadata)
}

func unknownStaffChangeReply(name string, staff []StaffOption) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "that technician"
	}
	if len(staff) == 0 {
		return "I do not see " + name + " in the AI-bookable technician list. Should I use anyone available, or send this to the owner for review?"
	}
	return "I do not see " + name + " in the AI-bookable technician list. Would you like anyone available, or another technician?"
}

func serviceEditDecisionForMessage(session Session, message string, result serviceUnderstandingResult, services []ServiceOption) serviceEditDecision {
	if pending, ok := pendingServiceEdit(session, services); ok && result.Status == serviceUnderstandingStatusUnknown {
		switch {
		case hasServiceAddSignal(message) || isPendingServiceAddDecision(message):
			return serviceEditDecision{Action: serviceEditAdd, Candidates: pending, Source: "pending_service_edit"}
		case hasServiceCorrectionSignal(message) || isPendingServiceReplaceDecision(message):
			return serviceEditDecision{Action: serviceEditReplace, Candidates: pending, Source: "pending_service_edit"}
		case isAffirmativeOnly(message):
			return serviceEditDecision{Action: serviceEditClarifyAddSwitch, Candidates: pending, Source: "pending_service_edit"}
		}
	}

	switch result.Status {
	case serviceUnderstandingStatusSelected:
		if len(result.Candidates) == 0 {
			return serviceEditDecision{}
		}
		if strings.TrimSpace(session.ServiceID) == "" || len(session.BookingSegments) == 0 {
			return serviceEditDecision{Action: serviceEditSelectInitial, Candidates: result.Candidates, Source: "service_understanding"}
		}
		if sameServiceSelection(session, result.Candidates) {
			return serviceEditDecision{Action: serviceEditDuplicate, Candidates: result.Candidates, Source: "service_understanding"}
		}
		if hasServiceAddSignal(message) {
			return serviceEditDecision{Action: serviceEditAdd, Candidates: result.Candidates, Source: "service_understanding"}
		}
		if extendsCurrentServiceSelection(session, result.Candidates) {
			return serviceEditDecision{Action: serviceEditAdd, Candidates: result.Candidates, Source: "multi_service_selection"}
		}
		if hasServiceCorrectionSignal(message) || hasExplicitServiceReplacementPhrase(message) {
			return serviceEditDecision{Action: serviceEditReplace, Candidates: result.Candidates, Source: "service_understanding"}
		}
		if shouldApplyBareServiceSwitch(session, message, result) && hasServiceSwitchContext(session) {
			return serviceEditDecision{Action: serviceEditReplace, Candidates: result.Candidates, Source: "bare_service_switch"}
		}
		if missingBookingField(session) == "customer_name" {
			return serviceEditDecision{Action: serviceEditReplace, Candidates: result.Candidates, Source: "customer_name_service_repair"}
		}
		return serviceEditDecision{Action: serviceEditClarifyAddSwitch, Candidates: result.Candidates, Source: "service_understanding"}
	case serviceUnderstandingStatusAmbiguous:
		if len(result.Candidates) == 0 {
			return serviceEditDecision{}
		}
		if strings.TrimSpace(session.ServiceID) == "" {
			return serviceEditDecision{}
		}
		if hasServiceCorrectionSignal(message) {
			return serviceEditDecision{Action: serviceEditClearAmbiguous, Candidates: result.Candidates, Source: "ambiguous_service_correction"}
		}
		if hasServiceAddSignal(message) {
			return serviceEditDecision{Action: serviceEditClarifyAddSwitch, Candidates: result.Candidates, Source: "ambiguous_service_add"}
		}
	}
	return serviceEditDecision{}
}

func applyServiceEditDecision(session *Session, decision serviceEditDecision) bool {
	if session == nil {
		return false
	}
	switch decision.Action {
	case serviceEditSelectInitial, serviceEditReplace:
		return applyServiceSelection(session, decision.Candidates)
	case serviceEditAdd:
		return addServiceSelection(session, decision.Candidates)
	case serviceEditClearAmbiguous:
		if strings.TrimSpace(session.ServiceID) == "" && len(session.BookingSegments) == 0 && len(session.OfferedSlots) == 0 {
			return false
		}
		clearServiceSelection(session)
		return true
	default:
		return false
	}
}

func applyServiceEditMetadata(turn *TurnRecord, decision serviceEditDecision) {
	if turn == nil || decision.Action == serviceEditNone {
		return
	}
	ids := make([]string, 0, len(decision.Candidates))
	names := make([]string, 0, len(decision.Candidates))
	for _, candidate := range decision.Candidates {
		if id := strings.TrimSpace(candidate.ID); id != "" {
			ids = append(ids, id)
		}
		if name := strings.TrimSpace(candidate.Name); name != "" {
			names = append(names, name)
		}
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"service_edit_action":        string(decision.Action),
		"service_edit_source":        strings.TrimSpace(decision.Source),
		"service_edit_candidate_ids": ids,
		"service_edit_candidates":    names,
	})
	if decision.Action != serviceEditClarifyAddSwitch {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"pending_service_edit_cleared": true,
			"pending_service_edit_reason":  string(decision.Action),
		})
	}
}

func applyServiceInquiryMetadata(turn *TurnRecord, result serviceUnderstandingResult) {
	if turn == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"service_inquiry":        true,
		"service_inquiry_status": string(result.Status),
	})
}

func serviceSwitchAcknowledgement(previous Session, current Session, decision serviceEditDecision, serviceChanged bool, services []ServiceOption) string {
	if !serviceChanged || decision.Action != serviceEditReplace {
		return ""
	}
	if strings.TrimSpace(previous.ServiceID) == "" || strings.TrimSpace(current.ServiceID) == "" {
		return ""
	}
	if strings.TrimSpace(previous.ServiceID) == strings.TrimSpace(current.ServiceID) {
		return ""
	}
	if len(previous.OfferedSlots) == 0 && strings.TrimSpace(previous.RequestedDate) == "" && previous.RequestedStartTime == nil {
		return ""
	}
	service := strings.TrimSpace(serviceSummary(current, services))
	if service == "" {
		return "Switching services."
	}
	return "Switching to " + service + "."
}

func shouldApplyBareServiceSwitch(session Session, message string, result serviceUnderstandingResult) bool {
	if strings.TrimSpace(session.ServiceID) == "" || !hasBookingProgress(session) {
		return false
	}
	if result.Reason != serviceUnderstandingExact && result.Reason != serviceUnderstandingAlias {
		return false
	}
	return isBareServiceOnlyUtterance(message, result)
}

func hasServiceSwitchContext(session Session) bool {
	return len(session.OfferedSlots) > 0 ||
		strings.TrimSpace(session.RequestedDate) != "" ||
		session.RequestedStartTime != nil ||
		missingBookingField(session) == "customer_name"
}

func extendsCurrentServiceSelection(session Session, candidates []ServiceOption) bool {
	current := selectedServiceIDs(session)
	if len(current) == 0 || len(candidates) <= len(current) {
		return false
	}
	currentSet := map[string]bool{}
	for _, id := range current {
		currentSet[id] = true
	}
	foundCurrent := false
	foundNew := false
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.ID)
		if id == "" {
			continue
		}
		if currentSet[id] {
			foundCurrent = true
		} else {
			foundNew = true
		}
	}
	return foundCurrent && foundNew
}

func isBareServiceOnlyUtterance(message string, result serviceUnderstandingResult) bool {
	normalized := normalizeServiceText(stripPoliteServiceWords(message))
	if normalized == "" {
		return false
	}
	checks := []string{normalizeServiceText(result.MatchedToken)}
	if result.Selected != nil {
		checks = append(checks, normalizeServiceText(result.Selected.Name))
	}
	if alias := normalizeServiceText(result.MatchedAlias); alias != "" {
		checks = append(checks, alias)
	}
	for _, check := range checks {
		if check != "" && normalized == check {
			return true
		}
	}
	return false
}

func stripPoliteServiceWords(message string) string {
	value := strings.ToLower(strings.TrimSpace(message))
	value = strings.Trim(value, " .,!?:;-")
	for {
		next := value
		for _, prefix := range []string{"the ", "a ", "an "} {
			if strings.HasPrefix(next, prefix) {
				next = strings.TrimSpace(strings.TrimPrefix(next, prefix))
			}
		}
		for _, suffix := range []string{" please", " pls", " service"} {
			if strings.HasSuffix(next, suffix) {
				next = strings.TrimSpace(strings.TrimSuffix(next, suffix))
			}
		}
		if next == value {
			return value
		}
		value = next
	}
}

func applyServiceSelection(session *Session, matches []ServiceOption) bool {
	if session == nil || len(matches) == 0 {
		return false
	}
	before := selectedServiceIDs(*session)
	session.ServiceID = matches[0].ID
	session.ServiceName = matches[0].Name
	session.BookingSegments = bookingSegmentsFromServices(matches, *session)
	session.PartyPlan = nil
	session.OfferedSlots = nil
	if len(session.BookingSegments) > 0 {
		session.StaffSelectionMode = session.BookingSegments[0].StaffSelectionMode
	}
	return !sameStringSlices(before, selectedServiceIDs(*session))
}

func addServiceSelection(session *Session, matches []ServiceOption) bool {
	if session == nil || len(matches) == 0 {
		return false
	}
	before := selectedServiceIDs(*session)
	segments := append([]booking.BookingSegmentRequest(nil), session.BookingSegments...)
	if len(segments) == 0 && strings.TrimSpace(session.ServiceID) != "" {
		segments = bookingSegmentsFromServices([]ServiceOption{{
			ID:   session.ServiceID,
			Name: session.ServiceName,
		}}, *session)
	}
	mode := staffSelectionModeForServiceRequest(*session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	seen := map[string]bool{}
	for _, segment := range segments {
		if id := strings.TrimSpace(segment.ServiceID); id != "" {
			seen[id] = true
		}
	}
	for _, service := range matches {
		serviceID := strings.TrimSpace(service.ID)
		if serviceID == "" || seen[serviceID] {
			continue
		}
		segments = append(segments, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: mode,
		})
		seen[serviceID] = true
	}
	if len(segments) == 0 {
		return false
	}
	session.BookingSegments = segments
	session.PartyPlan = nil
	if strings.TrimSpace(session.ServiceID) == "" {
		session.ServiceID = strings.TrimSpace(segments[0].ServiceID)
	}
	if session.ServiceName == "" {
		session.ServiceName = serviceName(session.ServiceID, matches, "")
	}
	session.OfferedSlots = nil
	if len(session.BookingSegments) > 0 {
		session.StaffSelectionMode = session.BookingSegments[0].StaffSelectionMode
	}
	return !sameStringSlices(before, selectedServiceIDs(*session))
}

func clearServiceSelection(session *Session) {
	if session == nil {
		return
	}
	session.ServiceID = ""
	session.ServiceName = ""
	session.BookingSegments = nil
	session.PartyPlan = nil
	session.OfferedSlots = nil
}

func sameServiceSelection(session Session, matches []ServiceOption) bool {
	current := selectedServiceIDs(session)
	if len(current) != len(matches) {
		return false
	}
	for i, match := range matches {
		if strings.TrimSpace(match.ID) != current[i] {
			return false
		}
	}
	return true
}

func selectedServiceIDs(session Session) []string {
	if len(session.BookingSegments) > 0 {
		out := make([]string, 0, len(session.BookingSegments))
		for _, segment := range session.BookingSegments {
			serviceID := strings.TrimSpace(segment.ServiceID)
			if serviceID != "" {
				out = append(out, serviceID)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if serviceID := strings.TrimSpace(session.ServiceID); serviceID != "" {
		return []string{serviceID}
	}
	return nil
}

func sameStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}

func hasServiceAddSignal(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	signals := []string{
		"also",
		"add",
		"add on",
		"addon",
		"plus",
		"too",
		"as well",
		"together",
		"same appointment",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return strings.HasPrefix(normalized, "with ")
}

func containsLoosePhrase(normalized string, phrase string) bool {
	normalized = strings.TrimSpace(normalized)
	phrase = strings.TrimSpace(phrase)
	if normalized == "" || phrase == "" {
		return false
	}
	return normalized == phrase ||
		strings.HasPrefix(normalized, phrase+" ") ||
		strings.HasSuffix(normalized, " "+phrase) ||
		strings.Contains(normalized, " "+phrase+" ")
}

func hasExplicitServiceReplacementPhrase(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	signals := []string{
		"name of service",
		"service is",
		"service should be",
		"the service is",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
}

func hasServiceCorrectionSignal(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	signals := []string{
		"actually",
		"i mean",
		"i meant",
		"instead",
		"change it to",
		"change to",
		"switch to",
		"make it",
	}
	for _, signal := range signals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	cleaned := strings.TrimLeft(lower, " ,.;:!?")
	return strings.HasPrefix(cleaned, "no ") || strings.HasPrefix(cleaned, "no,") || strings.Contains(lower, " not ")
}
