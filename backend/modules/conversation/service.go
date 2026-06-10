package conversation

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
)

const defaultGreeting = "Thank you for calling. How can I help you today?"

var (
	phonePattern        = regexp.MustCompile(`(?:\+?1[\s.-]?)?(?:\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4})`)
	emailPattern        = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	dateTimePattern     = regexp.MustCompile(`(?i)(\d{4}-\d{2}-\d{2})(?:[ t]+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?)`)
	relativeTimePattern = regexp.MustCompile(`(?i)\b(today|tomorrow)\b\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	namePatterns        = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bmy name is\s+([^,.;]+)`),
		regexp.MustCompile(`(?i)\bthis is\s+([^,.;]+)`),
		regexp.MustCompile(`(?i)\bi am\s+([^,.;]+)`),
		regexp.MustCompile(`(?i)\bi'm\s+([^,.;]+)`),
	}
)

type Service struct {
	store          Store
	bookingTool    BookingTool
	replyGenerator ReplyGenerator
	now            func() time.Time
}

func NewService(store Store, bookingTool BookingTool) *Service {
	return &Service{
		store:       store,
		bookingTool: bookingTool,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetReplyGenerator(generator ReplyGenerator) {
	s.replyGenerator = generator
}

func (s *Service) Start(ctx context.Context, salonID string, ownerUserID string, req StartSessionRequest) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	req = normalizeStartRequest(req)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	cfg, err := s.store.GetRuntimeConfig(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return s.store.CreateSession(ctx, NewSessionRecord{
		SalonID:       salonID,
		OwnerUserID:   ownerUserID,
		Channel:       req.Channel,
		CustomerName:  req.CustomerName,
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
		InitialReply:  initialReply(cfg),
	})
}

func (s *Service) StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req StartPhoneCallRequest) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderCallID = strings.TrimSpace(req.ProviderCallID)
	req.FromPhone = validation.NormalizePhone(req.FromPhone)
	req.ToPhone = validation.NormalizePhone(req.ToPhone)
	if salonID == "" || ownerUserID == "" || req.Provider == "" || req.ProviderCallID == "" {
		return nil, ErrValidation
	}
	cfg, err := s.store.GetRuntimeConfig(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	return s.store.CreateSession(ctx, NewSessionRecord{
		SalonID:        salonID,
		OwnerUserID:    ownerUserID,
		Channel:        ChannelPhone,
		Provider:       req.Provider,
		ProviderCallID: req.ProviderCallID,
		InboundPhone:   req.FromPhone,
		OutboundPhone:  req.ToPhone,
		CustomerPhone:  req.FromPhone,
		InitialReply:   initialReply(cfg),
	})
}

func (s *Service) Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req MessageRequest) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	message := strings.TrimSpace(req.Message)
	if salonID == "" || ownerUserID == "" || sessionID == "" || message == "" {
		return nil, ErrValidation
	}
	session, err := s.store.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != StatusActive {
		return nil, ErrSessionClosed
	}
	cfg, err := s.store.GetRuntimeConfig(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	services, err := s.store.ListBookableServices(ctx, salonID)
	if err != nil {
		return nil, err
	}
	staff, err := s.store.ListBookableStaff(ctx, salonID)
	if err != nil {
		return nil, err
	}
	knowledge, err := s.store.ListActiveKnowledge(ctx, salonID)
	if err != nil {
		return nil, err
	}

	next := *session
	applyExtraction(&next, message, services, staff, timezoneLocation(cfg.Timezone), s.now)
	intent := resolveIntent(session.Intent, message, next)
	next.Intent = intent

	turn := TurnRecord{
		SalonID:         salonID,
		OwnerUserID:     ownerUserID,
		Session:         *session,
		CustomerMessage: message,
		Update: SessionUpdate{
			Status:             StatusActive,
			Intent:             intent,
			Outcome:            OutcomeCollecting,
			CustomerName:       next.CustomerName,
			CustomerPhone:      next.CustomerPhone,
			CustomerEmail:      next.CustomerEmail,
			ServiceID:          next.ServiceID,
			StaffID:            next.StaffID,
			RequestedStartTime: next.RequestedStartTime,
			Summary:            summaryFor(next, services, staff, cfg),
		},
	}

	if shouldHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll pass this to the owner so they can help directly. This is not a confirmed appointment.", services, staff, cfg)
	}

	if intent != IntentBooking {
		if answer := knowledgeAnswer(message, knowledge); answer != "" {
			turn.AIMessage = answer
		} else {
			turn.AIMessage = "I can help with appointments. What service would you like to book?"
		}
		s.applyReplyGenerator(ctx, &turn, next, cfg, "", knowledge)
		return s.store.SaveTurn(ctx, turn)
	}

	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. I can take the request for the owner, but this is not a confirmed appointment.", services, staff, cfg)
	}

	if missing := missingBookingField(next); missing != "" {
		turn.AIMessage = promptForMissingField(missing)
		s.applyReplyGenerator(ctx, &turn, next, cfg, missing, knowledge)
		return s.store.SaveTurn(ctx, turn)
	}

	return s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
}

func (s *Service) List(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Session, error) {
	return s.store.ListSessions(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), clampLimit(limit))
}

func (s *Service) Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	return s.store.GetSessionForOwner(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(sessionID))
}

func (s *Service) tryBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, "I could not reach the booking path, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
	}
	attempt, err := s.bookingTool.Create(ctx, turn.SalonID, ownerUserID, booking.CreateBookingRequest{
		Source:        bookingSourceForSession(session),
		CustomerName:  session.CustomerName,
		CustomerPhone: session.CustomerPhone,
		CustomerEmail: session.CustomerEmail,
		ServiceID:     session.ServiceID,
		StaffID:       session.StaffID,
		StartTime:     *session.RequestedStartTime,
		Notes:         bookingNotesForSession(session),
	})
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, "I could not confirm that in Square Appointments, so the owner needs to review it. This is not a confirmed appointment.", services, staff, cfg)
	}

	toolMessage := "Booking service returned fallback pending."
	outcome := OutcomeBookingFallbackPending
	status := StatusCompleted
	aiMessage := "I could not confirm that in Square Appointments, so I created a pending request for the owner. This is not a confirmed appointment."
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
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	s.applyReplyGenerator(ctx, &turn, session, cfg, "", knowledge)
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) applyReplyGenerator(ctx context.Context, turn *TurnRecord, session Session, cfg *RuntimeConfig, missing string, knowledge []KnowledgeSnippet) {
	if s.replyGenerator == nil || turn == nil || strings.TrimSpace(turn.AIMessage) == "" {
		return
	}
	result, err := s.replyGenerator.GenerateReply(ctx, ReplyGenerationRequest{
		SalonID:             turn.SalonID,
		SessionID:           session.ID,
		Channel:             session.Channel,
		Intent:              turn.Update.Intent,
		Outcome:             turn.Update.Outcome,
		CustomerMessage:     turn.CustomerMessage,
		SafeReply:           turn.AIMessage,
		SalonName:           salonName(cfg),
		BookingConfirmed:    turn.Update.Outcome == OutcomeBookingConfirmed && turn.Update.BookingAttemptID != "" && turn.Update.AppointmentID != "",
		FallbackOrHandoff:   turn.Update.Outcome == OutcomeBookingFallbackPending || turn.Update.Outcome == OutcomeAIDisabled || turn.Update.Outcome == OutcomeHandoffRequested,
		MissingBookingField: missing,
		Summary:             turn.Update.Summary,
		KnowledgeContext:    formatKnowledgeContext(knowledge),
	})
	if err != nil {
		return
	}
	if message := strings.TrimSpace(result.Message); message != "" {
		turn.AIMessage = message
	}
}

func (s *Service) saveHandoffTurn(ctx context.Context, turn TurnRecord, session Session, reason string, reply string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
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
	return s.store.SaveTurn(ctx, turn)
}

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
	if cfg != nil && strings.TrimSpace(cfg.AIGreeting) != "" {
		return strings.TrimSpace(cfg.AIGreeting)
	}
	return defaultGreeting
}

func salonName(cfg *RuntimeConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.SalonName)
}

func resolveIntent(current string, message string, session Session) string {
	if shouldHandoff(message) {
		return IntentHandoff
	}
	if current == IntentBooking || hasBookingSignal(message) || session.ServiceID != "" || session.RequestedStartTime != nil {
		return IntentBooking
	}
	return IntentUnknown
}

func applyExtraction(session *Session, message string, services []ServiceOption, staff []StaffOption, loc *time.Location, now func() time.Time) {
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
	if session.ServiceID == "" {
		if service := matchService(message, services); service != nil {
			session.ServiceID = service.ID
			session.ServiceName = service.Name
		}
	}
	if session.StaffID == "" {
		if member := matchStaff(message, staff); member != nil {
			session.StaffID = member.ID
			session.StaffName = member.Name
		}
	}
	if session.RequestedStartTime == nil {
		if requestedAt, ok := parseRequestedTime(message, loc, now); ok {
			session.RequestedStartTime = &requestedAt
		}
	}
	if session.CustomerName == "" {
		if name := extractName(message); name != "" {
			session.CustomerName = name
		} else if canTreatAsName(message, *session) {
			session.CustomerName = strings.TrimSpace(message)
		}
	}
}

func shouldHandoff(message string) bool {
	lower := strings.ToLower(message)
	triggers := []string{
		"human", "owner", "manager", "person", "representative", "complaint",
		"refund", "payment dispute", "dispute", "chargeback", "wedding", "party",
		"group booking", "large group", "talk to someone", "speak to someone",
	}
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

func hasBookingSignal(message string) bool {
	lower := strings.ToLower(message)
	signals := []string{"book", "booking", "appointment", "schedule", "reschedule", "manicure", "pedicure", "nail", "acrylic", "gel"}
	for _, signal := range signals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func missingBookingField(session Session) string {
	switch {
	case strings.TrimSpace(session.CustomerName) == "":
		return "customer_name"
	case strings.TrimSpace(session.CustomerPhone) == "":
		return "customer_phone"
	case strings.TrimSpace(session.ServiceID) == "":
		return "service"
	case strings.TrimSpace(session.StaffID) == "":
		return "staff"
	case session.RequestedStartTime == nil:
		return "requested_start_time"
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
	case "requested_start_time":
		return "What date and time would you like?"
	default:
		return "What else should I know?"
	}
}

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

func canTreatAsName(message string, session Session) bool {
	if session.Intent != IntentBooking || session.CustomerName != "" {
		return false
	}
	if session.CustomerPhone != "" || session.ServiceID != "" || session.StaffID != "" || session.RequestedStartTime != nil {
		return false
	}
	trimmed := strings.TrimSpace(message)
	if len(trimmed) < 3 || len(trimmed) > 80 {
		return false
	}
	if phonePattern.MatchString(trimmed) || emailPattern.MatchString(trimmed) || hasBookingSignal(trimmed) {
		return false
	}
	for _, r := range trimmed {
		if !(r == ' ' || r == '\'' || r == '-' || r == '.' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

func cleanName(raw string) string {
	name := strings.TrimSpace(raw)
	for _, marker := range []string{" phone ", " for ", " at ", " on ", " wants ", " would "} {
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

func matchService(message string, services []ServiceOption) *ServiceOption {
	lower := strings.ToLower(message)
	ordered := append([]ServiceOption(nil), services...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i].Name) > len(ordered[j].Name)
	})
	for _, service := range ordered {
		name := strings.ToLower(service.Name)
		if name != "" && strings.Contains(lower, name) {
			item := service
			return &item
		}
	}
	for _, service := range ordered {
		for _, token := range significantWords(service.Name) {
			if strings.Contains(lower, token) {
				item := service
				return &item
			}
		}
	}
	return nil
}

func matchStaff(message string, staff []StaffOption) *StaffOption {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "anyone") || strings.Contains(lower, "any technician") || strings.Contains(lower, "any tech") {
		if len(staff) == 0 {
			return nil
		}
		item := staff[0]
		return &item
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
	if match == nil || strings.TrimSpace(match.Body) == "" {
		return ""
	}
	if hasUnsafeKnowledgeConfirmation(match.Body) {
		return "I can share salon policies, but I cannot confirm appointments unless Square Appointments confirms the booking. Would you like help with an appointment?"
	}
	return truncateWords(match.Body, 34) + " Would you like help with an appointment?"
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
		item := knowledge[i]
		score := 0
		for _, token := range append(significantWords(item.Title), significantWords(item.Category)...) {
			if strings.Contains(lower, token) {
				score += 2
			}
		}
		for _, token := range significantWords(item.Body) {
			if strings.Contains(lower, token) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = &item
		}
	}
	if bestScore == 0 {
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
	return time.Time{}, false
}

func parseDateAndClock(date string, hourRaw string, minuteRaw string, meridiem string, loc *time.Location) (time.Time, error) {
	if hourRaw == "" {
		return time.Time{}, fmt.Errorf("hour is required")
	}
	hour, err := strconv.Atoi(hourRaw)
	if err != nil {
		return time.Time{}, err
	}
	minute := 0
	if minuteRaw != "" {
		minute, err = strconv.Atoi(minuteRaw)
		if err != nil {
			return time.Time{}, err
		}
	}
	meridiem = strings.ToLower(meridiem)
	if meridiem == "pm" && hour < 12 {
		hour += 12
	}
	if meridiem == "am" && hour == 12 {
		hour = 0
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("invalid clock")
	}
	parsedDate, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), hour, minute, 0, 0, loc), nil
}

func timezoneLocation(name string) *time.Location {
	if strings.TrimSpace(name) == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func summaryFor(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	parts := []string{}
	if cfg != nil && cfg.SalonName != "" {
		parts = append(parts, cfg.SalonName)
	}
	if session.CustomerName != "" {
		parts = append(parts, "customer "+session.CustomerName)
	}
	if session.CustomerPhone != "" {
		parts = append(parts, session.CustomerPhone)
	}
	if name := serviceName(session.ServiceID, services, session.ServiceName); name != "" {
		parts = append(parts, name)
	}
	if name := staffName(session.StaffID, staff, session.StaffName); name != "" {
		parts = append(parts, "with "+name)
	}
	if session.RequestedStartTime != nil {
		parts = append(parts, "requested "+session.RequestedStartTime.Format(time.RFC3339))
	}
	if len(parts) == 0 {
		return "Conversation needs owner review."
	}
	return strings.Join(parts, " · ")
}

func confirmedMessage(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	service := serviceName(session.ServiceID, services, session.ServiceName)
	member := staffName(session.StaffID, staff, session.StaffName)
	when := ""
	if session.RequestedStartTime != nil {
		loc := timezoneLocation("")
		if cfg != nil {
			loc = timezoneLocation(cfg.Timezone)
		}
		when = session.RequestedStartTime.In(loc).Format("Jan 2 at 3:04 PM")
	}
	details := strings.TrimSpace(strings.Join(nonEmpty([]string{service, "with " + member, when}), " "))
	if details == "" {
		return "Your appointment is confirmed in Square Appointments."
	}
	return "Your appointment is confirmed in Square Appointments for " + details + "."
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && value != "with" {
			out = append(out, value)
		}
	}
	return out
}

func serviceName(id string, services []ServiceOption, fallback string) string {
	for _, item := range services {
		if item.ID == id {
			return item.Name
		}
	}
	return fallback
}

func staffName(id string, staff []StaffOption, fallback string) string {
	for _, item := range staff {
		if item.ID == id {
			return item.Name
		}
	}
	return fallback
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 25
	}
	if limit > 100 {
		return 100
	}
	return limit
}
