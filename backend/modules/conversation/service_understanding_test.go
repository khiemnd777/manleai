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
