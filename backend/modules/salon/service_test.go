package salon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
)

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

func TestPublicCatalogReadinessUsesSelectedAuthorityWithoutUniversalPOSOrStaffGate(t *testing.T) {
	tests := []struct {
		name            string
		authority       string
		services        int
		staff           int
		hours           int
		internalCurrent bool
		externalReady   bool
		wantReady       bool
		wantBlocker     string
	}{
		{
			name: "owner manual needs one canonical service only", authority: booking.SchedulingAuthorityOwnerManual,
			services: 1, wantReady: true,
		},
		{
			name: "internal calendar needs current activation and local hours", authority: booking.SchedulingAuthorityManleAICalendar,
			services: 1, staff: 1, hours: 1, internalCurrent: true, wantReady: true,
		},
		{
			name: "internal calendar fails closed on stale activation", authority: booking.SchedulingAuthorityManleAICalendar,
			services: 1, staff: 1, hours: 1, wantBlocker: "PUBLIC_CALENDAR_ACTIVATION_REQUIRED",
		},
		{
			name: "external catalog needs current connection and synced staff", authority: booking.SchedulingAuthorityExternalProvider,
			services: 1, staff: 1, externalReady: true, wantReady: true,
		},
		{
			name: "external catalog does not reuse canonical-only eligibility", authority: booking.SchedulingAuthorityExternalProvider,
			services: 1, wantBlocker: "PUBLIC_EXTERNAL_CATALOG_NOT_READY",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := &PublicCatalogSettings{
				PublicSlug: "lotus-nails", SchedulingAuthority: test.authority,
				EligibleServiceCount: test.services, EligibleStaffCount: test.staff,
				PublishedHoursCount: test.hours,
			}
			applyPublicCatalogReadinessWithFacts(settings, test.internalCurrent, test.externalReady)
			if settings.CanPublish != test.wantReady {
				t.Fatalf("CanPublish=%v blockers=%#v, want %v", settings.CanPublish, settings.ReadinessBlockers, test.wantReady)
			}
			if test.wantBlocker != "" && !containsPublicCatalogBlocker(settings.ReadinessBlockers, test.wantBlocker) {
				t.Fatalf("blockers=%#v, want %s", settings.ReadinessBlockers, test.wantBlocker)
			}
		})
	}
}

func containsPublicCatalogBlocker(items []PublicCatalogReadinessBlocker, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func TestSettingsJSONExposesSchedulingAuthorityWithoutAcceptingItOnUpdate(t *testing.T) {
	encoded, err := json.Marshal(Settings{SchedulingAuthority: "external_provider"})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if !strings.Contains(string(encoded), `"scheduling_authority":"external_provider"`) {
		t.Fatalf("settings JSON = %s, want scheduling_authority", encoded)
	}

	var update UpdateSettingsRequest
	if err := json.Unmarshal([]byte(`{"scheduling_authority":"owner_manual","ai_greeting":"Hello"}`), &update); err != nil {
		t.Fatalf("unmarshal settings update: %v", err)
	}
	encodedUpdate, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshal settings update: %v", err)
	}
	if strings.Contains(string(encodedUpdate), "scheduling_authority") {
		t.Fatalf("settings update unexpectedly accepts scheduling_authority: %s", encodedUpdate)
	}
}

func TestNormalizeCreateDefaultsOwnerManualAndTrimsOperationContract(t *testing.T) {
	normalized := normalizeCreate(CreateSalonRequest{
		OperationKey: "  create-123  ",
		Name:         "  Lotus Nails  ",
		Phone:        "  555-0100  ",
	})
	if normalized.OperationKey != "create-123" {
		t.Fatalf("operation key=%q, want trimmed create-123", normalized.OperationKey)
	}
	if normalized.SchedulingAuthority != booking.SchedulingAuthorityOwnerManual {
		t.Fatalf("default authority=%q, want owner_manual", normalized.SchedulingAuthority)
	}
	if normalized.Name != "Lotus Nails" || normalized.Phone != "555-0100" {
		t.Fatalf("normalized identity=%q/%q", normalized.Name, normalized.Phone)
	}
	if !validCreateOperationKey(normalized.OperationKey) {
		t.Fatal("trimmed operation key must be valid")
	}
	if validCreateOperationKey("") || validCreateOperationKey(strings.Repeat("x", 257)) {
		t.Fatal("empty or overlong operation key was accepted")
	}
}

func TestValidSchedulingAuthorityAllowsOnlySafeTenantOnboardingAuthority(t *testing.T) {
	if !validSchedulingAuthority(booking.SchedulingAuthorityOwnerManual) {
		t.Fatal("owner_manual must remain the safe onboarding authority")
	}
	for _, authority := range []string{
		"",
		booking.SchedulingAuthorityManleAICalendar,
		booking.SchedulingAuthorityExternalProvider,
		"square",
		"manual",
		"OWNER_MANUAL",
	} {
		if validSchedulingAuthority(authority) {
			t.Fatalf("tenant onboarding authority %q was accepted", authority)
		}
	}
}

func TestCreateSalonPayloadFingerprintUsesNormalizedIntentNotOperationKey(t *testing.T) {
	first := normalizeCreate(CreateSalonRequest{
		OperationKey:        "create-first",
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		Name:                " Lotus Nails ",
		Phone:               " 555-0100 ",
		City:                " Austin ",
	})
	replay := normalizeCreate(CreateSalonRequest{
		OperationKey:        "create-second",
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		Name:                "Lotus Nails",
		Phone:               "555-0100",
		City:                "Austin",
	})
	firstFingerprint, err := createSalonPayloadFingerprint(first)
	if err != nil {
		t.Fatalf("fingerprint first intent: %v", err)
	}
	replayFingerprint, err := createSalonPayloadFingerprint(replay)
	if err != nil {
		t.Fatalf("fingerprint normalized replay: %v", err)
	}
	if firstFingerprint != replayFingerprint {
		t.Fatalf("normalized equivalent intent fingerprints differ: %q/%q", firstFingerprint, replayFingerprint)
	}
	if len(firstFingerprint) != 64 {
		t.Fatalf("fingerprint length=%d, want 64", len(firstFingerprint))
	}

	replay.SchedulingAuthority = booking.SchedulingAuthorityExternalProvider
	changedFingerprint, err := createSalonPayloadFingerprint(replay)
	if err != nil {
		t.Fatalf("fingerprint changed intent: %v", err)
	}
	if changedFingerprint == firstFingerprint {
		t.Fatal("changed scheduling authority reused the original payload fingerprint")
	}
}

func TestSalonAndCreateRequestJSONExposeOnboardingAuthorityContract(t *testing.T) {
	encodedSalon, err := json.Marshal(Salon{
		SchedulingAuthority:        booking.SchedulingAuthorityOwnerManual,
		SchedulingAuthorityVersion: 1,
	})
	if err != nil {
		t.Fatalf("marshal salon: %v", err)
	}
	if !strings.Contains(string(encodedSalon), `"scheduling_authority":"owner_manual"`) ||
		!strings.Contains(string(encodedSalon), `"scheduling_authority_version":1`) {
		t.Fatalf("salon JSON missing authority contract: %s", encodedSalon)
	}

	var request CreateSalonRequest
	if err := json.Unmarshal([]byte(`{"operation_key":"create-json","scheduling_authority":"external_provider","name":"JSON Salon","phone":"555"}`), &request); err != nil {
		t.Fatalf("unmarshal salon create request: %v", err)
	}
	if request.OperationKey != "create-json" || request.SchedulingAuthority != booking.SchedulingAuthorityExternalProvider {
		t.Fatalf("create request contract=%q/%q", request.OperationKey, request.SchedulingAuthority)
	}
}
