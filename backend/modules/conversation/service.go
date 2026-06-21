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
	dateOnlyPattern     = regexp.MustCompile(`(?i)\b(\d{4}-\d{2}-\d{2})\b`)
	relativeDayPattern  = regexp.MustCompile(`(?i)\b(today|tomorrow)\b`)
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
	selectedOfferedSlot := false
	applyExtraction(&next, message, services, staff, timezoneLocation(cfg.Timezone), s.now)
	if selected := selectOfferedSlot(message, session.OfferedSlots); selected != nil {
		applySelectedOfferedSlot(&next, *selected)
		selectedOfferedSlot = true
	}
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
			StaffSelectionMode: staffSelectionModeForSession(next),
			RequestedStartTime: next.RequestedStartTime,
			OfferedSlots:       next.OfferedSlots,
			BookingSegments:    next.BookingSegments,
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

	if next.ServiceID != "" && next.RequestedStartTime != nil && !selectedOfferedSlot {
		available, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check Square Appointments availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
		}
		if !available {
			s.applyReplyGenerator(ctx, &turn, next, cfg, "requested_start_time", knowledge)
			return s.store.SaveTurn(ctx, turn)
		}
	}

	if missing := missingBookingField(next); missing != "" {
		if missing == "requested_start_time" {
			preferredDate := preferredDateFromMessage(message, nil, timezoneLocation(cfg.Timezone), s.now)
			if preferredDate != "" && next.ServiceID != "" {
				if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, preferredDate, false, cfg); err != nil {
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check Square Appointments availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
				}
				s.applyReplyGenerator(ctx, &turn, next, cfg, missing, knowledge)
				return s.store.SaveTurn(ctx, turn)
			}
		}
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

func (s *Service) applyAvailabilityForRequestedTime(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, error) {
	if session == nil || session.RequestedStartTime == nil {
		return false, nil
	}
	preferredDate := preferredDateFromMessage("", session.RequestedStartTime, timezoneLocation(cfg.Timezone), s.now)
	if preferredDate == "" {
		return false, nil
	}
	result, err := s.availableSlots(ctx, turn.SalonID, ownerUserID, *session, preferredDate)
	if err != nil {
		return false, err
	}
	for _, slot := range result.Slots {
		if !slot.StartTime.Equal(*session.RequestedStartTime) {
			continue
		}
		if session.StaffID != "" && slot.StaffID != session.StaffID {
			continue
		}
		applySelectedOfferedSlot(session, offeredSlotFromAvailability(result, slot))
		syncTurnUpdate(turn, *session, services, staff, cfg)
		return true, nil
	}
	applyAvailabilityOffer(turn, session, services, staff, cfg, result, true)
	return false, nil
}

func (s *Service) offerAvailableSlots(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, preferredDate string, unavailableRequestedTime bool, cfg *RuntimeConfig) error {
	result, err := s.availableSlots(ctx, turn.SalonID, ownerUserID, *session, preferredDate)
	if err != nil {
		return err
	}
	applyAvailabilityOffer(turn, session, services, staff, cfg, result, unavailableRequestedTime)
	return nil
}

func (s *Service) availableSlots(ctx context.Context, salonID string, ownerUserID string, session Session, preferredDate string) (*booking.AvailabilityResult, error) {
	if s.bookingTool == nil {
		return nil, fmt.Errorf("booking tool is unavailable")
	}
	staffSelectionMode := staffSelectionModeForAvailability(session)
	return s.bookingTool.AvailableSlots(ctx, salonID, ownerUserID, booking.AvailabilityRequest{
		ServiceID:          session.ServiceID,
		StaffID:            staffIDForAvailability(session),
		StaffSelectionMode: staffSelectionMode,
		Segments:           availabilitySegmentsForSession(session, staffSelectionMode),
		PreferredDate:      preferredDate,
		Limit:              3,
	})
}

func applyAvailabilityOffer(turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, result *booking.AvailabilityResult, unavailableRequestedTime bool) {
	offered := offeredSlotsFromAvailability(result)
	session.RequestedStartTime = nil
	session.OfferedSlots = offered
	turn.ToolMessage = availabilityToolMessage(len(offered))
	if len(offered) == 0 {
		turn.AIMessage = "I do not see open times for that day in Square Appointments. What other day works?"
	} else {
		turn.AIMessage = formatSlotOffer(offered, timezoneLocation(cfg.Timezone), unavailableRequestedTime)
	}
	syncTurnUpdate(turn, *session, services, staff, cfg)
}

func offeredSlotsFromAvailability(result *booking.AvailabilityResult) []OfferedSlot {
	if result == nil || len(result.Slots) == 0 {
		return nil
	}
	limit := len(result.Slots)
	if limit > 3 {
		limit = 3
	}
	out := make([]OfferedSlot, 0, limit)
	for _, slot := range result.Slots {
		if len(out) >= limit {
			break
		}
		if slot.StartTime.IsZero() || slot.EndTime.IsZero() || strings.TrimSpace(slot.StaffID) == "" {
			continue
		}
		out = append(out, offeredSlotFromAvailability(result, slot))
	}
	return out
}

func offeredSlotFromAvailability(result *booking.AvailabilityResult, slot booking.AvailabilitySlot) OfferedSlot {
	offered := OfferedSlot{
		StartTime:          slot.StartTime,
		EndTime:            slot.EndTime,
		StaffID:            slot.StaffID,
		StaffName:          slot.StaffName,
		StaffSelectionMode: firstNonEmpty(slot.StaffSelectionMode, result.StaffSelectionMode),
		Segments:           offeredSlotSegments(result, slot),
	}
	if offered.StaffSelectionMode == "" {
		offered.StaffSelectionMode = booking.StaffSelectionSpecific
	}
	if offered.StaffID == "" && len(offered.Segments) > 0 {
		offered.StaffID = offered.Segments[0].StaffID
		offered.StaffName = offered.Segments[0].StaffName
	}
	return offered
}

func offeredSlotSegments(result *booking.AvailabilityResult, slot booking.AvailabilitySlot) []OfferedSlotSegment {
	source := slot.Segments
	if len(source) == 0 {
		source = result.Segments
	}
	if len(source) == 0 {
		serviceID := firstNonEmpty(result.ServiceID)
		if serviceID == "" {
			return nil
		}
		source = []booking.AvailabilitySegment{{
			ServiceID:          serviceID,
			ServiceName:        result.ServiceName,
			StaffID:            firstNonEmpty(slot.StaffID, result.StaffID),
			StaffName:          firstNonEmpty(slot.StaffName, result.StaffName),
			StaffSelectionMode: firstNonEmpty(slot.StaffSelectionMode, result.StaffSelectionMode),
			DurationMinutes:    result.DurationMinutes,
		}}
	}
	out := make([]OfferedSlotSegment, 0, len(source))
	for _, segment := range source {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		mode := firstNonEmpty(segment.StaffSelectionMode, slot.StaffSelectionMode, result.StaffSelectionMode)
		if mode == "" {
			mode = booking.StaffSelectionSpecific
		}
		out = append(out, OfferedSlotSegment{
			ServiceID:          serviceID,
			ServiceName:        segment.ServiceName,
			StaffID:            firstNonEmpty(segment.StaffID, slot.StaffID, result.StaffID),
			StaffName:          firstNonEmpty(segment.StaffName, slot.StaffName, result.StaffName),
			StaffSelectionMode: mode,
			DurationMinutes:    segment.DurationMinutes,
		})
	}
	return out
}

func availabilityToolMessage(slotCount int) string {
	if slotCount == 0 {
		return "Availability check returned no bookable slots."
	}
	return fmt.Sprintf("Availability check returned %d bookable slot(s).", slotCount)
}

func formatSlotOffer(slots []OfferedSlot, loc *time.Location, unavailableRequestedTime bool) string {
	prefix := "I found these openings: "
	if unavailableRequestedTime {
		prefix = "That time is not available. I found these openings: "
	}
	parts := make([]string, 0, len(slots))
	for i, slot := range slots {
		label := ordinalLabel(i + 1)
		when := slot.StartTime.In(loc).Format("Mon Jan 2 at 3:04 PM")
		if strings.TrimSpace(slot.StaffName) != "" && !slotUsesAnyone(slot) {
			when += " with " + strings.TrimSpace(slot.StaffName)
		}
		parts = append(parts, label+" "+when)
	}
	return prefix + strings.Join(parts, "; ") + ". Which works?"
}

func ordinalLabel(index int) string {
	switch index {
	case 1:
		return "first:"
	case 2:
		return "second:"
	case 3:
		return "third:"
	default:
		return fmt.Sprintf("%d:", index)
	}
}

func syncTurnUpdate(turn *TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) {
	turn.Update.CustomerName = session.CustomerName
	turn.Update.CustomerPhone = session.CustomerPhone
	turn.Update.CustomerEmail = session.CustomerEmail
	turn.Update.ServiceID = session.ServiceID
	turn.Update.StaffID = session.StaffID
	turn.Update.StaffSelectionMode = staffSelectionModeForSession(session)
	turn.Update.RequestedStartTime = session.RequestedStartTime
	turn.Update.OfferedSlots = session.OfferedSlots
	turn.Update.BookingSegments = session.BookingSegments
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
}

func (s *Service) tryBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, "I could not reach the booking path, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
	}
	attempt, err := s.bookingTool.Create(ctx, turn.SalonID, ownerUserID, booking.CreateBookingRequest{
		Source:             bookingSourceForSession(session),
		CustomerName:       session.CustomerName,
		CustomerPhone:      session.CustomerPhone,
		CustomerEmail:      session.CustomerEmail,
		ServiceID:          session.ServiceID,
		StaffID:            session.StaffID,
		StaffSelectionMode: staffSelectionModeForSession(session),
		Segments:           bookingSegmentsForCreate(session),
		StartTime:          *session.RequestedStartTime,
		Notes:              bookingNotesForSession(session),
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
	turn.Update.OfferedSlots = nil
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
	if session == nil {
		return
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
	if matches := matchServices(message, services); len(matches) > 0 && (session.ServiceID == "" || len(session.BookingSegments) == 0) {
		session.ServiceID = matches[0].ID
		session.ServiceName = matches[0].Name
		session.BookingSegments = bookingSegmentsFromServices(matches, *session)
		if len(session.BookingSegments) > 0 {
			session.StaffSelectionMode = session.BookingSegments[0].StaffSelectionMode
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
	case strings.TrimSpace(session.ServiceID) == "":
		return "service"
	case session.RequestedStartTime == nil:
		return "requested_start_time"
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
	case "requested_start_time":
		return "What day would you like? I will check available times."
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
	matches := matchServices(message, services)
	if len(matches) > 0 {
		item := matches[0]
		return &item
	}
	return nil
}

func matchServices(message string, services []ServiceOption) []ServiceOption {
	lower := strings.ToLower(message)
	ordered := append([]ServiceOption(nil), services...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i].Name) > len(ordered[j].Name)
	})
	type match struct {
		service ServiceOption
		index   int
	}
	matches := make([]match, 0, len(ordered))
	seen := map[string]bool{}
	for _, service := range ordered {
		name := strings.ToLower(service.Name)
		if name == "" {
			continue
		}
		if index := strings.Index(lower, name); index >= 0 {
			matches = append(matches, match{service: service, index: index})
			seen[service.ID] = true
		}
	}
	if len(matches) == 0 {
		for _, service := range ordered {
			if seen[service.ID] {
				continue
			}
			for _, token := range significantWords(service.Name) {
				if index := strings.Index(lower, token); index >= 0 {
					matches = append(matches, match{service: service, index: index})
					seen[service.ID] = true
					break
				}
			}
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].index == matches[j].index {
			return len(matches[i].service.Name) > len(matches[j].service.Name)
		}
		return matches[i].index < matches[j].index
	})
	out := make([]ServiceOption, 0, len(matches))
	for _, item := range matches {
		out = append(out, item.service)
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
	if match := relativeDayPattern.FindStringSubmatch(message); len(match) > 1 {
		base := now().In(loc)
		if strings.EqualFold(match[1], "tomorrow") {
			base = base.AddDate(0, 0, 1)
		}
		return base.Format("2006-01-02")
	}
	return ""
}

func selectOfferedSlot(message string, slots []OfferedSlot) *OfferedSlot {
	index, ok := selectedSlotIndex(message)
	if !ok || index < 0 || index >= len(slots) {
		return nil
	}
	slot := slots[index]
	return &slot
}

func selectedSlotIndex(message string) (int, bool) {
	lower := strings.ToLower(message)
	checks := []struct {
		index   int
		signals []string
	}{
		{0, []string{"first", "1st", "number 1", "option 1", "the 1"}},
		{1, []string{"second", "2nd", "number 2", "option 2", "the 2"}},
		{2, []string{"third", "3rd", "number 3", "option 3", "the 3"}},
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
	session.StaffID = slot.StaffID
	session.StaffName = slot.StaffName
	session.StaffSelectionMode = offeredSlotStaffSelectionMode(slot)
	session.BookingSegments = bookingSegmentsFromOfferedSlot(slot)
	if session.StaffID == "" && len(slot.Segments) > 0 {
		session.StaffID = slot.Segments[0].StaffID
		session.StaffName = slot.Segments[0].StaffName
	}
	session.OfferedSlots = nil
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
	if name := serviceSummary(session, services); name != "" {
		parts = append(parts, name)
	}
	if name := staffName(session.StaffID, staff, session.StaffName); name != "" && !sessionUsesAnyone(session) {
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
	service := serviceSummary(session, services)
	member := ""
	if !sessionUsesAnyone(session) {
		member = staffName(session.StaffID, staff, session.StaffName)
	}
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

func serviceSummary(session Session, services []ServiceOption) string {
	names := make([]string, 0, len(session.BookingSegments))
	seen := map[string]bool{}
	for _, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" || seen[serviceID] {
			continue
		}
		if name := serviceName(serviceID, services, ""); name != "" {
			names = append(names, name)
			seen[serviceID] = true
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return serviceName(session.ServiceID, services, session.ServiceName)
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
