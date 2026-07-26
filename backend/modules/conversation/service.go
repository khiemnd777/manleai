package conversation

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/validation"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
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
	maxStateConflictRetries   = 2

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
	schedulingTool     NeutralSchedulingTool
	replyGenerator     ReplyGenerator
	turnInterpreter    TurnInterpreter
	answerContextCache *answerContextCache
	now                func() time.Time
	customerSMS        CustomerSMSConsentTool
}

func (s *Service) SetCustomerSMSConsentTool(tool CustomerSMSConsentTool) {
	if s != nil {
		s.customerSMS = tool
	}
}

type sessionTurnSerializer interface {
	WithSessionTurnSerialization(
		ctx context.Context,
		salonID string,
		ownerUserID string,
		sessionID string,
		operation func(context.Context) (*Session, error),
	) (*Session, error)
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
	service := &Service{
		store:              store,
		bookingTool:        bookingTool,
		answerContextCache: newAnswerContextCache(defaultAnswerContextTTL),
		now:                func() time.Time { return time.Now().UTC() },
	}
	if tool, ok := bookingTool.(NeutralSchedulingTool); ok {
		service.schedulingTool = tool
	}
	return service
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
	req.Message = strings.TrimSpace(req.Message)
	if salonID == "" || ownerUserID == "" || sessionID == "" || req.Message == "" {
		return nil, ErrValidation
	}
	return s.withSessionTurnSerialization(ctx, salonID, ownerUserID, sessionID, func(serializedCtx context.Context) (*Session, error) {
		return retrySessionStateConflict(serializedCtx, func() (*Session, error) {
			return s.messageOnce(serializedCtx, salonID, ownerUserID, sessionID, req)
		})
	})
}

func (s *Service) withSessionTurnSerialization(
	ctx context.Context,
	salonID string,
	ownerUserID string,
	sessionID string,
	operation func(context.Context) (*Session, error),
) (*Session, error) {
	serializer, ok := s.store.(sessionTurnSerializer)
	if !ok {
		return operation(ctx)
	}
	return serializer.WithSessionTurnSerialization(ctx, salonID, ownerUserID, sessionID, operation)
}

func retrySessionStateConflict(ctx context.Context, operation func() (*Session, error)) (*Session, error) {
	var conflictErr error
	for attempt := 0; attempt <= maxStateConflictRetries; attempt++ {
		session, err := operation()
		if !errors.Is(err, ErrSessionStateConflict) {
			return session, err
		}
		conflictErr = err
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
	}
	return nil, conflictErr
}

func (s *Service) messageOnce(ctx context.Context, salonID string, ownerUserID string, sessionID string, req MessageRequest) (*Session, error) {
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
	if handled, updated, err := s.handlePendingCustomerSMSConsent(ctx, ownerUserID, *session, message, eventKey, services, staff, cfg, knowledge); handled {
		return updated, err
	}
	if isConsultationSafetyConcern(message) {
		return s.saveConsultationSafetyHandoff(ctx, ownerUserID, *session, message, eventKey, services, staff, cfg, "deterministic", SafetyAssessment{
			Concern: true, Category: deterministicSafetyCategory(message), Confidence: 1, Reason: "deterministic_health_suitability_signal",
		})
	}
	routerStartedAt := time.Now()
	turnPlan := s.planTurn(message, *session, answerCtx, cfg)
	recordTurnTimingWithAttributes(ctx, TurnTimingStageTurnRouter, routerStartedAt, turnPlan.Route, turnPlan.timingAttributes())
	turnUnderstanding := turnPlan.Understanding
	if turnPlan.Route == TurnRouteSemanticLane {
		turnUnderstanding = s.turnUnderstandingForPlan(ctx, *session, message, services, serviceAliases, categoryAliases, staff, turnPlan)
		if turnUnderstanding.Safety.Concern {
			return s.saveConsultationSafetyHandoff(ctx, ownerUserID, *session, message, eventKey, services, staff, cfg, "structured_ai", turnUnderstanding.Safety)
		}
		if !customerActsAreSafe(turnUnderstanding, cfg, services, staff) {
			turnUnderstanding = TurnUnderstanding{
				Goal: turnUnderstanding.Goal, ModelInvoked: true, Source: "structured_ai",
				InterpreterOutcome: TurnInterpreterOutcomeCatalogRejected, Reason: "customer_field_validation_rejected",
			}
		}
	}
	pendingNameCandidate := turnPlan.PendingNameCandidate
	newPlannedTurn := func(before Session, after Session) TurnRecord {
		turn := newTurnRecord(salonID, ownerUserID, before, after, message, eventKey, services, staff, cfg)
		applyTurnPlanMetadata(&turn, turnPlan)
		return turn
	}
	storeManualTargetPhoneNameConfirmation := func(next Session) (bool, *Session, error) {
		if pendingNameCandidate == "" || !sessionHasManualAppointmentTarget(*session) || !sessionExpectsCustomerName(*session) {
			return false, nil, nil
		}
		next.CustomerName = ""
		setPendingCustomerName(&next, pendingNameCandidate)
		turn := newPlannedTurn(*session, next)
		turn.AIMessage = customerNameConfirmationPrompt(pendingNameCandidate)
		setPendingCustomerNameMetadata(&turn, pendingNameCandidate, "voice_short_bare_name")
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, *session, next, "customer_name", "customer_name", "customer_name_confirmation")
		updated, saveErr := s.store.SaveTurn(ctx, turn)
		return true, updated, saveErr
	}
	if handled, updated, err := s.handlePendingFuzzyServiceConfirmation(ctx, salonID, ownerUserID, *session, message, eventKey, turnPlan.ServiceUnderstanding, services, serviceAliases, categoryAliases, staff, cfg, knowledge); handled {
		return updated, err
	}

	if handled, updated, err := s.handlePendingOfferedSlotDateTimeCorrection(ctx, salonID, ownerUserID, *session, message, eventKey, services, staff, cfg, knowledge); handled {
		return updated, err
	}

	if handled, updated, err := s.handlePendingCustomerNameConfirmation(ctx, salonID, ownerUserID, *session, message, eventKey, turnUnderstanding, services, staff, cfg, knowledge); handled {
		return updated, err
	}
	if !consultationStateActive(normalizedDialogState(session.DialogState).Consultation) &&
		!turnGoalIs(turnUnderstanding, "consultation") &&
		strings.TrimSpace(turnPlan.PartySignal.ClarifyReason) == "" {
		if handled, updated, err := s.startFuzzyServiceConfirmation(ctx, salonID, ownerUserID, *session, message, eventKey, turnPlan.ServiceUnderstanding, turnUnderstanding, turnPlan.PartySignal, services, serviceAliases, categoryAliases, staff, cfg); handled {
			return updated, err
		}
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

	if handled, updated, err := s.handleGuidanceRecovery(ctx, ownerUserID, *session, message, eventKey, turnPlan, turnUnderstanding, turnPlan.ServiceUnderstanding, answerCtx, services, staff, cfg); handled {
		return updated, err
	}

	next := cloneSessionForTurn(*session)
	repairInvalidServiceEditPending(&next)
	selectedOfferedSlot := false
	exactRequestedTimeSelected := false
	loc := timezoneLocation(cfg.Timezone)
	if handled, updated, err := s.guardOfferedSlotDateTimeCorrection(ctx, salonID, ownerUserID, *session, message, eventKey, services, serviceAliases, categoryAliases, staff, cfg); handled {
		return updated, err
	}
	serviceUnderstanding := turnPlan.ServiceUnderstanding
	if turnPlan.Route != TurnRouteSemanticLane {
		path := turnPlan.Route
		if turnPlan.Reason == "offered_slot_selection" {
			path = TurnTimingPathStateScoped
		}
		recordSkippedTurnTimingWithAttributes(ctx, TurnTimingStageTurnInterpreter, path, map[string]string{
			"turn_interpreter_outcome": firstNonEmpty(turnUnderstanding.InterpreterOutcome, "skipped_"+turnPlan.Route),
		})
	}
	if turnUnderstanding.Safety.Concern {
		return s.saveConsultationSafetyHandoff(ctx, ownerUserID, *session, message, eventKey, services, staff, cfg, "structured_ai", turnUnderstanding.Safety)
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
	if consultationStateActive(normalizedDialogState(session.DialogState).Consultation) || turnGoalIs(turnUnderstanding, "consultation") ||
		(len(turnUnderstanding.Acts) == 0 && len(turnUnderstanding.Questions) == 0 && (turnUnderstanding.Goal == "" || turnUnderstanding.Goal == "unknown")) {
		if handled, updated, err := s.handleServiceConsultation(ctx, ownerUserID, *session, message, eventKey, serviceUnderstanding, turnUnderstanding, services, staff, cfg); handled {
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
		if handled, updated, saveErr := storeManualTargetPhoneNameConfirmation(next); handled {
			return updated, saveErr
		}
		closeConsultationForWorkflow(&next, "cancel_requested", false)
		return s.handleCancelMessage(ctx, ownerUserID, *session, next, message, eventKey, services, serviceAliases, categoryAliases, staff, cfg, knowledge)
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
	applyManualAppointmentTargetServiceSelection(&next, serviceUnderstanding)
	if handled, updated, saveErr := storeManualTargetPhoneNameConfirmation(next); handled {
		return updated, saveErr
	}
	if shouldRouteReschedule(*session, message) || turnGoalIs(turnUnderstanding, "reschedule_appointment") {
		closeConsultationForWorkflow(&next, "reschedule_requested", false)
		return s.handleRescheduleMessage(ctx, ownerUserID, *session, next, message, eventKey, services, serviceAliases, categoryAliases, staff, cfg, knowledge)
	}
	if len(turnUnderstanding.Acts) == 0 {
		if question, ok := firstDeferredInformationQuestion(turnUnderstanding); ok && question.Subject != ConversationQuestionCurrentBooking {
			route := routeStructuredQuestionAnswer(message, question, next, serviceUnderstanding, answerCtx, cfg, s.now)
			if route.Handled && route.Source != answerSourceBookingRedirect && strings.TrimSpace(route.Reply) != "" {
				turn := newPlannedTurn(*session, next)
				turn.AIMessage = strings.TrimSpace(route.Reply)
				resume, expectedInput, reviewStateChanged := resumeAfterInformationPrompt(&next, services, staff, cfg)
				if resume != "" {
					turn.AIMessage = answerWithoutGenericBookingOffer(turn.AIMessage) + " " + resume
				}
				if reviewStateChanged {
					syncTurnUpdate(&turn, next, services, staff, cfg)
				}
				applyAnswerRouteMetadata(&turn, route, answerCtx)
				applyTurnUnderstandingMetadata(&turn, turnUnderstanding, conversationDraftResult{})
				finalizeTurnMetadata(&turn, *session, next, expectedInput, expectedInput, "turn_question_then_resume")
				return s.store.SaveTurn(ctx, turn)
			}
		}
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
	if !conversationResult.Handled && turnUnderstanding.GuidanceAction == GuidanceActionNameService &&
		!hasSelectedServiceDraft(*session) && serviceUnderstanding.Status == serviceUnderstandingStatusSelected && len(serviceUnderstanding.Candidates) == 1 {
		serviceEdit = serviceEditDecision{Action: serviceEditSelectInitial, Candidates: append([]ServiceOption(nil), serviceUnderstanding.Candidates...), Source: "semantic_guidance_catalog_selection"}
	} else if !conversationResult.Handled && shouldUseDeterministicTurnFallback(turnUnderstanding) {
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
	if serviceChanged {
		clearGuidanceRecoveryState(&next, DialogPhaseDrafting)
	}
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
	invalidateCarriedAvailabilityProof(*session, &next)
	intent := resolveIntent(session.Intent, message, next, serviceUnderstanding, partySignal)
	intent = intentForTurnGoal(turnUnderstanding, intent)
	next.Intent = intent
	if consultationStateActive(normalizedDialogState(session.DialogState).Consultation) && intent == IntentBooking {
		closeConsultationForWorkflow(&next, "caller_requested_booking", false)
	}
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
			resume, expectedInput, reviewStateChanged := resumeAfterInformationPrompt(&next, services, staff, cfg)
			if resume != "" && !strings.Contains(turn.AIMessage, resume) {
				turn.AIMessage += " " + resume
			}
			if reviewStateChanged {
				syncTurnUpdate(&turn, next, services, staff, cfg)
			}
			finalizeTurnMetadata(&turn, *session, next, expectedInput, expectedInput, "turn_current_draft_then_resume")
			return s.store.SaveTurn(ctx, turn)
		}
		route := routeStructuredQuestionAnswer(message, question, next, serviceUnderstanding, answerCtx, cfg, s.now)
		if route.Handled && strings.TrimSpace(route.Reply) != "" {
			turn.AIMessage = strings.TrimSpace(route.Reply)
			prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
			resume, expectedInput, reviewStateChanged := resumeAfterInformationPrompt(&next, services, staff, cfg)
			if resume != "" {
				turn.AIMessage = answerWithoutGenericBookingOffer(turn.AIMessage)
				turn.AIMessage += " " + resume
			}
			if reviewStateChanged {
				syncTurnUpdate(&turn, next, services, staff, cfg)
			}
			applyAnswerRouteMetadata(&turn, route, answerCtx)
			finalizeTurnMetadata(&turn, *session, next, expectedInput, expectedInput, "turn_question_then_resume")
			return s.store.SaveTurn(ctx, turn)
		}
	}

	if turnGoalIs(turnUnderstanding, "human_handoff") {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonHumanRequested, "I'll pass this to the owner so they can help directly. This is not a confirmed appointment.", services, staff, cfg)
	}

	if pendingNameCandidate != "" && intent == IntentBooking {
		setPendingCustomerName(&next, pendingNameCandidate)
		syncTurnUpdate(&turn, next, services, staff, cfg)
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
	if configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this request, and no appointment is confirmed.", services, staff, cfg)
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
		available, requestOnly, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check appointment availability, so this is not confirmed. The owner needs to review it.", services, staff, cfg)
		}
		if !available {
			prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, *session, next, "requested_time", "requested_time", "availability_alternative")
			return s.store.SaveTurn(ctx, turn)
		}
		exactRequestedTimeSelected = !requestOnly
		prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
	}

	nextAction := planNextConversationAction(next, missingBookingField(next), cfg)
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
		if hasOperationalBookingProgress(*session) {
			prependConversationMutationAcknowledgement(&turn, conversationResult, next, services)
		}
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
	invalidateCarriedAvailabilityProof(before, &next)
	turn := newTurnRecord(before.SalonID, ownerUserID, before, next, message, eventKey, services, staff, cfg)
	turn.ToolMetadata = mergeMetadata(turn.ToolMetadata, map[string]any{"booking_action": BookingActionReschedule})

	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. The owner needs to review this reschedule request, and the appointment is not rescheduled yet.", services, staff, cfg)
	}
	if configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this reschedule request, and the appointment has not changed.", services, staff, cfg)
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

	if strings.TrimSpace(next.TargetAppointmentID) == "" && manualAppointmentTargetPending(before) {
		description := normalizeManualAppointmentTarget(message)
		if description == "" {
			turn.AIMessage = manualAppointmentTargetPrompt(BookingActionReschedule)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "reschedule_manual_target_repeated")
			return s.store.SaveTurn(ctx, turn)
		}
		applyManualAppointmentTarget(&next, description)
		clearSchedulingFieldsCapturedFromManualTarget(&next)
		syncTurnUpdate(&turn, next, services, staff, cfg)
	}
	if internalLifecyclePending(before, PendingInternalRescheduleConfirmation) {
		// The pending act is the authorization boundary. General caller intent
		// parsing cannot execute or alter the reviewed whole-root replacement.
		next = cloneSessionForTurn(before)
		if isNegativeOnly(message) || hasCancelNegation(message) {
			clearInternalLifecyclePending(&next)
			if next.PartyPlan != nil {
				next.PartyPlan.SelectedSplitOptionID = ""
			}
			next.RequestedStartTime = nil
			clearSelectedAvailabilityQuote(&next)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = "Okay, I did not change the appointment. Which complete replacement option would you prefer?"
			finalizeTurnMetadata(&turn, before, next, ExpectedInputOfferedSlot, ExpectedInputOfferedSlot, "internal_reschedule_confirmation_rejected")
			return s.store.SaveTurn(ctx, turn)
		}
		if !isAffirmativeOnly(message) {
			turn.AIMessage = internalLifecycleConfirmationPrompt(next, services, staff, cfg)
			finalizeTurnMetadata(&turn, before, next, ExpectedInputLifecycleConfirmation, ExpectedInputLifecycleConfirmation, "internal_reschedule_confirmation_repeated")
			return s.store.SaveTurn(ctx, turn)
		}
		return s.tryReschedule(ctx, ownerUserID, turn, next, services, staff, cfg)
	}

	if strings.TrimSpace(next.TargetAppointmentID) == "" && !sessionHasSchedulingTarget(next) {
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
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not look up the appointment safely. The owner needs to review this reschedule request, and the appointment is not rescheduled yet.", services, staff, cfg)
				}
				next.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
				syncTurnUpdate(&turn, next, services, staff, cfg)
			}
			switch len(next.RescheduleCandidates) {
			case 0:
				manualTargetAllowed, policyErr := s.conversationAllowsManualTarget(ctx, before.SalonID, ownerUserID, cfg)
				if policyErr != nil || !manualTargetAllowed {
					return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not find an upcoming appointment for this phone number, so the owner needs to review the reschedule request. The appointment is not rescheduled yet.", services, staff, cfg)
				}
				setManualAppointmentTargetPending(&next)
				syncTurnUpdate(&turn, next, services, staff, cfg)
				turn.AIMessage = manualAppointmentTargetPrompt(BookingActionReschedule)
				s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
				finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "reschedule_manual_target_prompt")
				return s.store.SaveTurn(ctx, turn)
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

	if strings.TrimSpace(next.TargetAppointmentID) != "" && !rescheduleTargetAutoSafe(next) {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "This reschedule needs owner review because I cannot safely update that appointment automatically. The appointment is not rescheduled yet.", services, staff, cfg)
	}
	if _, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle {
		option, selected := selectedPartySplitOption(next.PartyPlan)
		if !selected && before.PartyPlan != nil {
			if choice, ok := selectPartySplitOption(message, before.PartyPlan, loc); ok {
				option, selected = choice.Option, true
			}
		}
		if selected {
			if !prepareSelectedInternalLifecycleOption(&next, option) {
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not verify the complete replacement, so I did not change the appointment. The owner needs to review it.", services, staff, cfg)
			}
			setInternalLifecyclePending(&next, PendingInternalRescheduleConfirmation, option.ID)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = internalLifecycleConfirmationPrompt(next, services, staff, cfg)
			finalizeTurnMetadata(&turn, before, next, ExpectedInputLifecycleConfirmation, ExpectedInputLifecycleConfirmation, "internal_reschedule_confirmation_required")
			return s.store.SaveTurn(ctx, turn)
		}
	}
	if sessionHasManualAppointmentTarget(next) && strings.TrimSpace(next.ServiceID) == "" {
		turn.AIMessage = "Which service is on the appointment you want to reschedule?"
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "service", "service", knowledge)
		finalizeTurnMetadata(&turn, before, next, "service", "service", "reschedule_manual_target_missing_service")
		return s.store.SaveTurn(ctx, turn)
	}
	if sessionHasManualAppointmentTarget(next) && strings.TrimSpace(next.CustomerName) == "" {
		turn.AIMessage = "What name is on that appointment?"
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_name", "customer_name", knowledge)
		finalizeTurnMetadata(&turn, before, next, "customer_name", "customer_name", "reschedule_manual_target_missing_name")
		return s.store.SaveTurn(ctx, turn)
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
				if _, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle && errors.Is(err, booking.ErrOperationConflict) {
					return s.reofferInternalLifecycleAfterConflict(ctx, ownerUserID, turn, next, services, staff, cfg, scheduling.OperationKindReschedule)
				}
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check new appointment times. The owner needs to review this reschedule request, and the appointment is not rescheduled yet.", services, staff, cfg)
			}
			if len(next.OfferedSlots) > 0 {
				turn.AIMessage = formatRescheduleSlotOfferForSession(next.OfferedSlots, loc, false, next, services)
			}
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_time", "requested_time", knowledge)
			finalizeTurnMetadata(&turn, before, next, "requested_time", "requested_time", "reschedule_availability_offer")
			return s.store.SaveTurn(ctx, turn)
		}
		if shouldHandoffRepeatedRescheduleNewTime(before, message) {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I'm having trouble catching the new date and time. The owner needs to review this reschedule request, and the appointment is not rescheduled yet.", services, staff, cfg)
		}
		turn.AIMessage = "What new day and time would you like?"
		s.applyReplyGenerator(ctx, &turn, next, services, cfg, "requested_start_time", "requested_start_time", knowledge)
		finalizeTurnMetadata(&turn, before, next, "requested_start_time", "requested_start_time", "reschedule_missing_new_time")
		return s.store.SaveTurn(ctx, turn)
	}

	if invalidateCarriedAvailabilityProof(before, &next) {
		syncTurnUpdate(&turn, next, services, staff, cfg)
	}
	if shouldCheckAvailabilityForRequestedTime(before, next, selectedOfferedSlot) {
		available, _, err := s.applyAvailabilityForRequestedTime(ctx, ownerUserID, &turn, &next, services, staff, cfg)
		if err != nil {
			if _, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle && errors.Is(err, booking.ErrOperationConflict) {
				return s.reofferInternalLifecycleAfterConflict(ctx, ownerUserID, turn, next, services, staff, cfg, scheduling.OperationKindReschedule)
			}
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not check new appointment times. The owner needs to review this reschedule request, and the appointment is not rescheduled yet.", services, staff, cfg)
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

	if !cfg.AIEnabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "AI booking is not enabled yet. The owner needs to review this cancellation request, and the appointment is not cancelled yet.", services, staff, cfg)
	}
	if configuredConversationBookingMode(cfg) == scheduling.BookingModeDisabled {
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonAIBookingDisabled, "The AI receptionist is not accepting scheduling actions right now. The owner can help with this cancellation request, and the appointment is not cancelled.", services, staff, cfg)
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

	if strings.TrimSpace(next.TargetAppointmentID) == "" && manualAppointmentTargetPending(before) {
		description := normalizeManualAppointmentTarget(message)
		if description == "" {
			turn.AIMessage = manualAppointmentTargetPrompt(BookingActionCancel)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_manual_target_repeated")
			return s.store.SaveTurn(ctx, turn)
		}
		applyManualAppointmentTarget(&next, description)
		clearSchedulingFieldsCapturedFromManualTarget(&next)
		syncTurnUpdate(&turn, next, services, staff, cfg)
	}
	if internalLifecyclePending(before, PendingInternalCancelReason) {
		next = cloneSessionForTurn(before)
		candidate, ok := selectedInternalLifecycleCandidate(next)
		if !ok {
			return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
		}
		if isNegativeOnly(message) {
			clearCancelSelection(&next)
			clearInternalLifecyclePending(&next)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = "Okay, I did not cancel the appointment. What would you like help with instead?"
			finalizeTurnMetadata(&turn, before, next, ExpectedInputCallerGoal, ExpectedInputCallerGoal, "internal_cancel_reason_aborted")
			return s.store.SaveTurn(ctx, turn)
		}
		reason := strings.TrimSpace(message)
		if reason == "" {
			turn.AIMessage = internalLifecycleCancelReasonPrompt(candidate, loc)
			finalizeTurnMetadata(&turn, before, next, ExpectedInputCancellationReason, ExpectedInputCancellationReason, "internal_cancel_reason_repeated")
			return s.store.SaveTurn(ctx, turn)
		}
		setInternalLifecyclePending(&next, PendingInternalCancelConfirmation, reason)
		syncTurnUpdate(&turn, next, services, staff, cfg)
		turn.AIMessage = internalLifecycleCancelConfirmationPrompt(candidate, reason, loc)
		finalizeTurnMetadata(&turn, before, next, ExpectedInputLifecycleConfirmation, ExpectedInputLifecycleConfirmation, "internal_cancel_confirmation_required")
		return s.store.SaveTurn(ctx, turn)
	}
	if internalLifecyclePending(before, PendingInternalCancelConfirmation) {
		next = cloneSessionForTurn(before)
		if isNegativeOnly(message) || hasCancelNegation(message) {
			clearInternalLifecyclePending(&next)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = "Okay, I did not cancel the appointment. What would you like help with instead?"
			finalizeTurnMetadata(&turn, before, next, ExpectedInputCallerGoal, ExpectedInputCallerGoal, "internal_cancel_confirmation_rejected")
			return s.store.SaveTurn(ctx, turn)
		}
		if !isAffirmativeOnly(message) {
			candidate, ok := selectedInternalLifecycleCandidate(next)
			if !ok {
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, cancelErrorReply(), services, staff, cfg)
			}
			turn.AIMessage = internalLifecycleCancelConfirmationPrompt(candidate, selectedInternalLifecycleCancelReason(next), loc)
			finalizeTurnMetadata(&turn, before, next, ExpectedInputLifecycleConfirmation, ExpectedInputLifecycleConfirmation, "internal_cancel_confirmation_repeated")
			return s.store.SaveTurn(ctx, turn)
		}
		return s.tryCancel(ctx, ownerUserID, turn, next, services, staff, cfg)
	}
	if sessionHasManualAppointmentTarget(next) {
		if isNegativeOnly(message) || hasCancelNegation(message) {
			clearCancelSelection(&next)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = "Okay, I will not record a cancellation request. What would you like help with instead?"
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "caller_goal", "caller_goal", knowledge)
			finalizeTurnMetadata(&turn, before, next, "caller_goal", "caller_goal", "cancel_manual_target_rejected")
			return s.store.SaveTurn(ctx, turn)
		}
		if strings.TrimSpace(next.CustomerName) == "" {
			turn.AIMessage = "What name is on that appointment?"
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "customer_name", "customer_name", knowledge)
			finalizeTurnMetadata(&turn, before, next, "customer_name", "customer_name", "cancel_manual_target_missing_name")
			return s.store.SaveTurn(ctx, turn)
		}
		return s.tryCancel(ctx, ownerUserID, turn, next, services, staff, cfg)
	}

	if strings.TrimSpace(next.TargetAppointmentID) == "" {
		if selected := selectRescheduleCandidate(message, next.RescheduleCandidates, services, serviceAliases, categoryAliases, loc, s.now); selected != nil {
			applyCancelCandidate(&next, *selected, loc)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			if _, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle {
				setInternalLifecyclePending(&next, PendingInternalCancelReason, "")
				syncTurnUpdate(&turn, next, services, staff, cfg)
				turn.AIMessage = internalLifecycleCancelReasonPrompt(*selected, loc)
				finalizeTurnMetadata(&turn, before, next, ExpectedInputCancellationReason, ExpectedInputCancellationReason, "internal_cancel_reason_required")
				return s.store.SaveTurn(ctx, turn)
			}
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
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not look up the appointment safely. The owner needs to review this cancellation request, and the appointment is not cancelled yet.", services, staff, cfg)
			}
			next.RescheduleCandidates = rescheduleCandidatesFromAppointments(candidates)
			syncTurnUpdate(&turn, next, services, staff, cfg)
		}
		if selected := selectRescheduleCandidate(message, next.RescheduleCandidates, services, serviceAliases, categoryAliases, loc, s.now); selected != nil {
			applyCancelCandidate(&next, *selected, loc)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			if _, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle {
				setInternalLifecyclePending(&next, PendingInternalCancelReason, "")
				syncTurnUpdate(&turn, next, services, staff, cfg)
				turn.AIMessage = internalLifecycleCancelReasonPrompt(*selected, loc)
				finalizeTurnMetadata(&turn, before, next, ExpectedInputCancellationReason, ExpectedInputCancellationReason, "internal_cancel_reason_required")
				return s.store.SaveTurn(ctx, turn)
			}
			turn.AIMessage = cancelSingleCandidatePrompt(*selected, loc)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_target_confirmation")
			return s.store.SaveTurn(ctx, turn)
		}
		switch len(next.RescheduleCandidates) {
		case 0:
			manualTargetAllowed, policyErr := s.conversationAllowsManualTarget(ctx, before.SalonID, ownerUserID, cfg)
			if policyErr != nil || !manualTargetAllowed {
				return s.saveHandoffTurn(ctx, turn, next, HandoffReasonBookingUnavailable, "I could not find an upcoming appointment for this phone number, so the owner needs to review the cancellation request. The appointment is not cancelled yet.", services, staff, cfg)
			}
			setManualAppointmentTargetPending(&next)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			turn.AIMessage = manualAppointmentTargetPrompt(BookingActionCancel)
			s.applyReplyGenerator(ctx, &turn, next, services, cfg, "target_appointment", "target_appointment", knowledge)
			finalizeTurnMetadata(&turn, before, next, "target_appointment", "target_appointment", "cancel_manual_target_prompt")
			return s.store.SaveTurn(ctx, turn)
		case 1:
			applyCancelCandidate(&next, next.RescheduleCandidates[0], loc)
			syncTurnUpdate(&turn, next, services, staff, cfg)
			if _, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle {
				setInternalLifecyclePending(&next, PendingInternalCancelReason, "")
				syncTurnUpdate(&turn, next, services, staff, cfg)
				turn.AIMessage = internalLifecycleCancelReasonPrompt(next.RescheduleCandidates[0], loc)
			} else {
				turn.AIMessage = cancelSingleCandidatePrompt(next.RescheduleCandidates[0], loc)
			}
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
	if candidate, internalLifecycle := selectedInternalLifecycleCandidate(next); internalLifecycle {
		setInternalLifecyclePending(&next, PendingInternalCancelReason, "")
		syncTurnUpdate(&turn, next, services, staff, cfg)
		turn.AIMessage = internalLifecycleCancelReasonPrompt(candidate, loc)
		finalizeTurnMetadata(&turn, before, next, ExpectedInputCancellationReason, ExpectedInputCancellationReason, "internal_cancel_reason_required")
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

func manualAppointmentTargetPending(session Session) bool {
	return session.DialogState.Pending != nil && session.DialogState.Pending.PromptKey == PendingManualAppointmentTarget
}

func (s *Service) conversationAllowsManualTarget(ctx context.Context, salonID string, ownerUserID string, cfg *RuntimeConfig) (bool, error) {
	if cfg != nil && cfg.BookingMode != "" {
		return cfg.BookingMode == scheduling.BookingModePendingApproval, nil
	}
	if s.schedulingTool == nil {
		return false, nil
	}
	authority, err := s.schedulingTool.CurrentSchedulingAuthority(ctx, salonID, ownerUserID)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(authority) == booking.SchedulingAuthorityOwnerManual, nil
}

func sessionHasManualAppointmentTarget(session Session) bool {
	return strings.TrimSpace(session.TargetAppointmentID) == "" &&
		session.DialogState.ManualTarget != nil &&
		strings.TrimSpace(session.DialogState.ManualTarget.Description) != ""
}

func manualAppointmentTargetExpectedInput(session Session) string {
	if !sessionHasManualAppointmentTarget(session) {
		return ""
	}
	switch bookingActionForSession(session) {
	case BookingActionCancel:
		if strings.TrimSpace(session.CustomerName) == "" {
			return ExpectedInputCustomerName
		}
		return ExpectedInputBookingContinuation
	case BookingActionReschedule:
		if strings.TrimSpace(session.ServiceID) == "" {
			return ExpectedInputService
		}
		if strings.TrimSpace(session.CustomerName) == "" {
			return ExpectedInputCustomerName
		}
		if session.RequestedStartTime == nil {
			if strings.TrimSpace(session.RequestedDate) == "" {
				return ExpectedInputRequestedDate
			}
			return ExpectedInputRequestedTime
		}
		return ExpectedInputBookingContinuation
	default:
		return ""
	}
}

func applyManualAppointmentTargetServiceSelection(session *Session, understanding serviceUnderstandingResult) bool {
	if session == nil || manualAppointmentTargetExpectedInput(*session) != ExpectedInputService ||
		understanding.Status != serviceUnderstandingStatusSelected || len(understanding.Candidates) != 1 {
		return false
	}
	return applyServiceSelection(session, understanding.Candidates)
}

func setManualAppointmentTargetPending(session *Session) {
	if session == nil {
		return
	}
	state := normalizedDialogState(session.DialogState)
	state.ManualTarget = nil
	state.Pending = &PendingConversationAct{
		Kind:      "collect_appointment_target",
		Entity:    "appointment_target",
		PromptKey: PendingManualAppointmentTarget,
	}
	session.DialogState = state
}

func applyManualAppointmentTarget(session *Session, description string) {
	if session == nil || strings.TrimSpace(session.TargetAppointmentID) != "" {
		return
	}
	state := normalizedDialogState(session.DialogState)
	state.Pending = nil
	state.ManualTarget = &ManualAppointmentTarget{Description: normalizeManualAppointmentTarget(description)}
	session.DialogState = state
}

func clearSchedulingFieldsCapturedFromManualTarget(session *Session) {
	if session == nil {
		return
	}
	session.RequestedDate = ""
	session.RequestedStartTime = nil
	session.OfferedSlots = nil
	clearSelectedAvailabilityQuote(session)
	session.DialogState.SchedulingRequestOnly = false
}

func normalizeManualAppointmentTarget(description string) string {
	description = strings.Join(strings.Fields(strings.TrimSpace(description)), " ")
	const maxTargetRunes = 2000
	runes := []rune(description)
	if len(runes) > maxTargetRunes {
		description = string(runes[:maxTargetRunes])
	}
	return strings.TrimSpace(description)
}

func manualAppointmentTargetPrompt(action string) string {
	if action == BookingActionCancel {
		return "Please describe the appointment's day, time, and service so the owner can identify the cancellation request."
	}
	return "Please describe the current appointment's day, time, and service so the owner can identify the reschedule request."
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
