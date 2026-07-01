package conversation

import "testing"

func TestServiceUnderstandingGoldenEval(t *testing.T) {
	lotusCatalog := []ServiceOption{
		{ID: "classic_manicure", Name: "Classic Manicure"},
		{ID: "gel_manicure", Name: "Gel Manicure"},
		{ID: "dip_powder", Name: "Dip Powder Manicure"},
		{ID: "gel_removal", Name: "Gel Removal"},
	}
	studioCatalog := []ServiceOption{
		{ID: "builder_gel", Name: "Builder Gel"},
		{ID: "gel_removal", Name: "Gel Removal"},
		{ID: "nail_art", Name: "Nail Art"},
		{ID: "polish_change", Name: "Polish Change"},
	}
	cases := []struct {
		name          string
		catalog       []ServiceOption
		aliases       []ServiceAlias
		utterance     string
		wantStatus    serviceUnderstandingStatus
		wantReason    string
		wantSelected  string
		wantCandidate []string
	}{
		{
			name:         "exact service selects id",
			catalog:      lotusCatalog,
			utterance:    "I want a gel manicure this Thursday.",
			wantStatus:   serviceUnderstandingStatusSelected,
			wantReason:   serviceUnderstandingExact,
			wantSelected: "gel_manicure",
		},
		{
			name:          "generic family lists catalog services",
			catalog:       lotusCatalog,
			utterance:     "I want manicure at 1 PM.",
			wantStatus:    serviceUnderstandingStatusAmbiguous,
			wantReason:    serviceUnderstandingAmbiguousFamily,
			wantCandidate: []string{"classic_manicure", "gel_manicure", "dip_powder"},
		},
		{
			name:          "stt typo uses fuzzy catalog family",
			catalog:       lotusCatalog,
			utterance:     "Menikur.",
			wantStatus:    serviceUnderstandingStatusAmbiguous,
			wantReason:    serviceUnderstandingFuzzyFamily,
			wantCandidate: []string{"classic_manicure", "gel_manicure", "dip_powder"},
		},
		{
			name:          "mixed language still grounds known catalog family",
			catalog:       lotusCatalog,
			utterance:     "腳 manicure.",
			wantStatus:    serviceUnderstandingStatusAmbiguous,
			wantReason:    serviceUnderstandingAmbiguousFamily,
			wantCandidate: []string{"classic_manicure", "gel_manicure", "dip_powder"},
		},
		{
			name:          "salon-specific gel family uses salon catalog",
			catalog:       studioCatalog,
			utterance:     "I need gel.",
			wantStatus:    serviceUnderstandingStatusAmbiguous,
			wantReason:    serviceUnderstandingAmbiguousFamily,
			wantCandidate: []string{"builder_gel", "gel_removal"},
		},
		{
			name:         "salon-specific exact service",
			catalog:      studioCatalog,
			utterance:    "Builder gel please.",
			wantStatus:   serviceUnderstandingStatusSelected,
			wantReason:   serviceUnderstandingExact,
			wantSelected: "builder_gel",
		},
		{
			name:       "unknown service remains unknown",
			catalog:    studioCatalog,
			utterance:  "I need a haircut.",
			wantStatus: serviceUnderstandingStatusUnknown,
			wantReason: serviceUnderstandingUnknown,
		},
		{
			name:    "owner correction alias selects catalog service",
			catalog: lotusCatalog,
			aliases: []ServiceAlias{{
				ID:              "alias_shell",
				ServiceID:       "gel_manicure",
				Alias:           "shell manicure",
				NormalizedAlias: "shell manicure",
				Source:          "correction",
				Confidence:      0.96,
			}},
			utterance:    "Name of service, shell manicure.",
			wantStatus:   serviceUnderstandingStatusSelected,
			wantReason:   serviceUnderstandingAlias,
			wantSelected: "gel_manicure",
		},
		{
			name:    "catalog exact beats broad owner alias",
			catalog: lotusCatalog,
			aliases: []ServiceAlias{{
				ID:              "alias_gel",
				ServiceID:       "gel_manicure",
				Alias:           "gel",
				NormalizedAlias: "gel",
				Source:          "owner",
			}},
			utterance:    "I need gel removal.",
			wantStatus:   serviceUnderstandingStatusSelected,
			wantReason:   serviceUnderstandingExact,
			wantSelected: "gel_removal",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			result := interpretService(tt.utterance, tt.catalog, tt.aliases)
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s; result=%#v", result.Status, tt.wantStatus, result)
			}
			if result.Reason != tt.wantReason {
				t.Fatalf("reason = %s, want %s; result=%#v", result.Reason, tt.wantReason, result)
			}
			if tt.wantSelected != "" {
				if result.Selected == nil || result.Selected.ID != tt.wantSelected {
					t.Fatalf("selected = %#v, want %s", result.Selected, tt.wantSelected)
				}
			}
			if len(tt.wantCandidate) > 0 {
				if got := serviceIDs(result.Candidates); !sameStrings(got, tt.wantCandidate) {
					t.Fatalf("candidate ids = %#v, want %#v", got, tt.wantCandidate)
				}
			}
		})
	}
}
