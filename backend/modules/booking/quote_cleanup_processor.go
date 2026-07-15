package booking

import (
	"context"
	"time"
)

const (
	availabilityQuoteUnconsumedGrace         = 24 * time.Hour
	availabilityQuoteConsumedAuditRetention  = 30 * 24 * time.Hour
	defaultAvailabilityQuoteCleanupLimit     = 100
	maxAvailabilityQuoteCleanupLimit         = 500
	maxAvailabilityQuoteCleanupBatchesPerRun = 8
)

type AvailabilityQuoteCleanupStore interface {
	CleanupAvailabilityQuotes(ctx context.Context, unconsumedExpiredBefore time.Time, consumedBefore time.Time, limit int) (int, error)
}

type AvailabilityQuoteCleanupProcessor struct {
	store AvailabilityQuoteCleanupStore
	now   func() time.Time
}

func NewAvailabilityQuoteCleanupProcessor(store AvailabilityQuoteCleanupStore) *AvailabilityQuoteCleanupProcessor {
	return &AvailabilityQuoteCleanupProcessor{
		store: store,
		now:   time.Now,
	}
}

func (p *AvailabilityQuoteCleanupProcessor) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if p == nil || p.store == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	if p.now != nil {
		now = p.now().UTC()
	}
	unconsumedExpiredBefore := now.Add(-availabilityQuoteUnconsumedGrace)
	consumedBefore := now.Add(-availabilityQuoteConsumedAuditRetention)
	batchLimit := clampAvailabilityQuoteCleanupLimit(limit)
	totalDeleted := 0
	for batch := 0; batch < maxAvailabilityQuoteCleanupBatchesPerRun; batch++ {
		if err := ctx.Err(); err != nil {
			return totalDeleted, err
		}
		deleted, err := p.store.CleanupAvailabilityQuotes(
			ctx,
			unconsumedExpiredBefore,
			consumedBefore,
			batchLimit,
		)
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += deleted
		if deleted < batchLimit {
			break
		}
	}
	return totalDeleted, nil
}

func clampAvailabilityQuoteCleanupLimit(limit int) int {
	if limit <= 0 {
		return defaultAvailabilityQuoteCleanupLimit
	}
	if limit > maxAvailabilityQuoteCleanupLimit {
		return maxAvailabilityQuoteCleanupLimit
	}
	return limit
}
