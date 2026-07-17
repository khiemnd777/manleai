package conversationeval

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

const (
	SchemaVersion         = 2
	RequiredScenarioCount = 1000
	RequiredReviewRounds  = 100
	PilotScenarioCount    = 50
	PilotReviewRounds     = 10
	DirectReviewBatchSize = 5

	// These versions are part of the checkpoint/run identity. Any material
	// change to production-flow execution or reviewer semantics must bump the
	// corresponding value so retained paid evidence cannot be mixed.
	DirectEvaluationContractVersion = "production-flow-v8"
	DirectReviewContractVersion     = "evidence-review-v9"
)

type Corpus struct {
	SchemaVersion         int                       `json:"schema_version"`
	TaxonomyRelease       string                    `json:"taxonomy_release"`
	ExpectedScenarioCount int                       `json:"expected_scenario_count"`
	ExpectedReviewRounds  int                       `json:"expected_review_rounds"`
	CatalogFixtures       map[string]CatalogFixture `json:"catalog_fixtures"`
	Scenarios             []Scenario                `json:"scenarios"`
}

type CatalogFixture struct {
	Services      []conversation.ConversationServiceRef      `json:"services"`
	Aliases       []conversation.ConversationServiceAliasRef `json:"aliases"`
	Categories    []conversation.ConversationCategoryRef     `json:"categories"`
	Staff         []conversation.ConversationStaffRef        `json:"staff"`
	BusinessHours []BusinessHourFixture                      `json:"business_hours,omitempty"`
}

type BusinessHourFixture struct {
	ID             string `json:"id"`
	DayOfWeek      int    `json:"day_of_week"`
	StartLocalTime string `json:"start_local_time"`
	EndLocalTime   string `json:"end_local_time"`
}

type Scenario struct {
	ID             string                          `json:"id"`
	Family         string                          `json:"family"`
	Description    string                          `json:"description"`
	EvidenceLevel  string                          `json:"evidence_level"`
	Provenance     ScenarioProvenance              `json:"provenance"`
	CatalogFixture string                          `json:"catalog_fixture"`
	Request        voice.SemanticEvaluationRequest `json:"request"`
	Expected       ExpectedResult                  `json:"expected"`
	Invariants     []string                        `json:"invariants"`
}

type ScenarioProvenance struct {
	BaseCaseID       string `json:"base_case_id"`
	UtteranceVariant string `json:"utterance_variant"`
	Generated        bool   `json:"generated"`
	Scope            string `json:"scope"`
}

type ExpectedResult struct {
	Goal                    string                   `json:"goal,omitempty"`
	ForbiddenGoals          []string                 `json:"forbidden_goals,omitempty"`
	GuidanceAction          string                   `json:"guidance_action,omitempty"`
	GuidanceCatalogMode     string                   `json:"guidance_catalog_mode,omitempty"`
	GuidanceQuestionSubject string                   `json:"guidance_question_subject,omitempty"`
	AlternativeGuidance     []ExpectedGuidanceOption `json:"alternative_guidance,omitempty"`
	GuidancePartySize       int                      `json:"guidance_party_size,omitempty"`
	CurrentBookingSummary   bool                     `json:"current_booking_summary,omitempty"`
	AvailabilityIntent      bool                     `json:"availability_intent,omitempty"`
	RequiredActs            []ExpectedAct            `json:"required_acts,omitempty"`
	RequiredQuestions       []ExpectedQuestion       `json:"required_questions,omitempty"`
	Consultation            ExpectedConsultation     `json:"consultation,omitempty"`
	Safety                  ExpectedSafety           `json:"safety"`
}

type ExpectedGuidanceOption struct {
	Action          string `json:"action"`
	CatalogMode     string `json:"catalog_mode,omitempty"`
	QuestionSubject string `json:"question_subject,omitempty"`
}

type ExpectedAct struct {
	Kind             string   `json:"kind"`
	AlternativeKinds []string `json:"alternative_kinds,omitempty"`
	Entity           string   `json:"entity,omitempty"`
	SourceIDs        []string `json:"source_ids,omitempty"`
	TargetIDs        []string `json:"target_ids,omitempty"`
	GuestScope       string   `json:"guest_scope,omitempty"`
	GuestRef         string   `json:"guest_ref,omitempty"`
	Subject          string   `json:"subject,omitempty"`
}

type ExpectedQuestion struct {
	Subject string `json:"subject"`
	Mode    string `json:"mode,omitempty"`
}

type ExpectedConsultation struct {
	CurrentSystem        string   `json:"current_system,omitempty"`
	DesiredOutcome       string   `json:"desired_outcome,omitempty"`
	LengthChange         string   `json:"length_change,omitempty"`
	Priorities           []string `json:"priorities,omitempty"`
	Finishes             []string `json:"finishes,omitempty"`
	BookingRequested     bool     `json:"booking_requested,omitempty"`
	ConversationComplete bool     `json:"conversation_complete,omitempty"`
}

type ExpectedSafety struct {
	Checked               bool     `json:"checked"`
	Concern               bool     `json:"concern"`
	Category              string   `json:"category,omitempty"`
	AlternativeCategories []string `json:"alternative_categories,omitempty"`
}

type ReviewReport struct {
	SchemaVersion int           `json:"schema_version"`
	Scope         string        `json:"scope"`
	ScenarioCount int           `json:"scenario_count"`
	RoundCount    int           `json:"round_count"`
	Passed        bool          `json:"passed"`
	Rounds        []ReviewRound `json:"rounds"`
}

type ReviewRound struct {
	Round       int            `json:"round"`
	Dimension   string         `json:"dimension"`
	Question    string         `json:"question"`
	Answer      string         `json:"answer"`
	Critique    string         `json:"critique"`
	Resolution  string         `json:"resolution"`
	ScenarioIDs []string       `json:"scenario_ids"`
	Evidence    ReviewEvidence `json:"evidence"`
	Checks      []ReviewCheck  `json:"checks"`
	Passed      bool           `json:"passed"`
	Errors      []string       `json:"errors,omitempty"`
}

type ReviewEvidence struct {
	CheckedScenarioCount  int      `json:"checked_scenario_count"`
	PhoneCount            int      `json:"phone_count"`
	SimulatorCount        int      `json:"simulator_count"`
	GuidanceContractCount int      `json:"guidance_contract_count"`
	FullContractCount     int      `json:"full_contract_count"`
	Families              []string `json:"families"`
	CatalogFixtures       []string `json:"catalog_fixtures"`
	BaseCaseIDs           []string `json:"base_case_ids"`
}

type ReviewCheck struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

type EvaluationFailure struct {
	ScenarioID string                `json:"scenario_id"`
	Errors     []string              `json:"errors"`
	Actual     *voice.TurnModelReply `json:"actual,omitempty"`
}

type EvaluationReport struct {
	SchemaVersion            int                        `json:"schema_version"`
	EvaluationContract       string                     `json:"evaluation_contract,omitempty"`
	ReviewContract           string                     `json:"review_contract,omitempty"`
	Mode                     string                     `json:"mode"`
	ContextSource            string                     `json:"context_source"`
	RuntimeReadinessVerified bool                       `json:"runtime_readiness_verified"`
	RuntimePreflight         *RuntimePreflightEvidence  `json:"runtime_preflight,omitempty"`
	RunKey                   string                     `json:"run_key,omitempty"`
	ScenarioCount            int                        `json:"scenario_count"`
	ContractValidatedCount   int                        `json:"contract_validated_count"`
	ModelEvaluatedCount      int                        `json:"model_evaluated_count"`
	PassedCount              int                        `json:"passed_count"`
	FailedCount              int                        `json:"failed_count"`
	NotRunCount              int                        `json:"not_run_count"`
	TransientRetryCount      int                        `json:"transient_retry_count,omitempty"`
	RecoveredTransientCount  int                        `json:"recovered_transient_count,omitempty"`
	ModelCallBudget          int                        `json:"model_call_budget,omitempty"`
	ModelCallCount           int                        `json:"model_call_count,omitempty"`
	ReviewPassedCount        int                        `json:"review_passed_count,omitempty"`
	ReviewFailedCount        int                        `json:"review_failed_count,omitempty"`
	Usage                    ModelUsage                 `json:"usage,omitempty"`
	StartedAt                string                     `json:"started_at,omitempty"`
	CompletedAt              string                     `json:"completed_at,omitempty"`
	InFlightModelCall        *InFlightModelCall         `json:"in_flight_model_call,omitempty"`
	Failures                 []EvaluationFailure        `json:"failures,omitempty"`
	Results                  []ScenarioEvaluationResult `json:"results,omitempty"`
	ReviewRounds             []DirectReviewRound        `json:"review_rounds,omitempty"`
}

// RuntimePreflightEvidence is a zero-model-call database check recorded beside
// direct-model fixture evidence. It proves only the selected salon's current
// readiness fields; it does not make fixture scenario execution a runtime test.
type RuntimePreflightEvidence struct {
	SalonID                  string `json:"salon_id"`
	CheckedAt                string `json:"checked_at"`
	GuidanceServiceCount     int    `json:"guidance_service_count"`
	RecommendationReadyCount int    `json:"recommendation_ready_service_count"`
	ServiceGuidanceStatus    string `json:"service_guidance_status"`
	BookingServiceCount      int    `json:"booking_service_count"`
	ProviderSynced           bool   `json:"provider_synced"`
	BookingReady             bool   `json:"booking_ready"`
	Passed                   bool   `json:"passed"`
}

type ScenarioEvaluationResult struct {
	ScenarioID            string                `json:"scenario_id"`
	Status                string                `json:"status"`
	Channel               string                `json:"channel,omitempty"`
	CustomerMessage       string                `json:"customer_message,omitempty"`
	DurationMS            int64                 `json:"duration_ms,omitempty"`
	RecognitionDurationMS int64                 `json:"recognition_duration_ms,omitempty"`
	ReplyDurationMS       int64                 `json:"reply_duration_ms,omitempty"`
	ModelCalls            int                   `json:"model_calls,omitempty"`
	Usage                 ModelUsage            `json:"usage,omitempty"`
	BackendSafeReply      string                `json:"backend_safe_reply,omitempty"`
	FinalReply            string                `json:"final_reply,omitempty"`
	NextExpectedInput     string                `json:"next_expected_input,omitempty"`
	BackendEvidence       BackendEvidence       `json:"backend_evidence"`
	WouldCallTools        []ToolAttempt         `json:"would_call_tools,omitempty"`
	Resumed               bool                  `json:"resumed,omitempty"`
	Errors                []string              `json:"errors,omitempty"`
	Actual                *voice.TurnModelReply `json:"actual,omitempty"`
}

// BackendEvidence records the production conversation transition that
// generated FinalReply. Reviewer scoring consumes these facts instead of
// inferring booking or handoff state from model recognition fields.
type BackendEvidence struct {
	TurnRoute                string   `json:"turn_route,omitempty"`
	TurnRouteReason          string   `json:"turn_route_reason,omitempty"`
	DeterministicCoverage    string   `json:"deterministic_coverage,omitempty"`
	InterpreterOutcome       string   `json:"interpreter_outcome,omitempty"`
	ReplySource              string   `json:"reply_source,omitempty"`
	ReplyPolicy              string   `json:"reply_policy,omitempty"`
	IntentBefore             string   `json:"intent_before,omitempty"`
	IntentAfter              string   `json:"intent_after,omitempty"`
	OutcomeBefore            string   `json:"outcome_before,omitempty"`
	OutcomeAfter             string   `json:"outcome_after,omitempty"`
	DialogPhaseBefore        string   `json:"dialog_phase_before,omitempty"`
	DialogPhaseAfter         string   `json:"dialog_phase_after,omitempty"`
	SelectedServicesBefore   []string `json:"selected_services_before,omitempty"`
	SelectedServicesAfter    []string `json:"selected_services_after,omitempty"`
	StaffBefore              string   `json:"staff_before,omitempty"`
	StaffAfter               string   `json:"staff_after,omitempty"`
	RequestedDateBefore      string   `json:"requested_date_before,omitempty"`
	RequestedDateAfter       string   `json:"requested_date_after,omitempty"`
	TimePreferenceDirection  string   `json:"time_preference_direction,omitempty"`
	TimePreferenceMinutes    int      `json:"time_preference_minutes,omitempty"`
	TimePreferenceTimezone   string   `json:"time_preference_timezone,omitempty"`
	OfferedSlotLocalMinutes  []int    `json:"offered_slot_local_minutes,omitempty"`
	HandoffRequested         bool     `json:"handoff_requested"`
	HandoffMode              string   `json:"handoff_mode,omitempty"`
	BookingConfirmed         bool     `json:"booking_confirmed"`
	ProviderBookingIDPresent bool     `json:"provider_booking_id_present"`
}

type ModelUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

func (u *ModelUsage) Add(other ModelUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
}

type ToolAttempt struct {
	Tool        string `json:"tool"`
	SideEffect  bool   `json:"side_effect"`
	Blocked     bool   `json:"blocked"`
	Description string `json:"description,omitempty"`
}

type InFlightModelCall struct {
	Stage      string `json:"stage"`
	ScenarioID string `json:"scenario_id,omitempty"`
	Attempt    int    `json:"attempt"`
	StartedAt  string `json:"started_at"`
}

type DirectReviewRound struct {
	Round       int                   `json:"round"`
	ScenarioIDs []string              `json:"scenario_ids"`
	Passed      bool                  `json:"passed"`
	Scores      DirectReviewScores    `json:"scores"`
	Findings    []DirectReviewFinding `json:"findings,omitempty"`
	Summary     string                `json:"summary"`
	ModelCalls  int                   `json:"model_calls"`
	Usage       ModelUsage            `json:"usage,omitempty"`
	Errors      []string              `json:"errors,omitempty"`
}

type DirectReviewScores struct {
	Naturalness      int `json:"naturalness"`
	CatalogGrounding int `json:"catalog_grounding"`
	OneQuestionRule  int `json:"one_question_rule"`
	BookingSafety    int `json:"booking_safety"`
	CallerUsefulness int `json:"caller_usefulness"`
}

type DirectReviewFinding struct {
	ScenarioID     string `json:"scenario_id"`
	Dimension      string `json:"dimension"`
	Problem        string `json:"problem"`
	Recommendation string `json:"recommendation"`
}

func (scenario Scenario) ResolvedRequest(corpus Corpus) (voice.SemanticEvaluationRequest, error) {
	fixture, ok := corpus.CatalogFixtures[scenario.CatalogFixture]
	if !ok {
		return voice.SemanticEvaluationRequest{}, fmt.Errorf("catalog fixture %q does not exist", scenario.CatalogFixture)
	}
	req := scenario.Request
	req.ScenarioID = scenario.ID
	req.CatalogServices = append([]conversation.ConversationServiceRef(nil), fixture.Services...)
	req.CatalogServiceAliases = append([]conversation.ConversationServiceAliasRef(nil), fixture.Aliases...)
	req.CatalogCategories = append([]conversation.ConversationCategoryRef(nil), fixture.Categories...)
	req.CatalogStaff = append([]conversation.ConversationStaffRef(nil), fixture.Staff...)
	return req, nil
}

func ValidateCorpus(corpus Corpus) []string {
	return validateCorpusProfile(corpus, RequiredScenarioCount, RequiredReviewRounds, true)
}

func ValidatePilotCorpus(corpus Corpus) []string {
	return validateCorpusProfile(corpus, PilotScenarioCount, PilotReviewRounds, false)
}

func validateCorpusProfile(corpus Corpus, requiredScenarioCount int, requiredReviewRounds int, allowGenerated bool) []string {
	errorsFound := make([]string, 0)
	if corpus.SchemaVersion != SchemaVersion {
		errorsFound = append(errorsFound, fmt.Sprintf("schema_version=%d, want %d", corpus.SchemaVersion, SchemaVersion))
	}
	if strings.TrimSpace(corpus.TaxonomyRelease) == "" {
		errorsFound = append(errorsFound, "taxonomy_release is required")
	}
	if corpus.ExpectedScenarioCount != requiredScenarioCount || len(corpus.Scenarios) != requiredScenarioCount {
		errorsFound = append(errorsFound, fmt.Sprintf("scenario count=%d expected_field=%d, want %d", len(corpus.Scenarios), corpus.ExpectedScenarioCount, requiredScenarioCount))
	}
	if corpus.ExpectedReviewRounds != requiredReviewRounds {
		errorsFound = append(errorsFound, fmt.Sprintf("expected_review_rounds=%d, want %d", corpus.ExpectedReviewRounds, requiredReviewRounds))
	}
	if len(corpus.CatalogFixtures) < 3 {
		errorsFound = append(errorsFound, "at least three materially different catalog fixtures are required")
	}
	if !allowGenerated {
		for fixtureID, fixture := range corpus.CatalogFixtures {
			if len(fixture.BusinessHours) == 0 {
				errorsFound = append(errorsFound, fmt.Sprintf("catalog fixture %q requires structured business hours", fixtureID))
				continue
			}
			seenPeriods := map[string]bool{}
			for _, period := range fixture.BusinessHours {
				if strings.TrimSpace(period.ID) == "" || seenPeriods[period.ID] || period.DayOfWeek < 0 || period.DayOfWeek > 6 ||
					strings.TrimSpace(period.StartLocalTime) == "" || strings.TrimSpace(period.EndLocalTime) == "" {
					errorsFound = append(errorsFound, fmt.Sprintf("catalog fixture %q has an invalid business-hour period", fixtureID))
					break
				}
				seenPeriods[period.ID] = true
			}
		}
	}
	seenIDs := map[string]bool{}
	seenMessages := map[string]string{}
	channelCounts := map[string]int{}
	familyCounts := map[string]int{}
	for _, scenario := range corpus.Scenarios {
		prefix := scenario.ID + ": "
		if strings.TrimSpace(scenario.ID) == "" || seenIDs[scenario.ID] {
			errorsFound = append(errorsFound, prefix+"scenario id is empty or duplicated")
		}
		seenIDs[scenario.ID] = true
		familyCounts[scenario.Family]++
		channelCounts[scenario.Request.Channel]++
		messageKey := strings.ToLower(strings.Join([]string{scenario.CatalogFixture, scenario.Request.SemanticContract, scenario.Request.Channel, strings.TrimSpace(scenario.Request.CustomerMessage)}, "\x00"))
		if prior := seenMessages[messageKey]; prior != "" {
			errorsFound = append(errorsFound, prefix+"duplicates customer wording and state from "+prior)
		}
		seenMessages[messageKey] = scenario.ID
		if strings.TrimSpace(scenario.Family) == "" || strings.TrimSpace(scenario.Description) == "" {
			errorsFound = append(errorsFound, prefix+"family and description are required")
		}
		if scenario.EvidenceLevel != "semantic_turn_contract" || strings.TrimSpace(scenario.Provenance.BaseCaseID) == "" ||
			strings.TrimSpace(scenario.Provenance.UtteranceVariant) == "" || scenario.Provenance.Scope != "single_turn" {
			errorsFound = append(errorsFound, prefix+"honest single-turn evidence provenance is required")
		}
		if !allowGenerated && scenario.Provenance.Generated {
			errorsFound = append(errorsFound, prefix+"pilot scenarios must use directly authored wording")
		}
		if len(scenario.Invariants) == 0 || !contains(scenario.Invariants, "catalog_bound_semantics") || !contains(scenario.Invariants, "no_pos_confirmation") {
			errorsFound = append(errorsFound, prefix+"required safety invariants are missing")
		}
		req, err := scenario.ResolvedRequest(corpus)
		if err != nil {
			errorsFound = append(errorsFound, prefix+err.Error())
			continue
		}
		errorsFound = append(errorsFound, validateScenarioRequest(prefix, req, scenario.Expected)...)
	}
	if channelCounts[conversation.ChannelPhone] == 0 || channelCounts[conversation.ChannelSimulator] == 0 {
		errorsFound = append(errorsFound, "both phone and simulator channels must be represented")
	}
	for _, family := range requiredFamilies() {
		if familyCounts[family] == 0 {
			errorsFound = append(errorsFound, "required family missing: "+family)
		}
	}
	sort.Strings(errorsFound)
	return errorsFound
}

func validateScenarioRequest(prefix string, req voice.SemanticEvaluationRequest, expected ExpectedResult) []string {
	errorsFound := make([]string, 0)
	if !voice.ValidSemanticEvaluationRequest(req) {
		errorsFound = append(errorsFound, prefix+"request violates the runtime semantic-evaluation contract")
	}
	if strings.TrimSpace(req.CustomerMessage) == "" {
		errorsFound = append(errorsFound, prefix+"customer_message is required")
	}
	if req.Channel != conversation.ChannelPhone && req.Channel != conversation.ChannelSimulator {
		errorsFound = append(errorsFound, prefix+"invalid channel")
	}
	if req.SemanticContract != conversation.TurnSemanticContractFull && req.SemanticContract != conversation.TurnSemanticContractGuidance {
		errorsFound = append(errorsFound, prefix+"invalid semantic contract")
	}
	serviceIDs := map[string]bool{}
	for _, service := range req.CatalogServices {
		serviceIDs[service.ServiceID] = true
	}
	for _, alias := range req.CatalogServiceAliases {
		if !serviceIDs[alias.ServiceID] {
			errorsFound = append(errorsFound, prefix+"alias target is outside catalog: "+alias.ServiceID)
		}
	}
	for _, act := range expected.RequiredActs {
		for _, id := range append(append([]string(nil), act.SourceIDs...), act.TargetIDs...) {
			if !serviceIDs[id] {
				errorsFound = append(errorsFound, prefix+"expected act references service outside catalog: "+id)
			}
		}
	}
	if req.SemanticContract == conversation.TurnSemanticContractGuidance {
		if len(req.RecognizableGuidanceActions) == 0 || (expected.GuidanceAction == "" && !expected.Safety.Concern) {
			errorsFound = append(errorsFound, prefix+"guidance scenario requires recognizable and expected guidance actions")
		}
		if len(expected.RequiredActs) > 0 || len(expected.RequiredQuestions) > 0 {
			errorsFound = append(errorsFound, prefix+"guidance contract must not expect acts or questions")
		}
		if expected.GuidancePartySize != 0 && (expected.GuidanceAction != conversation.GuidanceActionBook || expected.GuidancePartySize < 2 || expected.GuidancePartySize > 20) {
			errorsFound = append(errorsFound, prefix+"guidance party size requires a book action and a size from 2 to 20")
		}
		for _, alternative := range expected.AlternativeGuidance {
			if !contains(req.RecognizableGuidanceActions, alternative.Action) || !validExpectedGuidanceOption(alternative) {
				errorsFound = append(errorsFound, prefix+"invalid alternative guidance tuple")
			}
		}
	} else if expected.GuidanceAction != "" || expected.GuidanceCatalogMode != "" || expected.GuidanceQuestionSubject != "" || len(expected.AlternativeGuidance) > 0 || expected.GuidancePartySize != 0 {
		errorsFound = append(errorsFound, prefix+"full contract must not expect guidance-only fields")
	}
	return errorsFound
}

func validExpectedGuidanceOption(option ExpectedGuidanceOption) bool {
	switch option.Action {
	case conversation.GuidanceActionServiceCatalog:
		return option.QuestionSubject == conversation.ConversationQuestionCatalog && contains([]string{
			conversation.ConversationQuestionModeList, conversation.ConversationQuestionModeCount,
			conversation.ConversationQuestionModeExistence, conversation.ConversationQuestionModeDetails,
			conversation.ConversationQuestionModeCompare,
		}, option.CatalogMode)
	case conversation.GuidanceActionSalonQuestion:
		return option.CatalogMode == "" && contains([]string{
			conversation.ConversationQuestionAvailability, conversation.ConversationQuestionPrice,
			conversation.ConversationQuestionHours, conversation.ConversationQuestionStaff,
			conversation.ConversationQuestionPolicy,
		}, option.QuestionSubject)
	default:
		return option.CatalogMode == "" && option.QuestionSubject == ""
	}
}

func EvaluateResult(scenario Scenario, corpus Corpus, result voice.TurnModelReply) []string {
	errorsFound := make([]string, 0)
	expected := scenario.Expected
	guidanceMatched := expected.GuidanceAction != "" && matchesExpectedGuidance(result, expected)
	goalMatchesDeclaredGuidance := guidanceMatched && result.Goal == conversation.GuidanceGoalForAction(result.GuidanceAction)
	if expected.Goal != "" && result.Goal != expected.Goal && !goalMatchesDeclaredGuidance {
		errorsFound = append(errorsFound, fmt.Sprintf("goal=%q want %q", result.Goal, expected.Goal))
	}
	if contains(expected.ForbiddenGoals, result.Goal) {
		errorsFound = append(errorsFound, fmt.Sprintf("goal=%q is forbidden", result.Goal))
	}
	if expected.GuidanceAction != "" && !guidanceMatched {
		errorsFound = append(errorsFound, fmt.Sprintf(
			"guidance=(%q,%q,%q) did not match primary=(%q,%q,%q) or declared alternatives",
			result.GuidanceAction, result.GuidanceCatalogMode, result.GuidanceQuestionSubject,
			expected.GuidanceAction, expected.GuidanceCatalogMode, expected.GuidanceQuestionSubject,
		))
	}
	if result.GuidancePartySize != expected.GuidancePartySize {
		errorsFound = append(errorsFound, fmt.Sprintf("guidance_party_size=%d want %d", result.GuidancePartySize, expected.GuidancePartySize))
	}
	for _, expectedAct := range expected.RequiredActs {
		if !hasExpectedAct(result.Acts, expectedAct) {
			errorsFound = append(errorsFound, fmt.Sprintf("required act missing: %+v", expectedAct))
		}
	}
	for _, expectedQuestion := range expected.RequiredQuestions {
		if !hasExpectedQuestion(result.Questions, expectedQuestion) {
			errorsFound = append(errorsFound, fmt.Sprintf("required question missing: %+v", expectedQuestion))
		}
	}
	if expected.CurrentBookingSummary && !hasCurrentBookingSummary(result) {
		errorsFound = append(errorsFound, "current booking summary signal missing")
	}
	if expected.AvailabilityIntent && !hasAvailabilityIntent(result) {
		errorsFound = append(errorsFound, "availability intent signal missing")
	}
	actualCurrentSystem := normalizedConsultationUnknown(result.Consultation.CurrentSystem, conversation.ConsultationSystemUnknown)
	actualDesiredOutcome := normalizedConsultationUnknown(result.Consultation.DesiredOutcome, conversation.ConsultationOutcomeUnknown)
	actualLengthChange := normalizedConsultationUnknown(result.Consultation.LengthChange, conversation.ConsultationLengthUnknown)
	if actualCurrentSystem != expected.Consultation.CurrentSystem {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.current_system=%q want %q", actualCurrentSystem, expected.Consultation.CurrentSystem))
	}
	if actualDesiredOutcome != expected.Consultation.DesiredOutcome {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.desired_outcome=%q want %q", actualDesiredOutcome, expected.Consultation.DesiredOutcome))
	}
	if actualLengthChange != expected.Consultation.LengthChange {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.length_change=%q want %q", actualLengthChange, expected.Consultation.LengthChange))
	}
	if !sameStringMultiset(result.Consultation.Priorities, expected.Consultation.Priorities) {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.priorities=%v want %v", result.Consultation.Priorities, expected.Consultation.Priorities))
	}
	if !sameStringMultiset(result.Consultation.DesiredFinishes, expected.Consultation.Finishes) {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.desired_finishes=%v want %v", result.Consultation.DesiredFinishes, expected.Consultation.Finishes))
	}
	actualBookingRequested := result.Consultation.BookingRequested
	actualConversationComplete := result.Consultation.ConversationComplete
	if scenario.Request.SemanticContract == conversation.TurnSemanticContractGuidance {
		actualBookingRequested = false
		actualConversationComplete = false
	}
	if actualBookingRequested != expected.Consultation.BookingRequested {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.booking_requested=%t want %t", actualBookingRequested, expected.Consultation.BookingRequested))
	}
	if actualConversationComplete != expected.Consultation.ConversationComplete {
		errorsFound = append(errorsFound, fmt.Sprintf("consultation.conversation_complete=%t want %t", actualConversationComplete, expected.Consultation.ConversationComplete))
	}
	for _, mutation := range result.Consultation.Mutations {
		if !evaluationMutationMatchesSnapshot(mutation, result.Consultation) {
			errorsFound = append(errorsFound, fmt.Sprintf("consultation mutation is not represented by same-turn snapshot: field=%q operation=%q values=%v", mutation.Field, mutation.Operation, mutation.Values))
		}
	}
	if expected.Safety.Checked {
		if result.Safety.Concern != expected.Safety.Concern {
			errorsFound = append(errorsFound, fmt.Sprintf("safety.concern=%t want %t", result.Safety.Concern, expected.Safety.Concern))
		}
		if expected.Safety.Category != "" && result.Safety.Category != expected.Safety.Category &&
			!contains(expected.Safety.AlternativeCategories, result.Safety.Category) {
			errorsFound = append(errorsFound, fmt.Sprintf("safety.category=%q want %q", result.Safety.Category, expected.Safety.Category))
		}
	}
	request, err := scenario.ResolvedRequest(corpus)
	if err == nil {
		allowedServiceIDs := map[string]bool{}
		for _, service := range request.CatalogServices {
			allowedServiceIDs[service.ServiceID] = true
		}
		allowedStaffIDs := map[string]bool{}
		for _, staff := range request.CatalogStaff {
			allowedStaffIDs[staff.StaffID] = true
		}
		allowedCategoryIDs := map[string]bool{}
		for _, category := range request.CatalogCategories {
			allowedCategoryIDs[category.CategoryID] = true
		}
		for _, act := range result.Acts {
			allowedEntityIDs := allowedServiceIDs
			entityLabel := "service"
			ids := append(append([]string(nil), act.SourceIDs...), act.TargetIDs...)
			if act.Entity == conversation.ConversationEntityStaff {
				allowedEntityIDs = allowedStaffIDs
				entityLabel = "staff"
				if act.Kind == conversation.ConversationActSet && act.Subject == "alternative" {
					ids = append([]string(nil), act.TargetIDs...)
				}
			}
			if act.Entity == conversation.ConversationEntityService || act.Entity == conversation.ConversationEntityStaff {
				for _, id := range ids {
					if id != "" && !allowedEntityIDs[id] {
						errorsFound = append(errorsFound, "model emitted "+entityLabel+" id outside catalog: "+id)
					}
				}
			}
			if !(act.Entity == conversation.ConversationEntityStaff && act.Kind == conversation.ConversationActSet && act.Subject == "alternative") {
				for _, categoryID := range []string{act.SourceCategoryID, act.TargetCategoryID} {
					if categoryID != "" && !allowedCategoryIDs[categoryID] {
						errorsFound = append(errorsFound, "model emitted category id outside catalog: "+categoryID)
					}
				}
			}
		}
		for _, question := range result.Questions {
			for _, id := range question.ServiceIDs {
				if !allowedServiceIDs[id] {
					errorsFound = append(errorsFound, "model emitted question service id outside catalog: "+id)
				}
			}
			for _, id := range question.StaffIDs {
				if !allowedStaffIDs[id] {
					errorsFound = append(errorsFound, "model emitted question staff id outside catalog: "+id)
				}
			}
		}
		for _, id := range result.Consultation.ComparedServiceIDs {
			if !allowedServiceIDs[id] {
				errorsFound = append(errorsFound, "model emitted compared service id outside catalog: "+id)
			}
		}
	}
	return errorsFound
}

func normalizedConsultationUnknown(value string, unknown string) string {
	value = strings.TrimSpace(value)
	if value == unknown {
		return ""
	}
	return value
}

func hasCurrentBookingSummary(result voice.TurnModelReply) bool {
	for _, question := range result.Questions {
		if question.Subject == conversation.ConversationQuestionCurrentBooking {
			return true
		}
	}
	for _, act := range result.Acts {
		if act.Kind == conversation.ConversationActSummarize {
			return true
		}
	}
	return false
}

func matchesExpectedGuidance(result voice.TurnModelReply, expected ExpectedResult) bool {
	if matchesGuidanceOption(result, ExpectedGuidanceOption{
		Action: expected.GuidanceAction, CatalogMode: expected.GuidanceCatalogMode, QuestionSubject: expected.GuidanceQuestionSubject,
	}) {
		return true
	}
	for _, alternative := range expected.AlternativeGuidance {
		if matchesGuidanceOption(result, alternative) {
			return true
		}
	}
	return false
}

func matchesGuidanceOption(result voice.TurnModelReply, expected ExpectedGuidanceOption) bool {
	return result.GuidanceAction == expected.Action && result.GuidanceCatalogMode == expected.CatalogMode &&
		result.GuidanceQuestionSubject == expected.QuestionSubject
}

func hasAvailabilityIntent(result voice.TurnModelReply) bool {
	for _, question := range result.Questions {
		if question.Subject == conversation.ConversationQuestionAvailability || question.Subject == conversation.ConversationQuestionHours {
			return true
		}
		if question.TimePreference.Direction != "" && question.TimePreference.Minutes >= 0 {
			return true
		}
	}
	for _, act := range result.Acts {
		if act.Kind == conversation.ConversationActSet && act.Entity == conversation.ConversationEntityDateTime &&
			(act.Subject == conversation.ExpectedInputRequestedDate || act.Subject == conversation.ExpectedInputRequestedTime) {
			return true
		}
	}
	return false
}

func hasExpectedAct(actual []voice.ActModelReply, expected ExpectedAct) bool {
	for _, candidate := range actual {
		kindMatches := candidate.Kind == expected.Kind || contains(expected.AlternativeKinds, candidate.Kind)
		if !kindMatches || (expected.Entity != "" && candidate.Entity != expected.Entity) ||
			(expected.GuestScope != "" && candidate.GuestScope != expected.GuestScope) ||
			(expected.GuestRef != "" && candidate.GuestRef != expected.GuestRef) ||
			(expected.Subject != "" && candidate.Subject != expected.Subject) ||
			!sameSetWhenExpected(candidate.SourceIDs, expected.SourceIDs) || !sameSetWhenExpected(candidate.TargetIDs, expected.TargetIDs) {
			continue
		}
		return true
	}
	return false
}

func hasExpectedQuestion(actual []voice.QuestionModelReply, expected ExpectedQuestion) bool {
	for _, candidate := range actual {
		if candidate.Subject == expected.Subject && (expected.Mode == "" || candidate.Mode == expected.Mode) {
			return true
		}
	}
	return false
}

func sameSetWhenExpected(actual []string, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	if len(actual) != len(expected) {
		return false
	}
	want := map[string]int{}
	for _, value := range expected {
		want[value]++
	}
	for _, value := range actual {
		want[value]--
	}
	for _, count := range want {
		if count != 0 {
			return false
		}
	}
	return true
}

func sameStringMultiset(actual []string, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	counts := map[string]int{}
	for _, value := range expected {
		counts[strings.TrimSpace(value)]++
	}
	for _, value := range actual {
		counts[strings.TrimSpace(value)]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func evaluationMutationMatchesSnapshot(mutation voice.ConsultationMutationModelReply, snapshot voice.ConsultationModelReply) bool {
	operation := strings.TrimSpace(mutation.Operation)
	if operation == conversation.ConsultationNeedOperationRemove || operation == conversation.ConsultationNeedOperationClear {
		return true
	}
	if operation != conversation.ConsultationNeedOperationSet && operation != conversation.ConsultationNeedOperationReplace && operation != conversation.ConsultationNeedOperationAdd {
		return true
	}
	values := make([]string, 0, len(mutation.Values))
	for _, value := range mutation.Values {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return true
	}
	switch strings.TrimSpace(mutation.Field) {
	case conversation.ConsultationNeedFieldCurrentSystem:
		return len(values) == 1 && strings.TrimSpace(snapshot.CurrentSystem) == values[0]
	case conversation.ConsultationNeedFieldDesiredOutcome:
		return len(values) == 1 && strings.TrimSpace(snapshot.DesiredOutcome) == values[0]
	case conversation.ConsultationNeedFieldLengthChange:
		return len(values) == 1 && strings.TrimSpace(snapshot.LengthChange) == values[0]
	case conversation.ConsultationNeedFieldPriorities:
		return containsAllStrings(snapshot.Priorities, values)
	case conversation.ConsultationNeedFieldDesiredFinishes:
		return containsAllStrings(snapshot.DesiredFinishes, values)
	case conversation.ConsultationNeedFieldComparedServiceIDs:
		return containsAllStrings(snapshot.ComparedServiceIDs, values)
	default:
		return true
	}
}

func containsAllStrings(actual []string, expected []string) bool {
	set := map[string]bool{}
	for _, value := range actual {
		set[strings.TrimSpace(value)] = true
	}
	for _, value := range expected {
		if !set[value] {
			return false
		}
	}
	return true
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func requiredFamilies() []string {
	return []string{
		"guidance_catalog", "guidance_consultation", "guidance_booking", "guidance_salon_question", "guidance_handoff",
		"service_selection", "service_edit", "availability", "party_booking", "consultation_details", "current_booking",
		"reschedule_cancel", "safety", "counterexample",
	}
}
