package pos

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUpdateServiceAIBookableDelegatesOwnerScopedUpdate(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	item, err := service.UpdateServiceAIBookable(context.Background(), "salon_1", "owner_1", " service_1 ", false)
	if err != nil {
		t.Fatalf("UpdateServiceAIBookable returned error: %v", err)
	}
	if item == nil || item.ID != "service_1" || item.AIBookable {
		t.Fatalf("updated service = %#v, want service_1 with ai_bookable=false", item)
	}
	if store.serviceUpdate.salonID != "salon_1" || store.serviceUpdate.ownerUserID != "owner_1" || store.serviceUpdate.serviceID != "service_1" {
		t.Fatalf("unexpected service update scope: %#v", store.serviceUpdate)
	}
}

func TestUpdateStaffAIBookableDelegatesOwnerScopedUpdate(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	item, err := service.UpdateStaffAIBookable(context.Background(), "salon_1", "owner_1", " staff_1 ", true)
	if err != nil {
		t.Fatalf("UpdateStaffAIBookable returned error: %v", err)
	}
	if item == nil || item.ID != "staff_1" || !item.AIBookable {
		t.Fatalf("updated staff = %#v, want staff_1 with ai_bookable=true", item)
	}
	if store.staffUpdate.salonID != "salon_1" || store.staffUpdate.ownerUserID != "owner_1" || store.staffUpdate.staffID != "staff_1" {
		t.Fatalf("unexpected staff update scope: %#v", store.staffUpdate)
	}
}

func TestUpdateAIBookableRejectsMissingIDs(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	_, serviceErr := service.UpdateServiceAIBookable(context.Background(), "salon_1", "owner_1", " ", true)
	if !errors.Is(serviceErr, ErrValidation) {
		t.Fatalf("service error = %v, want ErrValidation", serviceErr)
	}
	_, staffErr := service.UpdateStaffAIBookable(context.Background(), "salon_1", "owner_1", "", true)
	if !errors.Is(staffErr, ErrValidation) {
		t.Fatalf("staff error = %v, want ErrValidation", staffErr)
	}
	if store.serviceUpdate.serviceID != "" || store.staffUpdate.staffID != "" {
		t.Fatalf("store should not be called for invalid IDs: %#v %#v", store.serviceUpdate, store.staffUpdate)
	}
}

func TestCreateServiceNormalizesInputAndDefaultsActive(t *testing.T) {
	store := &fakePOSStore{activeProvider: ProviderSquare}
	service := NewService(store)

	item, err := service.CreateService(context.Background(), "salon_1", "owner_1", ServiceWriteRequest{
		Name:            " Classic Manicure ",
		Description:     "  Basic care ",
		AIDescription:   " AI copy ",
		DurationMinutes: 45,
		PriceFrom:       floatPtr(35),
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if item == nil || item.Name != "Classic Manicure" {
		t.Fatalf("created service = %#v, want trimmed service", item)
	}
	if store.serviceCreate.input.Name != "Classic Manicure" || store.serviceCreate.input.Description != "Basic care" || !store.serviceCreate.input.Active {
		t.Fatalf("unexpected create input: %#v", store.serviceCreate.input)
	}
	if store.serviceCreate.provider != ProviderSquare {
		t.Fatalf("provider = %s, want square", store.serviceCreate.provider)
	}
}

func TestCreateServiceQueuesSyncWhenProviderSupportsServiceUpsert(t *testing.T) {
	store := &fakePOSStore{activeProvider: ProviderSquare}
	service := NewService(store, fakeCapabilityProvider{name: ProviderSquare, capabilities: ProviderCapabilities{ServiceUpsert: true}})

	item, err := service.CreateService(context.Background(), "salon_1", "owner_1", ServiceWriteRequest{
		Name:            "Classic Manicure",
		DurationMinutes: 45,
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if item.SyncStatus != SyncStatusSyncing {
		t.Fatalf("sync status = %s, want syncing", item.SyncStatus)
	}
	if store.syncJob.Operation != SyncOperationUpsertService || store.syncJob.EntityType != EntityTypeService || store.syncJob.EntityID != "service_new" {
		t.Fatalf("unexpected sync job: %#v", store.syncJob)
	}
}

func TestCreateServiceDoesNotQueueWhenProviderDoesNotSupportServiceUpsert(t *testing.T) {
	store := &fakePOSStore{activeProvider: ProviderSquare}
	service := NewService(store, fakeCapabilityProvider{name: ProviderSquare})

	item, err := service.CreateService(context.Background(), "salon_1", "owner_1", ServiceWriteRequest{
		Name:            "Classic Manicure",
		DurationMinutes: 45,
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if item.SyncStatus != SyncStatusLocalOnly {
		t.Fatalf("sync status = %s, want local_only", item.SyncStatus)
	}
	if store.syncJob.Operation != "" {
		t.Fatalf("sync job should not be queued: %#v", store.syncJob)
	}
}

func TestCreateServiceRejectsInvalidInput(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	_, nameErr := service.CreateService(context.Background(), "salon_1", "owner_1", ServiceWriteRequest{
		Name:            " ",
		DurationMinutes: 45,
	})
	if !errors.Is(nameErr, ErrValidation) {
		t.Fatalf("name error = %v, want ErrValidation", nameErr)
	}
	_, durationErr := service.CreateService(context.Background(), "salon_1", "owner_1", ServiceWriteRequest{
		Name:            "Classic Manicure",
		DurationMinutes: 0,
	})
	if !errors.Is(durationErr, ErrValidation) {
		t.Fatalf("duration error = %v, want ErrValidation", durationErr)
	}
	_, priceErr := service.CreateService(context.Background(), "salon_1", "owner_1", ServiceWriteRequest{
		Name:            "Classic Manicure",
		DurationMinutes: 45,
		PriceFrom:       floatPtr(-1),
	})
	if !errors.Is(priceErr, ErrValidation) {
		t.Fatalf("price error = %v, want ErrValidation", priceErr)
	}
	if store.serviceCreate.salonID != "" {
		t.Fatalf("store should not be called for invalid input: %#v", store.serviceCreate)
	}
}

func TestUpdateServiceDelegatesNormalizedInput(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	item, err := service.UpdateService(context.Background(), "salon_1", "owner_1", " service_1 ", ServiceWriteRequest{
		Name:            " Gel Manicure ",
		DurationMinutes: 50,
		Active:          boolPtr(false),
	})
	if err != nil {
		t.Fatalf("UpdateService returned error: %v", err)
	}
	if item == nil || item.ID != "service_1" {
		t.Fatalf("updated service = %#v, want service_1", item)
	}
	if store.serviceUpdateDetails.serviceID != "service_1" || store.serviceUpdateDetails.input.Active {
		t.Fatalf("unexpected update input: %#v", store.serviceUpdateDetails)
	}
}

func TestArchiveServiceRejectsMissingID(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	_, err := service.ArchiveService(context.Background(), "salon_1", "owner_1", " ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("archive error = %v, want ErrValidation", err)
	}
	if store.serviceArchive.serviceID != "" {
		t.Fatalf("store should not be called for invalid archive: %#v", store.serviceArchive)
	}
}

func TestCreateStaffNormalizesInputAndDefaultsActive(t *testing.T) {
	store := &fakePOSStore{activeProvider: ProviderSquare}
	service := NewService(store)

	item, err := service.CreateStaff(context.Background(), "salon_1", "owner_1", StaffWriteRequest{
		Name:  " Mai Nguyen ",
		Phone: " 312-555-0101 ",
		Email: " mai@example.com ",
	})
	if err != nil {
		t.Fatalf("CreateStaff returned error: %v", err)
	}
	if item == nil || item.Name != "Mai Nguyen" {
		t.Fatalf("created staff = %#v, want trimmed staff", item)
	}
	if store.staffCreate.input.Name != "Mai Nguyen" || store.staffCreate.input.Phone != "312-555-0101" || !store.staffCreate.input.Active {
		t.Fatalf("unexpected create input: %#v", store.staffCreate.input)
	}
	if store.staffCreate.provider != ProviderSquare {
		t.Fatalf("provider = %s, want square", store.staffCreate.provider)
	}
}

func TestCreateStaffQueuesSyncWhenProviderSupportsStaffUpsert(t *testing.T) {
	store := &fakePOSStore{activeProvider: ProviderSquare}
	service := NewService(store, fakeCapabilityProvider{name: ProviderSquare, capabilities: ProviderCapabilities{StaffUpsert: true}})

	item, err := service.CreateStaff(context.Background(), "salon_1", "owner_1", StaffWriteRequest{
		Name: "Mai Nguyen",
	})
	if err != nil {
		t.Fatalf("CreateStaff returned error: %v", err)
	}
	if item.SyncStatus != SyncStatusSyncing {
		t.Fatalf("sync status = %s, want syncing", item.SyncStatus)
	}
	if store.syncJob.Operation != SyncOperationUpsertStaff || store.syncJob.EntityType != EntityTypeStaff || store.syncJob.EntityID != "staff_new" {
		t.Fatalf("unexpected sync job: %#v", store.syncJob)
	}
}

func TestCreateStaffRejectsMissingName(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	_, err := service.CreateStaff(context.Background(), "salon_1", "owner_1", StaffWriteRequest{
		Name: " ",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("name error = %v, want ErrValidation", err)
	}
	if store.staffCreate.salonID != "" {
		t.Fatalf("store should not be called for invalid input: %#v", store.staffCreate)
	}
}

func TestUpdateStaffDelegatesNormalizedInput(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	item, err := service.UpdateStaff(context.Background(), "salon_1", "owner_1", " staff_1 ", StaffWriteRequest{
		Name:   " Linh Tran ",
		Phone:  " 3125550102 ",
		Active: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("UpdateStaff returned error: %v", err)
	}
	if item == nil || item.ID != "staff_1" {
		t.Fatalf("updated staff = %#v, want staff_1", item)
	}
	if store.staffUpdateDetails.staffID != "staff_1" || store.staffUpdateDetails.input.Active {
		t.Fatalf("unexpected update input: %#v", store.staffUpdateDetails)
	}
}

func TestArchiveStaffRejectsMissingID(t *testing.T) {
	store := &fakePOSStore{}
	service := NewService(store)

	_, err := service.ArchiveStaff(context.Background(), "salon_1", "owner_1", " ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("archive error = %v, want ErrValidation", err)
	}
	if store.staffArchive.staffID != "" {
		t.Fatalf("store should not be called for invalid archive: %#v", store.staffArchive)
	}
}

func TestServicesUsesActiveProvider(t *testing.T) {
	store := &fakePOSStore{activeProvider: "future_provider"}
	service := NewService(store)

	if _, err := service.Services(context.Background(), "salon_1", "owner_1"); err != nil {
		t.Fatalf("Services returned error: %v", err)
	}
	if store.listServicesProvider != "future_provider" {
		t.Fatalf("provider = %s, want future_provider", store.listServicesProvider)
	}
}

func TestProviderSwitchReadinessBlocksWithoutAlternateAdapter(t *testing.T) {
	now := testTime()
	store := &fakePOSStore{
		activeProvider: ProviderSquare,
		connection: &Connection{
			ID:         "conn_1",
			SalonID:    "salon_1",
			Provider:   ProviderSquare,
			Status:     StatusActive,
			LocationID: "loc_1",
			LastSyncAt: &now,
			Scopes:     []string{},
		},
		summary: ProviderMappingSummary{
			ServiceCount:         2,
			StaffCount:           2,
			CustomerCount:        1,
			BookableServiceCount: 1,
			BookableStaffCount:   1,
			LinkedServiceCount:   2,
			LinkedStaffCount:     2,
			LinkedCustomerCount:  1,
		},
	}
	service := NewService(store, fakeCapabilityProvider{name: ProviderSquare})

	readiness, err := service.ProviderSwitchReadiness(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("ProviderSwitchReadiness returned error: %v", err)
	}
	if !readiness.DryRunBookingReady {
		t.Fatalf("dry-run booking should be ready: %#v", readiness)
	}
	if readiness.CanStartSwitch {
		t.Fatalf("switch should be blocked without an alternate adapter: %#v", readiness)
	}
	if len(readiness.UnavailableProviders) != 1 || readiness.UnavailableProviders[0].Installed {
		t.Fatalf("unavailable providers = %#v, want one disabled gate", readiness.UnavailableProviders)
	}
}

func TestCreateProviderSwitchRunBlocksMissingAdapter(t *testing.T) {
	store := &fakePOSStore{activeProvider: ProviderSquare}
	service := NewService(store, fakeCapabilityProvider{name: ProviderSquare})

	run, err := service.CreateProviderSwitchRun(context.Background(), "salon_1", "owner_1", ProviderSwitchRunRequest{
		ToProvider: "future_pos",
	})
	if err != nil {
		t.Fatalf("CreateProviderSwitchRun returned error: %v", err)
	}
	if run == nil || run.Status != SwitchRunStatusBlocked || run.BlockedReason == "" {
		t.Fatalf("run = %#v, want blocked run with reason", run)
	}
	if run.CanActivate {
		t.Fatalf("blocked run must not be activatable: %#v", run)
	}
	if store.createdSwitch.FromProvider != ProviderSquare || store.createdSwitch.ToProvider != "future_pos" || store.createdSwitch.Status != SwitchRunStatusBlocked {
		t.Fatalf("unexpected stored switch mutation: %#v", store.createdSwitch)
	}
	if store.serviceCandidateCalls != 0 || store.staffCandidateCalls != 0 || store.customerCandidateCalls != 0 || len(store.replacedMatches) != 0 {
		t.Fatalf("missing adapter should not generate matches: service calls=%d staff calls=%d customer calls=%d matches=%#v", store.serviceCandidateCalls, store.staffCandidateCalls, store.customerCandidateCalls, store.replacedMatches)
	}
}

func TestCreateProviderSwitchRunGeneratesServiceStaffAndCustomerMatches(t *testing.T) {
	store := &fakePOSStore{
		activeProvider: ProviderSquare,
		serviceSourceCandidates: []ProviderSwitchEntityCandidate{{
			ID:               "service_1",
			ProviderEntityID: "sq_service_1",
			Name:             "Classic Manicure",
			DurationMinutes:  45,
		}},
		serviceProviderCandidates: []ProviderSwitchEntityCandidate{{
			ID:               "future_service_1",
			ProviderEntityID: "future_service_1",
			Name:             "classic manicure",
			DurationMinutes:  45,
		}},
		staffSourceCandidates: []ProviderSwitchEntityCandidate{{
			ID:               "staff_1",
			ProviderEntityID: "sq_staff_1",
			Name:             "Mai Nguyen",
			Phone:            "312-555-0101",
			Email:            "mai@example.com",
		}},
		staffProviderCandidates: []ProviderSwitchEntityCandidate{{
			ID:               "future_staff_1",
			ProviderEntityID: "future_staff_1",
			Name:             "Mai N.",
			Phone:            "(312) 555-0101",
		}},
		customerSourceCandidates: []ProviderSwitchEntityCandidate{{
			ID:               "customer_1",
			ProviderEntityID: "sq_customer_1",
			Name:             "Amy Tran",
			Phone:            "312-555-0102",
			Email:            "amy@example.com",
		}},
		customerProviderCandidates: []ProviderSwitchEntityCandidate{{
			ID:               "future_customer_1",
			ProviderEntityID: "future_customer_1",
			Name:             "Amy T.",
			Phone:            "3125550102",
		}},
	}
	service := NewService(
		store,
		fakeCapabilityProvider{name: ProviderSquare},
		fakeCapabilityProvider{name: "future_pos"},
	)

	run, err := service.CreateProviderSwitchRun(context.Background(), "salon_1", "owner_1", ProviderSwitchRunRequest{
		ToProvider: "future_pos",
	})
	if err != nil {
		t.Fatalf("CreateProviderSwitchRun returned error: %v", err)
	}
	if run == nil || run.Status != SwitchRunStatusNeedsReview {
		t.Fatalf("run = %#v, want needs_review run", run)
	}
	if run.CanActivate {
		t.Fatalf("switch run must not be activatable in this slice: %#v", run)
	}
	if len(store.replacedMatches) != 3 {
		t.Fatalf("replaced matches = %#v, want service, staff, and customer matches", store.replacedMatches)
	}
	if run.MatchSummary.Total != 3 || run.MatchSummary.Suggested != 3 {
		t.Fatalf("match summary = %#v, want three suggested matches", run.MatchSummary)
	}
	serviceMatch := findSwitchMatch(run.Matches, EntityTypeService)
	if serviceMatch.CanonicalEntityID != "service_1" || serviceMatch.MatchConfidence != 95 || serviceMatch.MatchStatus != SwitchMatchStatusSuggested {
		t.Fatalf("service match = %#v, want exact name+duration suggestion", serviceMatch)
	}
	staffMatch := findSwitchMatch(run.Matches, EntityTypeStaff)
	if staffMatch.CanonicalEntityID != "staff_1" || staffMatch.MatchConfidence != 90 || staffMatch.MatchStatus != SwitchMatchStatusSuggested {
		t.Fatalf("staff match = %#v, want phone suggestion", staffMatch)
	}
	customerMatch := findSwitchMatch(run.Matches, EntityTypeCustomer)
	if customerMatch.CanonicalEntityID != "customer_1" || customerMatch.MatchConfidence != 95 || customerMatch.MatchStatus != SwitchMatchStatusSuggested {
		t.Fatalf("customer match = %#v, want phone suggestion", customerMatch)
	}
}

func TestBuildCustomerSwitchMatchesUsesPhoneEmailAndNameHeuristics(t *testing.T) {
	source := []ProviderSwitchEntityCandidate{
		{ID: "customer_phone", Name: "Amy Tran", Phone: "312-555-0102"},
		{ID: "customer_email", Name: "Binh Le", Email: "binh@example.com"},
		{ID: "customer_name", Name: "Chi Nguyen"},
	}
	provider := []ProviderSwitchEntityCandidate{
		{ProviderEntityID: "target_phone", Name: "Amy T.", Phone: "3125550102"},
		{ProviderEntityID: "target_email", Name: "B. Le", Email: "BINH@example.com"},
		{ProviderEntityID: "target_name", Name: "Chi Nguyen"},
	}

	matches := buildCustomerSwitchMatches(source, provider)
	if len(matches) != 3 {
		t.Fatalf("matches = %#v, want three customer matches", matches)
	}
	if matches[0].CanonicalEntityID != "customer_phone" || matches[0].MatchConfidence != 95 || matches[0].MatchReason != "Customer phone matches." {
		t.Fatalf("phone match = %#v, want phone match with confidence 95", matches[0])
	}
	if matches[1].CanonicalEntityID != "customer_email" || matches[1].MatchConfidence != 90 || matches[1].MatchReason != "Customer email matches." {
		t.Fatalf("email match = %#v, want email match with confidence 90", matches[1])
	}
	if matches[2].CanonicalEntityID != "customer_name" || matches[2].MatchConfidence != 70 || matches[2].MatchReason != "Customer name matches." {
		t.Fatalf("name match = %#v, want name match with confidence 70", matches[2])
	}
}

func TestBuildCustomerSwitchMatchesMarksDuplicateCanonicalAsConflict(t *testing.T) {
	source := []ProviderSwitchEntityCandidate{{
		ID:    "customer_1",
		Name:  "Amy Tran",
		Phone: "3125550102",
	}}
	provider := []ProviderSwitchEntityCandidate{
		{ProviderEntityID: "target_customer_1", Name: "Amy Tran", Phone: "+1 312 555 0102"},
		{ProviderEntityID: "target_customer_2", Name: "Amy T.", Phone: "(312) 555-0102"},
	}

	matches := buildCustomerSwitchMatches(source, provider)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want two customer matches", matches)
	}
	if matches[0].MatchStatus != SwitchMatchStatusSuggested || matches[0].CanonicalEntityID != "customer_1" {
		t.Fatalf("first match = %#v, want suggested canonical match", matches[0])
	}
	if matches[1].MatchStatus != SwitchMatchStatusConflict || matches[1].CanonicalEntityID != "customer_1" || matches[1].MatchReason != "Multiple provider records match the same canonical record." {
		t.Fatalf("second match = %#v, want conflict on same canonical customer", matches[1])
	}
}

func TestUpdateProviderSwitchMatchConfirmsSuggestionAndMarksRunReady(t *testing.T) {
	now := testTime()
	store := &fakePOSStore{
		switchRun: &ProviderSwitchRun{
			ID:           "run_1",
			SalonID:      "salon_1",
			FromProvider: ProviderSquare,
			ToProvider:   "future_pos",
			Status:       SwitchRunStatusNeedsReview,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		switchMatches: []ProviderSwitchMatch{{
			ID:                "match_1",
			RunID:             "run_1",
			SalonID:           "salon_1",
			EntityType:        EntityTypeService,
			CanonicalEntityID: "service_1",
			CanonicalName:     "Classic Manicure",
			ProviderEntityID:  "future_service_1",
			ProviderName:      "Classic Manicure",
			MatchStatus:       SwitchMatchStatusSuggested,
			MatchConfidence:   95,
			CreatedAt:         now,
			UpdatedAt:         now,
		}},
	}
	service := NewService(store)

	run, err := service.UpdateProviderSwitchMatch(context.Background(), "salon_1", "owner_1", "run_1", "match_1", ProviderSwitchMatchUpdateRequest{
		MatchStatus: SwitchMatchStatusConfirmed,
	})
	if err != nil {
		t.Fatalf("UpdateProviderSwitchMatch returned error: %v", err)
	}
	if store.updatedMatch.MatchStatus != SwitchMatchStatusConfirmed || store.updatedMatch.MatchConfidence != 100 {
		t.Fatalf("updated match mutation = %#v, want confirmed confidence 100", store.updatedMatch)
	}
	if store.updatedRunStatus != SwitchRunStatusReady {
		t.Fatalf("updated run status = %s, want ready", store.updatedRunStatus)
	}
	if run == nil || run.Status != SwitchRunStatusReady || run.MatchSummary.Confirmed != 1 || run.CanActivate {
		t.Fatalf("run = %#v, want ready review state with can_activate=false", run)
	}
}

func TestUpdateProviderSwitchMatchMarksUnmatchedAndNeedsReview(t *testing.T) {
	now := testTime()
	store := &fakePOSStore{
		switchRun: &ProviderSwitchRun{
			ID:           "run_1",
			SalonID:      "salon_1",
			FromProvider: ProviderSquare,
			ToProvider:   "future_pos",
			Status:       SwitchRunStatusNeedsReview,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		switchMatches: []ProviderSwitchMatch{{
			ID:                "match_1",
			RunID:             "run_1",
			SalonID:           "salon_1",
			EntityType:        EntityTypeStaff,
			CanonicalEntityID: "staff_1",
			CanonicalName:     "Mai Nguyen",
			ProviderEntityID:  "future_staff_1",
			ProviderName:      "Mai N.",
			MatchStatus:       SwitchMatchStatusSuggested,
			MatchConfidence:   90,
			CreatedAt:         now,
			UpdatedAt:         now,
		}},
	}
	service := NewService(store)

	run, err := service.UpdateProviderSwitchMatch(context.Background(), "salon_1", "owner_1", "run_1", "match_1", ProviderSwitchMatchUpdateRequest{
		MatchStatus: SwitchMatchStatusUnmatched,
	})
	if err != nil {
		t.Fatalf("UpdateProviderSwitchMatch returned error: %v", err)
	}
	if store.updatedMatch.CanonicalEntityID != "" || store.updatedMatch.MatchStatus != SwitchMatchStatusUnmatched || store.updatedMatch.MatchConfidence != 0 {
		t.Fatalf("updated match mutation = %#v, want unmatched with cleared canonical", store.updatedMatch)
	}
	if store.updatedRunStatus != "" {
		t.Fatalf("run status should remain needs_review without update, got %s", store.updatedRunStatus)
	}
	if run == nil || run.Status != SwitchRunStatusNeedsReview || run.MatchSummary.Unmatched != 1 {
		t.Fatalf("run = %#v, want needs_review unmatched summary", run)
	}
}

func TestUpdateProviderSwitchMatchRejectsBlockedRun(t *testing.T) {
	now := testTime()
	store := &fakePOSStore{
		switchRun: &ProviderSwitchRun{
			ID:           "run_1",
			SalonID:      "salon_1",
			FromProvider: ProviderSquare,
			ToProvider:   "future_pos",
			Status:       SwitchRunStatusBlocked,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		switchMatches: []ProviderSwitchMatch{{
			ID:               "match_1",
			RunID:            "run_1",
			SalonID:          "salon_1",
			EntityType:       EntityTypeService,
			ProviderEntityID: "future_service_1",
			ProviderName:     "Classic Manicure",
			MatchStatus:      SwitchMatchStatusSuggested,
			CreatedAt:        now,
			UpdatedAt:        now,
		}},
	}
	service := NewService(store)

	_, err := service.UpdateProviderSwitchMatch(context.Background(), "salon_1", "owner_1", "run_1", "match_1", ProviderSwitchMatchUpdateRequest{
		MatchStatus: SwitchMatchStatusConfirmed,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if store.updatedMatch.MatchID != "" {
		t.Fatalf("blocked run should not update match: %#v", store.updatedMatch)
	}
}

type fakePOSStore struct {
	activeProvider       string
	listServicesProvider string
	listStaffProvider    string
	connection           *Connection
	summary              ProviderMappingSummary
	serviceCreate        struct {
		salonID     string
		ownerUserID string
		provider    string
		input       ServiceMutation
	}
	serviceUpdateDetails struct {
		salonID     string
		ownerUserID string
		serviceID   string
		input       ServiceMutation
	}
	serviceArchive struct {
		salonID     string
		ownerUserID string
		serviceID   string
	}
	serviceUpdate struct {
		salonID     string
		ownerUserID string
		serviceID   string
		aiBookable  bool
	}
	staffCreate struct {
		salonID     string
		ownerUserID string
		provider    string
		input       StaffMutation
	}
	staffUpdateDetails struct {
		salonID     string
		ownerUserID string
		staffID     string
		input       StaffMutation
	}
	staffArchive struct {
		salonID     string
		ownerUserID string
		staffID     string
	}
	staffUpdate struct {
		salonID     string
		ownerUserID string
		staffID     string
		aiBookable  bool
	}
	syncJob                    SyncJobMutation
	createdSwitch              ProviderSwitchRunMutation
	switchRun                  *ProviderSwitchRun
	switchMatches              []ProviderSwitchMatch
	serviceSourceCandidates    []ProviderSwitchEntityCandidate
	serviceProviderCandidates  []ProviderSwitchEntityCandidate
	staffSourceCandidates      []ProviderSwitchEntityCandidate
	staffProviderCandidates    []ProviderSwitchEntityCandidate
	customerSourceCandidates   []ProviderSwitchEntityCandidate
	customerProviderCandidates []ProviderSwitchEntityCandidate
	replacedMatches            []ProviderSwitchMatchMutation
	updatedMatch               ProviderSwitchMatchUpdateMutation
	updatedRunStatus           string
	serviceCandidateCalls      int
	staffCandidateCalls        int
	customerCandidateCalls     int
}

func (f *fakePOSStore) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	return nil
}

func (f *fakePOSStore) GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	if f.activeProvider == "" {
		return ProviderSquare, nil
	}
	return f.activeProvider, nil
}

func (f *fakePOSStore) GetConnection(ctx context.Context, salonID string, provider string) (*Connection, error) {
	if f.connection == nil {
		return nil, ErrNotFound
	}
	return f.connection, nil
}

func (f *fakePOSStore) ListServices(ctx context.Context, salonID string, provider string) ([]Service, error) {
	f.listServicesProvider = provider
	return nil, nil
}

func (f *fakePOSStore) ListStaff(ctx context.Context, salonID string, provider string) ([]StaffMember, error) {
	f.listStaffProvider = provider
	return nil, nil
}

func (f *fakePOSStore) CreateService(ctx context.Context, salonID string, ownerUserID string, provider string, input ServiceMutation) (*Service, error) {
	f.serviceCreate.salonID = salonID
	f.serviceCreate.ownerUserID = ownerUserID
	f.serviceCreate.provider = provider
	f.serviceCreate.input = input
	return &Service{ID: "service_new", SalonID: salonID, POSProvider: provider, Name: input.Name, DurationMinutes: input.DurationMinutes, Active: input.Active, SyncStatus: SyncStatusLocalOnly, Source: EntitySourceLocal}, nil
}

func (f *fakePOSStore) UpdateService(ctx context.Context, salonID string, ownerUserID string, serviceID string, input ServiceMutation) (*Service, error) {
	f.serviceUpdateDetails.salonID = salonID
	f.serviceUpdateDetails.ownerUserID = ownerUserID
	f.serviceUpdateDetails.serviceID = serviceID
	f.serviceUpdateDetails.input = input
	return &Service{ID: serviceID, SalonID: salonID, Name: input.Name, DurationMinutes: input.DurationMinutes, Active: input.Active}, nil
}

func (f *fakePOSStore) ArchiveService(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
	f.serviceArchive.salonID = salonID
	f.serviceArchive.ownerUserID = ownerUserID
	f.serviceArchive.serviceID = serviceID
	return &Service{ID: serviceID, SalonID: salonID, Active: false, SyncStatus: SyncStatusArchived}, nil
}

func (f *fakePOSStore) CreateStaff(ctx context.Context, salonID string, ownerUserID string, provider string, input StaffMutation) (*StaffMember, error) {
	f.staffCreate.salonID = salonID
	f.staffCreate.ownerUserID = ownerUserID
	f.staffCreate.provider = provider
	f.staffCreate.input = input
	return &StaffMember{ID: "staff_new", SalonID: salonID, POSProvider: provider, Name: input.Name, Phone: input.Phone, Email: input.Email, Active: input.Active, SyncStatus: SyncStatusLocalOnly, Source: EntitySourceLocal}, nil
}

func (f *fakePOSStore) UpdateStaff(ctx context.Context, salonID string, ownerUserID string, staffID string, input StaffMutation) (*StaffMember, error) {
	f.staffUpdateDetails.salonID = salonID
	f.staffUpdateDetails.ownerUserID = ownerUserID
	f.staffUpdateDetails.staffID = staffID
	f.staffUpdateDetails.input = input
	return &StaffMember{ID: staffID, SalonID: salonID, Name: input.Name, Phone: input.Phone, Email: input.Email, Active: input.Active}, nil
}

func (f *fakePOSStore) ArchiveStaff(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error) {
	f.staffArchive.salonID = salonID
	f.staffArchive.ownerUserID = ownerUserID
	f.staffArchive.staffID = staffID
	return &StaffMember{ID: staffID, SalonID: salonID, Active: false, SyncStatus: SyncStatusArchived}, nil
}

func (f *fakePOSStore) EnqueuePOSSyncJob(ctx context.Context, input SyncJobMutation) (*SyncJob, error) {
	f.syncJob = input
	return &SyncJob{
		ID:         "sync_job_1",
		SalonID:    input.SalonID,
		Provider:   input.Provider,
		EntityType: input.EntityType,
		EntityID:   input.EntityID,
		Operation:  input.Operation,
		Status:     SyncJobStatusQueued,
	}, nil
}

func (f *fakePOSStore) UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error) {
	f.serviceUpdate.salonID = salonID
	f.serviceUpdate.ownerUserID = ownerUserID
	f.serviceUpdate.serviceID = serviceID
	f.serviceUpdate.aiBookable = aiBookable
	return &Service{ID: serviceID, SalonID: salonID, AIBookable: aiBookable, Active: true}, nil
}

func (f *fakePOSStore) UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error) {
	f.staffUpdate.salonID = salonID
	f.staffUpdate.ownerUserID = ownerUserID
	f.staffUpdate.staffID = staffID
	f.staffUpdate.aiBookable = aiBookable
	return &StaffMember{ID: staffID, SalonID: salonID, AIBookable: aiBookable, Active: true}, nil
}

func (f *fakePOSStore) ProviderMappingSummary(ctx context.Context, salonID string, ownerUserID string, provider string) (*ProviderMappingSummary, error) {
	return &f.summary, nil
}

func (f *fakePOSStore) CreateProviderSwitchRun(ctx context.Context, input ProviderSwitchRunMutation) (*ProviderSwitchRun, error) {
	f.createdSwitch = input
	if f.switchRun != nil {
		return f.switchRun, nil
	}
	now := testTime()
	f.switchRun = &ProviderSwitchRun{
		ID:              "switch_run_1",
		SalonID:         input.SalonID,
		FromProvider:    input.FromProvider,
		ToProvider:      input.ToProvider,
		Status:          input.Status,
		BlockedReason:   input.BlockedReason,
		DryRunReady:     input.DryRunReady,
		CreatedByUserID: input.CreatedByUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return f.switchRun, nil
}

func (f *fakePOSStore) GetProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string, runID string) (*ProviderSwitchRun, error) {
	if f.switchRun == nil {
		return nil, ErrNotFound
	}
	return f.switchRun, nil
}

func (f *fakePOSStore) LatestProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string) (*ProviderSwitchRun, error) {
	return f.switchRun, nil
}

func (f *fakePOSStore) ListProviderSwitchMatches(ctx context.Context, salonID string, ownerUserID string, runID string) ([]ProviderSwitchMatch, error) {
	return f.switchMatches, nil
}

func (f *fakePOSStore) UpdateProviderSwitchMatch(ctx context.Context, input ProviderSwitchMatchUpdateMutation) (*ProviderSwitchMatch, error) {
	f.updatedMatch = input
	for index, match := range f.switchMatches {
		if match.ID != input.MatchID {
			continue
		}
		f.switchMatches[index].CanonicalEntityID = input.CanonicalEntityID
		f.switchMatches[index].CanonicalName = input.CanonicalName
		f.switchMatches[index].MatchStatus = input.MatchStatus
		f.switchMatches[index].MatchConfidence = input.MatchConfidence
		f.switchMatches[index].MatchReason = input.MatchReason
		return &f.switchMatches[index], nil
	}
	return nil, ErrNotFound
}

func (f *fakePOSStore) UpdateProviderSwitchRunStatus(ctx context.Context, salonID string, ownerUserID string, runID string, status string, blockedReason string) (*ProviderSwitchRun, error) {
	f.updatedRunStatus = status
	if f.switchRun == nil {
		return nil, ErrNotFound
	}
	f.switchRun.Status = status
	f.switchRun.BlockedReason = blockedReason
	return f.switchRun, nil
}

func (f *fakePOSStore) ListProviderSwitchServiceCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error) {
	f.serviceCandidateCalls++
	return f.serviceSourceCandidates, f.serviceProviderCandidates, nil
}

func (f *fakePOSStore) ListProviderSwitchStaffCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error) {
	f.staffCandidateCalls++
	return f.staffSourceCandidates, f.staffProviderCandidates, nil
}

func (f *fakePOSStore) ListProviderSwitchCustomerCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error) {
	f.customerCandidateCalls++
	return f.customerSourceCandidates, f.customerProviderCandidates, nil
}

func (f *fakePOSStore) ReplaceProviderSwitchMatches(ctx context.Context, salonID string, runID string, matches []ProviderSwitchMatchMutation) ([]ProviderSwitchMatch, error) {
	f.replacedMatches = matches
	now := testTime()
	f.switchMatches = make([]ProviderSwitchMatch, 0, len(matches))
	for _, match := range matches {
		f.switchMatches = append(f.switchMatches, ProviderSwitchMatch{
			ID:                      "match_" + match.EntityType + "_" + match.ProviderEntityID,
			RunID:                   runID,
			SalonID:                 salonID,
			EntityType:              match.EntityType,
			CanonicalEntityID:       match.CanonicalEntityID,
			CanonicalName:           match.CanonicalName,
			ProviderEntityID:        match.ProviderEntityID,
			ProviderName:            match.ProviderName,
			ProviderPhone:           match.ProviderPhone,
			ProviderEmail:           match.ProviderEmail,
			ProviderDurationMinutes: match.ProviderDurationMinutes,
			MatchStatus:             match.MatchStatus,
			MatchConfidence:         match.MatchConfidence,
			MatchReason:             match.MatchReason,
			CreatedAt:               now,
			UpdatedAt:               now,
		})
	}
	return f.switchMatches, nil
}

func findSwitchMatch(matches []ProviderSwitchMatch, entityType string) ProviderSwitchMatch {
	for _, match := range matches {
		if match.EntityType == entityType {
			return match
		}
	}
	return ProviderSwitchMatch{}
}

func boolPtr(value bool) *bool {
	return &value
}

func floatPtr(value float64) *float64 {
	return &value
}

func testTime() time.Time {
	return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
}

type fakeCapabilityProvider struct {
	name         string
	capabilities ProviderCapabilities
}

func (f fakeCapabilityProvider) Name() string {
	return f.name
}

func (f fakeCapabilityProvider) Capabilities() ProviderCapabilities {
	return f.capabilities
}
