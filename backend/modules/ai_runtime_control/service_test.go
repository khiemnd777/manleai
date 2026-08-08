package ai_runtime_control

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/modules/pos"
)

type fakeRuntimeStore struct {
	state       pos.AIRuntimeState
	mutation    pos.AIRuntimeMutation
	replayed    bool
	getErr      error
	mutationErr error
}

func (store *fakeRuntimeStore) GetSalonAIRuntimeForPlatform(context.Context, string) (pos.AIRuntimeState, error) {
	return store.state, store.getErr
}

func (store *fakeRuntimeStore) SetSalonAIRuntimeForPlatform(_ context.Context, mutation pos.AIRuntimeMutation) (pos.AIRuntimeState, bool, error) {
	store.mutation = mutation
	if store.mutationErr != nil {
		return pos.AIRuntimeState{}, false, store.mutationErr
	}
	return pos.AIRuntimeState{Enabled: mutation.Enabled, Version: mutation.ExpectedVersion + 1}, store.replayed, nil
}

func TestUpdateIsAuthorityNeutralAndReplaySafe(t *testing.T) {
	store := &fakeRuntimeStore{state: pos.AIRuntimeState{Enabled: false, Version: 3}}
	service := NewService(store)

	result, replayed, err := service.Update(context.Background(), "salon-1", "admin-1", UpdateRequest{
		Enabled: true, ActionKey: "enable-runtime-1", ExpectedVersion: 3,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if replayed || !result.Enabled || result.Version != 4 {
		t.Fatalf("result/replayed = %#v/%v", result, replayed)
	}
	if store.mutation.SalonID != "salon-1" || store.mutation.ActorUserID != "admin-1" || store.mutation.ActionKey != "enable-runtime-1" {
		t.Fatalf("mutation identity = %#v", store.mutation)
	}
	if store.mutation.RequestFingerprint == "" {
		t.Fatal("request fingerprint must be stable and non-empty")
	}
}

func TestUpdateValidatesCommandBeforePersistence(t *testing.T) {
	service := NewService(&fakeRuntimeStore{})
	for _, request := range []UpdateRequest{
		{Enabled: true, ActionKey: "", ExpectedVersion: 1},
		{Enabled: true, ActionKey: "enable", ExpectedVersion: -1},
	} {
		if _, _, err := service.Update(context.Background(), "salon-1", "admin-1", request); !errors.Is(err, ErrValidation) {
			t.Fatalf("request=%#v err=%v, want validation", request, err)
		}
	}
}
