package conversationeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

const (
	RealSalonSchemaVersion      = 1
	RealSalonRequiredJourneys   = 100
	RealSalonMinCustomerTurns   = 3
	RealSalonMaxCustomerTurns   = 12
	RealSalonLiveCanaryJourneys = 10
	RealSalonLiveModelCallLimit = 60
	RealSalonCorpusContract     = "real-salon-multi-turn-v1"
	RealSalonEvaluationContract = "real-salon-production-flow-v1"
	RealSalonReviewContract     = "real-salon-transcript-review-v1"

	RealSalonStatusStructuralValidated = "structural_validated"
	RealSalonStatusRuntimeExecuted     = "runtime_executed"
	RealSalonStatusModelExecuted       = "model_executed"
	RealSalonStatusReviewPassed        = "review_passed"
	RealSalonStatusFailed              = "failed"
)

var realSalonRequiredFamilyCounts = map[string]int{
	"service_advice_discovery":             15,
	"service_consultation_recommendation":  15,
	"catalog_and_salon_questions":          10,
	"single_customer_booking":              20,
	"party_group_booking":                  10,
	"reschedule_cancel":                    10,
	"correction_interruption_multi_intent": 10,
	"safety_handoff":                       5,
	"disabled_empty_provider_failure":      5,
}

// RealSalonCorpus is intentionally separate from the single-turn semantic
// corpus. A journey owns one isolated salon fixture and keeps one production
// conversation session across all of its customer turns.
type RealSalonCorpus struct {
	SchemaVersion   int                       `json:"schema_version"`
	ContractVersion string                    `json:"contract_version"`
	Authorship      string                    `json:"authorship"`
	ExpectedCount   int                       `json:"expected_journey_count"`
	CatalogFixtures map[string]CatalogFixture `json:"catalog_fixtures"`
	Journeys        []RealSalonJourney        `json:"journeys"`
}

type RealSalonJourney struct {
	ID             string                `json:"id"`
	Family         string                `json:"family"`
	Title          string                `json:"title"`
	Channel        string                `json:"channel"`
	CatalogFixture string                `json:"catalog_fixture"`
	Authored       bool                  `json:"authored"`
	Generated      bool                  `json:"generated"`
	Scope          string                `json:"scope"`
	LiveCanary     bool                  `json:"live_canary"`
	InitialState   RealSalonInitialState `json:"initial_state"`
	Turns          []RealSalonTurn       `json:"turns"`
}

type RealSalonInitialState struct {
	Intent             string                          `json:"intent,omitempty"`
	Phase              string                          `json:"phase,omitempty"`
	BookingAction      string                          `json:"booking_action,omitempty"`
	ServiceIDs         []string                        `json:"service_ids,omitempty"`
	StaffID            string                          `json:"staff_id,omitempty"`
	RequestedDate      string                          `json:"requested_date,omitempty"`
	HasCustomerName    bool                            `json:"has_customer_name,omitempty"`
	HasCustomerPhone   bool                            `json:"has_customer_phone,omitempty"`
	PartySize          int                             `json:"party_size,omitempty"`
	Consultation       *conversation.ConsultationState `json:"consultation,omitempty"`
	ProviderState      string                          `json:"provider_state,omitempty"`
	GuidanceCatalogOff bool                            `json:"guidance_catalog_disabled,omitempty"`
}

type RealSalonTurn struct {
	CustomerMessage string                   `json:"customer_message"`
	ModelFixture    voice.TurnModelReply     `json:"model_fixture"`
	Expected        RealSalonTurnExpectation `json:"expected"`
}

// RealSalonTurnExpectation describes meaning and state obligations. It never
// contains an exact expected AI sentence, so presentation wording is not a
// hidden parser or a fixture-specific product rule.
type RealSalonTurnExpectation struct {
	ReplyObligations           []string `json:"reply_obligations"`
	ForbiddenReplyBehaviors    []string `json:"forbidden_reply_behaviors"`
	AllowedIntentsAfter        []string `json:"allowed_intents_after,omitempty"`
	AllowedPhasesAfter         []string `json:"allowed_phases_after,omitempty"`
	AllowedNextInputs          []string `json:"allowed_next_inputs,omitempty"`
	RequiredSelectedServiceIDs []string `json:"required_selected_service_ids,omitempty"`
	RequiredToolCalls          []string `json:"required_tool_calls,omitempty"`
	ToolPolicy                 string   `json:"tool_policy"`
	AllowedToolCalls           []string `json:"allowed_tool_calls,omitempty"`
	RetainFields               []string `json:"retain_fields,omitempty"`
	RequireHandoff             bool     `json:"require_handoff,omitempty"`
	AllowHandoff               bool     `json:"allow_handoff,omitempty"`
	NoBookingSideEffect        bool     `json:"no_booking_side_effect"`
	FinalReplyAssertion        bool     `json:"final_reply_assertion"`
}

type RealSalonReport struct {
	SchemaVersion           int                      `json:"schema_version"`
	ContractVersion         string                   `json:"contract_version"`
	EvaluationContract      string                   `json:"evaluation_contract"`
	ReviewContract          string                   `json:"review_contract"`
	Mode                    string                   `json:"mode"`
	RunKey                  string                   `json:"run_key"`
	OverallStatus           string                   `json:"overall_status"`
	Passed                  bool                     `json:"passed"`
	JourneyCount            int                      `json:"journey_count"`
	StructuralValidated     int                      `json:"structural_validated_count"`
	RuntimeExecuted         int                      `json:"runtime_executed_count"`
	ModelExecuted           int                      `json:"model_executed_count"`
	ReviewPassed            int                      `json:"review_passed_count"`
	Failed                  int                      `json:"failed_count"`
	NotRun                  int                      `json:"not_run_count"`
	ModelCallBudget         int                      `json:"model_call_budget,omitempty"`
	ModelCallCount          int                      `json:"model_call_count,omitempty"`
	RequestTimeoutMS        int64                    `json:"request_timeout_ms,omitempty"`
	TransientRetries        int                      `json:"transient_retries,omitempty"`
	TransientRetryCount     int                      `json:"transient_retry_count,omitempty"`
	RecoveredTransientCount int                      `json:"recovered_transient_count,omitempty"`
	Usage                   ModelUsage               `json:"usage,omitempty"`
	StartedAt               string                   `json:"started_at,omitempty"`
	CompletedAt             string                   `json:"completed_at,omitempty"`
	InFlightModelCall       *InFlightModelCall       `json:"in_flight_model_call,omitempty"`
	Results                 []RealSalonJourneyResult `json:"results"`
	ReviewRounds            []DirectReviewRound      `json:"review_rounds,omitempty"`
}

type RealSalonJourneyResult struct {
	JourneyID     string                `json:"journey_id"`
	Family        string                `json:"family"`
	Status        string                `json:"status"`
	ModelExecuted bool                  `json:"model_executed"`
	ReviewPassed  bool                  `json:"review_passed"`
	ModelCalls    int                   `json:"model_calls,omitempty"`
	Usage         ModelUsage            `json:"usage,omitempty"`
	Turns         []RealSalonTurnResult `json:"turns"`
	Errors        []string              `json:"errors,omitempty"`
}

type RealSalonTurnResult struct {
	Turn                       int           `json:"turn"`
	CustomerMessage            string        `json:"customer_message"`
	AIReply                    string        `json:"ai_reply"`
	IntentBefore               string        `json:"intent_before,omitempty"`
	IntentAfter                string        `json:"intent_after,omitempty"`
	PhaseBefore                string        `json:"phase_before,omitempty"`
	PhaseAfter                 string        `json:"phase_after,omitempty"`
	NextExpectedInput          string        `json:"next_expected_input,omitempty"`
	SelectedServiceIDs         []string      `json:"selected_service_ids,omitempty"`
	WouldCallTools             []ToolAttempt `json:"would_call_tools,omitempty"`
	BookingConfirmed           bool          `json:"booking_confirmed"`
	ProviderBookingIDPresent   bool          `json:"provider_booking_id_present"`
	HandoffRequested           bool          `json:"handoff_requested"`
	SemanticInterpreterInvoked bool          `json:"semantic_interpreter_invoked"`
	ReplyGeneratorInvoked      bool          `json:"reply_generator_invoked"`
	ModelCalls                 int           `json:"model_calls,omitempty"`
	Usage                      ModelUsage    `json:"usage,omitempty"`
	Errors                     []string      `json:"errors,omitempty"`
}

func ValidateRealSalonCorpus(corpus RealSalonCorpus) []string {
	problems := make([]string, 0)
	if corpus.SchemaVersion != RealSalonSchemaVersion {
		problems = append(problems, fmt.Sprintf("schema_version=%d, want %d", corpus.SchemaVersion, RealSalonSchemaVersion))
	}
	if corpus.ContractVersion != RealSalonCorpusContract {
		problems = append(problems, fmt.Sprintf("contract_version=%q, want %q", corpus.ContractVersion, RealSalonCorpusContract))
	}
	if corpus.Authorship != "independently_authored_no_paraphrase_expansion" {
		problems = append(problems, "authorship must declare independently authored journeys")
	}
	if corpus.ExpectedCount != RealSalonRequiredJourneys || len(corpus.Journeys) != RealSalonRequiredJourneys {
		problems = append(problems, fmt.Sprintf("journey count=%d expected_field=%d, want %d", len(corpus.Journeys), corpus.ExpectedCount, RealSalonRequiredJourneys))
	}
	if len(corpus.CatalogFixtures) < 3 {
		problems = append(problems, "at least three materially different salon catalog fixtures are required")
	}
	fixtureFingerprints := map[string]string{}
	for fixtureID, fixture := range corpus.CatalogFixtures {
		problems = append(problems, validateRealSalonCatalogFixture(fixtureID, fixture)...)
		raw, _ := json.Marshal(fixture)
		sum := sha256.Sum256(raw)
		fingerprint := hex.EncodeToString(sum[:])
		if prior := fixtureFingerprints[fingerprint]; prior != "" {
			problems = append(problems, fmt.Sprintf("catalog fixture %s duplicates %s", fixtureID, prior))
		}
		fixtureFingerprints[fingerprint] = fixtureID
	}
	familyCounts := map[string]int{}
	seenIDs := map[string]bool{}
	seenFingerprints := map[string]string{}
	liveCanaries := 0
	for _, journey := range corpus.Journeys {
		prefix := journey.ID + ": "
		if strings.TrimSpace(journey.ID) == "" || seenIDs[journey.ID] {
			problems = append(problems, prefix+"journey id is empty or duplicated")
		}
		seenIDs[journey.ID] = true
		familyCounts[journey.Family]++
		if _, ok := realSalonRequiredFamilyCounts[journey.Family]; !ok {
			problems = append(problems, prefix+"unknown family "+journey.Family)
		}
		if strings.TrimSpace(journey.Title) == "" {
			problems = append(problems, prefix+"title is required")
		}
		if journey.Channel != conversation.ChannelPhone && journey.Channel != conversation.ChannelSimulator {
			problems = append(problems, prefix+"channel must be phone or simulator")
		}
		if _, ok := corpus.CatalogFixtures[journey.CatalogFixture]; !ok {
			problems = append(problems, prefix+"catalog fixture does not exist")
		}
		if !journey.Authored || journey.Generated || journey.Scope != "multi_turn_real_salon" {
			problems = append(problems, prefix+"journey must be authored, non-generated, and multi-turn scoped")
		}
		if len(journey.Turns) < RealSalonMinCustomerTurns || len(journey.Turns) > RealSalonMaxCustomerTurns {
			problems = append(problems, fmt.Sprintf("%sturn count=%d, want %d-%d", prefix, len(journey.Turns), RealSalonMinCustomerTurns, RealSalonMaxCustomerTurns))
		}
		if journey.LiveCanary {
			liveCanaries++
		}
		fingerprint := realSalonJourneyFingerprint(journey)
		if prior := seenFingerprints[fingerprint]; prior != "" {
			problems = append(problems, prefix+"duplicates complete journey fingerprint from "+prior)
		}
		seenFingerprints[fingerprint] = journey.ID
		for index, turn := range journey.Turns {
			turnPrefix := fmt.Sprintf("%sturn %d: ", prefix, index+1)
			if strings.TrimSpace(turn.CustomerMessage) == "" {
				problems = append(problems, turnPrefix+"customer_message is required")
			}
			if len(turn.Expected.ReplyObligations) == 0 || len(turn.Expected.ForbiddenReplyBehaviors) == 0 {
				problems = append(problems, turnPrefix+"reply obligations and forbidden behaviors are required")
			}
			if !contains([]string{"none", "booking", "appointment_change"}, turn.Expected.ToolPolicy) {
				problems = append(problems, turnPrefix+"an explicit valid tool-call policy is required")
			}
			for _, allowedTool := range turn.Expected.AllowedToolCalls {
				if !realSalonToolPolicyAllows(turn.Expected.ToolPolicy, allowedTool) {
					problems = append(problems, turnPrefix+"allowed tool is outside the declared tool policy: "+allowedTool)
				}
			}
			for _, requiredTool := range turn.Expected.RequiredToolCalls {
				if !contains(turn.Expected.AllowedToolCalls, requiredTool) {
					problems = append(problems, turnPrefix+"required tool is outside the allowed tool policy: "+requiredTool)
				}
			}
			if turn.ModelFixture.Confidence <= 0 || turn.ModelFixture.Safety.Confidence <= 0 {
				problems = append(problems, turnPrefix+"deterministic semantic fixture is required")
			}
			if !turn.Expected.NoBookingSideEffect {
				problems = append(problems, turnPrefix+"no_booking_side_effect must be true")
			}
			if index == len(journey.Turns)-1 && !turn.Expected.FinalReplyAssertion {
				problems = append(problems, turnPrefix+"final reply assertion is required")
			}
			problems = append(problems, validateRealSalonServiceReferences(turnPrefix, corpus.CatalogFixtures[journey.CatalogFixture], turn.Expected.RequiredSelectedServiceIDs)...)
		}
	}
	for family, expected := range realSalonRequiredFamilyCounts {
		if familyCounts[family] != expected {
			problems = append(problems, fmt.Sprintf("family %s count=%d, want %d", family, familyCounts[family], expected))
		}
	}
	if liveCanaries != RealSalonLiveCanaryJourneys {
		problems = append(problems, fmt.Sprintf("live canary count=%d, want %d", liveCanaries, RealSalonLiveCanaryJourneys))
	}
	sort.Strings(problems)
	return problems
}

func realSalonToolPolicyAllows(policy, tool string) bool {
	switch policy {
	case "booking":
		return contains([]string{"available_slots", "create_booking"}, tool)
	case "appointment_change":
		return contains([]string{"available_slots", "reschedule_candidates", "cancel_booking", "reschedule_booking"}, tool)
	default:
		return false
	}
}

func RealSalonRunKey(corpus RealSalonCorpus, mode string, modelIdentity string) (string, error) {
	raw, err := json.Marshal(struct {
		SchemaVersion      int                       `json:"schema_version"`
		ContractVersion    string                    `json:"contract_version"`
		EvaluationContract string                    `json:"evaluation_contract"`
		ReviewContract     string                    `json:"review_contract"`
		SharedReview       string                    `json:"shared_review_contract"`
		Authorship         string                    `json:"authorship"`
		Mode               string                    `json:"mode"`
		ModelIdentity      string                    `json:"model_identity"`
		CatalogFixtures    map[string]CatalogFixture `json:"catalog_fixtures"`
		Journeys           []RealSalonJourney        `json:"journeys"`
	}{
		corpus.SchemaVersion, corpus.ContractVersion, RealSalonEvaluationContract,
		RealSalonReviewContract, DirectReviewContractVersion, corpus.Authorship,
		strings.TrimSpace(mode), strings.TrimSpace(modelIdentity), corpus.CatalogFixtures, corpus.Journeys,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "real-salon-v1-" + hex.EncodeToString(sum[:12]), nil
}

func realSalonJourneyFingerprint(journey RealSalonJourney) string {
	initialState, _ := json.Marshal(journey.InitialState)
	parts := []string{journey.Channel, journey.CatalogFixture, string(initialState)}
	for _, turn := range journey.Turns {
		parts = append(parts, strings.ToLower(strings.Join(strings.Fields(turn.CustomerMessage), " ")))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func validateRealSalonServiceReferences(prefix string, fixture CatalogFixture, ids []string) []string {
	allowed := map[string]bool{}
	for _, service := range fixture.Services {
		allowed[service.ServiceID] = true
	}
	problems := make([]string, 0)
	for _, id := range ids {
		if !allowed[id] {
			problems = append(problems, prefix+"expected service is outside fixture: "+id)
		}
	}
	return problems
}

func validateRealSalonCatalogFixture(fixtureID string, fixture CatalogFixture) []string {
	prefix := "catalog fixture " + fixtureID + ": "
	problems := make([]string, 0)
	if strings.TrimSpace(fixtureID) == "" || len(fixture.Services) == 0 || len(fixture.Categories) == 0 || len(fixture.Staff) == 0 || len(fixture.BusinessHours) == 0 {
		problems = append(problems, prefix+"services, categories, staff, and business hours are required")
	}
	serviceIDs := map[string]bool{}
	for _, service := range fixture.Services {
		if strings.TrimSpace(service.ServiceID) == "" || strings.TrimSpace(service.ServiceName) == "" || serviceIDs[service.ServiceID] {
			problems = append(problems, prefix+"service IDs and names must be present and unique")
			break
		}
		serviceIDs[service.ServiceID] = true
	}
	for _, alias := range fixture.Aliases {
		if strings.TrimSpace(alias.Alias) == "" || !serviceIDs[alias.ServiceID] {
			problems = append(problems, prefix+"service alias is empty or references a service outside the fixture")
			break
		}
	}
	categoryIDs := map[string]bool{}
	for _, category := range fixture.Categories {
		if strings.TrimSpace(category.CategoryID) == "" || strings.TrimSpace(category.CategoryName) == "" || categoryIDs[category.CategoryID] || len(category.ServiceIDs) == 0 {
			problems = append(problems, prefix+"category IDs, names, and service membership must be present and unique")
			break
		}
		categoryIDs[category.CategoryID] = true
		for _, serviceID := range category.ServiceIDs {
			if !serviceIDs[serviceID] {
				problems = append(problems, prefix+"category references a service outside the fixture: "+serviceID)
			}
		}
	}
	staffIDs := map[string]bool{}
	for _, staff := range fixture.Staff {
		if strings.TrimSpace(staff.StaffID) == "" || strings.TrimSpace(staff.StaffName) == "" || staffIDs[staff.StaffID] {
			problems = append(problems, prefix+"staff IDs and names must be present and unique")
			break
		}
		staffIDs[staff.StaffID] = true
	}
	return problems
}
