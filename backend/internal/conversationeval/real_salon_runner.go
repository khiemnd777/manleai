package conversationeval

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

var realSalonInternalIdentifier = regexp.MustCompile(`(?i)\b(?:svc|staff|cat|evaluation-session|dialog_state|next_required_field|turn_route)_[a-z0-9_-]+\b`)

func ReviewRealSalonStructure(corpus RealSalonCorpus) RealSalonReport {
	report := RealSalonReport{
		SchemaVersion: corpus.SchemaVersion, ContractVersion: corpus.ContractVersion,
		EvaluationContract: RealSalonEvaluationContract, ReviewContract: RealSalonReviewContract + "+" + DirectReviewContractVersion,
		Mode:          "structural_validation_without_runtime_or_model_execution",
		OverallStatus: "structural_validation_failed", JourneyCount: len(corpus.Journeys),
		Results: make([]RealSalonJourneyResult, 0, len(corpus.Journeys)),
	}
	problems := ValidateRealSalonCorpus(corpus)
	if len(problems) != 0 {
		report.Failed = len(corpus.Journeys)
		report.Results = append(report.Results, RealSalonJourneyResult{JourneyID: "corpus", Status: RealSalonStatusFailed, Errors: problems})
		return report
	}
	runKey, err := RealSalonRunKey(corpus, report.Mode, "not_run")
	if err != nil {
		report.Failed = len(corpus.Journeys)
		report.Results = append(report.Results, RealSalonJourneyResult{JourneyID: "corpus", Status: RealSalonStatusFailed, Errors: []string{err.Error()}})
		return report
	}
	report.RunKey = runKey
	report.StructuralValidated = len(corpus.Journeys)
	report.NotRun = len(corpus.Journeys)
	report.OverallStatus = "structural_validated_model_not_run"
	for _, journey := range corpus.Journeys {
		report.Results = append(report.Results, RealSalonJourneyResult{
			JourneyID: journey.ID, Family: journey.Family, Status: RealSalonStatusStructuralValidated,
		})
	}
	// Passed deliberately remains false. Structural validity is evidence that
	// the suite is well formed, never evidence that a model or receptionist
	// response passed.
	return report
}

func RunRealSalonDeterministic(ctx context.Context, corpus RealSalonCorpus) RealSalonReport {
	started := time.Now().UTC()
	report := ReviewRealSalonStructure(corpus)
	report.Mode = "production_conversation_service_scripted_semantics_no_model_no_side_effects"
	report.StartedAt = started.Format(time.RFC3339Nano)
	if report.StructuralValidated != len(corpus.Journeys) {
		report.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return report
	}
	runKey, err := RealSalonRunKey(corpus, report.Mode, "scripted_semantic_fixture")
	if err != nil {
		report.OverallStatus = RealSalonStatusFailed
		report.Failed = len(corpus.Journeys)
		report.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return report
	}
	report.RunKey = runKey
	report.Results = report.Results[:0]
	report.NotRun = 0
	report.RuntimeExecuted = 0
	report.Failed = 0
	for _, journey := range corpus.Journeys {
		result := runRealSalonJourney(ctx, corpus, journey, false, nil)
		report.Results = append(report.Results, result)
		if result.Status == RealSalonStatusRuntimeExecuted {
			report.RuntimeExecuted++
		} else {
			report.Failed++
		}
	}
	if report.Failed == 0 {
		report.OverallStatus = "runtime_executed_model_not_run"
	} else {
		report.OverallStatus = RealSalonStatusFailed
	}
	// A deterministic scripted-semantic execution proves production state and
	// side-effect behavior only. It cannot make the overall model evaluation pass.
	report.Passed = false
	report.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return report
}

type realSalonLiveExecutor interface {
	Interpret(ctx context.Context, journeyID string, turn int, request conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, int, ModelUsage, error)
	Replies(journeyID string, turn int) conversation.ReplyGenerator
	ReplyEvidence() (int, ModelUsage, []error)
}

func runRealSalonJourney(ctx context.Context, corpus RealSalonCorpus, journey RealSalonJourney, live bool, executor realSalonLiveExecutor) RealSalonJourneyResult {
	result := RealSalonJourneyResult{JourneyID: journey.ID, Family: journey.Family, Status: RealSalonStatusFailed}
	fixture := corpus.CatalogFixtures[journey.CatalogFixture]
	forbiddenIdentifiers := realSalonFixtureIdentifiers(fixture)
	baseRequest := realSalonInitialRequest(journey)
	store := newFixtureConversationStore("real-salon-evaluation", journey.ID, baseRequest, fixture)
	applyRealSalonInitialState(store, journey.InitialState)
	if journey.InitialState.GuidanceCatalogOff {
		store.services = nil
		store.serviceAliases = nil
		store.categoryAliases = nil
	}
	tool := &fixtureBookingTool{services: store.services, staff: store.staff}
	service := conversation.NewService(store, tool)
	for index, turn := range journey.Turns {
		before := store.session
		toolOffset := len(tool.attempts)
		semanticInterpreterInvoked := false
		replyGeneratorInvoked := false
		var interpreter conversation.TurnInterpreter = &realSalonScriptedInterpreter{
			turn: voice.TurnUnderstandingFromModelReply(turn.ModelFixture), invoked: &semanticInterpreterInvoked,
		}
		if live {
			interpreter = &realSalonJourneyInterpreter{executor: executor, journeyID: journey.ID, turn: index + 1, invoked: &semanticInterpreterInvoked}
			service.SetReplyGenerator(executor.Replies(journey.ID, index+1))
		} else {
			service.SetReplyGenerator(&realSalonScriptedReplyGenerator{invoked: &replyGeneratorInvoked})
		}
		service.SetTurnInterpreter(interpreter)
		updated, err := service.Message(ctx, "real-salon-evaluation", "evaluation-owner", store.session.ID, conversation.MessageRequest{
			Message: turn.CustomerMessage, EventKey: fmt.Sprintf("real-salon:%s:%03d", journey.ID, index+1),
		})
		after := store.session
		if updated != nil {
			after = *updated
		}
		turnResult := RealSalonTurnResult{
			Turn: index + 1, CustomerMessage: turn.CustomerMessage, AIReply: lastAIMessage(after.Transcript),
			IntentBefore: before.Intent, IntentAfter: after.Intent,
			PhaseBefore: before.DialogState.Phase, PhaseAfter: after.DialogState.Phase,
			SelectedServiceIDs: fixtureSessionServiceIDs(after),
			WouldCallTools:     append([]ToolAttempt(nil), tool.attempts[toolOffset:]...),
		}
		turnResult.SemanticInterpreterInvoked = semanticInterpreterInvoked
		turnResult.ReplyGeneratorInvoked = replyGeneratorInvoked
		turnResult.NextExpectedInput = firstMetadataString(store.lastTurn.AIMetadata, "next_required_field")
		if turnResult.NextExpectedInput == "" {
			turnResult.NextExpectedInput = firstMetadataString(store.lastTurn.CustomerMetadata, "next_required_field")
		}
		turnResult.ProviderBookingIDPresent = strings.TrimSpace(after.AppointmentID) != "" && strings.TrimSpace(after.BookingAttemptID) != ""
		turnResult.BookingConfirmed = after.Outcome == conversation.OutcomeBookingConfirmed && turnResult.ProviderBookingIDPresent
		turnResult.HandoffRequested = after.Status == conversation.StatusHandoff || after.Outcome == conversation.OutcomeHandoffRequested || after.Outcome == conversation.OutcomeBookingFallbackPending
		if err != nil {
			turnResult.Errors = append(turnResult.Errors, "production conversation service: "+err.Error())
		}
		turnResult.Errors = append(turnResult.Errors, validateRealSalonTurn(turn.Expected, before, after, turnResult, forbiddenIdentifiers)...)
		if live && executor != nil {
			calls, usage, callErrors := executor.ReplyEvidence()
			turnResult.ModelCalls += calls
			turnResult.Usage.Add(usage)
			for _, callErr := range callErrors {
				turnResult.Errors = append(turnResult.Errors, "reply model: "+callErr.Error())
			}
		}
		result.ModelCalls += turnResult.ModelCalls
		result.Usage.Add(turnResult.Usage)
		result.Errors = append(result.Errors, turnResult.Errors...)
		result.Turns = append(result.Turns, turnResult)
	}
	result.ModelExecuted = live && result.ModelCalls > 0
	if live && !result.ModelExecuted {
		result.Errors = append(result.Errors, "live canary completed without invoking the configured model")
	}
	if len(result.Errors) == 0 {
		if live {
			result.Status = RealSalonStatusModelExecuted
		} else {
			result.Status = RealSalonStatusRuntimeExecuted
		}
	}
	return result
}

type realSalonJourneyInterpreter struct {
	executor  realSalonLiveExecutor
	journeyID string
	turn      int
	invoked   *bool
}

func (i *realSalonJourneyInterpreter) InterpretTurn(ctx context.Context, request conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, error) {
	if i.invoked != nil {
		*i.invoked = true
	}
	understanding, _, _, err := i.executor.Interpret(ctx, i.journeyID, i.turn, request)
	return understanding, err
}

type realSalonScriptedInterpreter struct {
	turn    conversation.TurnUnderstanding
	invoked *bool
}

func (i *realSalonScriptedInterpreter) InterpretTurn(context.Context, conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, error) {
	if i.invoked != nil {
		*i.invoked = true
	}
	return i.turn, nil
}

func realSalonInitialRequest(journey RealSalonJourney) voice.SemanticEvaluationRequest {
	state := journey.InitialState
	phase := strings.TrimSpace(state.Phase)
	if !conversation.IsDialogPhase(phase) {
		phase = conversation.DialogPhaseOpen
	}
	action := strings.TrimSpace(state.BookingAction)
	if !conversation.IsBookingAction(action) {
		action = conversation.BookingActionBook
	}
	return voice.SemanticEvaluationRequest{
		ScenarioID: journey.ID, Channel: journey.Channel, CustomerMessage: "initial state",
		ExpectedInput: "caller_goal", SemanticContract: conversation.TurnSemanticContractFull,
		CurrentBookingStage: phase, BookingAction: action,
		CurrentDraft: conversation.ConversationDraftRef{
			ServiceIDs: append([]string(nil), state.ServiceIDs...), StaffID: state.StaffID,
			RequestedDate: state.RequestedDate, HasCustomerName: state.HasCustomerName,
			HasCustomerPhone: state.HasCustomerPhone, PartySize: state.PartySize,
		},
		Consultation: state.Consultation,
	}
}

func applyRealSalonInitialState(store *fixtureConversationStore, state RealSalonInitialState) {
	if store == nil {
		return
	}
	if strings.TrimSpace(state.Intent) != "" {
		store.session.Intent = state.Intent
	}
	if conversation.IsDialogPhase(state.Phase) {
		store.session.DialogState.Phase = state.Phase
	}
	if conversation.IsBookingAction(state.BookingAction) {
		store.session.BookingAction = state.BookingAction
	}
	if strings.TrimSpace(state.ProviderState) == "disabled" {
		store.cfg.AIEnabled = false
	}
}

func validateRealSalonTurn(expected RealSalonTurnExpectation, before conversation.Session, after conversation.Session, actual RealSalonTurnResult, forbiddenIdentifiers []string) []string {
	problems := make([]string, 0)
	if strings.TrimSpace(actual.AIReply) == "" {
		problems = append(problems, "AI reply is empty")
	}
	if contains(expected.ReplyObligations, "one_useful_question") && strings.Count(actual.AIReply, "?") > 1 {
		problems = append(problems, "AI reply asks more than one question")
	}
	if contains(expected.ForbiddenReplyBehaviors, "internal_state_leak") && realSalonInternalIdentifier.MatchString(actual.AIReply) {
		problems = append(problems, "AI reply exposes an internal identifier")
	}
	lowerReply := strings.ToLower(actual.AIReply)
	for _, identifier := range forbiddenIdentifiers {
		if containsDelimitedIdentifier(lowerReply, strings.ToLower(identifier)) {
			problems = append(problems, "AI reply exposes fixture identifier: "+identifier)
		}
	}
	if expected.NoBookingSideEffect {
		for _, attempt := range actual.WouldCallTools {
			if attempt.SideEffect && !attempt.Blocked {
				problems = append(problems, "booking side effect was not blocked")
			}
		}
		if actual.BookingConfirmed {
			problems = append(problems, "booking was confirmed in a no-side-effect evaluation")
		}
	}
	if actual.BookingConfirmed && !actual.ProviderBookingIDPresent {
		problems = append(problems, "booking was confirmed without provider booking evidence")
	}
	if expected.RequireHandoff && !actual.HandoffRequested {
		problems = append(problems, "required safety or owner handoff was not requested")
	}
	if actual.HandoffRequested && !expected.RequireHandoff && !expected.AllowHandoff {
		problems = append(problems, "unexpected safety or owner handoff")
	}
	if len(expected.AllowedIntentsAfter) > 0 && !contains(expected.AllowedIntentsAfter, after.Intent) {
		problems = append(problems, fmt.Sprintf("intent_after=%q is outside %v", after.Intent, expected.AllowedIntentsAfter))
	}
	if len(expected.AllowedPhasesAfter) > 0 && !contains(expected.AllowedPhasesAfter, after.DialogState.Phase) {
		problems = append(problems, fmt.Sprintf("phase_after=%q is outside %v", after.DialogState.Phase, expected.AllowedPhasesAfter))
	}
	if len(expected.AllowedNextInputs) > 0 && !contains(expected.AllowedNextInputs, actual.NextExpectedInput) {
		problems = append(problems, fmt.Sprintf("next_expected_input=%q is outside %v", actual.NextExpectedInput, expected.AllowedNextInputs))
	}
	for _, serviceID := range expected.RequiredSelectedServiceIDs {
		if !contains(actual.SelectedServiceIDs, serviceID) {
			problems = append(problems, "required selected service is missing: "+serviceID)
		}
	}
	for _, required := range expected.RequiredToolCalls {
		if !realSalonHasToolCall(actual.WouldCallTools, required) {
			problems = append(problems, "required tool call is missing: "+required)
		}
	}
	for _, attempt := range actual.WouldCallTools {
		if !contains(expected.AllowedToolCalls, attempt.Tool) {
			problems = append(problems, "unexpected tool call: "+attempt.Tool)
		}
	}
	for _, field := range expected.RetainFields {
		switch field {
		case "service":
			if before.ServiceID != "" && after.ServiceID != before.ServiceID {
				problems = append(problems, "known service was not retained")
			}
		case "staff":
			if before.StaffID != "" && after.StaffID != before.StaffID {
				problems = append(problems, "known staff preference was not retained")
			}
		case "date":
			if before.RequestedDate != "" && after.RequestedDate != before.RequestedDate {
				problems = append(problems, "known requested date was not retained")
			}
		case "customer":
			if before.CustomerName != "" && after.CustomerName != before.CustomerName {
				problems = append(problems, "known customer name was not retained")
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func realSalonHasToolCall(attempts []ToolAttempt, tool string) bool {
	for _, attempt := range attempts {
		if attempt.Tool == tool {
			return true
		}
	}
	return false
}

func realSalonFixtureIdentifiers(fixture CatalogFixture) []string {
	identifiers := make([]string, 0, len(fixture.Services)+len(fixture.Categories)+len(fixture.Staff))
	for _, service := range fixture.Services {
		identifiers = append(identifiers, service.ServiceID)
	}
	for _, category := range fixture.Categories {
		identifiers = append(identifiers, category.CategoryID)
	}
	for _, staff := range fixture.Staff {
		identifiers = append(identifiers, staff.StaffID)
	}
	return identifiers
}

// realSalonScriptedReplyGenerator keeps deterministic execution focused on
// production conversation state and side-effect rules. It is fixture-only and
// never participates in runtime caller interpretation or service selection.
type realSalonScriptedReplyGenerator struct {
	invoked *bool
}

func (g *realSalonScriptedReplyGenerator) GenerateReply(_ context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	if g.invoked != nil {
		*g.invoked = true
	}
	message := strings.TrimSpace(request.SafeReply)
	if message == "" {
		message = "What would you like help with next?"
	}
	return conversation.ReplyGenerationResult{Message: message, Confidence: 1, Reason: "deterministic evaluation fixture"}, nil
}

func (g *realSalonScriptedReplyGenerator) GenerateConsultationQuestion(_ context.Context, request conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, error) {
	if g.invoked != nil {
		*g.invoked = true
	}
	question := request.Question
	label := strings.ReplaceAll(strings.TrimSpace(question.Field), "_", " ")
	if label == "" {
		label = "service preference"
	}
	message := "What should the salon know about your " + label + "?"
	if len(question.Options) > 0 {
		message = "For your " + label + ", which listed option fits best: " + strings.Join(question.Options, ", ") + "?"
	}
	return conversation.ReplyGenerationResult{Message: message, Confidence: 1, Reason: "deterministic evaluation fixture"}, nil
}
