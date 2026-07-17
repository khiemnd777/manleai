package conversation

import (
	"strings"
	"time"
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
		return "What day would you like?"
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
		return "What day would you like for " + service + "?"
	case "requested_time", "requested_start_time":
		if date := strings.TrimSpace(session.RequestedDate); date != "" {
			return "For " + service + " on " + requestedDateLabel(date, timezoneLocation(timezoneFromConfig(cfg))) + ", what time works?"
		}
		return "Got it, " + service + ". What day and time would you like?"
	default:
		return ""
	}
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
	current := strings.TrimSpace(serviceSummary(session, services))
	if len(options) == 0 {
		if current == "" {
			return "Would you like to add another service, or replace the current service?"
		}
		return "Would you like to replace " + current + ", or add another service?"
	}
	if family := commonServiceFamily(candidates); family != "" && len(candidates) > 1 && current != "" {
		article := "a "
		if startsWithVowelSound(family) {
			article = "an "
		}
		return "You already have " + current + ". Would you like to switch to " + article + family + ", or add " + article + family + " to this appointment?"
	}
	requested := joinHumanList(options)
	if current == "" {
		return "Do you want " + requested + " for this appointment?"
	}
	if len(options) == 1 {
		return "Do you want to add " + requested + " to " + current + ", or switch to " + requested + " only?"
	}
	return "Do you want to add " + requested + " to " + current + ", or switch to one of those services only?"
}

func serviceEditTargetPrompt(session Session, candidates []ServiceOption, services []ServiceOption, add bool) string {
	current := strings.TrimSpace(serviceSummary(session, services))
	family := commonServiceFamily(candidates)
	options := serviceCandidateNames(candidates, 5)
	label := "service"
	if family != "" {
		label = family
	}
	action := "to add"
	if !add && current != "" {
		action = "instead of " + current
	}
	prompt := "Which " + label + " would you like " + action
	if len(options) > 0 {
		prompt += ": " + joinChoiceList(options)
	}
	return prompt + "?"
}

func serviceEditReplaceSourcePrompt(session Session, services []ServiceOption) string {
	options := make([]string, 0, len(session.BookingSegments))
	for _, segment := range session.BookingSegments {
		if name := strings.TrimSpace(serviceName(segment.ServiceID, services, "")); name != "" {
			options = append(options, name)
		}
	}
	if len(options) == 0 {
		return "Which current service would you like to replace?"
	}
	return "Which current service would you like to replace: " + joinChoiceList(options) + "?"
}

func commonServiceFamily(candidates []ServiceOption) string {
	var family string
	categoryBacked := len(candidates) > 0
	for _, candidate := range candidates {
		name := strings.TrimSpace(candidate.CategoryName)
		if name == "" {
			categoryBacked = false
			break
		}
		if family == "" {
			family = name
			continue
		}
		if !strings.EqualFold(family, name) {
			categoryBacked = false
			break
		}
	}
	if categoryBacked && family != "" {
		return strings.ToLower(family)
	}
	return singularServiceToken(commonServiceNameToken(candidates))
}

func startsWithVowelSound(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && strings.ContainsRune("aeiou", rune(value[0]))
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
		"Context: US nail salon appointment call. Languages: English and Vietnamese.",
	}
	if salon := salonName(cfg); salon != "" {
		parts = append(parts, "Salon keyword: "+salon+".")
	}
	if pending := serviceCandidateNames(pendingServiceCandidateServices(session, services), 8); len(pending) > 0 {
		parts = append(parts, "Priority keywords: "+strings.Join(pending, ", ")+".")
	}
	if names := transcriptionServiceNames(services, 40); len(names) > 0 {
		parts = append(parts, "Catalog keywords: "+strings.Join(names, ", ")+".")
	}
	if aliasKeywords := transcriptionAliasKeywords(aliases, 40); len(aliasKeywords) > 0 {
		parts = append(parts, "Alias keywords: "+strings.Join(aliasKeywords, ", ")+".")
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

func transcriptionAliasKeywords(aliases []ServiceAlias, limit int) []string {
	keywords := make([]string, 0, len(aliases))
	seen := map[string]bool{}
	for _, alias := range aliases {
		if len(keywords) >= limit {
			break
		}
		phrase := strings.TrimSpace(firstNonEmpty(alias.Alias, alias.NormalizedAlias))
		if phrase == "" {
			continue
		}
		key := strings.ToLower(phrase)
		if seen[key] {
			continue
		}
		seen[key] = true
		keywords = append(keywords, phrase)
	}
	return keywords
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
