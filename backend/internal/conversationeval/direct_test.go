package conversationeval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestRunDirectExecutesRecognitionReplyAndActualAIReviewWithinBudget(t *testing.T) {
	corpus := oneScenarioDirectCorpus()
	model := &fakeDirectModel{}
	checkpoint := &memoryCheckpoint{}
	preflight := &RuntimePreflightEvidence{
		SalonID: "salon_runtime", CheckedAt: "2026-07-17T00:00:00Z", GuidanceServiceCount: 2,
		RecommendationReadyCount: 2, ServiceGuidanceStatus: string(conversation.ServiceGuidanceCapabilityRecommendationReady),
		BookingServiceCount: 0, ProviderSynced: false, BookingReady: false, Passed: true,
	}
	report, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, checkpoint, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 3, RequestTimeout: time.Second, RuntimePreflight: preflight,
	})
	if err != nil {
		t.Fatalf("RunDirect: %v", err)
	}
	if report.ModelCallCount != 3 || model.interpretCalls != 1 || model.replyCalls != 1 || model.reviewCalls != 1 {
		t.Fatalf("model calls report=%d recognize=%d reply=%d review=%d", report.ModelCallCount, model.interpretCalls, model.replyCalls, model.reviewCalls)
	}
	if report.PassedCount != 1 || report.FailedCount != 0 || report.ReviewPassedCount != 1 || report.ReviewFailedCount != 0 || len(report.Results) != 1 || report.Results[0].FinalReply == "" || len(report.ReviewRounds) != 1 {
		t.Fatalf("direct report = %#v", report)
	}
	if report.InFlightModelCall != nil || checkpoint.saves < 7 {
		t.Fatalf("checkpoint fence = in_flight:%#v saves:%d", report.InFlightModelCall, checkpoint.saves)
	}
	if report.ContextSource != "isolated_fixture" || report.RuntimeReadinessVerified || report.RuntimePreflight == nil || !report.RuntimePreflight.Passed || report.RuntimePreflight.BookingReady {
		t.Fatalf("direct evidence provenance = %#v", report)
	}
}

func TestRunDirectRejectsFailedRuntimePreflightBeforePaidCalls(t *testing.T) {
	model := &fakeDirectModel{}
	_, err := RunDirect(context.Background(), oneScenarioDirectCorpus(), model, fakeBackendTurnRunner{}, &memoryCheckpoint{}, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 3, RequestTimeout: time.Second,
		RuntimePreflight: &RuntimePreflightEvidence{SalonID: "salon_runtime", ServiceGuidanceStatus: string(conversation.ServiceGuidanceCapabilityCatalogUnavailable)},
	})
	if err == nil || !strings.Contains(err.Error(), "runtime guidance preflight did not pass") {
		t.Fatalf("failed preflight error = %v", err)
	}
	if model.interpretCalls != 0 || model.replyCalls != 0 || model.reviewCalls != 0 {
		t.Fatalf("failed preflight consumed model calls: %#v", model)
	}
}

func TestRunDirectCountsLowScoringActualReviewAsFailed(t *testing.T) {
	corpus := oneScenarioDirectCorpus()
	model := &fakeDirectModel{reviewFails: true}
	report, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, &memoryCheckpoint{}, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 3, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RunDirect: %v", err)
	}
	if report.ReviewPassedCount != 0 || report.ReviewFailedCount != 1 || report.FailedCount != 0 {
		t.Fatalf("review failure summary = %#v", report)
	}
}

func TestRunDirectReviewsSelectedScenariosInBatchesOfAtMostFive(t *testing.T) {
	corpus := GeneratePilotCorpus()
	corpus.Scenarios = append([]Scenario(nil), corpus.Scenarios[:6]...)
	model := &fakeDirectModel{}
	report, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, &memoryCheckpoint{}, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 14, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RunDirect selected subset: %v", err)
	}
	if len(report.ReviewRounds) != 2 || model.reviewCalls != 2 || len(model.reviewBatchSizes) != 2 || model.reviewBatchSizes[0] != 5 || model.reviewBatchSizes[1] != 1 {
		t.Fatalf("review batching report=%#v model=%#v", report.ReviewRounds, model)
	}
}

func TestValidateDirectReviewRejectsPassedFlagBelowScoreGate(t *testing.T) {
	input := DirectReviewInput{Round: 1, Results: []ScenarioEvaluationResult{{ScenarioID: "pilot-001"}}}
	review := DirectReviewRound{
		Round: 1, ScenarioIDs: []string{"pilot-001"}, Passed: true, Summary: "Looks acceptable.",
		Scores: DirectReviewScores{Naturalness: 5, CatalogGrounding: 5, OneQuestionRule: 5, BookingSafety: 5, CallerUsefulness: 3},
	}
	if err := validateDirectReview(input, review); err == nil {
		t.Fatal("expected inconsistent review pass flag to fail validation")
	}
}

func TestValidateDirectReplyRejectsDynamicInternalIdentifiers(t *testing.T) {
	problems := validateDirectReply("Okay, I added Spa Pedicure for guest_2. What day works?", []string{"guest_2", "svc_spa_pedi"})
	if len(problems) != 1 || !strings.Contains(problems[0], "guest_2") {
		t.Fatalf("internal identifier problems = %#v", problems)
	}
	if problems := validateDirectReply("Okay, I added Spa Pedicure for the second guest. What day works?", []string{"guest_2", "svc_spa_pedi"}); len(problems) != 0 {
		t.Fatalf("human guest wording rejected: %#v", problems)
	}
	if problems := validateDirectReply("What do you currently have on your nails?", []string{"current"}); len(problems) != 0 {
		t.Fatalf("natural word containing an identifier prefix was rejected: %#v", problems)
	}
	if problems := validateDirectReply("The internal scope is current.", []string{"current"}); len(problems) != 1 {
		t.Fatalf("delimited internal identifier was not rejected: %#v", problems)
	}
}

func TestRunDirectResumeDoesNotRepeatCompletedPaidCalls(t *testing.T) {
	corpus := oneScenarioDirectCorpus()
	model := &fakeDirectModel{}
	checkpoint := &memoryCheckpoint{}
	opts := DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 3, RequestTimeout: time.Second,
		RuntimePreflight: &RuntimePreflightEvidence{SalonID: "salon_runtime", CheckedAt: "first", GuidanceServiceCount: 2, RecommendationReadyCount: 2, ServiceGuidanceStatus: "recommendation_ready", Passed: true},
	}
	first, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, checkpoint, opts)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	opts.RuntimePreflight = &RuntimePreflightEvidence{SalonID: "salon_runtime", CheckedAt: "second", GuidanceServiceCount: 2, RecommendationReadyCount: 2, ServiceGuidanceStatus: "recommendation_ready", BookingServiceCount: 2, ProviderSynced: true, BookingReady: true, Passed: true}
	second, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, checkpoint, opts)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if first.ModelCallCount != second.ModelCallCount || model.interpretCalls != 1 || model.replyCalls != 1 || model.reviewCalls != 1 || !second.Results[0].Resumed {
		t.Fatalf("resume repeated paid work: first=%d second=%d model=%#v result=%#v", first.ModelCallCount, second.ModelCallCount, model, second.Results[0])
	}
	if second.RuntimePreflight == nil || second.RuntimePreflight.CheckedAt != "second" || !second.RuntimePreflight.BookingReady {
		t.Fatalf("resume did not retain latest zero-call preflight: %#v", second.RuntimePreflight)
	}
}

func TestRunDirectStopsBeforeCallThatWouldExceedBudget(t *testing.T) {
	corpus := oneScenarioDirectCorpus()
	model := &fakeDirectModel{}
	report, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, &memoryCheckpoint{}, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 1, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("budget stop should be retained in report, got %v", err)
	}
	if report.ModelCallCount != 1 || model.interpretCalls != 1 || model.replyCalls != 0 || model.reviewCalls != 0 || report.NotRunCount != 1 || report.Results[0].Status != "not_run_budget_exhausted" {
		t.Fatalf("budget report=%#v model=%#v", report, model)
	}
}

func TestRunDirectHigherResumeBudgetContinuesPartialScenarioWithoutRepeatingRecognition(t *testing.T) {
	corpus := oneScenarioDirectCorpus()
	model := &fakeDirectModel{}
	checkpoint := &memoryCheckpoint{}
	if _, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, checkpoint, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 1, RequestTimeout: time.Second,
	}); err != nil {
		t.Fatalf("initial bounded run: %v", err)
	}
	report, err := RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, checkpoint, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 3, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("budget resume: %v", err)
	}
	if report.PassedCount != 1 || report.ModelCallCount != 3 || model.interpretCalls != 1 || model.replyCalls != 1 || model.reviewCalls != 1 {
		t.Fatalf("partial resume repeated or skipped work: report=%#v model=%#v", report, model)
	}
}

func TestRunDirectFailsClosedOnUncertainInFlightCheckpoint(t *testing.T) {
	corpus := oneScenarioDirectCorpus()
	model := &fakeDirectModel{}
	runKey, err := DirectRunKey(corpus, "salon_config_owner", "fake-model")
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := &memoryCheckpoint{found: true, report: EvaluationReport{
		RunKey: runKey, ModelCallCount: 1, InFlightModelCall: &InFlightModelCall{Stage: "recognition", ScenarioID: "pilot-test", Attempt: 1},
	}}
	_, err = RunDirect(context.Background(), corpus, model, fakeBackendTurnRunner{}, checkpoint, DirectRunOptions{
		SalonID: "salon_config_owner", ModelCallBudget: 3, RequestTimeout: time.Second,
	})
	if !errors.Is(err, ErrUncertainModelCall) || model.interpretCalls != 0 {
		t.Fatalf("uncertain checkpoint err=%v model=%#v", err, model)
	}
}

func TestFixtureBackendRunnerProducesCustomerReplyWithoutPOSWritesAcrossPilot(t *testing.T) {
	corpus := GeneratePilotCorpus()
	runner := FixtureBackendRunner{}
	for _, scenario := range corpus.Scenarios {
		result, err := runner.Run(context.Background(), "salon_config_owner", corpus, scenario, modelReplyFromExpected(scenario.Expected), fixtureReplyGenerator{})
		if err != nil && result.SafeReply == "" {
			t.Fatalf("scenario %s backend error=%v with no safe reply", scenario.ID, err)
		}
		if result.SafeReply == "" || result.FinalReply == "" || result.Evidence.IntentAfter == "" {
			t.Fatalf("scenario %s backend result=%#v err=%v", scenario.ID, result, err)
		}
		for _, attempt := range result.WouldCallTools {
			if attempt.SideEffect && !attempt.Blocked {
				t.Fatalf("scenario %s allowed side effect: %#v", scenario.ID, attempt)
			}
		}
	}
}

func TestFixtureBackendRunnerPreservesPrimaryGuidanceWhenAuxiliaryConsultationIsInvalid(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-003")
	actual := voice.TurnModelReply{
		Goal: "consultation", GuidanceAction: conversation.GuidanceActionConsultation, Confidence: 0.98,
		Consultation: voice.ConsultationModelReply{
			DesiredOutcome: conversation.ConsultationOutcomeUnknown, BookingRequested: true, Confidence: 0,
			Mutations: []voice.ConsultationMutationModelReply{{
				Field: conversation.ConsultationNeedFieldDesiredOutcome, Operation: conversation.ConsultationNeedOperationSet,
				Values: []string{conversation.ConsultationOutcomeUnknown}, Confidence: 0.96,
			}},
		},
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99},
	}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend guidance turn: %v", err)
	}
	if result.Evidence.InterpreterOutcome != conversation.TurnInterpreterOutcomeAccepted || result.Evidence.IntentAfter != conversation.IntentConsultation ||
		!strings.Contains(strings.ToLower(result.FinalReply), "what result") {
		t.Fatalf("guidance result = %#v", result)
	}
}

func TestFixtureBackendRunnerRetainsAvailabilityDateAndSemanticTimeWindow(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-032")
	actual := voice.TurnModelReply{
		Goal: "book_appointment", Confidence: 0.98,
		Questions: []voice.QuestionModelReply{{
			Subject:        conversation.ConversationQuestionAvailability,
			TimePreference: voice.TimePreferenceModelReply{Direction: conversation.TimePreferenceAfter, Minutes: 13*60 + 30},
			Confidence:     0.98,
		}},
		Consultation: voice.ConsultationModelReply{BookingRequested: true, Confidence: 0},
		Safety:       voice.SafetyModelReply{Concern: false, Confidence: 0.99},
	}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend availability turn: %v", err)
	}
	if result.Evidence.InterpreterOutcome != conversation.TurnInterpreterOutcomeAccepted || result.Evidence.RequestedDateAfter == "" ||
		result.Evidence.TimePreferenceDirection != conversation.TimePreferenceAfter || result.Evidence.TimePreferenceMinutes != 13*60+30 {
		t.Fatalf("availability evidence = %#v reply=%q", result.Evidence, result.FinalReply)
	}
	if strings.Contains(result.FinalReply, "10:00 AM") || strings.Contains(result.FinalReply, "1:00 PM") || !strings.Contains(result.FinalReply, "3:00 PM") {
		t.Fatalf("time-window reply ignored semantic constraint: %q", result.FinalReply)
	}
}

func TestFixtureBackendRunnerNormalizesClockHourBeforeFilteringAvailability(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-032")
	actual := voice.TurnModelReply{
		Goal: "book_appointment", Confidence: 0.98,
		Questions: []voice.QuestionModelReply{{
			Subject:        conversation.ConversationQuestionAvailability,
			TimePreference: voice.TimePreferenceModelReply{Direction: conversation.TimePreferenceAfter, Minutes: 13},
			Confidence:     0.98,
		}},
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99},
	}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend availability turn: %v", err)
	}
	if result.Evidence.TimePreferenceMinutes != 13*60 || !sameInts(result.Evidence.OfferedSlotLocalMinutes, []int{15 * 60}) {
		t.Fatalf("availability evidence = %#v reply=%q", result.Evidence, result.FinalReply)
	}
	if strings.Contains(result.FinalReply, "10:00 AM") || strings.Contains(result.FinalReply, "1:00 PM") || !strings.Contains(result.FinalReply, "3:00 PM") {
		t.Fatalf("clock-hour protocol value was not normalized before filtering: %q", result.FinalReply)
	}
	if problems := validateBackendResult(scenario, result); len(problems) != 0 {
		t.Fatalf("valid normalized availability result problems = %#v", problems)
	}
}

func TestValidateBackendResultRejectsOfferedSlotOutsideTimePreference(t *testing.T) {
	result := BackendTurnResult{Evidence: BackendEvidence{
		TimePreferenceDirection: conversation.TimePreferenceAfter,
		TimePreferenceMinutes:   14 * 60,
		TimePreferenceTimezone:  "America/Chicago",
		OfferedSlotLocalMinutes: []int{13 * 60, 15 * 60},
	}}
	problems := validateBackendResult(Scenario{}, result)
	if len(problems) != 1 || !strings.Contains(problems[0], "outside after constraint") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestValidateBackendResultAcceptsEachTimePreferenceDirection(t *testing.T) {
	tests := []BackendEvidence{
		{TimePreferenceDirection: conversation.TimePreferenceAfter, TimePreferenceMinutes: 13 * 60, OfferedSlotLocalMinutes: []int{13*60 + 30}},
		{TimePreferenceDirection: conversation.TimePreferenceBefore, TimePreferenceMinutes: 13 * 60, OfferedSlotLocalMinutes: []int{12*60 + 30}},
		{TimePreferenceDirection: conversation.TimePreferenceExact, TimePreferenceMinutes: 30, OfferedSlotLocalMinutes: []int{30}},
	}
	for _, evidence := range tests {
		if problems := validateBackendResult(Scenario{}, BackendTurnResult{Evidence: evidence}); len(problems) != 0 {
			t.Fatalf("evidence=%#v problems=%#v", evidence, problems)
		}
	}
}

func sameInts(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestFixtureBackendRunnerAnswersSalonQuestionWithoutGenericGuidanceMenu(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByBaseID(t, corpus, "guidance_salon_question-base-001")
	actual := modelReplyFromExpected(scenario.Expected)
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend hours turn: %v", err)
	}
	lower := strings.ToLower(result.FinalReply)
	if !strings.Contains(lower, "hours") || strings.Contains(lower, "book an appointment") || strings.Contains(lower, "service menu") || strings.Contains(lower, "owner help") {
		t.Fatalf("hours reply = %q evidence=%#v", result.FinalReply, result.Evidence)
	}
}

func TestFixtureBackendRunnerKeepsStaffAlternativeActWhenAuxiliaryConsultationIsInvalid(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-038")
	actual := voice.TurnModelReply{
		Goal: "book_appointment", Confidence: 0.98,
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActSet, Entity: conversation.ConversationEntityStaff,
			Subject: "alternative", Confidence: 0.98,
		}},
		Consultation: voice.ConsultationModelReply{CurrentSystem: conversation.ConsultationSystemUnknown, BookingRequested: true, Confidence: 0},
		Safety:       voice.SafetyModelReply{Concern: false, Confidence: 0.99},
	}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend staff-alternative turn: %v", err)
	}
	if result.Evidence.InterpreterOutcome != conversation.TurnInterpreterOutcomeAccepted || result.Evidence.StaffBefore != "staff_mai" || result.Evidence.StaffAfter != "staff_linh" || result.Evidence.HandoffRequested {
		t.Fatalf("staff alternative evidence = %#v reply=%q", result.Evidence, result.FinalReply)
	}
	if !strings.Contains(result.FinalReply, "changed the technician to Linh") {
		t.Fatalf("staff alternative mutation was not acknowledged: %q", result.FinalReply)
	}
}

func TestFixtureBackendRunnerPrioritizesStaffMutationOverBareConsultationGoal(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-038")
	actual := voice.TurnModelReply{
		Goal: "consultation", Confidence: 0.94,
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActSet, Entity: conversation.ConversationEntityStaff,
			Subject: "alternative", SourceIDs: []string{"staff_mai"}, Confidence: 0.94,
		}},
		Consultation: voice.ConsultationModelReply{},
		Safety:       voice.SafetyModelReply{Concern: false, Confidence: 0.99},
	}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend staff-alternative turn: %v", err)
	}
	if result.Evidence.IntentAfter != conversation.IntentBooking || result.Evidence.StaffBefore != "staff_mai" || result.Evidence.StaffAfter != "staff_linh" {
		t.Fatalf("staff alternative evidence = %#v reply=%q", result.Evidence, result.FinalReply)
	}
	if !strings.Contains(result.FinalReply, "changed the technician to Linh") || strings.Contains(strings.ToLower(result.FinalReply), "currently use") {
		t.Fatalf("staff mutation did not own final reply: %q", result.FinalReply)
	}
}

func TestFixtureBackendRunnerKeepsPartyGuestActWhenAuxiliaryConsultationIsInvalid(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-019")
	actual := modelReplyFromExpected(scenario.Expected)
	actual.Goal = "book_appointment"
	actual.Consultation = voice.ConsultationModelReply{BookingRequested: true, Confidence: 0}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, fixtureReplyGenerator{})
	if err != nil {
		t.Fatalf("backend party guest turn: %v", err)
	}
	lower := strings.ToLower(result.FinalReply)
	if result.Evidence.InterpreterOutcome != conversation.TurnInterpreterOutcomeAccepted || result.Evidence.HandoffRequested ||
		strings.Contains(lower, "which guest") || strings.Contains(lower, "which person") || strings.Contains(lower, "guest_2") {
		t.Fatalf("party guest act result = evidence:%#v reply:%q", result.Evidence, result.FinalReply)
	}
}

func TestFixtureBackendRunnerDoesNotRewriteTerminalOwnerRequestAsLiveTransfer(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := directScenarioByID(t, corpus, "pilot-015")
	actual := modelReplyFromExpected(scenario.Expected)
	generator := &unsafeTransferReplyGenerator{}
	result, err := (FixtureBackendRunner{}).Run(context.Background(), "salon_config_owner", corpus, scenario, actual, generator)
	if err != nil {
		t.Fatalf("backend owner handoff: %v", err)
	}
	lower := strings.ToLower(result.FinalReply)
	if generator.calls != 0 || !result.Evidence.HandoffRequested || result.Evidence.HandoffMode != "owner_request" ||
		strings.Contains(lower, "please hold") || strings.Contains(lower, "connect you now") {
		t.Fatalf("terminal owner request = calls:%d evidence:%#v reply:%q", generator.calls, result.Evidence, result.FinalReply)
	}
}

func directScenarioByID(t *testing.T, corpus Corpus, scenarioID string) Scenario {
	t.Helper()
	for _, scenario := range corpus.Scenarios {
		if scenario.ID == scenarioID {
			return scenario
		}
	}
	t.Fatalf("scenario %s not found", scenarioID)
	return Scenario{}
}

func directScenarioByBaseID(t *testing.T, corpus Corpus, baseCaseID string) Scenario {
	t.Helper()
	for _, scenario := range corpus.Scenarios {
		if scenario.Provenance.BaseCaseID == baseCaseID {
			return scenario
		}
	}
	t.Fatalf("scenario base %s not found", baseCaseID)
	return Scenario{}
}

func oneScenarioDirectCorpus() Corpus {
	full := GeneratePilotCorpus()
	full.Scenarios = append([]Scenario(nil), full.Scenarios[:1]...)
	full.ExpectedScenarioCount = 1
	full.ExpectedReviewRounds = 1
	return full
}

func modelReplyFromExpected(expected ExpectedResult) voice.TurnModelReply {
	reply := voice.TurnModelReply{
		Goal: expected.Goal, GuidanceAction: expected.GuidanceAction, GuidanceCatalogMode: expected.GuidanceCatalogMode,
		GuidanceQuestionSubject: expected.GuidanceQuestionSubject, GuidancePartySize: expected.GuidancePartySize,
		Confidence: 0.99,
		Consultation: voice.ConsultationModelReply{
			CurrentSystem: expected.Consultation.CurrentSystem, DesiredOutcome: expected.Consultation.DesiredOutcome,
			LengthChange: expected.Consultation.LengthChange, Priorities: append([]string(nil), expected.Consultation.Priorities...),
			DesiredFinishes: append([]string(nil), expected.Consultation.Finishes...), Confidence: 0.99,
		},
		Safety: voice.SafetyModelReply{Concern: expected.Safety.Concern, Category: expected.Safety.Category, Confidence: 0.99},
	}
	for _, expectedAct := range expected.RequiredActs {
		reply.Acts = append(reply.Acts, voice.ActModelReply{
			Kind: expectedAct.Kind, Entity: expectedAct.Entity, SourceIDs: append([]string(nil), expectedAct.SourceIDs...),
			TargetIDs: append([]string(nil), expectedAct.TargetIDs...), GuestScope: expectedAct.GuestScope,
			GuestRef: expectedAct.GuestRef, Subject: expectedAct.Subject, Confidence: 0.99,
		})
	}
	for _, expectedQuestion := range expected.RequiredQuestions {
		reply.Questions = append(reply.Questions, voice.QuestionModelReply{
			Subject: expectedQuestion.Subject, Mode: expectedQuestion.Mode,
			TimePreference: voice.TimePreferenceModelReply{Minutes: -1}, Confidence: 0.99,
		})
	}
	if expected.CurrentBookingSummary {
		reply.Questions = append(reply.Questions, voice.QuestionModelReply{
			Subject: conversation.ConversationQuestionCurrentBooking, Mode: conversation.ConversationQuestionModeList,
			TimePreference: voice.TimePreferenceModelReply{Minutes: -1}, Confidence: 0.99,
		})
	}
	if expected.AvailabilityIntent {
		reply.Questions = append(reply.Questions, voice.QuestionModelReply{
			Subject:        conversation.ConversationQuestionAvailability,
			TimePreference: voice.TimePreferenceModelReply{Minutes: -1}, Confidence: 0.99,
		})
	}
	return reply
}

type fakeDirectModel struct {
	interpretCalls     int
	replyCalls         int
	reviewCalls        int
	reviewFails        bool
	reviewBatchSizes   []int
	reviewMultiTurn    []bool
	reviewJourneySizes []int
}

func (f *fakeDirectModel) Identity(context.Context, string) (string, error) {
	return "fake-model", nil
}

func (f *fakeDirectModel) InterpretTurn(_ context.Context, _ string, request voice.SemanticEvaluationRequest) (voice.TurnModelReply, ModelUsage, error) {
	f.interpretCalls++
	return voice.TurnModelReply{
		Goal:           conversation.GuidanceGoalForAction(conversation.GuidanceActionServiceCatalog),
		GuidanceAction: conversation.GuidanceActionServiceCatalog, GuidanceCatalogMode: conversation.ConversationQuestionModeList,
		GuidanceQuestionSubject: conversation.ConversationQuestionCatalog, Confidence: 0.98,
		Safety: voice.SafetyModelReply{Concern: false, Confidence: 0.99},
	}, ModelUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, nil
}

func (f *fakeDirectModel) GenerateReply(_ context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, ModelUsage, error) {
	f.replyCalls++
	return conversation.ReplyGenerationResult{Message: request.SafeReply, Confidence: 0.98}, ModelUsage{InputTokens: 8, OutputTokens: 4, TotalTokens: 12}, nil
}

func (f *fakeDirectModel) GenerateConsultationQuestion(_ context.Context, request conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, ModelUsage, error) {
	f.replyCalls++
	return conversation.ReplyGenerationResult{Message: "Which result would you like for your nails?", Confidence: 0.98}, ModelUsage{InputTokens: 8, OutputTokens: 4, TotalTokens: 12}, nil
}

func (f *fakeDirectModel) ReviewReplies(_ context.Context, _ string, input DirectReviewInput) (DirectReviewRound, ModelUsage, error) {
	f.reviewCalls++
	f.reviewBatchSizes = append(f.reviewBatchSizes, len(input.Results))
	f.reviewMultiTurn = append(f.reviewMultiTurn, input.MultiTurn)
	f.reviewJourneySizes = append(f.reviewJourneySizes, len(input.JourneyResults))
	ids := make([]string, 0, len(input.Results))
	for _, result := range input.Results {
		ids = append(ids, result.ScenarioID)
	}
	review := DirectReviewRound{
		Round: input.Round, ScenarioIDs: ids, Passed: true, Summary: "The retained reply is natural, grounded, safe, and useful.",
		Scores: DirectReviewScores{Naturalness: 5, CatalogGrounding: 5, OneQuestionRule: 5, BookingSafety: 5, CallerUsefulness: 5},
	}
	if f.reviewFails {
		review.Passed = false
		review.Scores.CallerUsefulness = 2
		review.Summary = "The reply is not useful enough for the caller."
	}
	return review, ModelUsage{InputTokens: 12, OutputTokens: 6, TotalTokens: 18}, nil
}

type fakeBackendTurnRunner struct{}

func (fakeBackendTurnRunner) Run(ctx context.Context, salonID string, _ Corpus, scenario Scenario, _ voice.TurnModelReply, replies conversation.ReplyGenerator) (BackendTurnResult, error) {
	safe := "We offer several nail services. Which type would you like to hear about?"
	reply, err := replies.GenerateReply(ctx, conversation.ReplyGenerationRequest{
		SalonID: salonID, SessionID: "evaluation", Channel: scenario.Request.Channel,
		CustomerMessage: scenario.Request.CustomerMessage, SafeReply: safe,
		NextRequiredField: "service", ReplyPolicy: conversation.ReplyPolicyStyleOnly,
	})
	if err != nil {
		return BackendTurnResult{SafeReply: safe, FinalReply: safe}, err
	}
	return BackendTurnResult{
		SafeReply: safe, FinalReply: reply.Message, NextExpectedInput: "service",
		Evidence: BackendEvidence{InterpreterOutcome: conversation.TurnInterpreterOutcomeAccepted},
	}, nil
}

type fixtureReplyGenerator struct{}

func (fixtureReplyGenerator) GenerateReply(_ context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	return conversation.ReplyGenerationResult{Message: request.SafeReply, Confidence: 1}, nil
}

func (fixtureReplyGenerator) GenerateConsultationQuestion(_ context.Context, _ conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, error) {
	return conversation.ReplyGenerationResult{Message: "What result would you like for your nails?", Confidence: 1}, nil
}

type unsafeTransferReplyGenerator struct {
	calls int
}

func (g *unsafeTransferReplyGenerator) GenerateReply(context.Context, conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	g.calls++
	return conversation.ReplyGenerationResult{Message: "Please hold while I connect you now.", Confidence: 1}, nil
}

func (g *unsafeTransferReplyGenerator) GenerateConsultationQuestion(context.Context, conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, error) {
	g.calls++
	return conversation.ReplyGenerationResult{Message: "Please hold while I connect you now?", Confidence: 1}, nil
}

type memoryCheckpoint struct {
	report EvaluationReport
	found  bool
	saves  int
}

func (m *memoryCheckpoint) Load(context.Context) (EvaluationReport, bool, error) {
	return cloneEvaluationReport(m.report), m.found, nil
}

func (m *memoryCheckpoint) Save(_ context.Context, report EvaluationReport) error {
	m.report = cloneEvaluationReport(report)
	m.found = true
	m.saves++
	return nil
}

func cloneEvaluationReport(report EvaluationReport) EvaluationReport {
	raw, _ := json.Marshal(report)
	var cloned EvaluationReport
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}
