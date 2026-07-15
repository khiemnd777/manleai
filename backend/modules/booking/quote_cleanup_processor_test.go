package booking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAvailabilityQuoteCleanupProcessorDrainsBoundedBatchesWithStableRetentionCutoffs(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	store := &fakeAvailabilityQuoteCleanupStore{results: []availabilityQuoteCleanupResult{
		{deleted: 250},
		{deleted: 250},
		{deleted: 25},
	}}
	processor := NewAvailabilityQuoteCleanupProcessor(store)
	processor.now = func() time.Time { return now }

	deleted, err := processor.ProcessOnce(context.Background(), 250)
	if err != nil {
		t.Fatalf("process quote cleanup: %v", err)
	}
	if deleted != 525 {
		t.Fatalf("deleted = %d, want 525", deleted)
	}
	if len(store.calls) != 3 {
		t.Fatalf("cleanup calls = %d, want 3 batches", len(store.calls))
	}
	for index, call := range store.calls {
		if !call.unconsumedExpiredBefore.Equal(now.Add(-availabilityQuoteUnconsumedGrace)) {
			t.Fatalf("batch %d unconsumed cutoff = %s, want %s", index+1, call.unconsumedExpiredBefore, now.Add(-availabilityQuoteUnconsumedGrace))
		}
		if !call.consumedBefore.Equal(now.Add(-availabilityQuoteConsumedAuditRetention)) {
			t.Fatalf("batch %d consumed cutoff = %s, want %s", index+1, call.consumedBefore, now.Add(-availabilityQuoteConsumedAuditRetention))
		}
		if call.limit != 250 {
			t.Fatalf("batch %d limit = %d, want fixed 250", index+1, call.limit)
		}
	}
}

func TestAvailabilityQuoteCleanupProcessorCapsBacklogDrainPerRun(t *testing.T) {
	store := &fakeAvailabilityQuoteCleanupStore{defaultResult: availabilityQuoteCleanupResult{deleted: maxAvailabilityQuoteCleanupLimit}}
	processor := NewAvailabilityQuoteCleanupProcessor(store)
	processor.now = func() time.Time { return time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC) }

	deleted, err := processor.ProcessOnce(context.Background(), maxAvailabilityQuoteCleanupLimit+1)
	if err != nil {
		t.Fatalf("process bounded backlog: %v", err)
	}
	wantDeleted := maxAvailabilityQuoteCleanupLimit * maxAvailabilityQuoteCleanupBatchesPerRun
	if deleted != wantDeleted {
		t.Fatalf("deleted = %d, want bounded %d", deleted, wantDeleted)
	}
	if len(store.calls) != maxAvailabilityQuoteCleanupBatchesPerRun {
		t.Fatalf("cleanup calls = %d, want max %d", len(store.calls), maxAvailabilityQuoteCleanupBatchesPerRun)
	}
	for index, call := range store.calls {
		if call.limit != maxAvailabilityQuoteCleanupLimit {
			t.Fatalf("batch %d limit = %d, want clamped %d", index+1, call.limit, maxAvailabilityQuoteCleanupLimit)
		}
	}
}

func TestAvailabilityQuoteCleanupProcessorPreservesNilErrorAndCancellationSemantics(t *testing.T) {
	if deleted, err := (*AvailabilityQuoteCleanupProcessor)(nil).ProcessOnce(context.Background(), 10); err != nil || deleted != 0 {
		t.Fatalf("nil processor result = %d/%v, want 0/nil", deleted, err)
	}

	wantErr := errors.New("cleanup unavailable")
	store := &fakeAvailabilityQuoteCleanupStore{results: []availabilityQuoteCleanupResult{
		{deleted: defaultAvailabilityQuoteCleanupLimit},
		{err: wantErr},
	}}
	processor := NewAvailabilityQuoteCleanupProcessor(store)
	processor.now = func() time.Time { return time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC) }
	deleted, err := processor.ProcessOnce(context.Background(), 0)
	if !errors.Is(err, wantErr) {
		t.Fatalf("cleanup error = %v, want %v", err, wantErr)
	}
	if deleted != defaultAvailabilityQuoteCleanupLimit {
		t.Fatalf("partial cleanup count = %d, want %d", deleted, defaultAvailabilityQuoteCleanupLimit)
	}
	if len(store.calls) != 2 || store.calls[0].limit != defaultAvailabilityQuoteCleanupLimit {
		t.Fatalf("error-path calls = %#v, want two default-sized attempts", store.calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelStore := &fakeAvailabilityQuoteCleanupStore{
		defaultResult: availabilityQuoteCleanupResult{deleted: 100},
		afterCall: func(call int) {
			if call == 1 {
				cancel()
			}
		},
	}
	cancelProcessor := NewAvailabilityQuoteCleanupProcessor(cancelStore)
	cancelProcessor.now = processor.now
	deleted, err = cancelProcessor.ProcessOnce(ctx, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cleanup error = %v, want context.Canceled", err)
	}
	if deleted != 100 || len(cancelStore.calls) != 1 {
		t.Fatalf("cancelled cleanup = %d deletions/%d calls, want 100/1", deleted, len(cancelStore.calls))
	}
}

type fakeAvailabilityQuoteCleanupStore struct {
	results       []availabilityQuoteCleanupResult
	defaultResult availabilityQuoteCleanupResult
	afterCall     func(call int)
	calls         []availabilityQuoteCleanupCall
}

type availabilityQuoteCleanupResult struct {
	deleted int
	err     error
}

type availabilityQuoteCleanupCall struct {
	unconsumedExpiredBefore time.Time
	consumedBefore          time.Time
	limit                   int
}

func (f *fakeAvailabilityQuoteCleanupStore) CleanupAvailabilityQuotes(_ context.Context, unconsumedExpiredBefore time.Time, consumedBefore time.Time, limit int) (int, error) {
	f.calls = append(f.calls, availabilityQuoteCleanupCall{
		unconsumedExpiredBefore: unconsumedExpiredBefore,
		consumedBefore:          consumedBefore,
		limit:                   limit,
	})
	call := len(f.calls)
	result := f.defaultResult
	if call <= len(f.results) {
		result = f.results[call-1]
	}
	if f.afterCall != nil {
		f.afterCall(call)
	}
	return result.deleted, result.err
}
