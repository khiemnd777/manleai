package conversation

import (
	"strings"
	"time"
)

type serviceCatalogQuestionKind string

const (
	serviceCatalogQuestionNone      serviceCatalogQuestionKind = ""
	serviceCatalogQuestionMenu      serviceCatalogQuestionKind = "menu"
	serviceCatalogQuestionCount     serviceCatalogQuestionKind = "count"
	serviceCatalogQuestionExistence serviceCatalogQuestionKind = "existence"
)

func missingBookingField(session Session) string {
	switch {
	case strings.TrimSpace(session.ServiceID) == "":
		return "service"
	case session.RequestedStartTime == nil && strings.TrimSpace(session.RequestedDate) == "":
		return "requested_date"
	case session.RequestedStartTime == nil && !partyPlanHasSelectedSplitOption(session.PartyPlan):
		return "requested_time"
	case partyPlanSelectedSplitRequiresDateConsent(session.PartyPlan):
		return "party_split_date_consent"
	case strings.TrimSpace(session.CustomerName) == "":
		return "customer_name"
	case strings.TrimSpace(session.CustomerPhone) == "":
		return "customer_phone"
	case !hasStaffAssignment(session):
		return "staff"
	default:
		return ""
	}
}

func promptForMissingField(field string) string {
	switch field {
	case "customer_name":
		return "What name should I put on the appointment?"
	case "customer_phone":
		return "What phone number should we use?"
	case "service":
		return "Which service would you like?"
	case "staff":
		return "Which technician would you like, or should I use anyone available?"
	case "requested_date":
		return "What day would you like? I will check available times."
	case "requested_time":
		return "What time works for that day?"
	case "requested_start_time":
		return "What day and time would you like?"
	case "party_split_date_consent":
		return "Is it okay to split the group across different days?"
	default:
		return "What else should I know?"
	}
}

func promptForMissingFieldWithServiceContext(field string, session Session, services []ServiceOption, cfg *RuntimeConfig) string {
	service := strings.TrimSpace(serviceSummary(session, services))
	if service == "" {
		return ""
	}
	switch field {
	case "requested_date":
		return "Got it, " + service + ". What day would you like? I will check available times."
	case "requested_time", "requested_start_time":
		if date := strings.TrimSpace(session.RequestedDate); date != "" {
			return "For " + service + " on " + requestedDateLabel(date, timezoneLocation(timezoneFromConfig(cfg))) + ", what time works?"
		}
		return "Got it, " + service + ". What day and time would you like?"
	default:
		return ""
	}
}

func asksServiceMenu(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	signals := []string{
		"what service do you have",
		"what services do you have",
		"what service you have",
		"what services you have",
		"what services do you offer",
		"what do you offer",
		"service menu",
		"services menu",
		"list services",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	if strings.HasPrefix(normalized, "what ") {
		for _, signal := range []string{
			"service do you have",
			"services do you have",
			"service you have",
			"services you have",
			"service do you offer",
			"services do you offer",
			"option do you have",
			"options do you have",
			"option do you offer",
			"options do you offer",
		} {
			if strings.Contains(normalized, signal) {
				return true
			}
		}
	}
	return false
}

func asksServiceCatalogCount(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" || !containsLoosePhrase(normalized, "how many") {
		return false
	}
	if hasExplicitBookingRequestSignal(normalized) ||
		hasSchedulingAvailabilitySignal(normalized) ||
		looksLikeDateOrTimeInsteadOfName(message) ||
		mentionsPartyParticipants(normalized) {
		return false
	}
	for _, signal := range []string{
		"do you have",
		"you have",
		"do you offer",
		"you offer",
		"are there",
		"option",
		"options",
		"choice",
		"choices",
		"kind",
		"kinds",
		"type",
		"types",
		"service",
		"services",
		"menu",
	} {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func mentionsPartyParticipants(normalized string) bool {
	for _, signal := range []string{
		"person",
		"people",
		"guest",
		"guests",
		"client",
		"clients",
		"group",
		"party",
	} {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func classifyServiceCatalogQuestion(message string, result serviceUnderstandingResult) serviceCatalogQuestionKind {
	normalized := normalizeLooseText(message)
	if normalized == "" ||
		hasExplicitBookingRequestSignal(normalized) ||
		hasServiceAddSignal(message) ||
		hasServiceCorrectionSignal(message) ||
		hasExplicitServiceReplacementPhrase(message) ||
		looksLikeDateOrTimeInsteadOfName(message) ||
		hasSchedulingAvailabilitySignal(normalized) ||
		mentionsPartyParticipants(normalized) {
		return serviceCatalogQuestionNone
	}
	if asksServiceCatalogCount(message) {
		if result.Status != serviceUnderstandingStatusUnknown || mentionsGeneralServiceCatalog(normalized) {
			return serviceCatalogQuestionCount
		}
	}
	if asksServiceMenu(message) {
		return serviceCatalogQuestionMenu
	}
	if strings.HasPrefix(normalized, "do you have ") ||
		strings.HasPrefix(normalized, "do you guys have ") ||
		strings.HasPrefix(normalized, "do yall have ") ||
		strings.HasPrefix(normalized, "you have ") ||
		strings.HasPrefix(normalized, "do you offer ") ||
		strings.HasPrefix(normalized, "do you do ") ||
		strings.HasPrefix(normalized, "do you provide ") ||
		strings.HasPrefix(normalized, "is there ") ||
		(result.Status != serviceUnderstandingStatusUnknown &&
			strings.HasPrefix(normalized, "is ") &&
			strings.HasSuffix(normalized, " available")) {
		return serviceCatalogQuestionExistence
	}
	return serviceCatalogQuestionNone
}

func mentionsGeneralServiceCatalog(normalized string) bool {
	for _, signal := range []string{"service", "services", "service menu", "services menu"} {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func serviceMenuReply(services []ServiceOption) string {
	names := serviceCandidateNames(services, 8)
	if len(names) == 0 {
		return "Which service would you like?"
	}
	prefix := "Services include "
	if len(services) > len(names) {
		prefix = "Popular services include "
	}
	return prefix + joinHumanList(names) + ". Which one would you like to book?"
}

func isServiceInquiry(message string, result serviceUnderstandingResult) bool {
	return classifyServiceCatalogQuestion(message, result) != serviceCatalogQuestionNone
}

func hasExplicitBookingRequestSignal(normalized string) bool {
	signals := []string{
		"book",
		"booking",
		"appointment",
		"schedule",
		"reschedule",
		"cancel",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func hasSchedulingAvailabilitySignal(normalized string) bool {
	signals := []string{
		"availability",
		"available time",
		"available times",
		"open time",
		"open times",
		"opening",
		"openings",
		"slot",
		"slots",
		"spot",
		"spots",
		"time",
		"times",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func serviceInquiryReply(message string, session Session, result serviceUnderstandingResult, services []ServiceOption) string {
	if classifyServiceCatalogQuestion(message, result) == serviceCatalogQuestionCount {
		candidates := result.Candidates
		if len(candidates) == 0 {
			candidates = services
		}
		statement := serviceCatalogCountStatement(serviceCatalogQuestionLabel(result, candidates), candidates, 6)
		if statement == "" {
			return "I do not have a bookable service list available right now. What service are you looking for?"
		}
		return statement + " " + serviceInquiryResumePrompt(session, result, services)
	}
	switch result.Status {
	case serviceUnderstandingStatusSelected:
		names := serviceCandidateNames(result.Candidates, 3)
		if len(names) == 0 {
			break
		}
		service := joinHumanList(names)
		if current := strings.TrimSpace(serviceSummary(session, services)); current != "" {
			return "Yes, we offer " + service + ". Do you want to switch to " + service + ", or keep " + current + "?"
		}
		return "Yes, we offer " + service + ". Which service would you like to book?"
	case serviceUnderstandingStatusAmbiguous:
		names := serviceCandidateNames(result.Candidates, 5)
		if len(names) > 0 {
			return "We offer " + joinHumanList(names) + ". " + serviceInquiryResumePrompt(session, result, services)
		}
	}
	if names := serviceCandidateNames(services, 8); len(names) > 0 {
		return "I do not see that in the bookable service list. Services include " + joinHumanList(names) + ". Which service would you like to book?"
	}
	return "I do not see that in the bookable service list. Which service would you like?"
}

func serviceCatalogCountStatement(label string, candidates []ServiceOption, spokenLimit int) string {
	allNames := serviceCandidateNames(candidates, 0)
	if len(allNames) == 0 {
		return ""
	}
	label = singularServiceToken(label)
	if label == "" {
		label = "service"
	}
	optionWord := "options"
	if len(allNames) == 1 {
		optionWord = "option"
	}
	spokenNames := allNames
	if spokenLimit > 0 && len(spokenNames) > spokenLimit {
		spokenNames = spokenNames[:spokenLimit]
	}
	statement := "I can help book " + countWord(len(allNames)) + " " + label + " " + optionWord
	if len(spokenNames) < len(allNames) {
		return statement + ", including " + joinHumanList(spokenNames) + "."
	}
	return statement + ": " + joinHumanList(spokenNames) + "."
}

func serviceCatalogQuestionLabel(result serviceUnderstandingResult, candidates []ServiceOption) string {
	if label := strings.TrimSpace(result.MatchedCategoryName); label != "" {
		return normalizeServiceText(label)
	}
	if label := strings.TrimSpace(result.MatchedToken); label != "" {
		return singularServiceToken(label)
	}
	if label := strings.TrimSpace(commonServiceCategoryName(candidates)); label != "" {
		return normalizeServiceText(label)
	}
	if label := strings.TrimSpace(commonServiceNameToken(candidates)); label != "" {
		return singularServiceToken(label)
	}
	return "service"
}

func serviceInquiryResumePrompt(session Session, result serviceUnderstandingResult, services []ServiceOption) string {
	pending := pendingServiceCandidateServices(session, services)
	if len(pending) > 0 {
		if len(result.Candidates) > 0 && !sameServiceCandidateSet(pending, result.Candidates) {
			label := singularServiceToken(pendingServiceToken(session))
			if label == "" {
				label = serviceCatalogQuestionLabel(serviceUnderstandingResult{}, pending)
			}
			return "For your appointment, which " + label + " service would you like?"
		}
		if len(pending) == 1 {
			return "Would you like that one?"
		}
		return "Which one would you like?"
	}
	if current := strings.TrimSpace(serviceSummary(session, services)); current != "" {
		return "Would you like to keep " + current + ", or switch to another one?"
	}
	if hasBookingProgress(session) {
		return "Which service would you like to book?"
	}
	return "Would you like to book one of those?"
}

func sameServiceCandidateSet(left []ServiceOption, right []ServiceOption) bool {
	if len(left) != len(right) {
		return false
	}
	counts := map[string]int{}
	for _, service := range left {
		counts[strings.TrimSpace(service.ID)]++
	}
	for _, service := range right {
		id := strings.TrimSpace(service.ID)
		if counts[id] == 0 {
			return false
		}
		counts[id]--
	}
	return true
}

func serviceClarificationPrompt(session Session, result serviceUnderstandingResult, cfg *RuntimeConfig) string {
	if result.Status != serviceUnderstandingStatusAmbiguous || len(result.Candidates) == 0 {
		return ""
	}
	options := serviceCandidateNames(result.Candidates, 5)
	if len(options) == 0 {
		return ""
	}
	serviceLabel := "service"
	if token := strings.TrimSpace(result.MatchedToken); token != "" {
		serviceLabel = token + " service"
	}
	return "Which " + serviceLabel + " would you like: " + joinChoiceList(options) + "?"
}

func serviceEditClarificationPrompt(session Session, candidates []ServiceOption, services []ServiceOption) string {
	options := serviceCandidateNames(candidates, 3)
	if len(options) == 0 {
		return "Do you want to add that service to the appointment, or switch to that service only?"
	}
	current := strings.TrimSpace(serviceSummary(session, services))
	requested := joinHumanList(options)
	if current == "" {
		return "Do you want " + requested + " for this appointment?"
	}
	if len(options) == 1 {
		return "Do you want to add " + requested + " to " + current + ", or switch to " + requested + " only?"
	}
	return "Do you want to add " + requested + " to " + current + ", or switch to one of those services only?"
}

func serviceChangeConfirmationPrompt(session Session, candidates []ServiceOption, services []ServiceOption, cfg *RuntimeConfig) string {
	options := serviceCandidateNames(candidates, 3)
	requested := joinHumanList(options)
	if requested == "" {
		requested = "that service"
	}
	current := strings.TrimSpace(serviceSummary(session, services))
	if current == "" {
		return "Do you want to switch to " + requested + "?"
	}
	context := strings.TrimSpace(appointmentContextPhrase(session, cfg))
	if context != "" {
		return "I have " + current + " for " + context + ". Do you want to switch to " + requested + "?"
	}
	return "I have " + current + ". Do you want to switch to " + requested + "?"
}

func serviceKeepCurrentAcknowledgement(session Session, services []ServiceOption) string {
	current := strings.TrimSpace(serviceSummary(session, services))
	if current == "" {
		return ""
	}
	return "Okay, keeping " + current + "."
}

func serviceUnderstandingForClarification(session Session, services []ServiceOption, result serviceUnderstandingResult) serviceUnderstandingResult {
	if result.Status == serviceUnderstandingStatusAmbiguous && len(result.Candidates) > 0 {
		return result
	}
	pending := pendingServiceCandidateServices(session, services)
	if len(pending) == 0 {
		return result
	}
	result.Status = serviceUnderstandingStatusAmbiguous
	if result.Reason == "" || result.Reason == serviceUnderstandingUnknown {
		result.Reason = serviceUnderstandingAmbiguousFamily
	}
	result.Confidence = maxFloat(result.Confidence, 0.72)
	result.Candidates = pending
	result.MatchedToken = firstNonEmpty(result.MatchedToken, pendingServiceToken(session))
	return result
}

func pendingServiceToken(session Session) string {
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		if metadataBool(msg.Metadata, "pending_service_candidates_cleared") {
			return ""
		}
		if token := metadataString(msg.Metadata, "pending_service_token"); token != "" {
			return token
		}
	}
	return ""
}

func appointmentContextPhrase(session Session, cfg *RuntimeConfig) string {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	if session.RequestedStartTime != nil {
		return session.RequestedStartTime.In(loc).Format("Monday at 3:04 PM")
	}
	if strings.TrimSpace(session.RequestedDate) == "" {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", session.RequestedDate)
	if err != nil {
		return strings.TrimSpace(session.RequestedDate)
	}
	return parsed.Format("Monday")
}

func serviceCandidateNames(candidates []ServiceOption, limit int) []string {
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	names := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, service := range candidates[:limit] {
		name := strings.TrimSpace(service.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
	}
	return names
}

func transcriptionContextPrompt(session Session, cfg *RuntimeConfig, services []ServiceOption, aliases []ServiceAlias) string {
	parts := []string{
		"Nail salon appointment call. The caller may speak Vietnamese-accented English or switch between Vietnamese and English.",
		"When audio sounds close to an active service name or alias, transcribe the likely service name clearly.",
	}
	if salon := salonName(cfg); salon != "" {
		parts = append(parts, "Salon: "+salon+".")
	}
	if pending := serviceCandidateNames(pendingServiceCandidateServices(session, services), 8); len(pending) > 0 {
		parts = append(parts, "Current service options being clarified: "+strings.Join(pending, "; ")+".")
	}
	if names := transcriptionServiceNames(services, 40); len(names) > 0 {
		parts = append(parts, "Active service names: "+strings.Join(names, "; ")+".")
	}
	if aliasLines := transcriptionAliasLines(aliases, 40); len(aliasLines) > 0 {
		parts = append(parts, "Active service aliases: "+strings.Join(aliasLines, "; ")+".")
	}
	return truncateRunes(strings.Join(parts, "\n"), 1500)
}

func transcriptionServiceNames(services []ServiceOption, limit int) []string {
	names := make([]string, 0, len(services))
	seen := map[string]bool{}
	for _, service := range services {
		if len(names) >= limit {
			break
		}
		name := strings.TrimSpace(service.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
	}
	return names
}

func transcriptionAliasLines(aliases []ServiceAlias, limit int) []string {
	lines := make([]string, 0, len(aliases))
	seen := map[string]bool{}
	for _, alias := range aliases {
		if len(lines) >= limit {
			break
		}
		phrase := strings.TrimSpace(firstNonEmpty(alias.Alias, alias.NormalizedAlias))
		serviceName := strings.TrimSpace(alias.ServiceName)
		if phrase == "" || serviceName == "" {
			continue
		}
		key := strings.ToLower(phrase + "=>" + serviceName)
		if seen[key] {
			continue
		}
		seen[key] = true
		lines = append(lines, phrase+" -> "+serviceName)
	}
	return lines
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}
