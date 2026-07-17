package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/conversationeval"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestRunLiveCallsReadOnlySemanticEndpointAndScoresStructuredResult(t *testing.T) {
	corpus := conversationeval.GenerateCorpus()
	corpus.Scenarios = corpus.Scenarios[:1]
	expected := corpus.Scenarios[0].Expected
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/salons/salon_1/voice/semantic-evaluate" || req.Header.Get("Authorization") != "Bearer owner-token" {
			t.Fatalf("request path=%q authorization=%q", req.URL.Path, req.Header.Get("Authorization"))
		}
		var input voice.SemanticEvaluationRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if input.ScenarioID != corpus.Scenarios[0].ID || len(input.CatalogServices) == 0 {
			t.Fatalf("semantic request = %#v", input)
		}
		var body bytes.Buffer
		_ = json.NewEncoder(&body).Encode(voice.SemanticEvaluationResponse{
			ScenarioID: input.ScenarioID,
			Result: voice.TurnModelReply{
				Goal: expected.Goal, GuidanceAction: expected.GuidanceAction,
				GuidanceCatalogMode: expected.GuidanceCatalogMode, GuidanceQuestionSubject: expected.GuidanceQuestionSubject,
			},
		})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&body), Header: make(http.Header)}, nil
	})}

	report := runLiveWithClient(context.Background(), corpus, "https://example.test/api", "salon_1", "owner-token", 1, 5*time.Second, 0, client)
	if report.ScenarioCount != 1 || report.ContractValidatedCount != 1 || report.ModelEvaluatedCount != 1 || report.PassedCount != 1 || report.FailedCount != 0 ||
		len(report.Results) != 1 || report.Results[0].Status != "passed" || report.Results[0].Actual == nil {
		t.Fatalf("live report = %#v", report)
	}
}

func TestRunLiveRetriesOnlyTransientEndpointFailureAndReportsRecovery(t *testing.T) {
	corpus := conversationeval.GenerateCorpus()
	corpus.Scenarios = corpus.Scenarios[:1]
	expected := corpus.Scenarios[0].Expected
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusGatewayTimeout, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"TIMEOUT"}}`)), Header: make(http.Header)}, nil
		}
		var body bytes.Buffer
		_ = json.NewEncoder(&body).Encode(voice.SemanticEvaluationResponse{
			Result: voice.TurnModelReply{
				Goal: expected.Goal, GuidanceAction: expected.GuidanceAction,
				GuidanceCatalogMode: expected.GuidanceCatalogMode, GuidanceQuestionSubject: expected.GuidanceQuestionSubject,
			},
		})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&body), Header: make(http.Header)}, nil
	})}

	report := runLiveWithClient(context.Background(), corpus, "https://example.test/api", "salon_1", "owner-token", 1, 5*time.Second, 1, client)
	if calls != 2 || report.ModelEvaluatedCount != 1 || report.PassedCount != 1 || report.TransientRetryCount != 1 || report.RecoveredTransientCount != 1 || len(report.Results) != 1 {
		t.Fatalf("transient retry report = calls:%d report:%#v", calls, report)
	}
}

func TestOfflineScenarioResultsNeverClaimModelPass(t *testing.T) {
	corpus := conversationeval.GenerateCorpus()
	results := offlineScenarioResults(corpus.Scenarios)
	if len(results) != conversationeval.RequiredScenarioCount {
		t.Fatalf("offline results=%d", len(results))
	}
	for _, result := range results {
		if result.Status != "contract_validated_model_not_run" || result.Actual != nil {
			t.Fatalf("offline result claimed model evidence: %#v", result)
		}
	}
}

func TestLiveSafetyCapIsTwelveScenarios(t *testing.T) {
	if maxLiveScenarioCount != 12 {
		t.Fatalf("max live scenarios=%d", maxLiveScenarioCount)
	}
}

func TestSampleScenariosPerFamilyIsDeterministicAndBalanced(t *testing.T) {
	scenarios := []conversationeval.Scenario{
		{ID: "catalog-1", Family: "catalog"},
		{ID: "catalog-2", Family: "catalog"},
		{ID: "booking-1", Family: "booking"},
		{ID: "catalog-3", Family: "catalog"},
		{ID: "booking-2", Family: "booking"},
		{ID: "booking-3", Family: "booking"},
	}

	sample := sampleScenariosPerFamily(scenarios, 2)
	got := make([]string, 0, len(sample))
	for _, scenario := range sample {
		got = append(got, scenario.ID)
	}
	want := []string{"catalog-1", "catalog-2", "booking-1", "booking-2"}
	if len(got) != len(want) {
		t.Fatalf("sample ids = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("sample ids = %v, want %v", got, want)
		}
	}
}

func TestSelectScenariosByIDPreservesRequestedOrderAndRejectsUnknown(t *testing.T) {
	scenarios := []conversationeval.Scenario{
		{ID: "catalog-1", Family: "catalog"},
		{ID: "booking-1", Family: "booking"},
	}

	selected, err := selectScenariosByID(scenarios, []string{"booking-1", "catalog-1", "booking-1"})
	if err != nil {
		t.Fatalf("select scenarios: %v", err)
	}
	if len(selected) != 2 || selected[0].ID != "booking-1" || selected[1].ID != "catalog-1" {
		t.Fatalf("selected scenarios = %#v", selected)
	}
	if _, err := selectScenariosByID(scenarios, []string{"unknown"}); err == nil {
		t.Fatal("expected unknown scenario error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
