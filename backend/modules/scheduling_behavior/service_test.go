package scheduling_behavior

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type fakeStore struct {
	state       PersistedState
	command     UpdateBookingModeCommand
	result      BookingModeMutationResult
	replayed    bool
	getErr      error
	mutationErr error
}

func (store *fakeStore) Get(context.Context, string) (PersistedState, error) {
	return store.state, store.getErr
}

func (store *fakeStore) UpdateBookingMode(_ context.Context, command UpdateBookingModeCommand) (BookingModeMutationResult, bool, error) {
	store.command = command
	if store.mutationErr != nil {
		return BookingModeMutationResult{}, false, store.mutationErr
	}
	if store.result.Version == 0 {
		store.result = BookingModeMutationResult{BookingMode: command.BookingMode, Version: command.ExpectedVersion + 1}
	}
	return store.result, store.replayed, nil
}

func TestGetReturnsPersistedPolicyAndEffectiveBehavior(t *testing.T) {
	service := NewService(&fakeStore{state: PersistedState{
		SchedulingAuthority: booking.SchedulingAuthorityManleAICalendar,
		AuthorityVersion:    4, BookingMode: scheduling.BookingModePendingApproval, PolicyVersion: 7,
	}})
	state, err := service.Get(context.Background(), "salon-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if state.EffectiveBehavior != scheduling.ConversationSchedulingBehaviorOwnerReview || state.PolicyVersion != 7 || len(state.AllowedBookingModes) != 3 {
		t.Fatalf("state=%#v", state)
	}
}

func TestGetRejectsPersistedIncompatiblePolicy(t *testing.T) {
	service := NewService(&fakeStore{state: PersistedState{
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		BookingMode:         scheduling.BookingModeConfirmedBooking,
	}})
	if _, err := service.Get(context.Background(), "salon-1"); !errors.Is(err, ErrIncompatibleMode) {
		t.Fatalf("error=%v, want incompatible", err)
	}
}

func TestUpdateBookingModeCreatesStableScopedCommand(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	result, replayed, err := service.UpdateBookingMode(context.Background(), "salon-1", "admin-1", UpdateBookingModeRequest{
		BookingMode: scheduling.BookingModeConfirmedBooking, ExpectedVersion: 3, ActionKey: "set-confirmed-1",
	})
	if err != nil || replayed || result.Version != 4 {
		t.Fatalf("result=%#v replayed=%t error=%v", result, replayed, err)
	}
	if store.command.SalonID != "salon-1" || store.command.ActorUserID != "admin-1" || store.command.RequestFingerprint == "" {
		t.Fatalf("command=%#v", store.command)
	}
}

func TestUpdateBookingModeValidatesBeforePersistence(t *testing.T) {
	service := NewService(&fakeStore{})
	requests := []UpdateBookingModeRequest{
		{BookingMode: scheduling.BookingMode("other"), ExpectedVersion: 1, ActionKey: "unknown"},
		{BookingMode: scheduling.BookingModePendingApproval, ExpectedVersion: -1, ActionKey: "negative"},
		{BookingMode: scheduling.BookingModePendingApproval, ExpectedVersion: 1},
	}
	for _, request := range requests {
		if _, _, err := service.UpdateBookingMode(context.Background(), "salon-1", "admin-1", request); !errors.Is(err, ErrValidation) {
			t.Fatalf("request=%#v error=%v", request, err)
		}
	}
}
