package conversation

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	unconsumedSchedulingConstraintPattern = regexp.MustCompile(`(?i)\b(?:morning|afternoon|evening|noon|midday|lunch(?:time)?|early|earlier|earliest|late|later|latest|before|after|around|between)\b`)
)

const (
	TurnRouteFastLane     = "fast_lane"
	TurnRouteSemanticLane = "semantic_lane"
	TurnRouteAnswerLane   = "answer_lane"
	TurnRouteActionLane   = "action_lane"
	TurnRouteRecoveryLane = "recovery_lane"

	TurnCoverageNone     = "none"
	TurnCoveragePartial  = "partial"
	TurnCoverageComplete = "complete"

	ExpectedInputCallerGoal                 = "caller_goal"
	ExpectedInputService                    = "service"
	ExpectedInputRequestedDate              = "requested_date"
	ExpectedInputRequestedTime              = "requested_time"
	ExpectedInputOfferedSlot                = "offered_slot"
	ExpectedInputCustomerName               = "customer_name"
	ExpectedInputCustomerNameConfirmation   = "customer_name_confirmation"
	ExpectedInputCustomerPhone              = "customer_phone"
	ExpectedInputStaff                      = "staff"
	ExpectedInputPartySplitDateConsent      = "party_split_date_consent"
	ExpectedInputPendingServiceOperation    = "pending_service_operation"
	ExpectedInputDateTimeConfirmation       = "date_time_confirmation"
	ExpectedInputAppointmentTarget          = "appointment_target"
	ExpectedInputBookingReview              = "booking_review"
	ExpectedInputBookingContinuation        = "booking_continuation"
	ExpectedInputConsultationCurrentSystem  = "consultation_current_system"
	ExpectedInputConsultationDesiredOutcome = "consultation_desired_outcome"
	ExpectedInputConsultationSelection      = "consultation_selection"
	ExpectedInputConsultationBooking        = "consultation_booking"
)

func IsExpectedInput(value string) bool {
	switch strings.TrimSpace(value) {
	case ExpectedInputCallerGoal, ExpectedInputService, ExpectedInputRequestedDate,
		ExpectedInputRequestedTime, ExpectedInputOfferedSlot, ExpectedInputCustomerName,
		ExpectedInputCustomerNameConfirmation, ExpectedInputCustomerPhone, ExpectedInputStaff,
		ExpectedInputPartySplitDateConsent, ExpectedInputPendingServiceOperation,
		ExpectedInputDateTimeConfirmation, ExpectedInputAppointmentTarget, ExpectedInputBookingReview,
		ExpectedInputBookingContinuation, ExpectedInputConsultationCurrentSystem,
		ExpectedInputConsultationDesiredOutcome, ExpectedInputConsultationSelection,
		ExpectedInputConsultationBooking:
		return true
	default:
		return false
	}
}

// TurnEnvelope is the immutable state and catalog snapshot used to route one
// caller turn. It deliberately contains no persistence or provider handles.
type TurnEnvelope struct {
	Message         string
	Session         Session
	ExpectedInput   string
	Services        []ServiceOption
	ServiceAliases  []ServiceAlias
	CategoryAliases []ServiceCategoryAlias
	Staff           []StaffOption
	ActiveStaff     []StaffOption
}

// TurnPlan is a side-effect-free routing decision. Only the reducer and the
// downstream action planner may mutate booking state or invoke provider tools.
type TurnPlan struct {
	Message               string
	Route                 string
	ExpectedInput         string
	Reason                string
	DeterministicCoverage string
	Understanding         TurnUnderstanding
	ReviewResponse        reviewResponseKind

	ServiceUnderstanding        serviceUnderstandingResult
	StaffChange                 staffChangeRequest
	PartySignal                 partySignal
	PendingNameCandidate        string
	RecognizableGuidanceActions []string
	AvailableGuidanceActions    []string
	SemanticServices            []ServiceOption
	SemanticStaff               []StaffOption
}

func (p TurnPlan) timingAttributes() map[string]string {
	attributes := map[string]string{
		"turn_route":                  p.Route,
		"turn_expected_input":         p.ExpectedInput,
		"turn_route_reason":           p.Reason,
		"turn_deterministic_coverage": p.DeterministicCoverage,
		"turn_model_service_count":    strconv.Itoa(len(p.SemanticServices)),
		"turn_model_staff_count":      strconv.Itoa(len(p.SemanticStaff)),
	}
	if p.ReviewResponse != "" {
		attributes["turn_review_response"] = string(p.ReviewResponse)
	}
	return attributes
}

func applyTurnPlanMetadata(turn *TurnRecord, plan TurnPlan) {
	if turn == nil {
		return
	}
	metadata := map[string]any{
		"turn_route":                         plan.Route,
		"turn_expected_input":                plan.ExpectedInput,
		"turn_route_reason":                  plan.Reason,
		"turn_deterministic_coverage":        plan.DeterministicCoverage,
		"turn_model_service_count":           len(plan.SemanticServices),
		"turn_model_staff_count":             len(plan.SemanticStaff),
		"turn_recognizable_guidance_actions": append([]string(nil), plan.RecognizableGuidanceActions...),
		"turn_available_guidance_actions":    append([]string(nil), plan.AvailableGuidanceActions...),
	}
	if plan.ReviewResponse != "" {
		metadata["turn_review_response"] = string(plan.ReviewResponse)
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, metadata)
}

func expectedInputForSession(session Session) string {
	state := normalizedDialogState(session.DialogState)
	if guidanceRecoveryStateActive(state.Guidance) {
		return guidanceRecoveryExpectedInput(state.Guidance)
	}
	if consultation := state.Consultation; consultationStateActive(consultation) {
		switch consultation.Status {
		case ConsultationStatusAwaitingSelection:
			return ExpectedInputConsultationSelection
		case ConsultationStatusAwaitingBooking:
			return ExpectedInputConsultationBooking
		default:
			if field := strings.TrimSpace(consultation.LastAskedField); field != "" {
				return "consultation_" + field
			}
			return ExpectedInputConsultationDesiredOutcome
		}
	}
	if state.Pending != nil {
		if invalidServiceEditPending(session) {
			return ExpectedInputService
		}
		switch state.Pending.PromptKey {
		case pendingOfferedSlotDateTimeCorrection:
			return ExpectedInputDateTimeConfirmation
		case PendingCustomerNameConfirmation:
			return ExpectedInputCustomerNameConfirmation
		case PendingStaffAlternative:
			return ExpectedInputStaff
		default:
			return ExpectedInputPendingServiceOperation
		}
	}
	if missingBookingField(session) == "customer_name" && pendingCustomerName(session) != "" {
		return ExpectedInputCustomerNameConfirmation
	}
	if state.Phase == DialogPhaseReview {
		return ExpectedInputBookingReview
	}
	action := bookingActionForSession(session)
	if action == BookingActionCancel || action == BookingActionReschedule {
		if strings.TrimSpace(session.CustomerPhone) == "" {
			return ExpectedInputCustomerPhone
		}
		if strings.TrimSpace(session.TargetAppointmentID) == "" {
			return ExpectedInputAppointmentTarget
		}
	}
	if !hasOperationalBookingProgress(session) && strings.TrimSpace(session.Intent) != IntentBooking {
		return ExpectedInputCallerGoal
	}
	if len(session.OfferedSlots) > 0 && missingBookingField(session) == "requested_time" {
		return ExpectedInputOfferedSlot
	}
	switch missingBookingField(session) {
	case "service":
		return ExpectedInputService
	case "requested_date":
		return ExpectedInputRequestedDate
	case "requested_time", "requested_start_time":
		return ExpectedInputRequestedTime
	case "customer_name":
		return ExpectedInputCustomerName
	case "customer_phone":
		return ExpectedInputCustomerPhone
	case "staff":
		return ExpectedInputStaff
	case "party_split_date_consent":
		return ExpectedInputPartySplitDateConsent
	default:
		return ExpectedInputBookingContinuation
	}
}

func (s *Service) planTurn(message string, session Session, answerCtx *AIAnswerContext, cfg *RuntimeConfig) TurnPlan {
	services := answerServices(answerCtx)
	aliases := answerServiceAliases(answerCtx)
	categoryAliases := answerCategoryAliases(answerCtx)
	staff := answerStaff(answerCtx)
	activeStaff := answerActiveStaff(answerCtx)
	envelope := TurnEnvelope{
		Message: message, Session: cloneSessionForTurn(session), ExpectedInput: expectedInputForSession(session),
		Services: services, ServiceAliases: aliases, CategoryAliases: categoryAliases, Staff: staff, ActiveStaff: activeStaff,
	}
	plan := TurnPlan{Message: message, ExpectedInput: envelope.ExpectedInput, DeterministicCoverage: TurnCoverageNone}
	plan.ServiceUnderstanding = interpretServiceForSession(message, session, services, aliases, categoryAliases)
	if catalogUnderstanding := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases); catalogUnderstanding.Status != serviceUnderstandingStatusUnknown &&
		(plan.ServiceUnderstanding.Status == serviceUnderstandingStatusUnknown || !serviceSelectionScopedToPending(session, services)) {
		plan.ServiceUnderstanding = catalogUnderstanding
	}
	plan.StaffChange = detectStaffChangeRequest(message, session, services, aliases, categoryAliases, staff, activeStaff)
	plan.PartySignal = detectPartySignal(message, session, plan.ServiceUnderstanding, services, aliases, categoryAliases)
	plan.PendingNameCandidate = voiceCustomerNamePendingConfirmationCandidate(message, session)
	if plan.PendingNameCandidate != "" && !validCustomerNameCandidate(plan.PendingNameCandidate, cfg, services, staff) {
		plan.PendingNameCandidate = ""
	}

	state := normalizedDialogState(session.DialogState)
	guidanceStage := GuidanceRecoveryStageCallerGoal
	if envelope.ExpectedInput == ExpectedInputService {
		guidanceStage = GuidanceRecoveryStageService
	}
	plan.RecognizableGuidanceActions = GuidanceActionValues()
	plan.AvailableGuidanceActions = guidanceRecoveryOfferedActions(guidanceStage, services, cfg)
	if state.Pending != nil && state.Pending.PromptKey == PendingStaffAlternative {
		if matched := matchStaff(message, staff); matched != nil && stringSet(state.Pending.TargetServiceIDs)[strings.TrimSpace(matched.ID)] {
			plan.Understanding = TurnUnderstanding{
				Goal: "book_appointment", Acts: []ConversationAct{{
					Kind: ConversationActSet, Entity: ConversationEntityStaff, TargetServiceIDs: []string{matched.ID},
					Confidence: 1, Reason: "pending_staff_alternative_selection", Source: "turn_kernel",
				}}, Confidence: 1, Reason: "pending_staff_alternative_selection", Source: "turn_kernel", InterpreterOutcome: "skipped_fast_lane",
			}
			return finalizeTurnPlan(plan, TurnRouteFastLane, "pending_staff_alternative_selection", TurnCoverageComplete, session, services, staff)
		}
	}
	if state.Pending != nil && state.Pending.PromptKey == pendingOfferedSlotDateTimeCorrection {
		return finalizeTurnPlan(plan, TurnRouteFastLane, "pending_date_time_confirmation", TurnCoverageComplete, session, services, staff)
	}
	if envelope.ExpectedInput == ExpectedInputCustomerNameConfirmation {
		if isPendingCustomerNameAffirmative(message) || isExactNegativeNameConfirmation(message) {
			return finalizeTurnPlan(plan, TurnRouteFastLane, "pending_customer_name_confirmation", TurnCoverageComplete, session, services, staff)
		}
	}
	if salonIdentityReplyForMessage(message, session, cfg) != "" {
		return finalizeTurnPlan(plan, TurnRouteAnswerLane, "salon_identity", TurnCoverageComplete, session, services, staff)
	}
	if _, ok := offeredSlotRejectionForMessage(message, session, timezoneLocation(timezoneFromConfig(cfg))); ok {
		return finalizeTurnPlan(plan, TurnRouteFastLane, "offered_slot_rejection", TurnCoverageComplete, session, services, staff)
	}
	if _, ok := directionalSlotTimePreferenceForMessage(message, session); ok {
		return finalizeTurnPlan(plan, TurnRouteFastLane, "offered_slot_time_preference", TurnCoverageComplete, session, services, staff)
	}
	if reply, _ := customerNameSlotRepairReply(message, session, services, aliases, categoryAliases, cfg); reply != "" {
		return finalizeTurnPlan(plan, TurnRouteRecoveryLane, "customer_name_repair", TurnCoverageComplete, session, services, staff)
	}
	if repairReplyForMessage(message, session, cfg) != "" {
		return finalizeTurnPlan(plan, TurnRouteRecoveryLane, "deterministic_repair", TurnCoverageComplete, session, services, staff)
	}
	if consultationStateActive(state.Consultation) {
		if shouldClarifyCancelReschedule(session, message) {
			return finalizeTurnPlan(plan, TurnRouteActionLane, "cancel_reschedule_clarification", TurnCoverageComplete, session, services, staff)
		}
		if shouldRouteCancel(session, message) {
			plan.Understanding = deterministicGoalUnderstanding("cancel_appointment", "cancel_action")
			return finalizeTurnPlan(plan, TurnRouteActionLane, "cancel_action", TurnCoverageComplete, session, services, staff)
		}
		if shouldRouteReschedule(session, message) {
			plan.Understanding = deterministicGoalUnderstanding("reschedule_appointment", "reschedule_action")
			return finalizeTurnPlan(plan, TurnRouteActionLane, "reschedule_action", TurnCoverageComplete, session, services, staff)
		}
		if isGoodbyeUtterance(message) || isConsultationSafetyConcern(message) ||
			(state.Consultation.Status == ConsultationStatusAwaitingBooking && isAffirmativeOnly(message)) ||
			(state.Consultation.Status == ConsultationStatusAwaitingSelection && isExactAffirmativeResponse(message)) ||
			(state.Consultation.Status == ConsultationStatusAwaitingSelection && plan.ServiceUnderstanding.Status == serviceUnderstandingStatusSelected) {
			plan.Understanding = deterministicGoalUnderstanding("consultation", "consultation_state_owned_turn")
			return finalizeTurnPlan(plan, TurnRouteFastLane, "consultation_state_owned_turn", TurnCoverageComplete, session, services, staff)
		}
		return finalizeTurnPlan(plan, TurnRouteSemanticLane, "consultation_context_required", TurnCoverageNone, session, services, staff)
	}
	if shouldClarifyCancelReschedule(session, message) {
		return finalizeTurnPlan(plan, TurnRouteActionLane, "cancel_reschedule_clarification", TurnCoverageComplete, session, services, staff)
	}
	if shouldRouteCancel(session, message) {
		plan.Understanding = deterministicGoalUnderstanding("cancel_appointment", "cancel_action")
		return finalizeTurnPlan(plan, TurnRouteActionLane, "cancel_action", TurnCoverageComplete, session, services, staff)
	}
	if shouldRouteReschedule(session, message) {
		plan.Understanding = deterministicGoalUnderstanding("reschedule_appointment", "reschedule_action")
		return finalizeTurnPlan(plan, TurnRouteActionLane, "reschedule_action", TurnCoverageComplete, session, services, staff)
	}

	loc := timezoneLocation(timezoneFromConfig(cfg))
	if stateScopedOfferedSlotSelection(message, session, loc) {
		return finalizeTurnPlan(plan, TurnRouteFastLane, "offered_slot_selection", TurnCoverageComplete, session, services, staff)
	}
	signals := deterministicTurnSignals(envelope, plan, s.now, loc)
	if asksCurrentBookingQuestion(message, session) && isStandaloneCurrentBookingQuestion(message) && !hasServiceMutationLanguage(message) && !hasNonServiceTurnSignals(signals) {
		plan.Understanding = TurnUnderstanding{
			Goal: "information", Questions: []ConversationQuestion{{Subject: ConversationQuestionCurrentBooking, Confidence: 1, Reason: "current_booking_question"}},
			Confidence: 1, Reason: "current_booking_question", Source: "turn_kernel", InterpreterOutcome: "skipped_answer_lane",
		}
		return finalizeTurnPlan(plan, TurnRouteAnswerLane, "current_booking_question", TurnCoverageComplete, session, services, staff)
	}
	if len(plan.ServiceUnderstanding.Candidates) > 1 {
		return finalizeTurnPlan(plan, TurnRouteSemanticLane, "multi_service_semantic_context", TurnCoveragePartial, session, services, staff)
	}
	// Freeform initial goals and guidance turns are classified by the semantic
	// contract. Catalog/category matching contributes grounded evidence, but a
	// phrase list must never decide whether the caller asked for a menu, advice,
	// service details, or booking help.
	if envelope.ExpectedInput == ExpectedInputCallerGoal || guidanceRecoveryStateActive(state.Guidance) {
		reason := "initial_guidance_semantic_context"
		coverage := TurnCoverageNone
		if len(signals) > 1 {
			reason = "multiple_signals"
			coverage = TurnCoveragePartial
		}
		return finalizeTurnPlan(plan, TurnRouteSemanticLane, reason, coverage, session, services, staff)
	}
	answer := routeNonBookingAnswer(message, session, answerCtx, cfg, s.now)
	if answer.Source == answerSourceKnowledge &&
		(envelope.ExpectedInput == ExpectedInputCallerGoal || envelope.ExpectedInput == ExpectedInputService) &&
		!hasOperationalBookingProgress(session) {
		answer = answerRoute{}
	}
	if state.Phase == DialogPhaseReview {
		plan.ReviewResponse = classifyStateScopedReviewResponse(message)
		if plan.ReviewResponse != reviewResponseAccept && plan.ReviewResponse != reviewResponseReject {
			return finalizeTurnPlan(plan, TurnRouteSemanticLane, "review_semantic_context_required", TurnCoveragePartial, session, services, staff)
		}
	}
	if answer.Handled && answer.Source != answerSourceBookingRedirect && len(signals) == 0 {
		standaloneCatalogQuestion := answer.Source == answerSourceServiceCatalog
		if hasOperationalBookingProgress(session) && !standaloneCatalogQuestion {
			return finalizeTurnPlan(plan, TurnRouteSemanticLane, "booking_context_answer_or_correction", TurnCoveragePartial, session, services, staff)
		}
		plan.Understanding = deterministicAnswerUnderstanding(answer)
		return finalizeTurnPlan(plan, TurnRouteAnswerLane, "structured_answer", TurnCoverageComplete, session, services, staff)
	}

	deterministic := deterministicConversationActForReviewResponse(session, message, services, aliases, categoryAliases, plan.ReviewResponse)
	if deterministic.Kind != ConversationActUnknown {
		if deterministic.Entity == "" {
			deterministic.Entity = defaultConversationActEntity(deterministic.Kind)
		}
		plan.Understanding = TurnUnderstanding{
			Goal: "book_appointment", Acts: []ConversationAct{deterministic}, Confidence: deterministic.Confidence,
			Reason: deterministic.Reason, Source: "turn_kernel", InterpreterOutcome: "skipped_fast_lane",
		}
		return finalizeTurnPlan(plan, TurnRouteFastLane, "state_owned_conversation_act", TurnCoverageComplete, session, services, staff)
	}

	if shouldClarifyCancelReschedule(session, message) {
		return finalizeTurnPlan(plan, TurnRouteActionLane, "cancel_reschedule_clarification", TurnCoverageComplete, session, services, staff)
	}
	if shouldRouteCancel(session, message) {
		plan.Understanding = deterministicGoalUnderstanding("cancel_appointment", "cancel_action")
		return finalizeTurnPlan(plan, TurnRouteActionLane, "cancel_action", TurnCoverageComplete, session, services, staff)
	}
	if shouldRouteReschedule(session, message) {
		plan.Understanding = deterministicGoalUnderstanding("reschedule_appointment", "reschedule_action")
		return finalizeTurnPlan(plan, TurnRouteActionLane, "reschedule_action", TurnCoverageComplete, session, services, staff)
	}

	if plan.PartySignal.IsParty && session.PartyPlan == nil && plan.PartySignal.Confidence >= 0.85 {
		plan.Understanding = deterministicGoalUnderstanding("book_appointment", "catalog_backed_party_plan")
		return finalizeTurnPlan(plan, TurnRouteFastLane, "catalog_backed_party_plan", TurnCoverageComplete, session, services, staff)
	}

	if len(signals) == 1 && signalMatchesExpectedInput(signals[0], envelope.ExpectedInput, session) &&
		deterministicSignalFullyCoversTurn(message, signals[0], envelope.ExpectedInput) {
		plan.Understanding = deterministicGoalUnderstanding("book_appointment", "expected_input_resolved")
		return finalizeTurnPlan(plan, TurnRouteFastLane, "expected_input_resolved", TurnCoverageComplete, session, services, staff)
	}
	if len(signals) == 1 && signalMatchesExpectedInput(signals[0], envelope.ExpectedInput, session) {
		// A syntactic question can contain a deterministically extractable field
		// and an additional semantic constraint (for example, a date plus a time
		// window). Extraction may retain the grounded field, but it does not prove
		// that the whole caller turn has been consumed.
		return finalizeTurnPlan(plan, TurnRouteSemanticLane, "expected_input_partial_coverage", TurnCoveragePartial, session, services, staff)
	}
	if len(signals) > 1 {
		plan.DeterministicCoverage = TurnCoveragePartial
		return finalizeTurnPlan(plan, TurnRouteSemanticLane, "multiple_signals", TurnCoveragePartial, session, services, staff)
	}
	if answer.Handled && answer.Source != answerSourceBookingRedirect {
		plan.DeterministicCoverage = TurnCoveragePartial
		return finalizeTurnPlan(plan, TurnRouteSemanticLane, "structured_answer_conflict", TurnCoveragePartial, session, services, staff)
	}
	return finalizeTurnPlan(plan, TurnRouteSemanticLane, "semantic_context_required", TurnCoverageNone, session, services, staff)
}

func serviceSelectionScopedToPending(session Session, services []ServiceOption) bool {
	if len(pendingConsultationServices(session, services)) > 0 {
		return true
	}
	if len(pendingServiceCandidateServices(session, services)) > 0 {
		return true
	}
	if pending, mode, ok := pendingServiceEdit(session, services); ok && len(pending) > 0 && pendingServiceEditNeedsTarget(mode) {
		return true
	}
	state := normalizedDialogState(session.DialogState)
	return state.Pending != nil && (len(state.Pending.SourceServiceIDs) > 0 || len(state.Pending.TargetServiceIDs) > 0)
}

func asksCurrentBookingQuestion(message string, session Session) bool {
	if !hasOperationalBookingProgress(session) {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	questionSignal := strings.Contains(message, "?") || containsAnyLoosePhrase(normalized, []string{
		"what", "which", "how many", "do i have", "did i book", "have i booked", "what did i", "what does",
	})
	if !questionSignal {
		return false
	}
	return containsAnyLoosePhrase(normalized, []string{
		"my appointment", "my booking", "current appointment", "current booking", "did i book", "have i booked",
		"do i have", "what did i book", "what does my appointment", "what does my booking",
	}) || (containsLoosePhrase(normalized, "how many") && containsAnyLoosePhrase(normalized, []string{"i book", "i booked", "do i have", "my appointment", "my booking", "selected"}))
}

func isStandaloneCurrentBookingQuestion(message string) bool {
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	start := len(normalized)
	for _, marker := range []string{"what", "which", "how many", "do i have", "did i book", "have i booked"} {
		if index := strings.Index(normalized, marker); index >= 0 && index < start {
			start = index
		}
	}
	if start == len(normalized) {
		return false
	}
	prefix := strings.TrimSpace(normalized[:start])
	if prefix == "" {
		return true
	}
	allowed := map[string]bool{
		"and": true, "also": true, "please": true, "so": true, "then": true, "wait": true,
		"can": true, "could": true, "you": true, "tell": true, "me": true,
	}
	for _, token := range strings.Fields(prefix) {
		if !allowed[token] {
			return false
		}
	}
	return true
}

func hasNonServiceTurnSignals(signals []string) bool {
	for _, signal := range signals {
		if signal != ConversationEntityService {
			return true
		}
	}
	return false
}

func deterministicTurnSignals(envelope TurnEnvelope, plan TurnPlan, now func() time.Time, loc *time.Location) []string {
	signals := map[string]bool{}
	after := cloneSessionForTurn(envelope.Session)
	applyExtraction(&after, envelope.Message, envelope.Services, envelope.ServiceAliases, envelope.CategoryAliases, envelope.Staff, loc, now)
	if dateTimeSelectionChanged(envelope.Session, after) {
		signals[ConversationEntityDateTime] = true
	}
	if strings.TrimSpace(after.CustomerPhone) != strings.TrimSpace(envelope.Session.CustomerPhone) ||
		strings.TrimSpace(after.CustomerEmail) != strings.TrimSpace(envelope.Session.CustomerEmail) {
		signals[ExpectedInputCustomerPhone] = true
	}
	if strings.TrimSpace(after.CustomerName) != strings.TrimSpace(envelope.Session.CustomerName) || plan.PendingNameCandidate != "" {
		signals[ExpectedInputCustomerName] = true
	}
	if strings.TrimSpace(after.StaffID) != strings.TrimSpace(envelope.Session.StaffID) ||
		strings.TrimSpace(after.StaffSelectionMode) != strings.TrimSpace(envelope.Session.StaffSelectionMode) || plan.StaffChange.Intent {
		signals[ConversationEntityStaff] = true
	}
	if !plan.PartySignal.IsParty && plan.ServiceUnderstanding.Status != serviceUnderstandingStatusUnknown &&
		(plan.ExpectedInput == ExpectedInputService || plan.ExpectedInput == ExpectedInputCallerGoal ||
			catalogSelectionDiffersFromDraft(plan.ServiceUnderstanding, envelope.Session)) {
		signals[ConversationEntityService] = true
	}
	out := make([]string, 0, len(signals))
	for _, key := range []string{ConversationEntityService, ConversationEntityStaff, ConversationEntityDateTime, ExpectedInputCustomerName, ExpectedInputCustomerPhone} {
		if signals[key] {
			out = append(out, key)
		}
	}
	return out
}

func catalogSelectionDiffersFromDraft(understanding serviceUnderstandingResult, session Session) bool {
	if !hasSelectedServiceDraft(session) {
		return false
	}
	selected := stringSet(selectedServiceIDs(session))
	for _, candidate := range understanding.Candidates {
		id := strings.TrimSpace(candidate.ID)
		if id != "" && !selected[id] {
			return true
		}
	}
	return false
}

func signalMatchesExpectedInput(signal string, expected string, session Session) bool {
	switch signal {
	case ConversationEntityService:
		return !hasSelectedServiceDraft(session) && (expected == ExpectedInputService || expected == ExpectedInputCallerGoal)
	case ConversationEntityStaff:
		return expected == ExpectedInputStaff && !hasStaffAssignment(session)
	case ConversationEntityDateTime:
		return (expected == ExpectedInputRequestedDate || expected == ExpectedInputRequestedTime) && session.RequestedStartTime == nil
	case ExpectedInputCustomerName:
		return expected == ExpectedInputCustomerName && strings.TrimSpace(session.CustomerName) == ""
	case ExpectedInputCustomerPhone:
		return expected == ExpectedInputCustomerPhone && strings.TrimSpace(session.CustomerPhone) == ""
	default:
		return false
	}
}

func deterministicSignalFullyCoversTurn(message string, signal string, expected string) bool {
	message = strings.TrimSpace(message)
	if strings.Contains(message, "?") {
		return false
	}
	if signal != ConversationEntityDateTime {
		return true
	}
	switch expected {
	case ExpectedInputRequestedDate, ExpectedInputRequestedTime:
		// The deterministic extractor already proved that the expected field is
		// present. Fast-lane completion is safe only when the remaining utterance
		// does not carry a day-part or directional window that the extractor did
		// not represent in state.
		return !unconsumedSchedulingConstraintPattern.MatchString(message)
	default:
		return true
	}
}

func deterministicGoalUnderstanding(goal string, reason string) TurnUnderstanding {
	return TurnUnderstanding{
		Goal: goal, Confidence: 1, Reason: reason, Source: "turn_kernel", InterpreterOutcome: "skipped_fast_lane",
	}
}

func finalizeTurnPlan(plan TurnPlan, route string, reason string, coverage string, session Session, services []ServiceOption, staff []StaffOption) TurnPlan {
	plan.Route = route
	plan.Reason = reason
	plan.DeterministicCoverage = coverage
	if plan.Understanding.Source == "" && route != TurnRouteSemanticLane {
		plan.Understanding = deterministicGoalUnderstanding(turnGoalForRoute(route, session), reason)
	}
	plan.SemanticServices = semanticServiceScope(plan, session, services)
	plan.SemanticStaff = semanticStaffScope(plan, session, staff)
	return plan
}

func turnGoalForRoute(route string, session Session) string {
	if route == TurnRouteAnswerLane || route == TurnRouteRecoveryLane {
		return "unknown"
	}
	if consultationStateActive(normalizedDialogState(session.DialogState).Consultation) || strings.TrimSpace(session.Intent) == IntentConsultation {
		return "consultation"
	}
	if hasOperationalBookingProgress(session) || strings.TrimSpace(session.Intent) == IntentBooking {
		return "book_appointment"
	}
	return "unknown"
}

func semanticServiceScope(plan TurnPlan, session Session, services []ServiceOption) []ServiceOption {
	selected := selectedServiceOptions(session, services)
	if plan.Route != TurnRouteSemanticLane {
		return selected
	}
	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil {
		pendingIDs := append([]string(nil), state.Pending.SourceServiceIDs...)
		pendingIDs = append(pendingIDs, state.Pending.TargetServiceIDs...)
		return mergeServiceOptions(selected, servicesByIDs(services, pendingIDs), plan.ServiceUnderstanding.Candidates)
	}
	if semanticContractForTurnPlan(session, plan) == TurnSemanticContractGuidance {
		return mergeServiceOptions(selected, plan.ServiceUnderstanding.Candidates)
	}
	if strings.HasPrefix(plan.ExpectedInput, "consultation_") {
		return append([]ServiceOption(nil), services...)
	}
	if plan.ExpectedInput == ExpectedInputService || plan.ExpectedInput == ExpectedInputCallerGoal || session.PartyPlan != nil ||
		(hasOperationalBookingProgress(session) && (hasServiceMutationLanguage(plan.Message) || hasReferentialServiceMutation(plan.Message, session) ||
			mentionsServiceConcept(plan.Message, services) || (asksCurrentBookingQuestion(plan.Message, session) && !isStandaloneCurrentBookingQuestion(plan.Message)))) {
		return append([]ServiceOption(nil), services...)
	}
	return mergeServiceOptions(selected, plan.ServiceUnderstanding.Candidates)
}

func hasOperationalBookingProgress(session Session) bool {
	return session.Intent == IntentBooking ||
		strings.TrimSpace(session.ServiceID) != "" ||
		strings.TrimSpace(session.RequestedDate) != "" ||
		session.RequestedStartTime != nil ||
		len(session.OfferedSlots) > 0 ||
		activePartyPlan(session.PartyPlan) ||
		strings.TrimSpace(session.CustomerName) != "" ||
		hasStaffAssignment(session)
}

func hasSelectedServiceDraft(session Session) bool {
	if strings.TrimSpace(session.ServiceID) != "" {
		return true
	}
	for _, segment := range session.BookingSegments {
		if strings.TrimSpace(segment.ServiceID) != "" {
			return true
		}
	}
	return false
}

func invalidServiceEditPending(session Session) bool {
	if hasSelectedServiceDraft(session) {
		return false
	}
	state := normalizedDialogState(session.DialogState)
	if state.Pending == nil {
		return false
	}
	switch state.Pending.PromptKey {
	case "semantic_add_or_replace", "replace_target", "replace_source", "remove_source", "same_category_add_scope",
		PendingPartyServiceTarget, PendingPartyServiceGuest, PendingPartyServiceOperation, PendingPartyServiceSource:
		return true
	default:
		return false
	}
}

func repairInvalidServiceEditPending(session *Session) bool {
	if session == nil || !invalidServiceEditPending(*session) {
		return false
	}
	session.DialogState = resetDialogProgress(session.DialogState, DialogPhaseDrafting)
	return true
}

func hasServiceMutationLanguage(message string) bool {
	return hasServiceAddSignal(message) || hasServiceCorrectionSignal(message) ||
		hasExplicitServiceReplacementPhrase(message) || hasServiceReplaceSignal(message)
}

func mentionsServiceConcept(message string, services []ServiceOption) bool {
	normalized := normalizeServiceText(message)
	if normalized == "" {
		return false
	}
	for _, token := range []string{"service", "services", "treatment", "treatments", "option", "options", "choice", "choices", "manicure", "pedicure", "polish", "nail", "nails"} {
		if containsLoosePhrase(normalized, token) {
			return true
		}
	}
	for _, service := range services {
		for _, token := range append(serviceNameTokens(service.Name), serviceNameTokens(service.CategoryName)...) {
			if len(token) >= 4 && containsLoosePhrase(normalized, token) {
				return true
			}
		}
	}
	return false
}

func hasReferentialServiceMutation(message string, session Session) bool {
	if strings.TrimSpace(session.ServiceID) == "" {
		return false
	}
	normalized := normalizeLooseText(message)
	if normalized == "" {
		return false
	}
	directive := containsAnyLoosePhrase(normalized, []string{
		"use", "make", "change", "switch", "replace", "remove", "add", "prefer", "would suit", "better fit", "instead",
	})
	referent := containsAnyLoosePhrase(normalized, []string{
		"one", "it", "that", "this", "option", "choice", "service", "treatment", "appointment",
	})
	return directive && referent
}

func deterministicAnswerUnderstanding(route answerRoute) TurnUnderstanding {
	subject := ConversationQuestionPolicy
	switch route.Source {
	case answerSourceServiceCatalog:
		subject = ConversationQuestionCatalog
	case answerSourceBusinessHours:
		subject = ConversationQuestionHours
	case answerSourceStaff:
		subject = ConversationQuestionStaff
	case answerSourceAvailability:
		subject = ConversationQuestionAvailability
	}
	confidence := route.Confidence
	if confidence <= 0 {
		confidence = 1
	}
	return TurnUnderstanding{
		Goal: "information", Questions: []ConversationQuestion{{Subject: subject, Confidence: confidence, Reason: route.Reason}},
		Confidence: confidence, Reason: route.Reason, Source: "turn_kernel", InterpreterOutcome: "skipped_answer_lane",
	}
}

func semanticStaffScope(plan TurnPlan, session Session, staff []StaffOption) []StaffOption {
	selected := selectedStaffOptions(session, staff)
	if plan.Route != TurnRouteSemanticLane {
		return selected
	}
	if plan.ExpectedInput == ExpectedInputStaff || plan.StaffChange.Intent || matchStaff(plan.Message, staff) != nil || customerRequestedAnyone(plan.Message) {
		return append([]StaffOption(nil), staff...)
	}
	return selected
}

func mergeServiceOptions(groups ...[]ServiceOption) []ServiceOption {
	out := []ServiceOption{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, service := range group {
			id := strings.TrimSpace(service.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, service)
		}
	}
	return out
}

func answerServiceAliases(ctx *AIAnswerContext) []ServiceAlias {
	if ctx == nil {
		return nil
	}
	return ctx.ServiceAliases
}

func answerCategoryAliases(ctx *AIAnswerContext) []ServiceCategoryAlias {
	if ctx == nil {
		return nil
	}
	return ctx.CategoryAliases
}
