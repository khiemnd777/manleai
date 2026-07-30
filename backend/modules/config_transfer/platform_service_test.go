package configtransfer

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
)

func TestPlatformScopedV7InvestorPackCanonicalizesToV8(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/lotus-investor-demo-consultation-pack-v7.json")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var source ConfigurationBundle
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	// A v7 exporter could include reference-only connection metadata even though
	// this scoped fixture does not. Canonicalization must drop it in either case.
	source.POSConnection = &POSConnectionExport{Provider: "square", Status: "active", MerchantID: "reference-only"}

	canonical, adapted, err := canonicalizePlatformJSONBundle(source)
	if err != nil {
		t.Fatalf("canonicalizePlatformJSONBundle returned error: %v", err)
	}
	if !adapted || canonical.SchemaVersion != SchemaVersion {
		t.Fatalf("canonical schema=%q adapted=%v, want %q/true", canonical.SchemaVersion, adapted, SchemaVersion)
	}
	wantSections := []string{SectionCategories, SectionServiceAliases, SectionConsultation}
	if !sameStrings(canonical.IncludedSections, wantSections) {
		t.Fatalf("canonical sections=%v, want %v", canonical.IncludedSections, wantSections)
	}
	if canonical.POSConnection != nil {
		t.Fatal("v7 reference-only POS connection state must not survive canonicalization")
	}

	normalized, err := normalizeImportBundle(canonical)
	if err != nil {
		t.Fatalf("canonical v8 bundle is not importable: %v", err)
	}
	if normalized.ServiceCategories.Count != 5 || normalized.ServiceAliases.Count != 7 || normalized.ConsultationProfiles.Count != 7 {
		t.Fatalf("canonical counts=categories:%d aliases:%d profiles:%d, want 5/7/7", normalized.ServiceCategories.Count, normalized.ServiceAliases.Count, normalized.ConsultationProfiles.Count)
	}

	v8Raw, err := os.ReadFile("../../../docs/lotus-investor-demo-consultation-pack-v8.json")
	if err != nil {
		t.Fatalf("ReadFile v8 returned error: %v", err)
	}
	var shippedV8 ConfigurationBundle
	if err := json.Unmarshal(v8Raw, &shippedV8); err != nil {
		t.Fatalf("Unmarshal v8 returned error: %v", err)
	}
	shippedNormalized, err := normalizeImportBundle(shippedV8)
	if err != nil {
		t.Fatalf("shipped v8 pack is not importable: %v", err)
	}
	canonicalFingerprint, err := fingerprintBundle(normalized)
	if err != nil {
		t.Fatalf("fingerprint canonical bundle: %v", err)
	}
	shippedFingerprint, err := fingerprintBundle(shippedNormalized)
	if err != nil {
		t.Fatalf("fingerprint shipped v8 bundle: %v", err)
	}
	if shippedFingerprint != canonicalFingerprint {
		t.Fatal("shipped v8 pack must equal the server-canonicalized v7 content pack")
	}
}

func TestPlatformV7AdapterRejectsRuntimeScopeAndOlderSchemas(t *testing.T) {
	tests := []struct {
		name   string
		bundle ConfigurationBundle
	}{
		{
			name: "v7 runtime section",
			bundle: ConfigurationBundle{
				SchemaVersion:    LegacySchemaV7,
				IncludedSections: []string{SectionCategories, SectionAI},
			},
		},
		{
			name: "v7 missing explicit scope",
			bundle: ConfigurationBundle{
				SchemaVersion: LegacySchemaV7,
			},
		},
		{
			name: "v6 remains unsupported",
			bundle: ConfigurationBundle{
				SchemaVersion:    LegacySchemaV6,
				IncludedSections: []string{SectionCategories},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := canonicalizePlatformJSONBundle(test.bundle); !errors.Is(err, ErrUnsupportedSchema) {
				t.Fatalf("canonicalize error=%v, want ErrUnsupportedSchema", err)
			}
		})
	}
}

func TestNormalizePlatformV9LocalHoursRejectsOverlap(t *testing.T) {
	bundle := testImportBundle(time.Now().UTC())
	bundle.SchemaVersion = PlatformSchemaVersion
	bundle.IncludedSections = []string{SectionLocalHours}
	bundle.LocalBusinessHours = LocalBusinessHoursExport{
		ManagementMode: "local",
		Periods: []LocalBusinessHourPeriodExport{
			{DayOfWeek: 1, StartLocalTime: "09:00", EndLocalTime: "12:00"},
			{DayOfWeek: 1, StartLocalTime: "11:30", EndLocalTime: "15:00"},
		},
	}
	if _, err := normalizeImportBundle(bundle); err == nil {
		t.Fatal("overlapping local hour periods must be rejected")
	}
}

func TestCompatibilityV8CannotClaimPlatformLocalHours(t *testing.T) {
	bundle := testImportBundle(time.Now().UTC())
	bundle.SchemaVersion = SchemaVersion
	bundle.IncludedSections = []string{SectionLocalHours}
	bundle.LocalBusinessHours = LocalBusinessHoursExport{ManagementMode: "local"}
	if _, err := normalizeImportBundle(bundle); err == nil {
		t.Fatal("v8 has no local_business_hours contract")
	}
}

func TestPlatformSummaryIncludesLocalBusinessHours(t *testing.T) {
	values := summaryValues(map[string]*ImportSectionSummary{
		SectionLocalHours: {Section: SectionLocalHours, Updated: 1},
	})
	if len(values) != 1 || values[0].Section != SectionLocalHours || values[0].Updated != 1 {
		t.Fatalf("summary=%#v, want local business hours", values)
	}
}

func TestScopedAIExportPreservesAIRuntimeIntentWithoutSalonProfile(t *testing.T) {
	bundle := ConfigurationBundle{SalonProfile: SalonProfileExport{AIEnabled: true, ActivePOSProvider: "square"}}
	result := scopedBundle(bundle, []string{SectionAI})
	if !result.SalonProfile.AIEnabled {
		t.Fatal("AI-only export must retain ai_enabled intent")
	}
	if result.SalonProfile.ActivePOSProvider != "square" {
		t.Fatal("source provider metadata must remain reportable")
	}
	if result.SalonProfile.Name != "" {
		t.Fatal("unselected salon profile fields must be removed")
	}
}

func TestTransferCapabilityOwnership(t *testing.T) {
	tests := []struct {
		section string
		write   bool
		want    string
	}{
		{SectionSalon, false, "business.read"},
		{SectionLocalHours, true, "business.write"},
		{SectionCategories, false, "services.read"},
		{SectionConsultation, true, "services.write"},
		{SectionKnowledge, true, "training.write"},
		{SectionAI, false, "technical.read"},
		{SectionIntegrations, true, "technical.write"},
	}
	for _, test := range tests {
		got, ok := transferSectionCapability(test.section, test.write)
		if !ok || string(got) != test.want {
			t.Fatalf("capability(%s,%v)=%q/%v, want %q/true", test.section, test.write, got, ok, test.want)
		}
	}
}

func TestIntegrationTransferOnlyWritesPersistedProvidersWhosePortableSettingsChanged(t *testing.T) {
	source := integrationconfig.IntegrationConfigsResponse{
		Square: integrationconfig.SquareSettingsResponse{Environment: "production", ClientID: "source-client"},
		OpenAI: integrationconfig.OpenAISettingsResponse{Enabled: true, ReplyModel: "same-model"},
	}
	target := integrationconfig.IntegrationConfigsResponse{
		Square: integrationconfig.SquareSettingsResponse{Environment: "sandbox", ClientID: "target-client"},
		Twilio: integrationconfig.TwilioSettingsResponse{PublicBaseURL: "https://target.example.com"},
		OpenAI: integrationconfig.OpenAISettingsResponse{Enabled: true, ReplyModel: "same-model"},
	}
	providers := changedIntegrationProviders(source, target, []string{integrationconfig.ProviderSquare, integrationconfig.ProviderOpenAI})
	if len(providers) != 1 || providers[0] != integrationconfig.ProviderSquare {
		t.Fatalf("changed providers=%v, want only square", providers)
	}

	bundle := ConfigurationBundle{IntegrationProviders: []string{integrationconfig.ProviderSquare}, Integrations: source}
	mergeUnselectedIntegrationProviders(&bundle, ConfigurationBundle{Integrations: target})
	if bundle.Integrations.OpenAI.ReplyModel != target.OpenAI.ReplyModel || bundle.Integrations.Twilio.PublicBaseURL != target.Twilio.PublicBaseURL {
		t.Fatalf("providers absent from v9 source must preserve destination settings: %#v", bundle.Integrations)
	}
}

func TestTwilioTransferTreatsTenantRouteIdentityAsNonPortable(t *testing.T) {
	source := integrationconfig.IntegrationConfigsResponse{Twilio: integrationconfig.TwilioSettingsResponse{
		VoiceRouteID: "source-route", VoiceRoutingEnabled: true, VoiceInboundNumber: "+13125550101",
		PublicBaseURL: "https://source.example.com", IncomingPath: "/api/voice/twilio/source-route/incoming",
		VoiceTransport: "recording",
	}}
	target := integrationconfig.IntegrationConfigsResponse{Twilio: integrationconfig.TwilioSettingsResponse{
		VoiceRouteID: "target-route", VoiceRoutingEnabled: true, VoiceInboundNumber: "+13125550102",
		PublicBaseURL: "https://target.example.com", IncomingPath: "/api/voice/twilio/target-route/incoming",
		VoiceTransport: "recording",
	}}
	providers := changedIntegrationProviders(source, target, []string{integrationconfig.ProviderTwilio})
	if len(providers) != 0 {
		t.Fatalf("route-only changes selected providers=%v, want no portable Twilio write", providers)
	}
	normalized := normalizeIntegrationConfigs(source)
	if normalized.Twilio.VoiceRouteID != "" || normalized.Twilio.VoiceInboundNumber != "" || normalized.Twilio.PublicBaseURL != "" || normalized.Twilio.IncomingPath != "" || normalized.Twilio.VoiceRoutingEnabled {
		t.Fatalf("normalized transfer exposed tenant route identity: %#v", normalized.Twilio)
	}
}

func TestIntegrationProviderScopeKeepsV9AbsenceAsNoOpAndV8AsCompatibilityInput(t *testing.T) {
	v9, err := normalizeIntegrationProviders(PlatformSchemaVersion, nil)
	if err != nil || len(v9) != 0 {
		t.Fatalf("v9 providers=%v/%v, want explicit no-op", v9, err)
	}
	v8, err := normalizeIntegrationProviders(SchemaVersion, nil)
	if err != nil || len(v8) != 3 {
		t.Fatalf("v8 providers=%v/%v, want three compatibility blocks", v8, err)
	}
}

func TestSecretReentryOnlyReportsMissingDestinationCredentials(t *testing.T) {
	source := integrationconfig.IntegrationConfigsResponse{
		Square: integrationconfig.SquareSettingsResponse{ClientSecretConfigured: true},
		Twilio: integrationconfig.TwilioSettingsResponse{AuthTokenConfigured: true},
	}
	target := integrationconfig.IntegrationConfigsResponse{
		Square: integrationconfig.SquareSettingsResponse{ClientSecretConfigured: true},
	}
	providers := platformSecretReentry(source, target, []string{integrationconfig.ProviderSquare, integrationconfig.ProviderTwilio})
	if len(providers) != 1 || providers[0] != integrationconfig.ProviderTwilio {
		t.Fatalf("secret reentry=%v, want only twilio", providers)
	}
}
