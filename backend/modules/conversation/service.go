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
	defaultSessionListLimit   = 25
	maxSessionListLimit       = 100
	defaultWebhookLimit       = 50
	maxWebhookLimit           = 100
	maxCustomerNamePrompts    = 3
	availabilityOfferLimit    = 3
	exactAvailabilityLimit    = 24
)

var (
	phonePattern                    = regexp.MustCompile(`(?:\+?1[\s.-]?)?(?:\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4})`)
	emailPattern                    = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	dateTimePattern                 = regexp.MustCompile(`(?i)(\d{4}-\d{2}-\d{2})(?:[ t]+(?:at\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)?)`)
	relativeTimePattern             = regexp.MustCompile(`(?i)\b(today|tomorrow)\b\s*(?:at\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)?`)
	dateOnlyPattern                 = regexp.MustCompile(`(?i)\b(\d{4}-\d{2}-\d{2})\b`)
	monthDateTimePattern            = regexp.MustCompile(`(?i)\b(january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sep|sept|october|oct|november|nov|december|dec)\s+(\d{1,2})(?:st|nd|rd|th)?(?:,)?\s+(?:at\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)\b`)
	timeMonthDatePattern            = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)\s+(?:on\s+)?(january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sep|sept|october|oct|november|nov|december|dec)\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	monthDateOnlyPattern            = regexp.MustCompile(`(?i)\b(january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sep|sept|october|oct|november|nov|december|dec)\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	relativeDayPattern              = regexp.MustCompile(`(?i)\b(today|tomorrow)\b`)
	timeWithMeridiemPattern         = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+|for\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm)(?:$|[^a-z0-9])`)
	offeredSlotNumericTimePattern   = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|bpm|tm)(?:$|[^a-z0-9])`)
	offeredSlotWordTimePattern      = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)(?:\s+([0-5][0-9]|oh\s+[0-9]|fifteen|thirty|forty[- ]five))?\s*(a\.?\s*m\.?|p\.?\s*m\.?|bpm|tm)(?:$|[^a-z0-9])`)
	offeredSlotNumericOClockPattern = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+|for\s+)?(\d{1,2})\s*(?:o\s*['’]?\s*clock|oclock)\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm|bpm|tm)(?:$|[^a-z0-9])`)
	offeredSlotWordOClockPattern    = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s*(?:o\s*['’]?\s*clock|oclock)\s*(a\.?\s*m\.?|p\.?\s*m\.?|am|pm|bpm|tm)(?:$|[^a-z0-9])`)
	slotConfirmationPromptPatterns  = []*regexp.Regexp{
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
	store              Store
	bookingTool        BookingTool
	replyGenerator     ReplyGenerator
	answerContextCache *answerContextCache
	now                func() time.Time
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

type slotTimePreference struct {
	Direction string
	Minutes   int
}

type slotRejection struct {
	Preference slotTimePreference
	Remaining  []OfferedSlot
}

type serviceMatch struct {
	service ServiceOption
	index   int
	end     int
	token   string
}

type serviceEditAction string

const (
	serviceEditNone             serviceEditAction = ""
	serviceEditSelectInitial    serviceEditAction = "initial_select"
	serviceEditAdd              serviceEditAction = "add_service"
	serviceEditReplace          serviceEditAction = "replace_service"
	serviceEditDuplicate        serviceEditAction = "duplicate_service"
	serviceEditClarifyAddSwitch serviceEditAction = "clarify_add_or_switch"
	serviceEditClearAmbiguous   serviceEditAction = "clear_ambiguous_service"
)

type serviceEditDecision struct {
	Action     serviceEditAction
	Candidates []ServiceOption
	Source     string
}

func NewService(store Store, bookingTool BookingTool) *Service {
	return &Service{
		store:              store,
		bookingTool:        bookingTool,
		answerContextCache: newAnswerContextCache(defaultAnswerContextTTL),
		now:                func() time.Time { return time.Now().UTC() },
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
	answerCtx, err := s.loadAnswerContext(ctx, salonID)
	if err != nil {
		return nil, err
	}
	services := answerCtx.Services
	serviceAliases := answerCtx.ServiceAliases
	categoryAliases := answerCtx.CategoryAliases
	staff := answerCtx.Staff
	activeStaff := answerCtx.ActiveStaff
	knowledge := answerCtx.Knowledge

	if handled, updated, err := s.handlePendingCustomerNameConfirmation(ctx, salonID, ownerUserID, *session, message, eventKey, services, staff, cfg, knowledge); handled {
		return updated, err
	}

	if reply := salonIdentityReplyForMessage(message, *session, cfg); reply != "" {
		turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
		turn.AIMessage = reply
		missing := ""
		if hasBookingProgress(*session) {
			missing = missingBookingField(*session)
		}
		finalizeTurnMetadata(&turn, *session, *session, missing, missing, "salon_identity_check")
		return s.store.SaveTurn(ctx, turn)
	}

	if rejection, ok := offeredSlotRejectionForMessage(message, *session, timezoneLocation(timezoneFromConfig(cfg))); ok {
		next := *session
		next.OfferedSlots = rejection.Remaining
		turn := newTurnRecord(salonID, ownerUserID, *session, next, message, eventKey, services, staff, cfg)
		applySlotRejectionMetadata(&turn, rejection)
		if len(next.OfferedSlots) > 0 {
			turn.AIMessage = formatSlotOffer(next.OfferedSlots, timezoneLocation(timezoneFromConfig(cfg)), false)
		} else {
			turn.AIMessage = rejectedSlotNoRemainingReply(rejection.Preference.Direction)
		}
		finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "offered_slot_rejection")
		return s.store.SaveTurn(ctx, turn)
	}

	if reply, handoff := customerNameSlotRepairReply(message, *session, services, serviceAliases, categoryAliases, cfg); reply != "" {
		turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
		if handoff {
			return s.saveHandoffTurn(ctx, turn, *session, HandoffReasonCustomerDetailsUnavailable, reply, services, staff, cfg)
		}
		turn.AIMessage = reply
		s.applyReplyGenerator(ctx, &turn, *session, services, cfg, "customer_name", "customer_name", knowledge)
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
	serviceUnderstanding := interpretServiceForSession(message, *session, services, serviceAliases, categoryAliases)
	if activePartyPlan(session.PartyPlan) && !partyPlanComplete(session.PartyPlan) {
		if reply, ok := partyPlanServiceMenuReply(message, *session, services, cfg); ok {
			turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
			applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
			applyPartyPlanMetadata(&turn, session.PartyPlan)
			turn.AIMessage = reply
			finalizeTurnMetadata(&turn, *session, *session, "service", "service", "party_plan_service_menu")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	if asksServiceMenu(message) && serviceUnderstanding.Status == serviceUnderstandingStatusUnknown {
		route := routeNonBookingAnswer(message, *session, answerCtx, cfg, s.now)
		if route.Handled && route.Source != answerSourceBookingRedirect {
			turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
			turn.AIMessage = route.Reply
			applyAnswerRouteMetadata(&turn, route, answerCtx)
			s.applyReplyGenerator(ctx, &turn, *session, services, cfg, "", "", knowledge)
			finalizeTurnMetadata(&turn, *session, *session, "", "", "answer_router")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	if isServiceInquiry(message, serviceUnderstanding) && !asksStaffQuestion(message, staff, activeStaff) {
		turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
		applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
		applyServiceInquiryMetadata(&turn, serviceUnderstanding)
		route := routeServiceInquiryAnswer(*session, serviceUnderstanding, answerCtx)
		turn.AIMessage = route.Reply
		applyAnswerRouteMetadata(&turn, route, answerCtx)
		finalizeTurnMetadata(&turn, *session, *session, "", "", "service_inquiry")
		return s.store.SaveTurn(ctx, turn)
	}
	if !hasBookingProgress(*session) && !hasBookingSignal(message) {
		route := routeNonBookingAnswer(message, *session, answerCtx, cfg, s.now)
		if route.Handled && route.Source != answerSourceBookingRedirect {
			turn := newTurnRecord(salonID, ownerUserID, *session, *session, message, eventKey, services, staff, cfg)
			turn.AIMessage = route.Reply
			applyAnswerRouteMetadata(&turn, route, answerCtx)
			s.applyReplyGenerator(ctx, &turn, *session, services, cfg, "", "", knowledge)
			finalizeTurnMetadata(&turn, *session, *session, "", "", "answer_router")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	applyExtraction(&next, message, services, serviceAliases, categoryAliases, staff, loc, s.now)
	if shouldRouteReschedule(*session, message) {
		return s.handleRescheduleMessage(ctx, ownerUserID, *session, next, message, eventKey, services, staff, cfg, knowledge)
	}
	partyPlanApplied := false
	partyPlanTouched := false
	if activePartyPlan(next.PartyPlan) && !partyPlanComplete(next.PartyPlan) {
		partyPlanTouched = true
		plan := clonePartyPlan(next.PartyPlan)
		resolvePartyPlanFromMessage(plan, message, services, serviceAliases)
		autoResolveSingleCandidatePartyGroups(plan)
		next.PartyPlan = plan
		next.Intent = IntentBooking
		if partyPlanComplete(plan) {
			partyPlanApplied = applyPartyBookingPlan(&next, partyBookingPlan{
				PartySize: plan.PartySize,
				Segments:  partyPlanSegments(plan, next),
			})
		} else {
			turn := newTurnRecord(salonID, ownerUserID, *session, next, message, eventKey, services, staff, cfg)
			applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
			applyPartyPlanMetadata(&turn, plan)
			turn.AIMessage = partyPlanClarificationPrompt(next, plan, services, cfg)
			finalizeTurnMetadata(&turn, *session, next, "service", "service", "party_plan_clarification")
			return s.store.SaveTurn(ctx, turn)
		}
	} else if shouldGroupBookingHandoff(message) {
		if plan, ok := partyPlanFromMessage(message, services, serviceAliases, categoryAliases, next); ok {
			partyPlanTouched = true
			next.PartyPlan = plan
			next.Intent = IntentBooking
			if partyPlanComplete(plan) {
				partyPlanApplied = applyPartyBookingPlan(&next, partyBookingPlan{
					PartySize: plan.PartySize,
					Segments:  partyPlanSegments(plan, next),
				})
			} else {
				turn := newTurnRecord(salonID, ownerUserID, *session, next, message, eventKey, services, staff, cfg)
				applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
				applyPartyPlanMetadata(&turn, plan)
				turn.AIMessage = partyPlanClarificationPrompt(next, plan, services, cfg)
				finalizeTurnMetadata(&turn, *session, next, "service", "service", "party_plan_clarification")
				return s.store.SaveTurn(ctx, turn)
			}
		}
	}
	serviceEdit := serviceEditDecisionForMessage(*session, message, serviceUnderstanding, services)
	serviceChanged := false
	if !partyPlanTouched {
		serviceChanged = applyServiceEditDecision(&next, serviceEdit)
	}
	serviceChanged = serviceChanged || partyPlanApplied
	if pendingNameCandidate != "" {
		next.CustomerName = ""
	}
	if !serviceChanged && serviceEdit.Action != serviceEditClarifyAddSwitch && serviceEdit.Action != serviceEditClearAmbiguous {
		if selected := selectOfferedSlot(message, session.OfferedSlots, loc); selected != nil && offeredSlotMatchesServiceSelection(*selected, next) {
			applySelectedOfferedSlot(&next, *selected)
			selectedOfferedSlot = true
		} else if selected := selectConfirmedOfferedSlot(message, *session, loc); selected != nil && offeredSlotMatchesServiceSelection(*selected, next) {
			applySelectedOfferedSlot(&next, *selected)
			selectedOfferedSlot = true
		}
	}
	intent := resolveIntent(session.Intent, message, next)
	next.Intent = intent

	turn := newTurnRecord(salonID, ownerUserID, *session, next, message, eventKey, services, staff, cfg)
	applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
	applyServiceEditMetadata(&turn, serviceEdit)

	if partyPlanApplied {
		applyPartyBookingMetadata(&turn, next)
	}

	if shouldComplaintHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'm sorry to hear that. I'll send this to the owner so they can help directly. This is not a confirmed appointment.", services, staff, cfg)
	}

	if shouldHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll pass this to the owner so they can help directly. This is not a confirmed appointment.", services, staff, cfg)
	}

	if pendingNameCandidate != "" && intent == IntentBooking {
		turn.AIMessage = customerNameConfirmationPrompt(pendingNameCandidate)
		setPendingCustomerNameMetadata(&turn, pendingNameCandidate, "voice_short_bare_name")
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, *session, next, "customer_name", "customer_name", "customer_name_confirmation")
		return s.store.SaveTurn(ctx, turn)
	}

	if intent != IntentBooking {
		route := routeNonBookingAnswer(message, next, answerCtx, cfg, s.now)
		turn.AIMessage = route.Reply
		applyAnswerRouteMetadata(&turn, route, answerCtx)
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "", "", knowledge)
		finalizeTurnMetadata(&turn, *session, next, "", "", "knowledge_or_booking_redirect")
		return s.store.SaveTurn(ctx, turn)
	}

	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. I can take the request for the owner, but this is not a confirmed appointment.", services, staff, cfg)
	}

	if serviceEdit.Action == serviceEditClarifyAddSwitch && !partyPlanApplied {
		turn.AIMessage = serviceEditClarificationPrompt(*session, serviceEdit.Candidates, services)
		setPendingServiceEditMetadata(&turn, serviceEdit.Candidates)
		finalizeTurnMetadata(&turn, *session, next, "service", "service", "service_edit_clarification")
		return s.store.SaveTurn(ctx, turn)
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
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "availability_alternative")
			return s.store.SaveTurn(ctx, turn)
		}
		exactRequestedTimeSelected = true
	}

	if missing := missingBookingField(next); missing != "" {
		if missing == "requested_time" || missing == "requested_start_time" {
			if len(next.OfferedSlots) > 0 {
				turn.AIMessage = formatSlotOfferForSession(next.OfferedSlots, loc, false, next, services)
				s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
				finalizeTurnMetadata(&turn, *session, next, missing, missing, "availability_offer_repeated")
				return s.store.SaveTurn(ctx, turn)
			}
			preferredDate := preferredDateForAvailability(next, message, loc, s.now)
			if preferredDate != "" && next.ServiceID != "" {
				if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, preferredDate, false, cfg); err != nil {
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
				}
				if prefix := serviceSwitchAcknowledgement(*session, next, serviceEdit, serviceChanged, services); prefix != "" {
					turn.AIMessage = prefix + " " + turn.AIMessage
				}
				s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
				finalizeTurnMetadata(&turn, *session, next, missing, missing, "availability_offer")
				return s.store.SaveTurn(ctx, turn)
			}
		}
		turn.AIMessage = promptForMissingField(missing)
		if contextual := promptForMissingFieldWithServiceContext(missing, next, services, cfg); contextual != "" {
			turn.AIMessage = contextual
		}
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
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, *session, next, missing, missing, "missing_field")
		return s.store.SaveTurn(ctx, turn)
	}

	return s.tryBooking(ctx, ownerUserID, turn, next, services, staff, cfg, knowledge)
}

func shouldRouteReschedule(session Session, message string) bool {
	return bookingActionForSession(session) == BookingActionReschedule || hasRescheduleSignal(message)
}

func (s *Service) handleRescheduleMessage(ctx context.Context, ownerUserID string, before Session, next Session, message string, eventKey string, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	firstRescheduleTurn := bookingActionForSession(before) != BookingActionReschedule && hasRescheduleSignal(message)
	next.BookingAction = BookingActionReschedule
	next.Intent = IntentBooking
	if firstRescheduleTurn {
		clearNewRescheduleSlot(&next)
	}
	selectedOfferedSlot := false
	if selected := selectOfferedSlot(message, before.OfferedSlots, loc); selected != nil && offeredSlotMatchesServiceSelection(*selected, next) {
		applySelectedOfferedSlot(&next, *selected)
		selectedOfferedSlot = true
	}
	turn := newTurnRecord(before.SalonID, ownerUserID, before, next, message, eventKey, services, staff, cfg)
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{"booking_action": BookingActionReschedule})

	if shouldComplaintHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'm sorry to hear that. I'll send this reschedule request to the owner so they can help directly. The appointment is not rescheduled yet.", services, staff, cfg)
	}
	if shouldHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll pass this reschedule request to the owner so they can help directly. The appointment is not rescheduled yet.", services, staff, cfg)
	}
	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. I can send this reschedule request to the owner, but the appointment is not rescheduled yet.", services, staff, cfg)
	}
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	if strings.TrimSpace(next.CustomerPhone) == "" {
		turn.AIMessage = "What phone number is on the appointment you want to reschedule?"
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_phone", "customer_phone", knowledge)
		finalizeTurnMetadata(&turn, before, next, "customer_phone", "customer_phone", "reschedule_missing_phone")
		return s.store.SaveTurn(ctx, turn)
	}

	if strings.TrimSpace(next.TargetAppointmentID) == "" {
		clearNewRescheduleSlot(&next)
		if selected := selectRescheduleCandidate(message, next.RescheduleCandidates, loc, s.now); selected != nil {
			applyRescheduleCandidate(&next, *selected)
			syncTurnUpdate(&turn, next, services, staff, cfg)
		} else {
			if len(next.RescheduleCandidates) == 0 {
				candidates, err := s.bookingTool.RescheduleCandidates(ctx, before.SalonID, ownerUserID, booking.RescheduleLookupRequest{
					CustomerName:  next.CustomerName,
					CustomerPhone: next.CustomerPhone,
					Limit:         3,
				})
				if err != nil {
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not look up the appointment safely, so I will send this reschedule request to the owner. The appointment is not rescheduled yet.", services, staff, cfg)
				}
				next.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
				syncTurnUpdate(&turn, next, services, staff, cfg)
			}
			switch len(next.RescheduleCandidates) {
			case 0:
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not find an upcoming appointment for this phone number, so I will send this reschedule request to the owner. The appointment is not rescheduled yet.", services, staff, cfg)
			case 1:
				turn.AIMessage = rescheduleSingleCandidatePrompt(next.RescheduleCandidates[0], loc)
			default:
				if isRescheduleTargetFiller(message) && len(before.RescheduleCandidates) > 0 {
					turn.AIMessage = rescheduleConciseTargetPrompt(next.RescheduleCandidates, loc)
				} else {
					turn.AIMessage = rescheduleMultipleCandidatesPrompt(next.RescheduleCandidates, loc)
				}
			}
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "reschedule_target_lookup")
			return s.store.SaveTurn(ctx, turn)
		}
	}

	if !rescheduleTargetAutoSafe(next) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "This reschedule needs owner review because I cannot safely update that appointment automatically. The appointment is not rescheduled yet.", services, staff, cfg)
	}

	if next.RequestedStartTime == nil {
		if applyRelativeRescheduleDate(&next, message, loc) {
			syncTurnUpdate(&turn, next, services, staff, cfg)
		}
		if len(next.OfferedSlots) > 0 {
			turn.AIMessage = offeredSlotSelectionRetryReply(message, next, services, loc)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, before, next, "requested_time", "requested_time", "reschedule_offered_slot_repeated")
			return s.store.SaveTurn(ctx, turn)
		}
		if strings.TrimSpace(next.RequestedDate) != "" {
			if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, next.RequestedDate, false, cfg); err != nil {
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check new appointment times, so I will send this reschedule request to the owner. The appointment is not rescheduled yet.", services, staff, cfg)
			}
			if len(next.OfferedSlots) > 0 {
				turn.AIMessage = formatRescheduleSlotOfferForSession(next.OfferedSlots, loc, false, next, services)
			}
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, before, next, "requested_time", "requested_time", "reschedule_availability_offer")
			return s.store.SaveTurn(ctx, turn)
		}
		if shouldHandoffRepeatedRescheduleNewTime(before, message) {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I'm having trouble catching the new date and time, so I will send this reschedule request to the owner. The appointment is not rescheduled yet.", services, staff, cfg)
		}
		turn.AIMessage = "What new day and time would you like?"
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_start_time", "requested_start_time", knowledge)
		finalizeTurnMetadata(&turn, before, next, "requested_start_time", "requested_start_time", "reschedule_missing_new_time")
		return s.store.SaveTurn(ctx, turn)
	}

	if shouldCheckAvailabilityForRequestedTime(before, next, selectedOfferedSlot) {
		available, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check new appointment times, so I will send this reschedule request to the owner. The appointment is not rescheduled yet.", services, staff, cfg)
		}
		if !available {
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, before, next, "requested_time", "requested_time", "reschedule_availability_alternative")
			return s.store.SaveTurn(ctx, turn)
		}
	}

	return s.tryReschedule(ctx, ownerUserID, turn, next, services, staff, cfg)
}

func (s *Service) List(ctx context.Context, salonID string, ownerUserID string, limit int, offset int, lifecycleStatus string) (*ListSessionsResponse, error) {
	lifecycleStatus = normalizeLifecycleStatus(lifecycleStatus)
	if lifecycleStatus == "" {
		return nil, ErrValidation
	}
	pageLimit := clampLimit(limit)
	pageOffset := clampOffset(offset)
	items, err := s.store.ListSessions(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), lifecycleStatus, pageLimit+1, pageOffset)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > pageLimit
	if hasMore {
		items = items[:pageLimit]
	}
	return &ListSessionsResponse{
		Sessions: items,
		Limit:    pageLimit,
		Offset:   pageOffset,
		HasMore:  hasMore,
	}, nil
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
	answerCtx, err := s.loadAnswerContext(ctx, salonID)
	if err != nil {
		return TranscriptionContext{}, err
	}
	return TranscriptionContext{
		Prompt: transcriptionContextPrompt(*session, cfg, answerCtx.Services, answerCtx.ServiceAliases),
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

func (s *Service) ListPartyBookingRequests(ctx context.Context, salonID string, ownerUserID string, status string, limit int) ([]PartyBookingRequest, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	status = strings.TrimSpace(status)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	if status != "" && status != "all" && !allowedPartyRequestStatus(status) {
		return nil, ErrValidation
	}
	return s.store.ListPartyBookingRequests(ctx, salonID, ownerUserID, status, clampLimit(limit))
}

func (s *Service) UpdatePartyBookingRequestStatus(ctx context.Context, salonID string, ownerUserID string, requestID string, status string) (*PartyBookingRequest, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	requestID = strings.TrimSpace(requestID)
	status = strings.TrimSpace(status)
	if salonID == "" || ownerUserID == "" || requestID == "" || !allowedPartyRequestStatus(status) {
		return nil, ErrValidation
	}
	return s.store.UpdatePartyBookingRequestStatus(ctx, salonID, ownerUserID, requestID, status)
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
	limit := availabilityOfferLimit
	if _, ok := activeSlotTimePreference(session); ok {
		limit = exactAvailabilityLimit
	}
	return s.availableSlotsWithLimit(ctx, salonID, ownerUserID, session, preferredDate, limit)
}

func (s *Service) availableSlotsWithLimit(ctx context.Context, salonID string, ownerUserID string, session Session, preferredDate string, limit int) (*booking.AvailabilityResult, error) {
	if s.bookingTool == nil {
		return nil, fmt.Errorf("booking tool is unavailable")
	}
	staffSelectionMode := staffSelectionModeForAvailability(session)
	if limit <= 0 {
		limit = availabilityOfferLimit
	}
	req := booking.AvailabilityRequest{
		ServiceID:          session.ServiceID,
		StaffID:            staffIDForAvailability(session),
		StaffSelectionMode: staffSelectionMode,
		Segments:           availabilitySegmentsForSession(session, staffSelectionMode),
		PreferredDate:      preferredDate,
		Limit:              limit,
	}
	result, err := s.bookingTool.AvailableSlots(ctx, salonID, ownerUserID, req)
	if err != nil || result == nil {
		return result, err
	}
	if strings.TrimSpace(result.StaffSelectionMode) == "" {
		result.StaffSelectionMode = req.StaffSelectionMode
	}
	for i := range result.Slots {
		if strings.TrimSpace(result.Slots[i].StaffSelectionMode) == "" {
			result.Slots[i].StaffSelectionMode = req.StaffSelectionMode
		}
		for j := range result.Slots[i].Segments {
			if strings.TrimSpace(result.Slots[i].Segments[j].StaffSelectionMode) == "" {
				result.Slots[i].Segments[j].StaffSelectionMode = req.StaffSelectionMode
			}
		}
	}
	return result, nil
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
	offered := offeredSlotsFromAvailabilityForSession(result, *session, timezoneLocation(timezoneFromConfig(cfg)))
	session.RequestedStartTime = nil
	session.OfferedSlots = offered
	turn.ToolMessage = availabilityToolMessage(len(offered))
	if len(offered) == 0 {
		turn.AIMessage = "I do not see open times for that day. What other day works?"
	} else {
		turn.AIMessage = formatSlotOfferForSession(offered, timezoneLocation(cfg.Timezone), unavailableRequestedTime, *session, services)
	}
	syncTurnUpdate(turn, *session, services, staff, cfg)
}

func offeredSlotsFromAvailability(result *booking.AvailabilityResult) []OfferedSlot {
	return offeredSlotsFromAvailabilityLimit(result, availabilityOfferLimit)
}

func offeredSlotsFromAvailabilityLimit(result *booking.AvailabilityResult, limit int) []OfferedSlot {
	if result == nil || len(result.Slots) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(result.Slots) {
		limit = len(result.Slots)
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

func offeredSlotsFromAvailabilityForSession(result *booking.AvailabilityResult, session Session, loc *time.Location) []OfferedSlot {
	limit := availabilityOfferLimit
	if _, ok := activeSlotTimePreference(session); ok {
		limit = exactAvailabilityLimit
	}
	offered := offeredSlotsFromAvailabilityLimit(result, limit)
	if len(offered) == 0 {
		return nil
	}
	if preference, ok := activeSlotTimePreference(session); ok {
		offered = filterOfferedSlotsByPreference(offered, preference, loc)
	}
	if len(offered) > availabilityOfferLimit {
		offered = offered[:availabilityOfferLimit]
	}
	return offered
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

func offeredSlotMatchesServiceSelection(slot OfferedSlot, session Session) bool {
	want := selectedServiceIDs(session)
	if len(want) == 0 {
		return true
	}
	got := offeredSlotServiceIDs(slot)
	if len(got) == 0 {
		return true
	}
	return sameStringSlices(want, got)
}

func offeredSlotServiceIDs(slot OfferedSlot) []string {
	if len(slot.Segments) == 0 {
		return nil
	}
	out := make([]string, 0, len(slot.Segments))
	for _, segment := range slot.Segments {
		if serviceID := strings.TrimSpace(segment.ServiceID); serviceID != "" {
			out = append(out, serviceID)
		}
	}
	return out
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

func formatSlotOfferForSession(slots []OfferedSlot, loc *time.Location, unavailableRequestedTime bool, session Session, services []ServiceOption) string {
	service := strings.TrimSpace(serviceSummary(session, services))
	if service == "" {
		return formatSlotOffer(slots, loc, unavailableRequestedTime)
	}
	options := formatSlotOptions(slots, loc)
	if unavailableRequestedTime {
		return "That time is not available. For your " + service + ", I found these openings: " + options + ". Which works?"
	}
	return "For your " + service + ", I found these openings: " + options + ". Which works?"
}

func formatRescheduleSlotOfferForSession(slots []OfferedSlot, loc *time.Location, unavailableRequestedTime bool, session Session, services []ServiceOption) string {
	service := strings.TrimSpace(serviceSummary(session, services))
	prefix := "I found openings"
	if service != "" {
		prefix += " for your " + service
	}
	if unavailableRequestedTime {
		prefix = "That time is not available. " + prefix
	}
	if day, times, staffPhrase, ok := compactSameDaySlotOptions(slots, loc); ok {
		return prefix + " on " + day + " at " + joinHumanList(times) + staffPhrase + ". Which time works?"
	}
	return prefix + ": " + formatSlotOptions(slots, loc) + ". Which time works?"
}

func compactSameDaySlotOptions(slots []OfferedSlot, loc *time.Location) (string, []string, string, bool) {
	if len(slots) == 0 {
		return "", nil, "", false
	}
	if loc == nil {
		loc = time.UTC
	}
	firstLocal := slots[0].StartTime.In(loc)
	day := firstLocal.Format("Monday, January 2")
	staffPhrase := slotStaffPhrase(slots[0])
	times := make([]string, 0, len(slots))
	for _, slot := range slots {
		local := slot.StartTime.In(loc)
		if local.Format("2006-01-02") != firstLocal.Format("2006-01-02") {
			return "", nil, "", false
		}
		if slotStaffPhrase(slot) != staffPhrase {
			return "", nil, "", false
		}
		times = append(times, local.Format("3:04 PM"))
	}
	return day, times, staffPhrase, true
}

func offeredSlotSelectionRetryReply(message string, session Session, services []ServiceOption, loc *time.Location) string {
	reply := formatSlotOfferForSession(session.OfferedSlots, loc, false, session, services)
	if looksLikeUnclearOClockTime(message) {
		return "I heard a time but not clearly. " + reply
	}
	return reply
}

func looksLikeUnclearOClockTime(message string) bool {
	if len(clockCandidatesFromText(message)) > 0 {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "o clock") ||
		strings.Contains(normalized, "oclock") ||
		strings.Contains(normalized, "clock am") ||
		strings.Contains(normalized, "clock pm")
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
		label := ordinalSpeechLabel(i + 1)
		when := slot.StartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
		when += slotStaffPhrase(slot)
		parts = append(parts, label+" "+when)
	}
	return strings.Join(parts, "; ")
}

func slotStaffPhrase(slot OfferedSlot) string {
	if slotUsesAnyone(slot) {
		return availableTechnicianPhrase(slot)
	}
	if assigned := slotAssignedStaffLabel(slot); assigned != "" {
		return " with " + assigned
	}
	return ""
}

func selectedRequestedTimeReply(session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig, missing string) string {
	prompt := promptForMissingField(missing)
	if session.RequestedStartTime == nil {
		return prompt
	}
	loc := timezoneLocation(timezoneFromConfig(cfg))
	when := session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	sentence := when + " is available"
	if service := strings.TrimSpace(serviceSummary(session, services)); service != "" {
		sentence += " for your " + service
	}
	if sessionUsesAnyone(session) {
		sentence += availableTechnicianPhraseForSegments(session.BookingSegments)
	} else if assigned := sessionAssignedStaffLabel(session, staff); assigned != "" {
		sentence += " with " + assigned
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

func ordinalSpeechLabel(index int) string {
	switch index {
	case 1:
		return "First,"
	case 2:
		return "Second,"
	case 3:
		return "Third,"
	default:
		return fmt.Sprintf("%d.", index)
	}
}

func syncTurnUpdate(turn *TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) {
	turn.Update.BookingAction = bookingActionForSession(session)
	turn.Update.TargetAppointmentID = session.TargetAppointmentID
	turn.Update.RescheduleCandidates = session.RescheduleCandidates
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
	turn.Update.PartyPlan = clonePartyPlan(session.PartyPlan)
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
			Status:               StatusActive,
			Intent:               after.Intent,
			Outcome:              OutcomeCollecting,
			BookingAction:        bookingActionForSession(after),
			TargetAppointmentID:  after.TargetAppointmentID,
			RescheduleCandidates: after.RescheduleCandidates,
			CustomerName:         after.CustomerName,
			CustomerPhone:        after.CustomerPhone,
			CustomerEmail:        after.CustomerEmail,
			ServiceID:            after.ServiceID,
			StaffID:              after.StaffID,
			StaffSelectionMode:   staffSelectionModeForSession(after),
			RequestedDate:        after.RequestedDate,
			RequestedStartTime:   after.RequestedStartTime,
			OfferedSlots:         after.OfferedSlots,
			BookingSegments:      after.BookingSegments,
			PartyPlan:            clonePartyPlan(after.PartyPlan),
			Summary:              summaryFor(after, services, staff, cfg),
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
		"service_understanding_category_id":   result.MatchedCategoryID,
		"service_understanding_category":      result.MatchedCategoryName,
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
	if !bookingServiceSelectionConsistent(session) {
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
	s.applyReplyGenerator(ctx, &turn, session, services, cfg, "", "", knowledge)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "booking_result")
	return s.store.SaveTurn(ctx, turn)
}

func (s *Service) tryReschedule(ctx context.Context, ownerUserID string, turn TurnRecord, session Session, services []ServiceOption, staff []StaffOption, cfg *RuntimeConfig) (*Session, error) {
	if s.bookingTool == nil || strings.TrimSpace(session.TargetAppointmentID) == "" || session.RequestedStartTime == nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}
	appointment, fallback, err := s.bookingTool.Reschedule(ctx, turn.SalonID, ownerUserID, session.TargetAppointmentID, booking.RescheduleRequest{
		Source:    bookingSourceForSession(session),
		StartTime: *session.RequestedStartTime,
		StaffID:   session.StaffID,
		Notes:     "AI receptionist reschedule request.",
	})
	if err != nil {
		return s.saveHandoffTurn(ctx, turn, session, HandoffReasonBookingUnavailable, rescheduleErrorReply(), services, staff, cfg)
	}

	toolMessage := "Booking service returned reschedule fallback pending."
	outcome := OutcomeBookingFallbackPending
	status := StatusCompleted
	aiMessage := rescheduleFallbackReply()
	bookingAttemptID := ""
	appointmentID := ""
	if fallback != nil {
		bookingAttemptID = fallback.ID
	}
	if appointment != nil && appointment.Status == booking.StatusRescheduled && appointment.POSAppointmentID != "" {
		toolMessage = "Booking service rescheduled appointment through POS."
		outcome = OutcomeBookingRescheduled
		aiMessage = rescheduledMessage(session, cfg)
		appointmentID = appointment.ID
	} else if fallback == nil {
		toolMessage = "Booking service returned no reschedule result."
	}

	turn.ToolMessage = toolMessage
	turn.AIMessage = aiMessage
	turn.Update.Status = status
	turn.Update.Outcome = outcome
	turn.Update.BookingAction = BookingActionReschedule
	turn.Update.TargetAppointmentID = session.TargetAppointmentID
	turn.Update.RescheduleCandidates = nil
	turn.Update.BookingAttemptID = bookingAttemptID
	turn.Update.AppointmentID = appointmentID
	turn.Update.OfferedSlots = nil
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(session, services, staff, cfg)
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "reschedule_result")
	return s.store.SaveTurn(ctx, turn)
}

func bookingFallbackReply() string {
	return "I couldn't confirm the appointment, so I sent the request to the owner to review. This is not a confirmed appointment."
}

func bookingErrorReply() string {
	return "I couldn't complete the booking right now, so the owner needs to review it. This is not a confirmed appointment."
}

func rescheduleFallbackReply() string {
	return "I couldn't reschedule the appointment, so I sent the request to the owner to review. The original appointment has not been changed."
}

func rescheduleErrorReply() string {
	return "I couldn't complete the reschedule right now, so the owner needs to review it. The original appointment has not been changed."
}

func rescheduledMessage(session Session, cfg *RuntimeConfig) string {
	loc := timezoneLocation("")
	if cfg != nil {
		loc = timezoneLocation(cfg.Timezone)
	}
	when := ""
	if session.RequestedStartTime != nil {
		when = session.RequestedStartTime.In(loc).Format("Monday, January 2 at 3:04 PM")
	}
	salon := salonName(cfg)
	prefix := "Your appointment has been rescheduled"
	if salon != "" {
		prefix += " with " + salon
	}
	if when != "" {
		return prefix + " to " + when + ". Thank you, goodbye."
	}
	return prefix + ". Thank you, goodbye."
}

func bookingServiceSelectionConsistent(session Session) bool {
	if len(session.BookingSegments) == 0 {
		return true
	}
	primaryServiceID := strings.TrimSpace(session.ServiceID)
	if primaryServiceID == "" {
		return false
	}
	hasPrimary := false
	for _, segment := range session.BookingSegments {
		segmentServiceID := strings.TrimSpace(segment.ServiceID)
		if segmentServiceID == "" {
			continue
		}
		if segmentServiceID == primaryServiceID {
			hasPrimary = true
		}
	}
	return hasPrimary
}

func (s *Service) applyReplyGenerator(ctx context.Context, turn *TurnRecord, session Session, services []ServiceOption, cfg *RuntimeConfig, missing string, nextRequired string, knowledge []KnowledgeSnippet) {
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
		SalonID:              turn.SalonID,
		SessionID:            session.ID,
		Channel:              session.Channel,
		Intent:               turn.Update.Intent,
		Outcome:              turn.Update.Outcome,
		CustomerMessage:      turn.CustomerMessage,
		SafeReply:            turn.AIMessage,
		SalonName:            salonName(cfg),
		AITone:               aiTone(cfg),
		BookingConfirmed:     turn.Update.Outcome == OutcomeBookingConfirmed && turn.Update.BookingAttemptID != "" && turn.Update.AppointmentID != "",
		FallbackOrHandoff:    turn.Update.Outcome == OutcomeBookingFallbackPending || turn.Update.Outcome == OutcomeAIDisabled || turn.Update.Outcome == OutcomeHandoffRequested,
		MissingBookingField:  missing,
		KnownBookingFields:   knownBookingFields(session),
		NextRequiredField:    nextRequired,
		SelectedServiceNames: selectedServiceNames(session, services),
		Summary:              turn.Update.Summary,
		KnowledgeContext:     formatKnowledgeContext(knowledge),
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
		if rejectRescheduleReplyRewrite(turn, nextRequired, message) {
			turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
				"safe_reply":          safeReply,
				"llm_reply":           message,
				"llm_confidence":      result.Confidence,
				"llm_handoff":         result.Handoff,
				"llm_reason":          result.Reason,
				"llm_guardrail":       "rejected_reschedule_stage_flip",
				"reply_source":        "safe_reply",
				"next_required_field": nextRequired,
			})
			return
		}
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

func rejectRescheduleReplyRewrite(turn *TurnRecord, nextRequired string, message string) bool {
	if turn == nil || nextRequired != "target_appointment" || turn.Update.BookingAction != BookingActionReschedule {
		return false
	}
	if strings.TrimSpace(turn.Update.TargetAppointmentID) != "" {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	stageFlipSignals := []string{
		"new time",
		"new day",
		"new date",
		"what time",
		"what day",
		"schedule your",
		"reschedule your",
	}
	for _, signal := range stageFlipSignals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
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
	if reason == HandoffReasonGroupBooking {
		turn.PartyRequest = partyRequestRecordFromSession(turn, session, services, cfg, summary)
	}
	finalizeTurnMetadata(&turn, turn.Session, session, "", "", "handoff")
	return s.store.SaveTurn(ctx, turn)
}

func partyRequestRecordFromSession(turn TurnRecord, session Session, services []ServiceOption, cfg *RuntimeConfig, summary string) *PartyRequestRecord {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	requestedTimeWindow := ""
	if session.RequestedStartTime != nil {
		requestedTimeWindow = session.RequestedStartTime.In(loc).Format("3:04 PM")
	}
	return &PartyRequestRecord{
		EventKey:             normalizeEventKey(turn.EventKey),
		PartySize:            partySizeFromMessage(turn.CustomerMessage),
		RepresentativeName:   session.CustomerName,
		RepresentativePhone:  session.CustomerPhone,
		RequestedDate:        session.RequestedDate,
		RequestedTimeWindow:  requestedTimeWindow,
		GuestServiceRequests: partyGuestServicesFromSession(session, services),
		Summary:              summary,
	}
}

func partyGuestServicesFromSession(session Session, services []ServiceOption) []PartyGuestService {
	byID := map[string]ServiceOption{}
	for _, service := range services {
		byID[strings.TrimSpace(service.ID)] = service
	}
	items := make([]PartyGuestService, 0)
	seen := map[string]bool{}
	for _, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" || seen[serviceID] {
			continue
		}
		seen[serviceID] = true
		item := PartyGuestService{ServiceID: serviceID}
		if service, ok := byID[serviceID]; ok {
			item.ServiceName = service.Name
		}
		items = append(items, item)
	}
	if len(items) == 0 && strings.TrimSpace(session.ServiceID) != "" {
		serviceID := strings.TrimSpace(session.ServiceID)
		item := PartyGuestService{ServiceID: serviceID}
		if service, ok := byID[serviceID]; ok {
			item.ServiceName = service.Name
		} else if strings.TrimSpace(session.ServiceName) != "" {
			item.ServiceName = session.ServiceName
		}
		items = append(items, item)
	}
	return items
}

func partySizeFromMessage(message string) int {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return 0
	}
	if strings.Contains(normalized, "my friend and i") || strings.Contains(normalized, "me and my friend") {
		return 2
	}
	wordNumbers := map[string]int{
		"two":   2,
		"three": 3,
		"four":  4,
		"five":  5,
		"six":   6,
	}
	if strings.Contains(normalized, "me and two friends") {
		return 3
	}
	if strings.Contains(normalized, "me and three friends") {
		return 4
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`party of ([2-9])`),
		regexp.MustCompile(`for ([2-9]) people`),
		regexp.MustCompile(`([2-9]) people`),
		regexp.MustCompile(`([2-9]) appointments`),
	}
	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(normalized); len(matches) == 2 {
			if value, err := strconv.Atoi(matches[1]); err == nil {
				return value
			}
		}
	}
	for word, value := range wordNumbers {
		if strings.Contains(normalized, "party of "+word) ||
			strings.Contains(normalized, "for "+word+" people") ||
			strings.Contains(normalized, word+" people") ||
			strings.Contains(normalized, word+" appointments") {
			return value
		}
	}
	return 0
}

type partyBookingPlan struct {
	PartySize int
	Segments  []booking.BookingSegmentRequest
}

type partyServiceCountMatch struct {
	Start     int
	End       int
	Count     int
	Service   ServiceOption
	PhraseLen int
}

type partyServicePhrase struct {
	Service ServiceOption
	Phrase  string
	Family  bool
}

func partyBookingPlanFromMessage(message string, services []ServiceOption, session Session) (partyBookingPlan, bool) {
	partySize := partySizeFromMessage(message)
	counted := partyServiceCountSegmentsFromMessage(message, services, session)
	if len(counted) > 0 {
		if partySize > 0 && len(counted) != partySize {
			return partyBookingPlan{}, false
		}
		if partySize == 0 {
			partySize = len(counted)
		}
		return partyBookingPlan{PartySize: partySize, Segments: counted}, true
	}
	if partySize < 2 {
		return partyBookingPlan{}, false
	}
	service, ok := singlePartyServiceFromMessage(message, services)
	if !ok {
		if strings.TrimSpace(session.ServiceID) == "" {
			return partyBookingPlan{}, false
		}
		service = ServiceOption{ID: session.ServiceID, Name: session.ServiceName}
	}
	segments := partySegmentsForService(service, partySize, session)
	if len(segments) != partySize {
		return partyBookingPlan{}, false
	}
	return partyBookingPlan{PartySize: partySize, Segments: segments}, true
}

type partyPlanPhrase struct {
	Phrase     string
	Label      string
	Candidates []ServiceOption
}

type partyPlanCountMatch struct {
	Start     int
	End       int
	Count     int
	Phrase    partyPlanPhrase
	PhraseLen int
}

type partyPlanServiceSelection struct {
	Start     int
	End       int
	Service   ServiceOption
	PhraseLen int
}

func partyPlanFromMessage(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, session Session) (*PartyPlan, bool) {
	partySize := partySizeFromMessage(message)
	groups := partyPlanGroupsFromMessage(message, services, aliases, categoryAliases)
	if len(groups) > 0 {
		total := 0
		for _, group := range groups {
			total += group.Count
		}
		if total < 2 {
			return nil, false
		}
		if partySize > 0 && total != partySize {
			return nil, false
		}
		if partySize == 0 {
			partySize = total
		}
		plan := &PartyPlan{PartySize: partySize, Groups: groups}
		autoResolveSingleCandidatePartyGroups(plan)
		return plan, true
	}
	if plan, ok := partyBookingPlanFromMessage(message, services, session); ok {
		return completedPartyPlanFromSegments(plan, services), true
	}
	return nil, false
}

func partyPlanGroupsFromMessage(message string, services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) []PartyPlanGroup {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return nil
	}
	phrases := partyPlanPhrases(services, aliases, categoryAliases)
	matches := make([]partyPlanCountMatch, 0)
	for _, phrase := range phrases {
		if strings.TrimSpace(phrase.Phrase) == "" || len(phrase.Candidates) == 0 {
			continue
		}
		pattern := regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+` + regexp.QuoteMeta(phrase.Phrase) + `\b`)
		for _, indexes := range pattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			countToken := normalized[indexes[2]:indexes[3]]
			count, ok := partyCountTokenValue(countToken)
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyPlanCountMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Phrase:    phrase,
				PhraseLen: len(phrase.Phrase),
			})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].PhraseLen > matches[j].PhraseLen
		}
		return matches[i].Start < matches[j].Start
	})
	accepted := make([]partyPlanCountMatch, 0, len(matches))
	for _, match := range matches {
		if partyPlanCountMatchOverlaps(accepted, match) {
			continue
		}
		accepted = append(accepted, match)
	}
	groups := make([]PartyPlanGroup, 0, len(accepted))
	for _, match := range accepted {
		group := PartyPlanGroup{
			Label:               partyPlanGroupLabel(match.Phrase),
			Count:               match.Count,
			CandidateServiceIDs: serviceIDsFromOptions(match.Phrase.Candidates),
		}
		if len(group.CandidateServiceIDs) == 1 {
			group.ResolvedServiceIDs = repeatedString(group.CandidateServiceIDs[0], group.Count)
		}
		if group.Count > 0 && len(group.CandidateServiceIDs) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func partyPlanPhrases(services []ServiceOption, aliases []ServiceAlias, categoryAliases []ServiceCategoryAlias) []partyPlanPhrase {
	servicesByID := map[string]ServiceOption{}
	categoryServices := map[string][]ServiceOption{}
	tokenServices := map[string][]ServiceOption{}
	for _, service := range services {
		serviceID := strings.TrimSpace(service.ID)
		if serviceID == "" {
			continue
		}
		servicesByID[serviceID] = service
		if categoryID := strings.TrimSpace(service.CategoryID); categoryID != "" {
			categoryServices[categoryID] = appendUniqueService(categoryServices[categoryID], service)
		}
		for _, token := range serviceNameTokens(service.Name) {
			tokenServices[singularServiceToken(token)] = appendUniqueService(tokenServices[singularServiceToken(token)], service)
		}
	}
	seen := map[string]bool{}
	phrases := make([]partyPlanPhrase, 0)
	addPhrase := func(label string, phrase string, candidates []ServiceOption) {
		phrase = normalizeServiceText(phrase)
		candidates = orderedServices(candidates)
		if phrase == "" || len(candidates) == 0 {
			return
		}
		key := phrase + "\x00" + strings.Join(serviceIDsFromOptions(candidates), ",")
		if seen[key] {
			return
		}
		seen[key] = true
		phrases = append(phrases, partyPlanPhrase{
			Phrase:     phrase,
			Label:      strings.TrimSpace(label),
			Candidates: candidates,
		})
	}
	for _, service := range services {
		name := strings.TrimSpace(service.Name)
		if strings.TrimSpace(service.ID) == "" || name == "" {
			continue
		}
		addPhrase(name, name, []ServiceOption{service})
		addPhrase(name, pluralServicePhrase(name), []ServiceOption{service})
	}
	for _, alias := range aliases {
		service, ok := servicesByID[strings.TrimSpace(alias.ServiceID)]
		if !ok {
			continue
		}
		phrase := strings.TrimSpace(alias.NormalizedAlias)
		if phrase == "" {
			phrase = alias.Alias
		}
		addPhrase(service.Name, phrase, []ServiceOption{service})
		addPhrase(service.Name, pluralServicePhrase(phrase), []ServiceOption{service})
	}
	categoryLabels := map[string]string{}
	for categoryID, items := range categoryServices {
		label := ""
		for _, item := range items {
			if item.CategoryName != "" {
				label = item.CategoryName
				break
			}
		}
		if label == "" {
			continue
		}
		categoryLabels[categoryID] = label
		addPhrase(label, label, items)
		addPhrase(label, pluralServicePhrase(label), items)
	}
	for _, alias := range categoryAliases {
		items := categoryServices[strings.TrimSpace(alias.CategoryID)]
		if len(items) == 0 {
			continue
		}
		label := strings.TrimSpace(alias.CategoryName)
		if label == "" {
			label = categoryLabels[strings.TrimSpace(alias.CategoryID)]
		}
		if label == "" {
			label = alias.Alias
		}
		phrase := strings.TrimSpace(alias.NormalizedAlias)
		if phrase == "" {
			phrase = alias.Alias
		}
		addPhrase(label, phrase, items)
		addPhrase(label, pluralServicePhrase(phrase), items)
	}
	for token, items := range tokenServices {
		if token == "" {
			continue
		}
		addPhrase(token, token, items)
		addPhrase(token, pluralServicePhrase(token), items)
	}
	sort.SliceStable(phrases, func(i, j int) bool {
		if len(phrases[i].Phrase) == len(phrases[j].Phrase) {
			return phrases[i].Phrase < phrases[j].Phrase
		}
		return len(phrases[i].Phrase) > len(phrases[j].Phrase)
	})
	return phrases
}

func partyPlanGroupLabel(phrase partyPlanPhrase) string {
	label := normalizeServiceText(phrase.Label)
	if label == "" {
		label = normalizeServiceText(phrase.Phrase)
	}
	parts := strings.Fields(label)
	if len(parts) == 0 {
		return "service"
	}
	return singularServiceToken(parts[len(parts)-1])
}

func partyPlanCountMatchOverlaps(accepted []partyPlanCountMatch, candidate partyPlanCountMatch) bool {
	for _, item := range accepted {
		if candidate.Start < item.End && item.Start < candidate.End {
			return true
		}
	}
	return false
}

func completedPartyPlanFromSegments(plan partyBookingPlan, services []ServiceOption) *PartyPlan {
	if len(plan.Segments) == 0 {
		return nil
	}
	groups := make([]PartyPlanGroup, 0, len(plan.Segments))
	for _, segment := range plan.Segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		label := serviceName(serviceID, services, "service")
		if len(groups) > 0 {
			last := &groups[len(groups)-1]
			if len(last.ResolvedServiceIDs) > 0 && last.ResolvedServiceIDs[0] == serviceID {
				last.Count++
				last.ResolvedServiceIDs = append(last.ResolvedServiceIDs, serviceID)
				continue
			}
		}
		groups = append(groups, PartyPlanGroup{
			Label:               label,
			Count:               1,
			CandidateServiceIDs: []string{serviceID},
			ResolvedServiceIDs:  []string{serviceID},
		})
	}
	if len(groups) == 0 {
		return nil
	}
	partySize := plan.PartySize
	if partySize == 0 {
		for _, group := range groups {
			partySize += group.Count
		}
	}
	return &PartyPlan{PartySize: partySize, Groups: groups}
}

func activePartyPlan(plan *PartyPlan) bool {
	return plan != nil && (plan.PartySize > 0 || len(plan.Groups) > 0)
}

func clonePartyPlan(plan *PartyPlan) *PartyPlan {
	if plan == nil {
		return nil
	}
	out := &PartyPlan{
		PartySize: plan.PartySize,
		Groups:    make([]PartyPlanGroup, 0, len(plan.Groups)),
	}
	for _, group := range plan.Groups {
		out.Groups = append(out.Groups, PartyPlanGroup{
			Label:               group.Label,
			Count:               group.Count,
			CandidateServiceIDs: append([]string(nil), group.CandidateServiceIDs...),
			ResolvedServiceIDs:  append([]string(nil), group.ResolvedServiceIDs...),
		})
	}
	return out
}

func partyPlanComplete(plan *PartyPlan) bool {
	if plan == nil || plan.PartySize < 2 || len(plan.Groups) == 0 {
		return false
	}
	total := 0
	for _, group := range plan.Groups {
		if group.Count <= 0 {
			return false
		}
		total += group.Count
		resolved := nonEmptyStrings(group.ResolvedServiceIDs)
		if len(resolved) != group.Count {
			return false
		}
	}
	return total == plan.PartySize
}

func autoResolveSingleCandidatePartyGroups(plan *PartyPlan) {
	if plan == nil {
		return
	}
	for i := range plan.Groups {
		group := &plan.Groups[i]
		candidates := nonEmptyStrings(group.CandidateServiceIDs)
		if len(candidates) != 1 || group.Count <= 0 {
			continue
		}
		resolved := nonEmptyStrings(group.ResolvedServiceIDs)
		for len(resolved) < group.Count {
			resolved = append(resolved, candidates[0])
		}
		if len(resolved) > group.Count {
			resolved = resolved[:group.Count]
		}
		group.ResolvedServiceIDs = resolved
	}
}

func partyPlanServiceMenuReply(message string, session Session, services []ServiceOption, cfg *RuntimeConfig) (string, bool) {
	plan := session.PartyPlan
	groupIndex := firstUnresolvedPartyPlanGroup(plan)
	if groupIndex < 0 {
		return "", false
	}
	group := plan.Groups[groupIndex]
	candidates := servicesByIDs(services, group.CandidateServiceIDs)
	if len(candidates) == 0 || !asksPartyPlanServiceMenu(message, group, candidates) {
		return "", false
	}
	options := serviceCandidateNames(candidates, 6)
	if len(options) == 0 {
		return "", false
	}
	label := strings.TrimSpace(group.Label)
	if label == "" {
		label = "service"
	}
	reply := "We offer " + joinHumanList(options) + " for " + pluralServicePhrase(label) + ". "
	reply += partyPlanClarificationPrompt(session, plan, services, cfg)
	return reply, true
}

func asksPartyPlanServiceMenu(message string, group PartyPlanGroup, candidates []ServiceOption) bool {
	if asksServiceMenu(message) {
		return true
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if !strings.Contains(normalized, "what") && !strings.Contains(normalized, "which") && !strings.Contains(normalized, "list") {
		return false
	}
	if !strings.Contains(normalized, "service") && !strings.Contains(normalized, "option") && !strings.Contains(normalized, "have") && !strings.Contains(normalized, "offer") {
		return false
	}
	for _, phrase := range partyPlanGroupMenuPhrases(group, candidates) {
		if containsLoosePhrase(normalized, phrase) {
			return true
		}
	}
	return false
}

func partyPlanGroupMenuPhrases(group PartyPlanGroup, candidates []ServiceOption) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(value string) {
		value = normalizeServiceText(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
		if plural := pluralServicePhrase(value); plural != "" && !seen[plural] {
			seen[plural] = true
			out = append(out, plural)
		}
	}
	add(group.Label)
	for _, candidate := range candidates {
		add(candidate.CategoryName)
		add(candidate.CategorySlug)
		for _, token := range serviceNameTokens(candidate.Name) {
			add(singularServiceToken(token))
		}
	}
	return out
}

func resolvePartyPlanFromMessage(plan *PartyPlan, message string, services []ServiceOption, aliases []ServiceAlias) bool {
	if plan == nil {
		return false
	}
	groupIndex := firstUnresolvedPartyPlanGroup(plan)
	if groupIndex < 0 {
		return false
	}
	group := &plan.Groups[groupIndex]
	remaining := group.Count - len(nonEmptyStrings(group.ResolvedServiceIDs))
	if remaining <= 0 {
		return false
	}
	candidates := servicesByIDs(services, group.CandidateServiceIDs)
	if isAffirmativeOnly(message) {
		switch {
		case len(candidates) == 1:
			group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, repeatedString(candidates[0].ID, remaining)...)
			return true
		case remaining > 1 && remaining == len(candidates):
			group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, serviceIDsFromOptions(candidates)...)
			return true
		default:
			return false
		}
	}
	selected := partyPlanSelectedServices(message, candidates, services, aliases, *group)
	if len(selected) == 0 {
		return false
	}
	ids := serviceIDsFromOptions(selected)
	switch {
	case len(ids) == remaining:
		group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, ids...)
	case len(ids) == 1 && remaining > 1 && partyPlanCanRepeatSingleSelection(message):
		group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, repeatedString(ids[0], remaining)...)
	case len(ids) < remaining:
		group.ResolvedServiceIDs = append(group.ResolvedServiceIDs, ids...)
	default:
		return false
	}
	return true
}

func firstUnresolvedPartyPlanGroup(plan *PartyPlan) int {
	if plan == nil {
		return -1
	}
	for i, group := range plan.Groups {
		if group.Count > len(nonEmptyStrings(group.ResolvedServiceIDs)) {
			return i
		}
	}
	return -1
}

func partyPlanSelectedServices(message string, candidates []ServiceOption, services []ServiceOption, aliases []ServiceAlias, group PartyPlanGroup) []ServiceOption {
	normalized := normalizeServiceText(message)
	if normalized == "" || len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 && isAffirmativeOnly(message) {
		return append([]ServiceOption(nil), candidates[0])
	}
	pool := partyPlanSelectionPool(services, candidates, group)
	phrases := partyServicePhrasesWithAliases(pool, aliases)
	matches := make([]partyPlanServiceSelection, 0)
	for _, phrase := range phrases {
		index := indexNormalizedPhrase(normalized, phrase.Phrase)
		if index < 0 {
			continue
		}
		matches = append(matches, partyPlanServiceSelection{
			Start:     index,
			End:       index + len(phrase.Phrase),
			Service:   phrase.Service,
			PhraseLen: len(phrase.Phrase),
		})
	}
	if len(matches) == 0 {
		result := interpretServiceWithCategoryAliases(message, pool, aliases, nil)
		if result.Status == serviceUnderstandingStatusSelected {
			return append([]ServiceOption(nil), result.Candidates...)
		}
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].PhraseLen > matches[j].PhraseLen
		}
		return matches[i].Start < matches[j].Start
	})
	accepted := make([]partyPlanServiceSelection, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		if seen[strings.TrimSpace(match.Service.ID)] {
			continue
		}
		overlap := false
		for _, existing := range accepted {
			if match.Start < existing.End && existing.Start < match.End {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		accepted = append(accepted, match)
		seen[strings.TrimSpace(match.Service.ID)] = true
	}
	out := make([]ServiceOption, 0, len(accepted))
	for _, match := range accepted {
		out = append(out, match.Service)
	}
	return out
}

func partyPlanSelectionPool(services []ServiceOption, candidates []ServiceOption, group PartyPlanGroup) []ServiceOption {
	out := make([]ServiceOption, 0, len(candidates))
	for _, candidate := range candidates {
		out = appendUniqueService(out, candidate)
	}
	for _, service := range services {
		if partyServiceFitsPartyGroup(service, candidates, group) {
			out = appendUniqueService(out, service)
		}
	}
	return orderedServices(out)
}

func partyServiceFitsPartyGroup(service ServiceOption, candidates []ServiceOption, group PartyPlanGroup) bool {
	serviceID := strings.TrimSpace(service.ID)
	if serviceID == "" {
		return false
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ID) == serviceID {
			return true
		}
	}
	serviceCategoryID := strings.TrimSpace(service.CategoryID)
	serviceCategoryName := normalizeServiceText(firstNonEmpty(service.CategoryName, service.CategorySlug))
	groupLabel := normalizeServiceText(group.Label)
	for _, candidate := range candidates {
		if serviceCategoryID != "" && serviceCategoryID == strings.TrimSpace(candidate.CategoryID) {
			return true
		}
		candidateCategory := normalizeServiceText(firstNonEmpty(candidate.CategoryName, candidate.CategorySlug))
		if serviceCategoryName != "" && candidateCategory != "" && serviceCategoryName == candidateCategory {
			return true
		}
	}
	if groupLabel == "" {
		return false
	}
	if serviceCategoryName != "" && (serviceCategoryName == groupLabel || containsNormalizedPhrase(serviceCategoryName, groupLabel)) {
		return true
	}
	serviceName := normalizeServiceText(service.Name)
	return serviceName != "" && containsNormalizedPhrase(serviceName, groupLabel)
}

func partyServicePhrasesWithAliases(services []ServiceOption, aliases []ServiceAlias) []partyServicePhrase {
	phrases := partyServicePhrases(services)
	seen := map[string]bool{}
	servicesByID := map[string]ServiceOption{}
	for _, service := range services {
		servicesByID[strings.TrimSpace(service.ID)] = service
	}
	for _, phrase := range phrases {
		seen[strings.TrimSpace(phrase.Service.ID)+"\x00"+strings.TrimSpace(phrase.Phrase)] = true
	}
	addAliasPhrase := func(service ServiceOption, phrase string) {
		phrase = normalizeServiceText(phrase)
		if phrase == "" {
			return
		}
		key := strings.TrimSpace(service.ID) + "\x00" + phrase
		if seen[key] {
			return
		}
		seen[key] = true
		phrases = append(phrases, partyServicePhrase{Service: service, Phrase: phrase})
	}
	for _, alias := range aliases {
		service, ok := servicesByID[strings.TrimSpace(alias.ServiceID)]
		if !ok {
			continue
		}
		phrase := strings.TrimSpace(alias.NormalizedAlias)
		if phrase == "" {
			phrase = alias.Alias
		}
		addAliasPhrase(service, phrase)
		addAliasPhrase(service, pluralServicePhrase(phrase))
	}
	sortPartyServicePhrases(phrases)
	return phrases
}

func sortPartyServicePhrases(phrases []partyServicePhrase) {
	sort.SliceStable(phrases, func(i, j int) bool {
		if len(phrases[i].Phrase) == len(phrases[j].Phrase) {
			if phrases[i].Family == phrases[j].Family {
				return phrases[i].Phrase < phrases[j].Phrase
			}
			return !phrases[i].Family
		}
		return len(phrases[i].Phrase) > len(phrases[j].Phrase)
	})
}

func partyPlanCanRepeatSingleSelection(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	normalized := normalizeLooseText(message)
	if strings.Contains(lower, "&") {
		return false
	}
	for _, signal := range []string{"and", "plus", "also", "both", "each", "one each"} {
		if containsLoosePhrase(normalized, signal) {
			return false
		}
	}
	return true
}

func partyPlanSegments(plan *PartyPlan, session Session) []booking.BookingSegmentRequest {
	if plan == nil {
		return nil
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	segments := make([]booking.BookingSegmentRequest, 0, plan.PartySize)
	for _, group := range plan.Groups {
		for _, serviceID := range nonEmptyStrings(group.ResolvedServiceIDs) {
			segments = append(segments, booking.BookingSegmentRequest{
				ServiceID:          serviceID,
				StaffID:            staffID,
				StaffSelectionMode: mode,
			})
		}
	}
	return segments
}

func partyPlanClarificationPrompt(session Session, plan *PartyPlan, services []ServiceOption, cfg *RuntimeConfig) string {
	groupIndex := firstUnresolvedPartyPlanGroup(plan)
	if groupIndex < 0 {
		return "Which service would you like for the group appointment?"
	}
	group := plan.Groups[groupIndex]
	candidates := servicesByIDs(services, group.CandidateServiceIDs)
	options := serviceCandidateNames(candidates, 6)
	if len(options) == 0 {
		return "Which service would you like for the group appointment?"
	}
	label := strings.TrimSpace(group.Label)
	if label == "" {
		label = "service"
	}
	remaining := group.Count - len(nonEmptyStrings(group.ResolvedServiceIDs))
	if remaining < 1 {
		remaining = group.Count
	}
	prefix := ""
	if context := appointmentContextPhrase(session, cfg); context != "" {
		prefix = "I have " + context + " noted. "
	}
	countLabel := partyPlanCountLabel(remaining, label)
	switch {
	case len(options) == 1:
		return prefix + "Should I book " + serviceCountSpeech(remaining, options[0]) + "?"
	case remaining > 1 && remaining == len(options):
		return prefix + "Should I book " + oneEachServiceSpeech(options) + " for " + countLabel + "?"
	case remaining > 1:
		return prefix + "Which " + label + " services should I book for " + countLabel + ": " + joinChoiceList(options) + "?"
	default:
		return prefix + "Which " + label + " service should I book: " + joinChoiceList(options) + "?"
	}
}

func partyPlanCountLabel(count int, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "service"
	}
	switch count {
	case 1:
		return "the " + label
	case 2:
		return "the two " + pluralServicePhrase(label)
	case 3:
		return "the three " + pluralServicePhrase(label)
	case 4:
		return "the four " + pluralServicePhrase(label)
	default:
		return fmt.Sprintf("the %d %s", count, pluralServicePhrase(label))
	}
}

func serviceCountSpeech(count int, service string) string {
	service = strings.TrimSpace(service)
	if count <= 1 {
		return "one " + service
	}
	return countWord(count) + " " + pluralDisplayName(service)
}

func oneEachServiceSpeech(options []string) string {
	parts := make([]string, 0, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option != "" {
			parts = append(parts, "one "+option)
		}
	}
	return joinHumanList(parts)
}

func countWord(count int) string {
	switch count {
	case 1:
		return "one"
	case 2:
		return "two"
	case 3:
		return "three"
	case 4:
		return "four"
	case 5:
		return "five"
	case 6:
		return "six"
	case 7:
		return "seven"
	case 8:
		return "eight"
	case 9:
		return "nine"
	default:
		return fmt.Sprintf("%d", count)
	}
}

func pluralDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return name
	}
	last := parts[len(parts)-1]
	lower := strings.ToLower(last)
	switch {
	case strings.HasSuffix(lower, "s"):
		return name
	case strings.HasSuffix(lower, "y"):
		parts[len(parts)-1] = strings.TrimSuffix(last, last[len(last)-1:]) + "ies"
	default:
		parts[len(parts)-1] = last + "s"
	}
	return strings.Join(parts, " ")
}

func applyPartyPlanMetadata(turn *TurnRecord, plan *PartyPlan) {
	if turn == nil || plan == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"party_plan_active":     true,
		"party_plan_complete":   partyPlanComplete(plan),
		"party_plan_party_size": plan.PartySize,
	})
}

func serviceIDsFromOptions(services []ServiceOption) []string {
	out := make([]string, 0, len(services))
	seen := map[string]bool{}
	for _, service := range services {
		id := strings.TrimSpace(service.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func repeatedString(value string, count int) []string {
	if strings.TrimSpace(value) == "" || count <= 0 {
		return nil
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, value)
	}
	return out
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func partyServiceCountSegmentsFromMessage(message string, services []ServiceOption, session Session) []booking.BookingSegmentRequest {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return nil
	}
	phrases := partyServicePhrases(services)
	matches := make([]partyServiceCountMatch, 0)
	for _, phrase := range phrases {
		pattern := regexp.MustCompile(`\b(` + partyCountTokenPattern() + `)\s+` + regexp.QuoteMeta(phrase.Phrase) + `\b`)
		for _, indexes := range pattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(indexes) < 4 {
				continue
			}
			countToken := normalized[indexes[2]:indexes[3]]
			count, ok := partyCountTokenValue(countToken)
			if !ok || count < 1 {
				continue
			}
			matches = append(matches, partyServiceCountMatch{
				Start:     indexes[0],
				End:       indexes[1],
				Count:     count,
				Service:   phrase.Service,
				PhraseLen: len(phrase.Phrase),
			})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Start == matches[j].Start {
			return matches[i].PhraseLen > matches[j].PhraseLen
		}
		return matches[i].Start < matches[j].Start
	})
	accepted := make([]partyServiceCountMatch, 0, len(matches))
	for _, match := range matches {
		if partyCountMatchOverlaps(accepted, match) {
			continue
		}
		accepted = append(accepted, match)
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	segments := make([]booking.BookingSegmentRequest, 0)
	for _, match := range accepted {
		for i := 0; i < match.Count; i++ {
			segments = append(segments, booking.BookingSegmentRequest{
				ServiceID:          strings.TrimSpace(match.Service.ID),
				StaffID:            staffID,
				StaffSelectionMode: mode,
			})
		}
	}
	return segments
}

func partyCountMatchOverlaps(accepted []partyServiceCountMatch, candidate partyServiceCountMatch) bool {
	for _, item := range accepted {
		if candidate.Start < item.End && item.Start < candidate.End {
			return true
		}
	}
	return false
}

func partySegmentsForService(service ServiceOption, count int, session Session) []booking.BookingSegmentRequest {
	serviceID := strings.TrimSpace(service.ID)
	if serviceID == "" || count <= 0 {
		return nil
	}
	mode := staffSelectionModeForServiceRequest(session)
	staffID := strings.TrimSpace(session.StaffID)
	if mode == booking.StaffSelectionAnyone {
		staffID = ""
	}
	segments := make([]booking.BookingSegmentRequest, 0, count)
	for i := 0; i < count; i++ {
		segments = append(segments, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: mode,
		})
	}
	return segments
}

func singlePartyServiceFromMessage(message string, services []ServiceOption) (ServiceOption, bool) {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return ServiceOption{}, false
	}
	phrases := partyServicePhrases(services)
	matched := map[string]ServiceOption{}
	exactMatched := map[string]ServiceOption{}
	for _, phrase := range phrases {
		if !containsNormalizedPhrase(normalized, phrase.Phrase) {
			continue
		}
		serviceID := strings.TrimSpace(phrase.Service.ID)
		if serviceID == "" {
			continue
		}
		matched[serviceID] = phrase.Service
		if !phrase.Family {
			exactMatched[serviceID] = phrase.Service
		}
	}
	if len(exactMatched) == 1 {
		for _, service := range exactMatched {
			return service, true
		}
	}
	if len(matched) == 1 {
		for _, service := range matched {
			return service, true
		}
	}
	return ServiceOption{}, false
}

func partyServicePhrases(services []ServiceOption) []partyServicePhrase {
	familyCounts := map[string]int{}
	for _, service := range services {
		for _, token := range serviceNameTokens(service.Name) {
			familyCounts[singularServiceToken(token)]++
		}
	}
	seen := map[string]bool{}
	phrases := make([]partyServicePhrase, 0)
	addPhrase := func(service ServiceOption, phrase string, family bool) {
		phrase = normalizeServiceText(phrase)
		if phrase == "" {
			return
		}
		key := strings.TrimSpace(service.ID) + "\x00" + phrase
		if seen[key] {
			return
		}
		seen[key] = true
		phrases = append(phrases, partyServicePhrase{Service: service, Phrase: phrase, Family: family})
	}
	for _, service := range services {
		name := normalizeServiceText(service.Name)
		if name == "" || strings.TrimSpace(service.ID) == "" {
			continue
		}
		addPhrase(service, name, false)
		addPhrase(service, pluralServicePhrase(name), false)
		for _, token := range serviceNameTokens(service.Name) {
			singular := singularServiceToken(token)
			if familyCounts[singular] != 1 {
				continue
			}
			addPhrase(service, singular, true)
			addPhrase(service, pluralServicePhrase(singular), true)
		}
	}
	sortPartyServicePhrases(phrases)
	return phrases
}

func pluralServicePhrase(phrase string) string {
	phrase = normalizeServiceText(phrase)
	if phrase == "" {
		return ""
	}
	parts := strings.Fields(phrase)
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	switch {
	case strings.HasSuffix(last, "s"):
		return phrase
	case strings.HasSuffix(last, "y"):
		parts[len(parts)-1] = strings.TrimSuffix(last, "y") + "ies"
	default:
		parts[len(parts)-1] = last + "s"
	}
	return strings.Join(parts, " ")
}

func singularServiceToken(token string) string {
	token = normalizeServiceText(token)
	switch {
	case strings.HasSuffix(token, "ies") && len(token) > 3:
		return strings.TrimSuffix(token, "ies") + "y"
	case strings.HasSuffix(token, "s") && len(token) > 3:
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func partyCountTokenPattern() string {
	return `[2-9]|two|three|four|five|six|seven|eight|nine`
}

func partyCountTokenValue(token string) (int, bool) {
	token = strings.TrimSpace(strings.ToLower(token))
	switch token {
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
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 2 || value > 9 {
		return 0, false
	}
	return value, true
}

func applyPartyBookingPlan(session *Session, plan partyBookingPlan) bool {
	if session == nil || len(plan.Segments) == 0 {
		return false
	}
	before := selectedServiceIDs(*session)
	segments := make([]booking.BookingSegmentRequest, 0, len(plan.Segments))
	for _, segment := range plan.Segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		mode := normalizeConversationStaffSelectionMode(segment.StaffSelectionMode)
		if mode == "" {
			mode = staffSelectionModeForServiceRequest(*session)
		}
		staffID := strings.TrimSpace(segment.StaffID)
		if mode == booking.StaffSelectionAnyone {
			staffID = ""
		}
		segments = append(segments, booking.BookingSegmentRequest{
			ServiceID:          serviceID,
			StaffID:            staffID,
			StaffSelectionMode: mode,
		})
	}
	if len(segments) == 0 {
		return false
	}
	session.ServiceID = segments[0].ServiceID
	session.BookingSegments = segments
	session.StaffSelectionMode = segments[0].StaffSelectionMode
	if session.StaffSelectionMode == booking.StaffSelectionAnyone {
		session.StaffID = ""
		session.StaffName = ""
	}
	session.OfferedSlots = nil
	return !sameStringSlices(before, selectedServiceIDs(*session))
}

func applyPartyBookingMetadata(turn *TurnRecord, session Session) {
	if turn == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"party_booking":        true,
		"party_segment_count":  len(session.BookingSegments),
		"party_service_counts": partyServiceCountsForMetadata(session.BookingSegments),
	})
}

func partyServiceCountsForMetadata(segments []booking.BookingSegmentRequest) map[string]int {
	counts := map[string]int{}
	for _, segment := range segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		counts[serviceID]++
	}
	return counts
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
	return normalizeInitialGreeting(greeting, salonName(cfg))
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

func bookingActionForSession(session Session) string {
	action := strings.TrimSpace(session.BookingAction)
	if action == BookingActionReschedule {
		return BookingActionReschedule
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

func selectRescheduleCandidate(message string, candidates []RescheduleCandidate, loc *time.Location, now func() time.Time) *RescheduleCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 && isAffirmativeOnly(message) {
		return &candidates[0]
	}
	if selected := selectRescheduleCandidateByDateTime(message, candidates, loc, now); selected != nil {
		return selected
	}
	if selected := selectRescheduleCandidateByService(message, candidates); selected != nil {
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

func selectRescheduleCandidateByService(message string, candidates []RescheduleCandidate) *RescheduleCandidate {
	messageFamilies := rescheduleServiceFamilies(message)
	if len(messageFamilies) == 0 {
		return nil
	}
	matches := make([]int, 0, len(candidates))
	for i := range candidates {
		candidateFamilies := rescheduleServiceFamilies(candidates[i].ServiceLabel)
		for family := range messageFamilies {
			if candidateFamilies[family] {
				matches = append(matches, i)
				break
			}
		}
	}
	if len(matches) == 1 {
		return &candidates[matches[0]]
	}
	return nil
}

func rescheduleServiceFamilies(value string) map[string]bool {
	normalized := normalizeServiceText(value)
	families := map[string]bool{}
	for _, token := range strings.Fields(normalized) {
		switch token {
		case "mani", "manis", "manicure", "manicures":
			families["manicure"] = true
		case "pedi", "pedis", "pedicure", "pedicures":
			families["pedicure"] = true
		default:
			if len([]rune(token)) >= 4 {
				families[token] = true
			}
		}
	}
	return families
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

func rescheduleMultipleCandidatesPrompt(candidates []RescheduleCandidate, loc *time.Location) string {
	parts := []string{"I found more than one upcoming appointment. Which one should I reschedule?"}
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
			return "I have " + service + " on " + requestedDateLabel(date, timezoneLocation(timezoneFromConfig(cfg))) + ". What time works?"
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
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	if hasExplicitBookingRequestSignal(normalized) ||
		hasServiceAddSignal(message) ||
		hasServiceCorrectionSignal(message) ||
		hasExplicitServiceReplacementPhrase(message) ||
		looksLikeDateOrTimeInsteadOfName(message) ||
		hasSchedulingAvailabilitySignal(normalized) {
		return false
	}
	if asksServiceMenu(message) {
		return true
	}
	if strings.HasPrefix(normalized, "do you have ") ||
		strings.HasPrefix(normalized, "do you guys have ") ||
		strings.HasPrefix(normalized, "do yall have ") ||
		strings.HasPrefix(normalized, "you have ") ||
		strings.HasPrefix(normalized, "do you offer ") ||
		strings.HasPrefix(normalized, "do you do ") ||
		strings.HasPrefix(normalized, "do you provide ") ||
		strings.HasPrefix(normalized, "is there ") {
		return true
	}
	return result.Status != serviceUnderstandingStatusUnknown &&
		strings.HasPrefix(normalized, "is ") &&
		strings.HasSuffix(normalized, " available")
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

func serviceInquiryReply(session Session, result serviceUnderstandingResult, services []ServiceOption) string {
	switch result.Status {
	case serviceUnderstandingStatusSelected:
		names := serviceCandidateNames(result.Candidates, 3)
		if len(names) == 0 {
			break
		}
		service := joinHumanList(names)
		if current := strings.TrimSpace(serviceSummary(session, services)); current != "" {
			return "Yes, we offer " + service + ". I still have " + current + " noted. Which service would you like to book?"
		}
		return "Yes, we offer " + service + ". Which service would you like to book?"
	case serviceUnderstandingStatusAmbiguous:
		names := serviceCandidateNames(result.Candidates, 5)
		if len(names) > 0 {
			return "We offer " + joinHumanList(names) + ". Which one would you like to book?"
		}
	}
	if names := serviceCandidateNames(services, 8); len(names) > 0 {
		return "I do not see that in the bookable service list. Services include " + joinHumanList(names) + ". Which service would you like to book?"
	}
	return "I do not see that in the bookable service list. Which service would you like?"
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
	return prefix + "Which " + serviceLabel + " would you like: " + joinChoiceList(options) + "?"
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
	candidate := customerNameCandidate(message, session)
	if !isRiskySingleWordVoiceName(candidate) {
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

func setPendingServiceEditMetadata(turn *TurnRecord, candidates []ServiceOption) {
	if turn == nil || len(candidates) == 0 {
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
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"pending_service_edit_candidate_ids": ids,
		"pending_service_edit_candidates":    names,
		"pending_service_edit_mode":          "add_or_switch",
	})
}

func pendingServiceEdit(session Session, services []ServiceOption) ([]ServiceOption, bool) {
	for i := len(session.Transcript) - 1; i >= 0; i-- {
		msg := session.Transcript[i]
		if msg.Speaker != SpeakerAI {
			continue
		}
		if metadataBool(msg.Metadata, "pending_service_edit_cleared") {
			return nil, false
		}
		ids := metadataStringSlice(msg.Metadata, "pending_service_edit_candidate_ids")
		if len(ids) == 0 {
			continue
		}
		items := servicesByIDs(services, ids)
		return items, len(items) > 0
	}
	return nil, false
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
	return time.Time{}, false
}

func parseOClockCandidateForDate(message string, requestedDate string, loc *time.Location) (time.Time, bool) {
	candidates := oClockCandidatesFromText(message)
	if len(candidates) != 1 {
		return time.Time{}, false
	}
	minutes := candidates[0]
	parsedDate, err := time.ParseInLocation("2006-01-02", requestedDate, loc)
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

func activeSlotTimePreference(session Session) (slotTimePreference, bool) {
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
	if bookingActionForSession(session) == BookingActionReschedule {
		parts = append(parts, "reschedule request")
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
	if sessionUsesAnyone(session) {
		parts = append(parts, strings.TrimSpace(availableTechnicianPhraseForSegments(session.BookingSegments)))
	} else if member != "" {
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

func availableTechnicianPhrase(slot OfferedSlot) string {
	return availableTechnicianPhraseForSegmentCount(len(slot.Segments))
}

func availableTechnicianPhraseForSegments(segments []booking.BookingSegmentRequest) string {
	return availableTechnicianPhraseForSegmentCount(len(segments))
}

func availableTechnicianPhraseForSegmentCount(count int) string {
	if count > 1 {
		return " with available technicians assigned"
	}
	return " with an available technician"
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

func joinChoiceList(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
	}
}

func serviceSummary(session Session, services []ServiceOption) string {
	if activePartyPlan(session.PartyPlan) || hasRepeatedBookingService(session.BookingSegments) {
		if summary := selectedServiceCountSummary(session, services); summary != "" {
			return summary
		}
	}
	names := selectedServiceNames(session, services)
	if len(names) > 0 {
		return joinHumanList(names)
	}
	return ""
}

func hasRepeatedBookingService(segments []booking.BookingSegmentRequest) bool {
	seen := map[string]bool{}
	for _, segment := range segments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		if seen[serviceID] {
			return true
		}
		seen[serviceID] = true
	}
	return false
}

func selectedServiceCountSummary(session Session, services []ServiceOption) string {
	type serviceCount struct {
		ServiceID string
		Name      string
		Count     int
	}
	counts := make([]serviceCount, 0)
	indexes := map[string]int{}
	for _, segment := range session.BookingSegments {
		serviceID := strings.TrimSpace(segment.ServiceID)
		if serviceID == "" {
			continue
		}
		index, ok := indexes[serviceID]
		if !ok {
			name := serviceName(serviceID, services, "")
			if name == "" {
				continue
			}
			index = len(counts)
			indexes[serviceID] = index
			counts = append(counts, serviceCount{ServiceID: serviceID, Name: name})
		}
		counts[index].Count++
	}
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, item := range counts {
		if item.Count <= 1 {
			parts = append(parts, "1 "+item.Name)
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", item.Count, pluralDisplayName(item.Name)))
	}
	return joinHumanList(parts)
}

func selectedServiceNames(session Session, services []ServiceOption) []string {
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
	if len(names) == 0 {
		if name := serviceName(session.ServiceID, services, session.ServiceName); name != "" {
			names = append(names, name)
		}
	}
	return names
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
		return defaultSessionListLimit
	}
	if limit > maxSessionListLimit {
		return maxSessionListLimit
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
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

func allowedPartyRequestStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case PartyRequestStatusPending, PartyRequestStatusContacted, PartyRequestStatusResolved, PartyRequestStatusDismissed:
		return true
	default:
		return false
	}
}
