package conversation

import "testing"

func TestServiceInterpreterSelectsExactCatalogService(t *testing.T) {
	result := interpretService("I want a gel manicure at 1 PM.", testManicureCatalog())

	if result.Status != serviceUnderstandingStatusSelected {
		t.Fatalf("status = %s, want selected", result.Status)
	}
	if result.Selected == nil || result.Selected.ID != "service_gel" {
		t.Fatalf("selected = %#v, want gel manicure", result.Selected)
	}
	if result.Reason != serviceUnderstandingExact || result.Confidence < 0.99 {
		t.Fatalf("reason/confidence = %s/%f", result.Reason, result.Confidence)
	}
}

func TestServiceInterpreterReturnsCatalogCandidatesForAmbiguousFamily(t *testing.T) {
	result := interpretService("I want to book a manicure for Thursday.", testManicureCatalog())

	if result.Status != serviceUnderstandingStatusAmbiguous {
		t.Fatalf("status = %s, want ambiguous", result.Status)
	}
	if result.Reason != serviceUnderstandingAmbiguousFamily {
		t.Fatalf("reason = %s, want ambiguous family", result.Reason)
	}
	if got := serviceIDs(result.Candidates); !sameStrings(got, []string{"service_classic", "service_dip", "service_gel"}) {
		t.Fatalf("candidate ids = %#v", got)
	}
	if result.Selected != nil {
		t.Fatalf("ambiguous family should not select a service: %#v", result.Selected)
	}
}

func TestServiceInterpreterKeepsAdditiveFamilyPhraseAmbiguous(t *testing.T) {
	result := interpretService("Manicure as well.", testManicureCatalog())

	if result.Status != serviceUnderstandingStatusAmbiguous {
		t.Fatalf("status = %s, want ambiguous; result=%#v", result.Status, result)
	}
	if result.Reason != serviceUnderstandingAmbiguousFamily {
		t.Fatalf("reason = %s, want ambiguous family", result.Reason)
	}
	if got := serviceIDs(result.Candidates); !sameStrings(got, []string{"service_classic", "service_dip", "service_gel"}) {
		t.Fatalf("candidate ids = %#v", got)
	}
	if result.Selected != nil {
		t.Fatalf("additive family phrase should not guess a concrete service: %#v", result.Selected)
	}
}

func TestServiceInterpreterUsesPendingEditCandidatesForShortTargetReply(t *testing.T) {
	services := append(testManicureCatalog(),
		ServiceOption{ID: "service_classic_pedi", Name: "Classic Pedicure"},
		ServiceOption{ID: "service_art", Name: "Nail Art"},
	)
	for _, mode := range []string{pendingServiceEditModeAddSelection, pendingServiceEditModeReplaceSelection} {
		t.Run(mode, func(t *testing.T) {
			session := Session{
				ServiceID: "service_spa_pedi",
				Transcript: []TranscriptMessage{{
					Speaker: SpeakerAI,
					Metadata: map[string]any{
						"pending_service_edit_candidate_ids": []string{"service_classic", "service_dip", "service_gel"},
						"pending_service_edit_mode":          mode,
					},
				}},
			}

			result := interpretServiceForSession("Classic.", session, services, nil, nil)
			if result.Status != serviceUnderstandingStatusSelected || result.Selected == nil || result.Selected.ID != "service_classic" {
				t.Fatalf("result = %#v, want Classic Manicure within pending manicure candidates", result)
			}

			result = interpretServiceForSession("Nail Art.", session, services, nil, nil)
			if result.Status != serviceUnderstandingStatusSelected || result.Selected == nil || result.Selected.ID != "service_art" {
				t.Fatalf("result = %#v, want full-catalog fallback to Nail Art", result)
			}
		})
	}
}

func TestServiceInterpreterUsesCatalogFuzzyFamilyWithoutHardcodedAliases(t *testing.T) {
	tests := []string{"Menikur.", "Manecu."}
	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			result := interpretService(message, testManicureCatalog())
			if result.Status != serviceUnderstandingStatusAmbiguous {
				t.Fatalf("status = %s, want ambiguous", result.Status)
			}
			if result.Reason != serviceUnderstandingFuzzyFamily {
				t.Fatalf("reason = %s, want fuzzy family", result.Reason)
			}
			if got := serviceIDs(result.Candidates); !sameStrings(got, []string{"service_classic", "service_dip", "service_gel"}) {
				t.Fatalf("candidate ids = %#v", got)
			}
		})
	}
}

func TestServiceInterpreterSelectsDistinctNoisyCatalogService(t *testing.T) {
	tests := []string{
		"Clatic manicure.",
		"Klasos manicure.",
		"Classis Manikia.",
		"Klasik manikyur.",
	}
	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			result := interpretService(message, testManicureCatalog())
			if result.Status != serviceUnderstandingStatusSelected {
				t.Fatalf("status = %s, want selected; result=%#v", result.Status, result)
			}
			if result.Reason != serviceUnderstandingFuzzyService {
				t.Fatalf("reason = %s, want fuzzy service", result.Reason)
			}
			if result.Selected == nil || result.Selected.ID != "service_classic" {
				t.Fatalf("selected = %#v, want classic manicure", result.Selected)
			}
		})
	}
}

func TestServiceInterpreterUsesSinglePendingCategoryCandidateWithoutGuessing(t *testing.T) {
	services := []ServiceOption{
		{ID: "classic_mani", Name: "Classic Manicure", CategoryID: "manicure", CategoryName: "Manicure"},
		{ID: "gel_mani", Name: "Gel Manicure", CategoryID: "manicure", CategoryName: "Manicure"},
		{ID: "classic_pedi", Name: "Classic Pedicure", CategoryID: "pedicure", CategoryName: "Pedicure"},
	}

	singleCategoryCandidate := Session{DialogState: DialogState{
		Version: DialogStateVersion,
		Phase:   DialogPhaseConsultation,
		Consultation: &ConsultationState{
			Status:                ConsultationStatusAwaitingSelection,
			RecommendedServiceIDs: []string{"classic_mani", "classic_pedi"},
		},
	}}
	selected := interpretServiceForSession("class manicure", singleCategoryCandidate, services, nil, nil)
	if selected.Status != serviceUnderstandingStatusSelected || selected.Selected == nil || selected.Selected.ID != "classic_mani" {
		t.Fatalf("single pending category candidate = %#v, want Classic Manicure", selected)
	}

	multipleCategoryCandidates := singleCategoryCandidate
	multipleCategoryCandidates.DialogState.Consultation = &ConsultationState{
		Status:                ConsultationStatusAwaitingSelection,
		RecommendedServiceIDs: []string{"classic_mani", "gel_mani"},
	}
	ambiguous := interpretServiceForSession("manicure", multipleCategoryCandidates, services, nil, nil)
	if ambiguous.Status != serviceUnderstandingStatusAmbiguous || ambiguous.Selected != nil || len(ambiguous.Candidates) != 2 {
		t.Fatalf("multiple pending category candidates = %#v, want unresolved manicure choice", ambiguous)
	}
}

func TestServiceInterpreterDoesNotSelectNoisyGenericFamilyAsSpecificService(t *testing.T) {
	result := interpretService("Child manicure.", testManicureCatalog())

	if result.Status != serviceUnderstandingStatusAmbiguous {
		t.Fatalf("status = %s, want ambiguous", result.Status)
	}
	if result.Selected != nil {
		t.Fatalf("selected = %#v, want no guessed service", result.Selected)
	}
}

func TestServiceInterpreterUsesEachSalonCatalog(t *testing.T) {
	catalog := []ServiceOption{
		{ID: "builder_gel", Name: "Builder Gel"},
		{ID: "nail_art", Name: "Nail Art"},
		{ID: "removal", Name: "Gel Removal"},
	}

	result := interpretService("I need gel.", catalog)
	if result.Status != serviceUnderstandingStatusAmbiguous {
		t.Fatalf("status = %s, want ambiguous for salon-specific gel catalog", result.Status)
	}
	if got := serviceIDs(result.Candidates); !sameStrings(got, []string{"builder_gel", "removal"}) {
		t.Fatalf("candidate ids = %#v", got)
	}
}

func TestServiceInterpreterUsesSalonServiceAliases(t *testing.T) {
	aliases := []ServiceAlias{{
		ID:              "alias_1",
		ServiceID:       "service_gel",
		Alias:           "shell manicure",
		NormalizedAlias: "shell manicure",
		Source:          "correction",
		Confidence:      0.97,
	}}

	result := interpretService("Name of service, Shell Manicure.", testManicureCatalog(), aliases)

	if result.Status != serviceUnderstandingStatusSelected {
		t.Fatalf("status = %s, want selected", result.Status)
	}
	if result.Reason != serviceUnderstandingAlias {
		t.Fatalf("reason = %s, want service alias", result.Reason)
	}
	if result.MatchedSource != "correction" {
		t.Fatalf("matched source = %q, want correction", result.MatchedSource)
	}
	if result.Selected == nil || result.Selected.ID != "service_gel" {
		t.Fatalf("selected = %#v, want gel manicure", result.Selected)
	}
}

func TestServiceInterpreterExactCatalogServiceBeatsAlias(t *testing.T) {
	catalog := []ServiceOption{
		{ID: "service_gel", Name: "Gel Manicure"},
		{ID: "service_removal", Name: "Gel Removal"},
	}
	aliases := []ServiceAlias{{
		ID:              "alias_1",
		ServiceID:       "service_gel",
		Alias:           "gel",
		NormalizedAlias: "gel",
		Source:          "owner",
	}}

	result := interpretService("I need gel removal.", catalog, aliases)

	if result.Status != serviceUnderstandingStatusSelected {
		t.Fatalf("status = %s, want selected", result.Status)
	}
	if result.Reason != serviceUnderstandingExact {
		t.Fatalf("reason = %s, want exact catalog service", result.Reason)
	}
	if result.Selected == nil || result.Selected.ID != "service_removal" {
		t.Fatalf("selected = %#v, want gel removal", result.Selected)
	}
}

func testManicureCatalog() []ServiceOption {
	return []ServiceOption{
		{ID: "service_dip", Name: "Dip Powder Manicure"},
		{ID: "service_classic", Name: "Classic Manicure"},
		{ID: "service_gel", Name: "Gel Manicure"},
	}
}

func serviceIDs(services []ServiceOption) []string {
	out := make([]string, 0, len(services))
	for _, service := range services {
		out = append(out, service.ID)
	}
	return out
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, item := range got {
		seen[item]++
	}
	for _, item := range want {
		seen[item]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}
