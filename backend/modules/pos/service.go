package pos

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"

	"github.com/manleai/ai-receptionist/internal/validation"
)

const switchAdapterMissingReason = "The requested POS provider adapter is not installed in this deployment."

var (
	ErrValidation            = errors.New("pos validation failed")
	ErrProviderManagedFields = errors.New("operational fields are managed by the active pos provider")
)

type Store interface {
	EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error
	GetActiveProvider(ctx context.Context, salonID string, ownerUserID string) (string, error)
	GetConnection(ctx context.Context, salonID string, provider string) (*Connection, error)
	ListServices(ctx context.Context, salonID string, provider string) ([]Service, error)
	GetServiceForOwner(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error)
	ListServiceCategories(ctx context.Context, salonID string, ownerUserID string) ([]ServiceCategory, error)
	CreateServiceCategory(ctx context.Context, salonID string, ownerUserID string, input ServiceCategoryMutation) (*ServiceCategory, error)
	UpdateServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string, input ServiceCategoryMutation) (*ServiceCategory, error)
	ArchiveServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) (*ServiceCategory, error)
	RestoreServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) (*ServiceCategory, error)
	UpsertServiceCategoryAlias(ctx context.Context, salonID string, ownerUserID string, input ServiceCategoryAliasMutation) (*ServiceCategoryAlias, error)
	ArchiveServiceCategoryAlias(ctx context.Context, salonID string, ownerUserID string, aliasID string) (*ServiceCategoryAlias, error)
	AssignServiceCategory(ctx context.Context, salonID string, ownerUserID string, serviceID string, categoryID string) (*Service, error)
	RefreshServiceCategorySuggestions(ctx context.Context, salonID string, ownerUserID string) (*ServiceCategorySuggestionRefresh, error)
	ListStaff(ctx context.Context, salonID string, provider string) ([]StaffMember, error)
	CreateService(ctx context.Context, salonID string, ownerUserID string, provider string, input ServiceMutation) (*Service, error)
	UpdateService(ctx context.Context, salonID string, ownerUserID string, serviceID string, input ServiceMutation) (*Service, error)
	UpdateServiceOwnerControls(ctx context.Context, salonID string, ownerUserID string, serviceID string, input ServiceOwnerControlsMutation) (*Service, error)
	ArchiveService(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error)
	GetStaffForOwner(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error)
	CreateStaff(ctx context.Context, salonID string, ownerUserID string, provider string, input StaffMutation) (*StaffMember, error)
	UpdateStaff(ctx context.Context, salonID string, ownerUserID string, staffID string, input StaffMutation) (*StaffMember, error)
	ArchiveStaff(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error)
	EnqueuePOSSyncJob(ctx context.Context, input SyncJobMutation) (*SyncJob, error)
	UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error)
	UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error)
	ProviderMappingSummary(ctx context.Context, salonID string, ownerUserID string, provider string) (*ProviderMappingSummary, error)
	CreateProviderSwitchRun(ctx context.Context, input ProviderSwitchRunMutation) (*ProviderSwitchRun, error)
	GetProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string, runID string) (*ProviderSwitchRun, error)
	LatestProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string) (*ProviderSwitchRun, error)
	ListProviderSwitchMatches(ctx context.Context, salonID string, ownerUserID string, runID string) ([]ProviderSwitchMatch, error)
	UpdateProviderSwitchMatch(ctx context.Context, input ProviderSwitchMatchUpdateMutation) (*ProviderSwitchMatch, error)
	UpdateProviderSwitchRunStatus(ctx context.Context, salonID string, ownerUserID string, runID string, status string, blockedReason string) (*ProviderSwitchRun, error)
	ListProviderSwitchServiceCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error)
	ListProviderSwitchStaffCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error)
	ListProviderSwitchCustomerCandidates(ctx context.Context, salonID string, fromProvider string, toProvider string) ([]ProviderSwitchEntityCandidate, []ProviderSwitchEntityCandidate, error)
	ReplaceProviderSwitchMatches(ctx context.Context, salonID string, runID string, matches []ProviderSwitchMatchMutation) ([]ProviderSwitchMatch, error)
}

type ServiceLayer struct {
	repo      Store
	providers map[string]NamedProvider
}

func NewService(repo Store, providers ...NamedProvider) *ServiceLayer {
	byName := make(map[string]NamedProvider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		byName[provider.Name()] = provider
	}
	return &ServiceLayer{repo: repo, providers: byName}
}

func (s *ServiceLayer) Services(ctx context.Context, salonID string, ownerUserID string) ([]Service, error) {
	provider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListServices(ctx, salonID, provider)
	if err != nil {
		return nil, err
	}
	for i := range items {
		s.decorateService(&items[i])
	}
	return items, nil
}

func (s *ServiceLayer) ServiceCategories(ctx context.Context, salonID string, ownerUserID string) ([]ServiceCategory, error) {
	return s.repo.ListServiceCategories(ctx, salonID, ownerUserID)
}

func (s *ServiceLayer) CreateServiceCategory(ctx context.Context, salonID string, ownerUserID string, req ServiceCategoryWriteRequest) (*ServiceCategory, error) {
	input, err := normalizeServiceCategoryWriteRequest(req)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateServiceCategory(ctx, salonID, ownerUserID, input)
}

func (s *ServiceLayer) UpdateServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string, req ServiceCategoryWriteRequest) (*ServiceCategory, error) {
	categoryID = strings.TrimSpace(categoryID)
	if categoryID == "" {
		return nil, ErrValidation
	}
	input, err := normalizeServiceCategoryWriteRequest(req)
	if err != nil {
		return nil, err
	}
	return s.repo.UpdateServiceCategory(ctx, salonID, ownerUserID, categoryID, input)
}

func (s *ServiceLayer) ArchiveServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) (*ServiceCategory, error) {
	categoryID = strings.TrimSpace(categoryID)
	if categoryID == "" {
		return nil, ErrValidation
	}
	return s.repo.ArchiveServiceCategory(ctx, salonID, ownerUserID, categoryID)
}

func (s *ServiceLayer) RestoreServiceCategory(ctx context.Context, salonID string, ownerUserID string, categoryID string) (*ServiceCategory, error) {
	categoryID = strings.TrimSpace(categoryID)
	if categoryID == "" {
		return nil, ErrValidation
	}
	return s.repo.RestoreServiceCategory(ctx, salonID, ownerUserID, categoryID)
}

func (s *ServiceLayer) UpsertServiceCategoryAlias(ctx context.Context, salonID string, ownerUserID string, categoryID string, req ServiceCategoryAliasWriteRequest) (*ServiceCategoryAlias, error) {
	categoryID = strings.TrimSpace(categoryID)
	if categoryID == "" {
		return nil, ErrValidation
	}
	input, err := normalizeServiceCategoryAliasWriteRequest(categoryID, req)
	if err != nil {
		return nil, err
	}
	return s.repo.UpsertServiceCategoryAlias(ctx, salonID, ownerUserID, input)
}

func (s *ServiceLayer) ArchiveServiceCategoryAlias(ctx context.Context, salonID string, ownerUserID string, aliasID string) (*ServiceCategoryAlias, error) {
	aliasID = strings.TrimSpace(aliasID)
	if aliasID == "" {
		return nil, ErrValidation
	}
	return s.repo.ArchiveServiceCategoryAlias(ctx, salonID, ownerUserID, aliasID)
}

func (s *ServiceLayer) AssignServiceCategory(ctx context.Context, salonID string, ownerUserID string, serviceID string, req ServiceCategoryAssignRequest) (*Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrValidation
	}
	item, err := s.repo.AssignServiceCategory(ctx, salonID, ownerUserID, serviceID, strings.TrimSpace(req.ServiceCategoryID))
	if err != nil {
		return nil, err
	}
	return s.decorateService(item), nil
}

func (s *ServiceLayer) RefreshServiceCategorySuggestions(ctx context.Context, salonID string, ownerUserID string) (*ServiceCategorySuggestionRefresh, error) {
	return s.repo.RefreshServiceCategorySuggestions(ctx, salonID, ownerUserID)
}

func (s *ServiceLayer) Staff(ctx context.Context, salonID string, ownerUserID string) ([]StaffMember, error) {
	provider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListStaff(ctx, salonID, provider)
	if err != nil {
		return nil, err
	}
	for i := range items {
		s.decorateStaff(&items[i])
	}
	return items, nil
}

func (s *ServiceLayer) CreateService(ctx context.Context, salonID string, ownerUserID string, req ServiceWriteRequest) (*Service, error) {
	input, err := normalizeServiceWriteRequest(req, true)
	if err != nil {
		return nil, err
	}
	provider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.CreateService(ctx, salonID, ownerUserID, provider, input)
	if err != nil {
		return nil, err
	}
	if queued, err := s.enqueueServiceSync(ctx, item, SyncOperationUpsertService); err != nil {
		return nil, err
	} else if queued {
		item.SyncStatus = SyncStatusSyncing
		item.SyncError = ""
	}
	return s.decorateService(item), nil
}

func (s *ServiceLayer) UpdateService(ctx context.Context, salonID string, ownerUserID string, serviceID string, req ServiceWriteRequest) (*Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrValidation
	}
	current, err := s.repo.GetServiceForOwner(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if s.serviceFieldAuthority(current).OperationalWriteMode == OperationalWriteModeProviderReadOnly {
		return nil, ErrProviderManagedFields
	}
	input, err := normalizeServiceWriteRequest(req, false)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.UpdateService(ctx, salonID, ownerUserID, serviceID, input)
	if err != nil {
		return nil, err
	}
	if queued, err := s.enqueueServiceSync(ctx, item, SyncOperationUpsertService); err != nil {
		return nil, err
	} else if queued {
		item.SyncStatus = SyncStatusSyncing
		item.SyncError = ""
	}
	return s.decorateService(item), nil
}

func (s *ServiceLayer) UpdateServiceOwnerControls(ctx context.Context, salonID string, ownerUserID string, serviceID string, req ServiceOwnerControlsWriteRequest) (*Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrValidation
	}
	input, err := normalizeServiceOwnerControlsWriteRequest(req)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.UpdateServiceOwnerControls(ctx, salonID, ownerUserID, serviceID, input)
	if err != nil {
		return nil, err
	}
	return s.decorateService(item), nil
}

func (s *ServiceLayer) ArchiveService(ctx context.Context, salonID string, ownerUserID string, serviceID string) (*Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrValidation
	}
	item, err := s.repo.ArchiveService(ctx, salonID, ownerUserID, serviceID)
	if err != nil {
		return nil, err
	}
	if queued, err := s.enqueueServiceSync(ctx, item, SyncOperationArchiveService); err != nil {
		return nil, err
	} else if queued {
		item.SyncStatus = SyncStatusSyncing
		item.SyncError = ""
	}
	return s.decorateService(item), nil
}

func (s *ServiceLayer) CreateStaff(ctx context.Context, salonID string, ownerUserID string, req StaffWriteRequest) (*StaffMember, error) {
	input, err := normalizeStaffWriteRequest(req, true)
	if err != nil {
		return nil, err
	}
	provider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.CreateStaff(ctx, salonID, ownerUserID, provider, input)
	if err != nil {
		return nil, err
	}
	if queued, err := s.enqueueStaffSync(ctx, item, SyncOperationUpsertStaff); err != nil {
		return nil, err
	} else if queued {
		item.SyncStatus = SyncStatusSyncing
		item.SyncError = ""
	}
	return s.decorateStaff(item), nil
}

func (s *ServiceLayer) UpdateStaff(ctx context.Context, salonID string, ownerUserID string, staffID string, req StaffWriteRequest) (*StaffMember, error) {
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return nil, ErrValidation
	}
	current, err := s.repo.GetStaffForOwner(ctx, salonID, ownerUserID, staffID)
	if err != nil {
		return nil, err
	}
	if s.staffFieldAuthority(current).OperationalWriteMode == OperationalWriteModeProviderReadOnly {
		return nil, ErrProviderManagedFields
	}
	input, err := normalizeStaffWriteRequest(req, false)
	if err != nil {
		return nil, err
	}
	item, err := s.repo.UpdateStaff(ctx, salonID, ownerUserID, staffID, input)
	if err != nil {
		return nil, err
	}
	if queued, err := s.enqueueStaffSync(ctx, item, SyncOperationUpsertStaff); err != nil {
		return nil, err
	} else if queued {
		item.SyncStatus = SyncStatusSyncing
		item.SyncError = ""
	}
	return s.decorateStaff(item), nil
}

func (s *ServiceLayer) ArchiveStaff(ctx context.Context, salonID string, ownerUserID string, staffID string) (*StaffMember, error) {
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return nil, ErrValidation
	}
	item, err := s.repo.ArchiveStaff(ctx, salonID, ownerUserID, staffID)
	if err != nil {
		return nil, err
	}
	if queued, err := s.enqueueStaffSync(ctx, item, SyncOperationArchiveStaff); err != nil {
		return nil, err
	} else if queued {
		item.SyncStatus = SyncStatusSyncing
		item.SyncError = ""
	}
	return s.decorateStaff(item), nil
}

func (s *ServiceLayer) UpdateServiceAIBookable(ctx context.Context, salonID string, ownerUserID string, serviceID string, aiBookable bool) (*Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrValidation
	}
	item, err := s.repo.UpdateServiceAIBookable(ctx, salonID, ownerUserID, serviceID, aiBookable)
	if err != nil {
		return nil, err
	}
	return s.decorateService(item), nil
}

func (s *ServiceLayer) UpdateStaffAIBookable(ctx context.Context, salonID string, ownerUserID string, staffID string, aiBookable bool) (*StaffMember, error) {
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return nil, ErrValidation
	}
	item, err := s.repo.UpdateStaffAIBookable(ctx, salonID, ownerUserID, staffID, aiBookable)
	if err != nil {
		return nil, err
	}
	return s.decorateStaff(item), nil
}

func (s *ServiceLayer) ProviderSwitchReadiness(ctx context.Context, salonID string, ownerUserID string) (*ProviderSwitchReadiness, error) {
	activeProvider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.ProviderMappingSummary(ctx, salonID, ownerUserID, activeProvider)
	if err != nil {
		return nil, err
	}
	connection, err := s.repo.GetConnection(ctx, salonID, activeProvider)
	if errors.Is(err, ErrNotFound) {
		connection = &Connection{SalonID: salonID, Provider: activeProvider, Status: StatusNotConnected, Scopes: []string{}}
	} else if err != nil {
		return nil, err
	}

	installed := s.installedProviderOptions(activeProvider, connection)
	alternateInstalled := len(installed) > 1
	connected := connectionReady(connection)
	synced := connectionSynced(connection)
	dryRunReady := connected && synced && summary.BookableServiceCount > 0 && summary.BookableStaffCount > 0
	checks := []ProviderReadinessCheck{
		{Key: "active_provider", Label: "Active provider selected", Complete: activeProvider != "", Message: incompleteProviderMessage(activeProvider != "", "No active POS provider is selected.")},
		{Key: "provider_adapter", Label: "Active provider adapter installed", Complete: s.providers[activeProvider] != nil, Message: incompleteProviderMessage(s.providers[activeProvider] != nil, "The active POS provider adapter is not configured in this deployment.")},
		{Key: "provider_connected", Label: "Active provider connected", Complete: connected, Message: incompleteProviderMessage(connected, "Connect the active provider and select a booking location.")},
		{Key: "provider_synced", Label: "Active provider synced", Complete: synced, Message: incompleteProviderMessage(synced, "Sync records from the active provider.")},
		{Key: "services_mapped", Label: "Bookable services mapped", Complete: summary.BookableServiceCount > 0, Message: incompleteProviderMessage(summary.BookableServiceCount > 0, "At least one active AI-bookable service must have a synced provider link.")},
		{Key: "staff_mapped", Label: "Bookable staff mapped", Complete: summary.BookableStaffCount > 0, Message: incompleteProviderMessage(summary.BookableStaffCount > 0, "At least one active AI-bookable staff member must have a synced provider link.")},
		{Key: "alternate_adapter", Label: "Alternate provider adapter installed", Complete: alternateInstalled, Message: incompleteProviderMessage(alternateInstalled, "No alternate production POS adapter is installed in this deployment.")},
	}
	result := &ProviderSwitchReadiness{
		SalonID:              salonID,
		ActiveProvider:       activeProvider,
		ActiveProviderLabel:  providerLabel(activeProvider),
		InstalledProviders:   installed,
		UnavailableProviders: unavailableProviderOptions(alternateInstalled),
		Mapping:              *summary,
		Checks:               checks,
		DryRunBookingReady:   dryRunReady,
		CanStartSwitch:       alternateInstalled,
		CanActivateProvider:  false,
	}
	for _, check := range checks {
		if !check.Complete && result.BlockedReason == "" {
			result.BlockedReason = check.Message
		}
	}
	if !alternateInstalled && result.BlockedReason == "" {
		result.BlockedReason = "No alternate production POS adapter is installed in this deployment."
	}
	return result, nil
}

func (s *ServiceLayer) CreateProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string, req ProviderSwitchRunRequest) (*ProviderSwitchRun, error) {
	toProvider := strings.TrimSpace(req.ToProvider)
	if toProvider == "" {
		return nil, ErrValidation
	}
	fromProvider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if fromProvider == "" || strings.EqualFold(fromProvider, toProvider) {
		return nil, ErrValidation
	}

	status := SwitchRunStatusDraft
	blockedReason := ""
	if s.providers[toProvider] == nil {
		status = SwitchRunStatusBlocked
		blockedReason = switchAdapterMissingReason
	}

	run, err := s.repo.CreateProviderSwitchRun(ctx, ProviderSwitchRunMutation{
		SalonID:         salonID,
		OwnerUserID:     ownerUserID,
		FromProvider:    fromProvider,
		ToProvider:      toProvider,
		Status:          status,
		BlockedReason:   blockedReason,
		DryRunReady:     false,
		CreatedByUserID: ownerUserID,
	})
	if err != nil {
		return nil, err
	}
	if status == SwitchRunStatusBlocked {
		return s.withSwitchMatches(ctx, ownerUserID, run)
	}
	if err := s.generateProviderSwitchMatches(ctx, run); err != nil {
		return nil, err
	}
	return s.refreshSwitchRunReviewStatus(ctx, salonID, ownerUserID, run.ID)
}

func (s *ServiceLayer) LatestProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string) (*ProviderSwitchRun, error) {
	run, err := s.repo.LatestProviderSwitchRun(ctx, salonID, ownerUserID)
	if err != nil || run == nil {
		return run, err
	}
	return s.withSwitchMatches(ctx, ownerUserID, run)
}

func (s *ServiceLayer) GetProviderSwitchRun(ctx context.Context, salonID string, ownerUserID string, runID string) (*ProviderSwitchRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrValidation
	}
	run, err := s.repo.GetProviderSwitchRun(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	return s.withSwitchMatches(ctx, ownerUserID, run)
}

func (s *ServiceLayer) ProviderSwitchDryRunReadiness(ctx context.Context, salonID string, ownerUserID string, runID string) (*ProviderSwitchDryRunReadiness, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, ErrValidation
	}
	run, err := s.repo.GetProviderSwitchRun(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	matches, err := s.repo.ListProviderSwitchMatches(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	matchSummary := summarizeSwitchMatches(matches)

	activeProvider, err := s.activeProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	connection, err := s.repo.GetConnection(ctx, salonID, run.FromProvider)
	if errors.Is(err, ErrNotFound) {
		connection = &Connection{SalonID: salonID, Provider: run.FromProvider, Status: StatusNotConnected, Scopes: []string{}}
	} else if err != nil {
		return nil, err
	}
	mapping, err := s.repo.ProviderMappingSummary(ctx, salonID, ownerUserID, run.FromProvider)
	if err != nil {
		return nil, err
	}

	targetAdapterInstalled := s.providers[run.ToProvider] != nil
	runReviewable := switchRunDryRunReviewable(run.Status)
	importedRecordsExist := matchSummary.Total > 0
	matchesResolved := importedRecordsExist && matchSummary.Suggested == 0 && matchSummary.Unmatched == 0 && matchSummary.Conflicts == 0
	currentConnected := connectionReady(connection)
	currentSynced := connectionSynced(connection)
	currentBookingReady := activeProvider == run.FromProvider && currentSynced && mapping.BookableServiceCount > 0 && mapping.BookableStaffCount > 0
	dryRunExecutionAvailable := false

	checks := []ProviderReadinessCheck{
		{Key: "target_adapter", Label: "Target provider adapter installed", Complete: targetAdapterInstalled, Message: incompleteProviderMessage(targetAdapterInstalled, "The target POS provider adapter is not installed in this deployment.")},
		{Key: "switch_run_reviewable", Label: "Switch run is reviewable", Complete: runReviewable, Message: incompleteProviderMessage(runReviewable, "This switch run is blocked, activated, cancelled, or failed.")},
		{Key: "imported_records", Label: "Imported provider records exist", Complete: importedRecordsExist, Message: incompleteProviderMessage(importedRecordsExist, "Import records from a real alternate POS provider before dry-run checks can pass.")},
		{Key: "matches_resolved", Label: "Match conflicts resolved", Complete: matchesResolved, Message: incompleteProviderMessage(matchesResolved, "Resolve suggested, unmatched, or conflicting provider matches before dry-run.")},
		{Key: "current_provider_booking_ready", Label: "Current provider booking readiness passed", Complete: currentBookingReady, Message: dryRunCurrentProviderMessage(activeProvider, run.FromProvider, currentConnected, currentSynced, mapping)},
		{Key: "dry_run_execution_available", Label: "Alternate-provider dry-run execution available", Complete: dryRunExecutionAvailable, Message: "Alternate-provider dry-run execution is not available in the current production release."},
	}
	canRunDryRun := providerChecksComplete(checks)
	result := &ProviderSwitchDryRunReadiness{
		RunID:        run.ID,
		SalonID:      run.SalonID,
		FromProvider: run.FromProvider,
		ToProvider:   run.ToProvider,
		Status:       run.Status,
		Checks:       checks,
		CanRunDryRun: canRunDryRun,
		DryRunReady:  run.DryRunReady && canRunDryRun,
		CanActivate:  false,
	}
	for _, check := range checks {
		if !check.Complete && result.BlockedReason == "" {
			result.BlockedReason = check.Message
		}
	}
	return result, nil
}

func (s *ServiceLayer) UpdateProviderSwitchMatch(ctx context.Context, salonID string, ownerUserID string, runID string, matchID string, req ProviderSwitchMatchUpdateRequest) (*ProviderSwitchRun, error) {
	runID = strings.TrimSpace(runID)
	matchID = strings.TrimSpace(matchID)
	matchStatus := strings.TrimSpace(req.MatchStatus)
	if runID == "" || matchID == "" || !allowedSwitchMatchUpdateStatus(matchStatus) {
		return nil, ErrValidation
	}
	run, err := s.repo.GetProviderSwitchRun(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == SwitchRunStatusBlocked || run.Status == SwitchRunStatusActivated || run.Status == SwitchRunStatusCancelled {
		return nil, ErrValidation
	}
	matches, err := s.repo.ListProviderSwitchMatches(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	current, ok := findSwitchMatchByID(matches, matchID)
	if !ok {
		return nil, ErrNotFound
	}
	update := ProviderSwitchMatchUpdateMutation{
		SalonID:           salonID,
		OwnerUserID:       ownerUserID,
		RunID:             runID,
		MatchID:           matchID,
		MatchStatus:       matchStatus,
		CanonicalEntityID: current.CanonicalEntityID,
		CanonicalName:     current.CanonicalName,
		MatchConfidence:   current.MatchConfidence,
		MatchReason:       current.MatchReason,
	}
	switch matchStatus {
	case SwitchMatchStatusConfirmed:
		if strings.TrimSpace(current.CanonicalEntityID) == "" {
			return nil, ErrValidation
		}
		update.MatchConfidence = 100
		update.MatchReason = "Owner confirmed this provider match."
	case SwitchMatchStatusUnmatched:
		update.CanonicalEntityID = ""
		update.CanonicalName = ""
		update.MatchConfidence = 0
		update.MatchReason = "Owner marked this provider record unmatched."
	case SwitchMatchStatusSkipped:
		update.CanonicalEntityID = ""
		update.CanonicalName = ""
		update.MatchConfidence = 0
		update.MatchReason = "Owner skipped this provider record."
	}
	if _, err := s.repo.UpdateProviderSwitchMatch(ctx, update); err != nil {
		return nil, err
	}
	return s.refreshSwitchRunReviewStatus(ctx, salonID, ownerUserID, runID)
}

func (s *ServiceLayer) withSwitchMatches(ctx context.Context, ownerUserID string, run *ProviderSwitchRun) (*ProviderSwitchRun, error) {
	if run == nil {
		return nil, nil
	}
	matches, err := s.repo.ListProviderSwitchMatches(ctx, run.SalonID, ownerUserID, run.ID)
	if err != nil {
		return nil, err
	}
	run.Matches = matches
	run.MatchSummary = summarizeSwitchMatches(matches)
	run.CanActivate = false
	return run, nil
}

func (s *ServiceLayer) refreshSwitchRunReviewStatus(ctx context.Context, salonID string, ownerUserID string, runID string) (*ProviderSwitchRun, error) {
	run, err := s.repo.GetProviderSwitchRun(ctx, salonID, ownerUserID, runID)
	if err != nil {
		return nil, err
	}
	run, err = s.withSwitchMatches(ctx, ownerUserID, run)
	if err != nil {
		return nil, err
	}
	nextStatus := reviewSwitchRunStatus(run.Status, run.MatchSummary)
	if nextStatus != run.Status {
		run, err = s.repo.UpdateProviderSwitchRunStatus(ctx, salonID, ownerUserID, runID, nextStatus, "")
		if err != nil {
			return nil, err
		}
		return s.withSwitchMatches(ctx, ownerUserID, run)
	}
	return run, nil
}

func (s *ServiceLayer) generateProviderSwitchMatches(ctx context.Context, run *ProviderSwitchRun) error {
	if run == nil {
		return nil
	}
	sourceServices, providerServices, err := s.repo.ListProviderSwitchServiceCandidates(ctx, run.SalonID, run.FromProvider, run.ToProvider)
	if err != nil {
		return err
	}
	sourceStaff, providerStaff, err := s.repo.ListProviderSwitchStaffCandidates(ctx, run.SalonID, run.FromProvider, run.ToProvider)
	if err != nil {
		return err
	}
	sourceCustomers, providerCustomers, err := s.repo.ListProviderSwitchCustomerCandidates(ctx, run.SalonID, run.FromProvider, run.ToProvider)
	if err != nil {
		return err
	}

	mutations := make([]ProviderSwitchMatchMutation, 0, len(providerServices)+len(providerStaff)+len(providerCustomers))
	mutations = append(mutations, buildServiceSwitchMatches(sourceServices, providerServices)...)
	mutations = append(mutations, buildStaffSwitchMatches(sourceStaff, providerStaff)...)
	mutations = append(mutations, buildCustomerSwitchMatches(sourceCustomers, providerCustomers)...)
	_, err = s.repo.ReplaceProviderSwitchMatches(ctx, run.SalonID, run.ID, mutations)
	return err
}

type switchCandidateMatch struct {
	canonical  ProviderSwitchEntityCandidate
	confidence int
	reason     string
}

func buildServiceSwitchMatches(source []ProviderSwitchEntityCandidate, provider []ProviderSwitchEntityCandidate) []ProviderSwitchMatchMutation {
	usedCanonical := map[string]bool{}
	matches := make([]ProviderSwitchMatchMutation, 0, len(provider))
	for _, candidate := range provider {
		providerEntityID := strings.TrimSpace(candidate.ProviderEntityID)
		if providerEntityID == "" {
			continue
		}
		match := ProviderSwitchMatchMutation{
			EntityType:              EntityTypeService,
			ProviderEntityID:        providerEntityID,
			ProviderName:            strings.TrimSpace(candidate.Name),
			ProviderDurationMinutes: candidate.DurationMinutes,
			MatchStatus:             SwitchMatchStatusUnmatched,
			MatchReason:             "No service match found.",
		}
		best := bestServiceSwitchMatch(source, candidate)
		applySwitchCandidateMatch(&match, best, usedCanonical)
		matches = append(matches, match)
	}
	return matches
}

func buildStaffSwitchMatches(source []ProviderSwitchEntityCandidate, provider []ProviderSwitchEntityCandidate) []ProviderSwitchMatchMutation {
	usedCanonical := map[string]bool{}
	matches := make([]ProviderSwitchMatchMutation, 0, len(provider))
	for _, candidate := range provider {
		providerEntityID := strings.TrimSpace(candidate.ProviderEntityID)
		if providerEntityID == "" {
			continue
		}
		match := ProviderSwitchMatchMutation{
			EntityType:       EntityTypeStaff,
			ProviderEntityID: providerEntityID,
			ProviderName:     strings.TrimSpace(candidate.Name),
			ProviderPhone:    strings.TrimSpace(candidate.Phone),
			ProviderEmail:    strings.TrimSpace(candidate.Email),
			MatchStatus:      SwitchMatchStatusUnmatched,
			MatchReason:      "No staff match found.",
		}
		best := bestStaffSwitchMatch(source, candidate)
		applySwitchCandidateMatch(&match, best, usedCanonical)
		matches = append(matches, match)
	}
	return matches
}

func buildCustomerSwitchMatches(source []ProviderSwitchEntityCandidate, provider []ProviderSwitchEntityCandidate) []ProviderSwitchMatchMutation {
	usedCanonical := map[string]bool{}
	matches := make([]ProviderSwitchMatchMutation, 0, len(provider))
	for _, candidate := range provider {
		providerEntityID := strings.TrimSpace(candidate.ProviderEntityID)
		if providerEntityID == "" {
			continue
		}
		match := ProviderSwitchMatchMutation{
			EntityType:       EntityTypeCustomer,
			ProviderEntityID: providerEntityID,
			ProviderName:     strings.TrimSpace(candidate.Name),
			ProviderPhone:    strings.TrimSpace(candidate.Phone),
			ProviderEmail:    strings.TrimSpace(candidate.Email),
			MatchStatus:      SwitchMatchStatusUnmatched,
			MatchReason:      "No customer match found.",
		}
		best := bestCustomerSwitchMatch(source, candidate)
		applySwitchCandidateMatch(&match, best, usedCanonical)
		matches = append(matches, match)
	}
	return matches
}

func applySwitchCandidateMatch(match *ProviderSwitchMatchMutation, best switchCandidateMatch, usedCanonical map[string]bool) {
	if best.canonical.ID == "" {
		return
	}
	match.CanonicalEntityID = best.canonical.ID
	match.CanonicalName = strings.TrimSpace(best.canonical.Name)
	match.MatchConfidence = best.confidence
	if usedCanonical[best.canonical.ID] {
		match.MatchStatus = SwitchMatchStatusConflict
		match.MatchReason = "Multiple provider records match the same canonical record."
		return
	}
	usedCanonical[best.canonical.ID] = true
	match.MatchStatus = SwitchMatchStatusSuggested
	match.MatchReason = best.reason
}

func bestServiceSwitchMatch(source []ProviderSwitchEntityCandidate, candidate ProviderSwitchEntityCandidate) switchCandidateMatch {
	targetName := normalizeSwitchText(candidate.Name)
	if targetName == "" {
		return switchCandidateMatch{}
	}
	best := switchCandidateMatch{}
	for _, item := range source {
		if strings.TrimSpace(item.ID) == "" || normalizeSwitchText(item.Name) != targetName {
			continue
		}
		match := switchCandidateMatch{
			canonical:  item,
			confidence: 80,
			reason:     "Service name matches.",
		}
		if candidate.DurationMinutes > 0 && item.DurationMinutes == candidate.DurationMinutes {
			match.confidence = 95
			match.reason = "Service name and duration match."
		}
		if match.confidence > best.confidence {
			best = match
		}
	}
	return best
}

func bestStaffSwitchMatch(source []ProviderSwitchEntityCandidate, candidate ProviderSwitchEntityCandidate) switchCandidateMatch {
	targetEmail := normalizeSwitchEmail(candidate.Email)
	targetPhone := validation.NormalizePhone(candidate.Phone)
	targetName := normalizeSwitchText(candidate.Name)
	best := switchCandidateMatch{}
	for _, item := range source {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		match := switchCandidateMatch{}
		switch {
		case targetEmail != "" && normalizeSwitchEmail(item.Email) == targetEmail:
			match = switchCandidateMatch{canonical: item, confidence: 95, reason: "Staff email matches."}
		case targetPhone != "" && validation.NormalizePhone(item.Phone) == targetPhone:
			match = switchCandidateMatch{canonical: item, confidence: 90, reason: "Staff phone matches."}
		case targetName != "" && normalizeSwitchText(item.Name) == targetName:
			match = switchCandidateMatch{canonical: item, confidence: 75, reason: "Staff name matches."}
		}
		if match.confidence > best.confidence {
			best = match
		}
	}
	return best
}

func bestCustomerSwitchMatch(source []ProviderSwitchEntityCandidate, candidate ProviderSwitchEntityCandidate) switchCandidateMatch {
	targetPhone := validation.NormalizePhone(candidate.Phone)
	targetEmail := normalizeSwitchEmail(candidate.Email)
	targetName := normalizeSwitchText(candidate.Name)
	best := switchCandidateMatch{}
	for _, item := range source {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		match := switchCandidateMatch{}
		switch {
		case targetPhone != "" && validation.NormalizePhone(item.Phone) == targetPhone:
			match = switchCandidateMatch{canonical: item, confidence: 95, reason: "Customer phone matches."}
		case targetEmail != "" && normalizeSwitchEmail(item.Email) == targetEmail:
			match = switchCandidateMatch{canonical: item, confidence: 90, reason: "Customer email matches."}
		case targetName != "" && normalizeSwitchText(item.Name) == targetName:
			match = switchCandidateMatch{canonical: item, confidence: 70, reason: "Customer name matches."}
		}
		if match.confidence > best.confidence {
			best = match
		}
	}
	return best
}

func summarizeSwitchMatches(matches []ProviderSwitchMatch) ProviderSwitchMatchSummary {
	summary := ProviderSwitchMatchSummary{Total: len(matches)}
	for _, match := range matches {
		switch match.MatchStatus {
		case SwitchMatchStatusSuggested:
			summary.Suggested++
		case SwitchMatchStatusUnmatched:
			summary.Unmatched++
		case SwitchMatchStatusConflict:
			summary.Conflicts++
		case SwitchMatchStatusConfirmed:
			summary.Confirmed++
		case SwitchMatchStatusSkipped:
			summary.Skipped++
		}
	}
	return summary
}

func allowedSwitchMatchUpdateStatus(status string) bool {
	switch status {
	case SwitchMatchStatusConfirmed, SwitchMatchStatusUnmatched, SwitchMatchStatusSkipped:
		return true
	default:
		return false
	}
}

func findSwitchMatchByID(matches []ProviderSwitchMatch, matchID string) (ProviderSwitchMatch, bool) {
	for _, match := range matches {
		if match.ID == matchID {
			return match, true
		}
	}
	return ProviderSwitchMatch{}, false
}

func reviewSwitchRunStatus(current string, summary ProviderSwitchMatchSummary) string {
	switch current {
	case SwitchRunStatusBlocked, SwitchRunStatusActivated, SwitchRunStatusCancelled, SwitchRunStatusFailed:
		return current
	}
	if summary.Total == 0 {
		return SwitchRunStatusDraft
	}
	if summary.Suggested > 0 || summary.Unmatched > 0 || summary.Conflicts > 0 {
		return SwitchRunStatusNeedsReview
	}
	return SwitchRunStatusReady
}

func switchRunDryRunReviewable(status string) bool {
	switch status {
	case SwitchRunStatusBlocked, SwitchRunStatusActivated, SwitchRunStatusCancelled, SwitchRunStatusFailed:
		return false
	default:
		return strings.TrimSpace(status) != ""
	}
}

func providerChecksComplete(checks []ProviderReadinessCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.Complete {
			return false
		}
	}
	return true
}

func dryRunCurrentProviderMessage(activeProvider string, fromProvider string, connected bool, synced bool, mapping *ProviderMappingSummary) string {
	if activeProvider != fromProvider {
		return "This switch run no longer starts from the active POS provider."
	}
	if !connected {
		return "Connect the current active provider and select a booking location before dry-run."
	}
	if !synced {
		return "Sync records from the current active provider before dry-run."
	}
	if mapping == nil || mapping.BookableServiceCount == 0 {
		return "At least one active AI-bookable service must have a synced current-provider link."
	}
	if mapping.BookableStaffCount == 0 {
		return "At least one active AI-bookable staff member must have a synced current-provider link."
	}
	return ""
}

func normalizeSwitchText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func normalizeSwitchEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeServiceWriteRequest(req ServiceWriteRequest, defaultActive bool) (ServiceMutation, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || req.DurationMinutes <= 0 {
		return ServiceMutation{}, ErrValidation
	}
	if req.PriceFrom != nil && *req.PriceFrom < 0 {
		return ServiceMutation{}, ErrValidation
	}
	aiDescription := strings.TrimSpace(req.AIDescription)
	if len([]rune(aiDescription)) > 320 {
		return ServiceMutation{}, ErrValidation
	}
	consultationProfile, err := NormalizeConsultationProfileWriteRequest(req.ConsultationProfile)
	if err != nil {
		return ServiceMutation{}, err
	}
	active := defaultActive
	if req.Active != nil {
		active = *req.Active
	}
	return ServiceMutation{
		Name:                name,
		Description:         strings.TrimSpace(req.Description),
		AIDescription:       aiDescription,
		DurationMinutes:     req.DurationMinutes,
		PriceFrom:           req.PriceFrom,
		Active:              active,
		ServiceCategoryID:   strings.TrimSpace(req.ServiceCategoryID),
		ConsultationProfile: consultationProfile,
	}, nil
}

func normalizeServiceOwnerControlsWriteRequest(req ServiceOwnerControlsWriteRequest) (ServiceOwnerControlsMutation, error) {
	aiDescription := strings.TrimSpace(req.AIDescription)
	if len([]rune(aiDescription)) > 320 {
		return ServiceOwnerControlsMutation{}, ErrValidation
	}
	consultationProfile, err := NormalizeConsultationProfileWriteRequest(req.ConsultationProfile)
	if err != nil {
		return ServiceOwnerControlsMutation{}, err
	}
	return ServiceOwnerControlsMutation{
		AIDescription:       aiDescription,
		ServiceCategoryID:   strings.TrimSpace(req.ServiceCategoryID),
		ConsultationProfile: consultationProfile,
	}, nil
}

// NormalizeConsultationProfileWriteRequest is the provider-neutral validation
// boundary shared by direct service edits and portable configuration imports.
func NormalizeConsultationProfileWriteRequest(req *ServiceConsultationProfileWriteRequest) (*ServiceConsultationProfileMutation, error) {
	if req == nil {
		return nil, nil
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = ConsultationProfileStatusDraft
	}
	if status != ConsultationProfileStatusDraft && status != ConsultationProfileStatusReady && status != ConsultationProfileStatusDisabled {
		return nil, ErrValidation
	}
	outcomes, ok := normalizedConsultationValues(req.RecommendedOutcomes, map[string]bool{
		ConsultationOutcomeMaintain: true, ConsultationOutcomeShorten: true, ConsultationOutcomeAddLength: true,
		ConsultationOutcomeAddStrength: true, ConsultationOutcomeRepair: true, ConsultationOutcomeRemoval: true,
		ConsultationOutcomeColorRefresh: true,
	})
	if !ok {
		return nil, ErrValidation
	}
	systems, ok := normalizedConsultationValues(req.CompatibleCurrentSystems, map[string]bool{
		ConsultationSystemNatural: true, ConsultationSystemRegularPolish: true, ConsultationSystemGel: true,
		ConsultationSystemDip: true, ConsultationSystemAcrylic: true, ConsultationSystemExtension: true,
	})
	if !ok {
		return nil, ErrValidation
	}
	lengths, ok := normalizedConsultationValues(req.LengthCapabilities, map[string]bool{
		ConsultationLengthKeep: true, ConsultationLengthShorten: true, ConsultationLengthAddLength: true,
	})
	if !ok {
		return nil, ErrValidation
	}
	priorities, ok := normalizedConsultationValues(req.PriorityTags, map[string]bool{
		ConsultationPriorityDurability: true, ConsultationPriorityLowerMaintenance: true,
		ConsultationPriorityLowerCost: true, ConsultationPriorityShorterVisit: true,
	})
	if !ok {
		return nil, ErrValidation
	}
	finishes, ok := normalizedConsultationValues(req.FinishOptions, map[string]bool{
		ConsultationFinishNatural: true, ConsultationFinishRegularPolish: true, ConsultationFinishGelPolish: true,
		ConsultationFinishGlossy: true, ConsultationFinishMatte: true, ConsultationFinishNailArt: true,
	})
	if !ok {
		return nil, ErrValidation
	}
	maintenanceNote := strings.TrimSpace(req.MaintenanceNote)
	ownerSummary := strings.TrimSpace(req.OwnerApprovedSummary)
	if len([]rune(maintenanceNote)) > 320 || len([]rune(ownerSummary)) > 320 {
		return nil, ErrValidation
	}
	// Recommendation eligibility is fail-closed: an owner-approved profile must
	// define both what it is recommended for and which current systems it can
	// safely be compared against. Optional fields may refine ranking only.
	if status == ConsultationProfileStatusReady && (len(outcomes) == 0 || len(systems) == 0) {
		return nil, ErrValidation
	}
	return &ServiceConsultationProfileMutation{
		Status: status, RecommendedOutcomes: outcomes, CompatibleCurrentSystems: systems,
		LengthCapabilities: lengths, PriorityTags: priorities, FinishOptions: finishes,
		MaintenanceNote: maintenanceNote, OwnerApprovedSummary: ownerSummary,
	}, nil
}

func normalizedConsultationValues(values []string, allowed map[string]bool) ([]string, bool) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !allowed[value] {
			return nil, false
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, true
}

func normalizeServiceCategoryWriteRequest(req ServiceCategoryWriteRequest) (ServiceCategoryMutation, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ServiceCategoryMutation{}, ErrValidation
	}
	slug := normalizeCategorySlug(name)
	if slug == "" {
		return ServiceCategoryMutation{}, ErrValidation
	}
	return ServiceCategoryMutation{
		Name:        name,
		Slug:        slug,
		Description: strings.TrimSpace(req.Description),
		SortOrder:   req.SortOrder,
	}, nil
}

func normalizeServiceCategoryAliasWriteRequest(categoryID string, req ServiceCategoryAliasWriteRequest) (ServiceCategoryAliasMutation, error) {
	alias := strings.TrimSpace(req.Alias)
	normalized := normalizeAliasKey(alias)
	if alias == "" || normalized == "" {
		return ServiceCategoryAliasMutation{}, ErrValidation
	}
	confidence := 0.94
	if req.Confidence != nil {
		confidence = *req.Confidence
	}
	if confidence <= 0 || confidence > 1 {
		return ServiceCategoryAliasMutation{}, ErrValidation
	}
	return ServiceCategoryAliasMutation{
		CategoryID:      categoryID,
		Alias:           alias,
		NormalizedAlias: normalized,
		Confidence:      confidence,
	}, nil
}

func normalizeCategorySlug(value string) string {
	return strings.Trim(normalizeTextKey(value, "-"), "-")
}

func normalizeAliasKey(value string) string {
	return strings.TrimSpace(normalizeTextKey(value, " "))
}

func normalizeTextKey(value string, separator string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	previousSeparator := true
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			previousSeparator = false
		default:
			if !previousSeparator {
				b.WriteString(separator)
				previousSeparator = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func normalizeStaffWriteRequest(req StaffWriteRequest, defaultActive bool) (StaffMutation, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return StaffMutation{}, ErrValidation
	}
	active := defaultActive
	if req.Active != nil {
		active = *req.Active
	}
	return StaffMutation{
		Name:   name,
		Phone:  strings.TrimSpace(req.Phone),
		Email:  strings.TrimSpace(req.Email),
		Active: active,
	}, nil
}

func (s *ServiceLayer) activeProvider(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	provider, err := s.repo.GetActiveProvider(ctx, salonID, ownerUserID)
	if err != nil {
		return "", err
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ProviderSquare, nil
	}
	return provider, nil
}

func (s *ServiceLayer) enqueueServiceSync(ctx context.Context, item *Service, operation string) (bool, error) {
	if item == nil || strings.TrimSpace(item.ID) == "" {
		return false, nil
	}
	if operation == SyncOperationArchiveService && strings.TrimSpace(item.POSServiceID) == "" && !item.POSLinked {
		return false, nil
	}
	providerName := item.POSProvider
	if providerName == "" {
		providerName = ProviderSquare
	}
	if !providerSupportsOperation(s.providers[providerName], operation) {
		return false, nil
	}
	_, err := s.repo.EnqueuePOSSyncJob(ctx, SyncJobMutation{
		SalonID:    item.SalonID,
		Provider:   providerName,
		EntityType: EntityTypeService,
		EntityID:   item.ID,
		Operation:  operation,
	})
	return err == nil, err
}

func (s *ServiceLayer) enqueueStaffSync(ctx context.Context, item *StaffMember, operation string) (bool, error) {
	if item == nil || strings.TrimSpace(item.ID) == "" {
		return false, nil
	}
	if operation == SyncOperationArchiveStaff && strings.TrimSpace(item.POSStaffID) == "" && !item.POSLinked {
		return false, nil
	}
	providerName := item.POSProvider
	if providerName == "" {
		providerName = ProviderSquare
	}
	if !providerSupportsOperation(s.providers[providerName], operation) {
		return false, nil
	}
	_, err := s.repo.EnqueuePOSSyncJob(ctx, SyncJobMutation{
		SalonID:    item.SalonID,
		Provider:   providerName,
		EntityType: EntityTypeStaff,
		EntityID:   item.ID,
		Operation:  operation,
	})
	return err == nil, err
}

func providerSupportsOperation(provider NamedProvider, operation string) bool {
	if provider == nil {
		return false
	}
	capabilitiesProvider, ok := provider.(CapabilityProvider)
	if !ok {
		return false
	}
	capabilities := capabilitiesProvider.Capabilities()
	switch operation {
	case SyncOperationUpsertService:
		return capabilities.ServiceUpsert
	case SyncOperationArchiveService:
		return capabilities.ServiceArchive
	case SyncOperationUpsertStaff:
		return capabilities.StaffUpsert
	case SyncOperationArchiveStaff:
		return capabilities.StaffArchive
	case SyncOperationUpsertCustomer:
		return capabilities.CustomerUpsert
	default:
		return false
	}
}

func (s *ServiceLayer) installedProviderOptions(activeProvider string, activeConnection *Connection) []ProviderOption {
	items := make([]ProviderOption, 0, len(s.providers))
	for name, provider := range s.providers {
		option := ProviderOption{
			Provider:     name,
			Label:        providerLabel(name),
			Installed:    true,
			Active:       name == activeProvider,
			Status:       StatusNotConnected,
			Capabilities: providerCapabilities(provider),
		}
		if activeConnection != nil && activeConnection.Provider == name {
			option.Status = activeConnection.Status
		}
		items = append(items, option)
	}
	if len(items) == 0 {
		items = append(items, ProviderOption{
			Provider:      activeProvider,
			Label:         providerLabel(activeProvider),
			Installed:     false,
			Active:        true,
			Status:        StatusNotConnected,
			BlockedReason: "The active POS provider adapter is not configured in this deployment.",
		})
	}
	return items
}

func unavailableProviderOptions(alternateInstalled bool) []ProviderOption {
	if alternateInstalled {
		return []ProviderOption{}
	}
	return []ProviderOption{{
		Provider:      "",
		Label:         "No alternate POS adapter installed",
		Installed:     false,
		Active:        false,
		Status:        StatusDisabled,
		BlockedReason: "Square Appointments is the only native POS integration in the current production release.",
	}}
}

func providerCapabilities(provider NamedProvider) ProviderCapabilities {
	capabilitiesProvider, ok := provider.(CapabilityProvider)
	if !ok {
		return ProviderCapabilities{}
	}
	return capabilitiesProvider.Capabilities()
}

func (s *ServiceLayer) decorateService(item *Service) *Service {
	if item != nil {
		authority := s.serviceFieldAuthority(item)
		item.FieldAuthority = &authority
	}
	return item
}

func (s *ServiceLayer) decorateStaff(item *StaffMember) *StaffMember {
	if item != nil {
		authority := s.staffFieldAuthority(item)
		item.FieldAuthority = &authority
	}
	return item
}

func (s *ServiceLayer) serviceFieldAuthority(item *Service) EntityFieldAuthority {
	if item == nil {
		return EntityFieldAuthority{OperationalSource: FieldAuthoritySourceManleAI, OperationalWriteMode: OperationalWriteModeLocal}
	}
	provider := recordProvider(item.POSProvider)
	providerBacked := item.Source == EntitySourceImported || strings.TrimSpace(item.POSServiceID) != "" || item.POSLinked
	return s.entityFieldAuthority(provider, providerBacked, SyncOperationUpsertService)
}

func (s *ServiceLayer) staffFieldAuthority(item *StaffMember) EntityFieldAuthority {
	if item == nil {
		return EntityFieldAuthority{OperationalSource: FieldAuthoritySourceManleAI, OperationalWriteMode: OperationalWriteModeLocal}
	}
	provider := recordProvider(item.POSProvider)
	providerBacked := item.Source == EntitySourceImported || strings.TrimSpace(item.POSStaffID) != "" || item.POSLinked
	return s.entityFieldAuthority(provider, providerBacked, SyncOperationUpsertStaff)
}

func (s *ServiceLayer) entityFieldAuthority(provider string, providerBacked bool, operation string) EntityFieldAuthority {
	writeSupported := providerSupportsOperation(s.providers[provider], operation)
	if !providerBacked {
		authority := EntityFieldAuthority{
			OperationalSource:    FieldAuthoritySourceManleAI,
			OperationalWriteMode: OperationalWriteModeLocal,
		}
		if writeSupported {
			authority.Provider = provider
			authority.ProviderLabel = providerLabel(provider)
			authority.OperationalWriteMode = OperationalWriteModeProviderSync
		}
		return authority
	}
	mode := OperationalWriteModeProviderReadOnly
	if writeSupported {
		mode = OperationalWriteModeProviderSync
	}
	return EntityFieldAuthority{
		OperationalSource:    FieldAuthoritySourceProvider,
		Provider:             provider,
		ProviderLabel:        providerLabel(provider),
		OperationalWriteMode: mode,
	}
}

func recordProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ProviderSquare
	}
	return provider
}

func providerLabel(provider string) string {
	switch provider {
	case ProviderSquare:
		return "Square Appointments"
	case "":
		return "No provider"
	default:
		return provider
	}
}

func connectionReady(connection *Connection) bool {
	if connection == nil {
		return false
	}
	switch connection.Status {
	case StatusNotConnected, StatusError, StatusExpiredToken, StatusDisabled:
		return false
	default:
		return strings.TrimSpace(connection.ID) != "" && strings.TrimSpace(connection.LocationID) != ""
	}
}

func connectionSynced(connection *Connection) bool {
	return connectionReady(connection) && connection.Status == StatusActive && connection.LastSyncAt != nil
}

func incompleteProviderMessage(complete bool, message string) string {
	if complete {
		return ""
	}
	return message
}
