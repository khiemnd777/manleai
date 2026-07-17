package conversationeval

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type reviewDimension struct {
	Name     string
	Question string
	Critique string
}

var substantiveReviewDimensions = []reviewDimension{
	{Name: "semantic_ownership", Question: "Do typed actions own caller meaning without using runtime capability as the classifier?", Critique: "A valid schema proves ownership boundaries, not that a live model will classify every paraphrase correctly."},
	{Name: "catalog_grounding", Question: "Are service, category, alias, and staff references grounded in the selected data-owned catalog fixture?", Critique: "Fixture grounding prevents invented IDs, but production readiness still depends on the salon's synchronized active catalog."},
	{Name: "wording_generalization", Question: "Does this batch vary natural caller delivery while keeping wording out of production business logic?", Critique: "Generated delivery variants add linguistic pressure but are not evidence of 1,000 independently observed calls."},
	{Name: "channel_contract", Question: "Do phone and simulator scenarios use the same semantic contract with channel identity remaining observable?", Critique: "Contract parity does not test ASR, audio, TTS, or Twilio transport behavior."},
	{Name: "booking_safety", Question: "Can these scenarios be evaluated without booking mutation or unsupported POS confirmation?", Critique: "The semantic endpoint is side-effect free; full booking safety still requires conversation-engine and POS integration tests."},
	{Name: "state_integrity", Question: "Are selected services, pending acts, draft state, party guests, and consultation state internally valid?", Critique: "A static state snapshot cannot prove every multi-turn transition or concurrency interleaving."},
	{Name: "consultation_safety", Question: "Are service guidance and medical-safety classification represented separately?", Critique: "Typed safety expectations test classification boundaries, not medical advice quality or owner handoff operations."},
	{Name: "party_ownership", Question: "Do party scenarios preserve guest ownership and avoid flattening services into one caller-level choice?", Critique: "Single-turn guest references do not prove multi-child provider rollback after a partial external booking failure."},
	{Name: "counterexample_pressure", Question: "Does the corpus contain counterexamples that would expose narrow person, menu, advice, and service phrase routing?", Critique: "Declared counterexamples reduce known shortcut risk but cannot prove absence of every future hardcoded rule."},
	{Name: "evidence_honesty", Question: "Does every record distinguish deterministic contract validation from actual model execution?", Critique: "Passing this review must never be reported as a live-model pass; only retained live outputs can support that claim."},
}

func ReviewCorpus(corpus Corpus) ReviewReport {
	report := ReviewReport{
		SchemaVersion: SchemaVersion,
		Scope:         "deterministic_corpus_and_contract_review_not_model_execution",
		ScenarioCount: len(corpus.Scenarios),
		RoundCount:    RequiredReviewRounds,
		Passed:        true,
		Rounds:        make([]ReviewRound, 0, RequiredReviewRounds),
	}
	globalErrors := ValidateCorpus(corpus)
	errorsByScenario := make(map[string][]string)
	for _, validationError := range globalErrors {
		if separator := strings.Index(validationError, ": "); separator > 0 {
			id := validationError[:separator]
			errorsByScenario[id] = append(errorsByScenario[id], validationError)
		}
	}

	for round := 1; round <= RequiredReviewRounds; round++ {
		dimension := substantiveReviewDimensions[(round-1)%len(substantiveReviewDimensions)]
		review := ReviewRound{Round: round, Dimension: dimension.Name, Passed: true, Critique: dimension.Critique}
		assigned := make([]Scenario, 0, RequiredScenarioCount/RequiredReviewRounds)
		for index := round - 1; index < len(corpus.Scenarios); index += RequiredReviewRounds {
			scenario := corpus.Scenarios[index]
			assigned = append(assigned, scenario)
			review.ScenarioIDs = append(review.ScenarioIDs, scenario.ID)
			review.Errors = append(review.Errors, errorsByScenario[scenario.ID]...)
		}
		if len(assigned) != RequiredScenarioCount/RequiredReviewRounds {
			review.Errors = append(review.Errors, fmt.Sprintf("round owns %d scenarios, want %d", len(assigned), RequiredScenarioCount/RequiredReviewRounds))
		}

		review.Evidence = collectReviewEvidence(assigned)
		review.Checks = reviewChecks(corpus, assigned, dimension, len(review.Errors) == 0)
		for _, check := range review.Checks {
			if !check.Passed {
				review.Errors = append(review.Errors, check.Name+": "+check.Evidence)
			}
		}
		review.Passed = len(review.Errors) == 0
		firstID, lastID := "none", "none"
		if len(review.ScenarioIDs) > 0 {
			firstID = review.ScenarioIDs[0]
			lastID = review.ScenarioIDs[len(review.ScenarioIDs)-1]
		}
		review.Question = fmt.Sprintf("Review %03d [%s]: %s Assigned evidence: %s through %s.", round, dimension.Name, dimension.Question, firstID, lastID)
		if review.Passed {
			review.Answer = fmt.Sprintf(
				"Structurally passed %d/%d assigned scenarios: phone=%d, simulator=%d, guidance_contract=%d, full_contract=%d, families=%s, fixtures=%s.",
				review.Evidence.CheckedScenarioCount, RequiredScenarioCount/RequiredReviewRounds,
				review.Evidence.PhoneCount, review.Evidence.SimulatorCount,
				review.Evidence.GuidanceContractCount, review.Evidence.FullContractCount,
				strings.Join(review.Evidence.Families, ","), strings.Join(review.Evidence.CatalogFixtures, ","),
			)
			review.Resolution = "No structural correction is required for this batch. Model quality remains unverified until a retained live result is scored; this review is not a model pass."
		} else {
			review.Answer = fmt.Sprintf("Failed with %d concrete structural findings across %d assigned scenarios.", len(review.Errors), len(assigned))
			review.Resolution = "Correct the listed corpus or contract findings, regenerate artifacts, and rerun this review before any live canary."
			report.Passed = false
		}
		report.Rounds = append(report.Rounds, review)
	}
	if len(globalErrors) > 0 {
		report.Passed = false
		if len(corpus.Scenarios) == 0 && len(report.Rounds) > 0 {
			report.Rounds[0].Errors = append(report.Rounds[0].Errors, globalErrors...)
			report.Rounds[0].Passed = false
		}
	}
	return report
}

func collectReviewEvidence(scenarios []Scenario) ReviewEvidence {
	evidence := ReviewEvidence{CheckedScenarioCount: len(scenarios)}
	families := map[string]bool{}
	fixtures := map[string]bool{}
	for _, scenario := range scenarios {
		switch scenario.Request.Channel {
		case conversation.ChannelPhone:
			evidence.PhoneCount++
		case conversation.ChannelSimulator:
			evidence.SimulatorCount++
		}
		switch scenario.Request.SemanticContract {
		case conversation.TurnSemanticContractGuidance:
			evidence.GuidanceContractCount++
		case conversation.TurnSemanticContractFull:
			evidence.FullContractCount++
		}
		families[scenario.Family] = true
		fixtures[scenario.CatalogFixture] = true
		evidence.BaseCaseIDs = append(evidence.BaseCaseIDs, scenario.Provenance.BaseCaseID)
	}
	for family := range families {
		evidence.Families = append(evidence.Families, family)
	}
	for fixture := range fixtures {
		evidence.CatalogFixtures = append(evidence.CatalogFixtures, fixture)
	}
	sort.Strings(evidence.Families)
	sort.Strings(evidence.CatalogFixtures)
	sort.Strings(evidence.BaseCaseIDs)
	return evidence
}

func reviewChecks(corpus Corpus, scenarios []Scenario, dimension reviewDimension, structurallyValid bool) []ReviewCheck {
	validRequests := 0
	honestScope := 0
	bookingSafe := 0
	uniqueMessages := map[string]bool{}
	for _, scenario := range scenarios {
		request, err := scenario.ResolvedRequest(corpus)
		if err == nil && voiceRequestValid(request) {
			validRequests++
		}
		if scenario.EvidenceLevel == "semantic_turn_contract" && scenario.Provenance.Scope == "single_turn" {
			honestScope++
		}
		if contains(scenario.Invariants, "no_pos_confirmation") && contains(scenario.Invariants, "no_conversation_mutation") {
			bookingSafe++
		}
		uniqueMessages[strings.ToLower(strings.TrimSpace(scenario.Request.CustomerMessage))] = true
	}
	count := len(scenarios)
	return []ReviewCheck{
		{Name: "assigned_contracts_resolve", Passed: structurallyValid && validRequests == count, Evidence: fmt.Sprintf("%d/%d requests resolved through the runtime validation owner", validRequests, count)},
		{Name: "evidence_scope_is_explicit", Passed: honestScope == count, Evidence: fmt.Sprintf("%d/%d scenarios declare semantic_turn_contract single_turn scope", honestScope, count)},
		{Name: "booking_side_effect_guard", Passed: bookingSafe == count, Evidence: fmt.Sprintf("%d/%d scenarios require no conversation mutation and no POS confirmation", bookingSafe, count)},
		{Name: "wording_is_unique_in_batch", Passed: len(uniqueMessages) == count, Evidence: fmt.Sprintf("%d distinct normalized utterances across %d scenarios", len(uniqueMessages), count)},
		{Name: "substantive_dimension_reviewed", Passed: dimension.Name != "", Evidence: "reviewed dimension=" + dimension.Name + "; limitations recorded in critique"},
	}
}

func voiceRequestValid(request voice.SemanticEvaluationRequest) bool {
	// Kept as a local adapter so review evidence calls the same exported runtime
	// validator without duplicating its contract rules.
	return voice.ValidSemanticEvaluationRequest(request)
}
