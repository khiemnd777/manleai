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
