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

	res, err := service.List(context.Background(), " salon_1 ", " owner_1 ", 500)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if store.limit != maxCustomerLimit {
		t.Fatalf("limit = %d, want %d", store.limit, maxCustomerLimit)
	}
	if len(res.Customers) != 2 {
		t.Fatalf("customers length = %d, want 2", len(res.Customers))
	}
	if res.Summary.TotalKnownCustomers != 2 || res.Summary.ConfirmedAppointments != 2 || res.Summary.PendingRequests != 1 || res.Summary.CustomersWithCalls != 1 {
		t.Fatalf("unexpected summary: %#v", res.Summary)
	}
	if res.Summary.LastCustomerActivityAt == nil || !res.Summary.LastCustomerActivityAt.Equal(now) {
		t.Fatalf("last activity = %v, want %v", res.Summary.LastCustomerActivityAt, now)
	}
}

func TestListRejectsMissingScope(t *testing.T) {
	service := NewService(&fakeStore{}, nil)

	_, err := service.List(context.Background(), "", "owner_1", 0)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestListChecksOwnerBeforeAggregate(t *testing.T) {
	store := &fakeStore{err: ErrNotFound}
	service := NewService(store, nil)

	_, err := service.List(context.Background(), "salon_1", "owner_1", 0)
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

type fakeStore struct {
	customers    []Record
	limit        int
	ensureCalled bool
	listCalled   bool
	err          error
}

func (f *fakeStore) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	f.ensureCalled = true
	return f.err
}

func (f *fakeStore) ListCustomers(ctx context.Context, salonID string, ownerUserID string, limit int) ([]Record, error) {
	f.listCalled = true
	f.limit = limit
	return f.customers, f.err
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
