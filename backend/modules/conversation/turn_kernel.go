package conversation

import (
	"strconv"
	"strings"
	"time"
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

	ExpectedInputCallerGoal               = "caller_goal"
	ExpectedInputService                  = "service"
	ExpectedInputRequestedDate            = "requested_date"
	ExpectedInputRequestedTime            = "requested_time"
	ExpectedInputOfferedSlot              = "offered_slot"
	ExpectedInputCustomerName             = "customer_name"
	ExpectedInputCustomerNameConfirmation = "customer_name_confirmation"
	ExpectedInputCustomerPhone            = "customer_phone"
	ExpectedInputStaff                    = "staff"
	ExpectedInputPartySplitDateConsent    = "party_split_date_consent"
	ExpectedInputPendingServiceOperation  = "pending_service_operation"
	ExpectedInputDateTimeConfirmation     = "date_time_confirmation"
	ExpectedInputAppointmentTarget        = "appointment_target"
	ExpectedInputBookingReview            = "booking_review"
	ExpectedInputBookingContinuation      = "booking_continuation"
)

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

	ServiceUnderstanding serviceUnderstandingResult
	StaffChange          staffChangeRequest
	PartySignal          partySignal
	PendingNameCandidate string
	SemanticServices     []ServiceOption
	SemanticStaff        []StaffOption
}

func (p TurnPlan) timingAttributes() map[string]string {
	return map[string]string{
		"turn_route":                  p.Route,
		"turn_expected_input":         p.ExpectedInput,
		"turn_route_reason":           p.Reason,
		"turn_deterministic_coverage": p.DeterministicCoverage,
		"turn_model_service_count":    strconv.Itoa(len(p.SemanticServices)),
		"turn_model_staff_count":      strconv.Itoa(len(p.SemanticStaff)),
	}
}

func applyTurnPlanMetadata(turn *TurnRecord, plan TurnPlan) {
	if turn == nil {
		return
	}
	turn.CustomerMetadata = mergeMetadata(turn.CustomerMetadata, map[string]any{
		"turn_route":                  plan.Route,
		"turn_expected_input":         plan.ExpectedInput,
		"turn_route_reason":           plan.Reason,
		"turn_deterministic_coverage": plan.DeterministicCoverage,
		"turn_model_service_count":    len(plan.SemanticServices),
		"turn_model_staff_count":      len(plan.SemanticStaff),
	})
}

func expectedInputForSession(session Session) string {
	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil {
		if invalidServiceEditPending(session) {
			return ExpectedInputService
		}
		switch state.Pending.PromptKey {
		case pendingOfferedSlotDateTimeCorrection:
			return ExpectedInputDateTimeConfirmation
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
	if catalogUnderstanding := interpretServiceWithCategoryAliases(message, services, aliases, categoryAliases); isServiceInquiry(message, catalogUnderstanding) {
		plan.ServiceUnderstanding = catalogUnderstanding
	}
	plan.StaffChange = detectStaffChangeRequest(message, session, services, aliases, categoryAliases, staff, activeStaff)
	plan.PartySignal = detectPartySignal(message, session, plan.ServiceUnderstanding, services, aliases, categoryAliases)
	plan.PendingNameCandidate = voiceCustomerNamePendingConfirmationCandidate(message, session)

	state := normalizedDialogState(session.DialogState)
	if state.Pending != nil && state.Pending.PromptKey == pendingOfferedSlotDateTimeCorrection {
		return finalizeTurnPlan(plan, TurnRouteFastLane, "pending_date_time_confirmation", TurnCoverageComplete, session, services, staff)
	}
	if envelope.ExpectedInput == ExpectedInputCustomerNameConfirmation {
		if isAffirmativeOnly(message) || isNegativeNameConfirmation(message) || correctedCustomerNameCandidate(message, session) != "" {
			return finalizeTurnPlan(plan, TurnRouteFastLane, "pending_customer_name_confirmation", TurnCoverageComplete, session, services, staff)
		}
	}
	if salonIdentityReplyForMessage(message, session, cfg) != "" {
		return finalizeTurnPlan(plan, TurnRouteAnswerLane, "salon_identity", TurnCoverageComplete, session, services, staff)
	}
	if _, ok := offeredSlotRejectionForMessage(message, session, timezoneLocation(timezoneFromConfig(cfg))); ok {
		return finalizeTurnPlan(plan, TurnRouteFastLane, "offered_slot_rejection", TurnCoverageComplete, session, services, staff)
	}
	if reply, _ := customerNameSlotRepairReply(message, session, services, aliases, categoryAliases, cfg); reply != "" {
		return finalizeTurnPlan(plan, TurnRouteRecoveryLane, "customer_name_repair", TurnCoverageComplete, session, services, staff)
	}
	if repairReplyForMessage(message, session, cfg) != "" {
		return finalizeTurnPlan(plan, TurnRouteRecoveryLane, "deterministic_repair", TurnCoverageComplete, session, services, staff)
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
	answer := routeNonBookingAnswer(message, session, answerCtx, cfg, s.now)
	if isServiceInquiry(message, plan.ServiceUnderstanding) {
		answer = routeServiceInquiryAnswer(message, session, plan.ServiceUnderstanding, answerCtx)
	}
	if answer.Handled && answer.Source != answerSourceBookingRedirect && len(signals) == 0 {
		plan.Understanding = deterministicAnswerUnderstanding(answer)
		return finalizeTurnPlan(plan, TurnRouteAnswerLane, "structured_answer", TurnCoverageComplete, session, services, staff)
	}

	deterministic := deterministicConversationAct(session, message, services, aliases, categoryAliases)
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
	if shouldComplaintHandoff(message) || shouldHandoff(message) {
		return finalizeTurnPlan(plan, TurnRouteActionLane, "owner_handoff", TurnCoverageComplete, session, services, staff)
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

	if len(signals) == 1 && signalMatchesExpectedInput(signals[0], envelope.ExpectedInput, session) {
		plan.Understanding = deterministicGoalUnderstanding("book_appointment", "expected_input_resolved")
		return finalizeTurnPlan(plan, TurnRouteFastLane, "expected_input_resolved", TurnCoverageComplete, session, services, staff)
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
	if !plan.PartySignal.IsParty && !isServiceInquiry(envelope.Message, plan.ServiceUnderstanding) &&
		plan.ServiceUnderstanding.Status != serviceUnderstandingStatusUnknown &&
		(hasOperationalBookingProgress(envelope.Session) || hasBookingVerbSignal(envelope.Message)) {
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
