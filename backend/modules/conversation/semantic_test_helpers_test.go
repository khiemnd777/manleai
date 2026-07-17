package conversation

func testGuidanceUnderstanding(action, mode, subject string) TurnUnderstanding {
	return TurnUnderstanding{
		Goal:                    GuidanceGoalForAction(action),
		GuidanceAction:          action,
		GuidanceCatalogMode:     mode,
		GuidanceQuestionSubject: subject,
		Confidence:              0.97,
		Reason:                  "semantic_test_fixture",
	}
}

func testQuestionUnderstanding(subject, mode string, serviceIDs ...string) TurnUnderstanding {
	return TurnUnderstanding{
		Goal:       "information",
		Confidence: 0.97,
		Reason:     "semantic_test_fixture",
		Questions: []ConversationQuestion{{
			Subject: subject, Mode: mode, ServiceIDs: append([]string(nil), serviceIDs...),
			Confidence: 0.97, Reason: "semantic_test_fixture",
		}},
	}
}
