package conversation

import "testing"

func TestGuidanceGoalForActionOwnsEveryGuidanceProtocolValue(t *testing.T) {
	want := map[string]string{
		GuidanceActionBook:           "book_appointment",
		GuidanceActionServiceCatalog: "information",
		GuidanceActionConsultation:   "consultation",
		GuidanceActionSalonQuestion:  "information",
		GuidanceActionNameService:    "book_appointment",
		GuidanceActionHumanHandoff:   "human_handoff",
		GuidanceActionReschedule:     "reschedule_appointment",
		GuidanceActionCancel:         "cancel_appointment",
	}
	for _, action := range GuidanceActionValues() {
		if got := GuidanceGoalForAction(action); got != want[action] {
			t.Fatalf("GuidanceGoalForAction(%q) = %q, want %q", action, got, want[action])
		}
	}
	if got := GuidanceGoalForAction("not-a-protocol-action"); got != "unknown" {
		t.Fatalf("unknown guidance goal = %q", got)
	}
}
