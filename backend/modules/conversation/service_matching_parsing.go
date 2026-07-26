package conversation

import (
	"fmt"
	"github.com/manleai/ai-receptionist/modules/booking"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func matchService(message string, services []ServiceOption) *ServiceOption {
	matches := matchServices(message, services)
	if len(matches) > 0 {
		item := matches[0]
		return &item
	}
	return nil
}

func matchServices(message string, services []ServiceOption) []ServiceOption {
	result := interpretService(message, services)
	if result.Status != serviceUnderstandingStatusSelected {
		return nil
	}
	return append([]ServiceOption(nil), result.Candidates...)
}

func removeContainedServiceMatches(matches []serviceMatch) []serviceMatch {
	out := make([]serviceMatch, 0, len(matches))
	for i, item := range matches {
		contained := false
		for j, other := range matches {
			if i == j {
				continue
			}
			if item.index >= other.index && item.end <= other.end && len(item.service.Name) < len(other.service.Name) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, item)
		}
	}
	return out
}

func removeAmbiguousTokenMatches(matches []serviceMatch) []serviceMatch {
	if len(matches) <= 1 {
		return matches
	}
	counts := map[string]int{}
	for _, item := range matches {
		counts[item.token]++
	}
	out := make([]serviceMatch, 0, len(matches))
	for _, item := range matches {
		if counts[item.token] == 1 {
			out = append(out, item)
		}
	}
	return out
}

func matchStaff(message string, staff []StaffOption) *StaffOption {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "anyone") || strings.Contains(lower, "any technician") || strings.Contains(lower, "any tech") {
		return nil
	}
	for _, member := range staff {
		name := strings.ToLower(member.Name)
		if name != "" && strings.Contains(lower, name) {
			item := member
			return &item
		}
		parts := strings.Fields(name)
		if len(parts) > 0 && len(parts[0]) > 2 && strings.Contains(lower, parts[0]) {
			item := member
			return &item
		}
	}
	return nil
}

func matchNonBookableStaff(message string, staff []StaffOption) *StaffOption {
	match := matchStaff(message, staff)
	if match == nil || match.AIBookable {
		return nil
	}
	return match
}

func nonBookableStaffReply(member StaffOption) string {
	name := strings.TrimSpace(member.Name)
	if name == "" {
		name = "That technician"
	}
	return name + " is not enabled for AI booking. I will pass this request to the owner for review. This is not a confirmed appointment."
}

func significantWords(value string) []string {
	parts := strings.Fields(strings.ToLower(value))
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, " ,.;:/")
		if len(part) >= 4 {
			words = append(words, part)
		}
	}
	return words
}

func knowledgeAnswer(message string, knowledge []KnowledgeSnippet) string {
	match := bestKnowledgeMatch(message, knowledge)
	return knowledgeAnswerFromMatch(match)
}

func formatKnowledgeContext(knowledge []KnowledgeSnippet) string {
	if len(knowledge) == 0 {
		return ""
	}
	lines := make([]string, 0, len(knowledge))
	for _, item := range knowledge {
		title := strings.TrimSpace(item.Title)
		body := truncateWords(item.Body, 40)
		if title == "" || body == "" {
			continue
		}
		category := strings.TrimSpace(item.Category)
		if category == "" {
			category = "knowledge"
		}
		lines = append(lines, fmt.Sprintf("%s: %s - %s", category, title, body))
	}
	return strings.Join(lines, "\n")
}

func bestKnowledgeMatch(message string, knowledge []KnowledgeSnippet) *KnowledgeSnippet {
	lower := strings.ToLower(message)
	bestScore := 0
	var best *KnowledgeSnippet
	for i := range knowledge {
		score := 0
		for _, token := range append(significantWords(knowledge[i].Title), significantWords(knowledge[i].Category)...) {
			if strings.Contains(lower, token) {
				score += 2
			}
		}
		for _, token := range significantWords(knowledge[i].Body) {
			if strings.Contains(lower, token) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = &knowledge[i]
		}
	}
	if bestScore < 2 {
		return nil
	}
	return best
}

func truncateWords(value string, maxWords int) string {
	words := strings.Fields(strings.TrimSpace(value))
	if len(words) <= maxWords {
		return strings.TrimSpace(value)
	}
	return strings.Join(words[:maxWords], " ") + "..."
}

func hasUnsafeKnowledgeConfirmation(value string) bool {
	lower := strings.ToLower(value)
	unsafeAlways := []string{
		"you are booked",
		"you're booked",
		"appointment is set",
		"all set for",
		"see you at",
	}
	for _, phrase := range unsafeAlways {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if !strings.Contains(lower, "confirmed") {
		return false
	}
	for _, phrase := range []string{"not confirmed", "not a confirmed", "cannot confirm", "could not confirm", "not yet confirmed"} {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	return true
}

func parseRequestedTime(message string, loc *time.Location, now func() time.Time) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if match := dateTimePattern.FindStringSubmatch(message); len(match) > 0 {
		parsed, err := parseDateAndClock(match[1], match[2], match[3], match[4], loc)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	if match := monthDateTimePattern.FindStringSubmatch(message); len(match) > 0 {
		if date, ok := dateFromMonthDay(match[1], match[2], loc, now); ok {
			parsed, err := parseDateAndClock(date, match[3], match[4], match[5], loc)
			if err == nil {
				return parsed.UTC(), true
			}
		}
	}
	if match := timeMonthDatePattern.FindStringSubmatch(message); len(match) > 0 {
		if date, ok := dateFromMonthDay(match[4], match[5], loc, now); ok {
			parsed, err := parseDateAndClock(date, match[1], match[2], match[3], loc)
			if err == nil {
				return parsed.UTC(), true
			}
		}
	}
	if match := relativeTimePattern.FindStringSubmatch(message); len(match) > 0 {
		base := now().In(loc)
		if strings.EqualFold(match[1], "tomorrow") {
			base = base.AddDate(0, 0, 1)
		}
		date := base.Format("2006-01-02")
		parsed, err := parseDateAndClock(date, match[2], match[3], match[4], loc)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	if date := preferredDateFromMessage(message, nil, loc, now); date != "" {
		if parsed, ok := parseTimeOnlyForDate(message, date, loc); ok {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseTimeOnlyForDate(message string, requestedDate string, loc *time.Location) (time.Time, bool) {
	requestedDate = strings.TrimSpace(requestedDate)
	if requestedDate == "" {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	if match := timeWithMeridiemPattern.FindStringSubmatch(message); len(match) > 0 {
		parsed, err := parseDateAndClock(requestedDate, match[1], match[2], match[3], loc)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	if parsed, ok := parseOClockCandidateForDate(message, requestedDate, loc); ok {
		return parsed.UTC(), true
	}
	if parsed, ok := parseBareClockCandidateForDate(message, requestedDate, loc); ok {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func parseOClockCandidateForDate(message string, requestedDate string, loc *time.Location) (time.Time, bool) {
	candidates := oClockCandidatesFromText(message)
	if len(candidates) != 1 {
		return time.Time{}, false
	}
	minutes := candidates[0]
	return parseDateAndClockMinutes(requestedDate, minutes, loc)
}

func parseBareClockCandidateForDate(message string, requestedDate string, loc *time.Location) (time.Time, bool) {
	candidates := bareClockCandidatesFromText(message)
	if len(candidates) != 1 {
		return time.Time{}, false
	}
	return parseDateAndClockMinutes(requestedDate, candidates[0], loc)
}

func parseDateAndClockMinutes(requestedDate string, minutes int, loc *time.Location) (time.Time, bool) {
	if minutes < 0 || minutes >= 24*60 {
		return time.Time{}, false
	}
	parsedDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(requestedDate), loc)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), minutes/60, minutes%60, 0, 0, loc), true
}

func preferredDateFromMessage(message string, requestedStartTime *time.Time, loc *time.Location, now func() time.Time) string {
	if loc == nil {
		loc = time.UTC
	}
	if requestedStartTime != nil && !requestedStartTime.IsZero() {
		return requestedStartTime.In(loc).Format("2006-01-02")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if match := dateOnlyPattern.FindStringSubmatch(message); len(match) > 1 {
		return match[1]
	}
	if match := monthDateOnlyPattern.FindStringSubmatch(message); len(match) > 0 {
		if date, ok := dateFromMonthDay(match[1], match[2], loc, now); ok {
			return date
		}
	}
	if match := relativeDayPattern.FindStringSubmatch(message); len(match) > 1 {
		base := now().In(loc)
		if strings.EqualFold(match[1], "tomorrow") {
			base = base.AddDate(0, 0, 1)
		}
		return base.Format("2006-01-02")
	}
	if weekday, nextWeek, ok := weekdayFromMessage(message); ok {
		return dateForWeekday(now().In(loc), weekday, nextWeek).Format("2006-01-02")
	}
	return ""
}

func dateFromMonthDay(monthRaw string, dayRaw string, loc *time.Location, now func() time.Time) (string, bool) {
	month, ok := monthFromText(monthRaw)
	if !ok {
		return "", false
	}
	day, err := strconv.Atoi(strings.TrimSpace(dayRaw))
	if err != nil || day < 1 || day > 31 {
		return "", false
	}
	if loc == nil {
		loc = time.UTC
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	base := now().In(loc)
	candidate := time.Date(base.Year(), month, day, 0, 0, 0, 0, loc)
	if candidate.Month() != month || candidate.Day() != day {
		return "", false
	}
	today := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, loc)
	if candidate.Before(today) {
		candidate = time.Date(base.Year()+1, month, day, 0, 0, 0, 0, loc)
	}
	return candidate.Format("2006-01-02"), true
}

func monthFromText(value string) (time.Month, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "january", "jan":
		return time.January, true
	case "february", "feb":
		return time.February, true
	case "march", "mar":
		return time.March, true
	case "april", "apr":
		return time.April, true
	case "may":
		return time.May, true
	case "june", "jun":
		return time.June, true
	case "july", "jul":
		return time.July, true
	case "august", "aug":
		return time.August, true
	case "september", "sep", "sept":
		return time.September, true
	case "october", "oct":
		return time.October, true
	case "november", "nov":
		return time.November, true
	case "december", "dec":
		return time.December, true
	default:
		return 0, false
	}
}

func preferredDateForAvailability(session Session, message string, loc *time.Location, now func() time.Time) string {
	if session.RequestedStartTime != nil {
		return preferredDateFromMessage("", session.RequestedStartTime, loc, now)
	}
	if date := strings.TrimSpace(session.RequestedDate); date != "" {
		return date
	}
	return preferredDateFromMessage(message, nil, loc, now)
}

func applyRequestedStartTime(session *Session, requestedAt time.Time, loc *time.Location) {
	if session == nil || requestedAt.IsZero() {
		return
	}
	start := requestedAt.UTC()
	session.RequestedStartTime = &start
	clearSelectedAvailabilityQuote(session)
	clearSlotTimePreference(session)
	if loc == nil {
		loc = time.UTC
	}
	session.RequestedDate = start.In(loc).Format("2006-01-02")
	session.OfferedSlots = nil
}

func applyRequestedDate(session *Session, requestedDate string) {
	if session == nil {
		return
	}
	requestedDate = strings.TrimSpace(requestedDate)
	if requestedDate == "" {
		return
	}
	if session.RequestedDate != requestedDate {
		session.RequestedDate = requestedDate
		session.RequestedStartTime = nil
		clearSelectedAvailabilityQuote(session)
		session.OfferedSlots = nil
		return
	}
	if session.RequestedDate == "" {
		session.RequestedDate = requestedDate
	}
}

func weekdayFromMessage(message string) (time.Weekday, bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return time.Sunday, false, false
	}
	nextWeek := strings.Contains(lower, "next ") || strings.Contains(lower, "tuần sau") || strings.Contains(lower, "tuan sau")
	checks := []struct {
		weekday time.Weekday
		signals []string
	}{
		{time.Monday, []string{"monday", "mon", "thứ hai", "thu hai"}},
		{time.Tuesday, []string{"tuesday", "tues", "tue", "thứ ba", "thu ba"}},
		{time.Wednesday, []string{"wednesday", "wed", "thứ tư", "thu tu"}},
		{time.Thursday, []string{"thursday", "thurs", "thur", "thứ năm", "thu nam"}},
		{time.Friday, []string{"friday", "fri", "thứ sáu", "thu sau"}},
		{time.Saturday, []string{"saturday", "sat", "thứ bảy", "thu bay"}},
		{time.Sunday, []string{"sunday", "sun", "chủ nhật", "chu nhat"}},
	}
	for _, check := range checks {
		for _, signal := range check.signals {
			if containsDateSignal(lower, signal) {
				return check.weekday, nextWeek, true
			}
		}
	}
	return time.Sunday, false, false
}

func containsDateSignal(lower string, signal string) bool {
	if strings.Contains(signal, " ") || strings.ContainsAny(signal, "ứủảăâêôơư") {
		return strings.Contains(lower, signal)
	}
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(signal) + `\b`).MatchString(lower)
}

func dateForWeekday(base time.Time, target time.Weekday, nextWeek bool) time.Time {
	start := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location())
	days := (int(target) - int(start.Weekday()) + 7) % 7
	if nextWeek && days == 0 {
		days = 7
	}
	return start.AddDate(0, 0, days)
}

func offeredSlotRejectionForMessage(message string, session Session, loc *time.Location) (slotRejection, bool) {
	if len(session.OfferedSlots) == 0 || !hasOfferedSlotRejectionSignal(message) {
		return slotRejection{}, false
	}
	candidates := clockCandidatesFromText(message)
	if len(candidates) == 0 {
		return slotRejection{}, false
	}
	minutes := matchingOfferedSlotMinutes(candidates, session.OfferedSlots, loc)
	if len(minutes) != 1 {
		return slotRejection{}, false
	}
	preference := slotTimePreference{
		Direction: slotRejectionDirection(message),
		Minutes:   minutes[0],
	}
	remaining := filterOfferedSlotsByPreference(session.OfferedSlots, preference, loc)
	return slotRejection{Preference: preference, Remaining: remaining}, true
}

func directionalSlotTimePreferenceForMessage(message string, session Session) (slotTimePreference, bool) {
	if len(session.OfferedSlots) == 0 || missingBookingField(session) != "requested_time" {
		return slotTimePreference{}, false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" || !stateScopedDirectionalSlotTimePattern.MatchString(normalized) {
		return slotTimePreference{}, false
	}
	direction := ""
	switch {
	case containsAnyLoosePhrase(normalized, []string{"no earlier than", "not before"}):
		direction = TimePreferenceAfter
	case containsAnyLoosePhrase(normalized, []string{"no later than", "not after"}):
		direction = TimePreferenceBefore
	case containsAnyLoosePhrase(normalized, []string{"before", "earlier than", "trước", "truoc", "sớm hơn", "som hon"}):
		direction = TimePreferenceBefore
	case containsAnyLoosePhrase(normalized, []string{"after", "later than", "sau", "trễ hơn", "tre hon"}):
		direction = TimePreferenceAfter
	default:
		return slotTimePreference{}, false
	}
	candidates := clockCandidatesFromText(message)
	if len(candidates) != 1 {
		return slotTimePreference{}, false
	}
	return slotTimePreference{Direction: direction, Minutes: candidates[0]}, true
}

func hasOfferedSlotRejectionSignal(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	signals := []string{
		"too early",
		"too late",
		"does not work",
		"doesn t work",
		"doesnt work",
		"not work",
		"not that",
		"not this",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return strings.HasPrefix(normalized, "not ")
}

func slotRejectionDirection(message string) string {
	normalized := normalizeLooseText(message)
	switch {
	case strings.Contains(normalized, "too early"):
		return "after"
	case strings.Contains(normalized, "too late"):
		return "before"
	default:
		return "not_at"
	}
}

func matchingOfferedSlotMinutes(candidates []int, slots []OfferedSlot, loc *time.Location) []int {
	if loc == nil {
		loc = time.UTC
	}
	candidateSet := map[int]bool{}
	for _, candidate := range candidates {
		candidateSet[candidate] = true
	}
	seen := map[int]bool{}
	out := []int{}
	for _, slot := range slots {
		local := slot.StartTime.In(loc)
		minutes := local.Hour()*60 + local.Minute()
		if !candidateSet[minutes] || seen[minutes] {
			continue
		}
		seen[minutes] = true
		out = append(out, minutes)
	}
	return out
}

func filterOfferedSlotsByPreference(slots []OfferedSlot, preference slotTimePreference, loc *time.Location) []OfferedSlot {
	if len(slots) == 0 || preference.Minutes < 0 {
		return slots
	}
	if loc == nil {
		loc = time.UTC
	}
	out := make([]OfferedSlot, 0, len(slots))
	for _, slot := range slots {
		local := slot.StartTime.In(loc)
		minutes := local.Hour()*60 + local.Minute()
		keep := true
		switch preference.Direction {
		case "after":
			keep = minutes > preference.Minutes
		case "before":
			keep = minutes < preference.Minutes
		case TimePreferenceExact:
			keep = minutes == preference.Minutes
		default:
			keep = minutes != preference.Minutes
		}
		if keep {
			out = append(out, slot)
		}
	}
	return out
}

func rejectedSlotNoRemainingReply(direction string) string {
	switch direction {
	case "after":
		return "I understand that time is too early. What later time works?"
	case "before":
		return "I understand that time is too late. What earlier time works?"
	default:
		return "No problem. What other time works?"
	}
}

func applySlotRejectionMetadata(turn *TurnRecord, rejection slotRejection) {
	if turn == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"rejected_slot_minutes":        rejection.Preference.Minutes,
		"rejected_slot_direction":      rejection.Preference.Direction,
		"remaining_offered_slot_count": len(rejection.Remaining),
	})
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"slot_time_preference_direction": rejection.Preference.Direction,
		"slot_time_preference_minutes":   rejection.Preference.Minutes,
		"slot_time_preference_source":    "offered_slot_rejection",
	})
}

func applyDirectionalSlotTimePreferenceMetadata(turn *TurnRecord, preference slotTimePreference, remaining int) {
	if turn == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"slot_time_preference_direction": preference.Direction,
		"slot_time_preference_minutes":   preference.Minutes,
		"remaining_offered_slot_count":   remaining,
	})
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"slot_time_preference_direction": preference.Direction,
		"slot_time_preference_minutes":   preference.Minutes,
		"slot_time_preference_source":    "directional_constraint",
	})
}

func activeSlotTimePreference(session Session) (slotTimePreference, bool) {
	if preference, ok := normalizedSlotTimePreference(session.DialogState.TimePreference); ok {
		return preference, true
	}
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		direction := metadataString(msg.Metadata, "slot_time_preference_direction")
		minutes, ok := metadataInt(msg.Metadata, "slot_time_preference_minutes")
		if direction == "" || !ok {
			continue
		}
		return slotTimePreference{Direction: direction, Minutes: minutes}, true
	}
	return slotTimePreference{}, false
}

func normalizedSlotTimePreference(value *TimePreference) (slotTimePreference, bool) {
	if value == nil || value.Minutes < 0 || value.Minutes >= 24*60 {
		return slotTimePreference{}, false
	}
	direction := strings.TrimSpace(value.Direction)
	switch direction {
	case TimePreferenceBefore, TimePreferenceAfter, TimePreferenceExact:
		return slotTimePreference{Direction: direction, Minutes: value.Minutes}, true
	default:
		return slotTimePreference{}, false
	}
}

func setSlotTimePreference(session *Session, preference slotTimePreference) bool {
	if session == nil {
		return false
	}
	normalized, ok := normalizedSlotTimePreference(&preference)
	if !ok {
		return false
	}
	state := normalizedDialogState(session.DialogState)
	changed := state.TimePreference == nil || state.TimePreference.Direction != normalized.Direction || state.TimePreference.Minutes != normalized.Minutes
	stored := TimePreference(normalized)
	state.TimePreference = &stored
	session.DialogState = state
	return changed
}

func clearSlotTimePreference(session *Session) {
	if session == nil {
		return
	}
	state := normalizedDialogState(session.DialogState)
	state.TimePreference = nil
	session.DialogState = state
}

func applyActiveSlotTimePreferenceToOfferedSlots(session *Session, loc *time.Location) {
	if session == nil || len(session.OfferedSlots) == 0 {
		return
	}
	preference, ok := activeSlotTimePreference(*session)
	if !ok {
		return
	}
	session.OfferedSlots = filterOfferedSlotsByPreference(session.OfferedSlots, preference, loc)
}

func selectOfferedSlot(message string, slots []OfferedSlot, loc *time.Location) *OfferedSlot {
	if selected := offeredSlotForClockCandidates(clockCandidatesFromText(message), slots, loc); selected != nil {
		return selected
	}
	if selected := offeredSlotForStaffName(message, slots); selected != nil {
		return selected
	}
	if selected := offeredSlotForAlternativeStaffIntent(message, slots); selected != nil {
		return selected
	}
	index, ok := selectedSlotIndex(message)
	if ok && index >= 0 && index < len(slots) {
		slot := slots[index]
		return &slot
	}
	return nil
}

func offeredSlotForStaffName(message string, slots []OfferedSlot) *OfferedSlot {
	if len(slots) == 0 {
		return nil
	}
	lower := strings.ToLower(message)
	matches := []OfferedSlot{}
	for _, slot := range slots {
		names := offeredSlotStaffNames(slot)
		for _, name := range names {
			if staffNameMentioned(lower, name) {
				matches = append(matches, slot)
				break
			}
		}
	}
	if len(matches) != 1 {
		return nil
	}
	slot := matches[0]
	return &slot
}

func offeredSlotForAlternativeStaffIntent(message string, slots []OfferedSlot) *OfferedSlot {
	if len(slots) == 0 {
		return nil
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return nil
	}
	alternative := customerRequestedAnyone(message) ||
		strings.Contains(normalized, "another technician") ||
		strings.Contains(normalized, "another tech") ||
		strings.Contains(normalized, "someone else") ||
		strings.Contains(normalized, "same time")
	if !alternative {
		return nil
	}
	matches := []OfferedSlot{}
	for _, slot := range slots {
		if slotUsesAnyone(slot) {
			matches = append(matches, slot)
		}
	}
	if len(matches) != 1 {
		return nil
	}
	slot := matches[0]
	return &slot
}

func offeredSlotStaffNames(slot OfferedSlot) []string {
	names := []string{}
	seen := map[string]bool{}
	addStaffName(&names, seen, slot.StaffName)
	for _, segment := range slot.Segments {
		addStaffName(&names, seen, segment.StaffName)
	}
	return names
}

func staffNameMentioned(lowerMessage string, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if strings.Contains(lowerMessage, name) {
		return true
	}
	parts := strings.Fields(name)
	return len(parts) > 0 && len(parts[0]) > 2 && strings.Contains(lowerMessage, parts[0])
}

func selectedSlotIndex(message string) (int, bool) {
	lower := strings.ToLower(message)
	checks := []struct {
		index   int
		signals []string
	}{
		{0, []string{"first", "1st", "number 1", "number one", "option 1", "option one", "the 1"}},
		{1, []string{"second", "2nd", "number 2", "number two", "option 2", "option two", "the 2"}},
		{2, []string{"third", "3rd", "number 3", "number three", "option 3", "option three", "the 3"}},
	}
	for _, check := range checks {
		for _, signal := range check.signals {
			if strings.Contains(lower, signal) {
				return check.index, true
			}
		}
	}
	trimmed := strings.TrimSpace(lower)
	switch trimmed {
	case "1", "1.", "1)":
		return 0, true
	case "2", "2.", "2)":
		return 1, true
	case "3", "3.", "3)":
		return 2, true
	default:
		return 0, false
	}
}

var (
	stateScopedOfferedSlotSelectorPattern = regexp.MustCompile(`^(?:(?:i ll|i will|please|let s) (?:take|choose|pick|go with|do|have|want) )?(?:the )?(?:(?:option|number) )?(?:first|second|third|one|two|three|1st|2nd|3rd|1|2|3)(?: (?:one|option))?(?: (?:work|works|for me|is fine|is good|is okay|please|thank you|thanks))*$`)
	stateScopedSlotAffirmativePattern     = regexp.MustCompile(`^(?:yes|yeah|yep|ok|okay|sure|correct|right|that works|works for me|sounds good)(?: please)?$`)
	stateScopedOfferedSlotClockPattern    = regexp.MustCompile(`^(?:(?:i d|i would|i ll|i will|i|please|let s) (?:like|take|choose|pick|go with|do|have|want|prefer) (?:the )?)?(?:(?:opening|slot|time) (?:at )?)?(?:at )?(?:[0-9]{1,2}|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)(?: [0-5][0-9])? ?(?:a m|p m|am|pm|bpm|tm)(?: (?:opening|slot|time))?(?: (?:please|works|works for me|is fine|is good|is okay|thank you|thanks))*$`)
	stateScopedDirectionalSlotTimePattern = regexp.MustCompile(`^(?:(?:(?:could|can|would) you (?:keep it|make it|find something|show me) )|(?:(?:i d|i would|i ll|i will|i|please) (?:like|want|need|prefer) (?:something |anything |it )?)|(?:anything |something ))?(?:no earlier than|no later than|not before|not after|earlier than|later than|before|after|trước|truoc|sớm hơn|som hon|sau|trễ hơn|tre hon) (?:[0-9]{1,2}|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)(?: [0-5][0-9])? (?:a m|p m|am|pm|bpm|tm)(?: (?:works|would work|is fine|is good|is okay|please|if possible))*$`)
)

func stateScopedOfferedSlotSelection(message string, session Session, loc *time.Location) bool {
	if len(session.OfferedSlots) == 0 || normalizedDialogState(session.DialogState).Pending != nil {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if stateScopedOfferedSlotSelectorPattern.MatchString(normalized) {
		selected := selectOfferedSlot(message, session.OfferedSlots, loc)
		return selected != nil && offeredSlotMatchesServiceSelection(*selected, session)
	}
	if stateScopedOfferedSlotClockPattern.MatchString(normalized) {
		selected := offeredSlotForClockCandidates(clockCandidatesFromText(message), session.OfferedSlots, loc)
		return selected != nil && offeredSlotMatchesServiceSelection(*selected, session)
	}
	if stateScopedSlotAffirmativePattern.MatchString(normalized) {
		selected := selectConfirmedOfferedSlot(message, session, loc)
		return selected != nil && offeredSlotMatchesServiceSelection(*selected, session)
	}
	return false
}

func selectConfirmedOfferedSlot(message string, session Session, loc *time.Location) *OfferedSlot {
	if len(session.OfferedSlots) == 0 || !isAffirmativeSlotReply(message) {
		return nil
	}
	last := lastAITranscriptMessage(session)
	if last == "" {
		return nil
	}
	candidates := confirmationClockCandidates(last)
	if len(candidates) == 0 && looksLikeSlotConfirmationPrompt(last) {
		allCandidates := clockCandidatesFromText(last)
		if len(allCandidates) == 1 {
			candidates = allCandidates
		}
	}
	return offeredSlotForClockCandidates(candidates, session.OfferedSlots, loc)
}

func confirmationClockCandidates(message string) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, pattern := range slotConfirmationPromptPatterns {
		for _, match := range pattern.FindAllStringSubmatch(message, -1) {
			if len(match) < 2 {
				continue
			}
			for _, candidate := range clockCandidatesFromText(match[1]) {
				if seen[candidate] {
					continue
				}
				seen[candidate] = true
				out = append(out, candidate)
			}
		}
	}
	return out
}

func looksLikeSlotConfirmationPrompt(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	return (strings.Contains(lower, "does") && strings.Contains(lower, "work")) ||
		strings.Contains(lower, "would you like") ||
		strings.Contains(lower, "do you want") ||
		strings.Contains(lower, "should i book")
}

func isAffirmativeSlotReply(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	lower = strings.Trim(lower, " .,!?:;-")
	if lower == "" {
		return false
	}
	exact := []string{"yes", "yeah", "yep", "ok", "okay", "sure", "correct", "right"}
	for _, item := range exact {
		if lower == item {
			return true
		}
	}
	return strings.HasPrefix(lower, "yes ") ||
		strings.Contains(lower, "that works") ||
		strings.Contains(lower, "works for me") ||
		strings.Contains(lower, "sounds good") ||
		strings.Contains(lower, "i want to") ||
		strings.Contains(lower, "i would like that") ||
		strings.Contains(lower, "book it")
}

func clockCandidatesFromText(message string) []int {
	out := []int{}
	seen := map[int]bool{}
	add := func(minutes int, ok bool) {
		if !ok || seen[minutes] {
			return
		}
		seen[minutes] = true
		out = append(out, minutes)
	}
	for _, match := range offeredSlotNumericTimePattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 4 {
			continue
		}
		hour, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		minute := 0
		if strings.TrimSpace(match[2]) != "" {
			parsed, err := strconv.Atoi(match[2])
			if err != nil {
				continue
			}
			minute = parsed
		}
		add(clockMinutes(hour, minute, match[3]))
	}
	for _, match := range offeredSlotWordTimePattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 4 {
			continue
		}
		hour, ok := spokenHour(match[1])
		if !ok {
			continue
		}
		minute, ok := spokenMinute(match[2])
		if !ok {
			continue
		}
		add(clockMinutes(hour, minute, match[3]))
	}
	for _, minutes := range oClockCandidatesFromText(message) {
		add(minutes, true)
	}
	return out
}

func oClockCandidatesFromText(message string) []int {
	out := []int{}
	seen := map[int]bool{}
	add := func(minutes int, ok bool) {
		if !ok || seen[minutes] {
			return
		}
		seen[minutes] = true
		out = append(out, minutes)
	}
	for _, match := range offeredSlotNumericOClockPattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 3 {
			continue
		}
		hour, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		add(clockMinutes(hour, 0, match[2]))
	}
	for _, match := range offeredSlotWordOClockPattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 3 {
			continue
		}
		hour, ok := spokenHour(match[1])
		if !ok {
			continue
		}
		add(clockMinutes(hour, 0, match[2]))
	}
	return out
}

func bareClockCandidatesFromText(message string) []int {
	out := []int{}
	seen := map[int]bool{}
	add := func(minutes int, ok bool) {
		if !ok || seen[minutes] {
			return
		}
		seen[minutes] = true
		out = append(out, minutes)
	}
	for _, match := range bareClockWithColonPattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 3 {
			continue
		}
		hour, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		minute, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		add(clockMinutesWithoutMeridiem(hour, minute))
	}
	for _, match := range bareClockWithPrefixPattern.FindAllStringSubmatch(message, -1) {
		if len(match) < 2 {
			continue
		}
		hour, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		add(clockMinutesWithoutMeridiem(hour, 0))
	}
	return out
}

func offeredSlotForClockCandidates(candidates []int, slots []OfferedSlot, loc *time.Location) *OfferedSlot {
	if len(candidates) == 0 || len(slots) == 0 {
		return nil
	}
	if loc == nil {
		loc = time.UTC
	}
	candidateSet := map[int]bool{}
	for _, candidate := range candidates {
		candidateSet[candidate] = true
	}
	matches := []OfferedSlot{}
	for _, slot := range slots {
		local := slot.StartTime.In(loc)
		minutes := local.Hour()*60 + local.Minute()
		if candidateSet[minutes] {
			matches = append(matches, slot)
		}
	}
	if len(matches) != 1 {
		return nil
	}
	slot := matches[0]
	return &slot
}

func clockMinutes(hour int, minute int, meridiem string) (int, bool) {
	if hour < 1 || hour > 12 || minute < 0 || minute > 59 {
		return 0, false
	}
	switch normalizeMeridiem(meridiem) {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour != 12 {
			hour += 12
		}
	default:
		return 0, false
	}
	return hour*60 + minute, true
}

func clockMinutesWithoutMeridiem(hour int, minute int) (int, bool) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func normalizeMeridiem(value string) string {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	switch cleaned {
	case "am":
		return "am"
	case "pm", "bpm", "tm":
		return "pm"
	default:
		return ""
	}
}

func spokenHour(value string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "one":
		return 1, true
	case "two":
		return 2, true
	case "three":
		return 3, true
	case "four":
		return 4, true
	case "five":
		return 5, true
	case "six":
		return 6, true
	case "seven":
		return 7, true
	case "eight":
		return 8, true
	case "nine":
		return 9, true
	case "ten":
		return 10, true
	case "eleven":
		return 11, true
	case "twelve":
		return 12, true
	default:
		return 0, false
	}
}

func spokenMinute(value string) (int, bool) {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	cleaned = strings.ReplaceAll(cleaned, "-", " ")
	switch cleaned {
	case "":
		return 0, true
	case "fifteen":
		return 15, true
	case "thirty":
		return 30, true
	case "forty five":
		return 45, true
	}
	if strings.HasPrefix(cleaned, "oh ") {
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "oh "))
	}
	minute, err := strconv.Atoi(cleaned)
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}
	return minute, true
}

func customerRequestedAnyone(message string) bool {
	lower := strings.ToLower(message)
	signals := []string{"anyone", "any technician", "any tech", "whoever is available", "any available"}
	for _, signal := range signals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func applySelectedOfferedSlot(session *Session, slot OfferedSlot) {
	if session == nil {
		return
	}
	start := slot.StartTime
	session.RequestedStartTime = &start
	session.AvailabilityQuoteID = strings.TrimSpace(slot.AvailabilityQuoteID)
	session.SlotFingerprint = strings.TrimSpace(slot.SlotFingerprint)
	clearSlotTimePreference(session)
	session.RequestedDate = start.Format("2006-01-02")
	session.StaffID = slot.StaffID
	session.StaffName = slot.StaffName
	session.StaffSelectionMode = offeredSlotStaffSelectionMode(slot)
	segments := bookingSegmentsFromOfferedSlot(slot)
	if len(segments) == 0 {
		segments = availabilitySegmentsForSession(*session, offeredSlotStaffSelectionMode(slot))
		if len(segments) == 1 {
			segments[0].StaffID = strings.TrimSpace(slot.StaffID)
			segments[0].StaffSelectionMode = offeredSlotStaffSelectionMode(slot)
		}
	}
	session.BookingSegments = segments
	if session.StaffID == "" && len(slot.Segments) > 0 {
		session.StaffID = slot.Segments[0].StaffID
		session.StaffName = slot.Segments[0].StaffName
	}
	session.OfferedSlots = nil
}

func clearSelectedAvailabilityQuote(session *Session) {
	if session == nil {
		return
	}
	session.AvailabilityQuoteID = ""
	session.SlotFingerprint = ""
}

func bookingSegmentsFromServices(services []ServiceOption, session Session) []booking.BookingSegmentRequest {
	if len(services) == 0 {
		return nil
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	out := make([]booking.BookingSegmentRequest, 0, len(services))
	for _, service := range services {
		serviceID := strings.TrimSpace(service.ID)
		if serviceID == "" {
			continue
		}
		out = append(out, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: mode,
		})
	}
	return out
}

func applySpecificStaffToBookingSegments(session *Session, member StaffOption) {
	if session == nil || len(session.BookingSegments) == 0 {
		return
	}
	for i := range session.BookingSegments {
		session.BookingSegments[i].StaffID = member.ID
		session.BookingSegments[i].StaffSelectionMode = booking.StaffSelectionSpecific
	}
}

func clearBookingSegmentsStaffSelection(session *Session) {
	if session == nil || len(session.BookingSegments) == 0 {
		return
	}
	for i := range session.BookingSegments {
		session.BookingSegments[i].StaffID = ""
		session.BookingSegments[i].StaffSelectionMode = booking.StaffSelectionAnyone
	}
}

func availabilitySegmentsForSession(session Session, staffSelectionMode string) []booking.BookingSegmentRequest {
	if len(session.BookingSegments) > 0 {
		out := make([]booking.BookingSegmentRequest, 0, len(session.BookingSegments))
		for _, segment := range session.BookingSegments {
			serviceID := strings.TrimSpace(segment.ServiceID)
			if serviceID == "" {
				continue
			}
			mode := normalizeConversationStaffSelectionMode(firstNonEmpty(segment.StaffSelectionMode, staffSelectionMode))
			if mode == "" {
				mode = staffSelectionMode
			}
			staffID := strings.TrimSpace(segment.StaffID)
			if mode == booking.StaffSelectionAnyone {
				staffID = ""
			} else if staffID == "" {
				staffID = strings.TrimSpace(session.StaffID)
			}
			out = append(out, booking.BookingSegmentRequest{
				ServiceID:          serviceID,
				StaffID:            staffID,
				StaffSelectionMode: mode,
				GuestReference:     strings.TrimSpace(segment.GuestReference),
				Quantity:           segment.Quantity,
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	serviceID := strings.TrimSpace(session.ServiceID)
	if serviceID == "" {
		return nil
	}
	return []booking.BookingSegmentRequest{{
		ServiceID:          serviceID,
		StaffID:            staffIDForAvailability(session),
		StaffSelectionMode: staffSelectionMode,
	}}
}

func bookingSegmentsForCreate(session Session) []booking.BookingSegmentRequest {
	if len(session.BookingSegments) > 0 {
		out := make([]booking.BookingSegmentRequest, 0, len(session.BookingSegments))
		for _, segment := range session.BookingSegments {
			serviceID := strings.TrimSpace(segment.ServiceID)
			if serviceID == "" {
				continue
			}
			mode := normalizeConversationStaffSelectionMode(firstNonEmpty(segment.StaffSelectionMode, session.StaffSelectionMode))
			if mode == "" {
				mode = staffSelectionModeForSession(session)
			}
			staffID := strings.TrimSpace(segment.StaffID)
			if staffID == "" {
				staffID = strings.TrimSpace(session.StaffID)
			}
			out = append(out, booking.BookingSegmentRequest{
				ServiceID:          serviceID,
				StaffID:            staffID,
				StaffSelectionMode: mode,
				GuestReference:     strings.TrimSpace(segment.GuestReference),
				Quantity:           segment.Quantity,
			})
		}
		if len(out) > 0 {
			return out
		}
	}
	serviceID := strings.TrimSpace(session.ServiceID)
	if serviceID == "" {
		return nil
	}
	return []booking.BookingSegmentRequest{{
		ServiceID:          serviceID,
		StaffID:            strings.TrimSpace(session.StaffID),
		StaffSelectionMode: staffSelectionModeForSession(session),
	}}
}

func bookingSegmentsFromOfferedSlot(slot OfferedSlot) []booking.BookingSegmentRequest {
	if len(slot.Segments) == 0 {
		return nil
	}
	out := make([]booking.BookingSegmentRequest, 0, len(slot.Segments))
	for _, segment := range slot.Segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		mode := normalizeConversationStaffSelectionMode(firstNonEmpty(segment.StaffSelectionMode, slot.StaffSelectionMode))
		if mode == "" {
			mode = booking.StaffSelectionSpecific
		}
		out = append(out, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            firstNonEmpty(segment.StaffID, slot.StaffID),
			StaffSelectionMode: mode,
		})
	}
	return out
}

func staffSelectionModeForServiceRequest(session Session) string {
	mode := normalizeConversationStaffSelectionMode(session.StaffSelectionMode)
	if mode == booking.StaffSelectionAnyone || strings.TrimSpace(session.StaffID) == "" {
		return booking.StaffSelectionAnyone
	}
	return booking.StaffSelectionSpecific
}

func staffSelectionModeForAvailability(session Session) string {
	mode := normalizeConversationStaffSelectionMode(session.StaffSelectionMode)
	if mode == booking.StaffSelectionAnyone {
		return booking.StaffSelectionAnyone
	}
	if strings.TrimSpace(session.StaffID) == "" && !bookingSegmentsHaveStaff(session.BookingSegments) {
		return booking.StaffSelectionAnyone
	}
	return booking.StaffSelectionSpecific
}

func staffIDForAvailability(session Session) string {
	if staffSelectionModeForAvailability(session) == booking.StaffSelectionAnyone {
		return ""
	}
	return strings.TrimSpace(session.StaffID)
}

func staffSelectionModeForSession(session Session) string {
	if mode := normalizeConversationStaffSelectionMode(session.StaffSelectionMode); mode != "" {
		return mode
	}
	if len(session.BookingSegments) > 0 {
		if mode := normalizeConversationStaffSelectionMode(session.BookingSegments[0].StaffSelectionMode); mode != "" {
			return mode
		}
	}
	if strings.TrimSpace(session.StaffID) == "" {
		return booking.StaffSelectionAnyone
	}
	return booking.StaffSelectionSpecific
}

func offeredSlotStaffSelectionMode(slot OfferedSlot) string {
	if mode := normalizeConversationStaffSelectionMode(slot.StaffSelectionMode); mode != "" {
		return mode
	}
	for _, segment := range slot.Segments {
		if mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode); mode != "" {
			return mode
		}
	}
	return booking.StaffSelectionSpecific
}

func slotUsesAnyone(slot OfferedSlot) bool {
	if normalizeConversationStaffSelectionMode(slot.StaffSelectionMode) == booking.StaffSelectionAnyone {
		return true
	}
	for _, segment := range slot.Segments {
		if normalizeConversationStaffSelectionMode(segment.StaffSelectionMode) == booking.StaffSelectionAnyone {
			return true
		}
	}
	return false
}

func sessionUsesAnyone(session Session) bool {
	return staffSelectionModeForSession(session) == booking.StaffSelectionAnyone
}

func hasStaffAssignment(session Session) bool {
	if session.DialogState.SchedulingRequestOnly && normalizeConversationStaffSelectionMode(session.StaffSelectionMode) == booking.StaffSelectionAnyone {
		return true
	}
	if strings.TrimSpace(session.StaffID) != "" {
		return true
	}
	return bookingSegmentsHaveStaff(session.BookingSegments)
}

func bookingSegmentsHaveStaff(segments []booking.BookingSegmentRequest) bool {
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment.StaffID) == "" {
			return false
		}
	}
	return true
}

func normalizeConversationStaffSelectionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case booking.StaffSelectionSpecific:
		return booking.StaffSelectionSpecific
	case booking.StaffSelectionAnyone:
		return booking.StaffSelectionAnyone
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
