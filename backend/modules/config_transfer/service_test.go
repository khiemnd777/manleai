package configtransfer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/training"
)

func TestGetBuildsSanitizedConfigurationExportWithKnowledgeBase(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	exportedAt := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
	lastSyncAt := updatedAt.Add(-time.Hour)
	service := newTestService(updatedAt)
	service.pos = &fakePOSConnectionReader{
		connection: &pos.Connection{
			ID:                    "connection_1",
			SalonID:               "salon_1",
			Provider:              pos.ProviderSquare,
			Status:                pos.StatusActive,
			AccessTokenEncrypted:  "encrypted-access-token-value",
			RefreshTokenEncrypted: "encrypted-refresh-token-value",
			MerchantID:            "merchant_1",
			LocationID:            "location_1",
			Scopes:                []string{"APPOINTMENTS_READ", "APPOINTMENTS_WRITE"},
			LastSyncAt:            &lastSyncAt,
			ErrorMessage:          "do not export operational sync errors",
			CreatedAt:             updatedAt,
			UpdatedAt:             updatedAt,
		},
	}
	service.now = func() time.Time { return exportedAt }

	result, err := service.Get(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if result.SchemaVersion != SchemaVersion || !result.ExportedAt.Equal(exportedAt) {
		t.Fatalf("unexpected export metadata: %#v", result)
	}
	if result.SecretsExported || result.OperationalDataExported {
		t.Fatalf("export should not include secrets or operational data: %#v", result)
	}
	if result.KnowledgeBase.Count != 1 || result.KnowledgeBase.Items[0].SourceKey == "" {
		t.Fatalf("knowledge base was not exported with stable source key: %#v", result.KnowledgeBase)
	}
	if len(result.RequiresSecretReentry) != 3 {
		t.Fatalf("secret re-entry providers = %#v, want three providers", result.RequiresSecretReentry)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rawJSON := string(raw)
	for _, forbidden := range []string{
		"encrypted-access-token-value",
		"encrypted-refresh-token-value",
		"access_token",
		"refresh_token",
		"owner_user_id",
		"settings_1",
		"salon_1",
		"do not export operational sync errors",
	} {
		if strings.Contains(rawJSON, forbidden) {
			t.Fatalf("export leaked forbidden value %q in %s", forbidden, rawJSON)
		}
	}
}

func TestPreviewImportSkipsUnsafeAIBookingEnablement(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{publicCanPublish: true, canEnableAI: false}
	service := newTestService(updatedAt)
	service.imports = store
	bundle := testImportBundle(updatedAt)
	bundle.SalonProfile.AIEnabled = true
	bundle.AIReceptionist.BookingMode = "confirmed_booking"

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-ai-gate",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if !result.CanApply {
		t.Fatalf("preview should still be applyable with skipped unsafe fields: %#v", result)
	}
	if sectionSummary(result.Summary, SectionSalon).Skipped == 0 {
		t.Fatalf("salon summary should skip ai_enabled: %#v", result.Summary)
	}
	if sectionSummary(result.Summary, SectionAI).Skipped == 0 {
		t.Fatalf("AI summary should skip confirmed booking mode: %#v", result.Summary)
	}
	if len(result.Warnings) < 2 {
		t.Fatalf("warnings = %#v, want AI gating warnings", result.Warnings)
	}
}

func TestApplyImportIsIdempotentForKnowledgeSourceKeys(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	knowledge := &fakeKnowledgeReader{items: []training.KnowledgeItem{}}
	store := &fakeImportStore{publicCanPublish: true, canEnableAI: true, knowledge: knowledge}
	service := newTestService(updatedAt)
	service.knowledge = knowledge
	service.imports = store
	bundle := testImportBundle(updatedAt)

	first, err := service.ApplyImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-1",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("first ApplyImport returned error: %v", err)
	}
	if sectionSummary(first.Summary, SectionKnowledge).Created != 1 {
		t.Fatalf("first import summary = %#v, want one created knowledge item", first.Summary)
	}
	if len(knowledge.items) != 1 {
		t.Fatalf("knowledge count after first import = %d, want 1", len(knowledge.items))
	}

	second, err := service.ApplyImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-2",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("second ApplyImport returned error: %v", err)
	}
	if sectionSummary(second.Summary, SectionKnowledge).Unchanged != 1 {
		t.Fatalf("second import summary = %#v, want one unchanged knowledge item", second.Summary)
	}
	if len(knowledge.items) != 1 {
		t.Fatalf("knowledge count after repeated import = %d, want no duplicate", len(knowledge.items))
	}
}

func TestPreviewOnboardingImportBlocksExistingSalon(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{ownerHasSalon: true}
	service := newTestService(updatedAt)
	service.imports = store

	result, err := service.PreviewOnboardingImport(context.Background(), "owner_1", ImportRequest{
		RequestID:     "req-onboarding-existing",
		Configuration: testImportBundle(updatedAt),
	})
	if err != nil {
		t.Fatalf("PreviewOnboardingImport returned error: %v", err)
	}
	if result.CanApply {
		t.Fatalf("preview should not be applyable when the owner already has a salon: %#v", result)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Code != "owner_salon_exists" {
		t.Fatalf("conflicts = %#v, want owner_salon_exists", result.Conflicts)
	}
}

func TestApplyOnboardingImportCreatesSalonAndSkipsUnsafeLiveStates(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{publicCanPublish: false, canEnableAI: false}
	service := newTestService(updatedAt)
	service.imports = store
	bundle := testImportBundle(updatedAt)
	bundle.SalonProfile.AIEnabled = true
	bundle.AIReceptionist.BookingMode = "confirmed_booking"
	bundle.PublicBookingPage.PublicCatalogEnabled = true

	result, err := service.ApplyOnboardingImport(context.Background(), "owner_1", ImportRequest{
		RequestID:     "req-onboarding-create",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("ApplyOnboardingImport returned error: %v", err)
	}
	if result.SalonID == "" || result.ImportRunID == "" {
		t.Fatalf("result should include created salon and import run ids: %#v", result)
	}
	if store.onboardingCreates != 1 {
		t.Fatalf("onboarding create count = %d, want 1", store.onboardingCreates)
	}
	if store.lastOnboardingPlan == nil {
		t.Fatalf("store did not receive onboarding plan")
	}
	if store.lastOnboardingPlan.AIEnabled {
		t.Fatalf("AI booking should be skipped for a newly imported salon without Square readiness")
	}
	if store.lastOnboardingPlan.BookingMode == "confirmed_booking" {
		t.Fatalf("confirmed booking mode should be skipped for a newly imported salon without Square readiness")
	}
	if store.lastOnboardingPlan.PublicCatalogEnabled {
		t.Fatalf("public catalog should be skipped for a newly imported salon without service/staff readiness")
	}
	if len(result.Warnings) < 3 {
		t.Fatalf("warnings = %#v, want skipped live-state warnings", result.Warnings)
	}
}

func TestApplyOnboardingImportIsIdempotentForSameRequest(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{publicCanPublish: false, canEnableAI: false}
	service := newTestService(updatedAt)
	service.imports = store
	req := ImportRequest{
		RequestID:     "req-onboarding-idempotent",
		Configuration: testImportBundle(updatedAt),
	}

	first, err := service.ApplyOnboardingImport(context.Background(), "owner_1", req)
	if err != nil {
		t.Fatalf("first ApplyOnboardingImport returned error: %v", err)
	}
	second, err := service.ApplyOnboardingImport(context.Background(), "owner_1", req)
	if err != nil {
		t.Fatalf("second ApplyOnboardingImport returned error: %v", err)
	}
	if first.ImportRunID != second.ImportRunID || first.SalonID != second.SalonID {
		t.Fatalf("same request should return same ids: first=%#v second=%#v", first, second)
	}
	if store.onboardingCreates != 1 {
		t.Fatalf("onboarding create count = %d, want idempotent create count 1", store.onboardingCreates)
	}
}

func newTestService(updatedAt time.Time) *Service {
	knowledge := &fakeKnowledgeReader{
		items: []training.KnowledgeItem{
			{
				ID:        "knowledge_1",
				SalonID:   "salon_1",
				Title:     "Deposit policy",
				Category:  training.CategoryPolicy,
				Body:      "Deposits are required for groups of four or more.",
				Status:    training.StatusActive,
				Source:    training.SourceOwner,
				CreatedAt: updatedAt,
				UpdatedAt: updatedAt,
			},
		},
	}
	return NewService(
		&fakeSalonReader{
			salon: &salon.Salon{
				ID:                "salon_1",
				Name:              "Lotus Nails",
				Phone:             "+16292536211",
				Address:           "1200 W Sample Ave",
				City:              "Chicago",
				State:             "IL",
				ZipCode:           "60601",
				Timezone:          "America/Chicago",
				OwnerUserID:       "owner_1",
				PrimaryLanguage:   "en",
				SecondaryLanguage: "vi",
				HandoffPhone:      "+13125550102",
				AIEnabled:         false,
				ActivePOSProvider: pos.ProviderSquare,
				PublicSlug:        "lotus-nails",
				CreatedAt:         updatedAt,
				UpdatedAt:         updatedAt,
			},
			settings: &salon.Settings{
				ID:                      "settings_1",
				SalonID:                 "salon_1",
				AIGreeting:              "Thanks for calling Lotus Nails.",
				AIVoice:                 "professional_female",
				BookingMode:             "pending_approval",
				RecordingEnabled:        true,
				RecordingConsentMessage: "This call may be recorded.",
				SMSConfirmationEnabled:  true,
				SMSReminderEnabled:      true,
				ReminderHoursBefore:     24,
				HandoffEnabled:          true,
				CreatedAt:               updatedAt,
				UpdatedAt:               updatedAt,
			},
			public: &salon.PublicCatalogSettings{
				SalonID:              "salon_1",
				PublicSlug:           "lotus-nails",
				PublicCatalogEnabled: false,
				PublicPath:           "/s/lotus-nails",
				BookableServiceCount: 4,
				BookableStaffCount:   3,
				CanPublish:           true,
				UpdatedAt:            updatedAt,
			},
		},
		&fakeIntegrationReader{response: testIntegrationResponse(updatedAt)},
		&fakePOSConnectionReader{err: pos.ErrNotFound},
		knowledge,
		&fakeImportStore{publicCanPublish: true, canEnableAI: true, knowledge: knowledge},
	)
}

func testImportBundle(updatedAt time.Time) ConfigurationBundle {
	return ConfigurationBundle{
		SchemaVersion:           SchemaVersion,
		ExportedAt:              updatedAt,
		SecretsExported:         false,
		OperationalDataExported: false,
		SalonProfile: SalonProfileExport{
			Name:              "Lotus Nails",
			Phone:             "+16292536211",
			Timezone:          "America/Chicago",
			PrimaryLanguage:   "en",
			SecondaryLanguage: "vi",
			ActivePOSProvider: pos.ProviderSquare,
			UpdatedAt:         updatedAt,
		},
		AIReceptionist: AIReceptionistExport{
			AIGreeting:              "Thanks for calling Lotus Nails.",
			AIVoice:                 "professional_female",
			BookingMode:             "pending_approval",
			RecordingEnabled:        true,
			RecordingConsentMessage: "This call may be recorded.",
			SMSConfirmationEnabled:  true,
			SMSReminderEnabled:      true,
			ReminderHoursBefore:     24,
			HandoffEnabled:          true,
			UpdatedAt:               updatedAt,
		},
		PublicBookingPage: PublicBookingPageExport{PublicSlug: "lotus-nails", PublicCatalogEnabled: false, PublicPath: "/s/lotus-nails", UpdatedAt: updatedAt},
		Integrations:      *testIntegrationResponse(updatedAt),
		KnowledgeBase: KnowledgeBaseExport{
			Items: []KnowledgeItemExport{
				{
					SourceKey: "knowledge:deposit-policy",
					Title:     "Deposit policy",
					Category:  training.CategoryPolicy,
					Body:      "Deposits are required for groups of four or more.",
					Status:    training.StatusActive,
					Source:    training.SourceOwner,
					CreatedAt: updatedAt,
					UpdatedAt: updatedAt,
				},
			},
			Count: 1,
		},
	}
}

func testIntegrationResponse(updatedAt time.Time) *integrationconfig.IntegrationConfigsResponse {
	return &integrationconfig.IntegrationConfigsResponse{
		Square: integrationconfig.SquareSettingsResponse{
			Provider:               integrationconfig.ProviderSquare,
			Configured:             true,
			Environment:            "sandbox",
			ClientID:               "square-client-id",
			RedirectURL:            "https://api.example.com/api/integrations/square/callback",
			APIVersion:             "2026-05-20",
			ClientSecretConfigured: true,
			ClientSecretSource:     integrationconfig.SecretSourceDatabase,
			UpdatedAt:              &updatedAt,
		},
		Twilio: integrationconfig.TwilioSettingsResponse{
			Provider:            integrationconfig.ProviderTwilio,
			Configured:          true,
			PublicBaseURL:       "https://api.example.com",
			IncomingPath:        "/api/voice/twilio/incoming",
			TurnPath:            "/api/voice/twilio/turn",
			RecordingPath:       "/api/voice/twilio/recording",
			InboundWebhookURL:   "https://api.example.com/api/voice/twilio/incoming",
			TurnWebhookURL:      "https://api.example.com/api/voice/twilio/turn",
			RecordingWebhookURL: "https://api.example.com/api/voice/twilio/recording",
			AuthTokenConfigured: true,
			AuthTokenSource:     integrationconfig.SecretSourceDatabase,
			UpdatedAt:           &updatedAt,
		},
		OpenAI: integrationconfig.OpenAISettingsResponse{
			Provider:           integrationconfig.ProviderOpenAI,
			Enabled:            true,
			Configured:         true,
			BaseURL:            "https://api.openai.com/v1",
			TranscriptionModel: "gpt-4o-mini-transcribe",
			ReplyModel:         "gpt-4.1-mini",
			SpeechModel:        "gpt-4o-mini-tts",
			SpeechVoice:        "alloy",
			APIKeyConfigured:   true,
			APIKeySource:       integrationconfig.SecretSourceDatabase,
			UpdatedAt:          &updatedAt,
		},
	}
}

func sectionSummary(items []ImportSectionSummary, section string) ImportSectionSummary {
	for _, item := range items {
		if item.Section == section {
			return item
		}
	}
	return ImportSectionSummary{}
}

type fakeSalonReader struct {
	salon    *salon.Salon
	settings *salon.Settings
	public   *salon.PublicCatalogSettings
	err      error
}

func (f *fakeSalonReader) Get(ctx context.Context, salonID string, ownerUserID string) (*salon.Salon, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.salon == nil {
		return nil, salon.ErrNotFound
	}
	return f.salon, nil
}

func (f *fakeSalonReader) GetSettings(ctx context.Context, salonID string, ownerUserID string) (*salon.Settings, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.settings == nil {
		return nil, salon.ErrNotFound
	}
	return f.settings, nil
}

func (f *fakeSalonReader) GetPublicCatalogSettings(ctx context.Context, salonID string, ownerUserID string) (*salon.PublicCatalogSettings, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.public == nil {
		return nil, salon.ErrNotFound
	}
	return f.public, nil
}

type fakeIntegrationReader struct {
	response *integrationconfig.IntegrationConfigsResponse
	err      error
}

func (f *fakeIntegrationReader) GetAll(ctx context.Context, salonID string, ownerUserID string) (*integrationconfig.IntegrationConfigsResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.response == nil {
		return nil, errors.New("missing fake integration response")
	}
	return f.response, nil
}

type fakePOSConnectionReader struct {
	connection *pos.Connection
	err        error
}

func (f *fakePOSConnectionReader) GetConnection(ctx context.Context, salonID string, provider string) (*pos.Connection, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.connection == nil {
		return nil, pos.ErrNotFound
	}
	return f.connection, nil
}

type fakeKnowledgeReader struct {
	items []training.KnowledgeItem
	err   error
}

func (f *fakeKnowledgeReader) ListKnowledge(ctx context.Context, salonID string, ownerUserID string) ([]training.KnowledgeItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]training.KnowledgeItem{}, f.items...), nil
}

type fakeImportStore struct {
	publicCanPublish   bool
	canEnableAI        bool
	slugTaken          bool
	ownerHasSalon      bool
	knowledge          *fakeKnowledgeReader
	appliedRuns        map[string]string
	onboardingRuns     map[string]fakeOnboardingRun
	onboardingCreates  int
	lastOnboardingPlan *importPlan
}

type fakeOnboardingRun struct {
	runID       string
	salonID     string
	fingerprint string
}

func (f *fakeImportStore) TargetImportState(ctx context.Context, salonID string, ownerUserID string, current *ConfigurationBundle) (*importTargetState, error) {
	byImportKey := map[string]KnowledgeItemExport{}
	byContentHash := map[string]KnowledgeItemExport{}
	for _, item := range current.KnowledgeBase.Items {
		byImportKey[item.SourceKey] = item
		byContentHash[knowledgeContentHash(item)] = item
	}
	return &importTargetState{
		SalonProfile:           current.SalonProfile,
		AIReceptionist:         current.AIReceptionist,
		PublicBookingPage:      current.PublicBookingPage,
		PublicCanPublish:       f.publicCanPublish,
		CanEnableAIBooking:     f.canEnableAI,
		Integrations:           current.Integrations,
		KnowledgeByImportKey:   byImportKey,
		KnowledgeByContentHash: byContentHash,
	}, nil
}

func (f *fakeImportStore) PublicSlugTaken(ctx context.Context, salonID string, slug string) (bool, error) {
	return f.slugTaken, nil
}

func (f *fakeImportStore) OwnerHasSalon(ctx context.Context, ownerUserID string) (bool, error) {
	return f.ownerHasSalon, nil
}

func (f *fakeImportStore) ExistingOnboardingImport(ctx context.Context, ownerUserID string, requestID string, fingerprint string) (string, string, bool, bool, error) {
	if f.onboardingRuns == nil {
		return "", "", false, false, nil
	}
	run, ok := f.onboardingRuns[requestID]
	if !ok {
		return "", "", false, false, nil
	}
	return run.salonID, run.runID, true, run.fingerprint == fingerprint, nil
}

func (f *fakeImportStore) ApplyImport(ctx context.Context, salonID string, ownerUserID string, plan *importPlan) (string, bool, error) {
	if f.appliedRuns == nil {
		f.appliedRuns = map[string]string{}
	}
	if runID, ok := f.appliedRuns[plan.RequestID]; ok {
		return runID, true, nil
	}
	f.appliedRuns[plan.RequestID] = "run_" + plan.RequestID
	if f.knowledge != nil {
		for _, planned := range plan.Knowledge {
			if planned.Operation == "unchanged" || planned.Operation == "skipped" {
				continue
			}
			found := false
			for i := range f.knowledge.items {
				if f.knowledge.items[i].ImportKey == planned.Item.SourceKey {
					f.knowledge.items[i].Title = planned.Item.Title
					f.knowledge.items[i].Category = planned.Item.Category
					f.knowledge.items[i].Body = planned.Item.Body
					f.knowledge.items[i].Status = planned.Item.Status
					f.knowledge.items[i].Source = planned.Item.Source
					found = true
				}
			}
			if !found {
				f.knowledge.items = append(f.knowledge.items, training.KnowledgeItem{
					ID:        planned.Item.SourceKey,
					SalonID:   salonID,
					ImportKey: planned.Item.SourceKey,
					Title:     planned.Item.Title,
					Category:  planned.Item.Category,
					Body:      planned.Item.Body,
					Status:    planned.Item.Status,
					Source:    planned.Item.Source,
					CreatedAt: planned.Item.CreatedAt,
					UpdatedAt: planned.Item.UpdatedAt,
				})
			}
		}
	}
	return f.appliedRuns[plan.RequestID], false, nil
}

func (f *fakeImportStore) ApplyOnboardingImport(ctx context.Context, ownerUserID string, plan *importPlan) (string, string, bool, error) {
	if f.onboardingRuns == nil {
		f.onboardingRuns = map[string]fakeOnboardingRun{}
	}
	if run, ok := f.onboardingRuns[plan.RequestID]; ok {
		if run.fingerprint != plan.PayloadFingerprint {
			return run.salonID, run.runID, true, ErrImportConflict
		}
		return run.salonID, run.runID, true, nil
	}
	f.onboardingCreates++
	f.lastOnboardingPlan = plan
	run := fakeOnboardingRun{
		runID:       "run_" + plan.RequestID,
		salonID:     "salon_" + plan.RequestID,
		fingerprint: plan.PayloadFingerprint,
	}
	f.onboardingRuns[plan.RequestID] = run
	return run.salonID, run.runID, false, nil
}
