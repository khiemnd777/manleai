package conversation

import (
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	answerSourceServiceCatalog  = "structured_service_catalog"
	answerSourceBusinessHours   = "structured_business_hours"
	answerSourceStaff           = "structured_staff"
	answerSourceAvailability    = "booking_availability"
	answerSourceKnowledge       = "knowledge"
	answerSourceBookingRedirect = "booking_redirect"
)

type answerRoute struct {
	Handled         bool
	Reply           string
	Source          string
	Reason          string
	Intent          string
	Confidence      float64
	SourceRecordIDs []string
}

func routeServiceMenuAnswer(services []ServiceOption) answerRoute {
	names := serviceCandidateNames(services, 8)
	confidence := 0.96
	if len(names) == 0 {
		confidence = 0.72
	}
	return answerRoute{
		Handled:         true,
		Reply:           serviceMenuReply(services),
		Source:          answerSourceServiceCatalog,
		Reason:          "service_menu",
		Intent:          "service_menu",
		Confidence:      confidence,
		SourceRecordIDs: answerServiceIDs(limitServices(services, 8)),
	}
}

func routeStructuredServiceQuestion(question ConversationQuestion, session Session, result serviceUnderstandingResult, answerCtx *AIAnswerContext) answerRoute {
	services := answerServices(answerCtx)
	candidates := servicesByIDs(services, question.ServiceIDs)
	if len(candidates) == 0 && result.Status != serviceUnderstandingStatusUnknown {
		candidates = append([]ServiceOption(nil), result.Candidates...)
	}
	mode := strings.TrimSpace(question.Mode)
	switch mode {
	case ConversationQuestionModeCount:
		if len(candidates) == 0 {
			candidates = services
		}
		labelUnderstanding := result
		if len(question.ServiceIDs) > 0 {
			labelUnderstanding = serviceUnderstandingResult{}
		}
		statement := serviceCatalogCountStatement(serviceCatalogQuestionLabel(labelUnderstanding, candidates), candidates, 6)
		if statement == "" {
			statement = "I don't have a bookable service list available right now."
		}
		return answerRoute{Handled: true, Reply: statement, Source: answerSourceServiceCatalog, Reason: "service_catalog_count", Intent: "service_catalog_count", Confidence: 0.96, SourceRecordIDs: answerServiceIDs(candidates)}
	case ConversationQuestionModeExistence:
		if len(candidates) == 0 {
			if len(question.ServiceIDs) == 0 && result.Status == serviceUnderstandingStatusUnknown && len(services) > 0 {
				return answerRoute{Handled: true, Reply: "Yes. " + serviceMenuReply(services), Source: answerSourceServiceCatalog, Reason: "service_catalog_exists", Intent: "service_catalog_existence", Confidence: 0.96, SourceRecordIDs: answerServiceIDs(services)}
			}
			return answerRoute{Handled: true, Reply: "I don't see that in the current bookable service list. " + serviceMenuReply(services), Source: answerSourceServiceCatalog, Reason: "service_catalog_not_found", Intent: "service_catalog_existence", Confidence: 0.9, SourceRecordIDs: answerServiceIDs(services)}
		}
		return answerRoute{Handled: true, Reply: "Yes, we offer " + joinHumanList(serviceCandidateNames(candidates, 5)) + ". Which one would you like?", Source: answerSourceServiceCatalog, Reason: "service_catalog_exists", Intent: "service_catalog_existence", Confidence: 0.96, SourceRecordIDs: answerServiceIDs(candidates)}
	case ConversationQuestionModeDetails, ConversationQuestionModeCompare:
		if len(candidates) == 0 {
			return routeServiceMenuAnswer(services)
		}
		return answerRoute{Handled: true, Reply: consultationComparisonReply(candidates), Source: answerSourceServiceCatalog, Reason: "service_catalog_details", Intent: "service_catalog_details", Confidence: 0.95, SourceRecordIDs: answerServiceIDs(candidates)}
	default:
		if len(candidates) > 0 {
			return routeServiceMenuAnswer(candidates)
		}
		return routeServiceMenuAnswer(services)
	}
}

func routeStructuredQuestionAnswer(message string, question ConversationQuestion, session Session, result serviceUnderstandingResult, answerCtx *AIAnswerContext, cfg *RuntimeConfig, now func() time.Time) answerRoute {
	switch question.Subject {
	case ConversationQuestionCatalog:
		return routeStructuredServiceQuestion(question, session, result, answerCtx)
	case ConversationQuestionPrice:
		return routeStructuredPriceQuestion(question, result, answerCtx)
	case ConversationQuestionHours:
		reply, ids, confidence := businessHoursAnswer(message, answerBusinessHours(answerCtx), answerSchedulingAuthority(answerCtx), cfg, now)
		return answerRoute{Handled: true, Reply: reply, Source: answerSourceBusinessHours, Reason: "business_hours", Intent: "hours_question", Confidence: confidence, SourceRecordIDs: ids}
	case ConversationQuestionStaff:
		reply, ids, confidence := staffAnswer(message, answerStaff(answerCtx), answerActiveStaff(answerCtx))
		return answerRoute{Handled: true, Reply: reply, Source: answerSourceStaff, Reason: "staff_question", Intent: "staff_question", Confidence: confidence, SourceRecordIDs: ids}
	case ConversationQuestionAvailability:
		return answerRoute{Handled: true, Reply: availabilityPromptForSession(session, answerServices(answerCtx)), Source: answerSourceAvailability, Reason: "availability_intent_missing_details", Intent: "availability_question", Confidence: 0.9}
	case ConversationQuestionPolicy:
		if match := bestKnowledgeMatch(message, answerKnowledge(answerCtx)); match != nil && strings.TrimSpace(match.Body) != "" {
			return answerRoute{Handled: true, Reply: knowledgeAnswerFromMatch(match), Source: answerSourceKnowledge, Reason: "knowledge_match", Intent: "knowledge_question", Confidence: 0.74, SourceRecordIDs: answerKnowledgeIDs(*match)}
		}
	}
	return answerRoute{Handled: true, Reply: "I don't have a verified answer for that. I can ask the owner to help.", Source: answerSourceBookingRedirect, Reason: "structured_answer_unavailable", Intent: "owner_help", Confidence: 0.7}
}

func routeStructuredPriceQuestion(question ConversationQuestion, result serviceUnderstandingResult, answerCtx *AIAnswerContext) answerRoute {
	services := answerServices(answerCtx)
	candidates := servicesByIDs(services, question.ServiceIDs)
	if len(candidates) == 0 && result.Status != serviceUnderstandingStatusUnknown {
		candidates = append([]ServiceOption(nil), result.Candidates...)
	}
	if len(candidates) == 0 {
		candidates = services
	}
	parts := make([]string, 0, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, service := range candidates {
		if len(parts) >= 6 {
			break
		}
		price := consultationPrice(service)
		if price == "" {
			continue
		}
		parts = append(parts, strings.TrimSpace(service.Name)+" is "+price)
		ids = append(ids, strings.TrimSpace(service.ID))
	}
	if len(parts) == 0 {
		return answerRoute{Handled: true, Reply: "I don't have verified pricing for that service. I can ask the owner to help.", Source: answerSourceServiceCatalog, Reason: "service_price_unavailable", Intent: "service_price", Confidence: 0.72}
	}
	reply := strings.Join(parts, ". ") + "."
	if len(candidates) > len(parts) {
		reply += " I can ask the owner about pricing for the other services."
	}
	return answerRoute{Handled: true, Reply: reply, Source: answerSourceServiceCatalog, Reason: "service_price", Intent: "service_price", Confidence: 0.96, SourceRecordIDs: ids}
}

func routeNonBookingAnswer(message string, session Session, answerCtx *AIAnswerContext, cfg *RuntimeConfig, now func() time.Time) answerRoute {
	services := answerServices(answerCtx)
	staff := answerStaff(answerCtx)
	activeStaff := answerActiveStaff(answerCtx)
	knowledge := answerKnowledge(answerCtx)
	hours := answerBusinessHours(answerCtx)

	if asksBusinessHours(message) {
		reply, ids, confidence := businessHoursAnswer(message, hours, answerSchedulingAuthority(answerCtx), cfg, now)
		return answerRoute{
			Handled:         true,
			Reply:           reply,
			Source:          answerSourceBusinessHours,
			Reason:          "business_hours",
			Intent:          "hours_question",
			Confidence:      confidence,
			SourceRecordIDs: ids,
		}
	}

	if asksStaffQuestion(message, staff, activeStaff) {
		reply, ids, confidence := staffAnswer(message, staff, activeStaff)
		return answerRoute{
			Handled:         true,
			Reply:           reply,
			Source:          answerSourceStaff,
			Reason:          "staff_question",
			Intent:          "staff_question",
			Confidence:      confidence,
			SourceRecordIDs: ids,
		}
	}

	if asksAvailabilityQuestion(message) {
		return answerRoute{
			Handled:    true,
			Reply:      availabilityPromptForSession(session, services),
			Source:     answerSourceAvailability,
			Reason:     "availability_intent_missing_details",
			Intent:     "availability_question",
			Confidence: 0.82,
		}
	}

	if match := bestKnowledgeMatch(message, knowledge); match != nil && strings.TrimSpace(match.Body) != "" {
		reply := knowledgeAnswerFromMatch(match)
		return answerRoute{
			Handled:         true,
			Reply:           reply,
			Source:          answerSourceKnowledge,
			Reason:          "knowledge_match",
			Intent:          "knowledge_question",
			Confidence:      0.74,
			SourceRecordIDs: answerKnowledgeIDs(*match),
		}
	}

	return answerRoute{
		Handled:    true,
		Reply:      "I can help with appointments. What service would you like to book?",
		Source:     answerSourceBookingRedirect,
		Reason:     "no_answer_source_match",
		Intent:     "booking_redirect",
		Confidence: 0.54,
	}
}

func applyAnswerRouteMetadata(turn *TurnRecord, route answerRoute, answerCtx *AIAnswerContext) {
	if turn == nil || !route.Handled {
		return
	}
	metadata := map[string]any{
		"answer_source":            route.Source,
		"answer_source_reason":     route.Reason,
		"answer_source_confidence": route.Confidence,
		"router_intent":            route.Intent,
	}
	if len(route.SourceRecordIDs) > 0 {
		metadata["source_record_ids"] = append([]string(nil), route.SourceRecordIDs...)
	}
	if answerCtx != nil {
		metadata["answer_context_cache_hit"] = answerCtx.CacheHit
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, metadata)
}

func answerServices(ctx *AIAnswerContext) []ServiceOption {
	if ctx == nil {
		return nil
	}
	return ctx.Services
}

func answerStaff(ctx *AIAnswerContext) []StaffOption {
	if ctx == nil {
		return nil
	}
	return ctx.Staff
}

func answerActiveStaff(ctx *AIAnswerContext) []StaffOption {
	if ctx == nil {
		return nil
	}
	return ctx.ActiveStaff
}

func answerKnowledge(ctx *AIAnswerContext) []KnowledgeSnippet {
	if ctx == nil {
		return nil
	}
	return ctx.Knowledge
}

func answerBusinessHours(ctx *AIAnswerContext) []BusinessHourPeriod {
	if ctx == nil {
		return nil
	}
	return ctx.BusinessHours
}

func asksBusinessHours(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "opening") || strings.Contains(normalized, "openings") ||
		strings.Contains(normalized, "open time") || strings.Contains(normalized, "open times") {
		return false
	}
	signals := []string{
		"business hours",
		"store hours",
		"salon hours",
		"your hours",
		"what hours",
		"hours today",
		"hours tomorrow",
		"what time do you open",
		"when do you open",
		"what time do you close",
		"when do you close",
		"are you open",
		"are you closed",
		"open today",
		"open tomorrow",
		"closed today",
		"closed tomorrow",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return normalized == "hours" || strings.HasPrefix(normalized, "hours ")
}

func asksAvailabilityQuestion(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" || asksBusinessHours(message) {
		return false
	}
	if strings.Contains(normalized, "available") &&
		(containsLoosePhrase(normalized, "time") ||
			containsLoosePhrase(normalized, "times") ||
			containsLoosePhrase(normalized, "slot") ||
			containsLoosePhrase(normalized, "slots") ||
			containsLoosePhrase(normalized, "spot") ||
			containsLoosePhrase(normalized, "spots")) {
		return true
	}
	signals := []string{
		"availability",
		"available time",
		"available times",
		"time available",
		"times available",
		"what time available",
		"what times available",
		"openings",
		"opening",
		"slots",
		"slot",
		"spots",
		"spot",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func asksStaffQuestion(message string, staff []StaffOption, activeStaff []StaffOption) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if matchStaff(message, staff) != nil || matchStaff(message, activeStaff) != nil {
		return true
	}
	signals := []string{
		"staff",
		"technician",
		"technicians",
		"tech",
		"techs",
		"nail tech",
		"nail techs",
		"who works",
		"who is working",
		"who do you have",
	}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func businessHoursAnswer(message string, periods []BusinessHourPeriod, authority string, cfg *RuntimeConfig, now func() time.Time) (string, []string, float64) {
	if len(periods) == 0 {
		switch strings.TrimSpace(authority) {
		case booking.SchedulingAuthorityOwnerManual:
			return "Business hours have not been configured for this salon yet. The owner can help with that. Would you like help with something else?", nil, 0.52
		case booking.SchedulingAuthorityExternalProvider:
			return "I do not have synced business hours from the POS yet. The owner needs to review hours before I can answer that. Would you like help with an appointment?", nil, 0.52
		case booking.SchedulingAuthorityManleAICalendar:
			return "Current internal business hours are not available yet. The owner needs to review the internal calendar setup. Would you like help with something else?", nil, 0.52
		default:
			return "Verified business hours are not available yet. The owner can help with that. Would you like help with something else?", nil, 0.52
		}
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if day, label, ok := requestedBusinessHourDay(message, loc, now); ok {
		matches := periodsForDay(periods, day)
		if len(matches) == 0 {
			return "I do not see open business hours for " + label + ". Would you like help checking another day?", nil, 0.9
		}
		return "Hours for " + label + " are " + formatBusinessHourRanges(matches) + ". Would you like help with an appointment?", answerBusinessHourIDs(matches), 0.94
	}
	today := periodsForDay(periods, int(now().In(loc).Weekday()))
	if len(today) == 0 {
		return "The salon is closed today. Which day would you like our hours for?", nil, 0.9
	}
	return "Today's hours are " + formatBusinessHourRanges(today) + ". Would you like hours for another day?", answerBusinessHourIDs(today), 0.92
}

func answerSchedulingAuthority(answerCtx *AIAnswerContext) string {
	if answerCtx == nil {
		return ""
	}
	return strings.TrimSpace(answerCtx.SchedulingAuthority)
}

func requestedBusinessHourDay(message string, loc *time.Location, now func() time.Time) (int, string, bool) {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return 0, "", false
	}
	if loc == nil {
		loc = time.UTC
	}
	current := now().In(loc)
	if strings.Contains(normalized, "today") {
		return int(current.Weekday()), "today", true
	}
	if strings.Contains(normalized, "tomorrow") {
		tomorrow := current.AddDate(0, 0, 1)
		return int(tomorrow.Weekday()), "tomorrow", true
	}
	if match := dateOnlyPattern.FindString(message); match != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", match, loc); err == nil {
			return int(parsed.Weekday()), parsed.Format("Monday, January 2"), true
		}
	}
	for day, name := range weekdayNames() {
		if containsLoosePhrase(normalized, strings.ToLower(name)) {
			return day, name, true
		}
	}
	return 0, "", false
}

func weekdayNames() map[int]string {
	return map[int]string{
		0: "Sunday",
		1: "Monday",
		2: "Tuesday",
		3: "Wednesday",
		4: "Thursday",
		5: "Friday",
		6: "Saturday",
	}
}

func periodsForDay(periods []BusinessHourPeriod, day int) []BusinessHourPeriod {
	out := make([]BusinessHourPeriod, 0)
	for _, period := range periods {
		if period.DayOfWeek == day {
			out = append(out, period)
		}
	}
	return out
}

func formatWeeklyBusinessHours(periods []BusinessHourPeriod) string {
	parts := make([]string, 0, 7)
	names := weekdayNames()
	for day := 0; day <= 6; day++ {
		dayPeriods := periodsForDay(periods, day)
		if len(dayPeriods) == 0 {
			continue
		}
		parts = append(parts, names[day]+" "+formatBusinessHourRanges(dayPeriods))
	}
	if len(parts) == 0 {
		return "not synced"
	}
	return strings.Join(parts, "; ")
}

func formatBusinessHourRanges(periods []BusinessHourPeriod) string {
	parts := make([]string, 0, len(periods))
	for _, period := range periods {
		start := formatLocalClock(period.StartLocalTime)
		end := formatLocalClock(period.EndLocalTime)
		if start == "" || end == "" {
			continue
		}
		parts = append(parts, start+" to "+end)
	}
	if len(parts) == 0 {
		return "not synced"
	}
	return joinHumanList(parts)
}

func formatLocalClock(value string) string {
	value = strings.TrimSpace(value)
	if value == "24:00" || value == "24:00:00" {
		return "12:00 AM"
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("3:04 PM")
		}
	}
	return ""
}

func staffAnswer(message string, staff []StaffOption, activeStaff []StaffOption) (string, []string, float64) {
	if matched := matchStaff(message, staff); matched != nil {
		return matched.Name + " is available for AI booking. Would you like help with an appointment?", []string{matched.ID}, 0.93
	}
	if matched := matchNonBookableStaff(message, activeStaff); matched != nil {
		return matched.Name + " is not enabled for AI booking. The owner can review requests for that technician. This is not a confirmed appointment.", []string{matched.ID}, 0.88
	}
	if len(staff) == 0 {
		return "I do not have synced AI-bookable staff yet. The owner needs to review technician requests. Would you like help with an appointment?", nil, 0.58
	}
	limited := limitStaff(staff, 6)
	names := make([]string, 0, len(limited))
	for _, member := range limited {
		if name := strings.TrimSpace(member.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "I can use anyone available for booking. Would you like help with an appointment?", nil, 0.68
	}
	prefix := "AI-bookable technicians include "
	if len(staff) > len(limited) {
		prefix = "Some AI-bookable technicians include "
	}
	return prefix + joinHumanList(names) + ", or anyone available. Would you like help with an appointment?", answerStaffIDs(limited), 0.86
}

func availabilityPromptForSession(session Session, services []ServiceOption) string {
	service := strings.TrimSpace(serviceSummary(session, services))
	date := strings.TrimSpace(session.RequestedDate)
	switch {
	case service == "":
		return "I can check real appointment availability. Which service should I check?"
	case date == "" && session.RequestedStartTime == nil:
		return "I can check real appointment availability for your " + service + ". What day should I check?"
	default:
		return "I can check real appointment availability for your " + service + ". What time works?"
	}
}

func answerServiceIDs(services []ServiceOption) []string {
	ids := make([]string, 0, len(services))
	for _, service := range services {
		if id := strings.TrimSpace(service.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func answerStaffIDs(staff []StaffOption) []string {
	ids := make([]string, 0, len(staff))
	for _, member := range staff {
		if id := strings.TrimSpace(member.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func answerBusinessHourIDs(periods []BusinessHourPeriod) []string {
	ids := make([]string, 0, len(periods))
	for _, period := range periods {
		if id := strings.TrimSpace(period.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func answerKnowledgeIDs(item KnowledgeSnippet) []string {
	if id := strings.TrimSpace(item.ID); id != "" {
		return []string{id}
	}
	return nil
}

func limitServices(services []ServiceOption, limit int) []ServiceOption {
	if limit <= 0 || len(services) <= limit {
		return services
	}
	return services[:limit]
}

func limitStaff(staff []StaffOption, limit int) []StaffOption {
	if limit <= 0 || len(staff) <= limit {
		return staff
	}
	return staff[:limit]
}

func knowledgeAnswerFromMatch(match *KnowledgeSnippet) string {
	if match == nil || strings.TrimSpace(match.Body) == "" {
		return ""
	}
	if hasUnsafeKnowledgeConfirmation(match.Body) {
		return "I can share salon policies, but I cannot confirm appointments unless the booking is completed successfully. Would you like help with an appointment?"
	}
	return truncateWords(match.Body, 34) + " Would you like help with an appointment?"
}
