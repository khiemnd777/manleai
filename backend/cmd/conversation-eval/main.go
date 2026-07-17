package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/conversationeval"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/modules/conversation"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type liveResult struct {
	index              int
	transientRetries   int
	recoveredTransient bool
	modelEvaluated     bool
	result             conversationeval.ScenarioEvaluationResult
	failure            *conversationeval.EvaluationFailure
}

const maxLiveScenarioCount = 12

type semanticEndpointError struct {
	statusCode int
	body       string
}

func (e *semanticEndpointError) Error() string {
	return fmt.Sprintf("semantic endpoint status=%d body=%s", e.statusCode, e.body)
}

func main() {
	var corpusPath string
	var mode string
	var apiBase string
	var salonID string
	var tokenFile string
	var outputPath string
	var scenarioIDs string
	var concurrency int
	var limit int
	var samplePerFamily int
	var transientRetries int
	var requestTimeout time.Duration
	var checkpointPath string
	var maxModelCalls int
	flag.StringVar(&corpusPath, "corpus", "", "corpus path; defaults to the 50-scenario pilot for direct-model and the 1,000-scenario corpus otherwise")
	flag.StringVar(&mode, "mode", "offline", "evaluation mode: offline, runtime-canary (live alias), or direct-model")
	flag.StringVar(&apiBase, "api-base", "http://127.0.0.1:8080/api", "API base URL for live mode")
	flag.StringVar(&salonID, "salon-id", "", "owner-scoped salon ID for live mode; database config and zero-token runtime-readiness selector for direct-model")
	flag.StringVar(&tokenFile, "token-file", "", "path to a file containing an owner bearer token for live mode")
	flag.StringVar(&outputPath, "output", "", "optional JSON report output path")
	flag.StringVar(&scenarioIDs, "scenario-ids", "", "optional comma-separated scenario IDs; cannot be combined with -limit or -sample-per-family")
	flag.IntVar(&concurrency, "concurrency", 4, "number of concurrent live semantic requests (1-20)")
	flag.IntVar(&limit, "limit", 0, "optional scenario limit; zero runs the full corpus")
	flag.IntVar(&samplePerFamily, "sample-per-family", 0, "optional deterministic sample size per scenario family; cannot be combined with -limit")
	flag.IntVar(&transientRetries, "transient-retries", 0, "bounded transient transport/provider retries (0-3); semantic mismatches are never retried")
	flag.DurationVar(&requestTimeout, "request-timeout", 45*time.Second, "per-request timeout inherited by the provider call")
	flag.StringVar(&checkpointPath, "checkpoint", "", "direct-model checkpoint path; defaults to <output>.checkpoint.json")
	flag.IntVar(&maxModelCalls, "max-model-calls", 0, "required hard ceiling for paid direct-model calls, including retries and review rounds")
	flag.Parse()
	mode = strings.TrimSpace(mode)
	if strings.TrimSpace(corpusPath) == "" {
		if mode == "direct-model" {
			corpusPath = "modules/conversation/testdata/receptionist_semantic_pilot_50.json"
		} else {
			corpusPath = "modules/conversation/testdata/receptionist_semantic_scenarios.json"
		}
	}

	corpus, err := readCorpus(corpusPath)
	if err != nil {
		fatalf("read corpus: %v", err)
	}
	validateCorpus := conversationeval.ValidateCorpus
	if mode == "direct-model" {
		validateCorpus = conversationeval.ValidatePilotCorpus
	}
	if problems := validateCorpus(corpus); len(problems) > 0 {
		fatalf("corpus validation failed with %d problems; first: %s", len(problems), problems[0])
	}
	reviewCorpus := corpus
	if limit < 0 || limit > len(corpus.Scenarios) {
		fatalf("limit must be between zero and %d", len(corpus.Scenarios))
	}
	if samplePerFamily < 0 {
		fatalf("sample-per-family must be zero or greater")
	}
	if limit > 0 && samplePerFamily > 0 {
		fatalf("limit and sample-per-family cannot be combined")
	}
	if strings.TrimSpace(scenarioIDs) != "" && (limit > 0 || samplePerFamily > 0) {
		fatalf("scenario-ids cannot be combined with limit or sample-per-family")
	}
	if limit > 0 {
		corpus.Scenarios = corpus.Scenarios[:limit]
	} else if samplePerFamily > 0 {
		corpus.Scenarios = sampleScenariosPerFamily(corpus.Scenarios, samplePerFamily)
	} else if strings.TrimSpace(scenarioIDs) != "" {
		var err error
		corpus.Scenarios, err = selectScenariosByID(corpus.Scenarios, strings.Split(scenarioIDs, ","))
		if err != nil {
			fatalf("select scenarios: %v", err)
		}
	}

	var report conversationeval.EvaluationReport
	switch mode {
	case "offline":
		review := conversationeval.ReviewCorpus(reviewCorpus)
		report = conversationeval.EvaluationReport{
			SchemaVersion:            conversationeval.SchemaVersion,
			Mode:                     "offline_corpus_validation_without_model_execution",
			ContextSource:            "corpus_fixture",
			RuntimeReadinessVerified: false,
			ScenarioCount:            len(corpus.Scenarios),
			ContractValidatedCount:   len(corpus.Scenarios),
			NotRunCount:              len(corpus.Scenarios),
			Results:                  offlineScenarioResults(corpus.Scenarios),
		}
		if !review.Passed {
			report.ContractValidatedCount = 0
			report.FailedCount = len(corpus.Scenarios)
			report.NotRunCount = 0
			report.Failures = []conversationeval.EvaluationFailure{{ScenarioID: "corpus_review", Errors: []string{"100-round corpus review failed"}}}
		}
	case "live", "runtime-canary":
		if strings.TrimSpace(salonID) == "" || strings.TrimSpace(tokenFile) == "" {
			fatalf("live mode requires -salon-id and -token-file")
		}
		if concurrency < 1 || concurrency > 20 {
			fatalf("concurrency must be between 1 and 20")
		}
		if requestTimeout < time.Second || requestTimeout > 2*time.Minute {
			fatalf("request-timeout must be between 1s and 2m")
		}
		if transientRetries < 0 || transientRetries > 3 {
			fatalf("transient-retries must be between 0 and 3")
		}
		if len(corpus.Scenarios) > maxLiveScenarioCount {
			fatalf("live mode is capped at %d scenarios; select an explicit canary set with -scenario-ids or -limit", maxLiveScenarioCount)
		}
		if strings.TrimSpace(outputPath) == "" {
			fatalf("live mode requires -output so every pass and failure retains its model result")
		}
		token, err := os.ReadFile(tokenFile)
		if err != nil {
			fatalf("read token file: %v", err)
		}
		report = runLive(context.Background(), corpus, strings.TrimSpace(apiBase), strings.TrimSpace(salonID), strings.TrimSpace(string(token)), concurrency, requestTimeout, transientRetries)
	case "direct-model":
		if strings.TrimSpace(salonID) == "" {
			fatalf("direct-model requires -salon-id to select encrypted OpenAI config and run the zero-token runtime preflight")
		}
		if strings.TrimSpace(outputPath) == "" {
			fatalf("direct-model requires -output so every paid result is retained")
		}
		if maxModelCalls <= 0 {
			fatalf("direct-model requires a positive -max-model-calls hard ceiling")
		}
		// Selection is applied only after the complete 50-scenario pilot corpus
		// passes contract validation. This supports a bounded paid canary without
		// weakening or silently rewriting the source corpus.
		if requestTimeout < time.Second || requestTimeout > 2*time.Minute {
			fatalf("request-timeout must be between 1s and 2m")
		}
		if transientRetries < 0 || transientRetries > 3 {
			fatalf("transient-retries must be between 0 and 3")
		}
		if strings.TrimSpace(checkpointPath) == "" {
			checkpointPath = outputPath + ".checkpoint.json"
		}
		cfg := config.Load()
		db, err := database.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			fatalf("open database for strict OpenAI config resolution: %v", err)
		}
		defer db.Close()
		var ownerUserID string
		if err := db.QueryRowContext(context.Background(), `
			SELECT owner_user_id::text
			FROM salons
			WHERE id = $1
		`, strings.TrimSpace(salonID)).Scan(&ownerUserID); err != nil {
			fatalf("load salon owner for runtime preflight: %v", err)
		}
		readiness, err := voice.NewRepository(db).GetPhoneBookingReadiness(context.Background(), strings.TrimSpace(salonID), ownerUserID)
		if err != nil {
			fatalf("run zero-token runtime readiness preflight: %v", err)
		}
		runtimePreflight := &conversationeval.RuntimePreflightEvidence{
			SalonID: strings.TrimSpace(salonID), CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
			GuidanceServiceCount:     readiness.GuidanceServiceCount,
			RecommendationReadyCount: readiness.ConsultationReadyServices,
			ServiceGuidanceStatus:    string(readiness.ServiceGuidance.Status),
			BookingServiceCount:      readiness.ServiceCount, ProviderSynced: readiness.ProviderSynced,
			BookingReady: readiness.Ready,
			Passed:       readiness.ServiceGuidance.Status == conversation.ServiceGuidanceCapabilityRecommendationReady,
		}
		if !runtimePreflight.Passed {
			fatalf("zero-token runtime guidance preflight failed: status=%s guidance_services=%d ready_profiles=%d", runtimePreflight.ServiceGuidanceStatus, runtimePreflight.GuidanceServiceCount, runtimePreflight.RecommendationReadyCount)
		}
		cipher, err := encryption.NewTokenCipher(cfg.EncryptionKey)
		if err != nil {
			fatalf("create database secret cipher: %v", err)
		}
		integrationService := integrationconfig.NewService(integrationconfig.NewRepository(db), cipher, cfg)
		resolver := strictDatabaseOpenAIResolver{service: integrationService, configSalonID: strings.TrimSpace(salonID)}
		storedOpenAIConfig, enabled, err := resolver.ResolveOpenAIConfig(context.Background(), "direct-model-evaluation")
		if err != nil {
			fatalf("resolve encrypted database OpenAI config: %v", err)
		} else if !enabled {
			fatalf("the stored OpenAI integration is disabled")
		}
		// Freeze the strictly database-resolved configuration for the whole run so
		// one report cannot mix model or credential revisions changed mid-pilot.
		model := conversationeval.NewOpenAIDirectModel(pinnedOpenAIConfigResolver{cfg: storedOpenAIConfig})
		report, err = conversationeval.RunDirect(
			context.Background(), corpus, model, conversationeval.FixtureBackendRunner{},
			conversationeval.JSONCheckpointStore{Path: checkpointPath},
			conversationeval.DirectRunOptions{
				SalonID: "direct-model-evaluation", ModelCallBudget: maxModelCalls,
				RequestTimeout: requestTimeout, TransientRetries: transientRetries,
				RuntimePreflight: runtimePreflight,
			},
		)
		if err != nil {
			fatalf("run direct-model pilot: %v", err)
		}
	default:
		fatalf("mode must be offline, runtime-canary (live alias), or direct-model")
	}

	if outputPath != "" {
		if err := writeJSON(outputPath, report); err != nil {
			fatalf("write report: %v", err)
		}
	}
	fmt.Printf("mode=%s scenarios=%d contract_validated=%d model_evaluated=%d passed=%d failed=%d not_run=%d review_passed=%d review_failed=%d transient_retries=%d recovered_transients=%d\n", report.Mode, report.ScenarioCount, report.ContractValidatedCount, report.ModelEvaluatedCount, report.PassedCount, report.FailedCount, report.NotRunCount, report.ReviewPassedCount, report.ReviewFailedCount, report.TransientRetryCount, report.RecoveredTransientCount)
	if report.FailedCount > 0 || report.ReviewFailedCount > 0 {
		os.Exit(1)
	}
}

type strictDatabaseOpenAIResolver struct {
	service       *integrationconfig.Service
	configSalonID string
}

type pinnedOpenAIConfigResolver struct {
	cfg config.OpenAIVoiceConfig
}

func (r pinnedOpenAIConfigResolver) ResolveOpenAIConfig(context.Context, string) (config.OpenAIVoiceConfig, bool, error) {
	return r.cfg, true, nil
}

func (r strictDatabaseOpenAIResolver) ResolveOpenAIConfig(ctx context.Context, _ string) (config.OpenAIVoiceConfig, bool, error) {
	if r.service == nil || strings.TrimSpace(r.configSalonID) == "" {
		return config.OpenAIVoiceConfig{}, false, errors.New("integration config service is required")
	}
	return r.service.ResolveOpenAIConfigStrict(ctx, r.configSalonID)
}

func offlineScenarioResults(scenarios []conversationeval.Scenario) []conversationeval.ScenarioEvaluationResult {
	results := make([]conversationeval.ScenarioEvaluationResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		results = append(results, conversationeval.ScenarioEvaluationResult{
			ScenarioID: scenario.ID,
			Status:     "contract_validated_model_not_run",
		})
	}
	return results
}

func sampleScenariosPerFamily(scenarios []conversationeval.Scenario, perFamily int) []conversationeval.Scenario {
	if perFamily <= 0 {
		return append([]conversationeval.Scenario(nil), scenarios...)
	}
	counts := make(map[string]int)
	sample := make([]conversationeval.Scenario, 0)
	for _, scenario := range scenarios {
		if counts[scenario.Family] >= perFamily {
			continue
		}
		counts[scenario.Family]++
		sample = append(sample, scenario)
	}
	return sample
}

func selectScenariosByID(scenarios []conversationeval.Scenario, requestedIDs []string) ([]conversationeval.Scenario, error) {
	byID := make(map[string]conversationeval.Scenario, len(scenarios))
	for _, scenario := range scenarios {
		byID[scenario.ID] = scenario
	}
	selected := make([]conversationeval.Scenario, 0, len(requestedIDs))
	seen := make(map[string]bool, len(requestedIDs))
	for _, rawID := range requestedIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		scenario, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("scenario %q does not exist", id)
		}
		seen[id] = true
		selected = append(selected, scenario)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one scenario ID is required")
	}
	return selected, nil
}

func readCorpus(path string) (conversationeval.Corpus, error) {
	var corpus conversationeval.Corpus
	raw, err := os.ReadFile(path)
	if err != nil {
		return corpus, err
	}
	err = json.Unmarshal(raw, &corpus)
	return corpus, err
}

func runLive(ctx context.Context, corpus conversationeval.Corpus, apiBase string, salonID string, token string, concurrency int, timeout time.Duration, transientRetries int) conversationeval.EvaluationReport {
	return runLiveWithClient(ctx, corpus, apiBase, salonID, token, concurrency, timeout, transientRetries, &http.Client{})
}

func runLiveWithClient(ctx context.Context, corpus conversationeval.Corpus, apiBase string, salonID string, token string, concurrency int, timeout time.Duration, transientRetries int, client *http.Client) conversationeval.EvaluationReport {
	report := conversationeval.EvaluationReport{
		SchemaVersion: conversationeval.SchemaVersion, Mode: "live_salon_scoped_model",
		ContextSource: "runtime_model_with_request_fixture", RuntimeReadinessVerified: false,
		ScenarioCount: len(corpus.Scenarios), ContractValidatedCount: len(corpus.Scenarios),
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/salons/" + url.PathEscape(salonID) + "/voice/semantic-evaluate"
	jobs := make(chan int)
	results := make(chan liveResult, len(corpus.Scenarios))
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				scenario := corpus.Scenarios[index]
				request, err := scenario.ResolvedRequest(corpus)
				if err != nil {
					results <- failedLiveResult(index, scenario.ID, err.Error())
					continue
				}
				var response *voice.SemanticEvaluationResponse
				var callErr error
				retriesUsed := 0
				for {
					requestCtx, cancel := context.WithTimeout(ctx, timeout)
					response, callErr = callSemanticEvaluation(requestCtx, client, endpoint, token, request)
					cancel()
					if callErr == nil || retriesUsed >= transientRetries || !isTransientSemanticEndpointError(callErr) {
						break
					}
					retriesUsed++
				}
				if callErr != nil {
					failed := failedLiveResult(index, scenario.ID, callErr.Error())
					failed.transientRetries = retriesUsed
					results <- failed
					continue
				}
				problems := conversationeval.EvaluateResult(scenario, corpus, response.Result)
				actual := response.Result
				if len(problems) > 0 {
					results <- liveResult{
						index: index, transientRetries: retriesUsed, recoveredTransient: retriesUsed > 0, modelEvaluated: true,
						result:  conversationeval.ScenarioEvaluationResult{ScenarioID: scenario.ID, Status: "failed", DurationMS: response.DurationMS, Errors: append([]string(nil), problems...), Actual: &actual},
						failure: &conversationeval.EvaluationFailure{ScenarioID: scenario.ID, Errors: problems, Actual: &actual},
					}
					continue
				}
				results <- liveResult{
					index: index, transientRetries: retriesUsed, recoveredTransient: retriesUsed > 0, modelEvaluated: true,
					result: conversationeval.ScenarioEvaluationResult{ScenarioID: scenario.ID, Status: "passed", DurationMS: response.DurationMS, Actual: &actual},
				}
			}
		}()
	}
	go func() {
		for index := range corpus.Scenarios {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	ordered := make([]liveResult, 0, len(corpus.Scenarios))
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i int, j int) bool { return ordered[i].index < ordered[j].index })
	for _, result := range ordered {
		report.TransientRetryCount += result.transientRetries
		if result.recoveredTransient {
			report.RecoveredTransientCount++
		}
		if result.modelEvaluated {
			report.ModelEvaluatedCount++
		}
		report.Results = append(report.Results, result.result)
		if result.failure == nil {
			report.PassedCount++
			continue
		}
		report.FailedCount++
		report.Failures = append(report.Failures, *result.failure)
	}
	return report
}

func callSemanticEvaluation(ctx context.Context, client *http.Client, endpoint string, token string, input voice.SemanticEvaluationRequest) (*voice.SemanticEvaluationResponse, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, &semanticEndpointError{statusCode: res.StatusCode, body: strings.TrimSpace(string(body))}
	}
	var output voice.SemanticEvaluationResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 2*1024*1024)).Decode(&output); err != nil {
		return nil, err
	}
	return &output, nil
}

func isTransientSemanticEndpointError(err error) bool {
	var endpointErr *semanticEndpointError
	if !errors.As(err, &endpointErr) {
		return false
	}
	return endpointErr.statusCode == http.StatusBadGateway || endpointErr.statusCode == http.StatusServiceUnavailable || endpointErr.statusCode == http.StatusGatewayTimeout
}

func failedLiveResult(index int, scenarioID string, problem string) liveResult {
	errorsFound := []string{problem}
	return liveResult{
		index:   index,
		result:  conversationeval.ScenarioEvaluationResult{ScenarioID: scenarioID, Status: "request_failed", Errors: errorsFound},
		failure: &conversationeval.EvaluationFailure{ScenarioID: scenarioID, Errors: errorsFound},
	}
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
