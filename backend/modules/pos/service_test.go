package pos

import (
	"context"
	"errors"
	"testing"
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

type fakePOSStore struct {
	serviceUpdate struct {
		salonID     string
		ownerUserID string
		serviceID   string
		aiBookable  bool
	}
	staffUpdate struct {
		salonID     string
		ownerUserID string
		staffID     string
		aiBookable  bool
	}
}

func (f *fakePOSStore) EnsureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	return nil
}

func (f *fakePOSStore) ListServices(ctx context.Context, salonID string, provider string) ([]Service, error) {
	return nil, nil
}

func (f *fakePOSStore) ListStaff(ctx context.Context, salonID string, provider string) ([]StaffMember, error) {
	return nil, nil
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
