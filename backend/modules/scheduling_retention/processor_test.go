package schedulingretention

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeRetentionStore struct {
	remaining map[string]int
	calls     []string
	errKind   string
}

func (f *fakeRetentionStore) ProcessNext(_ context.Context, kind string) (bool, error) {
	f.calls = append(f.calls, kind)
	if kind == f.errKind {
		return false, errors.New("database dependency failed")
	}
	if f.remaining[kind] <= 0 {
		return false, nil
	}
	f.remaining[kind]--
	return true, nil
}

func TestProcessorUsesBoundedRoundRobinAcrossRetentionOwners(t *testing.T) {
	store := &fakeRetentionStore{remaining: map[string]int{
		KindOwnerRetentionExpiry:    1,
		KindCustomerRetentionExpiry: 1,
		KindSchedulingRequest:       5,
		KindOwnerNotification:       1,
		KindCustomerNotification:    1,
		KindVoiceAudio:              1,
	}}
	processed, err := NewProcessor(store).ProcessOnce(context.Background(), 6)
	if err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if processed != 6 {
		t.Fatalf("processed=%d", processed)
	}
	want := []string{KindOwnerRetentionExpiry, KindCustomerRetentionExpiry, KindSchedulingRequest, KindOwnerNotification, KindCustomerNotification, KindVoiceAudio}
	if !reflect.DeepEqual(store.calls, want) {
		t.Fatalf("calls=%v, want fair first cycle %v", store.calls, want)
	}
}

func TestProcessorStopsIdempotentlyWhenNoRowsRemain(t *testing.T) {
	store := &fakeRetentionStore{remaining: map[string]int{KindVoiceAudio: 1}}
	processor := NewProcessor(store)
	first, err := processor.ProcessOnce(context.Background(), 10)
	if err != nil || first != 1 {
		t.Fatalf("first run=%d/%v", first, err)
	}
	second, err := processor.ProcessOnce(context.Background(), 10)
	if err != nil || second != 0 {
		t.Fatalf("repeat run=%d/%v", second, err)
	}
}

func TestProcessorReturnsOnlySafeDependencyErrorToSchedulerBoundary(t *testing.T) {
	store := &fakeRetentionStore{
		remaining: map[string]int{},
		errKind:   KindCustomerNotification,
	}
	processed, err := NewProcessor(store).ProcessOnce(context.Background(), 10)
	if processed != 0 || err == nil {
		t.Fatalf("processed/error=%d/%v", processed, err)
	}
}

func TestProcessorClampsInvalidAndOversizedBatches(t *testing.T) {
	if got := clampBatch(0); got != DefaultProcessBatch {
		t.Fatalf("default batch=%d", got)
	}
	if got := clampBatch(MaxProcessBatch + 1); got != MaxProcessBatch {
		t.Fatalf("max batch=%d", got)
	}
}
