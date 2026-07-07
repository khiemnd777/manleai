package conversation

import (
	"github.com/manleai/ai-receptionist/modules/booking"
	"strings"
	"time"
)

func shouldHandoff(message string) bool {
	normalized := normalizeLooseText(message)
	triggers := []string{
		"human", "owner", "manager", "person", "representative", "complaint",
		"refund", "payment dispute", "dispute", "chargeback", "wedding",
		"talk to someone", "speak to someone",
	}
	for _, trigger := range triggers {
		if containsLoosePhrase(normalized, trigger) {
			return true
		}
	}
	return false
}

func shouldComplaintHandoff(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	triggers := []string{
		"really bad",
		"very bad",
		"bad service",
		"not good",
		"not happy",
		"unhappy",
		"upset",
		"angry",
		"terrible",
		"horrible",
		"awful",
	}
	for _, trigger := range triggers {
		if strings.Contains(normalized, trigger) {
			return true
		}
	}
	return false
}

func hasBookingVerbSignal(message string) bool {
	normalized := normalizeLooseText(message)
	signals := []string{"book", "booking", "appointment", "schedule", "reschedule"}
	for _, signal := range signals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func hasRescheduleSignal(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	signals := []string{
		"reschedule",
		"re schedule",
		"move my appointment",
		"move the appointment",
		"change my appointment",
		"change the appointment",
		"change appointment",
		"switch my appointment",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
}

func hasCancelSignal(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" || hasCancelNegation(message) {
		return false
	}
	directSignals := []string{
		"cancel",
		"cancel appointment",
		"cancel my appointment",
		"cancel the appointment",
		"cancel booking",
		"cancel my booking",
		"cancel the booking",
		"cancel reservation",
		"cancel my reservation",
		"call off appointment",
		"call it off",
		"take me off the schedule",
		"remove me from the schedule",
		"delete my appointment",
		"drop my appointment",
		"don t need that appointment anymore",
		"do not need that appointment anymore",
		"don t need my appointment anymore",
		"do not need my appointment anymore",
	}
	for _, signal := range directSignals {
		if normalized == signal || containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	hasAppointmentTerm := false
	for _, term := range []string{"appointment", "booking", "reservation", "schedule"} {
		if containsLoosePhrase(normalized, term) {
			hasAppointmentTerm = true
			break
		}
	}
	if !hasAppointmentTerm {
		return false
	}
	unableSignals := []string{
		"can t make",
		"cannot make",
		"won t be able to come",
		"will not be able to come",
		"can t come",
		"cannot come",
		"not coming",
		"won t make it",
		"will not make it",
	}
	for _, signal := range unableSignals {
		if containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func hasCancelNegation(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	negations := []string{
		"don t cancel",
		"do not cancel",
		"dont cancel",
		"not cancel",
		"no cancel",
		"never mind cancel",
		"don t want to cancel",
		"do not want to cancel",
	}
	for _, negation := range negations {
		if normalized == negation || containsLoosePhrase(normalized, negation) {
			return true
		}
	}
	return false
}

func looksLikeCurrentBookingDraftCancel(message string, session Session) bool {
	if !hasBookingProgress(session) || bookingActionForSession(session) != BookingActionBook {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	hasDraftSignal := false
	for _, signal := range []string{"cancel that", "cancel it", "cancel this", "scratch that", "nevermind that", "never mind that", "forget that"} {
		if normalized == signal || containsLoosePhrase(normalized, signal) {
			hasDraftSignal = true
			break
		}
	}
	if !hasDraftSignal {
		return false
	}
	for _, term := range []string{"appointment", "booking", "reservation", "schedule"} {
		if containsLoosePhrase(normalized, term) {
			return false
		}
	}
	return true
}

func bookingActionForSession(session Session) string {
	action := strings.TrimSpace(session.BookingAction)
	if action == BookingActionReschedule {
		return BookingActionReschedule
	}
	if action == BookingActionCancel {
		return BookingActionCancel
	}
	return BookingActionBook
}

func clearNewRescheduleSlot(session *Session) {
	if session == nil {
		return
	}
	session.ServiceID = ""
	session.ServiceName = ""
	session.StaffID = ""
	session.StaffName = ""
	session.StaffSelectionMode = ""
	session.RequestedDate = ""
	session.RequestedStartTime = nil
	session.OfferedSlots = nil
	session.BookingSegments = nil
}

func clearCancelSelection(session *Session) {
	if session == nil {
		return
	}
	session.TargetAppointmentID = ""
	session.ServiceID = ""
	session.ServiceName = ""
	session.StaffID = ""
	session.StaffName = ""
	session.StaffSelectionMode = ""
	session.RequestedDate = ""
	session.RequestedStartTime = nil
	session.OfferedSlots = nil
	session.BookingSegments = nil
}

func rescheduleCandidatesFromAppointments(items []booking.AppointmentActionRef) []RescheduleCandidate {
	candidates := make([]RescheduleCandidate, 0, len(items))
	for _, item := range items {
		segments := rescheduleCandidateSegments(item)
		candidate := RescheduleCandidate{
			AppointmentID:      strings.TrimSpace(item.ID),
			ServiceLabel:       rescheduleServiceLabel(item),
			StaffLabel:         rescheduleStaffLabel(item),
			ServiceID:          strings.TrimSpace(item.Service.ID),
			StaffID:            strings.TrimSpace(item.Staff.ID),
			StaffSelectionMode: normalizeRescheduleStaffSelectionMode(item.StaffSelectionMode, item.Staff.ID),
			Segments:           segments,
			StartTime:          item.StartTime,
			EndTime:            item.EndTime,
		}
		if len(segments) > 0 {
			candidate.ServiceID = strings.TrimSpace(segments[0].ServiceID)
			candidate.StaffID = strings.TrimSpace(segments[0].StaffID)
			candidate.StaffSelectionMode = normalizeRescheduleStaffSelectionMode(segments[0].StaffSelectionMode, segments[0].StaffID)
		}
		if candidate.AppointmentID != "" {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func rescheduleCandidateSegments(item booking.AppointmentActionRef) []booking.BookingSegmentRequest {
	segments := make([]booking.BookingSegmentRequest, 0, len(item.Segments))
	for _, segment := range item.Segments {
		serviceID := strings.TrimSpace(segment.Service.ID)
		staffID := strings.TrimSpace(segment.Staff.ID)
		if serviceID == "" {
			continue
		}
		segments = append(segments, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: normalizeRescheduleStaffSelectionMode(segment.StaffSelectionMode, staffID),
		})
	}
	if len(segments) > 0 {
		return segments
	}
	serviceID := strings.TrimSpace(item.Service.ID)
	if serviceID == "" {
		return nil
	}
	staffID := strings.TrimSpace(item.Staff.ID)
	return []booking.BookingSegmentRequest{{
		ServiceID:          serviceID,
		StaffID:            staffID,
		StaffSelectionMode: normalizeRescheduleStaffSelectionMode(item.StaffSelectionMode, staffID),
	}}
}

func normalizeRescheduleStaffSelectionMode(mode string, staffID string) string {
	mode = strings.TrimSpace(mode)
	if strings.TrimSpace(staffID) != "" {
		return booking.StaffSelectionSpecific
	}
	if mode == booking.StaffSelectionAnyone || mode == booking.StaffSelectionSpecific {
		return mode
	}
	return booking.StaffSelectionAnyone
}

func rescheduleServiceLabel(item booking.AppointmentActionRef) string {
	names := []string{}
	seen := map[string]bool{}
	for _, segment := range item.Segments {
		addServiceName(&names, seen, segment.Service.Name)
	}
	addServiceName(&names, seen, item.Service.Name)
	return joinHumanList(names)
}

func rescheduleStaffLabel(item booking.AppointmentActionRef) string {
	names := []string{}
	seen := map[string]bool{}
	for _, segment := range item.Segments {
		addStaffName(&names, seen, segment.Staff.Name)
	}
	addStaffName(&names, seen, item.Staff.Name)
	return joinHumanList(names)
}

func addServiceName(names *[]string, seen map[string]bool, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := strings.ToLower(name)
	if seen[key] {
		return
	}
	seen[key] = true
	*names = append(*names, name)
}

func selectRescheduleCandidate(message string, candidates []RescheduleCandidate, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, loc *time.Location, now func() time.Time) *RescheduleCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 && isAffirmativeOnly(message) {
		return &candidates[0]
	}
	if selected := selectRescheduleCandidateByDateTime(message, candidates, loc, now); selected != nil {
		return selected
	}
	if selected := selectRescheduleCandidateByService(message, candidates, services, aliases, categoryAliases); selected != nil {
		return selected
	}
	if selected := selectRescheduleCandidateByOrdinal(message, candidates); selected != nil {
		return selected
	}
	return nil
}

func selectRescheduleCandidateByDateTime(message string, candidates []RescheduleCandidate, loc *time.Location, now func() time.Time) *RescheduleCandidate {
	if loc == nil {
		loc = time.UTC
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if requestedAt, ok := parseRequestedTime(message, loc, now); ok {
		for i := range candidates {
			if candidates[i].StartTime.Equal(requestedAt) {
				return &candidates[i]
			}
		}
		return nil
	}
	if selected := selectRescheduleCandidateByClock(message, candidates, loc); selected != nil {
		return selected
	}
	if requestedDate := preferredDateFromMessage(message, nil, loc, now); requestedDate != "" {
		matches := make([]int, 0, len(candidates))
		for i := range candidates {
			if candidates[i].StartTime.In(loc).Format("2006-01-02") == requestedDate {
				matches = append(matches, i)
			}
		}
		if len(matches) == 1 {
			return &candidates[matches[0]]
		}
	}
	return nil
}

func selectRescheduleCandidateByClock(message string, candidates []RescheduleCandidate, loc *time.Location) *RescheduleCandidate {
	if loc == nil {
		loc = time.UTC
	}
	match := timeWithMeridiemPattern.FindStringSubmatch(message)
	if len(match) == 0 {
		return nil
	}
	parsed, err := parseDateAndClock("2000-01-01", match[1], match[2], match[3], loc)
	if err != nil {
		return nil
	}
	hour, minute := parsed.In(loc).Hour(), parsed.In(loc).Minute()
	matches := make([]int, 0, len(candidates))
	for i := range candidates {
		start := candidates[i].StartTime.In(loc)
		if start.Hour() == hour && start.Minute() == minute {
			matches = append(matches, i)
		}
	}
	if len(matches) == 1 {
		return &candidates[matches[0]]
	}
	return nil
}

func selectRescheduleCandidateByService(message string, candidates []RescheduleCandidate, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) *RescheduleCandidate {
	servicePool := rescheduleCandidateServicePool(candidates, services)
	if len(servicePool) == 0 {
		return nil
	}
	result := interpretServiceWithCategoryAliases(message, servicePool, aliases, categoryAliases)
	matchedServiceIDs := serviceIDsFromServiceUnderstanding(result)
	if len(matchedServiceIDs) == 0 {
		return nil
	}
	matched := map[string]bool{}
	for _, serviceID := range matchedServiceIDs {
		matched[strings.TrimSpace(serviceID)] = true
	}
	matches := make([]int, 0, len(candidates))
	for i := range candidates {
		if rescheduleCandidateHasService(candidates[i], matched) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 1 {
		return &candidates[matches[0]]
	}
	return nil
}

func serviceIDsFromServiceUnderstanding(result serviceUnderstandingResult) []string {
	switch result.Status {
	case serviceUnderstandingStatusSelected, serviceUnderstandingStatusAmbiguous:
		return serviceIDsFromOptions(result.Candidates)
	default:
		return nil
	}
}

func rescheduleCandidateServicePool(candidates []RescheduleCandidate, services []ServiceOption) []ServiceOption {
	servicesByID := map[string]ServiceOption{}
	for _, service := range services {
		serviceID := strings.TrimSpace(service.ID)
		if serviceID == "" {
			continue
		}
		servicesByID[serviceID] = service
	}
	pool := make([]ServiceOption, 0, len(services))
	for _, candidate := range candidates {
		for _, serviceID := range rescheduleCandidateServiceIDs(candidate) {
			service, ok := servicesByID[serviceID]
			if !ok {
				service = ServiceOption{ID: serviceID, Name: strings.TrimSpace(candidate.ServiceLabel)}
			}
			pool = appendUniqueService(pool, service)
		}
	}
	return orderedServices(pool)
}

func rescheduleCandidateServiceIDs(candidate RescheduleCandidate) []string {
	ids := make([]string, 0, 1+len(candidate.Segments))
	if serviceID := strings.TrimSpace(candidate.ServiceID); serviceID != "" {
		ids = append(ids, serviceID)
	}
	for _, segment := range candidate.Segments {
		if serviceID := strings.TrimSpace(segment.ServiceID); serviceID != "" {
			ids = append(ids, serviceID)
		}
	}
	return uniqueStrings(ids)
}

func rescheduleCandidateHasService(candidate RescheduleCandidate, serviceIDs map[string]bool) bool {
	for _, serviceID := range rescheduleCandidateServiceIDs(candidate) {
		if serviceIDs[strings.TrimSpace(serviceID)] {
			return true
		}
	}
	return false
}

func selectRescheduleCandidateByOrdinal(message string, candidates []RescheduleCandidate) *RescheduleCandidate {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return nil
	}
	selections := []struct {
		Index int
		Terms []string
	}{
		{0, []string{"first", "first one", "the first", "the first one", "number one", "number 1", "option one", "option 1", "appointment one", "appointment 1", "1st"}},
		{1, []string{"second", "second one", "the second", "the second one", "number two", "number 2", "option two", "option 2", "appointment two", "appointment 2", "2nd"}},
		{2, []string{"third", "third one", "the third", "the third one", "number three", "number 3", "option three", "option 3", "appointment three", "appointment 3", "3rd"}},
	}
	for _, selection := range selections {
		if selection.Index >= len(candidates) {
			continue
		}
		for _, term := range selection.Terms {
			if normalized == term || containsLoosePhrase(normalized, term) {
				return &candidates[selection.Index]
			}
		}
	}
	switch normalized {
	case "one", "1":
		return &candidates[0]
	case "two", "2":
		if len(candidates) > 1 {
			return &candidates[1]
		}
	case "three", "3":
		if len(candidates) > 2 {
			return &candidates[2]
		}
	}
	return nil
}

func applyRescheduleCandidate(session *Session, candidate RescheduleCandidate) {
	if session == nil {
		return
	}
	session.TargetAppointmentID = strings.TrimSpace(candidate.AppointmentID)
	session.ServiceID = strings.TrimSpace(candidate.ServiceID)
	session.ServiceName = strings.TrimSpace(candidate.ServiceLabel)
	session.StaffID = strings.TrimSpace(candidate.StaffID)
	session.StaffName = strings.TrimSpace(candidate.StaffLabel)
	session.StaffSelectionMode = normalizeRescheduleStaffSelectionMode(candidate.StaffSelectionMode, candidate.StaffID)
	session.RequestedDate = ""
	session.RequestedStartTime = nil
	session.OfferedSlots = nil
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), candidate.Segments...)
	if len(session.BookingSegments) == 0 && session.ServiceID != "" {
		session.BookingSegments = []booking.BookingSegmentRequest{{
			ServiceID:          session.ServiceID,
			StaffID:            session.StaffID,
			StaffSelectionMode: session.StaffSelectionMode,
		}}
	}
}

func applyCancelCandidate(session *Session, candidate RescheduleCandidate, loc *time.Location) {
	applyRescheduleCandidate(session, candidate)
	if session == nil {
		return
	}
	if loc == nil {
		loc = time.UTC
	}
	start := candidate.StartTime
	if !start.IsZero() {
		session.RequestedStartTime = &start
		session.RequestedDate = start.In(loc).Format("2006-01-02")
	}
}

func rescheduleTargetAutoSafe(session Session) bool {
	if strings.TrimSpace(session.TargetAppointmentID) == "" ||
		strings.TrimSpace(session.ServiceID) == "" ||
		strings.TrimSpace(session.StaffID) == "" ||
		!hasStaffAssignment(session) {
		return false
	}
	return len(session.BookingSegments) == 1
}

func applyRelativeRescheduleDate(session *Session, message string, loc *time.Location) bool {
	if session == nil || !hasNextDayRescheduleSignal(message) {
		return false
	}
	candidate := selectedRescheduleCandidate(*session)
	if candidate == nil || candidate.StartTime.IsZero() {
		return false
	}
	if loc == nil {
		loc = time.UTC
	}
	requestedDate := candidate.StartTime.In(loc).AddDate(0, 0, 1).Format("2006-01-02")
	applyRequestedDate(session, requestedDate)
	return true
}

func selectedRescheduleCandidate(session Session) *RescheduleCandidate {
	targetID := strings.TrimSpace(session.TargetAppointmentID)
	if targetID == "" {
		return nil
	}
	for i := range session.RescheduleCandidates {
		if strings.TrimSpace(session.RescheduleCandidates[i].AppointmentID) == targetID {
			return &session.RescheduleCandidates[i]
		}
	}
	return nil
}

func isNegativeOnly(message string) bool {
	normalized := normalizeLooseText(message)
	switch normalized {
	case "no", "nope", "nah", "not that one", "not this one", "wrong one":
		return true
	default:
		return false
	}
}

func cancelTargetConfirmed(message string) bool {
	if isAffirmativeOnly(message) {
		return true
	}
	normalized := normalizeLooseText(message)
	if normalized == "" || hasCancelNegation(message) {
		return false
	}
	confirmSignals := []string{
		"yes cancel",
		"cancel it",
		"cancel that",
		"please cancel",
		"go ahead and cancel",
		"that one",
		"that s the one",
		"that is the one",
		"correct",
		"right",
	}
	for _, signal := range confirmSignals {
		if normalized == signal || containsLoosePhrase(normalized, signal) {
			return true
		}
	}
	return false
}

func isCancelTargetFiller(message string) bool {
	normalized := normalizeLooseText(message)
	switch normalized {
	case "cancel", "to cancel", "i want to cancel", "i need to cancel", "cancel appointment", "cancel my appointment":
		return true
	default:
		return false
	}
}

func hasNextDayRescheduleSignal(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	signals := []string{
		"next day",
		"the next day",
		"following day",
		"the following day",
		"day after",
		"the day after",
	}
	for _, signal := range signals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
}

func isRescheduleTargetFiller(message string) bool {
	normalized := normalizeLooseText(message)
	switch normalized {
	case "reschedule", "to reschedule", "i want to reschedule", "i need to reschedule", "change appointment", "change my appointment":
		return true
	default:
		return false
	}
}

func shouldHandoffRepeatedRescheduleNewTime(session Session, message string) bool {
	return recentRescheduleNewTimePromptCount(session) >= 2 && looksLikeUnparsedDateOrTime(message)
}

func recentRescheduleNewTimePromptCount(session Session) int {
	count := 0
	seenAI := 0
	for i := len(session.Transcript) - 1; i >= 0 && seenAI < 4; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		seenAI++
		if transcriptMessageAsksForRescheduleNewTime(msg) {
			count++
		}
	}
	return count
}

func transcriptMessageAsksForRescheduleNewTime(msg TranscriptMessage) bool {
	if field := metadataString(msg.Metadata, "next_required_field"); field == "requested_start_time" || field == "requested_time" {
		return true
	}
	normalized := normalizeLooseText(msg.Body)
	return strings.Contains(normalized, "new day and time") ||
		strings.Contains(normalized, "new date and time") ||
		strings.Contains(normalized, "what time would you like") ||
		strings.Contains(normalized, "what time work")
}

func looksLikeUnparsedDateOrTime(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	normalized := normalizeLooseText(message)
	if hasNextDayRescheduleSignal(message) ||
		dateOnlyPattern.MatchString(message) ||
		monthDateOnlyPattern.MatchString(message) ||
		timeWithMeridiemPattern.MatchString(message) {
		return true
	}
	if _, _, ok := weekdayFromMessage(message); ok {
		return true
	}
	timeWords := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}
	minuteWords := []string{"fifteen", "thirty", "forty five", "fortyfive", "o clock"}
	hasHourWord := false
	for _, word := range timeWords {
		if containsLoosePhrase(normalized, word) {
			hasHourWord = true
			break
		}
	}
	if hasHourWord {
		for _, word := range minuteWords {
			if strings.Contains(normalized, word) {
				return true
			}
		}
	}
	return false
}

func rescheduleSingleCandidatePrompt(candidate RescheduleCandidate, loc *time.Location) string {
	return "I found your appointment " + rescheduleCandidatePhrase(candidate, loc) + ". Is this the appointment you want to reschedule?"
}

func cancelSingleCandidatePrompt(candidate RescheduleCandidate, loc *time.Location) string {
	return "I found your appointment " + rescheduleCandidatePhrase(candidate, loc) + ". Is this the appointment you want to cancel?"
}

func rescheduleMultipleCandidatesPrompt(candidates []RescheduleCandidate, loc *time.Location) string {
	parts := []string{"I found more than one upcoming appointment. Which one should I reschedule?"}
	for i, candidate := range candidates {
		parts = append(parts, rescheduleCandidateOptionPhrase(i+1, candidate, loc))
	}
	return strings.Join(parts, " ")
}

func cancelMultipleCandidatesPrompt(candidates []RescheduleCandidate, loc *time.Location) string {
	parts := []string{"I found more than one upcoming appointment. Which one should I cancel?"}
	for i, candidate := range candidates {
		parts = append(parts, rescheduleCandidateOptionPhrase(i+1, candidate, loc))
	}
	return strings.Join(parts, " ")
}

func rescheduleCandidateOptionPhrase(index int, candidate RescheduleCandidate, loc *time.Location) string {
	phrase := strings.TrimSpace(rescheduleCandidatePhrase(candidate, loc))
	phrase = strings.TrimPrefix(phrase, "for ")
	if phrase == "" {
		return strings.TrimSuffix(ordinalSpeechLabel(index), ",") + "."
	}
	return ordinalSpeechLabel(index) + " " + phrase + "."
}

func rescheduleConciseTargetPrompt(candidates []RescheduleCandidate, loc *time.Location) string {
	parts := make([]string, 0, len(candidates))
	for i, candidate := range candidates {
		if i >= 3 {
			break
		}
		parts = append(parts, rescheduleConciseCandidatePhrase(i+1, candidate, loc))
	}
	if len(parts) == 0 {
		return "Which appointment should I reschedule?"
	}
	return "Which one should I reschedule, " + joinHumanList(parts) + "?"
}

func cancelConciseTargetPrompt(candidates []RescheduleCandidate, loc *time.Location) string {
	parts := make([]string, 0, len(candidates))
	for i, candidate := range candidates {
		if i >= 3 {
			break
		}
		parts = append(parts, rescheduleConciseCandidatePhrase(i+1, candidate, loc))
	}
	if len(parts) == 0 {
		return "Which appointment should I cancel?"
	}
	return "Which one should I cancel, " + joinHumanList(parts) + "?"
}

func rescheduleConciseCandidatePhrase(index int, candidate RescheduleCandidate, loc *time.Location) string {
	label := "the " + strings.TrimSuffix(ordinalLabel(index), ":")
	if loc == nil {
		loc = time.UTC
	}
	if candidate.StartTime.IsZero() {
		return label
	}
	return label + " at " + candidate.StartTime.In(loc).Format("3:04 PM")
}

func rescheduleCandidatePhrase(candidate RescheduleCandidate, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	parts := []string{}
	if service := strings.TrimSpace(candidate.ServiceLabel); service != "" {
		parts = append(parts, "for "+service)
	}
	if !candidate.StartTime.IsZero() {
		parts = append(parts, "on "+candidate.StartTime.In(loc).Format("Monday, January 2 at 3:04 PM"))
	}
	if staff := strings.TrimSpace(candidate.StaffLabel); staff != "" {
		parts = append(parts, "with "+staff)
	}
	return strings.Join(parts, " ")
}
