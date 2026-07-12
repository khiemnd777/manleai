package conversation

import (
	"context"
	"github.com/manleai/ai-receptionist/internal/validation"
	"strconv"
	"strings"
	"unicode"
)

func extractPhone(message string) string {
	raw := phonePattern.FindString(message)
	if raw == "" {
		return ""
	}
	return validation.NormalizePhone(raw)
}

func extractEmail(message string) string {
	return strings.ToLower(strings.TrimSpace(emailPattern.FindString(message)))
}

func extractName(message string) string {
	for _, pattern := range namePatterns {
		match := pattern.FindStringSubmatch(message)
		if len(match) < 2 {
			continue
		}
		if name := cleanName(match[1]); name != "" {
			return name
		}
	}
	return ""
}

func (s *Service) handlePendingCustomerNameConfirmation(ctx context.Context, salonID string, ownerUserID string, session Session, message string, eventKey string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (bool, *Session, error) {
	if missingBookingField(session) != "customer_name" {
		return false, nil, nil
	}
	pendingName := pendingCustomerName(session)
	if pendingName == "" {
		return false, nil, nil
	}
	if isAffirmativeOnly(message) {
		next := session
		next.CustomerName = pendingName
		if next.CustomerPhone == "" {
			next.CustomerPhone = extractPhone(message)
		}
		if next.CustomerEmail == "" {
			next.CustomerEmail = extractEmail(message)
		}
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		clearPendingCustomerNameMetadata(&turn, "confirmed")
		updated, err := s.continueAfterCustomerName(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
		return true, updated, err
	}
	if candidate := correctedCustomerNameCandidate(message, session); candidate != "" {
		next := session
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = customerNameConfirmationPrompt(candidate)
		setPendingCustomerNameMetadata(&turn, candidate, "customer_corrected_name")
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, session, next, "customer_name", "customer_name", "customer_name_confirmation")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if isNegativeNameConfirmation(message) {
		next := session
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = "Please say or spell the customer name for the appointment."
		clearPendingCustomerNameMetadata(&turn, "rejected")
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, session, next, "customer_name", "customer_name", "customer_name_repair")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	return false, nil, nil
}

func (s *Service) continueAfterCustomerName(ctx context.Context, ownerUserID string, turn TurnRecord, next Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if missing := missingBookingField(next); missing != "" {
		turn.AIMessage = promptForMissingField(missing)
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, turn.Session, next, missing, missing, "customer_name_confirmed")
		return s.store.SaveTurn(ctx, turn)
	}
	return s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
}

func voiceCustomerNamePendingConfirmationCandidate(message string, session Session) string {
	if session.Channel != ChannelPhone || missingBookingField(session) != "customer_name" {
		return ""
	}
	if spelled := spelledCustomerName(message); spelled != "" {
		return ""
	}
	if explicit := extractName(message); explicit != "" {
		if isRiskySingleWordVoiceName(explicit) {
			return explicit
		}
		return ""
	}
	candidate := customerNameCandidate(message, session)
	return candidate
}

func customerNameCandidate(message string, session Session) string {
	if name := spelledCustomerName(message); name != "" {
		return name
	}
	if name := extractName(message); name != "" {
		return name
	}
	return bareCustomerNameForSession(message, session)
}

func correctedCustomerNameCandidate(message string, session Session) string {
	if name := spelledCustomerName(message); name != "" {
		return name
	}
	cleaned := stripNameCorrectionPrefix(message)
	if cleaned == "" || cleaned == strings.TrimSpace(message) {
		return customerNameCandidate(message, session)
	}
	if name := extractName(cleaned); name != "" {
		return name
	}
	return cleanBareCustomerName(cleaned)
}

func stripNameCorrectionPrefix(message string) string {
	value := strings.TrimSpace(message)
	for {
		lower := strings.ToLower(strings.TrimSpace(value))
		next := value
		for _, prefix := range []string{
			"no,",
			"no ",
			"nope,",
			"nope ",
			"not ",
			"it's ",
			"it is ",
			"this is ",
			"my name is ",
			"my name ",
			"the name is ",
			"name is ",
		} {
			if strings.HasPrefix(lower, prefix) {
				next = strings.TrimSpace(value[len(prefix):])
				break
			}
		}
		if next == value {
			return strings.TrimSpace(strings.Trim(value, " ,.;"))
		}
		value = next
	}
}

func spelledCustomerName(message string) string {
	cleaned := normalizeSpelledNameText(message)
	if cleaned == "" {
		return ""
	}
	tokens := strings.FieldsFunc(cleaned, func(r rune) bool {
		return r == ' ' || r == '-' || r == '.' || r == ','
	})
	letters := make([]rune, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		runes := []rune(token)
		if len(runes) != 1 || !isLatinLetter(runes[0]) {
			return ""
		}
		letters = append(letters, unicode.ToLower(runes[0]))
	}
	if len(letters) < 2 || len(letters) > 24 {
		return ""
	}
	letters[0] = unicode.ToUpper(letters[0])
	return string(letters)
}

func normalizeSpelledNameText(message string) string {
	value := strings.ToLower(strings.TrimSpace(message))
	replacer := strings.NewReplacer(
		"spelled", "",
		"spell", "",
		"it's", "",
		"it is", "",
		"my name is", "",
		"name is", "",
		"nope", "",
		"no", "",
	)
	value = replacer.Replace(value)
	return strings.TrimSpace(strings.Trim(value, " ,.;:!?"))
}

func isShortSingleWordName(name string) bool {
	name = strings.TrimSpace(strings.Trim(name, "."))
	if name == "" || len(strings.Fields(name)) != 1 {
		return false
	}
	runeCount := len([]rune(name))
	return runeCount >= 2 && runeCount <= 6
}

func isRiskySingleWordVoiceName(name string) bool {
	name = strings.TrimSpace(strings.Trim(name, "."))
	if name == "" || len(strings.Fields(name)) != 1 {
		return false
	}
	if isShortSingleWordName(name) {
		return true
	}
	lower := strings.ToLower(name)
	return len([]rune(name)) <= 9 && strings.HasSuffix(lower, "ing")
}

func customerNameConfirmationPrompt(name string) string {
	return "I heard " + strings.TrimSpace(name) + ". Is that the correct name for the appointment?"
}

func isNegativeNameConfirmation(message string) bool {
	normalized := normalizeLooseText(message)
	return normalized == "no" ||
		normalized == "nope" ||
		normalized == "not correct" ||
		normalized == "wrong" ||
		normalized == "incorrect" ||
		strings.HasPrefix(normalized, "no ") ||
		strings.HasPrefix(normalized, "nope ")
}

func pendingCustomerName(session Session) string {
	if strings.TrimSpace(session.CustomerName) != "" {
		return ""
	}
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		if metadataBool(msg.Metadata, "pending_customer_name_cleared") {
			return ""
		}
		if name := metadataString(msg.Metadata, "pending_customer_name"); name != "" {
			return name
		}
	}
	return ""
}

func setPendingCustomerNameMetadata(turn *TurnRecord, name string, reason string) {
	if turn == nil {
		return
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_customer_name":        strings.TrimSpace(name),
		"pending_customer_name_reason": strings.TrimSpace(reason),
	})
}

func clearPendingCustomerNameMetadata(turn *TurnRecord, reason string) {
	if turn == nil {
		return
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_customer_name_cleared": true,
		"pending_customer_name_reason":  strings.TrimSpace(reason),
	})
}

func setPendingServiceCandidateMetadata(turn *TurnRecord, result serviceUnderstandingResult) {
	if turn == nil || len(result.Candidates) == 0 {
		return
	}
	ids := make([]string, 0, len(result.Candidates))
	names := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if id := strings.TrimSpace(candidate.ID); id != "" {
			ids = append(ids, id)
		}
		if name := strings.TrimSpace(candidate.Name); name != "" {
			names = append(names, name)
		}
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_service_candidate_ids": ids,
		"pending_service_candidates":    names,
		"pending_service_token":         strings.TrimSpace(result.MatchedToken),
		"pending_service_reason":        strings.TrimSpace(result.Reason),
	})
}

func setPendingServiceEditMetadata(turn *TurnRecord, candidates []ServiceOption, mode string) {
	if turn == nil {
		return
	}
	ids := make([]string, 0, len(candidates))
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate.ID); id != "" {
			ids = append(ids, id)
		}
		if name := strings.TrimSpace(candidate.Name); name != "" {
			names = append(names, name)
		}
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = pendingServiceEditModeAddOrSwitch
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_service_edit_candidate_ids": ids,
		"pending_service_edit_candidates":    names,
		"pending_service_edit_mode":          mode,
	})
}

func pendingServiceEdit(session Session, services []ServiceOption) ([]ServiceOption, string, bool) {
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		if metadataBool(msg.Metadata, "pending_service_edit_cleared") {
			return nil, "", false
		}
		ids := metadataStringSlice(msg.Metadata, "pending_service_edit_candidate_ids")
		mode := strings.TrimSpace(metadataString(msg.Metadata, "pending_service_edit_mode"))
		if mode == "" {
			if len(ids) == 0 {
				continue
			}
			mode = pendingServiceEditModeAddOrSwitch
		}
		items := servicesByIDs(services, ids)
		if len(ids) > 0 && len(items) == 0 {
			return nil, "", false
		}
		return items, mode, true
	}
	return nil, "", false
}

func servicesByIDs(services []ServiceOption, ids []string) []ServiceOption {
	byID := map[string]ServiceOption{}
	for _, service := range services {
		byID[strings.TrimSpace(service.ID)] = service
	}
	out := make([]ServiceOption, 0, len(ids))
	for _, id := range ids {
		if service, ok := byID[strings.TrimSpace(id)]; ok {
			out = append(out, service)
		}
	}
	return out
}

func isPendingServiceAddDecision(message string) bool {
	normalized := normalizeLooseText(message)
	switch normalized {
	case "add it", "add that", "add this", "both", "both services", "same appointment", "together":
		return true
	default:
		return strings.Contains(normalized, "same visit") ||
			strings.Contains(normalized, "add to") ||
			strings.Contains(normalized, "keep both")
	}
}

func isPendingServiceReplaceDecision(message string) bool {
	normalized := normalizeLooseText(message)
	switch normalized {
	case "switch", "switch it", "switch to that", "change", "change it", "that only", "only that", "just that", "just that one":
		return true
	default:
		return strings.Contains(normalized, "only") ||
			strings.Contains(normalized, "instead")
	}
}

func isPendingServiceKeepDecision(message string) bool {
	normalized := normalizeLooseText(message)
	switch normalized {
	case "no", "nope", "nah", "do not switch", "dont switch", "don't switch", "do not change", "dont change", "don't change", "keep it", "keep that", "keep current", "keep the current service", "keep original", "keep the original", "same service":
		return true
	default:
		return strings.Contains(normalized, "keep ") ||
			strings.Contains(normalized, "do not switch") ||
			strings.Contains(normalized, "dont switch") ||
			strings.Contains(normalized, "don't switch") ||
			strings.Contains(normalized, "do not change") ||
			strings.Contains(normalized, "dont change") ||
			strings.Contains(normalized, "don't change")
	}
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			if text = strings.TrimSpace(text); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func metadataBool(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	value, ok := metadata[key]
	if !ok {
		return false
	}
	flag, ok := value.(bool)
	return ok && flag
}

func metadataInt(metadata map[string]any, key string) (int, bool) {
	if metadata == nil {
		return 0, false
	}
	value, ok := metadata[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func customerNameSlotRepairReply(message string, session Session, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, cfg *RuntimeConfig) (string, bool) {
	if missingBookingField(session) != "customer_name" {
		return "", false
	}
	if isGoodbyeUtterance(message) {
		return "No problem. I'll send this request to the owner to review. This is not a confirmed appointment. Goodbye.", true
	}
	if voiceCustomerNameNeedsRepair(message, session) {
		if customerNamePromptCount(session) >= maxCustomerNamePrompts {
			return "I'm having trouble catching the name. I'll send this request to the owner to review. This is not a confirmed appointment.", true
		}
		return "I may have heard that wrong. Please spell the name for the appointment.", false
	}
	if service := repeatedSelectedServiceInsteadOfName(message, session, services, aliases, categoryAliases); service != "" {
		if customerNamePromptCount(session) >= maxCustomerNamePrompts {
			return "I'm having trouble catching the name. I'll send this request to the owner to review. This is not a confirmed appointment.", true
		}
		return "I have " + service + " already. What name should I put on the appointment?", false
	}
	if looksLikeServiceInsteadOfName(message, services, aliases, categoryAliases) {
		return "", false
	}
	if extractName(message) != "" {
		return "", false
	}
	if bareCustomerNameForSession(message, session) != "" {
		return "", false
	}
	if looksLikeDateOrTimeInsteadOfName(message) {
		return timeInsteadOfNameReply(message, session, cfg), false
	}
	if customerNamePromptCount(session) >= maxCustomerNamePrompts {
		return "I'm having trouble catching the name. I'll send this request to the owner to review. This is not a confirmed appointment.", true
	}
	if !isCustomerNameNonAnswer(message, services, aliases, categoryAliases) {
		return "", false
	}
	if isConnectionCheck(message) {
		return "I can hear you. What name should I put on the appointment?", false
	}
	if isNameRepairRequest(message) {
		return "I'm asking for the customer name. What name should I put on the appointment?", false
	}
	return "Please say the customer name for the appointment, for example: \"My name is Linh.\"", false
}

func repeatedSelectedServiceInsteadOfName(message string, session Session, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) string {
	result := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases)
	if result.Status != serviceUnderstandingStatusSelected || len(result.Candidates) == 0 {
		return ""
	}
	if !sameServiceSelection(session, result.Candidates) {
		return ""
	}
	return strings.TrimSpace(serviceSummary(session, services))
}

func timeInsteadOfNameReply(message string, session Session, cfg *RuntimeConfig) string {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	when := ""
	if session.RequestedStartTime != nil {
		when = session.RequestedStartTime.In(loc).Format("3:04 PM")
	} else if selected := selectOfferedSlot(message, session.OfferedSlots, loc); selected != nil {
		when = selected.StartTime.In(loc).Format("3:04 PM")
	}
	if when != "" {
		return "I have " + when + ". What name should I put on the appointment?"
	}
	return "I have the appointment time. What name should I put on the appointment?"
}

func customerNamePromptCount(session Session) int {
	count := 0
	for _, msg := range session.Transcript {
		if msg.Speaker != SpeakerAI {
			continue
		}
		lower := strings.ToLower(msg.Body)
		if strings.Contains(lower, "name") && (strings.Contains(lower, "appointment") || strings.Contains(lower, "customer")) {
			count++
		}
	}
	return count
}

func isCustomerNameNonAnswer(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) bool {
	return isAffirmativeOnly(message) ||
		isConnectionCheck(message) ||
		isNameRepairRequest(message) ||
		hasCancelSignal(message) ||
		looksLikeServiceInsteadOfName(message, services, aliases, categoryAliases) ||
		phonePattern.MatchString(message) ||
		emailPattern.MatchString(message) ||
		looksLikeDateOrTimeInsteadOfName(message)
}

func voiceCustomerNameNeedsRepair(message string, session Session) bool {
	if session.Channel != ChannelPhone || missingBookingField(session) != "customer_name" {
		return false
	}
	if spelledCustomerName(message) != "" {
		return false
	}
	candidate := customerNameCandidate(message, session)
	if candidate == "" {
		return false
	}
	return isLowQualityVoiceCustomerName(candidate)
}

func isLowQualityVoiceCustomerName(name string) bool {
	words := customerNameWords(name)
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if isPhraseLikeNameToken(word) {
			return true
		}
	}
	if len(words) >= 4 && !looksLikeNameCaseSequence(words) {
		return true
	}
	return false
}

func customerNameWords(name string) []string {
	fields := strings.Fields(strings.TrimSpace(name))
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.;:!?\"'")
		if field != "" {
			words = append(words, field)
		}
	}
	return words
}

func isPhraseLikeNameToken(word string) bool {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "for", "fur", "für", "to", "of", "the", "is", "are", "am", "was", "were", "you", "your", "me", "appointment", "service", "book", "booking":
		return true
	default:
		return false
	}
}

func looksLikeNameCaseSequence(words []string) bool {
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		runes := []rune(strings.TrimSpace(word))
		if len(runes) == 0 || !unicode.IsUpper(runes[0]) {
			return false
		}
	}
	return true
}

func isAffirmativeOnly(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	switch normalized {
	case "yes", "yeah", "yep", "ok", "okay", "sure", "correct", "right", "yes you can", "yes i can", "yes please", "yes i want to":
		return true
	}
	return strings.HasPrefix(normalized, "yes ") ||
		strings.HasPrefix(normalized, "ok ") ||
		strings.HasPrefix(normalized, "okay ") ||
		strings.Contains(normalized, "i want to book")
}

func isGoodbyeUtterance(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	normalized := normalizeLooseText(message)
	return normalized == "bye" ||
		normalized == "bye bye" ||
		normalized == "goodbye" ||
		strings.Contains(lower, "bye-bye") ||
		strings.Contains(normalized, "i have to go") ||
		strings.Contains(normalized, "hang up")
}

func isNameRepairRequest(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	normalized := normalizeLooseText(message)
	return strings.Contains(lower, "pardon") ||
		strings.Contains(lower, "can't understand") ||
		strings.Contains(lower, "cannot understand") ||
		strings.Contains(normalized, "can t understand") ||
		strings.Contains(normalized, "could not understand") ||
		strings.Contains(normalized, "what did you say") ||
		strings.Contains(normalized, "what you say") ||
		strings.Contains(normalized, "say that again") ||
		strings.Contains(normalized, "repeat")
}

func looksLikeServiceInsteadOfName(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases ...[]ServiceCategoryAlias) bool {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "service") || strings.Contains(lower, "name of") {
		return true
	}
	if len(categoryAliases) > 0 {
		result := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases[0])
		return result.Status == serviceUnderstandingStatusSelected || result.Status == serviceUnderstandingStatusAmbiguous
	}
	result := interpretService(message, services, aliases)
	return result.Status == serviceUnderstandingStatusSelected || result.Status == serviceUnderstandingStatusAmbiguous
}

func looksLikeDateOrTimeInsteadOfName(message string) bool {
	lower := strings.ToLower(message)
	checks := []string{
		"today", "tomorrow", "this week", "next week", "monday", "tuesday", "wednesday",
		"thursday", "friday", "saturday", "sunday", " am", " pm", "a.m", "p.m", "o'clock",
		"one pm", "one p m", "two pm", "three pm", "four pm", "five pm",
	}
	for _, check := range checks {
		if strings.Contains(lower, check) {
			return true
		}
	}
	return dateOnlyPattern.MatchString(message) || timeWithMeridiemPattern.MatchString(message)
}

func normalizeLooseText(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	replacer := strings.NewReplacer(
		".", " ",
		",", " ",
		"!", " ",
		"?", " ",
		":", " ",
		";", " ",
		"-", " ",
		"_", " ",
		"'", " ",
		"\"", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(lower)), " ")
}

func bareCustomerNameForSession(message string, session Session) string {
	if session.Intent != IntentBooking || strings.TrimSpace(session.CustomerName) != "" {
		return ""
	}
	if missingBookingField(session) != "customer_name" {
		if session.ServiceID != "" || session.StaffID != "" || session.RequestedDate != "" || session.RequestedStartTime != nil || session.CustomerPhone != "" {
			return ""
		}
	}
	return cleanBareCustomerName(message)
}

func cleanBareCustomerName(raw string) string {
	name := cleanName(raw)
	name = strings.Trim(name, " \"'")
	name = strings.Trim(name, ".")
	if len([]rune(name)) < 2 || len([]rune(name)) > 80 {
		return ""
	}
	if phonePattern.MatchString(name) || emailPattern.MatchString(name) || hasBookingVerbSignal(name) || hasCancelSignal(name) {
		return ""
	}
	if isAffirmativeOnly(name) || isConnectionCheck(name) || isGoodbyeUtterance(name) || isNameRepairRequest(name) || looksLikeDateOrTimeInsteadOfName(name) {
		return ""
	}
	if len(strings.Fields(name)) > 4 {
		return ""
	}
	for _, r := range name {
		if r == ' ' || r == '\'' || r == '-' || r == '.' {
			continue
		}
		if isLatinLetter(r) {
			continue
		}
		return ""
	}
	return strings.TrimSpace(strings.Trim(name, "."))
}

func isLatinLetter(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	return unicode.IsLetter(r) && unicode.Is(unicode.Latin, r)
}

func cleanName(raw string) string {
	name := strings.TrimSpace(raw)
	for _, marker := range []string{" and my ", " phone ", " for ", " at ", " on ", " wants ", " would "} {
		if idx := strings.Index(strings.ToLower(name), marker); idx >= 0 {
			name = strings.TrimSpace(name[:idx])
		}
	}
	name = strings.Trim(name, " ,.;")
	if len(name) > 80 || phonePattern.MatchString(name) {
		return ""
	}
	return name
}
