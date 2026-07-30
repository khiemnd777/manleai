package conversationeval

import (
	"fmt"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

type scenarioCase struct {
	Text      string
	Fixture   string
	Expected  ExpectedResult
	Configure func(*voice.SemanticEvaluationRequest)
}

type utteranceVariant struct {
	Name    string
	Format  string
	Channel string
}

var fiveUtteranceVariants = []utteranceVariant{
	{Name: "direct_phone", Format: "%s", Channel: conversation.ChannelPhone},
	{Name: "direct_simulator", Format: "%s", Channel: conversation.ChannelSimulator},
	{Name: "phone_opening", Format: "Hi, I'm calling the salon. %s", Channel: conversation.ChannelPhone},
	{Name: "self_correction", Format: "Sorry, let me start over. %s", Channel: conversation.ChannelSimulator},
	{Name: "return_to_request", Format: "Thanks. My actual question is: %s", Channel: conversation.ChannelPhone},
}

var sixUtteranceVariants = append(append([]utteranceVariant(nil), fiveUtteranceVariants...), utteranceVariant{Name: "busy_caller", Format: "I only have a minute. %s", Channel: conversation.ChannelSimulator})

func GenerateCorpus() Corpus {
	corpus := Corpus{
		SchemaVersion:         SchemaVersion,
		TaxonomyRelease:       "us-nail-v1",
		ExpectedScenarioCount: RequiredScenarioCount,
		ExpectedReviewRounds:  RequiredReviewRounds,
		CatalogFixtures:       catalogFixtures(),
	}
	appendCases(&corpus, "guidance_catalog", "Catalog questions across list, count, existence, details, and comparison modes.", guidanceCatalogCases(), sixUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "guidance_consultation", "Needs-based requests for help choosing a nail service.", guidanceConsultationCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "guidance_booking", "Explicit requests to begin booking or name a known service.", guidanceBookingCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "guidance_salon_question", "Operational questions about hours, price, staff, policy, and availability.", guidanceSalonQuestionCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "guidance_handoff", "Explicit human help requests that must not be inferred from unrelated person language.", guidanceHandoffCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "service_selection", "Concrete service selections against standard and nonstandard data-owned catalogs.", serviceSelectionCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "service_edit", "Add, replace, remove, and undo operations on an existing booking draft.", serviceEditCases(), fiveUtteranceVariants, conversation.TurnSemanticContractFull)
	appendCases(&corpus, "availability", "Availability questions asked while a booking draft already contains a service.", availabilityCases(), fiveUtteranceVariants, conversation.TurnSemanticContractFull)
	appendCases(&corpus, "party_booking", "Guest-scoped service changes for a real party booking draft.", partyBookingCases(), fiveUtteranceVariants, conversation.TurnSemanticContractFull)
	appendCases(&corpus, "consultation_details", "Structured consultation needs and corrections without model-authored recommendations.", consultationDetailCases(), fiveUtteranceVariants, conversation.TurnSemanticContractFull)
	appendCases(&corpus, "current_booking", "Questions about the caller's current draft rather than the salon's full catalog.", currentBookingCases(), fiveUtteranceVariants, conversation.TurnSemanticContractFull)
	appendCases(&corpus, "reschedule_cancel", "Reschedule and cancellation goals that remain distinct from new booking.", rescheduleCancelCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "safety", "Safety concerns that require a typed concern while preserving the caller's operational goal.", safetyCases(), fiveUtteranceVariants, conversation.TurnSemanticContractGuidance)
	appendCases(&corpus, "counterexample", "Counterexamples for person, service-menu, and advice language previously vulnerable to phrase routing.", counterexampleCases(), fiveUtteranceVariants, "")
	return corpus
}

// GeneratePilotCorpus selects directly authored, operationally distinct cases
// from the full corpus. Five high-risk caller situations are executed once per
// channel; the remaining forty executions each own a different base case.
// Phrase text remains evaluation evidence and is never used as runtime logic.
func GeneratePilotCorpus() Corpus {
	full := GenerateCorpus()
	pilot := Corpus{
		SchemaVersion:         SchemaVersion,
		TaxonomyRelease:       full.TaxonomyRelease,
		ExpectedScenarioCount: PilotScenarioCount,
		ExpectedReviewRounds:  PilotReviewRounds,
		CatalogFixtures:       full.CatalogFixtures,
	}
	directByBase := map[string]map[string]Scenario{}
	baseOrder := make([]string, 0)
	for _, scenario := range full.Scenarios {
		variant := scenario.Provenance.UtteranceVariant
		if scenario.Provenance.Generated || (variant != "direct_phone" && variant != "direct_simulator") {
			continue
		}
		baseID := scenario.Provenance.BaseCaseID
		if directByBase[baseID] == nil {
			directByBase[baseID] = map[string]Scenario{}
			baseOrder = append(baseOrder, baseID)
		}
		directByBase[baseID][scenario.Request.Channel] = scenario
	}

	// These are regression fixtures for the exact reported failures and one
	// normal catalog-backed selection. They do not participate in runtime
	// classification or reply decisions.
	pairedBaseIDs := []string{
		"guidance_catalog-base-002",
		"guidance_consultation-base-001",
		"guidance_consultation-base-002",
		"guidance_booking-base-001",
		"service_selection-base-001",
	}
	selectedBases := map[string]bool{}
	for _, baseID := range pairedBaseIDs {
		channels := directByBase[baseID]
		for _, channel := range []string{conversation.ChannelPhone, conversation.ChannelSimulator} {
			if scenario, ok := channels[channel]; ok {
				pilot.Scenarios = append(pilot.Scenarios, scenario)
			}
		}
		selectedBases[baseID] = true
	}

	remaining := PilotScenarioCount - len(pilot.Scenarios)
	addBase := func(baseID string) bool {
		if remaining == 0 || selectedBases[baseID] {
			return false
		}
		channel := conversation.ChannelPhone
		if remaining%2 == 0 {
			channel = conversation.ChannelSimulator
		}
		scenario, ok := directByBase[baseID][channel]
		if !ok {
			return false
		}
		pilot.Scenarios = append(pilot.Scenarios, scenario)
		selectedBases[baseID] = true
		remaining--
		return true
	}
	// Fill round-robin by operational family so catalog and consultation do
	// not crowd out booking, editing, availability, party, safety, and
	// appointment-management situations merely because of generator order.
	familyBases := map[string][]string{}
	for _, baseID := range baseOrder {
		channels := directByBase[baseID]
		for _, scenario := range channels {
			familyBases[scenario.Family] = append(familyBases[scenario.Family], baseID)
			break
		}
	}
	// The paid pilot has room for only three initial salon-question cases. Keep
	// those cases semantically distinct so it measures hours, technician
	// information, and policy classification instead of spending all three
	// executions on equivalent hours wording. This is evaluation selection
	// metadata only; the base IDs never participate in runtime routing.
	familyBases["guidance_salon_question"] = prioritizePilotBaseIDs(
		familyBases["guidance_salon_question"],
		"guidance_salon_question-base-001",
		"guidance_salon_question-base-008",
		"guidance_salon_question-base-011",
	)
	familyOffsets := map[string]int{}
	for remaining > 0 {
		progress := false
		for _, family := range requiredFamilies() {
			bases := familyBases[family]
			for familyOffsets[family] < len(bases) {
				baseID := bases[familyOffsets[family]]
				familyOffsets[family]++
				if addBase(baseID) {
					progress = true
					break
				}
			}
			if remaining == 0 {
				break
			}
		}
		if !progress {
			break
		}
	}
	for index := range pilot.Scenarios {
		pilot.Scenarios[index].ID = fmt.Sprintf("pilot-%03d", index+1)
	}
	return pilot
}

func prioritizePilotBaseIDs(baseIDs []string, priorities ...string) []string {
	result := make([]string, 0, len(baseIDs))
	seen := make(map[string]bool, len(baseIDs))
	available := make(map[string]bool, len(baseIDs))
	for _, baseID := range baseIDs {
		available[baseID] = true
	}
	for _, baseID := range priorities {
		if available[baseID] && !seen[baseID] {
			result = append(result, baseID)
			seen[baseID] = true
		}
	}
	for _, baseID := range baseIDs {
		if !seen[baseID] {
			result = append(result, baseID)
			seen[baseID] = true
		}
	}
	return result
}

func appendCases(corpus *Corpus, family string, description string, cases []scenarioCase, variants []utteranceVariant, defaultContract string) {
	for variantIndex, variant := range variants {
		for caseIndex, base := range cases {
			index := variantIndex*len(cases) + caseIndex + 1
			contract := defaultContract
			if contract == "" {
				contract = counterexampleContract(base.Text)
			}
			request := voice.SemanticEvaluationRequest{
				Channel:          variant.Channel,
				CustomerMessage:  withContext(base.Text, variant.Format),
				ExpectedInput:    "caller_goal",
				SemanticContract: contract,
				BookingAction:    conversation.BookingActionBook,
			}
			if contract == conversation.TurnSemanticContractGuidance {
				request.RecognizableGuidanceActions = conversation.GuidanceActionValues()
			}
			if base.Configure != nil {
				base.Configure(&request)
			}
			corpus.Scenarios = append(corpus.Scenarios, Scenario{
				ID:            fmt.Sprintf("%s-%03d", family, index),
				Family:        family,
				Description:   description,
				EvidenceLevel: "semantic_turn_contract",
				Provenance: ScenarioProvenance{
					BaseCaseID: fmt.Sprintf("%s-base-%03d", family, caseIndex+1), UtteranceVariant: variant.Name,
					Generated: variant.Format != "%s", Scope: "single_turn",
				},
				CatalogFixture: base.Fixture,
				Request:        request,
				Expected:       base.Expected,
				Invariants: []string{
					"catalog_bound_semantics",
					"no_pos_confirmation",
					"no_conversation_mutation",
					"same_contract_across_channels",
				},
			})
		}
	}
}

func withContext(text string, contextText string) string {
	text = strings.TrimSpace(text)
	contextText = strings.TrimSpace(contextText)
	if contextText == "" {
		return text
	}
	return strings.TrimSpace(fmt.Sprintf(contextText, text))
}

func guidanceExpected(action string, mode string, subject string) ExpectedResult {
	goal := "unknown"
	switch action {
	case conversation.GuidanceActionBook, conversation.GuidanceActionNameService:
		goal = "book_appointment"
	case conversation.GuidanceActionServiceCatalog, conversation.GuidanceActionSalonQuestion:
		goal = "information"
	case conversation.GuidanceActionConsultation:
		goal = "consultation"
	case conversation.GuidanceActionHumanHandoff:
		goal = "human_handoff"
	case conversation.GuidanceActionReschedule:
		goal = "reschedule_appointment"
	case conversation.GuidanceActionCancel:
		goal = "cancel_appointment"
	}
	return ExpectedResult{
		Goal: goal, GuidanceAction: action, GuidanceCatalogMode: mode, GuidanceQuestionSubject: subject,
		Safety: ExpectedSafety{Checked: true, Concern: false},
	}
}

func guidanceCatalogCases() []scenarioCase {
	list := []string{
		"What services can I book?", "Show me your services.", "Could you walk me through the service menu?", "What nail treatments do you offer?",
		"I'd like to hear the bookable options.", "Can you list what's available?", "What can I get done at the salon?", "Please give me an overview of the menu.",
	}
	count := []string{"How many services are on your menu?", "How many bookable options do you have?", "What's the total number of nail services?", "Can you tell me the service count?"}
	existence := []string{"Do you have any services I can book?", "Is there a nail service menu?", "Are nail treatments available here?", "Do you offer appointments for nail care?"}
	details := []scenarioCase{
		{Text: "Tell me more about Luna Renewal.", Fixture: "aurora", Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeDetails, conversation.ConversationQuestionCatalog)},
		{Text: "What is included with Cloud Foot Reset?", Fixture: "aurora", Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeDetails, conversation.ConversationQuestionCatalog)},
	}
	compare := []scenarioCase{
		{Text: "Can you compare Luna Renewal and Velvet Shield?", Fixture: "aurora", Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeCompare, conversation.ConversationQuestionCatalog)},
		{Text: "What's the difference between Gel Manicure and Classic Manicure?", Fixture: "lotus", Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeCompare, conversation.ConversationQuestionCatalog)},
	}
	cases := make([]scenarioCase, 0, 20)
	for index, text := range list {
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index), Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeList, conversation.ConversationQuestionCatalog)})
	}
	for index, text := range count {
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index + 1), Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeCount, conversation.ConversationQuestionCatalog)})
	}
	for index, text := range existence {
		expected := guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeExistence, conversation.ConversationQuestionCatalog)
		expected.AlternativeGuidance = []ExpectedGuidanceOption{{
			Action: conversation.GuidanceActionServiceCatalog, CatalogMode: conversation.ConversationQuestionModeList,
			QuestionSubject: conversation.ConversationQuestionCatalog,
		}}
		if index >= 2 {
			expected.AlternativeGuidance = append(expected.AlternativeGuidance, ExpectedGuidanceOption{
				Action: conversation.GuidanceActionSalonQuestion, QuestionSubject: conversation.ConversationQuestionAvailability,
			})
		}
		if index == 3 {
			expected.AlternativeGuidance = append(expected.AlternativeGuidance, ExpectedGuidanceOption{Action: conversation.GuidanceActionBook})
		}
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index + 2), Expected: expected})
	}
	cases = append(cases, details...)
	cases = append(cases, compare...)
	return cases
}

func guidanceConsultationCases() []scenarioCase {
	texts := []string{
		"I don't know whether I should choose a service", "I want a service for my nails", "What would fit someone who wants a durable finish?", "Help me narrow down the right nail service.",
		"I'm unsure what works for natural nails.", "Can you guide me based on the look I want?", "I need advice before picking from the menu.", "Which type of appointment should I consider?",
		"I'd like something low maintenance but don't know the service name.", "Can you ask me a few things and help me choose?", "I want to keep my current length and need guidance.", "I want a glossy finish but I'm not sure what to book.",
		"I have gel now and need help deciding what comes next.", "I want stronger nails without guessing a service.", "Please help me compare what fits my priorities.", "I'm new to nail services and need a recommendation process.",
		"I know the result I want, but not the appointment name.", "Could you help choose based on upkeep?", "I need help deciding between the salon's real options.", "Let's figure out the right service before booking.",
	}
	cases := make([]scenarioCase, 0, len(texts))
	for index, text := range texts {
		expected := guidanceExpected(conversation.GuidanceActionConsultation, "", "")
		switch index {
		case 2:
			expected.Consultation.Priorities = []string{conversation.ConsultationPriorityDurability}
		case 8:
			expected.Consultation.Priorities = []string{conversation.ConsultationPriorityLowerMaintenance}
		case 10:
			expected.Consultation.LengthChange = conversation.ConsultationLengthKeep
		case 11:
			expected.Consultation.Finishes = []string{conversation.ConsultationFinishGlossy}
		case 12:
			expected.Consultation.CurrentSystem = conversation.ConsultationSystemGel
		case 13:
			expected.Consultation.DesiredOutcome = conversation.ConsultationOutcomeAddStrength
		}
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index), Expected: expected})
	}
	return cases
}

func guidanceBookingCases() []scenarioCase {
	bookTexts := []string{
		"I'd like to book an appointment.", "Can we start scheduling a visit?", "I want to make a nail appointment.", "Please help me reserve a time.",
		"I'm ready to schedule something.", "Can you begin a booking for me?", "I need appointments for three people.", "Let's schedule for me and one friend.",
	}
	nameTexts := []string{
		"I know the service I want.", "I already have a service in mind.", "Let me give you the appointment name.", "I can tell you exactly what I want to book.",
		"I know which menu item I need.", "I'll name the service for the appointment.", "I have the treatment name ready.", "I don't need advice; I can name it.",
	}
	cases := make([]scenarioCase, 0, 16)
	for index, text := range bookTexts {
		expected := guidanceExpected(conversation.GuidanceActionBook, "", "")
		if index == 6 {
			expected.GuidancePartySize = 3
		}
		if index == 7 {
			expected.GuidancePartySize = 2
		}
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index), Expected: expected})
	}
	for index, text := range nameTexts {
		expected := guidanceExpected(conversation.GuidanceActionNameService, "", "")
		expected.AlternativeGuidance = []ExpectedGuidanceOption{{Action: conversation.GuidanceActionBook}}
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index + 1), Expected: expected})
	}
	return cases
}

func guidanceSalonQuestionCases() []scenarioCase {
	type questionCase struct{ text, subject string }
	questions := []questionCase{
		{"What time do you open tomorrow?", conversation.ConversationQuestionHours}, {"What are your weekend hours?", conversation.ConversationQuestionHours}, {"When does the salon close?", conversation.ConversationQuestionHours},
		{"How much do services cost?", conversation.ConversationQuestionPrice}, {"Can you tell me the price range?", conversation.ConversationQuestionPrice}, {"What does Luna Renewal cost?", conversation.ConversationQuestionPrice},
		{"Who is working this afternoon?", conversation.ConversationQuestionStaff}, {"Which nail tech I can book with?", conversation.ConversationQuestionStaff}, {"Do you have someone who does nail art?", conversation.ConversationQuestionStaff},
		{"Do you accept walk-ins?", conversation.ConversationQuestionPolicy}, {"If I come late, salon policy is what?", conversation.ConversationQuestionPolicy}, {"Can I bring a child with me?", conversation.ConversationQuestionPolicy},
		{"Do you have any openings today?", conversation.ConversationQuestionAvailability}, {"Is anything available Friday afternoon?", conversation.ConversationQuestionAvailability}, {"Can I come in after work?", conversation.ConversationQuestionAvailability}, {"Are there appointments this weekend?", conversation.ConversationQuestionAvailability},
	}
	cases := make([]scenarioCase, 0, 16)
	for index, item := range questions {
		fixture := fixtureByIndex(index)
		if strings.Contains(item.text, "Luna Renewal") {
			fixture = "aurora"
		}
		expected := guidanceExpected(conversation.GuidanceActionSalonQuestion, "", item.subject)
		if item.subject == conversation.ConversationQuestionAvailability {
			expected.AlternativeGuidance = []ExpectedGuidanceOption{{Action: conversation.GuidanceActionBook}}
		}
		if item.text == "Can I come in after work?" {
			expected.AlternativeGuidance = append(expected.AlternativeGuidance, ExpectedGuidanceOption{
				Action: conversation.GuidanceActionSalonQuestion, QuestionSubject: conversation.ConversationQuestionHours,
			})
		}
		if item.text == "Do you accept walk-ins?" {
			expected.AlternativeGuidance = []ExpectedGuidanceOption{{
				Action: conversation.GuidanceActionSalonQuestion, QuestionSubject: conversation.ConversationQuestionAvailability,
			}}
		}
		if item.text == "Do you have someone who does nail art?" {
			expected.AlternativeGuidance = []ExpectedGuidanceOption{{
				Action: conversation.GuidanceActionServiceCatalog, CatalogMode: conversation.ConversationQuestionModeExistence,
				QuestionSubject: conversation.ConversationQuestionCatalog,
			}, {
				Action: conversation.GuidanceActionSalonQuestion, QuestionSubject: conversation.ConversationQuestionAvailability,
			}}
		}
		cases = append(cases, scenarioCase{Text: item.text, Fixture: fixture, Expected: expected})
	}
	return cases
}

func guidanceHandoffCases() []scenarioCase {
	texts := []string{
		"Please connect me with the owner.", "I need to speak with a person.", "Can a human receptionist help me?", "I'd rather talk to someone at the salon.",
		"Please have the manager take this call.", "Can you transfer me to a staff member?", "I need human help with this request.", "Let me speak directly with the salon team.",
	}
	cases := make([]scenarioCase, 0, len(texts))
	for index, text := range texts {
		cases = append(cases, scenarioCase{Text: text, Fixture: fixtureByIndex(index), Expected: guidanceExpected(conversation.GuidanceActionHumanHandoff, "", "")})
	}
	return cases
}

func serviceSelectionCases() []scenarioCase {
	type selection struct{ text, fixture, target string }
	selections := []selection{
		{"Book Gel Manicure for me.", "lotus", "svc_gel_mani"}, {"I'd like Classic Manicure.", "lotus", "svc_classic_mani"}, {"Please add Spa Pedicure as my service.", "lotus", "svc_spa_pedi"}, {"I want Acrylic Full Set.", "lotus", "svc_acrylic_full"},
		{"Dip Powder Manicure is the one I want.", "lotus", "svc_dip_mani"}, {"I need Gel Removal.", "lotus", "svc_gel_remove"}, {"Set me up for Nail Art.", "lotus", "svc_nail_art"}, {"I need Nail Repair.", "lotus", "svc_nail_repair"},
		{"I want the no-chip hands service.", "lotus", "svc_gel_mani"}, {"Fresh feet is what I need.", "lotus", "svc_spa_pedi"}, {"Book the powder color option.", "lotus", "svc_dip_mani"}, {"I call it a gel takeoff; book that.", "lotus", "svc_gel_remove"},
		{"I'd like Luna Renewal.", "aurora", "svc_luna"}, {"Book Velvet Shield.", "aurora", "svc_velvet"}, {"Cloud Foot Reset is my choice.", "aurora", "svc_cloud"}, {"I want the moon refresh service.", "aurora", "svc_luna"},
		{"Please book soft armor.", "aurora", "svc_velvet"}, {"The cloud pedi option, please.", "aurora", "svc_cloud"}, {"I want Harbor Care.", "harbor", "svc_harbor"}, {"Please select the quiet reset service.", "harbor", "svc_harbor"},
	}
	cases := make([]scenarioCase, 0, len(selections))
	for _, item := range selections {
		item := item
		cases = append(cases, scenarioCase{
			Text: item.text, Fixture: item.fixture,
			Expected: guidanceExpected(conversation.GuidanceActionNameService, "", ""),
		})
	}
	return cases
}

func serviceEditCases() []scenarioCase {
	texts := []string{
		"Also add Spa Pedicure.", "Add Nail Art too.", "Include Gel Removal with it.", "I want Nail Repair as another service.", "Please add Classic Manicure as well.",
		"Replace Gel Manicure with Spa Pedicure.", "Change Classic Manicure to Acrylic Full Set.", "Swap Dip Powder Manicure for Gel Manicure.", "Use Nail Art instead of Nail Repair.", "Make it Classic Manicure instead of Gel Manicure.",
		"Remove Gel Manicure from my appointment.", "Take off Spa Pedicure from the booking.", "Drop Nail Art.", "I no longer want Gel Removal.", "Delete Classic Manicure from my services.",
		"Undo that service change.", "Put the last service edit back the way it was.", "Reverse what I just changed.", "Cancel my previous service edit.", "Go back one service change.",
	}
	targets := []string{"svc_spa_pedi", "svc_nail_art", "svc_gel_remove", "svc_nail_repair", "svc_classic_mani"}
	replaceSources := []string{"svc_gel_mani", "svc_classic_mani", "svc_dip_mani", "svc_nail_repair", "svc_gel_mani"}
	replaceTargets := []string{"svc_spa_pedi", "svc_acrylic_full", "svc_gel_mani", "svc_nail_art", "svc_classic_mani"}
	removeSources := []string{"svc_gel_mani", "svc_spa_pedi", "svc_nail_art", "svc_gel_remove", "svc_classic_mani"}
	cases := make([]scenarioCase, 0, 20)
	for index, text := range texts {
		index := index
		expectedAct := ExpectedAct{Entity: conversation.ConversationEntityService}
		selected := []string{"svc_gel_mani"}
		switch {
		case index < 5:
			expectedAct.Kind = conversation.ConversationActAdd
			expectedAct.TargetIDs = []string{targets[index]}
		case index < 10:
			expectedAct.Kind = conversation.ConversationActReplace
			expectedAct.SourceIDs = []string{replaceSources[index-5]}
			expectedAct.TargetIDs = []string{replaceTargets[index-5]}
			selected = []string{replaceSources[index-5]}
		case index < 15:
			expectedAct.Kind = conversation.ConversationActRemove
			expectedAct.SourceIDs = []string{removeSources[index-10]}
			selected = []string{removeSources[index-10], "svc_spa_pedi"}
		default:
			expectedAct.Kind = conversation.ConversationActUndo
		}
		cases = append(cases, scenarioCase{
			Text: text, Fixture: "lotus",
			Expected: ExpectedResult{RequiredActs: []ExpectedAct{expectedAct}, Safety: ExpectedSafety{Checked: true, Concern: false}},
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.ExpectedInput = conversation.ExpectedInputRequestedDate
				req.CurrentBookingStage = conversation.DialogPhaseDrafting
				req.CurrentDraft.ServiceIDs = append([]string(nil), selected...)
				req.SelectedServices = serviceRefs("lotus", selected...)
			},
		})
	}
	return cases
}

func availabilityCases() []scenarioCase {
	texts := []string{
		"What times are open tomorrow?", "Do you have anything Friday afternoon?", "Can I get an appointment before noon?", "Is there a slot after five?",
		"Who can take me Saturday morning?", "What is the earliest opening?", "Do you have a later time that day?", "Could I come in around three?",
		"Are there openings with any technician?", "Can you check availability for next Monday?", "What times work for this service?", "Is the salon free during lunch?",
		"Can I move ahead with the first available time?", "Do you have something in the evening?", "Is there an opening close to four thirty?", "Please check the weekend schedule.",
	}
	cases := make([]scenarioCase, 0, len(texts))
	for _, text := range texts {
		cases = append(cases, scenarioCase{
			Text: text, Fixture: "lotus",
			// Availability questions are executed from their structured subject;
			// goal does not alter this state-owned answer path.
			Expected: ExpectedResult{AvailabilityIntent: true, Safety: ExpectedSafety{Checked: true, Concern: false}},
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.ExpectedInput = conversation.ExpectedInputRequestedDate
				req.CurrentBookingStage = conversation.DialogPhaseDrafting
				req.CurrentDraft.ServiceIDs = []string{"svc_gel_mani"}
				req.SelectedServices = serviceRefs("lotus", "svc_gel_mani")
			},
		})
	}
	return cases
}

func partyBookingCases() []scenarioCase {
	type partyCase struct {
		text, target string
		replace      bool
	}
	items := []partyCase{
		{"Add Spa Pedicure for guest two.", "svc_spa_pedi", false}, {"Guest two also wants Nail Art.", "svc_nail_art", false}, {"Put Gel Removal on the second guest.", "svc_gel_remove", false}, {"Add Nail Repair for guest two.", "svc_nail_repair", false},
		{"The second person wants Classic Manicure too.", "svc_classic_mani", false}, {"Add Acrylic Full Set for the other guest.", "svc_acrylic_full", false}, {"Guest two needs Dip Powder Manicure as well.", "svc_dip_mani", false}, {"Include Gel Manicure for the second guest.", "svc_gel_mani", false},
		{"Replace guest two's Classic Manicure with Spa Pedicure.", "svc_spa_pedi", true}, {"For the second guest, change Classic Manicure to Nail Art.", "svc_nail_art", true}, {"Swap guest two from Classic Manicure to Gel Removal.", "svc_gel_remove", true}, {"Make the other guest's service Nail Repair instead of Classic Manicure.", "svc_nail_repair", true},
		{"Guest two wants Acrylic Full Set instead of Classic Manicure.", "svc_acrylic_full", true}, {"Change the second person's Classic Manicure to Dip Powder Manicure.", "svc_dip_mani", true}, {"Use Gel Manicure for guest two instead of Classic Manicure.", "svc_gel_mani", true}, {"Replace the other guest's Classic Manicure with Gel Removal.", "svc_gel_remove", true},
	}
	cases := make([]scenarioCase, 0, len(items))
	for _, item := range items {
		item := item
		act := ExpectedAct{Kind: conversation.ConversationActAdd, Entity: conversation.ConversationEntityService, TargetIDs: []string{item.target}, GuestRef: "guest_2"}
		if item.text == "Put Gel Removal on the second guest." {
			// Neutral assignment language is operationally ambiguous when that
			// guest already has a service. Runtime source grounding prevents an
			// inferred replacement from mutating the group without clarification.
			act.AlternativeKinds = []string{conversation.ConversationActReplace}
		}
		if item.replace {
			act.Kind = conversation.ConversationActReplace
			act.SourceIDs = []string{"svc_classic_mani"}
		}
		cases = append(cases, scenarioCase{
			Text: item.text, Fixture: "lotus",
			Expected: ExpectedResult{RequiredActs: []ExpectedAct{act}, Safety: ExpectedSafety{Checked: true, Concern: false}},
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.ExpectedInput = conversation.ExpectedInputService
				req.CurrentBookingStage = conversation.DialogPhaseDrafting
				req.CurrentDraft = conversation.ConversationDraftRef{
					ServiceIDs: []string{"svc_gel_mani", "svc_classic_mani"}, PartySize: 2,
					PartyGroups: []conversation.ConversationPartyGroupRef{{GuestRef: "guest_caller", Count: 1, ServiceIDs: []string{"svc_gel_mani"}}, {GuestRef: "guest_2", Count: 1, ServiceIDs: []string{"svc_classic_mani"}}},
				}
				req.SelectedServices = serviceRefs("lotus", "svc_gel_mani", "svc_classic_mani")
			},
		})
	}
	return cases
}

func consultationDetailCases() []scenarioCase {
	type detail struct {
		text     string
		expected ExpectedConsultation
	}
	items := []detail{
		{"My nails are natural right now.", ExpectedConsultation{CurrentSystem: conversation.ConsultationSystemNatural}},
		{"I currently have gel on them.", ExpectedConsultation{CurrentSystem: conversation.ConsultationSystemGel}},
		{"I'm wearing acrylic now.", ExpectedConsultation{CurrentSystem: conversation.ConsultationSystemAcrylic}},
		{"I have dip powder at the moment.", ExpectedConsultation{CurrentSystem: conversation.ConsultationSystemDip}},
		{"I want to add some length.", ExpectedConsultation{LengthChange: conversation.ConsultationLengthAddLength, DesiredOutcome: conversation.ConsultationOutcomeAddLength}},
		{"Keep the length I already have.", ExpectedConsultation{LengthChange: conversation.ConsultationLengthKeep, DesiredOutcome: conversation.ConsultationOutcomeMaintain}},
		{"I'd like to shorten my nails.", ExpectedConsultation{LengthChange: conversation.ConsultationLengthShorten, DesiredOutcome: conversation.ConsultationOutcomeShorten}},
		{"I need more strength for everyday wear.", ExpectedConsultation{DesiredOutcome: conversation.ConsultationOutcomeAddStrength}},
		{"Durability matters most to me.", ExpectedConsultation{Priorities: []string{conversation.ConsultationPriorityDurability}}},
		{"I prefer something lower maintenance.", ExpectedConsultation{Priorities: []string{conversation.ConsultationPriorityLowerMaintenance}}},
		{"I'd like to keep the cost lower.", ExpectedConsultation{Priorities: []string{conversation.ConsultationPriorityLowerCost}}},
		{"I need a shorter salon visit.", ExpectedConsultation{Priorities: []string{conversation.ConsultationPriorityShorterVisit}}},
		{"I want a glossy finish.", ExpectedConsultation{Finishes: []string{conversation.ConsultationFinishGlossy}}},
		{"Make the finish matte.", ExpectedConsultation{Finishes: []string{conversation.ConsultationFinishMatte}}},
		{"I'd like nail art in the finish.", ExpectedConsultation{Finishes: []string{conversation.ConsultationFinishNailArt}}},
		{"I want a natural-looking finish.", ExpectedConsultation{Finishes: []string{conversation.ConsultationFinishNatural}}},
	}
	cases := make([]scenarioCase, 0, len(items))
	for _, item := range items {
		item := item
		expected := ExpectedResult{Goal: "consultation", Consultation: item.expected, Safety: ExpectedSafety{Checked: true, Concern: false}}
		cases = append(cases, scenarioCase{
			Text: item.text, Fixture: "lotus",
			Expected: expected,
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.ExpectedInput = conversation.ExpectedInputConsultationDesiredOutcome
				req.CurrentBookingStage = conversation.DialogPhaseConsultation
				req.Consultation = &conversation.ConsultationState{Status: conversation.ConsultationStatusCollectingNeeds}
			},
		})
	}
	return cases
}

func currentBookingCases() []scenarioCase {
	type currentCase struct{ text, mode string }
	items := []currentCase{
		{"What services are in my appointment now?", conversation.ConversationQuestionModeList},
		{"How many services have I selected?", conversation.ConversationQuestionModeCount},
		{"Is Gel Manicure still in my booking?", conversation.ConversationQuestionModeExistence},
		{"Tell me the details of my current booking.", conversation.ConversationQuestionModeDetails},
		{"Compare the two services in my appointment.", conversation.ConversationQuestionModeCompare},
		{"Can you read back what I chose?", conversation.ConversationQuestionModeList},
		{"Does my booking include Spa Pedicure?", conversation.ConversationQuestionModeExistence},
		{"What's currently on the appointment?", conversation.ConversationQuestionModeList},
	}
	cases := make([]scenarioCase, 0, len(items))
	for _, item := range items {
		item := item
		cases = append(cases, scenarioCase{
			Text: item.text, Fixture: "lotus",
			// The runtime summarizes the current draft from the question subject;
			// information mode is intentionally presentation-neutral for this subject.
			Expected: ExpectedResult{CurrentBookingSummary: true, Safety: ExpectedSafety{Checked: true, Concern: false}},
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.ExpectedInput = conversation.ExpectedInputBookingReview
				req.CurrentBookingStage = conversation.DialogPhaseReview
				req.CurrentDraft.ServiceIDs = []string{"svc_gel_mani", "svc_spa_pedi"}
				req.SelectedServices = serviceRefs("lotus", "svc_gel_mani", "svc_spa_pedi")
			},
		})
	}
	return cases
}

func rescheduleCancelCases() []scenarioCase {
	items := []struct{ text, goal, action string }{
		{"I need to reschedule my appointment.", "reschedule_appointment", conversation.BookingActionReschedule},
		{"Can I move my booking to another day?", "reschedule_appointment", conversation.BookingActionReschedule},
		{"Please change the time of my existing appointment.", "reschedule_appointment", conversation.BookingActionReschedule},
		{"I want a different slot for the appointment I already have.", "reschedule_appointment", conversation.BookingActionReschedule},
		{"I need to cancel my appointment.", "cancel_appointment", conversation.BookingActionCancel},
		{"Please remove my existing booking.", "cancel_appointment", conversation.BookingActionCancel},
		{"I won't make it, so cancel the visit.", "cancel_appointment", conversation.BookingActionCancel},
		{"Can you cancel the appointment on my account?", "cancel_appointment", conversation.BookingActionCancel},
	}
	cases := make([]scenarioCase, 0, len(items))
	for _, item := range items {
		item := item
		action := conversation.GuidanceActionReschedule
		if item.action == conversation.BookingActionCancel {
			action = conversation.GuidanceActionCancel
		}
		cases = append(cases, scenarioCase{
			Text: item.text, Fixture: "lotus", Expected: guidanceExpected(action, "", ""),
		})
	}
	return cases
}

func safetyCases() []scenarioCase {
	items := []struct{ text, category string }{
		{"My nail is painful; is it safe to put acrylic over it?", conversation.SafetyCategoryPain},
		{"I injured my finger and want to know which service treats it.", conversation.SafetyCategoryInjury},
		{"The skin looks infected; can a manicure fix it?", conversation.SafetyCategoryInfection},
		{"I had an allergic reaction to gel; what should I book?", conversation.SafetyCategoryAllergy},
		{"My cuticle is bleeding; can I still get this done?", conversation.SafetyCategoryBleeding},
		{"My finger is swollen; which nail service is medically safe?", conversation.SafetyCategorySwelling},
		{"Is dip powder medically suitable during my treatment?", conversation.SafetyCategoryMedicalSuitability},
		{"There's a health issue around the nail; can the salon treat it?", conversation.SafetyCategoryOtherHealth},
	}
	cases := make([]scenarioCase, 0, len(items))
	for _, item := range items {
		expectedSafety := ExpectedSafety{Checked: true, Concern: true, Category: item.category}
		if item.category != conversation.SafetyCategoryMedicalSuitability {
			expectedSafety.AlternativeCategories = []string{conversation.SafetyCategoryMedicalSuitability}
		}
		cases = append(cases, scenarioCase{Text: item.text, Fixture: "lotus", Expected: ExpectedResult{Safety: expectedSafety}})
	}
	return cases
}

func counterexampleCases() []scenarioCase {
	return []scenarioCase{
		{
			Text: "It is for another person.", Fixture: "lotus",
			Expected: ExpectedResult{ForbiddenGoals: []string{"human_handoff"}, Safety: ExpectedSafety{Checked: true, Concern: false}},
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.SemanticContract = conversation.TurnSemanticContractFull
				req.ExpectedInput = conversation.ExpectedInputService
				req.CurrentDraft.PartySize = 2
			},
		},
		{
			Text: "I need a different person to do the service.", Fixture: "lotus",
			Expected: ExpectedResult{
				RequiredActs: []ExpectedAct{{
					Kind:    conversation.ConversationActSet,
					Entity:  conversation.ConversationEntityStaff,
					Subject: "alternative",
				}},
				ForbiddenGoals: []string{"human_handoff"},
				Safety:         ExpectedSafety{Checked: true, Concern: false},
			},
			Configure: func(req *voice.SemanticEvaluationRequest) {
				req.SemanticContract = conversation.TurnSemanticContractFull
				req.ExpectedInput = conversation.ExpectedInputStaff
				req.CurrentDraft.ServiceIDs = []string{"svc_gel_mani"}
				req.CurrentDraft.StaffID = "staff_mai"
				req.SelectedServices = serviceRefs("lotus", "svc_gel_mani")
				req.SelectedStaff = []conversation.ConversationStaffRef{{StaffID: "staff_mai", StaffName: "Mai"}}
			},
		},
		{Text: "I don't know which service fits me.", Fixture: "aurora", Expected: guidanceExpected(conversation.GuidanceActionConsultation, "", "")},
		{Text: "Show me what services you have.", Fixture: "harbor", Expected: guidanceExpected(conversation.GuidanceActionServiceCatalog, conversation.ConversationQuestionModeList, conversation.ConversationQuestionCatalog)},
	}
}

func counterexampleContract(text string) string {
	if strings.Contains(text, "fits me") || strings.Contains(text, "services you have") {
		return conversation.TurnSemanticContractGuidance
	}
	return conversation.TurnSemanticContractFull
}

func fixtureByIndex(index int) string {
	fixtures := []string{"lotus", "aurora", "harbor"}
	return fixtures[index%len(fixtures)]
}

func serviceRefs(fixture string, ids ...string) []conversation.ConversationServiceRef {
	catalog := catalogFixtures()[fixture]
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	result := make([]conversation.ConversationServiceRef, 0, len(ids))
	for _, service := range catalog.Services {
		if wanted[service.ServiceID] {
			result = append(result, service)
		}
	}
	return result
}

func catalogFixtures() map[string]CatalogFixture {
	readyProfile := func(outcomes []string, systems []string, lengths []string, priorities []string, finishes []string, summary string) *conversation.ConversationConsultationProfileRef {
		return &conversation.ConversationConsultationProfileRef{
			Status: conversation.ConsultationProfileStatusReady, RecommendedOutcomes: outcomes, CompatibleCurrentSystems: systems,
			LengthCapabilities: lengths, PriorityTags: priorities, FinishOptions: finishes, OwnerApprovedSummary: summary, Revision: 1,
		}
	}
	lotusServices := []conversation.ConversationServiceRef{
		{ServiceID: "svc_classic_mani", ServiceName: "Classic Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", ConsultationProfile: readyProfile([]string{conversation.ConsultationOutcomeMaintain, conversation.ConsultationOutcomeColorRefresh}, []string{conversation.ConsultationSystemNatural, conversation.ConsultationSystemRegularPolish}, []string{conversation.ConsultationLengthKeep, conversation.ConsultationLengthShorten}, []string{conversation.ConsultationPriorityLowerCost, conversation.ConsultationPriorityShorterVisit}, []string{conversation.ConsultationFinishRegularPolish, conversation.ConsultationFinishNatural}, "Basic hand and nail care with regular polish options.")},
		{ServiceID: "svc_gel_mani", ServiceName: "Gel Manicure", CategoryID: "cat_mani", CategoryName: "Manicure", ConsultationProfile: readyProfile([]string{conversation.ConsultationOutcomeMaintain, conversation.ConsultationOutcomeColorRefresh}, []string{conversation.ConsultationSystemNatural, conversation.ConsultationSystemGel}, []string{conversation.ConsultationLengthKeep, conversation.ConsultationLengthShorten}, []string{conversation.ConsultationPriorityDurability}, []string{conversation.ConsultationFinishGelPolish, conversation.ConsultationFinishGlossy}, "Hand and nail care finished with gel polish.")},
		{ServiceID: "svc_spa_pedi", ServiceName: "Spa Pedicure", CategoryID: "cat_pedi", CategoryName: "Pedicure"},
		{ServiceID: "svc_acrylic_full", ServiceName: "Acrylic Full Set", CategoryID: "cat_acrylic", CategoryName: "Acrylic"},
		{ServiceID: "svc_dip_mani", ServiceName: "Dip Powder Manicure", CategoryID: "cat_dip", CategoryName: "Dip Powder"},
		{ServiceID: "svc_gel_remove", ServiceName: "Gel Removal", CategoryID: "cat_removal", CategoryName: "Removal"},
		{ServiceID: "svc_nail_art", ServiceName: "Nail Art", CategoryID: "cat_art", CategoryName: "Nail Art"},
		{ServiceID: "svc_nail_repair", ServiceName: "Nail Repair", CategoryID: "cat_repair", CategoryName: "Nail Repair"},
	}
	return map[string]CatalogFixture{
		"lotus": {
			Services: lotusServices,
			Aliases:  []conversation.ConversationServiceAliasRef{{ServiceID: "svc_gel_mani", Alias: "no-chip hands"}, {ServiceID: "svc_spa_pedi", Alias: "fresh feet"}, {ServiceID: "svc_dip_mani", Alias: "powder color"}, {ServiceID: "svc_gel_remove", Alias: "gel takeoff"}},
			Categories: []conversation.ConversationCategoryRef{
				{CategoryID: "cat_mani", CategoryName: "Manicure", Aliases: []string{"hand nails", "mani"}, ServiceIDs: []string{"svc_classic_mani", "svc_gel_mani"}},
				{CategoryID: "cat_pedi", CategoryName: "Pedicure", Aliases: []string{"foot care", "pedi"}, ServiceIDs: []string{"svc_spa_pedi"}},
				{CategoryID: "cat_acrylic", CategoryName: "Acrylic", ServiceIDs: []string{"svc_acrylic_full"}}, {CategoryID: "cat_dip", CategoryName: "Dip Powder", ServiceIDs: []string{"svc_dip_mani"}},
				{CategoryID: "cat_removal", CategoryName: "Removal", ServiceIDs: []string{"svc_gel_remove"}}, {CategoryID: "cat_art", CategoryName: "Nail Art", ServiceIDs: []string{"svc_nail_art"}}, {CategoryID: "cat_repair", CategoryName: "Nail Repair", ServiceIDs: []string{"svc_nail_repair"}},
			},
			Staff:         []conversation.ConversationStaffRef{{StaffID: "staff_mai", StaffName: "Mai"}, {StaffID: "staff_linh", StaffName: "Linh"}},
			BusinessHours: evaluationBusinessHours(),
		},
		"aurora": {
			Services: []conversation.ConversationServiceRef{
				{ServiceID: "svc_luna", ServiceName: "Luna Renewal", CategoryID: "cat_ritual", CategoryName: "Signature Ritual", ConsultationProfile: readyProfile([]string{conversation.ConsultationOutcomeMaintain}, []string{conversation.ConsultationSystemNatural}, []string{conversation.ConsultationLengthKeep}, []string{conversation.ConsultationPriorityLowerMaintenance}, []string{conversation.ConsultationFinishNatural}, "A salon-defined renewal ritual for natural nails.")},
				{ServiceID: "svc_velvet", ServiceName: "Velvet Shield", CategoryID: "cat_protective", CategoryName: "Protective Finish", ConsultationProfile: readyProfile([]string{conversation.ConsultationOutcomeAddStrength}, []string{conversation.ConsultationSystemNatural}, []string{conversation.ConsultationLengthKeep}, []string{conversation.ConsultationPriorityDurability}, []string{conversation.ConsultationFinishGlossy}, "A salon-defined protective finish.")},
				{ServiceID: "svc_cloud", ServiceName: "Cloud Foot Reset", CategoryID: "cat_foot", CategoryName: "Foot Care"},
			},
			Aliases:       []conversation.ConversationServiceAliasRef{{ServiceID: "svc_luna", Alias: "moon refresh"}, {ServiceID: "svc_velvet", Alias: "soft armor"}, {ServiceID: "svc_cloud", Alias: "cloud pedi"}},
			Categories:    []conversation.ConversationCategoryRef{{CategoryID: "cat_ritual", CategoryName: "Signature Ritual", Aliases: []string{"renewal"}, ServiceIDs: []string{"svc_luna"}}, {CategoryID: "cat_protective", CategoryName: "Protective Finish", Aliases: []string{"shield"}, ServiceIDs: []string{"svc_velvet"}}, {CategoryID: "cat_foot", CategoryName: "Foot Care", Aliases: []string{"foot ritual"}, ServiceIDs: []string{"svc_cloud"}}},
			Staff:         []conversation.ConversationStaffRef{{StaffID: "staff_aria", StaffName: "Aria"}},
			BusinessHours: evaluationBusinessHours(),
		},
		"harbor": {
			Services:      []conversation.ConversationServiceRef{{ServiceID: "svc_harbor", ServiceName: "Harbor Care", CategoryID: "cat_harbor", CategoryName: "Quiet Care"}},
			Aliases:       []conversation.ConversationServiceAliasRef{{ServiceID: "svc_harbor", Alias: "quiet reset"}},
			Categories:    []conversation.ConversationCategoryRef{{CategoryID: "cat_harbor", CategoryName: "Quiet Care", Aliases: []string{"simple care"}, ServiceIDs: []string{"svc_harbor"}}},
			Staff:         []conversation.ConversationStaffRef{{StaffID: "staff_sam", StaffName: "Sam"}},
			BusinessHours: evaluationBusinessHours(),
		},
	}
}

func evaluationBusinessHours() []BusinessHourFixture {
	return []BusinessHourFixture{
		{ID: "hours_monday", DayOfWeek: 1, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "hours_tuesday", DayOfWeek: 2, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "hours_wednesday", DayOfWeek: 3, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "hours_thursday", DayOfWeek: 4, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "hours_friday", DayOfWeek: 5, StartLocalTime: "09:00:00", EndLocalTime: "19:00:00"},
		{ID: "hours_saturday", DayOfWeek: 6, StartLocalTime: "10:00:00", EndLocalTime: "17:00:00"},
		{ID: "hours_sunday", DayOfWeek: 0, StartLocalTime: "11:00:00", EndLocalTime: "16:00:00"},
	}
}
