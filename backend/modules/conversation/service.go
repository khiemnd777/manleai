package conversation

import (
	"context"
	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
	"regexp"
	"strings"
	"time"
)

const (
	defaultGreeting           = "Thank you for calling. How can I help today?"
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
	splitAvailabilityLimit    = 5
	splitPartyOptionLimit     = 2
	splitCombinationLimit     = 256

	partySplitDatePolicyRequestedDate = "requested_date"
	partySplitDatePolicyAlternateDate = "alternate_date"
	partySplitDatePolicyMultiDay      = "multi_day"
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
	bareClockWithColonPattern       = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+|for\s+)?(\d{1,2}):([0-5]\d)\b`)
	bareClockWithPrefixPattern      = regexp.MustCompile(`(?i)\b(?:at\s+|around\s+|about\s+)(\d{1,2})(?:$|[^a-z0-9])`)
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
	staffChangeTargetPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:change|switch|move)\s+(?:it\s+|this\s+|me\s+)?(?:to|with)\s+([^,.;?!]+)`),
		regexp.MustCompile(`(?i)\bwith\s+([^,.;?!]+?)\s+instead\b`),
		regexp.MustCompile(`(?i)\binstead\s+(?:with\s+)?([^,.;?!]+)`),
		regexp.MustCompile(`(?i)\bactually\s+(?:with\s+)?([^,.;?!]+)`),
		regexp.MustCompile(`(?i)\bwith\s+([^,.;?!]+)`),
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
	turnInterpreter    TurnInterpreter
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

type slotTimePreference = TimePreference

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
	serviceEditNone                 serviceEditAction = ""
	serviceEditSelectInitial        serviceEditAction = "initial_select"
	serviceEditAdd                  serviceEditAction = "add_service"
	serviceEditReplace              serviceEditAction = "replace_service"
	serviceEditDuplicate            serviceEditAction = "duplicate_service"
	serviceEditClarifyAddSwitch     serviceEditAction = "clarify_add_or_switch"
	serviceEditClarifyAddTarget     serviceEditAction = "clarify_add_target"
	serviceEditClarifyReplaceTarget serviceEditAction = "clarify_replace_target"
	serviceEditClarifyReplaceSource serviceEditAction = "clarify_replace_source"
	serviceEditReplaceSegment       serviceEditAction = "replace_service_segment"
	serviceEditConfirmReplace       serviceEditAction = "confirm_replace_service"
	serviceEditKeepCurrent          serviceEditAction = "keep_current_service"

	pendingServiceEditModeAddOrSwitch            = "add_or_switch"
	pendingServiceEditModeAddSelection           = "add_selection"
	pendingServiceEditModeReplaceSelection       = "replace_selection"
	pendingServiceEditModeReplaceSourceSelection = "replace_source_selection"
	pendingServiceEditModeReplaceConfirmation    = "replace_confirmation"
)

type serviceEditDecision struct {
	Action           serviceEditAction
	Candidates       []ServiceOption
	ReplaceServiceID string
	Source           string
}

type staffChangeRequest struct {
	Intent           bool
	RequestedAnyone  bool
	HasMatchedStaff  bool
	MatchedStaff     StaffOption
	HasNonBookable   bool
	NonBookableStaff StaffOption
	UnknownStaffName string
	Source           string
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

func (s *Service) SetTurnInterpreter(interpreter TurnInterpreter) {
	s.turnInterpreter = interpreter
}

func (s *Service) PrewarmAnswerContext(ctx context.Context, salonID string) error {
	salonID = strings.TrimSpace(salonID)
	if salonID == "" {
		return ErrValidation
	}
	_, err := s.loadAnswerContext(ctx, salonID)
	return err
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
		InitialReply:   initialPhoneReply(cfg),
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
	ctx = withTurnTimingRecorder(ctx, req.TimingRecorder)
	sessionLoadStartedAt := time.Now()
	sessionLoadRecorded := false
	recordSessionLoad := func(result string) {
		if sessionLoadRecorded {
			return
		}
		sessionLoadRecorded = true
		recordTurnTiming(ctx, TurnTimingStageSessionLoad, sessionLoadStartedAt, result)
	}
	session, err := s.store.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		recordSessionLoad(TurnTimingResultError)
		return nil, err
	}
	if eventKey != "" {
		if processed, ok, err := s.store.GetSessionByTurnEventKey(ctx, salonID, ownerUserID, sessionID, eventKey); err != nil {
			recordSessionLoad(TurnTimingResultError)
			return nil, err
		} else if ok {
			recordSessionLoad(TurnTimingResultDeduplicated)
			return processed, nil
		}
	}
	if session.Status != StatusActive {
		recordSessionLoad(TurnTimingResultSessionClosed)
		return nil, ErrSessionClosed
	}
	cfg, err := s.store.GetRuntimeConfig(ctx, salonID, ownerUserID)
	if err != nil {
		recordSessionLoad(TurnTimingResultError)
		return nil, err
	}
	recordSessionLoad(TurnTimingResultOK)
	answerContextStartedAt := time.Now()
	answerCtx, err := s.loadAnswerContext(ctx, salonID)
	recordTurnTiming(ctx, TurnTimingStageAnswerContext, answerContextStartedAt, turnTimingResult(err))
	if err != nil {
		return nil, err
	}
	services := answerCtx.Services
	serviceAliases := answerCtx.ServiceAliases
	categoryAliases := answerCtx.CategoryAliases
	staff := answerCtx.Staff
	activeStaff := answerCtx.ActiveStaff
	knowledge := answerCtx.Knowledge
	routerStartedAt := time.Now()
	turnPlan := s.planTurn(message, *session, answerCtx, cfg)
	recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnRouter, routerStartedAt, turnPlan.Route, turnPlan.timingAttributes())
	newPlannedTurn := func(before Session, after Session) TurnRecord {
		turn := newTurnRecord(salonID, ownerUserID, before, after, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, turnPlan)
		return turn
	}

	if handled, updated, err := s.handlePendingOfferedSlotDateTimeCorrection(ctx, salonID, ownerUserID, *session, message, eventKey, services, staff, cfg, knowledge); handled {
		return updated, err
	}

	if handled, updated, err := s.handlePendingCustomerNameConfirmation(ctx, salonID, ownerUserID, *session, message, eventKey, services, staff, cfg, knowledge); handled {
		return updated, err
	}

	if reply := salonIdentityReplyForMessage(message, *session, cfg); reply != "" {
		turn := newPlannedTurn(*session, *session)
		turn.AIMessage = reply
		missing := ""
		if hasBookingProgress(*session) {
			missing = missingBookingField(*session)
		}
		finalizeTurnMetadata(&turn, *session, *session, missing, missing, "salon_identity_check")
		return s.store.SaveTurn(ctx, turn)
	}

	if rejection, ok := offeredSlotRejectionForMessage(message, *session, timezoneLocation(timezoneFromConfig(cfg))); ok {
		next := cloneSessionForTurn(*session)
		setSlotTimePreference(&next, rejection.Preference)
		next.OfferedSlots = rejection.Remaining
		turn := newPlannedTurn(*session, next)
		applySlotRejectionMetadata(&turn, rejection)
		if len(next.OfferedSlots) > 0 {
			turn.AIMessage = formatSlotOffer(next.OfferedSlots, timezoneLocation(timezoneFromConfig(cfg)), false)
		} else {
			turn.AIMessage = rejectedSlotNoRemainingReply(rejection.Preference.Direction)
		}
		finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "offered_slot_rejection")
		return s.store.SaveTurn(ctx, turn)
	}

	if preference, ok := directionalSlotTimePreferenceForMessage(message, *session); ok {
		next := cloneSessionForTurn(*session)
		setSlotTimePreference(&next, preference)
		next.OfferedSlots = filterOfferedSlotsByPreference(next.OfferedSlots, preference, timezoneLocation(timezoneFromConfig(cfg)))
		turn := newPlannedTurn(*session, next)
		if len(next.OfferedSlots) == 0 {
			preferredDate := preferredDateForAvailability(next, message, timezoneLocation(timezoneFromConfig(cfg)), s.now)
			if preferredDate != "" && next.ServiceID != "" {
				if err := s.offerAvailableSlots(ctx, ownerUserID, &turn, &next, services, staff, preferredDate, false, cfg); err != nil {
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
				}
			}
		}
		applyDirectionalSlotTimePreferenceMetadata(&turn, preference, len(next.OfferedSlots))
		if len(next.OfferedSlots) > 0 {
			turn.AIMessage = formatSlotOfferForSession(next.OfferedSlots, timezoneLocation(timezoneFromConfig(cfg)), false, next, services)
		} else {
			turn.AIMessage = "I don't have an opening in that time range. What other time works?"
		}
		finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "offered_slot_time_preference")
		return s.store.SaveTurn(ctx, turn)
	}

	staffChange := turnPlan.StaffChange
	_, pendingServiceEditMode, pendingServiceEditOK := pendingServiceEdit(*session, services)
	pendingServiceReplaceConfirmation := pendingServiceEditOK && pendingServiceEditMode == pendingServiceEditModeReplaceConfirmation
	if !pendingServiceReplaceConfirmation && !staffChange.Intent && bookingActionForSession(*session) != BookingActionCancel {
		if reply, handoff := customerNameSlotRepairReply(message, *session, services, serviceAliases, categoryAliases, cfg); reply != "" {
			turn := newPlannedTurn(*session, *session)
			if handoff {
				return s.saveHandoffTurn(ctx, turn, *session, HandoffReasonCustomerDetailsUnavailable, reply, services, staff, cfg)
			}
			turn.AIMessage = reply
			s.applyReplyGenerator(ctx, &turn, *session, services, cfg, "customer_name", "customer_name", knowledge)
			finalizeTurnMetadata(&turn, *session, *session, "customer_name", "customer_name", "customer_name_repair")
			return s.store.SaveTurn(ctx, turn)
		}
	}

	if repairReply := repairReplyForMessage(message, *session, cfg); repairReply != "" {
		turn := newPlannedTurn(*session, *session)
		turn.AIMessage = repairReply
		missing := ""
		if session.Intent == IntentBooking || session.ServiceID != "" || session.RequestedDate != "" || session.RequestedStartTime != nil {
			missing = missingBookingField(*session)
		}
		finalizeTurnMetadata(&turn, *session, *session, missing, missing, "deterministic_repair")
		return s.store.SaveTurn(ctx, turn)
	}

	next := cloneSessionForTurn(*session)
	repairInvalidServiceEditPending(&next)
	selectedOfferedSlot := false
	exactRequestedTimeSelected := false
	loc := timezoneLocation(cfg.Timezone)
	if handled, updated, err := s.guardOfferedSlotDateTimeCorrection(ctx, salonID, ownerUserID, *session, message, eventKey, services, serviceAliases, categoryAliases, staff, cfg); handled {
		return updated, err
	}
	pendingNameCandidate := turnPlan.PendingNameCandidate
	serviceUnderstanding := turnPlan.ServiceUnderstanding
	turnUnderstanding := turnPlan.Understanding
	if turnPlan.Route == TurnRouteSemanticLane {
		turnUnderstanding = s.turnUnderstandingForPlan(ctx, *session, message, services, serviceAliases, categoryAliases, staff, turnPlan)
	} else {
		path := turnPlan.Route
		if turnPlan.Reason == "offered_slot_selection" {
			path = TurnTimingPathStateScoped
		}
		recordSkippedTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, path, map[string]string{
			"turn_interpreter_outcome": firstNonEmpty(turnUnderstanding.InterpreterOutcome, "skipped_"+turnPlan.Route),
		})
	}
	conversationAct := primaryConversationAct(turnUnderstanding)
	partySignal := turnPlan.PartySignal
	extractionApplied := false
	if turnIsStandaloneDraftSummary(turnUnderstanding) {
		extracted := cloneSessionForTurn(*session)
		applyExtraction(&extracted, message, services, serviceAliases, categoryAliases, staff, loc, s.now)
		if !draftChanged(*session, extracted) {
			result := s.applyTurnUnderstandingToDraft(&next, turnUnderstanding, services, staff)
			turn := newPlannedTurn(*session, next)
			turn.AIMessage = result.Reply
			applyConversationActMetadata(&turn, conversationAct, result)
			applyTurnUnderstandingMetadata(&turn, turnUnderstanding, result)
			finalizeTurnMetadata(&turn, *session, next, "", "", result.ReplySource)
			return s.store.SaveTurn(ctx, turn)
		}
		next = extracted
		extractionApplied = true
		turnUnderstanding.Acts = nil
		turnUnderstanding.Questions = nil
		turnUnderstanding.Goal = "book_appointment"
		turnUnderstanding.Reason = "deterministic_extraction_overrode_summary"
		conversationAct = primaryConversationAct(turnUnderstanding)
	}
	if len(turnUnderstanding.Acts) == 0 && len(turnUnderstanding.Questions) == 0 && (turnUnderstanding.Goal == "" || turnUnderstanding.Goal == "unknown") {
		if handled, updated, err := s.handleServiceConsultation(ctx, ownerUserID, *session, message, eventKey, serviceUnderstanding, services, staff, cfg); handled {
			return updated, err
		}
	}
	fallback := semanticServiceEditFallback(&next, message, turnUnderstanding, serviceUnderstanding, services)
	if fallback.Act.Kind != "" && fallback.Act.Kind != ConversationActUnknown {
		turnUnderstanding.Goal = "book_appointment"
		turnUnderstanding.Acts = []ConversationAct{fallback.Act}
		turnUnderstanding.Questions = nil
		turnUnderstanding.Reason = fallback.Act.Reason
		conversationAct = fallback.Act
	}
	if fallback.Clarification {
		next.Intent = IntentBooking
		turn := newPlannedTurn(*session, next)
		turn.AIMessage = fallback.Reply
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, fallback)
		if fallback.Escalate {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonServiceClarification, fallback.Reply, services, staff, cfg)
		}
		finalizeTurnMetadata(&turn, *session, next, "service_operation", "service_operation", fallback.ReplySource)
		return s.store.SaveTurn(ctx, turn)
	}
	if shouldClarifyCancelReschedule(*session, message) {
		next.Intent = IntentBooking
		turn := newPlannedTurn(*session, next)
		turn.AIMessage = "Do you want to cancel the existing appointment, or move it to a new time?"
		turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{"booking_action_clarification": "cancel_or_reschedule"})
		finalizeTurnMetadata(&turn, *session, next, "booking_action", "booking_action", "appointment_action_clarification")
		return s.store.SaveTurn(ctx, turn)
	}
	if shouldRouteCancel(*session, message) || turnGoalIs(turnUnderstanding, "cancel_appointment") {
		applyExtraction(&next, message, services, serviceAliases, categoryAliases, staff, loc, s.now)
		return s.handleCancelMessage(ctx, ownerUserID, *session, next, message, eventKey, services, serviceAliases, categoryAliases, staff, cfg, knowledge)
	}
	if activePartyPlan(session.PartyPlan) && !partyPlanComplete(session.PartyPlan) {
		if reply, ok := partyPlanServiceMenuReply(message, *session, services, cfg); ok {
			turn := newPlannedTurn(*session, *session)
			applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
			applyPartyPlanMetadata(&turn, session.PartyPlan)
			turn.AIMessage = reply
			finalizeTurnMetadata(&turn, *session, *session, "service", "service", "party_plan_service_menu")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	if (asksServiceMenu(message) || classifyServiceCatalogQuestion(message, serviceUnderstanding) == serviceCatalogQuestionCount) && serviceUnderstanding.Status == serviceUnderstandingStatusUnknown {
		route := routeNonBookingAnswer(message, *session, answerCtx, cfg, s.now)
		if route.Handled && route.Source != answerSourceBookingRedirect {
			turn := newPlannedTurn(*session, *session)
			turn.AIMessage = route.Reply
			applyAnswerRouteMetadata(&turn, route, answerCtx)
			if route.Intent != "service_catalog_count" {
				s.applyReplyGenerator(ctx, &turn, *session, services, cfg, "", "", knowledge)
			}
			finalizeTurnMetadata(&turn, *session, *session, "", "", "answer_router")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	if !turnHasMutations(turnUnderstanding) && isServiceInquiry(message, serviceUnderstanding) && !asksStaffQuestion(message, staff, activeStaff) {
		turn := newPlannedTurn(*session, *session)
		applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
		applyServiceInquiryMetadata(&turn, serviceUnderstanding)
		route := routeServiceInquiryAnswer(message, *session, serviceUnderstanding, answerCtx)
		turn.AIMessage = route.Reply
		applyAnswerRouteMetadata(&turn, route, answerCtx)
		finalizeTurnMetadata(&turn, *session, *session, "", "", "service_inquiry")
		return s.store.SaveTurn(ctx, turn)
	}
	if !hasBookingProgress(*session) && !hasBookingVerbSignal(message) && !partySignal.IsParty && !serviceUnderstandingStartsBooking(serviceUnderstanding, message) {
		route := routeNonBookingAnswer(message, *session, answerCtx, cfg, s.now)
		if route.Handled && route.Source != answerSourceBookingRedirect {
			turn := newPlannedTurn(*session, *session)
			turn.AIMessage = route.Reply
			applyAnswerRouteMetadata(&turn, route, answerCtx)
			s.applyReplyGenerator(ctx, &turn, *session, services, cfg, "", "", knowledge)
			finalizeTurnMetadata(&turn, *session, *session, "", "", "answer_router")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	if !extractionApplied {
		applyExtraction(&next, message, services, serviceAliases, categoryAliases, staff, loc, s.now)
	}
	if shouldRouteReschedule(*session, message) || turnGoalIs(turnUnderstanding, "reschedule_appointment") {
		return s.handleRescheduleMessage(ctx, ownerUserID, *session, next, message, eventKey, services, serviceAliases, categoryAliases, staff, cfg, knowledge)
	}
	partyPlanApplied := false
	partyPlanTouched := false
	if activePartyPlan(next.PartyPlan) && !partyPlanComplete(next.PartyPlan) {
		partyPlanTouched = true
		plan := clonePartyPlan(next.PartyPlan)
		resolvePartyPlanFromMessage(plan, message, services, serviceAliases, categoryAliases)
		autoResolveSingleCandidatePartyGroups(plan)
		next.PartyPlan = plan
		next.Intent = IntentBooking
		if partyPlanComplete(plan) {
			partyPlanApplied = applyPartyBookingPlan(&next, partyBookingPlan{
				PartySize: plan.PartySize,
				Segments:  partyPlanSegments(plan, next),
			})
		} else {
			turn := newPlannedTurn(*session, next)
			applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
			applyPartyPlanMetadata(&turn, plan)
			turn.AIMessage = partyPlanClarificationReply(message, next, plan, services, cfg)
			finalizeTurnMetadata(&turn, *session, next, "service", "service", "party_plan_clarification")
			return s.store.SaveTurn(ctx, turn)
		}
	} else if partySignal.IsParty {
		if plan, ok := partyPlanFromSignal(partySignal, next); ok {
			partyPlanTouched = true
			next.PartyPlan = plan
			next.Intent = IntentBooking
			if partyPlanComplete(plan) {
				partyPlanApplied = applyPartyBookingPlan(&next, partyBookingPlan{
					PartySize: plan.PartySize,
					Segments:  partyPlanSegments(plan, next),
				})
			} else {
				turn := newPlannedTurn(*session, next)
				applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
				applyPartyPlanMetadata(&turn, plan)
				turn.AIMessage = partyPlanClarificationReply(message, next, plan, services, cfg)
				finalizeTurnMetadata(&turn, *session, next, "service", "service", "party_plan_clarification")
				return s.store.SaveTurn(ctx, turn)
			}
		}
	}
	conversationResult := s.applyTurnUnderstandingToDraft(&next, turnUnderstanding, services, staff)
	applyActiveSlotTimePreferenceToOfferedSlots(&next, loc)
	if conversationResult.Clarification {
		next.Intent = IntentBooking
		turn := newPlannedTurn(*session, next)
		turn.AIMessage = conversationResult.Reply
		applyConversationActMetadata(&turn, conversationAct, conversationResult)
		applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationResult)
		if conversationResult.Escalate {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonServiceClarification, conversationResult.Reply, services, staff, cfg)
		}
		finalizeTurnMetadata(&turn, *session, next, "service", "service", conversationResult.ReplySource)
		return s.store.SaveTurn(ctx, turn)
	}
	serviceEdit := serviceEditDecision{}
	if !conversationResult.Handled && shouldUseDeterministicTurnFallback(turnUnderstanding) {
		// State-scoped fast lanes and safe semantic fallback reuse the catalog-backed
		// service decision without giving the model or parser side-effect ownership.
		serviceEdit = serviceEditDecisionForMessage(*session, message, serviceUnderstanding, services)
	}
	serviceChanged := false
	if !partyPlanTouched {
		if conversationResult.Handled {
			serviceChanged = conversationResult.Changed
		} else {
			serviceChanged = applyServiceEditDecision(&next, serviceEdit)
		}
	}
	serviceChanged = serviceChanged || partyPlanApplied
	if pendingNameCandidate != "" {
		next.CustomerName = ""
	}
	if !serviceChanged && !isServiceEditClarification(serviceEdit.Action) {
		if selectedSplit, ok := selectPartySplitOption(message, next.PartyPlan, loc); ok {
			applySelectedPartySplitOption(&next, selectedSplit.Option, selectedSplit.DateConsentConfirmed)
			selectedOfferedSlot = true
		} else if selected := selectOfferedSlot(message, session.OfferedSlots, loc); selected != nil && offeredSlotMatchesServiceSelection(*selected, next) {
			if offeredSlotAllowedForStaffChange(*selected, staffChange) {
				applySelectedOfferedSlot(&next, *selected)
				selectedOfferedSlot = true
			}
		} else if selected := selectConfirmedOfferedSlot(message, *session, loc); selected != nil && offeredSlotMatchesServiceSelection(*selected, next) {
			if offeredSlotAllowedForStaffChange(*selected, staffChange) {
				applySelectedOfferedSlot(&next, *selected)
				selectedOfferedSlot = true
			}
		}
	}
	if staffChange.Intent && !selectedOfferedSlot {
		next.OfferedSlots = nil
	}
	intent := resolveIntent(session.Intent, message, next, serviceUnderstanding, partySignal)
	intent = intentForTurnGoal(turnUnderstanding, intent)
	next.Intent = intent
	advanceDraftRevision(*session, &next)

	turn := newPlannedTurn(*session, next)
	if len(pendingConsultationServices(*session, services)) > 0 && (serviceChanged || next.Intent != IntentConsultation) {
		clearPendingConsultationMetadata(&turn, "conversation_progressed")
	}
	applyServiceUnderstandingMetadata(&turn, serviceUnderstanding)
	applyServiceEditMetadata(&turn, serviceEdit)
	applyConversationActMetadata(&turn, conversationAct, conversationResult)
	applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationResult)
	applyStaffChangeMetadata(&turn, staffChange)

	if partyPlanApplied {
		applyPartyBookingMetadata(&turn, next)
	}
	if question, ok := firstDeferredInformationQuestion(turnUnderstanding); ok {
		if question.Subject == ConversationQuestionCurrentBooking && strings.TrimSpace(conversationResult.Reply) != "" {
			turn.AIMessage = strings.TrimSpace(conversationResult.Reply)
			if resume := resumeBookingPrompt(next, services, cfg); resume != "" && !strings.Contains(turn.AIMessage, resume) {
				turn.AIMessage += " " + resume
			}
			finalizeTurnMetadata(&turn, *session, next, missingBookingField(next), missingBookingField(next), "turn_current_draft_then_resume")
			return s.store.SaveTurn(ctx, turn)
		}
		var route answerRoute
		switch question.Subject {
		case ConversationQuestionCatalog, ConversationQuestionPrice:
			route = routeServiceInquiryAnswer(message, next, serviceUnderstanding, answerCtx)
		default:
			route = routeNonBookingAnswer(message, next, answerCtx, cfg, s.now)
		}
		if route.Handled && strings.TrimSpace(route.Reply) != "" {
			turn.AIMessage = strings.TrimSpace(route.Reply)
			prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
			if resume := resumeBookingPrompt(next, services, cfg); resume != "" {
				turn.AIMessage = answerWithoutGenericBookingOffer(turn.AIMessage)
				turn.AIMessage += " " + resume
			}
			applyAnswerRouteMetadata(&turn, route, answerCtx)
			finalizeTurnMetadata(&turn, *session, next, missingBookingField(next), missingBookingField(next), "turn_question_then_resume")
			return s.store.SaveTurn(ctx, turn)
		}
	}

	if shouldComplaintHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'm sorry to hear that. I'll send this to the owner so they can help directly. This is not a confirmed appointment.", services, staff, cfg)
	}

	if shouldHandoff(message) || turnGoalIs(turnUnderstanding, "human_handoff") {
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

	state := normalizedDialogState(next.DialogState)
	if state.Pending != nil && isPartyServiceCorrectionPrompt(state.Pending.PromptKey) {
		turn.AIMessage = pendingConversationPrompt(next, services, state, false)
		finalizeTurnMetadata(&turn, *session, next, "party_service_correction", "party_service_correction", "party_service_correction_resume")
		return s.store.SaveTurn(ctx, turn)
	}

	if serviceEdit.Action == serviceEditConfirmReplace && !partyPlanApplied {
		turn.AIMessage = serviceChangeConfirmationPrompt(*session, serviceEdit.Candidates, services, cfg)
		setPendingServiceEditMetadata(&turn, serviceEdit.Candidates, pendingServiceEditModeReplaceConfirmation)
		finalizeTurnMetadata(&turn, *session, next, "service", "service", "service_change_confirmation")
		return s.store.SaveTurn(ctx, turn)
	}

	if serviceEdit.Action == serviceEditClarifyAddSwitch && !partyPlanApplied {
		turn.AIMessage = serviceEditClarificationPrompt(*session, serviceEdit.Candidates, services)
		setPendingServiceEditMetadata(&turn, serviceEdit.Candidates, pendingServiceEditModeAddOrSwitch)
		finalizeTurnMetadata(&turn, *session, next, "service", "service", "service_edit_clarification")
		return s.store.SaveTurn(ctx, turn)
	}

	if serviceEdit.Action == serviceEditClarifyAddTarget && !partyPlanApplied {
		turn.AIMessage = serviceEditTargetPrompt(*session, serviceEdit.Candidates, services, true)
		setPendingServiceEditMetadata(&turn, serviceEdit.Candidates, pendingServiceEditModeAddSelection)
		finalizeTurnMetadata(&turn, *session, next, "service", "service", "service_add_target_clarification")
		return s.store.SaveTurn(ctx, turn)
	}

	if serviceEdit.Action == serviceEditClarifyReplaceTarget && !partyPlanApplied {
		turn.AIMessage = serviceEditTargetPrompt(*session, serviceEdit.Candidates, services, false)
		setPendingServiceEditMetadata(&turn, serviceEdit.Candidates, pendingServiceEditModeReplaceSelection)
		finalizeTurnMetadata(&turn, *session, next, "service", "service", "service_replace_target_clarification")
		return s.store.SaveTurn(ctx, turn)
	}

	if serviceEdit.Action == serviceEditClarifyReplaceSource && !partyPlanApplied {
		turn.AIMessage = serviceEditReplaceSourcePrompt(*session, services)
		setPendingServiceEditMetadata(&turn, serviceEdit.Candidates, pendingServiceEditModeReplaceSourceSelection)
		finalizeTurnMetadata(&turn, *session, next, "service", "service", "service_replace_source_clarification")
		return s.store.SaveTurn(ctx, turn)
	}

	if staffChange.Intent && staffChange.UnknownStaffName != "" {
		turn.AIMessage = unknownStaffChangeReply(staffChange.UnknownStaffName, staff)
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "staff", "staff", knowledge)
		finalizeTurnMetadata(&turn, *session, next, "staff", "staff", "staff_change_unknown")
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
			prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "availability_alternative")
			return s.store.SaveTurn(ctx, turn)
		}
		exactRequestedTimeSelected = true
		prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
	}

	nextAction := planNextConversationAction(next, missingBookingField(next))
	if nextAction.Kind == AssistantActionAskMissingField {
		missing := nextAction.MissingField
		if missing == "requested_time" || missing == "requested_start_time" {
			if len(next.OfferedSlots) > 0 {
				turn.AIMessage = formatSlotOfferForSession(next.OfferedSlots, loc, false, next, services)
				prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
				if serviceEdit.Action == serviceEditKeepCurrent {
					if prefix := serviceKeepCurrentAcknowledgement(next, services); prefix != "" {
						turn.AIMessage = prefix + " " + turn.AIMessage
					}
				}
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
				prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
				s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
				finalizeTurnMetadata(&turn, *session, next, missing, missing, "availability_offer")
				return s.store.SaveTurn(ctx, turn)
			}
		}
		if missing == "party_split_date_consent" {
			turn.AIMessage = partySplitDateConsentPrompt(next, services, cfg)
			turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{
				"availability_policy": "party_split_date_consent_required",
			})
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
			finalizeTurnMetadata(&turn, *session, next, missing, missing, "party_split_date_consent")
			return s.store.SaveTurn(ctx, turn)
		}
		turn.AIMessage = promptForMissingField(missing)
		if contextual := promptForMissingFieldWithServiceContext(missing, next, services, cfg); contextual != "" && !conversationResult.Changed {
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
		prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
		if serviceEdit.Action == serviceEditKeepCurrent {
			if prefix := serviceKeepCurrentAcknowledgement(next, services); prefix != "" {
				turn.AIMessage = prefix + " " + turn.AIMessage
			}
		}
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, missing, missing, knowledge)
		finalizeTurnMetadata(&turn, *session, next, missing, missing, "missing_field")
		return s.store.SaveTurn(ctx, turn)
	}

	return s.continueAfterDraftReady(ctx, ownerUserID, turn, *session, next, services, staff, cfg, knowledge)
}

func shouldRouteReschedule(session Session, message string) bool {
	return bookingActionForSession(session) == BookingActionReschedule || hasRescheduleSignal(message)
}

func shouldRouteCancel(session Session, message string) bool {
	if bookingActionForSession(session) == BookingActionCancel {
		return true
	}
	return hasCancelSignal(message) &&
		!hasCancelNegation(message) &&
		!looksLikeCurrentBookingDraftCancel(message, session) &&
		!hasRescheduleSignal(message)
}

func shouldClarifyCancelReschedule(session Session, message string) bool {
	return bookingActionForSession(session) == BookingActionBook &&
		hasCancelSignal(message) &&
		hasRescheduleSignal(message) &&
		!hasCancelNegation(message)
}

func (s *Service) handleRescheduleMessage(ctx context.Context, ownerUserID string, before Session, next Session, message string, eventKey string, services []ServiceOption, serviceAliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
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
		if selected := selectRescheduleCandidate(message, next.RescheduleCandidates, services, serviceAliases, categoryAliases, loc, s.now); selected != nil {
			applyRescheduleCandidate(&next, *selected)
			syncTurnUpdate(&turn, next, services, staff, cfg)
		} else {
			if len(next.RescheduleCandidates) == 0 {
				startedAt := time.Now()
				candidates, err := s.bookingTool.RescheduleCandidates(ctx, before.SalonID, ownerUserID, booking.RescheduleLookupRequest{
					CustomerName:  next.CustomerName,
					CustomerPhone: next.CustomerPhone,
					Limit:         3,
				})
				recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
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

func (s *Service) handleCancelMessage(ctx context.Context, ownerUserID string, before Session, next Session, message string, eventKey string, services []ServiceOption, serviceAliases []ServiceAlias, categoryAliases []ServiceCategoryAlias, staff []StaffOption, cfg *RuntimeConfig, knowledge []KnowledgeSnippet) (*Session, error) {
	loc := timezoneLocation(timezoneFromConfig(cfg))
	firstCancelTurn := bookingActionForSession(before) != BookingActionCancel && hasCancelSignal(message)
	next.BookingAction = BookingActionCancel
	next.Intent = IntentBooking
	if firstCancelTurn {
		clearCancelSelection(&next)
	}
	turn := newTurnRecord(before.SalonID, ownerUserID, before, next, message, eventKey, services, staff, cfg)
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{"booking_action": BookingActionCancel})

	if shouldComplaintHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'm sorry to hear that. I'll send this cancellation request to the owner so they can help directly. The appointment is not cancelled yet.", services, staff, cfg)
	}
	if shouldHandoff(message) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll pass this cancellation request to the owner so they can help directly. The appointment is not cancelled yet.", services, staff, cfg)
	}
	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. I can send this cancellation request to the owner, but the appointment is not cancelled yet.", services, staff, cfg)
	}
	if s.bookingTool == nil {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
	}
	if strings.TrimSpace(next.CustomerPhone) == "" {
		turn.AIMessage = "What phone number is on the appointment you want to cancel?"
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_phone", "customer_phone", knowledge)
		finalizeTurnMetadata(&turn, before, next, "customer_phone", "customer_phone", "cancel_missing_phone")
		return s.store.SaveTurn(ctx, turn)
	}

	if strings.TrimSpace(next.TargetAppointmentID) == "" {
		if selected := selectRescheduleCandidate(message, next.RescheduleCandidates, services, serviceAliases, categoryAliases, loc, s.now); selected != nil {
			applyCancelCandidate(&next, *selected, loc)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = cancelSingleCandidatePrompt(*selected, loc)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_target_confirmation")
			return s.store.SaveTurn(ctx, turn)
		}
		if len(next.RescheduleCandidates) == 0 {
			startedAt := time.Now()
			candidates, err := s.bookingTool.RescheduleCandidates(ctx, before.SalonID, ownerUserID, booking.RescheduleLookupRequest{
				CustomerName:  next.CustomerName,
				CustomerPhone: next.CustomerPhone,
				Limit:         3,
			})
			recordTurnTiming(ctx, TurnTimingStageAvailabilityPOS, startedAt, turnTimingResult(err))
			if err != nil {
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not look up the appointment safely, so I will send this cancellation request to the owner. The appointment is not cancelled yet.", services, staff, cfg)
			}
			next.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
			syncTurnUpdate(&turn, next, services, staff, cfg)
		}
		if selected := selectRescheduleCandidate(message, next.RescheduleCandidates, services, serviceAliases, categoryAliases, loc, s.now); selected != nil {
			applyCancelCandidate(&next, *selected, loc)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = cancelSingleCandidatePrompt(*selected, loc)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_target_confirmation")
			return s.store.SaveTurn(ctx, turn)
		}
		switch len(next.RescheduleCandidates) {
		case 0:
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not find an upcoming appointment for this phone number, so I will send this cancellation request to the owner. The appointment is not cancelled yet.", services, staff, cfg)
		case 1:
			applyCancelCandidate(&next, next.RescheduleCandidates[0], loc)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = cancelSingleCandidatePrompt(next.RescheduleCandidates[0], loc)
		default:
			if isCancelTargetFiller(message) && len(before.RescheduleCandidates) > 0 {
				turn.AIMessage = cancelConciseTargetPrompt(next.RescheduleCandidates, loc)
			} else {
				turn.AIMessage = cancelMultipleCandidatesPrompt(next.RescheduleCandidates, loc)
			}
		}
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
		finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_target_lookup")
		return s.store.SaveTurn(ctx, turn)
	}

	if isNegativeOnly(message) || hasCancelNegation(message) {
		clearCancelSelection(&next)
		syncTurnUpdate(&turn, next, services, staff, cfg)
		if len(next.RescheduleCandidates) > 1 {
			turn.AIMessage = cancelConciseTargetPrompt(next.RescheduleCandidates, loc)
		} else {
			turn.AIMessage = "Okay, I will not cancel that appointment. Which appointment did you want to cancel?"
		}
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
		finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_target_rejected")
		return s.store.SaveTurn(ctx, turn)
	}
	if !cancelTargetConfirmed(message) {
		if selected := selectedRescheduleCandidate(next); selected != nil {
			turn.AIMessage = cancelSingleCandidatePrompt(*selected, loc)
		} else {
			turn.AIMessage = "Please confirm which appointment you want to cancel."
		}
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
		finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_target_confirmation")
		return s.store.SaveTurn(ctx, turn)
	}

	return s.tryCancel(ctx, ownerUserID, turn, next, services, staff, cfg)
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

func (s *Service) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int, offset int) (*ListWebhookEventsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	if salonID == "" || ownerUserID == "" || sessionID == "" {
		return nil, ErrValidation
	}
	pageLimit := clampWebhookLimit(limit)
	pageOffset := clampOffset(offset)
	items, err := s.store.ListWebhookEvents(ctx, salonID, ownerUserID, sessionID, pageLimit+1, pageOffset)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > pageLimit
	if hasMore {
		items = items[1:]
	}
	return &ListWebhookEventsResponse{Events: items, Limit: pageLimit, Offset: pageOffset, HasMore: hasMore}, nil
}

func (s *Service) ListPartyBookingRequests(ctx context.Context, salonID string, ownerUserID string, status string, limit int, offset int) (*ListPartyBookingRequestsResponse, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	status = strings.TrimSpace(status)
	if salonID == "" || ownerUserID == "" {
		return nil, ErrValidation
	}
	if status != "" && status != "all" && !allowedPartyRequestStatus(status) {
		return nil, ErrValidation
	}
	pageLimit := clampLimit(limit)
	pageOffset := clampOffset(offset)
	items, err := s.store.ListPartyBookingRequests(ctx, salonID, ownerUserID, status, pageLimit+1, pageOffset)
	if err != nil {
		return nil, err
	}
	hasMore := len(items) > pageLimit
	if hasMore {
		items = items[:pageLimit]
	}
	return &ListPartyBookingRequestsResponse{
		PartyBookingRequests: items,
		Limit:                pageLimit,
		Offset:               pageOffset,
		HasMore:              hasMore,
	}, nil
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
