package pos

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSyncProcessorProcessesSupportedServiceUpsert(t *testing.T) {
	store := &fakeSyncProcessorStore{
		jobs: []SyncJob{
			{
				ID:         "job_1",
				SalonID:    "salon_1",
				Provider:   "fake",
				EntityType: EntityTypeService,
				EntityID:   "service_1",
				Operation:  SyncOperationUpsertService,
				Status:     SyncJobStatusRunning,
			},
		},
		service: &Service{ID: "service_1", SalonID: "salon_1", POSProvider: "fake", Name: "Classic Manicure"},
	}
	provider := &fakeWriteProvider{
		name:         "fake",
		capabilities: ProviderCapabilities{ServiceUpsert: true},
		serviceResult: &ProviderSyncResult{
			ProviderEntityID: "provider_service_1",
			ProviderVersion:  12,
		},
	}
	processor := NewSyncProcessor(store, []POSProvider{provider})

	processed, err := processor.ProcessOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProcessOnce returned error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if provider.upsertedService == nil || provider.upsertedService.ID != "service_1" {
		t.Fatalf("provider did not receive service: %#v", provider.upsertedService)
	}
	if store.succeededJob.ID != "job_1" || store.syncResult.ProviderEntityID != "provider_service_1" || store.syncResult.ProviderVersion != 12 {
		t.Fatalf("unexpected success result: %#v %#v", store.succeededJob, store.syncResult)
	}
	if store.completedLogStatus != SyncJobStatusSucceeded {
		t.Fatalf("completed log status = %s, want succeeded", store.completedLogStatus)
	}
}

func TestSyncProcessorFailsUnsupportedOperationWithoutProviderWrite(t *testing.T) {
	store := &fakeSyncProcessorStore{
		jobs: []SyncJob{
			{
				ID:         "job_2",
				SalonID:    "salon_1",
				Provider:   "fake",
				EntityType: EntityTypeStaff,
				EntityID:   "staff_1",
				Operation:  SyncOperationUpsertStaff,
				Status:     SyncJobStatusRunning,
			},
		},
		staff: &StaffMember{ID: "staff_1", SalonID: "salon_1", POSProvider: "fake", Name: "Mai Nguyen"},
	}
	provider := &fakeWriteProvider{name: "fake"}
	processor := NewSyncProcessor(store, []POSProvider{provider})

	processed, err := processor.ProcessOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProcessOnce returned error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if provider.upsertedStaff != nil {
		t.Fatalf("provider write should not be called: %#v", provider.upsertedStaff)
	}
	if store.failedJob.ID != "job_2" || !strings.Contains(store.failedMessage, "unsupported") {
		t.Fatalf("unexpected failure: %#v %q", store.failedJob, store.failedMessage)
	}
	if store.loggedError.Operation != SyncOperationUpsertStaff {
		t.Fatalf("unexpected logged error: %#v", store.loggedError)
	}
	if store.completedLogStatus != SyncJobStatusFailed {
		t.Fatalf("completed log status = %s, want failed", store.completedLogStatus)
	}
}

type fakeSyncProcessorStore struct {
	jobs []SyncJob

	service *Service
	staff   *StaffMember

	succeededJob SyncJob
	failedJob    SyncJob
	syncResult   ProviderSyncResult

	failedMessage      string
	completedLogID     string
	completedLogStatus string
	completedLogMsg    string
	loggedError        POSError
}

func (f *fakeSyncProcessorStore) ClaimPOSSyncJobs(ctx context.Context, limit int) ([]SyncJob, error) {
	return f.jobs, nil
}

func (f *fakeSyncProcessorStore) GetServiceForSync(ctx context.Context, salonID string, serviceID string) (*Service, error) {
	if f.service == nil {
		return nil, ErrNotFound
	}
	return f.service, nil
}

func (f *fakeSyncProcessorStore) GetStaffForSync(ctx context.Context, salonID string, staffID string) (*StaffMember, error) {
	if f.staff == nil {
		return nil, ErrNotFound
	}
	return f.staff, nil
}

func (f *fakeSyncProcessorStore) MarkPOSSyncJobSucceeded(ctx context.Context, job SyncJob, result ProviderSyncResult) error {
	f.succeededJob = job
	f.syncResult = result
	return nil
}

func (f *fakeSyncProcessorStore) MarkPOSSyncJobFailed(ctx context.Context, job SyncJob, message string) error {
	f.failedJob = job
	f.failedMessage = message
	return nil
}

func (f *fakeSyncProcessorStore) CreateSyncLog(ctx context.Context, salonID string, provider string, syncType string) (string, error) {
	return "sync_log_1", nil
}

func (f *fakeSyncProcessorStore) CompleteSyncLog(ctx context.Context, id string, status string, message string) error {
	f.completedLogID = id
	f.completedLogStatus = status
	f.completedLogMsg = message
	return nil
}

func (f *fakeSyncProcessorStore) LogError(ctx context.Context, item POSError) error {
	f.loggedError = item
	return nil
}

type fakeWriteProvider struct {
	name         string
	capabilities ProviderCapabilities

	serviceResult *ProviderSyncResult
	staffResult   *ProviderSyncResult
	writeErr      error

	upsertedService *Service
	archivedService *Service
	upsertedStaff   *StaffMember
	archivedStaff   *StaffMember
}

func (f *fakeWriteProvider) Name() string {
	return f.name
}

func (f *fakeWriteProvider) Capabilities() ProviderCapabilities {
	return f.capabilities
}

func (f *fakeWriteProvider) UpsertService(ctx context.Context, salonID string, service Service) (*ProviderSyncResult, error) {
	f.upsertedService = &service
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.serviceResult, nil
}

func (f *fakeWriteProvider) ArchiveService(ctx context.Context, salonID string, service Service) (*ProviderSyncResult, error) {
	f.archivedService = &service
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.serviceResult, nil
}

func (f *fakeWriteProvider) UpsertStaff(ctx context.Context, salonID string, staff StaffMember) (*ProviderSyncResult, error) {
	f.upsertedStaff = &staff
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.staffResult, nil
}

func (f *fakeWriteProvider) ArchiveStaff(ctx context.Context, salonID string, staff StaffMember) (*ProviderSyncResult, error) {
	f.archivedStaff = &staff
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.staffResult, nil
}

func (f *fakeWriteProvider) UpsertCustomer(ctx context.Context, salonID string, customer Customer) (*ProviderSyncResult, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return nil, errors.New("customer sync is not implemented in test provider")
}

func (f *fakeWriteProvider) Connect(ctx context.Context, input ConnectInput) (*Connection, error) {
	return nil, nil
}

func (f *fakeWriteProvider) HealthCheck(ctx context.Context, salonID string) error {
	return nil
}

func (f *fakeWriteProvider) ListLocations(ctx context.Context, salonID string) ([]Location, error) {
	return nil, nil
}

func (f *fakeWriteProvider) ListServices(ctx context.Context, salonID string) ([]Service, error) {
	return nil, nil
}

func (f *fakeWriteProvider) ListStaff(ctx context.Context, salonID string) ([]StaffMember, error) {
	return nil, nil
}

func (f *fakeWriteProvider) SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*Customer, error) {
	return nil, nil
}

func (f *fakeWriteProvider) CreateCustomer(ctx context.Context, salonID string, input CreateCustomerInput) (*Customer, error) {
	return nil, nil
}

func (f *fakeWriteProvider) CheckAvailability(ctx context.Context, salonID string, input AvailabilityInput) ([]TimeSlot, error) {
	return nil, nil
}

func (f *fakeWriteProvider) CreateAppointment(ctx context.Context, salonID string, input CreateAppointmentInput) (*Appointment, error) {
	return nil, nil
}

func (f *fakeWriteProvider) RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input RescheduleInput) (*Appointment, error) {
	return nil, nil
}

func (f *fakeWriteProvider) CancelAppointment(ctx context.Context, salonID string, appointmentID string, input CancelInput) (*Appointment, error) {
	return nil, nil
}

func (f *fakeWriteProvider) Sync(ctx context.Context, salonID string) error {
	return nil
}

func TestSyncProcessorMarksProviderWriteErrorsFailed(t *testing.T) {
	store := &fakeSyncProcessorStore{
		jobs: []SyncJob{
			{
				ID:         "job_3",
				SalonID:    "salon_1",
				Provider:   "fake",
				EntityType: EntityTypeService,
				EntityID:   "service_1",
				Operation:  SyncOperationUpsertService,
				Status:     SyncJobStatusRunning,
			},
		},
		service: &Service{ID: "service_1", SalonID: "salon_1", POSProvider: "fake", Name: "Classic Manicure"},
	}
	provider := &fakeWriteProvider{
		name:         "fake",
		capabilities: ProviderCapabilities{ServiceUpsert: true},
		writeErr:     errors.New("provider catalog write failed"),
	}
	processor := NewSyncProcessor(store, []POSProvider{provider})

	processed, err := processor.ProcessOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProcessOnce returned error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if store.failedJob.ID != "job_3" || store.failedMessage != "provider catalog write failed" {
		t.Fatalf("unexpected failed job: %#v %q", store.failedJob, store.failedMessage)
	}
	if store.loggedError.ErrorMessage != "provider catalog write failed" {
		t.Fatalf("unexpected logged error: %#v", store.loggedError)
	}
}
