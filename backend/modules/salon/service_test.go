package salon

import "testing"

func TestNormalizePublicSlug(t *testing.T) {
	tests := map[string]string{
		" Lotus Nails Studio ": "lotus-nails-studio",
		"LOTUS__NAILS":         "lotus-nails",
		"lotus--nails":         "lotus-nails",
		"ab":                   "",
		"***":                  "",
	}

	for input, want := range tests {
		if got := normalizePublicSlug(input); got != want {
			t.Fatalf("normalizePublicSlug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidAITone(t *testing.T) {
	for _, value := range []string{
		AIToneProfessionalWarm,
		AIToneNaturalHuman,
		AIToneFriendlyYoung,
		AIToneConciseCalm,
	} {
		if !validAITone(value) {
			t.Fatalf("validAITone(%q) = false, want true", value)
		}
	}
	if validAITone("unrestricted_prompt") {
		t.Fatalf("validAITone accepted unsupported tone")
	}
}

func TestConsultationCanBeEnabledRequiresReadyService(t *testing.T) {
	if consultationCanBeEnabled(true, 0) {
		t.Fatal("consultation enablement accepted zero eligible ready services")
	}
	if !consultationCanBeEnabled(true, 1) {
		t.Fatal("consultation enablement rejected an eligible ready service")
	}
	if !consultationCanBeEnabled(false, 0) {
		t.Fatal("consultation disablement must remain available")
	}
}
