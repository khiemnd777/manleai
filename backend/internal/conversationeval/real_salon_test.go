package conversationeval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

func TestRealSalonCorpusArtifactIsAuthoredUniqueAndCurrent(t *testing.T) {
	corpus := loadRealSalonCorpusArtifact(t)
	if problems := ValidateRealSalonCorpus(corpus); len(problems) != 0 {
		t.Fatalf("artifact validation failed: %v", problems)
	}
	if !reflect.DeepEqual(corpus, DefaultRealSalonCorpus()) {
		t.Fatal("retained real-salon JSON is stale relative to the authored source")
	}
	canaries, turnCount := 0, 0
	for _, journey := range corpus.Journeys {
		if journey.LiveCanary {
			canaries++
		}
		if journey.Generated || journey.Scope == "single_turn" || len(journey.Turns) < RealSalonMinCustomerTurns {
			t.Fatalf("journey is generated or not multi-turn: %#v", journey)
		}
		turnCount += len(journey.Turns)
	}
	if canaries != RealSalonLiveCanaryJourneys || turnCount < RealSalonRequiredJourneys*RealSalonMinCustomerTurns {
		t.Fatalf("canaries=%d turns=%d", canaries, turnCount)
	}
	advice := realSalonJourneyByID(t, corpus, "advice-001")
	if advice.Turns[0].CustomerMessage != "I don't know what service I should book for my nails." ||
		!strings.Contains(advice.Turns[1].CustomerMessage, "too long") {
		t.Fatalf("reported advice regression is missing: %#v", advice.Turns)
	}
	safety := realSalonJourneyByID(t, corpus, "safety-001")
	if !safety.Turns[len(safety.Turns)-1].Expected.RequireHandoff {
		t.Fatal("safety journey does not assert the terminal handoff")
	}
}

func TestRealSalonStructuralEvidenceNeverClaimsModelPass(t *testing.T) {
	report := ReviewRealSalonStructure(loadRealSalonCorpusArtifact(t))
	if report.OverallStatus != "structural_validated_model_not_run" || report.Passed || report.ModelExecuted != 0 || report.ReviewPassed != 0 {
		t.Fatalf("structural evidence made an unsupported model claim: %#v", report)
	}
	if report.StructuralValidated != RealSalonRequiredJourneys || report.NotRun != RealSalonRequiredJourneys {
		t.Fatalf("unexpected structural counts: %#v", report)
	}
	retained := loadRealSalonReportArtifact(t, "receptionist_real_salon_structural_report.json")
	if !reflect.DeepEqual(report, retained) {
		t.Fatal("retained structural report is stale relative to the validated corpus")
	}
	runtime := loadRealSalonReportArtifact(t, "receptionist_real_salon_runtime_report.json")
	if runtime.JourneyCount != RealSalonRequiredJourneys || runtime.ModelExecuted != 0 || runtime.ReviewPassed != 0 || runtime.Passed {
		t.Fatalf("retained deterministic runtime report makes an unsupported model claim: %#v", runtime)
	}
}

func TestRealSalonDeterministicRunnerExecutesAllJourneysWithoutModelOrSideEffects(t *testing.T) {
	corpus := loadRealSalonCorpusArtifact(t)
	report := RunRealSalonDeterministic(context.Background(), corpus)
	if len(report.Results) != RealSalonRequiredJourneys || report.RuntimeExecuted+report.Failed != RealSalonRequiredJourneys {
		t.Fatalf("deterministic runner did not execute all journeys: runtime=%d failed=%d results=%d", report.RuntimeExecuted, report.Failed, len(report.Results))
	}
	if report.ModelExecuted != 0 || report.Passed {
		t.Fatalf("scripted-semantic runtime evidence was mislabeled as a model pass: %#v", report)
	}
	canaryIDs := map[string]bool{}
	for _, journey := range corpus.Journeys {
		canaryIDs[journey.ID] = journey.LiveCanary
	}
	for _, journey := range report.Results {
		if len(journey.Turns) < RealSalonMinCustomerTurns {
			t.Fatalf("journey %s did not retain the full transcript", journey.JourneyID)
		}
		for _, turn := range journey.Turns {
			if canaryIDs[journey.JourneyID] && (turn.SemanticInterpreterInvoked || turn.ReplyGeneratorInvoked) {
				canaryIDs[journey.JourneyID] = false
			}
			if turn.BookingConfirmed || turn.ProviderBookingIDPresent {
				t.Fatalf("journey %s turn %d created booking evidence", journey.JourneyID, turn.Turn)
			}
			for _, attempt := range turn.WouldCallTools {
				if attempt.SideEffect && !attempt.Blocked {
					t.Fatalf("journey %s turn %d allowed side effect %#v", journey.JourneyID, turn.Turn, attempt)
				}
			}
		}
	}
	for journeyID, missingModelPath := range canaryIDs {
		if missingModelPath {
			t.Fatalf("live canary %s does not exercise a production model path", journeyID)
		}
	}
}

func TestRealSalonValidatorRejectsGeneratedDuplicateAndMissingFinalAssertion(t *testing.T) {
	corpus := DefaultRealSalonCorpus()
	corpus.ContractVersion = "stale-corpus-contract"
	corpus.Journeys[0].Generated = true
	corpus.Journeys[1].Channel = corpus.Journeys[0].Channel
	corpus.Journeys[1].CatalogFixture = corpus.Journeys[0].CatalogFixture
	corpus.Journeys[1].Family = corpus.Journeys[0].Family
	corpus.Journeys[1].Turns = append([]RealSalonTurn(nil), corpus.Journeys[0].Turns...)
	corpus.Journeys[2].Turns[len(corpus.Journeys[2].Turns)-1].Expected.FinalReplyAssertion = false
	corpus.Journeys[3].Turns[0].Expected.ToolPolicy = "none"
	corpus.Journeys[3].Turns[0].Expected.AllowedToolCalls = []string{"create_booking"}
	problems := strings.Join(ValidateRealSalonCorpus(corpus), "\n")
	for _, expected := range []string{"contract_version", "authored, non-generated", "duplicates complete journey fingerprint", "final reply assertion is required", "allowed tool is outside the declared tool policy"} {
		if !strings.Contains(problems, expected) {
			t.Fatalf("validator did not reject %q; problems:\n%s", expected, problems)
		}
	}
}

func TestRealSalonTurnValidatorRejectsUnexpectedHandoff(t *testing.T) {
	expected := RealSalonTurnExpectation{
		ForbiddenReplyBehaviors: []string{"repeat_known_question"},
	}
	actual := RealSalonTurnResult{
		AIReply:          "Which service would you like?",
		HandoffRequested: true,
	}
	problems := strings.Join(validateRealSalonTurn(
		expected,
		conversation.Session{},
		conversation.Session{},
		actual,
		nil,
	), "\n")
	if !strings.Contains(problems, "unexpected safety or owner handoff") {
		t.Fatalf("turn validator did not reject unexpected handoff; problems:\n%s", problems)
	}
}

func TestRealSalonLiveCanaryEnforcesPaidCallCeilingAndStops(t *testing.T) {
	corpus := DefaultRealSalonCorpus()
	model := &fakeDirectModel{}
	checkpoint := &memoryRealSalonCheckpoint{}
	report, err := RunRealSalonLive(context.Background(), corpus, model, checkpoint, RealSalonLiveOptions{
		SalonID: "evaluation", ModelCallBudget: 1, RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ModelCallCount != 1 || report.Passed || len(report.Results) != 1 {
		t.Fatalf("live ceiling or stop-on-first-failure contract was violated: calls=%d passed=%t results=%d status=%s", report.ModelCallCount, report.Passed, len(report.Results), report.OverallStatus)
	}
	if model.reviewCalls != 0 {
		t.Fatalf("review spent calls after an incomplete failed canary: %d", model.reviewCalls)
	}
	interpretCalls, replyCalls := model.interpretCalls, model.replyCalls
	resumed, err := RunRealSalonLive(context.Background(), corpus, model, checkpoint, RealSalonLiveOptions{
		SalonID: "evaluation", ModelCallBudget: 1, RequestTimeout: time.Second,
	})
	if err != nil || resumed.RunKey != report.RunKey || model.interpretCalls != interpretCalls || model.replyCalls != replyCalls {
		t.Fatalf("terminal failed checkpoint repeated paid work: err=%v interpret=%d/%d reply=%d/%d", err, model.interpretCalls, interpretCalls, model.replyCalls, replyCalls)
	}
	_, err = RunRealSalonLive(context.Background(), corpus, model, &memoryRealSalonCheckpoint{}, RealSalonLiveOptions{
		SalonID: "evaluation", ModelCallBudget: RealSalonLiveModelCallLimit + 1,
	})
	if err == nil || !strings.Contains(err.Error(), "model-call budget") {
		t.Fatalf("budget above ceiling was accepted: %v", err)
	}
}

func TestRealSalonReviewUsesTwoCompleteMultiTurnBatches(t *testing.T) {
	model := &fakeDirectModel{}
	checkpoint := &memoryRealSalonCheckpoint{}
	report := RealSalonReport{ModelCallBudget: 2}
	for index := 1; index <= RealSalonLiveCanaryJourneys; index++ {
		report.Results = append(report.Results, RealSalonJourneyResult{
			JourneyID: fmt.Sprintf("review-journey-%02d", index),
			Status:    RealSalonStatusModelExecuted,
			Turns: []RealSalonTurnResult{
				{Turn: 1, CustomerMessage: "I need help choosing.", AIReply: "What result would you like?"},
				{Turn: 2, CustomerMessage: "Something durable.", AIReply: "The catalog has a durable option. Would you like to book it?"},
			},
		})
	}
	executor := &realSalonModelExecutor{
		ctx: context.Background(), model: model, checkpoint: checkpoint, report: &report,
		opts: RealSalonLiveOptions{
			SalonID: "evaluation", ModelCallBudget: 2, RequestTimeout: time.Second,
			Now: func() time.Time { return time.Unix(1, 0).UTC() },
		},
	}
	if err := reviewRealSalonCanaries(context.Background(), &report, executor); err != nil {
		t.Fatal(err)
	}
	if len(report.ReviewRounds) != 2 || model.reviewCalls != 2 {
		t.Fatalf("review rounds=%d calls=%d", len(report.ReviewRounds), model.reviewCalls)
	}
	for index := range model.reviewBatchSizes {
		if model.reviewBatchSizes[index] != DirectReviewBatchSize ||
			!model.reviewMultiTurn[index] || model.reviewJourneySizes[index] != DirectReviewBatchSize {
			t.Fatalf("review batch %d: flat=%d multi_turn=%t journeys=%d", index+1, model.reviewBatchSizes[index], model.reviewMultiTurn[index], model.reviewJourneySizes[index])
		}
	}
	for _, result := range report.Results {
		if result.Status != RealSalonStatusReviewPassed || !result.ReviewPassed {
			t.Fatalf("journey was not marked review-passed: %#v", result)
		}
	}
}

type memoryRealSalonCheckpoint struct {
	report RealSalonReport
	found  bool
}

func (m *memoryRealSalonCheckpoint) Load(context.Context) (RealSalonReport, bool, error) {
	return cloneRealSalonReport(m.report), m.found, nil
}

func (m *memoryRealSalonCheckpoint) Save(_ context.Context, report RealSalonReport) error {
	m.report = cloneRealSalonReport(report)
	m.found = true
	return nil
}

func cloneRealSalonReport(report RealSalonReport) RealSalonReport {
	raw, _ := json.Marshal(report)
	var cloned RealSalonReport
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

func loadRealSalonCorpusArtifact(t *testing.T) RealSalonCorpus {
	t.Helper()
	path := filepath.Join("..", "..", "modules", "conversation", "testdata", "receptionist_real_salon_100.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var corpus RealSalonCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func loadRealSalonReportArtifact(t *testing.T, name string) RealSalonReport {
	t.Helper()
	path := filepath.Join("..", "..", "modules", "conversation", "testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report RealSalonReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func realSalonJourneyByID(t *testing.T, corpus RealSalonCorpus, id string) RealSalonJourney {
	t.Helper()
	for _, journey := range corpus.Journeys {
		if journey.ID == id {
			return journey
		}
	}
	t.Fatalf("journey %s not found", id)
	return RealSalonJourney{}
}
