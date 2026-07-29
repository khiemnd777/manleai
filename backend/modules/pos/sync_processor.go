package pos

import (
	"context"
	"errors"
	"fmt"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

var ErrPOSSyncUnsupported = errors.New("pos sync operation is unsupported")

type SyncProcessorStore interface {
	ClaimPOSSyncJobs(ctx context.Context, limit int) ([]SyncJob, error)
	GetServiceForSync(ctx context.Context, salonID string, serviceID string) (*Service, error)
	GetStaffForSync(ctx context.Context, salonID string, staffID string) (*StaffMember, error)
	MarkPOSSyncJobSucceeded(ctx context.Context, job SyncJob, result ProviderSyncResult) error
	MarkPOSSyncJobFailed(ctx context.Context, job SyncJob, message string) error
	CreateSyncLog(ctx context.Context, salonID string, provider string, syncType string) (string, error)
	CompleteSyncLog(ctx context.Context, id string, status string, message string) error
	LogError(ctx context.Context, item POSError) error
}

type SyncProcessor struct {
	store     SyncProcessorStore
	providers map[string]POSProvider
}

func NewSyncProcessor(store SyncProcessorStore, providers []POSProvider) *SyncProcessor {
	byName := make(map[string]POSProvider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		byName[provider.Name()] = provider
	}
	return &SyncProcessor{store: store, providers: byName}
}

func (p *SyncProcessor) ProcessOnce(ctx context.Context, limit int) (int, error) {
	jobs, err := p.store.ClaimPOSSyncJobs(ctx, limit)
	if err != nil {
		return 0, err
	}
	for index, job := range jobs {
		if err := p.processOne(ctx, job); err != nil {
			return index, err
		}
	}
	return len(jobs), nil
}

func (p *SyncProcessor) processOne(ctx context.Context, job SyncJob) error {
	ctx = databasecontext.WithSystemSalon(ctx, databasecontext.ScopeWorker, job.SalonID)
	logID, err := p.store.CreateSyncLog(ctx, job.SalonID, job.Provider, job.Operation)
	if err != nil {
		return err
	}
	err = p.runProviderWrite(ctx, job)
	if err != nil {
		code := ErrorUnknown
		if errors.Is(err, ErrPOSSyncUnsupported) {
			code = ErrorWriteUnsupported
		}
		message := SafeErrorMessage(code)
		_ = p.store.LogError(ctx, POSError{
			SalonID:      job.SalonID,
			Provider:     job.Provider,
			Operation:    job.Operation,
			ErrorCode:    code,
			ErrorMessage: message,
		})
		if completeErr := p.store.CompleteSyncLog(ctx, logID, SyncJobStatusFailed, message); completeErr != nil {
			return completeErr
		}
		return p.store.MarkPOSSyncJobFailed(ctx, job, message)
	}
	if err := p.store.CompleteSyncLog(ctx, logID, SyncJobStatusSucceeded, "POS sync job completed."); err != nil {
		return err
	}
	return nil
}

func (p *SyncProcessor) runProviderWrite(ctx context.Context, job SyncJob) error {
	provider := p.providers[job.Provider]
	if provider == nil {
		return fmt.Errorf("%w: provider %q is not configured", ErrPOSSyncUnsupported, job.Provider)
	}
	if !providerSupportsOperation(provider, job.Operation) {
		return fmt.Errorf("%w: provider %q does not support %s", ErrPOSSyncUnsupported, job.Provider, job.Operation)
	}
	writer, ok := provider.(POSWriteProvider)
	if !ok {
		return fmt.Errorf("%w: provider %q does not implement POS write methods", ErrPOSSyncUnsupported, job.Provider)
	}

	var result *ProviderSyncResult
	var err error
	switch job.Operation {
	case SyncOperationUpsertService:
		service, loadErr := p.store.GetServiceForSync(ctx, job.SalonID, job.EntityID)
		if loadErr != nil {
			return loadErr
		}
		result, err = writer.UpsertService(ctx, job.SalonID, *service)
	case SyncOperationArchiveService:
		service, loadErr := p.store.GetServiceForSync(ctx, job.SalonID, job.EntityID)
		if loadErr != nil {
			return loadErr
		}
		result, err = writer.ArchiveService(ctx, job.SalonID, *service)
		if result == nil {
			result = &ProviderSyncResult{ProviderEntityID: service.POSServiceID, ProviderVersion: service.POSServiceVersion}
		}
	case SyncOperationUpsertStaff:
		staff, loadErr := p.store.GetStaffForSync(ctx, job.SalonID, job.EntityID)
		if loadErr != nil {
			return loadErr
		}
		result, err = writer.UpsertStaff(ctx, job.SalonID, *staff)
	case SyncOperationArchiveStaff:
		staff, loadErr := p.store.GetStaffForSync(ctx, job.SalonID, job.EntityID)
		if loadErr != nil {
			return loadErr
		}
		result, err = writer.ArchiveStaff(ctx, job.SalonID, *staff)
		if result == nil {
			result = &ProviderSyncResult{ProviderEntityID: staff.POSStaffID}
		}
	default:
		return fmt.Errorf("%w: operation %q is not available in this slice", ErrPOSSyncUnsupported, job.Operation)
	}
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("provider %q returned no sync result for %s", job.Provider, job.Operation)
	}
	if job.Operation == SyncOperationUpsertService || job.Operation == SyncOperationUpsertStaff {
		if result.ProviderEntityID == "" {
			return fmt.Errorf("provider %q returned no provider entity id for %s", job.Provider, job.Operation)
		}
	}
	return p.store.MarkPOSSyncJobSucceeded(ctx, job, *result)
}
