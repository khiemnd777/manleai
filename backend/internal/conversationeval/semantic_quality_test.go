package conversationeval

import (
	"context"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

func TestP3DirectPilotCoversDistinctInformationalSemantics(t *testing.T) {
	corpus := GeneratePilotCorpus()
	coverage := map[string]bool{}
	fixtures := map[string]bool{}
	for _, scenario := range corpus.Scenarios {
		if scenario.Provenance.Generated {
			t.Fatalf("pilot scenario %s is generated", scenario.ID)
		}
		switch scenario.Expected.GuidanceQuestionSubject {
		case conversation.ConversationQuestionHours:
			coverage["informational_hours"] = true
			fixtures[scenario.CatalogFixture] = true
		case conversation.ConversationQuestionStaff:
			coverage["informational_staff"] = true
			fixtures[scenario.CatalogFixture] = true
		case conversation.ConversationQuestionPolicy:
			coverage["policy_fallback"] = true
			fixtures[scenario.CatalogFixture] = true
		}
		if scenario.Expected.AvailabilityIntent {
			coverage["scheduling_availability"] = true
		}
		if scenario.Expected.GuidanceAction == conversation.GuidanceActionServiceCatalog {
			coverage["service_catalog"] = true
		}
		if scenario.Expected.CurrentBookingSummary {
			coverage["selected_booking_services"] = true
		}
		for _, act := range scenario.Expected.RequiredActs {
			if act.Entity == conversation.ConversationEntityStaff && act.Subject == "alternative" {
				coverage["staff_preference"] = true
			}
		}
	}
	for _, required := range []string{
		"informational_hours", "scheduling_availability", "informational_staff", "staff_preference",
		"policy_fallback", "service_catalog", "selected_booking_services",
	} {
		if !coverage[required] {
			t.Fatalf("direct pilot is missing %s coverage: %#v", required, coverage)
		}
	}
	if len(fixtures) < 2 {
		t.Fatalf("critical informational pilot coverage uses %d fixture(s), want at least two", len(fixtures))
	}
	staff := directScenarioByBaseID(t, corpus, "guidance_salon_question-base-008")
	policy := directScenarioByBaseID(t, corpus, "guidance_salon_question-base-011")
	if staff.Request.CustomerMessage != "Which nail tech I can book with?" ||
		policy.Request.CustomerMessage != "If I come late, salon policy is what?" {
		t.Fatalf("direct ASR-like/non-native fixtures changed: staff=%q policy=%q", staff.Request.CustomerMessage, policy.Request.CustomerMessage)
	}
}

func TestP3RealSalonLiveCanarySelectionOwnsInformationalBoundaries(t *testing.T) {
	corpus := DefaultRealSalonCorpus()
	want := map[string]bool{
		"advice-001": true, "consult-001": true, "question-008": true,
		"question-003": true, "question-005": true, "question-010": true,
		"booking-001": true, "repair-003": true, "safety-001": true, "failure-002": true,
	}
	got := map[string]bool{}
	for _, journey := range corpus.Journeys {
		if journey.LiveCanary {
			got[journey.ID] = true
		}
	}
	if len(got) != RealSalonLiveCanaryJourneys || len(got) != len(want) {
		t.Fatalf("live canary count=%d want=%d: %#v", len(got), len(want), got)
	}
	for journeyID := range want {
		if !got[journeyID] {
			t.Fatalf("P3 live canary is missing %s: %#v", journeyID, got)
		}
	}
}

func TestP3DeterministicEvidenceRoutesInformationWithoutSchedulingSideEffects(t *testing.T) {
	corpus := DefaultRealSalonCorpus()
	report := RunRealSalonDeterministic(context.Background(), corpus)
	if report.ModelExecuted != 0 || report.Passed {
		t.Fatalf("deterministic evidence was mislabeled as a model pass: %#v", report)
	}
	for _, journey := range corpus.Journeys {
		if !journey.LiveCanary {
			continue
		}
		result := p3JourneyResult(t, report, journey.ID)
		if result.Status != RealSalonStatusRuntimeExecuted {
			t.Fatalf("live canary %s failed deterministic preflight: %#v", journey.ID, result.Errors)
		}
	}

	hours := p3JourneyResult(t, report, "question-003")
	p3AssertInformationalTurn(t, hours.Turns[0], conversation.TurnRouteSemanticLane, "structured_business_hours", "business_hours")
	staff := p3JourneyResult(t, report, "question-005")
	p3AssertInformationalTurn(t, staff.Turns[0], conversation.TurnRouteSemanticLane, "structured_staff", "staff_question")
	policy := p3JourneyResult(t, report, "question-010")
	p3AssertInformationalTurn(t, policy.Turns[0], conversation.TurnRouteSemanticLane, "booking_redirect", "structured_answer_unavailable")
	if strings.Contains(policy.Turns[0].AIReply, "service menu") || !strings.Contains(policy.Turns[0].AIReply, "verified answer") {
		t.Fatalf("policy fallback reply=%q", policy.Turns[0].AIReply)
	}

	comparison := p3JourneyResult(t, report, "question-008")
	if comparison.Status != RealSalonStatusRuntimeExecuted || strings.Count(comparison.Turns[0].AIReply, "?") != 1 ||
		comparison.Turns[0].AnswerSource != "structured_service_catalog" {
		t.Fatalf("comparison evidence=%#v", comparison.Turns[0])
	}

	detour := p3JourneyResult(t, report, "repair-003")
	if detour.Status != RealSalonStatusRuntimeExecuted || len(detour.Turns) < 2 {
		t.Fatalf("detour result=%#v", detour)
	}
	turn := detour.Turns[1]
	if turn.AnswerSource != "structured_business_hours" || turn.AnswerSourceReason != "business_hours" ||
		strings.Count(turn.AIReply, "?") != 1 || !contains(turn.SelectedServiceIDs, "svc_willow_gel") ||
		len(turn.WouldCallTools) != 0 || turn.BookingConfirmed || turn.ProviderBookingIDPresent {
		t.Fatalf("detour informational evidence=%#v", turn)
	}
}

func p3JourneyResult(t *testing.T, report RealSalonReport, journeyID string) RealSalonJourneyResult {
	t.Helper()
	for _, result := range report.Results {
		if result.JourneyID == journeyID {
			return result
		}
	}
	t.Fatalf("journey result %s not found", journeyID)
	return RealSalonJourneyResult{}
}

func p3AssertInformationalTurn(t *testing.T, turn RealSalonTurnResult, route string, source string, reason string) {
	t.Helper()
	if turn.TurnRoute != route || turn.InterpreterOutcome != conversation.TurnInterpreterOutcomeAccepted ||
		turn.AnswerSource != source || turn.AnswerSourceReason != reason || len(turn.WouldCallTools) != 0 ||
		turn.BookingConfirmed || turn.ProviderBookingIDPresent {
		t.Fatalf("informational turn evidence=%#v", turn)
	}
}
