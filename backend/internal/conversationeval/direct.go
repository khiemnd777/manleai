package conversationeval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

var (
	ErrModelCallBudgetExhausted = errors.New("direct evaluation model-call budget exhausted")
	ErrUncertainModelCall       = errors.New("direct evaluation has an uncertain in-flight model call")
)

type DirectModel interface {
	Identity(ctx context.Context, salonID string) (string, error)
	InterpretTurn(ctx context.Context, salonID string, request voice.SemanticEvaluationRequest) (voice.TurnModelReply, ModelUsage, error)
	GenerateReply(ctx context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, ModelUsage, error)
	GenerateConsultationQuestion(ctx context.Context, request conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, ModelUsage, error)
	ReviewReplies(ctx context.Context, salonID string, input DirectReviewInput) (DirectReviewRound, ModelUsage, error)
}

type DirectReviewInput struct {
	Round              int                        `json:"round"`
	EvaluationContract string                     `json:"evaluation_contract"`
	ReviewContract     string                     `json:"review_contract"`
	MultiTurn          bool                       `json:"multi_turn,omitempty"`
	Results            []ScenarioEvaluationResult `json:"results"`
	JourneyResults     []RealSalonJourneyResult   `json:"journey_results,omitempty"`
}

type BackendTurnResult struct {
	SafeReply         string
	FinalReply        string
	NextExpectedInput string
	Evidence          BackendEvidence
	WouldCallTools    []ToolAttempt
}

type BackendTurnRunner interface {
	Run(ctx context.Context, salonID string, corpus Corpus, scenario Scenario, actual voice.TurnModelReply, replies conversation.ReplyGenerator) (BackendTurnResult, error)
}

type CheckpointStore interface {
	Load(ctx context.Context) (EvaluationReport, bool, error)
	Save(ctx context.Context, report EvaluationReport) error
}

type DirectRunOptions struct {
	SalonID          string
	RunKey           string
	ModelCallBudget  int
	RequestTimeout   time.Duration
	TransientRetries int
	Now              func() time.Time
	RuntimePreflight *RuntimePreflightEvidence
}

func DirectRunKey(corpus Corpus, salonID string, modelIdentity string) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion      int        `json:"schema_version"`
		EvaluationContract string     `json:"evaluation_contract"`
		ReviewContract     string     `json:"review_contract"`
		Taxonomy           string     `json:"taxonomy"`
		SalonID            string     `json:"salon_id"`
		Model              string     `json:"model"`
		Scenarios          []Scenario `json:"scenarios"`
	}{
		SchemaVersion: corpus.SchemaVersion, EvaluationContract: DirectEvaluationContractVersion,
		ReviewContract: DirectReviewContractVersion, Taxonomy: corpus.TaxonomyRelease,
		SalonID: strings.TrimSpace(salonID), Model: strings.TrimSpace(modelIdentity), Scenarios: corpus.Scenarios,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("direct-v%d-%x", corpus.SchemaVersion, sum[:12]), nil
}

func RunDirect(ctx context.Context, corpus Corpus, model DirectModel, backend BackendTurnRunner, checkpoint CheckpointStore, opts DirectRunOptions) (EvaluationReport, error) {
	if model == nil || backend == nil || checkpoint == nil || strings.TrimSpace(opts.SalonID) == "" || opts.ModelCallBudget <= 0 {
		return EvaluationReport{}, fmt.Errorf("direct evaluation configuration is invalid")
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 45 * time.Second
	}
	if opts.TransientRetries < 0 || opts.TransientRetries > 3 {
		return EvaluationReport{}, fmt.Errorf("transient retries must be between 0 and 3")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.RuntimePreflight != nil && !opts.RuntimePreflight.Passed {
		return EvaluationReport{}, fmt.Errorf("runtime guidance preflight did not pass")
	}
	identity, err := model.Identity(ctx, opts.SalonID)
	if err != nil {
		return EvaluationReport{}, fmt.Errorf("resolve direct model identity: %w", err)
	}
	runKey := strings.TrimSpace(opts.RunKey)
	if runKey == "" {
		runKey, err = DirectRunKey(corpus, opts.SalonID, identity)
		if err != nil {
			return EvaluationReport{}, err
		}
	}

	report, found, err := checkpoint.Load(ctx)
	if err != nil {
		return EvaluationReport{}, fmt.Errorf("load direct checkpoint: %w", err)
	}
	if found {
		if report.RunKey != runKey {
			return EvaluationReport{}, fmt.Errorf("checkpoint run_key=%q does not match %q", report.RunKey, runKey)
		}
		if report.InFlightModelCall != nil {
			return report, fmt.Errorf("%w: stage=%s scenario=%s attempt=%d", ErrUncertainModelCall, report.InFlightModelCall.Stage, report.InFlightModelCall.ScenarioID, report.InFlightModelCall.Attempt)
		}
		report.ContextSource = "isolated_fixture"
		report.RuntimeReadinessVerified = false
		report.RuntimePreflight = opts.RuntimePreflight
		if opts.ModelCallBudget < report.ModelCallCount {
			return report, fmt.Errorf("model-call budget %d is below already-used count %d", opts.ModelCallBudget, report.ModelCallCount)
		}
		report.ModelCallBudget = opts.ModelCallBudget
	} else {
		report = EvaluationReport{
			SchemaVersion:            SchemaVersion,
			EvaluationContract:       DirectEvaluationContractVersion,
			ReviewContract:           DirectReviewContractVersion,
			Mode:                     "direct_model_database_config_no_side_effects",
			ContextSource:            "isolated_fixture",
			RuntimeReadinessVerified: false,
			RuntimePreflight:         opts.RuntimePreflight,
			RunKey:                   runKey,
			ScenarioCount:            len(corpus.Scenarios),
			ContractValidatedCount:   len(corpus.Scenarios),
			ModelCallBudget:          opts.ModelCallBudget,
			StartedAt:                opts.Now().Format(time.RFC3339Nano),
		}
	}
	executor := directCallExecutor{model: model, checkpoint: checkpoint, report: &report, opts: opts}
	retainedResults := make([]ScenarioEvaluationResult, 0, len(report.Results))
	completed := make(map[string]bool, len(report.Results))
	partialResults := make(map[string]ScenarioEvaluationResult)
	for index := range report.Results {
		if report.Results[index].Status == "not_run_budget_exhausted" {
			partialResults[report.Results[index].ScenarioID] = report.Results[index]
			continue
		}
		report.Results[index].Resumed = true
		completed[report.Results[index].ScenarioID] = true
		retainedResults = append(retainedResults, report.Results[index])
	}
	report.Results = retainedResults
	retainedReviews := report.ReviewRounds[:0]
	for _, review := range report.ReviewRounds {
		if review.ModelCalls == 0 && len(review.Errors) > 0 {
			continue
		}
		retainedReviews = append(retainedReviews, review)
	}
	report.ReviewRounds = retainedReviews

	for _, scenario := range corpus.Scenarios {
		if completed[scenario.ID] {
			continue
		}
		partial, hasPartial := partialResults[scenario.ID]
		var partialPtr *ScenarioEvaluationResult
		if hasPartial {
			partialPtr = &partial
		}
		result, runErr := runDirectScenario(ctx, corpus, scenario, backend, &executor, partialPtr)
		if errors.Is(runErr, ErrModelCallBudgetExhausted) {
			result.Status = "not_run_budget_exhausted"
			result.Errors = appendUnique(result.Errors, runErr.Error())
		}
		report.Results = append(report.Results, result)
		if err := checkpoint.Save(ctx, report); err != nil {
			return report, fmt.Errorf("save scenario checkpoint: %w", err)
		}
		if errors.Is(runErr, ErrModelCallBudgetExhausted) {
			break
		}
	}

	if len(report.Results) == len(corpus.Scenarios) && len(corpus.Scenarios) > 0 {
		if err := runDirectReviews(ctx, corpus, &executor); err != nil && !errors.Is(err, ErrModelCallBudgetExhausted) {
			return report, err
		}
	}
	recalculateDirectSummary(&report)
	report.CompletedAt = opts.Now().Format(time.RFC3339Nano)
	if err := checkpoint.Save(ctx, report); err != nil {
		return report, fmt.Errorf("save final direct checkpoint: %w", err)
	}
	return report, nil
}

func runDirectScenario(ctx context.Context, corpus Corpus, scenario Scenario, backend BackendTurnRunner, executor *directCallExecutor, partial *ScenarioEvaluationResult) (ScenarioEvaluationResult, error) {
	started := time.Now()
	result := ScenarioEvaluationResult{
		ScenarioID: scenario.ID, Status: "failed", Channel: scenario.Request.Channel,
		CustomerMessage: scenario.Request.CustomerMessage,
	}
	if partial != nil {
		result = *partial
		result.Status = "failed"
		result.Resumed = true
		result.Errors = nil
		result.DurationMS = 0
		result.ReplyDurationMS = 0
	}
	request, err := scenario.ResolvedRequest(corpus)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	var actual voice.TurnModelReply
	if result.Actual == nil {
		recognitionStarted := time.Now()
		value, usage, calls, err := executor.call(ctx, "recognition", scenario.ID, func(callCtx context.Context) (any, ModelUsage, error) {
			modelResult, callUsage, callErr := executor.model.InterpretTurn(callCtx, executor.opts.SalonID, request)
			return modelResult, callUsage, callErr
		})
		result.RecognitionDurationMS = time.Since(recognitionStarted).Milliseconds()
		result.ModelCalls += calls
		result.Usage.Add(usage)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.DurationMS = time.Since(started).Milliseconds()
			return result, err
		}
		actual = value.(voice.TurnModelReply)
		result.Actual = &actual
	} else {
		actual = *result.Actual
	}
	result.Errors = append(result.Errors, EvaluateResult(scenario, corpus, actual)...)

	replyCalls := &directServiceReplyGenerator{executor: executor, scenarioID: scenario.ID}
	backendResult, backendErr := backend.Run(ctx, executor.opts.SalonID, corpus, scenario, actual, replyCalls)
	result.ModelCalls += replyCalls.modelCalls
	result.Usage.Add(replyCalls.usage)
	result.ReplyDurationMS = replyCalls.duration.Milliseconds()
	result.BackendSafeReply = strings.TrimSpace(backendResult.SafeReply)
	result.FinalReply = strings.TrimSpace(backendResult.FinalReply)
	result.NextExpectedInput = strings.TrimSpace(backendResult.NextExpectedInput)
	result.BackendEvidence = backendResult.Evidence
	result.WouldCallTools = append([]ToolAttempt(nil), backendResult.WouldCallTools...)
	if backendErr != nil {
		result.Errors = append(result.Errors, "backend turn: "+backendErr.Error())
	}
	for _, replyErr := range replyCalls.errors {
		result.Errors = append(result.Errors, "reply generation: "+replyErr.Error())
	}
	if result.BackendSafeReply == "" {
		result.Errors = append(result.Errors, "backend produced an empty safe reply")
		result.DurationMS = time.Since(started).Milliseconds()
		return result, nil
	}
	if result.FinalReply == "" {
		result.Errors = append(result.Errors, "backend produced an empty final reply")
	}
	result.Errors = append(result.Errors, validateBackendResult(scenario, backendResult)...)
	result.Errors = append(result.Errors, validateDirectReply(result.FinalReply, directReplyForbiddenIdentifiers(request, actual))...)
	if replyCalls.budgetExhausted {
		result.DurationMS = time.Since(started).Milliseconds()
		return result, ErrModelCallBudgetExhausted
	}
	if len(result.Errors) == 0 {
		result.Status = "passed"
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result, nil
}

func runDirectReviews(ctx context.Context, corpus Corpus, executor *directCallExecutor) error {
	report := executor.report
	if DirectReviewBatchSize <= 0 {
		return errors.New("direct review batch size must be positive")
	}
	rounds := (len(report.Results) + DirectReviewBatchSize - 1) / DirectReviewBatchSize
	for round := len(report.ReviewRounds) + 1; round <= rounds; round++ {
		start := (round - 1) * DirectReviewBatchSize
		end := start + DirectReviewBatchSize
		if end > len(report.Results) {
			end = len(report.Results)
		}
		batch := append([]ScenarioEvaluationResult(nil), report.Results[start:end]...)
		input := DirectReviewInput{
			Round: round, EvaluationContract: DirectEvaluationContractVersion,
			ReviewContract: DirectReviewContractVersion, Results: batch,
		}
		value, usage, calls, err := executor.call(ctx, "review", fmt.Sprintf("review-%03d", round), func(callCtx context.Context) (any, ModelUsage, error) {
			review, callUsage, callErr := executor.model.ReviewReplies(callCtx, executor.opts.SalonID, input)
			return review, callUsage, callErr
		})
		if err != nil {
			review := DirectReviewRound{Round: round, ModelCalls: calls, Usage: usage, Errors: []string{err.Error()}}
			for _, result := range batch {
				review.ScenarioIDs = append(review.ScenarioIDs, result.ScenarioID)
			}
			report.ReviewRounds = append(report.ReviewRounds, review)
			if saveErr := executor.checkpoint.Save(ctx, *report); saveErr != nil {
				return saveErr
			}
			return err
		}
		review := value.(DirectReviewRound)
		review.Round = round
		review.ModelCalls = calls
		review.Usage = usage
		if len(review.ScenarioIDs) == 0 {
			for _, result := range batch {
				review.ScenarioIDs = append(review.ScenarioIDs, result.ScenarioID)
			}
		}
		report.ReviewRounds = append(report.ReviewRounds, review)
		if err := executor.checkpoint.Save(ctx, *report); err != nil {
			return err
		}
	}
	return nil
}

type directServiceReplyGenerator struct {
	executor        *directCallExecutor
	scenarioID      string
	modelCalls      int
	usage           ModelUsage
	duration        time.Duration
	errors          []error
	budgetExhausted bool
}

func (g *directServiceReplyGenerator) GenerateReply(ctx context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	started := time.Now()
	value, usage, calls, err := g.executor.call(ctx, "reply", g.scenarioID, func(callCtx context.Context) (any, ModelUsage, error) {
		reply, callUsage, callErr := g.executor.model.GenerateReply(callCtx, request)
		return reply, callUsage, callErr
	})
	g.duration += time.Since(started)
	g.modelCalls += calls
	g.usage.Add(usage)
	g.recordError(err)
	if err != nil {
		return conversation.ReplyGenerationResult{}, err
	}
	return value.(conversation.ReplyGenerationResult), nil
}

func (g *directServiceReplyGenerator) GenerateConsultationQuestion(ctx context.Context, request conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, error) {
	started := time.Now()
	value, usage, calls, err := g.executor.call(ctx, "consultation_question", g.scenarioID, func(callCtx context.Context) (any, ModelUsage, error) {
		reply, callUsage, callErr := g.executor.model.GenerateConsultationQuestion(callCtx, request)
		return reply, callUsage, callErr
	})
	g.duration += time.Since(started)
	g.modelCalls += calls
	g.usage.Add(usage)
	g.recordError(err)
	if err != nil {
		return conversation.ReplyGenerationResult{}, err
	}
	return value.(conversation.ReplyGenerationResult), nil
}

func (g *directServiceReplyGenerator) recordError(err error) {
	if err == nil {
		return
	}
	g.errors = append(g.errors, err)
	if errors.Is(err, ErrModelCallBudgetExhausted) {
		g.budgetExhausted = true
	}
}

func validateBackendResult(scenario Scenario, result BackendTurnResult) []string {
	problems := make([]string, 0)
	for _, attempt := range result.WouldCallTools {
		if attempt.SideEffect && !attempt.Blocked {
			problems = append(problems, "backend allowed a side effect during no-side-effect evaluation")
		}
	}
	if result.Evidence.BookingConfirmed && !result.Evidence.ProviderBookingIDPresent {
		problems = append(problems, "backend marked booking confirmed without a provider booking ID")
	}
	if scenario.Expected.Safety.Concern && !result.Evidence.HandoffRequested {
		problems = append(problems, "backend did not hand off a validated safety concern")
	}
	if scenario.Expected.GuidanceAction == conversation.GuidanceActionBook &&
		result.Evidence.IntentAfter != conversation.IntentBooking {
		problems = append(problems, "backend did not enter the booking workflow for a validated booking action")
	}
	if scenario.Expected.GuidanceAction == conversation.GuidanceActionConsultation &&
		result.Evidence.IntentAfter != conversation.IntentConsultation && !result.Evidence.HandoffRequested {
		problems = append(problems, "backend did not enter consultation or a safe handoff for a validated consultation action")
	}
	if scenario.Expected.AvailabilityIntent && result.Evidence.InterpreterOutcome != conversation.TurnInterpreterOutcomeAccepted {
		problems = append(problems, "backend did not retain the accepted semantic availability interpretation")
	}
	if problem := validateOfferedSlotsAgainstTimePreference(result.Evidence); problem != "" {
		problems = append(problems, problem)
	}
	return problems
}

func validateOfferedSlotsAgainstTimePreference(evidence BackendEvidence) string {
	direction := strings.TrimSpace(evidence.TimePreferenceDirection)
	preference := evidence.TimePreferenceMinutes
	if direction == "" || preference < 0 || len(evidence.OfferedSlotLocalMinutes) == 0 {
		return ""
	}
	if preference >= 24*60 {
		return fmt.Sprintf("backend retained invalid time preference minutes=%d", preference)
	}
	for _, offered := range evidence.OfferedSlotLocalMinutes {
		matches := false
		switch direction {
		case conversation.TimePreferenceAfter:
			matches = offered > preference
		case conversation.TimePreferenceBefore:
			matches = offered < preference
		case conversation.TimePreferenceExact:
			matches = offered == preference
		default:
			return fmt.Sprintf("backend retained invalid time preference direction=%q", direction)
		}
		if !matches {
			return fmt.Sprintf(
				"backend offered local minute=%d outside %s constraint minute=%d timezone=%q",
				offered, direction, preference, evidence.TimePreferenceTimezone,
			)
		}
	}
	return ""
}

type directCallExecutor struct {
	model      DirectModel
	checkpoint CheckpointStore
	report     *EvaluationReport
	opts       DirectRunOptions
}

func (e *directCallExecutor) call(ctx context.Context, stage string, scenarioID string, fn func(context.Context) (any, ModelUsage, error)) (any, ModelUsage, int, error) {
	var totalUsage ModelUsage
	for attempt := 0; attempt <= e.opts.TransientRetries; attempt++ {
		if e.report.ModelCallCount >= e.report.ModelCallBudget {
			return nil, totalUsage, attempt, ErrModelCallBudgetExhausted
		}
		e.report.ModelCallCount++
		e.report.InFlightModelCall = &InFlightModelCall{
			Stage: stage, ScenarioID: scenarioID, Attempt: attempt + 1,
			StartedAt: e.opts.Now().Format(time.RFC3339Nano),
		}
		if err := e.checkpoint.Save(ctx, *e.report); err != nil {
			return nil, totalUsage, attempt, fmt.Errorf("reserve model call: %w", err)
		}
		callCtx, cancel := context.WithTimeout(ctx, e.opts.RequestTimeout)
		value, usage, err := fn(callCtx)
		cancel()
		e.report.InFlightModelCall = nil
		e.report.Usage.Add(usage)
		totalUsage.Add(usage)
		if err := e.checkpoint.Save(ctx, *e.report); err != nil {
			return nil, totalUsage, attempt + 1, fmt.Errorf("complete model call checkpoint: %w", err)
		}
		if err == nil {
			if attempt > 0 {
				e.report.RecoveredTransientCount++
			}
			return value, totalUsage, attempt + 1, nil
		}
		if attempt >= e.opts.TransientRetries || !isTransientDirectModelError(err) {
			return nil, totalUsage, attempt + 1, err
		}
		e.report.TransientRetryCount++
	}
	return nil, totalUsage, 0, errors.New("unreachable model call state")
}

func isTransientDirectModelError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var providerErr *voice.ProviderRequestError
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode == 408 || providerErr.StatusCode == 409 || providerErr.StatusCode == 429 || providerErr.StatusCode >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary())
}

func validateDirectReply(reply string, forbiddenIdentifiers []string) []string {
	reply = strings.TrimSpace(reply)
	problems := make([]string, 0)
	if reply == "" {
		return []string{"final reply is empty"}
	}
	if strings.Count(reply, "?") > 1 {
		problems = append(problems, "final reply asks more than one question")
	}
	lowerReply := strings.ToLower(reply)
	seen := make(map[string]bool, len(forbiddenIdentifiers))
	for _, identifier := range forbiddenIdentifiers {
		identifier = strings.TrimSpace(identifier)
		normalized := strings.ToLower(identifier)
		if len(identifier) >= 4 && !seen[normalized] && containsDelimitedIdentifier(lowerReply, normalized) {
			problems = append(problems, "final reply exposes internal identifier "+identifier)
		}
		seen[normalized] = true
	}
	return problems
}

func containsDelimitedIdentifier(lowerReply string, normalizedIdentifier string) bool {
	pattern := `(^|[^a-z0-9])` + regexp.QuoteMeta(normalizedIdentifier) + `([^a-z0-9]|$)`
	return regexp.MustCompile(pattern).MatchString(lowerReply)
}

func directReplyForbiddenIdentifiers(request voice.SemanticEvaluationRequest, actual voice.TurnModelReply) []string {
	identifiers := make([]string, 0)
	for _, service := range request.CatalogServices {
		identifiers = append(identifiers, service.ServiceID, service.CategoryID)
	}
	for _, category := range request.CatalogCategories {
		identifiers = append(identifiers, category.CategoryID)
		identifiers = append(identifiers, category.ServiceIDs...)
	}
	for _, staff := range request.CatalogStaff {
		identifiers = append(identifiers, staff.StaffID)
	}
	identifiers = append(identifiers, request.CurrentDraft.ServiceIDs...)
	identifiers = append(identifiers, request.CurrentDraft.StaffID)
	for _, group := range request.CurrentDraft.PartyGroups {
		identifiers = append(identifiers, group.GuestRef)
		identifiers = append(identifiers, group.ServiceIDs...)
	}
	for _, act := range actual.Acts {
		identifiers = append(identifiers, act.SourceIDs...)
		identifiers = append(identifiers, act.TargetIDs...)
		identifiers = append(identifiers, act.SourceCategoryID, act.TargetCategoryID, act.GuestRef)
	}
	return identifiers
}

func recalculateDirectSummary(report *EvaluationReport) {
	if report == nil {
		return
	}
	report.ModelEvaluatedCount = 0
	report.PassedCount = 0
	report.FailedCount = 0
	report.NotRunCount = 0
	report.ReviewPassedCount = 0
	report.ReviewFailedCount = 0
	report.Failures = nil
	for _, result := range report.Results {
		if result.Actual != nil {
			report.ModelEvaluatedCount++
		}
		switch result.Status {
		case "passed":
			report.PassedCount++
		case "not_run_budget_exhausted":
			report.NotRunCount++
		default:
			report.FailedCount++
			report.Failures = append(report.Failures, EvaluationFailure{ScenarioID: result.ScenarioID, Errors: append([]string(nil), result.Errors...), Actual: result.Actual})
		}
	}
	if missing := report.ScenarioCount - len(report.Results); missing > 0 {
		report.NotRunCount += missing
	}
	for _, review := range report.ReviewRounds {
		if review.Passed {
			report.ReviewPassedCount++
		} else {
			report.ReviewFailedCount++
		}
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

type JSONCheckpointStore struct {
	Path string
}

func (s JSONCheckpointStore) Load(ctx context.Context) (EvaluationReport, bool, error) {
	var report EvaluationReport
	if err := ctx.Err(); err != nil {
		return report, false, err
	}
	file, err := os.Open(strings.TrimSpace(s.Path))
	if errors.Is(err, os.ErrNotExist) {
		return report, false, nil
	}
	if err != nil {
		return report, false, err
	}
	defer file.Close()
	if err := json.NewDecoder(io.LimitReader(file, 32*1024*1024)).Decode(&report); err != nil {
		return report, false, err
	}
	return report, true, nil
}

func (s JSONCheckpointStore) Save(ctx context.Context, report EvaluationReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := strings.TrimSpace(s.Path)
	if path == "" {
		return errors.New("checkpoint path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temporary, err := os.CreateTemp(dir, ".conversation-eval-checkpoint-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
