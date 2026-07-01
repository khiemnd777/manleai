package conversation

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	defaultGreeting           = "Thank you for calling. This call may be recorded to help us manage appointments and improve service. How can I help today?"
	recordingDisclosure       = "This call may be recorded to help us manage appointments and improve service."
	openEndedHelpPrompt       = "How can I help today?"
	connectionCheckOpenPrompt = "Hi, I can hear you. How can I help today?"
	defaultRetentionLimit     = 50
	maxRetentionLimit         = 500
	defaultWebhookLimit       = 50
	maxWebhookLimit           = 100
	maxCustomerNamePrompts    = 3
	availabilityOfferLimit    = 3
	exactAvailabilityLimit    = 24
)

var (
	phonePattern                   = regexp.MustCompile(`(?:\+?1[\s.-]?)?(?:\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4})`)
	emailPattern                   = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	dateTimePattern                = regexp.MustCompile(`(?i)(\d{4}-\d{2}-\d{2})(?:[ t]+(?:at\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)?)`)
	relativeTimePattern            = regexp.MustCompile(`(?i)\b(today|tomorrow)\b\s*(?:at\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)?`)
	dateOnlyPattern                = regexp.MustCompile(`(?i)\b(\d{4}-\d{2}-\d{2})\b`)
	relativeDayPattern             = regexp.MustCompile(`(?i)\b(today|tomorrow)\b`)
	timeWithMeridiemPattern        = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)(?:$|[^a-z0-9])`)
	offeredSlotNumericTimePattern  = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|bpm|tm)(?:$|[^a-z0-9])`)
	offeredSlotWordTimePattern     = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)(?:\s+([0-5][0-9]|oh\s+[0-9]|fifteen|thirty|forty[- ]five))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|bpm|tm)(?:$|[^a-z0-9])`)
	slotConfirmationPromptPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bdoes\s+(.{0,80}?)\s+work\b`),
		regexp.MustCompile(`(?i)\bwould you like\s+(.{0,80}?)(?:\?|$)`),
		regexp.MustCompile(`(?i)\bdo you want\s+(.{0,80}?)(?:\?|$)`),
		regexp.MustCompile(`(?i)\bshould i book\s+(.{0,80}?)(?:\?|$)`),
	}
	namePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bmy name is\s+([^,.;]+)`),
		regexp.MustCompile(`(?i)\bmy name\s+([^,.;]+)`),
		regexp.MustCompile(`(?i)\bname is\s+([^,.;]+)`),
		regexp.MustCompile(`(?i)\bthe name is\s+([^,.;]+)`),
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

type availabilitySelection struct {
	Slot       booking.AvailabilitySlot
	Policy     string
	Candidates []assignmentCandidate
}

type assignmentCandidate struct {
	StaffID        string
	StaffName      string
	AssignedCount  int
	LastAssignedAt *time.Time
	Slot           booking.AvailabilitySlot
}

type serviceMatch struct {
	service ServiceOption
	index   int
	end     int
	token   string
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
	eventKey := normalizeEventKey(req.EventKey)
	if salonID == "" || ownerUserID == "" || sessionID == "" || message == "" {
		return nil, ErrValidation
	}
	session, err := s.store.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		return nil, err
	}
	if eventKey != "" {
		if processed, ok, err := s.store.GetSessionByTurnEventKey(ctx, salonID, ownerUserID, sessionID, eventKey); err != nil {
			return nil, err
		} else if ok {
			return processed, nil
		}
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
	serviceAliases, err := s.store.ListActiveServiceAliases(ctx, salonID)
	if err != nil {
		return nil, err
	}
	staff, err := s.store.ListBookableStaff(ctx, salonID)
	if err != nil {
		return nil, err
	}
	activeStaff, err := s.store.ListActiveStaff(ctx, salonID)
	if err != nil {
		return nil, err
	}
	knowledge, err := s.store.ListActiveKnowledge(ctx, salonID)
	if err != nil {
		return nil, err
	}

	if handled, updated, err := s.handlePendingCustomerNameConfirmation(ctx, salonID, ownerUserID, *session, message, eventKey, services, staff, cfg, knowledge); handled {
		return updated, err
	}

	if reply, handoff := customerNameSlotRepairReply(message, *session, services, serviceAliases); reply != "" {
		turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
		if handoff {
			return s.saveHandoffTurn(ctx, turn, *session, HandoffReasonCustomerDetailsUnavailable, reply, services, staff, cfg)
		}
		turn.AIMessage = reply
		s.applyReplyGenerator(ctx, &turn, *session, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, *session, *session, "customer_name", "customer_name", "customer_name_repair")
		return s.store.SaveTurn(ctx, turn)
	}

	if repairReply := repairReplyForMessage(message, *session, cfg); repairReply != "" {
		turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
		turn.AIMessage = repairReply
		missing := ""
		if session.Intent == IntentBooking || session.ServiceID != "" || session.RequestedDate != "" || session.RequestedStartTime != nil {
			missing = missingBookingField(*session)
		}
		finalizeTurnMetadata(&turn, *session, *session, missing, missing, "deterministic_repair")
		return s.store.SaveTurn(ctx, turn)
	}

	next := *session
	selectedOfferedSlot := false
	exactRequestedTimeSelected := false
	loc := timezoneLocation(cfg.Timezone)
	pendingNameCandidate := voiceCustomerNamePendingConfirmationCandidate(message, *session)
	serviceUnderstanding := interpretServiceForSession(message, *session, services, serviceAliases)
	applyExtraction(&next, message, services, serviceAliases, staff, loc, s.now)
	if shouldApplyServiceUnderstandingSelection(*session, message, serviceUnderstanding) {
		applyServiceSelection(&next, serviceUnderstanding.Candidates)
	}
	if pendingNameCandidate != "" {
		next.CustomerName = ""
	}
	if selected := selectOfferedSlot(message, session.OfferedSlots, loc); selected != nil {
		applySelectedOfferedSlot(&next, *selected)
		selectedOfferedSlot = true
	} else if selected := selectConfirmedOfferedSlot(message, *session, loc); selected != nil {
		applySelectedOfferedSlot(&next, *selected)
		selectedOfferedSlot = true
	}
	intent := resolveIntent(session.Intent, message, next)
	next.Intent = intent

	turn := newTurnRecord(salonID, ownerUserID, *session, next, message, eventKey, services, staff, cfg)
	applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)

	if shouldGroupBookingHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonGroupBooking, groupBookingHandoffReply(), services, staff, cfg)
	}

	if shouldHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll pass this to the owner so they can help directly. This is not a confirmed appointment.", services, staff, cfg)
	}

	if pendingNameCandidate != "" && intent == IntentBooking {
		turn.AIMessage = customerNameConfirmationPrompt(pendingNameCandidate)
		setPendingCustomerNameMetadata(&turn, pendingNameCandidate, "voice_short_bare_name")
		s.applyReplyGenerator(ctx, &turn, next, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, *session, next, "customer_name", "customer_name", "customer_name_confirmation")
		return s.store.SaveTurn(ctx, turn)
	}

	if intent != IntentBooking {
		if answer := knowledgeAnswer(message, knowledge); answer != "" {
			turn.AIMessage = answer
		} else {
			turn.AIMessage = "I can help with appointments. What service would you like to book?"
		}
		s.applyReplyGenerator(ctx, &turn, next, cfg, "", "", knowledge)
		finalizeTurnMetadata(&turn, *session, next, "", "", "knowledge_or_booking_redirect")
		return s.store.SaveTurn(ctx, turn)
	}

	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. I can take the request for the owner, but this is not a confirmed appointment.", services, staff, cfg)
	}

	if requestedStaff := matchNonBookableStaff(message, activeStaff); requestedStaff != nil {
		next.StaffName = requestedStaff.Name
		next.StaffSelectionMode = booking.StaffSelectionSpecific
		turn.Update.StaffSelectionMode = staffSelectionModeForSession(next)
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, nonBookableStaffReply(*requestedStaff), services, staff, cfg)
	}

	if shouldCheckAvailabilityForRequestedTime(*session, next, selectedOfferedSlot) {
		available, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
		}
		if !available {
			s.applyReplyGenerator(ctx, &turn, next, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "availability_alternative")
			return s.store.SaveTurn(ctx, turn)
		}
		exactRequestedTimeSelected = true
	}

	if missing := missingBookingField(next); missing != "" {
		if missing == "requested_time" || missing == "requested_start_time" {
			if len(next.OfferedSlots) > 0 {
				turn.AIMessage = formatSlotOffer(next.OfferedSlots, loc, false)
				s.applyReplyGenerator(ctx, &turn, next, cfg, missing, missing, knowledge)
				finalizeTurnMetadata(&turn, *session, next, missing, missing, "availability_offer_repeated")
				return s.store.SaveTurn(ctx, turn)
			}
			preferredDate := preferredDateForAvailability(next, message, loc, s.now)
			if preferredDate != "" && next.ServiceID != "" {
				if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, preferredDate, false, cfg); err != nil {
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
				}
				s.applyReplyGenerator(ctx, &turn, next, cfg, missing, missing, knowledge)
				finalizeTurnMetadata(&turn, *session, next, missing, missing, "availability_offer")
				return s.store.SaveTurn(ctx, turn)
			}
		}
		turn.AIMessage = promptForMissingField(missing)
		if missing == "service" {
			serviceClarification := serviceUnderstandingForClarification(next, services, serviceUnderstanding)
			if prompt := serviceClarificationPrompt(next, serviceClarification, cfg); prompt != "" {
				turn.AIMessage = prompt
				setPendingServiceCandidateMetadata(&turn, serviceClarification)
				finalizeTurnMetadata(&turn, *session, next, missing, missing, "service_understanding_clarification")
				return s.store.SaveTurn(ctx, turn)
			}
		}
		if exactRequestedTimeSelected {
			turn.AIMessage = selectedRequestedTimeReply(next, services, staff, cfg, missing)
		}
		s.applyReplyGenerator(ctx, &turn, next, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, *session, next, missing, missing, "missing_field")
		return s.store.SaveTurn(ctx, turn)
	}

	return s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
}

func (s *Service) List(ctx context.Context, salonID string, ownerUserID string, limit int, lifecycleStatus string) ([]Session, error) {
	lifecycleStatus = normalizeLifecycleStatus(lifecycleStatus)
	if lifecycleStatus == "" {
		return nil, ErrValidation
	}
	return s.store.ListSessions(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), lifecycleStatus, clampLimit(limit))
}

func (s *Service) Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	return s.store.GetSessionForOwner(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(sessionID))
}

func (s *Service) TranscriptionContext(ctx context.Context, salonID string, ownerUserID string, sessionID string) (TranscriptionContext, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	if salonID == "" || ownerUserID == "" || sessionID == "" {
		return TranscriptionContext{}, ErrValidation
	}
	session, err := s.store.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		return TranscriptionContext{}, err
	}
	cfg, err := s.store.GetRuntimeConfig(ctx, salonID, ownerUserID)
	if err != nil {
		return TranscriptionContext{}, err
	}
	services, err := s.store.ListBookableServices(ctx, salonID)
	if err != nil {
		return TranscriptionContext{}, err
	}
	aliases, err := s.store.ListActiveServiceAliases(ctx, salonID)
	if err != nil {
		return TranscriptionContext{}, err
	}
	return TranscriptionContext{
		Prompt: transcriptionContextPrompt(*session, cfg, services, aliases),
	}, nil
}

func (s *Service) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int) ([]WebhookEventLog, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	if salonID == "" || ownerUserID == "" || sessionID == "" {
		return nil, ErrValidation
	}
	return s.store.ListWebhookEvents(ctx, salonID, ownerUserID, sessionID, clampWebhookLimit(limit))
}

func (s *Service) Archive(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	if salonID == "" || ownerUserID == "" || sessionID == "" {
		return nil, ErrValidation
	}
	return s.store.ArchiveSession(ctx, salonID, ownerUserID, sessionID)
}

func (s *Service) Redact(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	if salonID == "" || ownerUserID == "" || sessionID == "" {
		return nil, ErrValidation
	}
	return s.store.RedactSession(ctx, salonID, ownerUserID, sessionID)
}

func (s *Service) applyAvailabilityForRequestedTime(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (bool, error) {
	if session == nil || session.RequestedStartTime == nil {
		return false, nil
	}
	preferredDate := preferredDateFromMessage("", session.RequestedStartTime, timezoneLocation(cfg.Timezone), s.now)
	if preferredDate == "" {
		return false, nil
	}
	result, err := s.availableSlotsWithLimit(ctx, turn.SalonID, ownerUserID, *session, preferredDate, exactAvailabilityLimit)
	if err != nil {
		return false, err
	}
	slots := []booking.AvailabilitySlot{}
	if result != nil {
		slots = result.Slots
	}
	matches := exactAvailabilityMatches(slots, *session)
	if len(matches) > 0 {
		selection, err := s.selectAvailabilitySlot(ctx, turn.SalonID, *session, matches, cfg)
		if err != nil {
			return false, err
		}
		turn.ToolMessage = availabilityToolMessage(len(slots))
		applyAssignmentSelectionMetadata(turn, selection)
		applySelectedOfferedSlot(session, offeredSlotFromAvailability(result, selection.Slot))
		syncTurnUpdate(turn, *session, services, staff, cfg)
		return true, nil
	}
	if handled, err := s.applySpecificStaffUnavailableOffer(ctx, ownerUserID, turn, session, services, staff, cfg, preferredDate, result); err != nil {
		return false, err
	} else if handled {
		return false, nil
	}
	applyAvailabilityOffer(turn, session, services, staff, cfg, result, true)
	return false, nil
}

func shouldCheckAvailabilityForRequestedTime(before Session, after Session, selectedOfferedSlot bool) bool {
	if selectedOfferedSlot || strings.TrimSpace(after.ServiceID) == "" || after.RequestedStartTime == nil {
		return false
	}
	if strings.TrimSpace(before.ServiceID) != strings.TrimSpace(after.ServiceID) {
		return true
	}
	if strings.TrimSpace(before.ServiceID) == "" || !sameOptionalTime(before.RequestedStartTime, after.RequestedStartTime) {
		return true
	}
	if strings.TrimSpace(before.RequestedDate) != strings.TrimSpace(after.RequestedDate) {
		return true
	}
	if staffSelectionModeForAvailability(before) != staffSelectionModeForAvailability(after) {
		return true
	}
	if strings.TrimSpace(before.StaffID) != strings.TrimSpace(after.StaffID) {
		return true
	}
	return !hasStaffAssignment(before)
}

func sameOptionalTime(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func exactAvailabilityMatches(slots []booking.AvailabilitySlot, session Session) []booking.AvailabilitySlot {
	if session.RequestedStartTime == nil {
		return nil
	}
	matches := []booking.AvailabilitySlot{}
	for _, slot := range slots {
		if !slot.StartTime.Equal(*session.RequestedStartTime) {
			continue
		}
		if staffID := strings.TrimSpace(session.StaffID); staffID != "" && availabilitySlotStaffID(slot) != staffID {
			continue
		}
		matches = append(matches, slot)
	}
	return matches
}

func (s *Service) selectAvailabilitySlot(ctx context.Context, salonID string, session Session, matches []booking.AvailabilitySlot, cfg *RuntimeConfig) (availabilitySelection, error) {
	if len(matches) == 0 {
		return availabilitySelection{}, ErrNotFound
	}
	if staffSelectionModeForAvailability(session) == booking.StaffSelectionSpecific && strings.TrimSpace(session.StaffID) != "" {
		sort.SliceStable(matches, stableAvailabilitySlotLess(matches))
		slot := matches[0]
		return availabilitySelection{
			Slot:   slot,
			Policy: "customer_requested_staff",
			Candidates: []assignmentCandidate{{
				StaffID:   availabilitySlotStaffID(slot),
				StaffName: availabilitySlotStaffName(slot),
				Slot:      slot,
			}},
		}, nil
	}
	return s.selectFairAvailabilitySlot(ctx, salonID, session, matches, cfg)
}

func (s *Service) selectFairAvailabilitySlot(ctx context.Context, salonID string, session Session, matches []booking.AvailabilitySlot, cfg *RuntimeConfig) (availabilitySelection, error) {
	unique := uniqueAvailabilitySlotsByStaff(matches)
	if len(unique) == 0 {
		unique = append([]booking.AvailabilitySlot(nil), matches...)
	}
	staffIDs := make([]string, 0, len(unique))
	for _, slot := range unique {
		if staffID := availabilitySlotStaffID(slot); staffID != "" {
			staffIDs = append(staffIDs, staffID)
		}
	}
	from, to := assignmentStatsWindow(session.RequestedStartTime, timezoneLocation(timezoneFromConfig(cfg)))
	stats, err := s.store.ListStaffAssignmentStats(ctx, salonID, staffIDs, from, to)
	if err != nil {
		return availabilitySelection{}, err
	}
	candidates := make([]assignmentCandidate, 0, len(unique))
	for _, slot := range unique {
		staffID := availabilitySlotStaffID(slot)
		stat := stats[staffID]
		candidates = append(candidates, assignmentCandidate{
			StaffID:        staffID,
			StaffName:      availabilitySlotStaffName(slot),
			AssignedCount:  stat.AssignedCount,
			LastAssignedAt: stat.LastAssignedAt,
			Slot:           slot,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return assignmentCandidateLess(candidates[i], candidates[j])
	})
	return availabilitySelection{
		Slot:       candidates[0].Slot,
		Policy:     "fair_rotation",
		Candidates: candidates,
	}, nil
}

func stableAvailabilitySlotLess(slots []booking.AvailabilitySlot) func(i int, j int) bool {
	return func(i int, j int) bool {
		leftID := availabilitySlotStaffID(slots[i])
		rightID := availabilitySlotStaffID(slots[j])
		if leftID != rightID {
			return leftID < rightID
		}
		leftName := strings.ToLower(availabilitySlotStaffName(slots[i]))
		rightName := strings.ToLower(availabilitySlotStaffName(slots[j]))
		if leftName != rightName {
			return leftName < rightName
		}
		return slots[i].StartTime.Before(slots[j].StartTime)
	}
}

func uniqueAvailabilitySlotsByStaff(slots []booking.AvailabilitySlot) []booking.AvailabilitySlot {
	out := make([]booking.AvailabilitySlot, 0, len(slots))
	seen := map[string]bool{}
	for _, slot := range slots {
		staffID := availabilitySlotStaffID(slot)
		if staffID == "" {
			continue
		}
		if seen[staffID] {
			continue
		}
		seen[staffID] = true
		out = append(out, slot)
	}
	return out
}

func availabilitySlotStaffID(slot booking.AvailabilitySlot) string {
	if staffID := strings.TrimSpace(slot.StaffID); staffID != "" {
		return staffID
	}
	for _, segment := range slot.Segments {
		if staffID := strings.TrimSpace(segment.StaffID); staffID != "" {
			return staffID
		}
	}
	return ""
}

func availabilitySlotStaffName(slot booking.AvailabilitySlot) string {
	if name := strings.TrimSpace(slot.StaffName); name != "" {
		return name
	}
	for _, segment := range slot.Segments {
		if name := strings.TrimSpace(segment.StaffName); name != "" {
			return name
		}
	}
	return ""
}

func assignmentStatsWindow(requestedStartTime *time.Time, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	if requestedStartTime == nil || requestedStartTime.IsZero() {
		now := time.Now().In(loc)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return start.UTC(), start.AddDate(0, 0, 1).UTC()
	}
	local := requestedStartTime.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

func assignmentCandidateLess(left assignmentCandidate, right assignmentCandidate) bool {
	if left.AssignedCount != right.AssignedCount {
		return left.AssignedCount < right.AssignedCount
	}
	if left.LastAssignedAt == nil && right.LastAssignedAt != nil {
		return true
	}
	if left.LastAssignedAt != nil && right.LastAssignedAt == nil {
		return false
	}
	if left.LastAssignedAt != nil && right.LastAssignedAt != nil && !left.LastAssignedAt.Equal(*right.LastAssignedAt) {
		return left.LastAssignedAt.Before(*right.LastAssignedAt)
	}
	if left.StaffID != right.StaffID {
		return left.StaffID < right.StaffID
	}
	return strings.ToLower(left.StaffName) < strings.ToLower(right.StaffName)
}

func applyAssignmentSelectionMetadata(turn *TurnRecord, selection availabilitySelection) {
	if turn == nil {
		return
	}
	candidates := make([]map[string]any, 0, len(selection.Candidates))
	for _, item := range selection.Candidates {
		entry := map[string]any{
			"staff_id":       item.StaffID,
			"staff_name":     item.StaffName,
			"assigned_count": item.AssignedCount,
		}
		if item.LastAssignedAt != nil {
			entry["last_assigned_at"] = item.LastAssignedAt.Format(time.RFC3339)
		}
		candidates = append(candidates, entry)
	}
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"assignment_policy":      selection.Policy,
		"selected_staff_id":      availabilitySlotStaffID(selection.Slot),
		"selected_staff_name":    availabilitySlotStaffName(selection.Slot),
		"assignment_candidates":  candidates,
		"assignment_candidate_n": len(candidates),
	})
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
	return s.availableSlotsWithLimit(ctx, salonID, ownerUserID, session, preferredDate, availabilityOfferLimit)
}

func (s *Service) availableSlotsWithLimit(ctx context.Context, salonID string, ownerUserID string, session Session, preferredDate string, limit int) (*booking.AvailabilityResult, error) {
	if s.bookingTool == nil {
		return nil, fmt.Errorf("booking tool is unavailable")
	}
	staffSelectionMode := staffSelectionModeForAvailability(session)
	if limit <= 0 {
		limit = availabilityOfferLimit
	}
	return s.bookingTool.AvailableSlots(ctx, salonID, ownerUserID, booking.AvailabilityRequest{
		ServiceID:          session.ServiceID,
		StaffID:            staffIDForAvailability(session),
		StaffSelectionMode: staffSelectionMode,
		Segments:           availabilitySegmentsForSession(session, staffSelectionMode),
		PreferredDate:      preferredDate,
		Limit:              limit,
	})
}

func (s *Service) applySpecificStaffUnavailableOffer(ctx context.Context, ownerUserID string, turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, preferredDate string, requestedStaffResult *booking.AvailabilityResult) (bool, error) {
	if session == nil || session.RequestedStartTime == nil || staffSelectionModeForAvailability(*session) != booking.StaffSelectionSpecific || strings.TrimSpace(session.StaffID) == "" {
		return false, nil
	}
	requestedStart := *session.RequestedStartTime
	anyoneSession := *session
	anyoneSession.StaffID = ""
	anyoneSession.StaffName = ""
	anyoneSession.StaffSelectionMode = booking.StaffSelectionAnyone
	clearBookingSegmentsStaffSelection(&anyoneSession)
	anyoneResult, err := s.availableSlotsWithLimit(ctx, turn.SalonID, ownerUserID, anyoneSession, preferredDate, exactAvailabilityLimit)
	if err != nil {
		return false, err
	}

	otherStaffMatches := exactAvailabilityMatches(availabilitySlots(anyoneResult), anyoneSession)
	filteredOtherStaffMatches := make([]booking.AvailabilitySlot, 0, len(otherStaffMatches))
	for _, slot := range otherStaffMatches {
		if availabilitySlotStaffID(slot) == strings.TrimSpace(session.StaffID) {
			continue
		}
		filteredOtherStaffMatches = append(filteredOtherStaffMatches, slot)
	}

	offered := []OfferedSlot{}
	if len(filteredOtherStaffMatches) > 0 {
		selection, err := s.selectFairAvailabilitySlot(ctx, turn.SalonID, anyoneSession, filteredOtherStaffMatches, cfg)
		if err != nil {
			return false, err
		}
		applyAssignmentSelectionMetadata(turn, selection)
		offered = append(offered, offeredSlotFromAvailability(anyoneResult, selection.Slot))
	}
	for _, slot := range offeredSlotsFromAvailability(requestedStaffResult) {
		if len(offered) >= availabilityOfferLimit {
			break
		}
		if offeredSlotAlreadyIncluded(offered, slot) {
			continue
		}
		offered = append(offered, slot)
	}

	session.RequestedStartTime = nil
	session.OfferedSlots = offered
	turn.ToolMessage = availabilityToolMessage(len(offered))
	turn.AIMessage = formatSpecificStaffUnavailableOffer(*session, staff, requestedStart, offered, timezoneLocation(timezoneFromConfig(cfg)))
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
		"availability_policy":          "specific_staff_unavailable",
		"requested_staff_id":           strings.TrimSpace(session.StaffID),
		"requested_staff_name":         staffName(session.StaffID, staff, session.StaffName),
		"same_time_alternative_count":  len(filteredOtherStaffMatches),
		"requested_staff_option_count": len(offeredSlotsFromAvailability(requestedStaffResult)),
	})
	syncTurnUpdate(turn, *session, services, staff, cfg)
	return true, nil
}

func availabilitySlots(result *booking.AvailabilityResult) []booking.AvailabilitySlot {
	if result == nil {
		return nil
	}
	return result.Slots
}

func offeredSlotAlreadyIncluded(slots []OfferedSlot, candidate OfferedSlot) bool {
	for _, slot := range slots {
		if slot.StartTime.Equal(candidate.StartTime) && strings.TrimSpace(slot.StaffID) == strings.TrimSpace(candidate.StaffID) {
			return true
		}
	}
	return false
}

func applyAvailabilityOffer(turn *TurnRecord, session *Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, result *booking.AvailabilityResult, unavailableRequestedTime bool) {
	offered := offeredSlotsFromAvailability(result)
	session.RequestedStartTime = nil
	session.OfferedSlots = offered
	turn.ToolMessage = availabilityToolMessage(len(offered))
	if len(offered) == 0 {
		turn.AIMessage = "I do not see open times for that day. What other day works?"
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
	return prefix + formatSlotOptions(slots, loc) + ". Which works?"
}

func formatSpecificStaffUnavailableOffer(session Session, staff []StaffOption, requestedStart time.Time, slots []OfferedSlot, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	requestedStaff := staffName(session.StaffID, staff, session.StaffName)
	if requestedStaff == "" {
		requestedStaff = "That technician"
	}
	when := requestedStart.In(loc).Format("3:04 PM Monday")
	prefix := requestedStaff + " is not available at " + when + ". "
	if len(slots) == 0 {
		return prefix + "What other time works with " + requestedStaff + ", or should I use anyone available?"
	}
	return prefix + "I found these options: " + formatSlotOptions(slots, loc) + ". Which works?"
}

func formatSlotOptions(slots []OfferedSlot, loc *time.Location) string {
	parts := make([]string, 0, len(slots))
	for i, slot := range slots {
		label := ordinalLabel(i + 1)
		when := slot.StartTime.In(loc).Format("Mon Jan 2 at 3:04 PM")
		if assigned := slotAssignedStaffLabel(slot); assigned != "" {
			when += " with " + assigned + " assigned"
		}
		parts = append(parts, label+" "+when)
	}
	return strings.Join(parts, "; ")
}

func selectedRequestedTimeReply(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, missing string) string {
	prompt := promptForMissingField(missing)
	if session.RequestedStartTime == nil {
		return prompt
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	when := session.RequestedStartTime.In(loc).Format("3:04 PM Monday")
	sentence := when + " is available"
	if service := strings.TrimSpace(serviceSummary(session, services)); service != "" {
		sentence += " for your " + service
	}
	if assigned := sessionAssignedStaffLabel(session, staff); assigned != "" {
		sentence += " with " + assigned + " assigned"
	}
	return sentence + ". " + prompt
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
	turn.Update.RequestedDate = session.RequestedDate
	turn.Update.RequestedStartTime = session.RequestedStartTime
	turn.Update.OfferedSlots = session.OfferedSlots
	turn.Update.BookingSegments = session.BookingSegments
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
}

func newTurnRecord(salonID string, ownerUserID string, before Session, after Session, message string, eventKey string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) TurnRecord {
	return TurnRecord{
		SalonID:         salonID,
		OwnerUserID:     ownerUserID,
		Session:         before,
		CustomerMessage: message,
		EventKey:        eventKey,
		Update: SessionUpdate{
			Status:             StatusActive,
			Intent:             after.Intent,
			Outcome:            OutcomeCollecting,
			CustomerName:       after.CustomerName,
			CustomerPhone:      after.CustomerPhone,
			CustomerEmail:      after.CustomerEmail,
			ServiceID:          after.ServiceID,
			StaffID:            after.StaffID,
			StaffSelectionMode: staffSelectionModeForSession(after),
			RequestedDate:      after.RequestedDate,
			RequestedStartTime: after.RequestedStartTime,
			OfferedSlots:       after.OfferedSlots,
			BookingSegments:    after.BookingSegments,
			Summary:            summaryFor(after, services, staff, cfg),
		},
	}
}

func finalizeTurnMetadata(turn *TurnRecord, before Session, after Session, missing string, nextRequired string, replySource string) {
	if turn == nil {
		return
	}
	customer := map[string]any{
		"slots_before": bookingSlotSnapshot(before),
		"slots_after":  bookingSlotSnapshot(after),
	}
	if turn.EventKey != "" {
		customer["event_key"] = turn.EventKey
	}
	if missing != "" {
		customer["missing_booking_field"] = missing
	}
	if nextRequired != "" {
		customer["next_required_field"] = nextRequired
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, customer)
	ai := map[string]any{
		"turn_path":            replySource,
		"known_booking_fields": knownBookingFields(after),
	}
	if nextRequired != "" {
		ai["next_required_field"] = nextRequired
	}
	if missing != "" {
		ai["missing_booking_field"] = missing
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, ai)
}

func applyServiceUnderstandingMetadata(turn *TurnRecord, result serviceUnderstandingResult) {
	if turn == nil || (result.Status == serviceUnderstandingStatusUnknown && strings.TrimSpace(result.NormalizedInput) == "") {
		return
	}
	candidateIDs := make([]string, 0, len(result.Candidates))
	candidateNames := make([]string, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		candidateIDs = append(candidateIDs, strings.TrimSpace(candidate.ID))
		candidateNames = append(candidateNames, strings.TrimSpace(candidate.Name))
	}
	selectedID := ""
	selectedName := ""
	if result.Selected != nil {
		selectedID = strings.TrimSpace(result.Selected.ID)
		selectedName = strings.TrimSpace(result.Selected.Name)
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"service_understanding_status":        string(result.Status),
		"service_understanding_reason":        result.Reason,
		"service_understanding_confidence":    result.Confidence,
		"service_understanding_token":         result.MatchedToken,
		"service_understanding_source":        result.MatchedSource,
		"service_understanding_alias_id":      result.MatchedAliasID,
		"service_understanding_alias":         result.MatchedAlias,
		"service_understanding_normalized":    result.NormalizedInput,
		"service_understanding_candidate_ids": candidateIDs,
		"service_understanding_candidates":    candidateNames,
		"service_understanding_selected_id":   selectedID,
		"service_understanding_selected":      selectedName,
	})
}

func mergeMetadata(base map[string]any, updates map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range updates {
		out[key] = value
	}
	return out
}

func bookingSlotSnapshot(session Session) map[string]any {
	out := map[string]any{
		"intent":                 session.Intent,
		"service_id":             strings.TrimSpace(session.ServiceID),
		"staff_id":               strings.TrimSpace(session.StaffID),
		"staff_selection_mode":   staffSelectionModeForSession(session),
		"requested_date":         strings.TrimSpace(session.RequestedDate),
		"offered_slot_count":     len(session.OfferedSlots),
		"booking_segment_count":  len(session.BookingSegments),
		"customer_name_present":  strings.TrimSpace(session.CustomerName) != "",
		"customer_phone_present": strings.TrimSpace(session.CustomerPhone) != "",
	}
	if session.RequestedStartTime != nil {
		out["requested_start_time"] = session.RequestedStartTime.Format(time.RFC3339)
	}
	return out
}

func knownBookingFields(session Session) []string {
	fields := []string{}
	if strings.TrimSpace(session.ServiceID) != "" {
		fields = append(fields, "service")
	}
	if strings.TrimSpace(session.RequestedDate) != "" || session.RequestedStartTime != nil {
		fields = append(fields, "requested_date")
	}
	if session.RequestedStartTime != nil {
		fields = append(fields, "requested_start_time", "requested_time")
	}
	if strings.TrimSpace(session.CustomerName) != "" {
		fields = append(fields, "customer_name")
	}
	if strings.TrimSpace(session.CustomerPhone) != "" {
		fields = append(fields, "customer_phone")
	}
	if hasStaffAssignment(session) {
		fields = append(fields, "staff")
	}
	return fields
}

func (s *Service) tryBooking(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
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
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, bookingErrorReply(), services, staff, cfg)
	}

	toolMessage := "Booking service returned fallback pending."
	outcome := OutcomeBookingFallbackPending
	status := StatusCompleted
	aiMessage := bookingFallbackReply()
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
	s.applyReplyGenerator(ctx, &turn, session, cfg, "", "", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "booking_result")
	return s.store.SaveTurn(ctx, turn)
}

func bookingFallbackReply() string {
	return "I couldn't confirm the appointment, so I sent the request to the owner to review. This is not a confirmed appointment."
}

func bookingErrorReply() string {
	return "I couldn't complete the booking right now, so the owner needs to review it. This is not a confirmed appointment."
}

func (s *Service) applyReplyGenerator(ctx context.Context, turn *TurnRecord, session Session, cfg *RuntimeConfig, missing string, nextRequired string, knowledge []KnowledgeSnippet) {
	if s.replyGenerator == nil || turn == nil || strings.TrimSpace(turn.AIMessage) == "" {
		return
	}
	safeReply := strings.TrimSpace(turn.AIMessage)
	if turn.Update.EndSession {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"safe_reply":          safeReply,
			"llm_guardrail":       "skipped_terminal_reply",
			"reply_source":        "safe_reply",
			"next_required_field": nextRequired,
		})
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
		KnownBookingFields:  knownBookingFields(session),
		NextRequiredField:   nextRequired,
		Summary:             turn.Update.Summary,
		KnowledgeContext:    formatKnowledgeContext(knowledge),
	})
	if err != nil {
		turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
			"safe_reply":          safeReply,
			"llm_guardrail":       "fallback_to_safe_reply",
			"llm_error":           err.Error(),
			"reply_source":        "safe_reply",
			"next_required_field": nextRequired,
		})
		return
	}
	if message := strings.TrimSpace(result.Message); message != "" {
		turn.AIMessage = message
	}
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"safe_reply":          safeReply,
		"llm_reply":           strings.TrimSpace(result.Message),
		"llm_confidence":      result.Confidence,
		"llm_handoff":         result.Handoff,
		"llm_reason":          result.Reason,
		"llm_guardrail":       "accepted",
		"reply_source":        "llm_rewrite",
		"next_required_field": nextRequired,
	})
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
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "handoff")
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
			return "I have " + requestedDateLabel(session.RequestedDate, timezoneLocation(timezoneFromConfig(cfg))) + ". What time works best?"
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
	return normalizeInitialGreeting(greeting, salonName(cfg))
}

func normalizeInitialGreeting(greeting string, salon string) string {
	greeting = strings.TrimSpace(greeting)
	if greeting == "" {
		greeting = defaultGreeting
	}
	greeting = ensureSalonInGreeting(greeting, salon)
	greeting = ensureRecordingDisclosure(greeting)
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

func ensureRecordingDisclosure(greeting string) string {
	if containsFold(greeting, "recorded") {
		return greeting
	}
	return insertAfterFirstSentence(greeting, recordingDisclosure)
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

func applyExtraction(session *Session, message string, services []ServiceOption, aliases []ServiceAlias, staff []StaffOption, loc *time.Location, now func() time.Time) {
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
	matches := matchServices(message, services)
	if shouldApplyServiceSelection(*session, message, matches) {
		applyServiceSelection(session, matches)
	} else if shouldClearAmbiguousServiceCorrection(*session, message, services, matches) {
		clearServiceSelection(session)
	}
	if session.CustomerName == "" {
		if name := spelledCustomerName(message); name != "" && missingBookingField(*session) == "customer_name" {
			session.CustomerName = name
		} else if name := extractName(message); name != "" {
			session.CustomerName = name
		} else if !looksLikeServiceInsteadOfName(message, services, aliases) {
			if name := bareCustomerNameForSession(message, *session); name != "" {
				session.CustomerName = name
			}
		}
	}
}

func shouldApplyServiceUnderstandingSelection(session Session, message string, result serviceUnderstandingResult) bool {
	if result.Status != serviceUnderstandingStatusSelected || len(result.Candidates) == 0 {
		return false
	}
	if strings.TrimSpace(session.ServiceID) == "" || len(session.BookingSegments) == 0 {
		return true
	}
	if sameServiceSelection(session, result.Candidates) {
		return false
	}
	if hasServiceCorrectionSignal(message) {
		return true
	}
	return missingBookingField(session) == "customer_name"
}

func shouldApplyServiceSelection(session Session, message string, matches []ServiceOption) bool {
	if len(matches) == 0 {
		return false
	}
	if strings.TrimSpace(session.ServiceID) == "" || len(session.BookingSegments) == 0 {
		return true
	}
	if !hasServiceCorrectionSignal(message) {
		return false
	}
	return !sameServiceSelection(session, matches)
}

func shouldClearAmbiguousServiceCorrection(session Session, message string, services []ServiceOption, matches []ServiceOption) bool {
	if len(matches) > 0 || strings.TrimSpace(session.ServiceID) == "" || !hasServiceCorrectionSignal(message) {
		return false
	}
	return containsServiceVocabulary(message, services)
}

func applyServiceSelection(session *Session, matches []ServiceOption) {
	if session == nil || len(matches) == 0 {
		return
	}
	session.ServiceID = matches[0].ID
	session.ServiceName = matches[0].Name
	session.BookingSegments = bookingSegmentsFromServices(matches, *session)
	session.OfferedSlots = nil
	if len(session.BookingSegments) > 0 {
		session.StaffSelectionMode = session.BookingSegments[0].StaffSelectionMode
	}
}

func clearServiceSelection(session *Session) {
	if session == nil {
		return
	}
	session.ServiceID = ""
	session.ServiceName = ""
	session.BookingSegments = nil
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

func containsServiceVocabulary(message string, services []ServiceOption) bool {
	lower := strings.ToLower(message)
	if lower == "" {
		return false
	}
	for _, service := range services {
		name := strings.ToLower(strings.TrimSpace(service.Name))
		if name != "" && strings.Contains(lower, name) {
			return true
		}
		for _, token := range significantWords(service.Name) {
			if strings.Contains(lower, token) {
				return true
			}
		}
	}
	generic := []string{"manicure", "pedicure", "nail", "nails", "acrylic", "gel", "dip", "powder", "polish", "shellac", "french", "chrome"}
	for _, token := range generic {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func shouldHandoff(message string) bool {
	lower := strings.ToLower(message)
	triggers := []string{
		"human", "owner", "manager", "person", "representative", "complaint",
		"refund", "payment dispute", "dispute", "chargeback", "wedding", "party",
		"talk to someone", "speak to someone",
	}
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

func shouldGroupBookingHandoff(message string) bool {
	lower := strings.ToLower(message)
	normalized := normalizeLooseText(message)
	triggers := []string{
		"group booking",
		"large group",
		"me and my friend",
		"my friend and i",
		"me and two friends",
		"me and three friends",
		"my friends and i",
		"for me and",
		"for my friends",
		"party of",
		"appointments",
	}
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) || strings.Contains(normalized, trigger) {
			if strings.Contains(trigger, "appointments") && !containsMultiAppointmentQuantity(normalized) {
				continue
			}
			return true
		}
	}
	return containsMultiPersonQuantity(normalized)
}

func containsMultiAppointmentQuantity(normalized string) bool {
	return strings.Contains(normalized, "two appointments") ||
		strings.Contains(normalized, "three appointments") ||
		strings.Contains(normalized, "four appointments") ||
		strings.Contains(normalized, "2 appointments") ||
		strings.Contains(normalized, "3 appointments") ||
		strings.Contains(normalized, "4 appointments")
}

func containsMultiPersonQuantity(normalized string) bool {
	patterns := []string{
		"for 2 people",
		"for 3 people",
		"for 4 people",
		"for two people",
		"for three people",
		"for four people",
		"two people",
		"three people",
		"four people",
	}
	for _, pattern := range patterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

func groupBookingHandoffReply() string {
	return "For multiple people, the owner needs to coordinate the services, timing, and technicians. I'll send this request to the owner to review. This is not a confirmed appointment."
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
	case session.RequestedStartTime == nil && strings.TrimSpace(session.RequestedDate) == "":
		return "requested_date"
	case session.RequestedStartTime == nil:
		return "requested_time"
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
	default:
		return "What else should I know?"
	}
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
	prefix := ""
	if context := appointmentContextPhrase(session, cfg); context != "" {
		prefix = "I have " + context + " noted. "
	}
	return prefix + "Which " + serviceLabel + " would you like: " + joinHumanList(options) + "?"
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
		s.applyReplyGenerator(ctx, &turn, next, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, session, next, "customer_name", "customer_name", "customer_name_confirmation")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	if isNegativeNameConfirmation(message) {
		next := session
		turn := newTurnRecord(salonID, ownerUserID, session, next, message, eventKey, services, staff, cfg)
		turn.AIMessage = "Please say or spell the customer name for the appointment."
		clearPendingCustomerNameMetadata(&turn, "rejected")
		s.applyReplyGenerator(ctx, &turn, next, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, session, next, "customer_name", "customer_name", "customer_name_repair")
		updated, err := s.store.SaveTurn(ctx, turn)
		return true, updated, err
	}
	return false, nil, nil
}

func (s *Service) continueAfterCustomerName(ctx context.Context, ownerUserID string, turn TurnRecord, next Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	if missing := missingBookingField(next); missing != "" {
		turn.AIMessage = promptForMissingField(missing)
		s.applyReplyGenerator(ctx, &turn, next, cfg, missing, missing, knowledge)
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
	candidate := customerNameCandidate(message, session)
	if !isShortSingleWordName(candidate) {
		return ""
	}
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

func customerNameSlotRepairReply(message string, session Session, services []ServiceOption, aliases []ServiceAlias) (string, bool) {
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
	if looksLikeServiceInsteadOfName(message, services, aliases) {
		return "", false
	}
	if extractName(message) != "" {
		return "", false
	}
	if bareCustomerNameForSession(message, session) != "" {
		return "", false
	}
	if customerNamePromptCount(session) >= maxCustomerNamePrompts {
		return "I'm having trouble catching the name. I'll send this request to the owner to review. This is not a confirmed appointment.", true
	}
	if !isCustomerNameNonAnswer(message, services, aliases) {
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

func isCustomerNameNonAnswer(message string, services []ServiceOption, aliases ...[]ServiceAlias) bool {
	serviceAliases := []ServiceAlias(nil)
	if len(aliases) > 0 {
		serviceAliases = aliases[0]
	}
	return isAffirmativeOnly(message) ||
		isConnectionCheck(message) ||
		isNameRepairRequest(message) ||
		looksLikeServiceInsteadOfName(message, services, serviceAliases) ||
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

func looksLikeServiceInsteadOfName(message string, services []ServiceOption, aliases ...[]ServiceAlias) bool {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "service") || strings.Contains(lower, "name of") {
		return true
	}
	if len(aliases) > 0 {
		result := interpretService(message, services, aliases[0])
		return result.Status == serviceUnderstandingStatusSelected || result.Status == serviceUnderstandingStatusAmbiguous
	}
	return len(matchServices(message, services)) > 0
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
	if phonePattern.MatchString(name) || emailPattern.MatchString(name) || hasBookingSignal(name) {
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
	if match == nil || strings.TrimSpace(match.Body) == "" {
		return ""
	}
	if hasUnsafeKnowledgeConfirmation(match.Body) {
		return "I can share salon policies, but I cannot confirm appointments unless the booking is completed successfully. Would you like help with an appointment?"
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
	if weekday, nextWeek, ok := weekdayFromMessage(message); ok {
		return dateForWeekday(now().In(loc), weekday, nextWeek).Format("2006-01-02")
	}
	return ""
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
	if nextWeek {
		if days == 0 {
			days = 7
		} else {
			days += 7
		}
	}
	return start.AddDate(0, 0, days)
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
	session.RequestedDate = start.Format("2006-01-02")
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

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
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
	meridiem = strings.ToLower(strings.TrimSpace(meridiem))
	meridiem = strings.NewReplacer(".", "", " ", "").Replace(meridiem)
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
	if name := sessionAssignedStaffLabel(session, staff); name != "" {
		if sessionUsesAnyone(session) {
			parts = append(parts, "assigned "+name)
		} else {
			parts = append(parts, "with "+name)
		}
	}
	if session.RequestedStartTime != nil {
		parts = append(parts, "requested "+session.RequestedStartTime.Format(time.RFC3339))
	} else if session.RequestedDate != "" {
		parts = append(parts, "requested date "+session.RequestedDate)
	}
	if len(parts) == 0 {
		return "Conversation needs owner review."
	}
	return strings.Join(parts, " · ")
}

func confirmedMessage(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) string {
	service := serviceSummary(session, services)
	member := sessionAssignedStaffLabel(session, staff)
	salon := salonName(cfg)
	when := ""
	if session.RequestedStartTime != nil {
		loc := timezoneLocation("")
		if cfg != nil {
			loc = timezoneLocation(cfg.Timezone)
		}
		when = session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	}
	parts := []string{}
	if service != "" {
		parts = append(parts, "for your "+service)
	}
	if when != "" {
		parts = append(parts, "on "+when)
	}
	if member != "" {
		parts = append(parts, "with "+member)
	}
	prefix := "You're confirmed"
	if salon != "" {
		prefix += " with " + salon
	}
	message := strings.TrimSpace(prefix + " " + strings.Join(parts, " "))
	if message == prefix {
		message = prefix
	}
	message += "."
	if name := strings.TrimSpace(session.CustomerName); name != "" {
		message += " The appointment is under " + name + "."
	}
	message += " Thank you, goodbye."
	return message
}

func slotAssignedStaffLabel(slot OfferedSlot) string {
	names := []string{}
	seen := map[string]bool{}
	addStaffName(&names, seen, slot.StaffName)
	for _, segment := range slot.Segments {
		addStaffName(&names, seen, segment.StaffName)
	}
	return joinHumanList(names)
}

func sessionAssignedStaffLabel(session Session, staff []StaffOption) string {
	names := []string{}
	seen := map[string]bool{}
	addStaffName(&names, seen, staffName(session.StaffID, staff, session.StaffName))
	for _, segment := range session.BookingSegments {
		addStaffName(&names, seen, staffName(segment.StaffID, staff, ""))
	}
	return joinHumanList(names)
}

func addStaffName(names *[]string, seen map[string]bool, name string) {
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

func joinHumanList(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " and " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", and " + values[len(values)-1]
	}
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

func clampRetentionLimit(limit int) int {
	if limit <= 0 {
		return defaultRetentionLimit
	}
	if limit > maxRetentionLimit {
		return maxRetentionLimit
	}
	return limit
}

func clampWebhookLimit(limit int) int {
	if limit <= 0 {
		return defaultWebhookLimit
	}
	if limit > maxWebhookLimit {
		return maxWebhookLimit
	}
	return limit
}

func normalizeLifecycleStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "", LifecycleActive:
		return LifecycleActive
	case LifecycleArchived:
		return LifecycleArchived
	case LifecycleRedacted:
		return LifecycleRedacted
	default:
		return ""
	}
}
