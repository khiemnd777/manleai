package conversationeval

import (
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestGenerateCorpusBuildsOneThousandReviewedScenarios(t *testing.T) {
	corpus := GenerateCorpus()
	if problems := ValidateCorpus(corpus); len(problems) > 0 {
		t.Fatalf("corpus validation failed with %d problems; first: %s", len(problems), problems[0])
	}
	report := ReviewCorpus(corpus)
	if !report.Passed || report.RoundCount != RequiredReviewRounds || len(report.Rounds) != RequiredReviewRounds {
		t.Fatalf("review report = passed:%t rounds:%d/%d", report.Passed, report.RoundCount, len(report.Rounds))
	}
	for _, round := range report.Rounds {
		if !round.Passed || len(round.ScenarioIDs) != 10 || !strings.HasPrefix(round.Answer, "Structurally passed") ||
			round.Dimension == "" || len(round.Checks) < 5 || !strings.Contains(round.Resolution, "not a model pass") {
			t.Fatalf("review round %d = %#v", round.Round, round)
		}
	}
}

func TestGeneratePilotCorpusUsesFiftyDirectAuthoredExecutionsAndFiveChannelPairs(t *testing.T) {
	corpus := GeneratePilotCorpus()
	if problems := ValidatePilotCorpus(corpus); len(problems) > 0 {
		t.Fatalf("pilot validation failed: %v", problems)
	}
	if len(corpus.Scenarios) != PilotScenarioCount || corpus.ExpectedReviewRounds != PilotReviewRounds {
		t.Fatalf("pilot shape = scenarios:%d reviews:%d", len(corpus.Scenarios), corpus.ExpectedReviewRounds)
	}
	for fixtureID, fixture := range corpus.CatalogFixtures {
		if len(fixture.BusinessHours) != 7 {
			t.Fatalf("pilot fixture %q business hours=%d, want seven structured days", fixtureID, len(fixture.BusinessHours))
		}
	}
	channelsByBase := map[string]map[string]bool{}
	messages := map[string]bool{}
	familyCounts := map[string]int{}
	for _, scenario := range corpus.Scenarios {
		familyCounts[scenario.Family]++
		if scenario.Provenance.Generated {
			t.Fatalf("pilot contains generated wording: %#v", scenario.Provenance)
		}
		messageKey := scenario.Request.Channel + "\x00" + scenario.Request.CustomerMessage
		if messages[messageKey] {
			t.Fatalf("pilot duplicates authored wording: %q", scenario.Request.CustomerMessage)
		}
		messages[messageKey] = true
		if channelsByBase[scenario.Provenance.BaseCaseID] == nil {
			channelsByBase[scenario.Provenance.BaseCaseID] = map[string]bool{}
		}
		channelsByBase[scenario.Provenance.BaseCaseID][scenario.Request.Channel] = true
	}
	paired := 0
	for _, channels := range channelsByBase {
		if channels[conversation.ChannelPhone] && channels[conversation.ChannelSimulator] {
			paired++
		}
	}
	if len(channelsByBase) != 45 || paired != 5 {
		t.Fatalf("pilot bases=%d paired=%d, want 45 and 5", len(channelsByBase), paired)
	}
	for _, family := range requiredFamilies() {
		if familyCounts[family] < 2 || familyCounts[family] > 7 {
			t.Fatalf("pilot family %q count=%d is not operationally balanced", family, familyCounts[family])
		}
	}
}

func TestGenerateCorpusDisclosesGeneratedUtteranceVariantsInsteadOfCallingThemIndependentCalls(t *testing.T) {
	corpus := GenerateCorpus()
	direct := 0
	generated := 0
	reportedChannels := map[string]map[string]bool{
		"I don't know whether I should choose a service": {},
		"I want a service for my nails":                  {},
	}
	for _, scenario := range corpus.Scenarios {
		if scenario.Provenance.Generated {
			generated++
		} else {
			direct++
		}
		if scenario.Provenance.BaseCaseID == "" || scenario.Provenance.UtteranceVariant == "" || scenario.EvidenceLevel != "semantic_turn_contract" {
			t.Fatalf("scenario provenance = %#v", scenario)
		}
		if channels, ok := reportedChannels[scenario.Request.CustomerMessage]; ok && scenario.Expected.GuidanceAction == conversation.GuidanceActionConsultation {
			channels[scenario.Request.Channel] = true
		}
	}
	if direct != 392 || generated != 608 {
		t.Fatalf("corpus provenance direct=%d generated=%d", direct, generated)
	}
	for message, channels := range reportedChannels {
		if !channels[conversation.ChannelPhone] || !channels[conversation.ChannelSimulator] {
			t.Fatalf("reported wording %q does not have exact phone/simulator parity: %#v", message, channels)
		}
	}
}

func TestGenerateCorpusIncludesDataOwnedNonstandardCatalogCounterexamples(t *testing.T) {
	corpus := GenerateCorpus()
	aurora := corpus.CatalogFixtures["aurora"]
	if len(aurora.Services) != 3 || aurora.Services[0].ServiceName != "Luna Renewal" || aurora.Aliases[0].Alias != "moon refresh" {
		t.Fatalf("nonstandard catalog fixture = %#v", aurora)
	}
	foundAliasSelection := false
	foundPersonCounterexample := false
	for _, scenario := range corpus.Scenarios {
		if strings.Contains(scenario.Request.CustomerMessage, "moon refresh") && scenario.CatalogFixture == "aurora" &&
			scenario.Expected.GuidanceAction == conversation.GuidanceActionNameService {
			foundAliasSelection = true
		}
		if strings.HasPrefix(scenario.Request.CustomerMessage, "It is for another person.") && contains(scenario.Expected.ForbiddenGoals, "human_handoff") {
			foundPersonCounterexample = true
		}
	}
	if !foundAliasSelection || !foundPersonCounterexample {
		t.Fatalf("expected nonstandard alias and person counterexamples; alias=%t person=%t", foundAliasSelection, foundPersonCounterexample)
	}
}

func TestGenerateCorpusCoversTypedInitialPartyAndAppointmentManagement(t *testing.T) {
	corpus := GenerateCorpus()
	partyScenarios := 0
	actions := map[string]int{}
	for _, scenario := range corpus.Scenarios {
		if scenario.Expected.GuidancePartySize >= 2 {
			partyScenarios++
		}
		if scenario.Expected.GuidanceAction != "" {
			actions[scenario.Expected.GuidanceAction]++
		}
	}
	if partyScenarios != 10 || actions[conversation.GuidanceActionReschedule] != 20 || actions[conversation.GuidanceActionCancel] != 20 {
		t.Fatalf("typed guidance coverage = parties:%d actions:%#v", partyScenarios, actions)
	}
}

func TestEvaluateResultRejectsInventedServiceID(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.Family == "service_selection" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Goal: "book_appointment",
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActAdd, Entity: conversation.ConversationEntityService,
			TargetIDs: []string{"invented_service"},
		}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	problems := EvaluateResult(scenario, corpus, result)
	if len(problems) == 0 || !containsSubstring(problems, "outside catalog") {
		t.Fatalf("evaluation problems = %#v, want outside-catalog failure", problems)
	}
}

func TestEvaluateResultRejectsInventedConsultationMutation(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := scenarioWithID(t, corpus, "pilot-003")
	result := voice.TurnModelReply{
		Goal: "consultation", GuidanceAction: conversation.GuidanceActionConsultation,
		Consultation: voice.ConsultationModelReply{
			BookingRequested: true,
			Mutations: []voice.ConsultationMutationModelReply{{
				Field: conversation.ConsultationNeedFieldDesiredOutcome, Operation: conversation.ConsultationNeedOperationSet,
				Values: []string{conversation.ConsultationOutcomeMaintain},
			}},
		},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	problems := EvaluateResult(scenario, corpus, result)
	if !containsSubstring(problems, "mutation is not represented by same-turn snapshot") {
		t.Fatalf("invented consultation recognition problems = %#v", problems)
	}
}

func TestEvaluateResultRejectsInventedBookingSignalDuringActiveConsultation(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := scenarioWithID(t, corpus, "pilot-020")
	result := voice.TurnModelReply{
		Goal: "consultation",
		Consultation: voice.ConsultationModelReply{
			CurrentSystem: conversation.ConsultationSystemNatural, BookingRequested: true,
			Mutations: []voice.ConsultationMutationModelReply{{
				Field: conversation.ConsultationNeedFieldCurrentSystem, Operation: conversation.ConsultationNeedOperationSet,
				Values: []string{conversation.ConsultationSystemNatural},
			}},
		},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); !containsSubstring(problems, "consultation.booking_requested=true want false") {
		t.Fatalf("invented booking signal problems = %#v", problems)
	}
}

func TestEvaluateResultTreatsProtocolUnknownAsAbsent(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := scenarioWithID(t, corpus, "pilot-019")
	result := modelReplyFromExpected(scenario.Expected)
	result.Consultation.CurrentSystem = conversation.ConsultationSystemUnknown
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("protocol unknown should normalize to absence: %#v", problems)
	}
}

func TestEvaluateResultAcceptsExactGroundedConsultationSnapshotAndMutation(t *testing.T) {
	corpus := GeneratePilotCorpus()
	scenario := scenarioWithID(t, corpus, "pilot-020")
	result := voice.TurnModelReply{
		Goal: "consultation",
		Consultation: voice.ConsultationModelReply{
			CurrentSystem: conversation.ConsultationSystemNatural,
			Mutations: []voice.ConsultationMutationModelReply{{
				Field: conversation.ConsultationNeedFieldCurrentSystem, Operation: conversation.ConsultationNeedOperationSet,
				Values: []string{conversation.ConsultationSystemNatural},
			}},
		},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("grounded consultation result problems = %#v", problems)
	}
}

func scenarioWithID(t *testing.T, corpus Corpus, id string) Scenario {
	t.Helper()
	for _, scenario := range corpus.Scenarios {
		if scenario.ID == id {
			return scenario
		}
	}
	t.Fatalf("scenario %q not found", id)
	return Scenario{}
}

func TestEvaluateResultAcceptsRuntimeEquivalentCurrentBookingSummaryAct(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.Family == "current_booking" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Goal:   "unknown",
		Acts:   []voice.ActModelReply{{Kind: conversation.ConversationActSummarize, Entity: conversation.ConversationEntityService}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("current booking summary problems = %#v", problems)
	}
}

func TestEvaluateResultAcceptsRuntimeEquivalentAvailabilityDateTimeAct(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.Family == "availability" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Goal: "book_appointment",
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActSet, Entity: conversation.ConversationEntityDateTime,
			Subject: conversation.ExpectedInputRequestedDate, Value: "Friday afternoon",
		}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("availability date-time problems = %#v", problems)
	}
}

func TestEvaluateResultAcceptsTimeConstrainedQuestionAsAvailabilitySignal(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.ID == "availability-008" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Questions: []voice.QuestionModelReply{{
			Subject:        conversation.ConversationQuestionCurrentBooking,
			TimePreference: voice.TimePreferenceModelReply{Direction: conversation.TimePreferenceExact, Minutes: 15 * 60},
		}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("time-constrained availability problems = %#v", problems)
	}
}

func TestEvaluateResultAcceptsDeclaredOperationalSubjectAndSafetyAlternatives(t *testing.T) {
	corpus := GenerateCorpus()
	var walkIn Scenario
	var health Scenario
	var initialAvailability Scenario
	for _, candidate := range corpus.Scenarios {
		switch candidate.ID {
		case "guidance_salon_question-010":
			walkIn = candidate
		case "safety-008":
			health = candidate
		case "guidance_salon_question-015":
			initialAvailability = candidate
		}
	}
	walkInResult := voice.TurnModelReply{
		Goal: "information", GuidanceAction: conversation.GuidanceActionSalonQuestion,
		GuidanceQuestionSubject: conversation.ConversationQuestionAvailability,
		Safety:                  voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(walkIn, corpus, walkInResult); len(problems) != 0 {
		t.Fatalf("walk-in subject alternative problems = %#v", problems)
	}
	healthResult := voice.TurnModelReply{
		Safety: voice.SafetyModelReply{Concern: true, Category: conversation.SafetyCategoryMedicalSuitability},
	}
	if problems := EvaluateResult(health, corpus, healthResult); len(problems) != 0 {
		t.Fatalf("health category alternative problems = %#v", problems)
	}
	availabilityResult := voice.TurnModelReply{
		Goal: "book_appointment", GuidanceAction: conversation.GuidanceActionBook,
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(initialAvailability, corpus, availabilityResult); len(problems) != 0 {
		t.Fatalf("cross-goal guidance alternative problems = %#v", problems)
	}
}

func TestEvaluateResultAcceptsHoursAsOperationalOpeningIntent(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.ID == "availability-006" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Goal: "information",
		Questions: []voice.QuestionModelReply{{
			Subject: conversation.ConversationQuestionHours, Mode: conversation.ConversationQuestionModeDetails,
		}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("opening-hours availability problems = %#v", problems)
	}
}

func TestEvaluateResultMirrorsAlternativeStaffNormalizationAndTargetOwnership(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.ID == "counterexample-002" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActSet, Entity: conversation.ConversationEntityStaff, Subject: "alternative",
			SourceIDs: []string{"svc_gel_mani"}, TargetIDs: []string{"staff_mai"},
		}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	problems := EvaluateResult(scenario, corpus, result)
	if containsSubstring(problems, "outside catalog") {
		t.Fatalf("entity ownership problems = %#v", problems)
	}
	result.Acts[0].TargetIDs = []string{"svc_gel_mani"}
	problems = EvaluateResult(scenario, corpus, result)
	if !containsSubstring(problems, "staff id outside catalog: svc_gel_mani") {
		t.Fatalf("invalid alternative target problems = %#v", problems)
	}
}

func TestEvaluateResultAcceptsAmbiguousPartyAssignmentOnlyForDeclaredKinds(t *testing.T) {
	corpus := GenerateCorpus()
	var scenario Scenario
	for _, candidate := range corpus.Scenarios {
		if candidate.ID == "party_booking-003" {
			scenario = candidate
			break
		}
	}
	result := voice.TurnModelReply{
		Acts: []voice.ActModelReply{{
			Kind: conversation.ConversationActReplace, Entity: conversation.ConversationEntityService,
			SourceIDs: []string{"svc_classic_mani"}, TargetIDs: []string{"svc_gel_remove"}, GuestRef: "guest_2",
		}},
		Safety: voice.SafetyModelReply{Concern: false},
	}
	if problems := EvaluateResult(scenario, corpus, result); len(problems) != 0 {
		t.Fatalf("ambiguous party assignment problems = %#v", problems)
	}
	result.Acts[0].Kind = conversation.ConversationActRemove
	if problems := EvaluateResult(scenario, corpus, result); !containsSubstring(problems, "required act missing") {
		t.Fatalf("undeclared party operation problems = %#v", problems)
	}
}

func containsSubstring(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
