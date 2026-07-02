package customer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

func TestListReturnsCustomersAndSummary(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		customers: []Record{
			{
				Key:                   "phone:+13125550101",
				Name:                  "Linh Tran",
				Phone:                 "+13125550101",
				LastActivityAt:        now,
				LastActivitySource:    SourceAppointment,
				LastOutcome:           "confirmed",
				ConfirmedAppointments: 2,
				PendingRequests:       0,
				CallCount:             1,
			},
			{
				Key:                "phone:+13125550102",
				Name:               "Mai Nguyen",
				Phone:              "+13125550102",
				LastActivityAt:     now.Add(-time.Hour),
				LastActivitySource: SourceBookingAttempt,
				LastOutcome:        "fallback_pending",
				PendingRequests:    1,
			},
		},
	}
	service := NewService(store, nil)

	res, err := service.List(context.Background(), " salon_1 ", " owner_1 ", 500, -10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if store.limit != maxCustomerLimit+1 {
		t.Fatalf("limit = %d, want %d", store.limit, maxCustomerLimit+1)
	}
	if store.offset != 0 {
		t.Fatalf("offset = %d, want 0", store.offset)
	}
	if len(res.Customers) != 2 {
		t.Fatalf("customers length = %d, want 2", len(res.Customers))
	}
	if res.Limit != maxCustomerLimit || res.Offset != 0 || res.HasMore {
		t.Fatalf("unexpected pagination metadata: limit=%d offset=%d has_more=%v", res.Limit, res.Offset, res.HasMore)
	}
	if res.Summary.TotalKnownCustomers != 2 || res.Summary.ActiveCustomers != 0 || res.Summary.POSLinkedCustomers != 0 || res.Summary.ConfirmedAppointments != 2 || res.Summary.PendingRequests != 1 || res.Summary.CustomersWithCalls != 1 {
		t.Fatalf("unexpected summary: %#v", res.Summary)
	}
	if res.Summary.LastCustomerActivityAt == nil || !res.Summary.LastCustomerActivityAt.Equal(now) {
		t.Fatalf("last activity = %v, want %v", res.Summary.LastCustomerActivityAt, now)
	}
}

func TestListReturnsHasMoreWithoutSlicingSummary(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{
		customers: []Record{
			{
				ID:                    "customer_1",
				Key:                   "customer:customer_1",
				Name:                  "Linh Tran",
				Active:                true,
				POSLinked:             true,
				LastActivityAt:        now,
				ConfirmedAppointments: 2,
				CallCount:             1,
			},
			{
				ID:                    "customer_2",
				Key:                   "customer:customer_2",
				Name:                  "Mai Nguyen",
				Active:                true,
				LastActivityAt:        now.Add(-time.Hour),
				ConfirmedAppointments: 1,
				PendingRequests:       1,
			},
		},
	}
	service := NewService(store, nil)

	res, err := service.List(context.Background(), "salon_1", "owner_1", 1, 10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if store.limit != 2 || store.offset != 10 {
		t.Fatalf("store pagination = limit %d offset %d, want limit 2 offset 10", store.limit, store.offset)
	}
	if len(res.Customers) != 1 {
		t.Fatalf("customers length = %d, want 1", len(res.Customers))
	}
	if res.Limit != 1 || res.Offset != 10 || !res.HasMore {
		t.Fatalf("unexpected pagination metadata: limit=%d offset=%d has_more=%v", res.Limit, res.Offset, res.HasMore)
	}
	if res.Summary.TotalKnownCustomers != 2 || res.Summary.ActiveCustomers != 2 || res.Summary.POSLinkedCustomers != 1 || res.Summary.ConfirmedAppointments != 3 || res.Summary.PendingRequests != 1 {
		t.Fatalf("summary should cover full list, got %#v", res.Summary)
	}
}

func TestListRejectsMissingScope(t *testing.T) {
	service := NewService(&fakeStore{}, nil)

	_, err := service.List(context.Background(), "", "owner_1", 0, 0)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestListChecksOwnerBeforeAggregate(t *testing.T) {
	store := &fakeStore{err: ErrNotFound}
	service := NewService(store, nil)

	_, err := service.List(context.Background(), "salon_1", "owner_1", 0, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if !store.ensureCalled {
		t.Fatal("expected owner check")
	}
	if store.listCalled {
		t.Fatal("aggregate query should not run after owner check failure")
	}
}

func TestSearchPOSNormalizesPhoneAndDelegatesProvider(t *testing.T) {
	store := &fakeStore{}
	provider := &fakeProvider{customer: &pos.Customer{POSCustomerID: "cust_1", Name: "Linh Tran", Phone: "+13125550101"}}
	service := NewService(store, []pos.POSProvider{provider})

	res, err := service.SearchPOS(context.Background(), "salon_1", "owner_1", "", "(312) 555-0101")
	if err != nil {
		t.Fatalf("SearchPOS returned error: %v", err)
	}
	if !store.ensureCalled {
		t.Fatal("expected owner check")
	}
	if provider.phone != "3125550101" {
		t.Fatalf("provider phone = %q, want normalized digits", provider.phone)
	}
	if !res.Found || res.Customer.POSCustomerID != "cust_1" || res.Provider != pos.ProviderSquare {
		t.Fatalf("unexpected response: %#v", res)
	}
}

func TestSearchPOSRejectsInvalidRequestBeforeOwnerCheck(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, []pos.POSProvider{&fakeProvider{}})

	_, err := service.SearchPOS(context.Background(), "salon_1", "owner_1", pos.ProviderSquare, " ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if store.ensureCalled {
		t.Fatal("owner check should not run for invalid phone")
	}
}

func TestSearchPOSRequiresProvider(t *testing.T) {
	service := NewService(&fakeStore{}, nil)

	_, err := service.SearchPOS(context.Background(), "salon_1", "owner_1", pos.ProviderSquare, "+13125550101")
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
}

func TestCreateNormalizesAndPersistsCustomer(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, nil)

	res, err := service.Create(context.Background(), "salon_1", "owner_1", WriteRequest{
		Name:  " Linh Tran ",
		Phone: "(312) 555-0101",
		Email: " LINH@EXAMPLE.COM ",
		Notes: " First visit ",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if store.created == nil {
		t.Fatal("expected create call")
	}
	if store.created.Phone != "3125550101" || store.created.NormalizedEmail != "linh@example.com" || store.created.Notes != "First visit" {
		t.Fatalf("created input = %#v, want normalized customer", store.created)
	}
	if res.Customer.ID != "customer_new" || !res.Customer.Active {
		t.Fatalf("response = %#v, want active customer", res.Customer)
	}
}

func TestCreateRejectsMissingContact(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, nil)

	_, err := service.Create(context.Background(), "salon_1", "owner_1", WriteRequest{Name: "Linh Tran"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if store.ensureCalled {
		t.Fatal("owner check should not run for invalid customer")
	}
}

func TestCreateSurfacesDuplicate(t *testing.T) {
	store := &fakeStore{writeErr: ErrDuplicate}
	service := NewService(store, nil)

	_, err := service.Create(context.Background(), "salon_1", "owner_1", WriteRequest{Name: "Linh Tran", Phone: "3125550101"})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("error = %v, want ErrDuplicate", err)
	}
}

func TestUpdateAndArchiveCustomer(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, nil)
	active := false

	updated, err := service.Update(context.Background(), "salon_1", "owner_1", " customer_1 ", WriteRequest{
		Name:   "Linh Tran",
		Phone:  "3125550102",
		Active: &active,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if store.updated == nil || store.updated.Active {
		t.Fatalf("updated = %#v, want inactive mutation", store.updated)
	}
	if updated.Customer.ID != "customer_1" {
		t.Fatalf("updated response = %#v", updated.Customer)
	}

	archived, err := service.Archive(context.Background(), "salon_1", "owner_1", "customer_1")
	if err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	if store.archivedID != "customer_1" || archived.Customer.SyncStatus != pos.SyncStatusArchived {
		t.Fatalf("archive state id=%s response=%#v", store.archivedID, archived.Customer)
	}
}

type fakeStore struct {
	customers    []Record
	limit        int
	offset       int
	ensureCalled bool
	listCalled   bool
	created      *Mutation
	updated      *Mutation
	archivedID   string
	err          error
	writeErr     error
}

func (f *fakeStore) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	f.ensureCalled = true
	return f.err
}

func (f *fakeStore) ListCustomers(ctx context.Context, salonID string, ownerUserID string, limit int, offset int) ([]Record, error) {
	f.listCalled = true
	f.limit = limit
	f.offset = offset
	return f.customers, f.err
}

func (f *fakeStore) CustomerSummary(ctx context.Context, salonID string, ownerUserID string) (Summary, error) {
	if f.err != nil {
		return Summary{}, f.err
	}
	return summarize(f.customers), nil
}

func (f *fakeStore) CreateCustomer(ctx context.Context, salonID string, ownerUserID string, input Mutation) (*Record, error) {
	f.created = &input
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &Record{
		ID:         "customer_new",
		Key:        "customer:customer_new",
		SalonID:    salonID,
		Name:       input.Name,
		Phone:      input.Phone,
		Email:      input.Email,
		Notes:      input.Notes,
		Active:     input.Active,
		SyncStatus: pos.SyncStatusLocalOnly,
		Source:     pos.EntitySourceLocal,
	}, nil
}

func (f *fakeStore) UpdateCustomer(ctx context.Context, salonID string, ownerUserID string, customerID string, input Mutation) (*Record, error) {
	f.updated = &input
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &Record{
		ID:         customerID,
		Key:        "customer:" + customerID,
		SalonID:    salonID,
		Name:       input.Name,
		Phone:      input.Phone,
		Email:      input.Email,
		Active:     input.Active,
		SyncStatus: pos.SyncStatusLocalOnly,
		Source:     pos.EntitySourceLocal,
	}, nil
}

func (f *fakeStore) ArchiveCustomer(ctx context.Context, salonID string, ownerUserID string, customerID string) (*Record, error) {
	f.archivedID = customerID
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	return &Record{
		ID:         customerID,
		Key:        "customer:" + customerID,
		SalonID:    salonID,
		Name:       "Linh Tran",
		Active:     false,
		SyncStatus: pos.SyncStatusArchived,
		ArchivedAt: &now,
		Source:     pos.EntitySourceLocal,
	}, nil
}

type fakeProvider struct {
	customer *pos.Customer
	phone    string
	err      error
}

func (f *fakeProvider) Name() string {
	return pos.ProviderSquare
}

func (f *fakeProvider) Connect(ctx context.Context, input pos.ConnectInput) (*pos.Connection, error) {
	return nil, nil
}

func (f *fakeProvider) HealthCheck(ctx context.Context, salonID string) error {
	return nil
}

func (f *fakeProvider) ListLocations(ctx context.Context, salonID string) ([]pos.Location, error) {
	return nil, nil
}

func (f *fakeProvider) ListServices(ctx context.Context, salonID string) ([]pos.Service, error) {
	return nil, nil
}

func (f *fakeProvider) ListStaff(ctx context.Context, salonID string) ([]pos.StaffMember, error) {
	return nil, nil
}

func (f *fakeProvider) SearchCustomerByPhone(ctx context.Context, salonID string, phone string) (*pos.Customer, error) {
	f.phone = phone
	return f.customer, f.err
}

func (f *fakeProvider) CreateCustomer(ctx context.Context, salonID string, input pos.CreateCustomerInput) (*pos.Customer, error) {
	return nil, nil
}

func (f *fakeProvider) CheckAvailability(ctx context.Context, salonID string, input pos.AvailabilityInput) ([]pos.TimeSlot, error) {
	return nil, nil
}

func (f *fakeProvider) CreateAppointment(ctx context.Context, salonID string, input pos.CreateAppointmentInput) (*pos.Appointment, error) {
	return nil, nil
}

func (f *fakeProvider) RescheduleAppointment(ctx context.Context, salonID string, appointmentID string, input pos.RescheduleInput) (*pos.Appointment, error) {
	return nil, nil
}

func (f *fakeProvider) CancelAppointment(ctx context.Context, salonID string, appointmentID string, input pos.CancelInput) (*pos.Appointment, error) {
	return nil, nil
}

func (f *fakeProvider) Sync(ctx context.Context, salonID string) error {
	return nil
}
