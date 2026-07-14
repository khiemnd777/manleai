package configtransfer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
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
	posReader := &fakePOSConnectionReader{
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
		services: []pos.Service{{
			ID:              "service_1",
			Name:            "Builder Gel",
			DurationMinutes: 75,
			PriceDisplay:    "$70.00",
			ConsultationProfile: &pos.ServiceConsultationProfile{
				ID:                       "profile_1",
				SalonID:                  "salon_1",
				ServiceID:                "service_1",
				Status:                   pos.ConsultationProfileStatusReady,
				RecommendedOutcomes:      []string{pos.ConsultationOutcomeAddStrength},
				CompatibleCurrentSystems: []string{pos.ConsultationSystemNatural},
				LengthCapabilities:       []string{pos.ConsultationLengthKeep},
				PriorityTags:             []string{pos.ConsultationPriorityDurability},
				FinishOptions:            []string{pos.ConsultationFinishGlossy},
				MaintenanceNote:          "Return for a professional rebalance.",
				OwnerApprovedSummary:     "A structured overlay for added strength.",
				Revision:                 4,
				UpdatedBy:                "owner_1",
				CreatedAt:                &updatedAt,
				UpdatedAt:                &updatedAt,
			},
		}},
	}
	service.pos = posReader
	service.services = posReader
	service.knowledge.(*fakeKnowledgeReader).aliases = []training.ServiceAlias{{
		ID:              "alias_1",
		SalonID:         "salon_1",
		ServiceID:       "service_1",
		ServiceName:     "Builder Gel",
		Alias:           "overlay",
		NormalizedAlias: "overlay",
		Source:          training.AliasSourceOwner,
		Status:          training.AliasStatusActive,
		Confidence:      0.94,
		CreatedAt:       updatedAt,
		UpdatedAt:       updatedAt,
	}}
	service.categories = &fakeServiceCategoryReader{items: []pos.ServiceCategory{{
		ID:          "cat_1",
		SalonID:     "salon_1",
		Name:        "Manicure",
		Slug:        "manicure",
		Description: "Hand nail services.",
		Status:      pos.ServiceCategoryStatusActive,
		Source:      pos.ServiceCategorySourceManual,
		SortOrder:   10,
		Aliases: []pos.ServiceCategoryAlias{{
			ID:              "cat_alias_1",
			SalonID:         "salon_1",
			CategoryID:      "cat_1",
			CategoryName:    "Manicure",
			Alias:           "mani",
			NormalizedAlias: "mani",
			Source:          pos.ServiceCategoryAliasSourceOwner,
			Status:          pos.ServiceCategoryStatusActive,
			Confidence:      0.94,
			CreatedAt:       updatedAt,
			UpdatedAt:       updatedAt,
		}},
		CreatedAt: updatedAt,
		UpdatedAt: updatedAt,
	}}}
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
	if result.ServiceCategories.Count != 1 || result.ServiceCategories.Items[0].SourceKey != "service_category:manicure" {
		t.Fatalf("service categories were not exported with stable source key: %#v", result.ServiceCategories)
	}
	if result.ServiceAliases.Count != 1 || result.ServiceAliases.Items[0].SourceKey != "service_alias:overlay" {
		t.Fatalf("service aliases were not exported with stable source key: %#v", result.ServiceAliases)
	}
	if target := result.ServiceAliases.Items[0].TargetService; target.Name != "Builder Gel" || target.DurationMinutes != 75 {
		t.Fatalf("service alias target = %#v, want Builder Gel reference", target)
	}
	if result.ConsultationProfiles.Count != 1 {
		t.Fatalf("consultation profiles = %#v, want one portable profile", result.ConsultationProfiles)
	}
	consultation := result.ConsultationProfiles.Items[0]
	if consultation.TargetService.Name != "Builder Gel" || consultation.TargetService.DurationMinutes != 75 || consultation.Status != pos.ConsultationProfileStatusReady {
		t.Fatalf("consultation profile target = %#v, want portable Builder Gel profile", consultation)
	}
	if got := result.ServiceCategories.Items[0].Aliases[0].SourceKey; got != "service_category_alias:mani" {
		t.Fatalf("category alias source key = %q, want stable alias key", got)
	}
	if result.AIReceptionist.AITone != "natural_human" {
		t.Fatalf("AI tone = %q, want natural_human", result.AIReceptionist.AITone)
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
		"profile_1",
		"owner_1",
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

func TestNormalizeImportBundleDefaultsLegacyAITone(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	bundle := testImportBundle(updatedAt)
	bundle.SchemaVersion = LegacySchemaV2
	bundle.AIReceptionist.AITone = ""

	normalized, err := normalizeImportBundle(bundle)
	if err != nil {
		t.Fatalf("normalizeImportBundle returned error: %v", err)
	}
	if normalized.AIReceptionist.AITone != "professional_warm" {
		t.Fatalf("legacy AI tone = %q, want professional_warm", normalized.AIReceptionist.AITone)
	}
}

func TestPreviewImportPlansRealtimeAndStreamIntegrationFields(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	service := newTestService(updatedAt)
	bundle := testImportBundle(updatedAt)
	bundle.Integrations.Twilio.StreamPath = "/api/voice/twilio/live-stream"
	bundle.Integrations.Twilio.VoiceTransport = "realtime_stream"
	bundle.Integrations.OpenAI.RealtimeEnabled = true
	bundle.Integrations.OpenAI.RealtimeModel = "gpt-realtime-2"
	bundle.Integrations.OpenAI.RealtimeVoice = "marin"
	bundle.Integrations.OpenAI.RealtimeNoiseProfile = "noisy_salon"
	bundle.Integrations.OpenAI.RealtimeInstructions = "Keep answers short."

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-realtime",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if sectionSummary(result.Summary, SectionIntegrations).Updated < 6 {
		t.Fatalf("integration summary = %#v, want stream and realtime field changes", result.Summary)
	}
}

func TestPreviewImportSkipsServiceAliasWhenTargetServiceMissing(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	service := newTestService(updatedAt)
	bundle := testImportBundle(updatedAt)
	bundle.ServiceAliases = ServiceAliasBundleExport{
		Items: []ServiceAliasExport{{
			SourceKey:       "service_alias:overlay",
			Alias:           "overlay",
			NormalizedAlias: "overlay",
			TargetService:   ServiceAliasTargetExport{Name: "Builder Gel", DurationMinutes: 75},
			Source:          training.AliasSourceImport,
			Status:          training.AliasStatusActive,
			Confidence:      0.94,
			CreatedAt:       updatedAt,
			UpdatedAt:       updatedAt,
		}},
		Count: 1,
	}

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-alias-missing-target",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if !result.CanApply {
		t.Fatalf("missing service alias target should skip alias without blocking other configuration: %#v", result)
	}
	if sectionSummary(result.Summary, SectionServiceAliases).Skipped != 1 {
		t.Fatalf("service alias summary = %#v, want one skipped alias", result.Summary)
	}
	if len(result.Warnings) == 0 || result.Warnings[len(result.Warnings)-1].Code != "service_alias_target_not_found" {
		t.Fatalf("warnings = %#v, want missing target warning", result.Warnings)
	}
}

func TestPreviewImportPlansServiceAliasForResolvedTargetService(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	targetKey := serviceAliasTargetKey(ServiceAliasTargetExport{Name: "Builder Gel", DurationMinutes: 75})
	store := &fakeImportStore{
		publicCanPublish: true,
		canEnableAI:      true,
		targetServices: map[string]importServiceTarget{
			targetKey: {
				ServiceID:       "target_service_1",
				Name:            "Builder Gel",
				DurationMinutes: 75,
			},
		},
	}
	service := newTestService(updatedAt)
	service.imports = store
	bundle := testImportBundle(updatedAt)
	bundle.ServiceAliases = ServiceAliasBundleExport{
		Items: []ServiceAliasExport{{
			SourceKey:       "service_alias:overlay",
			Alias:           "overlay",
			NormalizedAlias: "overlay",
			TargetService:   ServiceAliasTargetExport{Name: "Builder Gel", DurationMinutes: 75},
			Source:          training.AliasSourceImport,
			Status:          training.AliasStatusActive,
			Confidence:      0.94,
			CreatedAt:       updatedAt,
			UpdatedAt:       updatedAt,
		}},
		Count: 1,
	}

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-alias-resolved",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if sectionSummary(result.Summary, SectionServiceAliases).Created != 1 {
		t.Fatalf("service alias summary = %#v, want one created alias", result.Summary)
	}
	plan, err := service.buildImportPlan(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-alias-resolved-plan",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("buildImportPlan returned error: %v", err)
	}
	if len(plan.ServiceAliases) != 1 || plan.ServiceAliases[0].TargetServiceID != "target_service_1" {
		t.Fatalf("planned service aliases = %#v, want resolved target service id", plan.ServiceAliases)
	}
}

func TestPreviewImportBlocksServiceAliasCategoryAliasConflict(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	targetKey := serviceAliasTargetKey(ServiceAliasTargetExport{Name: "Builder Gel", DurationMinutes: 75})
	store := &fakeImportStore{
		publicCanPublish:        true,
		canEnableAI:             true,
		activeCategoryAliasKeys: map[string]bool{"overlay": true},
		targetServices: map[string]importServiceTarget{
			targetKey: {
				ServiceID:       "target_service_1",
				Name:            "Builder Gel",
				DurationMinutes: 75,
			},
		},
	}
	service := newTestService(updatedAt)
	service.imports = store
	bundle := testImportBundle(updatedAt)
	bundle.ServiceAliases = ServiceAliasBundleExport{
		Items: []ServiceAliasExport{{
			SourceKey:       "service_alias:overlay",
			Alias:           "overlay",
			NormalizedAlias: "overlay",
			TargetService:   ServiceAliasTargetExport{Name: "Builder Gel", DurationMinutes: 75},
			Source:          training.AliasSourceImport,
			Status:          training.AliasStatusActive,
			Confidence:      0.94,
			CreatedAt:       updatedAt,
			UpdatedAt:       updatedAt,
		}},
		Count: 1,
	}

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-alias-category-conflict",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if result.CanApply {
		t.Fatalf("preview should block active service/category alias conflict: %#v", result)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Code != "service_alias_conflicts_with_category_alias" {
		t.Fatalf("conflicts = %#v, want service alias category conflict", result.Conflicts)
	}
}

func TestPreviewImportPlansReadyConsultationProfileForEligibleSquareService(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	target := ServiceAliasTargetExport{Name: "Structured Gel Overlay", DurationMinutes: 70}
	store := &fakeImportStore{
		publicCanPublish: true,
		canEnableAI:      true,
		targetServices: map[string]importServiceTarget{
			serviceAliasTargetKey(target): {
				ServiceID:            "target_service_overlay",
				Name:                 target.Name,
				DurationMinutes:      target.DurationMinutes,
				ConsultationEligible: true,
			},
		},
	}
	service := newTestService(updatedAt)
	service.imports = store
	bundle := testImportBundle(updatedAt)
	bundle.ConsultationProfiles = testConsultationProfileBundle(target, updatedAt)
	bundle.AIReceptionist.ConsultationEnabled = true

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-overlay",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if !result.CanApply {
		t.Fatalf("eligible consultation profile should be applyable: %#v", result)
	}
	if got := sectionSummary(result.Summary, SectionConsultation).Created; got != 1 {
		t.Fatalf("consultation summary created = %d, want 1: %#v", got, result.Summary)
	}
	plan, err := service.buildImportPlan(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-overlay-plan",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("buildImportPlan returned error: %v", err)
	}
	if len(plan.ConsultationProfiles) != 1 || plan.ConsultationProfiles[0].TargetServiceID != "target_service_overlay" || !plan.ConsultationReady || !plan.ConsultationEnabled {
		t.Fatalf("consultation plan = %#v, want resolved ready profile and enabled runtime", plan)
	}
}

func TestPreviewImportBlocksMissingConsultationProfileTarget(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	service := newTestService(updatedAt)
	bundle := testImportBundle(updatedAt)
	bundle.ConsultationProfiles = testConsultationProfileBundle(ServiceAliasTargetExport{Name: "Silk Wrap Repair", DurationMinutes: 55}, updatedAt)
	bundle.AIReceptionist.ConsultationEnabled = true

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-target-missing",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if result.CanApply {
		t.Fatalf("missing consultation target should block apply: %#v", result)
	}
	if got := sectionSummary(result.Summary, SectionConsultation).Conflicts; got != 1 {
		t.Fatalf("consultation conflicts = %d, want 1: %#v", got, result)
	}
	if !hasIssueCode(result.Conflicts, "consultation_profile_target_not_found") {
		t.Fatalf("conflicts = %#v, want target-not-found issue", result.Conflicts)
	}
}

func TestPreviewImportBlocksReadyProfileForIneligibleService(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	target := ServiceAliasTargetExport{Name: "Natural Nail Repair", DurationMinutes: 35}
	service := newTestService(updatedAt)
	service.imports = &fakeImportStore{
		publicCanPublish: true,
		canEnableAI:      true,
		targetServices: map[string]importServiceTarget{
			serviceAliasTargetKey(target): {
				ServiceID:            "target_service_repair",
				Name:                 target.Name,
				DurationMinutes:      target.DurationMinutes,
				ConsultationEligible: false,
			},
		},
	}
	bundle := testImportBundle(updatedAt)
	bundle.ConsultationProfiles = testConsultationProfileBundle(target, updatedAt)

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-ineligible",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if result.CanApply || !hasIssueCode(result.Conflicts, "consultation_profile_target_ineligible") {
		t.Fatalf("ineligible ready profile should block apply: %#v", result)
	}
}

func TestPreviewImportBlocksAmbiguousConsultationProfileTarget(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	target := ServiceAliasTargetExport{Name: "Express Gel Color", DurationMinutes: 40}
	key := serviceAliasTargetKey(target)
	service := newTestService(updatedAt)
	service.imports = &fakeImportStore{
		publicCanPublish:             true,
		canEnableAI:                  true,
		ambiguousServiceTargets:      map[string]bool{key: true},
		ambiguousConsultationTargets: map[string]bool{key: true},
		targetServices: map[string]importServiceTarget{
			key: {ServiceID: "one_of_two_services", Name: target.Name, DurationMinutes: target.DurationMinutes, ConsultationEligible: true},
		},
	}
	bundle := testImportBundle(updatedAt)
	bundle.ConsultationProfiles = testConsultationProfileBundle(target, updatedAt)

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-ambiguous",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if result.CanApply || !hasIssueCode(result.Conflicts, "consultation_profile_target_ambiguous") {
		t.Fatalf("ambiguous consultation target should block apply: %#v", result)
	}
}

func TestReadyProfileUsesUniqueEligibleTargetWhenLegacyServiceNameIsDuplicated(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	target := ServiceAliasTargetExport{Name: "Gel Rebalance", DurationMinutes: 60}
	key := serviceAliasTargetKey(target)
	service := newTestService(updatedAt)
	service.imports = &fakeImportStore{
		publicCanPublish:        true,
		canEnableAI:             true,
		ambiguousServiceTargets: map[string]bool{key: true},
		targetServices: map[string]importServiceTarget{
			key: {ServiceID: "active_square_service", Name: target.Name, DurationMinutes: target.DurationMinutes, ConsultationEligible: true},
		},
	}
	bundle := testImportBundle(updatedAt)
	bundle.ConsultationProfiles = testConsultationProfileBundle(target, updatedAt)

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-unique-eligible",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if !result.CanApply || len(result.Conflicts) != 0 {
		t.Fatalf("one eligible active-provider target should resolve despite an ineligible duplicate: %#v", result)
	}
}

func TestPreviewImportLeavesIdenticalConsultationProfileUnchanged(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	target := ServiceAliasTargetExport{Name: "Builder Overlay", DurationMinutes: 65, PriceDisplay: "$72.00"}
	profileBundle := testConsultationProfileBundle(target, updatedAt)
	profile := profileBundle.Items[0]
	service := newTestService(updatedAt)
	posReader := &fakePOSConnectionReader{
		err: pos.ErrNotFound,
		services: []pos.Service{{
			ID:              "existing_service_overlay",
			Name:            target.Name,
			DurationMinutes: target.DurationMinutes,
			PriceDisplay:    target.PriceDisplay,
			ConsultationProfile: &pos.ServiceConsultationProfile{
				Status:                   profile.Status,
				RecommendedOutcomes:      profile.RecommendedOutcomes,
				CompatibleCurrentSystems: profile.CompatibleCurrentSystems,
				LengthCapabilities:       profile.LengthCapabilities,
				PriorityTags:             profile.PriorityTags,
				FinishOptions:            profile.FinishOptions,
				MaintenanceNote:          profile.MaintenanceNote,
				OwnerApprovedSummary:     profile.OwnerApprovedSummary,
			},
		}},
	}
	service.pos = posReader
	service.services = posReader
	service.imports = &fakeImportStore{
		publicCanPublish: true,
		canEnableAI:      true,
		targetServices: map[string]importServiceTarget{
			serviceAliasTargetKey(target): {
				ServiceID:            "existing_service_overlay",
				Name:                 target.Name,
				DurationMinutes:      target.DurationMinutes,
				ConsultationEligible: true,
			},
		},
	}
	bundle := testImportBundle(updatedAt)
	bundle.ConsultationProfiles = profileBundle

	plan, err := service.buildImportPlan(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-consultation-unchanged",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("buildImportPlan returned error: %v", err)
	}
	if len(plan.ConsultationProfiles) != 1 || plan.ConsultationProfiles[0].Operation != "unchanged" {
		t.Fatalf("consultation plan = %#v, want unchanged", plan.ConsultationProfiles)
	}
	if got := summary(plan, SectionConsultation).Unchanged; got != 1 {
		t.Fatalf("consultation unchanged = %d, want 1", got)
	}
}

func TestNormalizeV6BundlePreservesExplicitConsultationToggle(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	bundle := testImportBundle(updatedAt)
	bundle.SchemaVersion = LegacySchemaV6
	bundle.AIReceptionist.ConsultationEnabled = false

	normalized, err := normalizeImportBundle(bundle)
	if err != nil {
		t.Fatalf("normalizeImportBundle returned error: %v", err)
	}
	if normalized.AIReceptionist.ConsultationEnabled {
		t.Fatalf("v6 explicit consultation_enabled=false was overwritten")
	}
	if normalized.ConsultationProfiles.Count != 0 || len(normalized.ConsultationProfiles.Items) != 0 {
		t.Fatalf("v6 should not import v7 consultation profile data: %#v", normalized.ConsultationProfiles)
	}
}

func TestInvestorConsultationPackIsValidPortableV7Data(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/lotus-investor-demo-consultation-pack-v7.json")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var bundle ConfigurationBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	normalized, err := normalizeImportBundle(bundle)
	if err != nil {
		t.Fatalf("consultation pack is not importable: %v", err)
	}
	if normalized.ServiceCategories.Count != 5 || normalized.ServiceAliases.Count != 7 || normalized.ConsultationProfiles.Count != 7 {
		t.Fatalf("consultation pack counts = categories:%d aliases:%d profiles:%d, want 5/7/7", normalized.ServiceCategories.Count, normalized.ServiceAliases.Count, normalized.ConsultationProfiles.Count)
	}
	if bundleIncludes(normalized, SectionSalon) || bundleIncludes(normalized, SectionIntegrations) || bundleIncludes(normalized, SectionAI) {
		t.Fatalf("consultation pack must not overwrite salon, provider, or AI runtime settings: %#v", normalized.IncludedSections)
	}
	targets := map[string]importServiceTarget{}
	for i, item := range normalized.ConsultationProfiles.Items {
		targets[serviceAliasTargetKey(item.TargetService)] = importServiceTarget{
			ServiceID:            "production_service_" + strconv.Itoa(i+1),
			Name:                 item.TargetService.Name,
			DurationMinutes:      item.TargetService.DurationMinutes,
			PriceDisplay:         item.TargetService.PriceDisplay,
			ConsultationEligible: true,
		}
	}
	service := newTestService(time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC))
	service.imports = &fakeImportStore{publicCanPublish: true, canEnableAI: true, targetServices: targets}
	preview, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-investor-consultation-pack",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if !preview.CanApply || len(preview.Conflicts) != 0 {
		t.Fatalf("consultation pack preview should apply cleanly: %#v", preview)
	}
	if got := sectionSummary(preview.Summary, SectionCategories).Created; got != 25 {
		t.Fatalf("category taxonomy creates = %d, want 5 categories plus 20 aliases", got)
	}
	if got := sectionSummary(preview.Summary, SectionServiceAliases).Created; got != 7 {
		t.Fatalf("service alias creates = %d, want 7", got)
	}
	if got := sectionSummary(preview.Summary, SectionConsultation).Created; got != 7 {
		t.Fatalf("consultation profile creates = %d, want 7", got)
	}
}

func TestPreviewOnboardingBlocksPartialConsultationPack(t *testing.T) {
	updatedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	bundle := testImportBundle(updatedAt)
	bundle.IncludedSections = []string{SectionCategories, SectionServiceAliases, SectionConsultation}
	bundle.ConsultationProfiles = testConsultationProfileBundle(ServiceAliasTargetExport{Name: "Builder Overlay", DurationMinutes: 65}, updatedAt)
	service := newTestService(updatedAt)

	result, err := service.PreviewOnboardingImport(context.Background(), "owner_1", ImportRequest{
		RequestID:     "req-partial-onboarding",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewOnboardingImport returned error: %v", err)
	}
	if result.CanApply || !hasIssueCode(result.Conflicts, "onboarding_requires_full_bundle") {
		t.Fatalf("partial data pack must be imported after onboarding: %#v", result)
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

func TestPreviewOnboardingImportAllowsAvailablePublicSlugWithoutSalonID(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{publicCanPublish: true, canEnableAI: true}
	service := newTestService(updatedAt)
	service.imports = store

	result, err := service.PreviewOnboardingImport(context.Background(), "owner_1", ImportRequest{
		RequestID:     "req-onboarding-slug",
		Configuration: testImportBundle(updatedAt),
	})
	if err != nil {
		t.Fatalf("PreviewOnboardingImport returned error: %v", err)
	}
	if !result.CanApply {
		t.Fatalf("preview should be applyable for an available public slug: %#v", result)
	}
	if store.lastSlugSalonID != "" {
		t.Fatalf("onboarding slug check salon id = %q, want empty id for a new salon", store.lastSlugSalonID)
	}
	if len(result.Conflicts) != 0 || sectionSummary(result.Summary, SectionPublic).Conflicts != 0 {
		t.Fatalf("public slug should not create conflicts: result=%#v", result)
	}
}

func TestPreviewOnboardingImportBlocksTakenPublicSlug(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{publicCanPublish: true, canEnableAI: true, slugTaken: true}
	service := newTestService(updatedAt)
	service.imports = store

	result, err := service.PreviewOnboardingImport(context.Background(), "owner_1", ImportRequest{
		RequestID:     "req-onboarding-slug-taken",
		Configuration: testImportBundle(updatedAt),
	})
	if err != nil {
		t.Fatalf("PreviewOnboardingImport returned error: %v", err)
	}
	if result.CanApply {
		t.Fatalf("preview should not be applyable for a taken public slug: %#v", result)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Code != "public_slug_unavailable" {
		t.Fatalf("conflicts = %#v, want public_slug_unavailable", result.Conflicts)
	}
	if sectionSummary(result.Summary, SectionPublic).Conflicts != 1 {
		t.Fatalf("public summary = %#v, want one conflict", result.Summary)
	}
}

func TestPreviewImportCountsPublicSlugCheckFailure(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{
		publicCanPublish: true,
		canEnableAI:      true,
		slugErr:          errors.New("slug check failed"),
	}
	service := newTestService(updatedAt)
	service.imports = store

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-slug-check-failed",
		Configuration: testImportBundle(updatedAt),
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if result.CanApply {
		t.Fatalf("preview should not be applyable when slug check fails: %#v", result)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Code != "public_slug_check_failed" {
		t.Fatalf("conflicts = %#v, want public_slug_check_failed", result.Conflicts)
	}
	if sectionSummary(result.Summary, SectionPublic).Conflicts != 1 {
		t.Fatalf("public summary = %#v, want one conflict", result.Summary)
	}
}

func TestPreviewImportBlocksCategoryAliasServiceAliasConflict(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	store := &fakeImportStore{
		publicCanPublish:       true,
		canEnableAI:            true,
		activeServiceAliasKeys: map[string]bool{"classic": true},
	}
	service := newTestService(updatedAt)
	service.imports = store

	bundle := testImportBundle(updatedAt)
	bundle.ServiceCategories = ServiceCategoryBundleExport{
		Items: []ServiceCategoryExport{{
			SourceKey: "service_category:manicure",
			Name:      "Manicure",
			Slug:      "manicure",
			Status:    pos.ServiceCategoryStatusActive,
			Source:    pos.ServiceCategorySourceManual,
			SortOrder: 10,
			Aliases: []ServiceCategoryAliasExport{{
				SourceKey:       "service_category_alias:classic",
				Alias:           "classic",
				NormalizedAlias: "classic",
				Source:          pos.ServiceCategoryAliasSourceOwner,
				Status:          pos.ServiceCategoryStatusActive,
				Confidence:      0.94,
			}},
		}},
		Count: 1,
	}

	result, err := service.PreviewImport(context.Background(), "salon_1", "owner_1", ImportRequest{
		RequestID:     "req-category-conflict",
		Configuration: bundle,
	})
	if err != nil {
		t.Fatalf("PreviewImport returned error: %v", err)
	}
	if result.CanApply {
		t.Fatalf("CanApply = true, want conflict for service alias overlap: %#v", result)
	}
	found := false
	for _, issue := range result.Conflicts {
		if issue.Code == "category_alias_conflicts_with_service_alias" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conflicts = %#v, want category/service alias conflict", result.Conflicts)
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

func TestImportResponseEncodesEmptyListsAsArrays(t *testing.T) {
	updatedAt := time.Date(2026, 6, 26, 10, 30, 0, 0, time.UTC)
	bundle := testImportBundle(updatedAt)
	plan := newImportPlan(bundle, "fingerprint-empty-lists", "req-empty-lists", "", onboardingImportTargetState())
	plan.Warnings = nil
	plan.Conflicts = nil
	plan.RequiresSecretReentry = nil

	raw, err := json.Marshal(importResponse(plan, false, "run_empty_lists"))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rawJSON := string(raw)
	for _, forbidden := range []string{`"warnings":null`, `"conflicts":null`, `"requires_secret_reentry":null`, `"excluded_data":null`} {
		if strings.Contains(rawJSON, forbidden) {
			t.Fatalf("response encoded nil list as null: %s", rawJSON)
		}
	}
	for _, required := range []string{`"warnings":[]`, `"conflicts":[]`, `"requires_secret_reentry":[]`} {
		if !strings.Contains(rawJSON, required) {
			t.Fatalf("response did not encode empty list as array %s in %s", required, rawJSON)
		}
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
				AITone:                  "natural_human",
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
			AITone:                  "natural_human",
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
			StreamPath:          "/api/voice/twilio/stream",
			VoiceTransport:      "recording",
			InboundWebhookURL:   "https://api.example.com/api/voice/twilio/incoming",
			TurnWebhookURL:      "https://api.example.com/api/voice/twilio/turn",
			RecordingWebhookURL: "https://api.example.com/api/voice/twilio/recording",
			StreamWebhookURL:    "wss://api.example.com/api/voice/twilio/stream",
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
			RealtimeVoice:      "alloy",
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

func hasIssueCode(items []ImportIssue, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func testConsultationProfileBundle(target ServiceAliasTargetExport, updatedAt time.Time) ServiceConsultationProfileBundleExport {
	return ServiceConsultationProfileBundleExport{
		Items: []ServiceConsultationProfileExport{
			{
				SourceKey:                serviceConsultationProfileSourceKey(target),
				TargetService:            target,
				Status:                   pos.ConsultationProfileStatusReady,
				RecommendedOutcomes:      []string{pos.ConsultationOutcomeAddStrength},
				CompatibleCurrentSystems: []string{pos.ConsultationSystemNatural},
				LengthCapabilities:       []string{pos.ConsultationLengthKeep},
				PriorityTags:             []string{pos.ConsultationPriorityDurability},
				FinishOptions:            []string{pos.ConsultationFinishGlossy},
				MaintenanceNote:          "Return for professional maintenance.",
				OwnerApprovedSummary:     "A structured service for clients who want added strength.",
				CreatedAt:                &updatedAt,
				UpdatedAt:                &updatedAt,
			},
		},
		Count: 1,
	}
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
	connection  *pos.Connection
	err         error
	services    []pos.Service
	servicesErr error
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

func (f *fakePOSConnectionReader) ListServices(ctx context.Context, salonID string, provider string) ([]pos.Service, error) {
	if f.servicesErr != nil {
		return nil, f.servicesErr
	}
	return append([]pos.Service{}, f.services...), nil
}

type fakeServiceCategoryReader struct {
	items []pos.ServiceCategory
	err   error
}

func (f *fakeServiceCategoryReader) ListServiceCategories(ctx context.Context, salonID string, ownerUserID string) ([]pos.ServiceCategory, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

type fakeKnowledgeReader struct {
	items   []training.KnowledgeItem
	aliases []training.ServiceAlias
	err     error
}

func (f *fakeKnowledgeReader) ListKnowledge(ctx context.Context, salonID string, ownerUserID string) ([]training.KnowledgeItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]training.KnowledgeItem{}, f.items...), nil
}

func (f *fakeKnowledgeReader) ListServiceAliases(ctx context.Context, salonID string, ownerUserID string) ([]training.ServiceAlias, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]training.ServiceAlias{}, f.aliases...), nil
}

type fakeImportStore struct {
	publicCanPublish             bool
	canEnableAI                  bool
	slugTaken                    bool
	slugErr                      error
	lastSlugSalonID              string
	ownerHasSalon                bool
	knowledge                    *fakeKnowledgeReader
	activeServiceAliasKeys       map[string]bool
	activeCategoryAliasKeys      map[string]bool
	targetServices               map[string]importServiceTarget
	ambiguousServiceTargets      map[string]bool
	ambiguousConsultationTargets map[string]bool
	appliedRuns                  map[string]string
	onboardingRuns               map[string]fakeOnboardingRun
	onboardingCreates            int
	lastOnboardingPlan           *importPlan
}

type fakeOnboardingRun struct {
	runID       string
	salonID     string
	fingerprint string
}

func (f *fakeImportStore) TargetImportState(ctx context.Context, salonID string, ownerUserID string, current *ConfigurationBundle) (*importTargetState, error) {
	byImportKey := map[string]KnowledgeItemExport{}
	byContentHash := map[string]KnowledgeItemExport{}
	categoryBySlug := map[string]ServiceCategoryExport{}
	categoryAliasByKey := map[string]ServiceCategoryAliasExport{}
	serviceAliasByKey := map[string]ServiceAliasExport{}
	consultationProfileByTarget := map[string]ServiceConsultationProfileExport{}
	for _, item := range current.KnowledgeBase.Items {
		byImportKey[item.SourceKey] = item
		byContentHash[knowledgeContentHash(item)] = item
	}
	for _, item := range current.ServiceCategories.Items {
		categoryBySlug[item.Slug] = item
		for _, alias := range item.Aliases {
			categoryAliasByKey[alias.NormalizedAlias] = alias
		}
	}
	for _, item := range current.ServiceAliases.Items {
		serviceAliasByKey[item.NormalizedAlias] = item
	}
	for _, item := range current.ConsultationProfiles.Items {
		consultationProfileByTarget[serviceAliasTargetKey(item.TargetService)] = item
	}
	consultationTargets := map[string]importServiceTarget{}
	ambiguousConsultationTargets := map[string]bool{}
	for key, target := range f.targetServices {
		if !target.ConsultationEligible {
			continue
		}
		consultationTargets[key] = target
		if f.ambiguousConsultationTargets[key] {
			ambiguousConsultationTargets[key] = true
		}
	}
	return &importTargetState{
		SalonProfile:                 current.SalonProfile,
		AIReceptionist:               current.AIReceptionist,
		PublicBookingPage:            current.PublicBookingPage,
		PublicCanPublish:             f.publicCanPublish,
		CanEnableAIBooking:           f.canEnableAI,
		Integrations:                 current.Integrations,
		ServiceCategoryBySlug:        categoryBySlug,
		CategoryAliasByKey:           categoryAliasByKey,
		ActiveServiceAliasKeys:       f.activeServiceAliasKeys,
		ActiveCategoryAliasKeys:      f.activeCategoryAliasKeys,
		ServiceAliasByKey:            serviceAliasByKey,
		ConsultationProfileByTarget:  consultationProfileByTarget,
		ServiceTargetsByKey:          f.targetServices,
		AmbiguousServiceTargets:      f.ambiguousServiceTargets,
		ConsultationTargetsByKey:     consultationTargets,
		AmbiguousConsultationTargets: ambiguousConsultationTargets,
		KnowledgeByImportKey:         byImportKey,
		KnowledgeByContentHash:       byContentHash,
	}, nil
}

func (f *fakeImportStore) PublicSlugTaken(ctx context.Context, salonID string, slug string) (bool, error) {
	f.lastSlugSalonID = salonID
	return f.slugTaken, f.slugErr
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
