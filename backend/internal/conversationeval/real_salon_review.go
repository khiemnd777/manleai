package conversationeval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type RealSalonCheckpointStore interface {
	Load(ctx context.Context) (RealSalonReport, bool, error)
	Save(ctx context.Context, report RealSalonReport) error
}

type RealSalonLiveOptions struct {
	SalonID          string
	ModelCallBudget  int
	RequestTimeout   time.Duration
	TransientRetries int
	Now              func() time.Time
}

func RunRealSalonLive(ctx context.Context, corpus RealSalonCorpus, model DirectModel, checkpoint RealSalonCheckpointStore, opts RealSalonLiveOptions) (RealSalonReport, error) {
	if problems := ValidateRealSalonCorpus(corpus); len(problems) != 0 {
		return RealSalonReport{}, fmt.Errorf("real-salon corpus validation failed: %s", problems[0])
	}
	if model == nil || checkpoint == nil || strings.TrimSpace(opts.SalonID) == "" {
		return RealSalonReport{}, errors.New("real-salon live evaluation configuration is invalid")
	}
	if opts.ModelCallBudget <= 0 || opts.ModelCallBudget > RealSalonLiveModelCallLimit {
		return RealSalonReport{}, fmt.Errorf("model-call budget must be between 1 and %d", RealSalonLiveModelCallLimit)
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 45 * time.Second
	}
	if opts.RequestTimeout < time.Second || opts.RequestTimeout > 2*time.Minute {
		return RealSalonReport{}, errors.New("request timeout must be between 1s and 2m")
	}
	if opts.TransientRetries < 0 || opts.TransientRetries > 3 {
		return RealSalonReport{}, errors.New("transient retries must be between 0 and 3")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	identity, err := model.Identity(ctx, opts.SalonID)
	if err != nil {
		return RealSalonReport{}, fmt.Errorf("resolve real-salon model identity: %w", err)
	}
	liveRunIdentity := fmt.Sprintf("live_canary_production_conversation_service_no_side_effects|timeout=%s|retries=%d", opts.RequestTimeout, opts.TransientRetries)
	runKey, err := RealSalonRunKey(corpus, liveRunIdentity, identity)
	if err != nil {
		return RealSalonReport{}, err
	}
	report, found, err := checkpoint.Load(ctx)
	if err != nil {
		return RealSalonReport{}, fmt.Errorf("load real-salon checkpoint: %w", err)
	}
	if found {
		if report.RunKey != runKey {
			return report, fmt.Errorf("checkpoint run_key=%q does not match %q", report.RunKey, runKey)
		}
		if report.InFlightModelCall != nil {
			return report, fmt.Errorf("%w: stage=%s scenario=%s attempt=%d", ErrUncertainModelCall, report.InFlightModelCall.Stage, report.InFlightModelCall.ScenarioID, report.InFlightModelCall.Attempt)
		}
		if opts.ModelCallBudget < report.ModelCallCount {
			return report, fmt.Errorf("model-call budget %d is below already-used count %d", opts.ModelCallBudget, report.ModelCallCount)
		}
		if strings.TrimSpace(report.CompletedAt) != "" {
			return report, nil
		}
		for _, result := range report.Results {
			if result.Status == RealSalonStatusFailed {
				recalculateRealSalonReport(&report)
				report.OverallStatus = RealSalonStatusFailed
				report.CompletedAt = opts.Now().Format(time.RFC3339Nano)
				_ = checkpoint.Save(ctx, report)
				return report, nil
			}
		}
		report.ModelCallBudget = opts.ModelCallBudget
	} else {
		report = RealSalonReport{
			SchemaVersion: corpus.SchemaVersion, ContractVersion: corpus.ContractVersion,
			EvaluationContract: RealSalonEvaluationContract, ReviewContract: RealSalonReviewContract + "+" + DirectReviewContractVersion,
			Mode: "live_canary_production_conversation_service_no_side_effects", RunKey: runKey,
			OverallStatus: "model_execution_in_progress", JourneyCount: len(corpus.Journeys),
			StructuralValidated: len(corpus.Journeys), NotRun: RealSalonLiveCanaryJourneys,
			ModelCallBudget: opts.ModelCallBudget, RequestTimeoutMS: opts.RequestTimeout.Milliseconds(),
			TransientRetries: opts.TransientRetries, StartedAt: opts.Now().Format(time.RFC3339Nano),
		}
	}
	executor := &realSalonModelExecutor{ctx: ctx, model: model, checkpoint: checkpoint, report: &report, opts: opts}
	completed := map[string]bool{}
	for _, result := range report.Results {
		if result.Status == RealSalonStatusModelExecuted || result.Status == RealSalonStatusReviewPassed {
			completed[result.JourneyID] = true
		}
	}
	for _, journey := range corpus.Journeys {
		if !journey.LiveCanary || completed[journey.ID] {
			continue
		}
		result := runRealSalonJourney(ctx, corpus, journey, true, executor)
		report.Results = append(report.Results, result)
		recalculateRealSalonReport(&report)
		if result.Status == RealSalonStatusFailed {
			report.OverallStatus = RealSalonStatusFailed
			report.CompletedAt = opts.Now().Format(time.RFC3339Nano)
		}
		if saveErr := checkpoint.Save(ctx, report); saveErr != nil {
			return report, saveErr
		}
		// Do not spend more calls after any production-state, grounding, safety,
		// or reply contract failure. The remaining canaries stay explicitly not run.
		if result.Status == RealSalonStatusFailed {
			return report, nil
		}
	}
	if report.ModelExecuted != RealSalonLiveCanaryJourneys {
		report.OverallStatus = "model_execution_incomplete"
		recalculateRealSalonReport(&report)
		_ = checkpoint.Save(ctx, report)
		return report, nil
	}
	if err := reviewRealSalonCanaries(ctx, &report, executor); err != nil {
		if errors.Is(err, ErrModelCallBudgetExhausted) {
			report.OverallStatus = "review_not_run_budget_exhausted"
			recalculateRealSalonReport(&report)
			_ = checkpoint.Save(ctx, report)
			return report, nil
		}
		return report, err
	}
	recalculateRealSalonReport(&report)
	if report.ReviewPassed == RealSalonLiveCanaryJourneys && report.Failed == 0 {
		report.Passed = true
		report.OverallStatus = RealSalonStatusReviewPassed
	} else {
		report.Passed = false
		report.OverallStatus = RealSalonStatusFailed
	}
	report.CompletedAt = opts.Now().Format(time.RFC3339Nano)
	if err := checkpoint.Save(ctx, report); err != nil {
		return report, err
	}
	return report, nil
}

type realSalonModelExecutor struct {
	ctx        context.Context
	model      DirectModel
	checkpoint RealSalonCheckpointStore
	report     *RealSalonReport
	opts       RealSalonLiveOptions
	turnCalls  int
	turnUsage  ModelUsage
	turnErrors []error
}

func (e *realSalonModelExecutor) Interpret(ctx context.Context, journeyID string, turn int, request conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, int, ModelUsage, error) {
	semantic := voice.SemanticEvaluationRequest{
		ScenarioID: fmt.Sprintf("%s-turn-%03d", journeyID, turn), Channel: request.Channel,
		CustomerMessage: request.CustomerMessage, ExpectedInput: request.ExpectedInput,
		SemanticContract: request.SemanticContract, RecognizableGuidanceActions: append([]string(nil), request.RecognizableGuidanceActions...),
		SelectedServices: append([]conversation.ConversationServiceRef(nil), request.SelectedServices...), CatalogServices: append([]conversation.ConversationServiceRef(nil), request.CatalogServices...),
		CatalogServiceAliases: append([]conversation.ConversationServiceAliasRef(nil), request.CatalogServiceAliases...), CatalogCategories: append([]conversation.ConversationCategoryRef(nil), request.CatalogCategories...),
		SelectedStaff: append([]conversation.ConversationStaffRef(nil), request.SelectedStaff...), CatalogStaff: append([]conversation.ConversationStaffRef(nil), request.CatalogStaff...),
		Pending: request.Pending, CurrentBookingStage: request.CurrentBookingStage, BookingAction: request.BookingAction,
		CurrentDraft: request.CurrentDraft, Consultation: request.Consultation,
	}
	value, usage, calls, err := e.call(ctx, "recognition", semantic.ScenarioID, func(callCtx context.Context) (any, ModelUsage, error) {
		reply, callUsage, callErr := e.model.InterpretTurn(callCtx, e.opts.SalonID, semantic)
		return reply, callUsage, callErr
	})
	e.turnCalls += calls
	e.turnUsage.Add(usage)
	if err != nil {
		e.turnErrors = append(e.turnErrors, err)
		return conversation.TurnUnderstanding{}, calls, usage, realSalonTurnInterpreterError(err)
	}
	return voice.TurnUnderstandingFromModelReply(value.(voice.TurnModelReply)), calls, usage, nil
}

func (e *realSalonModelExecutor) Replies(journeyID string, turn int) conversation.ReplyGenerator {
	return &realSalonLiveReplyGenerator{executor: e, scenarioID: fmt.Sprintf("%s-turn-%03d", journeyID, turn)}
}

func (e *realSalonModelExecutor) ReplyEvidence() (int, ModelUsage, []error) {
	calls, usage, callErrors := e.turnCalls, e.turnUsage, append([]error(nil), e.turnErrors...)
	e.turnCalls = 0
	e.turnUsage = ModelUsage{}
	e.turnErrors = nil
	return calls, usage, callErrors
}

func (e *realSalonModelExecutor) call(ctx context.Context, stage, scenarioID string, fn func(context.Context) (any, ModelUsage, error)) (any, ModelUsage, int, error) {
	var total ModelUsage
	for attempt := 1; attempt <= e.opts.TransientRetries+1; attempt++ {
		if e.report.ModelCallCount >= e.opts.ModelCallBudget {
			return nil, total, 0, ErrModelCallBudgetExhausted
		}
		e.report.InFlightModelCall = &InFlightModelCall{Stage: stage, ScenarioID: scenarioID, Attempt: attempt, StartedAt: e.opts.Now().Format(time.RFC3339Nano)}
		if err := e.checkpoint.Save(e.ctx, *e.report); err != nil {
			return nil, total, 0, err
		}
		callCtx, cancel := context.WithTimeout(ctx, e.opts.RequestTimeout)
		value, usage, err := fn(callCtx)
		cancel()
		e.report.InFlightModelCall = nil
		e.report.ModelCallCount++
		e.report.Usage.Add(usage)
		total.Add(usage)
		if saveErr := e.checkpoint.Save(e.ctx, *e.report); saveErr != nil {
			return nil, total, attempt, saveErr
		}
		if err == nil {
			if attempt > 1 {
				e.report.RecoveredTransientCount++
			}
			return value, total, attempt, nil
		}
		if !isTransientDirectModelError(err) || attempt > e.opts.TransientRetries {
			return nil, total, attempt, err
		}
		e.report.TransientRetryCount++
	}
	return nil, total, e.opts.TransientRetries + 1, errors.New("model call retry loop exhausted")
}

type realSalonLiveReplyGenerator struct {
	executor   *realSalonModelExecutor
	scenarioID string
}

func (g *realSalonLiveReplyGenerator) GenerateReply(ctx context.Context, request conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	value, usage, calls, err := g.executor.call(ctx, "reply", g.scenarioID, func(callCtx context.Context) (any, ModelUsage, error) {
		reply, callUsage, callErr := g.executor.model.GenerateReply(callCtx, request)
		return reply, callUsage, callErr
	})
	g.executor.turnCalls += calls
	g.executor.turnUsage.Add(usage)
	if err != nil {
		g.executor.turnErrors = append(g.executor.turnErrors, err)
		return conversation.ReplyGenerationResult{}, err
	}
	return value.(conversation.ReplyGenerationResult), nil
}

func (g *realSalonLiveReplyGenerator) GenerateConsultationQuestion(ctx context.Context, request conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, error) {
	value, usage, calls, err := g.executor.call(ctx, "consultation_question", g.scenarioID, func(callCtx context.Context) (any, ModelUsage, error) {
		reply, callUsage, callErr := g.executor.model.GenerateConsultationQuestion(callCtx, request)
		return reply, callUsage, callErr
	})
	g.executor.turnCalls += calls
	g.executor.turnUsage.Add(usage)
	if err != nil {
		g.executor.turnErrors = append(g.executor.turnErrors, err)
		return conversation.ReplyGenerationResult{}, err
	}
	return value.(conversation.ReplyGenerationResult), nil
}

func reviewRealSalonCanaries(ctx context.Context, report *RealSalonReport, executor *realSalonModelExecutor) error {
	completedRounds := len(report.ReviewRounds)
	for start, round := completedRounds*DirectReviewBatchSize, completedRounds+1; start < len(report.Results); start, round = start+DirectReviewBatchSize, round+1 {
		end := start + DirectReviewBatchSize
		if end > len(report.Results) {
			end = len(report.Results)
		}
		batch := make([]ScenarioEvaluationResult, 0, end-start)
		for _, journey := range report.Results[start:end] {
			batch = append(batch, flattenRealSalonJourneyForReview(journey))
		}
		input := DirectReviewInput{
			Round: round, EvaluationContract: report.EvaluationContract, ReviewContract: report.ReviewContract,
			MultiTurn: true, Results: batch, JourneyResults: append([]RealSalonJourneyResult(nil), report.Results[start:end]...),
		}
		value, usage, calls, err := executor.call(ctx, "review", fmt.Sprintf("real-salon-review-%02d", round), func(callCtx context.Context) (any, ModelUsage, error) {
			review, callUsage, callErr := executor.model.ReviewReplies(callCtx, executor.opts.SalonID, input)
			return review, callUsage, callErr
		})
		if err != nil {
			if errors.Is(err, ErrModelCallBudgetExhausted) {
				return err
			}
			review := DirectReviewRound{Round: round, ModelCalls: calls, Usage: usage, Errors: []string{err.Error()}}
			for index := start; index < end; index++ {
				review.ScenarioIDs = append(review.ScenarioIDs, report.Results[index].JourneyID)
				report.Results[index].Status = RealSalonStatusFailed
				report.Results[index].Errors = append(report.Results[index].Errors, "model transcript review: "+err.Error())
			}
			report.ReviewRounds = append(report.ReviewRounds, review)
			return executor.checkpoint.Save(ctx, *report)
		}
		review := value.(DirectReviewRound)
		review.ModelCalls = calls
		review.Usage = usage
		report.ReviewRounds = append(report.ReviewRounds, review)
		for index := start; index < end; index++ {
			report.Results[index].ReviewPassed = review.Passed
			if review.Passed {
				report.Results[index].Status = RealSalonStatusReviewPassed
			} else {
				report.Results[index].Status = RealSalonStatusFailed
				report.Results[index].Errors = append(report.Results[index].Errors, "model transcript review failed")
			}
		}
		if err := executor.checkpoint.Save(ctx, *report); err != nil {
			return err
		}
		if !review.Passed {
			return nil
		}
	}
	return nil
}

func flattenRealSalonJourneyForReview(journey RealSalonJourneyResult) ScenarioEvaluationResult {
	lines := make([]string, 0, len(journey.Turns)*2)
	for _, turn := range journey.Turns {
		lines = append(lines, fmt.Sprintf("CUSTOMER TURN %d: %s", turn.Turn, turn.CustomerMessage))
		lines = append(lines, fmt.Sprintf("AI TURN %d: %s", turn.Turn, turn.AIReply))
	}
	last := RealSalonTurnResult{}
	if len(journey.Turns) > 0 {
		last = journey.Turns[len(journey.Turns)-1]
	}
	return ScenarioEvaluationResult{
		ScenarioID: journey.JourneyID, Status: journey.Status,
		CustomerMessage:  "Review the complete interleaved multi-turn transcript in final_reply.",
		BackendSafeReply: strings.Join(lines, "\n"), FinalReply: strings.Join(lines, "\n"),
		BackendEvidence: BackendEvidence{IntentAfter: last.IntentAfter, DialogPhaseAfter: last.PhaseAfter, SelectedServicesAfter: last.SelectedServiceIDs, HandoffRequested: last.HandoffRequested, BookingConfirmed: last.BookingConfirmed, ProviderBookingIDPresent: last.ProviderBookingIDPresent},
		WouldCallTools:  append([]ToolAttempt(nil), last.WouldCallTools...), Errors: append([]string(nil), journey.Errors...),
	}
}

func recalculateRealSalonReport(report *RealSalonReport) {
	report.RuntimeExecuted = 0
	report.ModelExecuted = 0
	report.ReviewPassed = 0
	report.Failed = 0
	for _, result := range report.Results {
		switch result.Status {
		case RealSalonStatusRuntimeExecuted:
			report.RuntimeExecuted++
		case RealSalonStatusModelExecuted:
			report.ModelExecuted++
		case RealSalonStatusReviewPassed:
			report.ModelExecuted++
			report.ReviewPassed++
		case RealSalonStatusFailed:
			report.Failed++
			if result.ModelExecuted {
				report.ModelExecuted++
			}
		}
	}
	report.NotRun = RealSalonLiveCanaryJourneys - len(report.Results)
	if report.NotRun < 0 {
		report.NotRun = 0
	}
	if report.ModelExecuted < RealSalonLiveCanaryJourneys || report.ReviewPassed < RealSalonLiveCanaryJourneys || report.Failed > 0 {
		report.Passed = false
	}
}

func realSalonTurnInterpreterError(err error) error {
	outcome := conversation.TurnInterpreterOutcomeProviderError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		outcome = conversation.TurnInterpreterOutcomeTimeout
	case errors.Is(err, voice.ErrProviderDisabled):
		outcome = conversation.TurnInterpreterOutcomeProviderDisabled
	case errors.Is(err, voice.ErrTurnModelEmptyOutput):
		outcome = conversation.TurnInterpreterOutcomeEmptyOutput
	case errors.Is(err, voice.ErrTurnModelInvalidOutput):
		outcome = conversation.TurnInterpreterOutcomeSchemaInvalid
	}
	return conversation.NewTurnInterpreterError(outcome, err)
}

type JSONRealSalonCheckpointStore struct {
	Path string
}

func (s JSONRealSalonCheckpointStore) Load(ctx context.Context) (RealSalonReport, bool, error) {
	var report RealSalonReport
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

func (s JSONRealSalonCheckpointStore) Save(ctx context.Context, report RealSalonReport) error {
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
	temporary, err := os.CreateTemp(dir, ".real-salon-eval-checkpoint-*")
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
