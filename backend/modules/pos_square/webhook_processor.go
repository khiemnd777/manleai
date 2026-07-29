package pos_square

import (
	"context"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/modules/booking"
)

const squareCalendarRepairTimeout = 2 * time.Minute

type SquareWebhookProcessorStore interface {
	ClaimBookingWebhooks(ctx context.Context, limit int) ([]SquareBookingWebhookEvent, error)
	CompleteBookingWebhook(ctx context.Context, id string, processingToken string, processingAttempts int, processingErr error) error
	ClaimCalendarRepairTargets(ctx context.Context, limit int) ([]SquareCalendarRepairTarget, error)
	CompleteCalendarRepair(ctx context.Context, salonID string, leaseToken string, repairErr error) error
}

type SquareCalendarSyncer interface {
	SyncCalendar(ctx context.Context, salonID string, ownerUserID string, req booking.CalendarSyncRequest) (*booking.CalendarSyncResponse, error)
}

type WebhookProcessor struct {
	store  SquareWebhookProcessorStore
	syncer SquareCalendarSyncer
	now    func() time.Time
}

func NewWebhookProcessor(store SquareWebhookProcessorStore, syncer SquareCalendarSyncer) *WebhookProcessor {
	return &WebhookProcessor{store: store, syncer: syncer, now: time.Now}
}

func (p *WebhookProcessor) ProcessWebhookEvents(ctx context.Context, limit int) (int, error) {
	if p == nil || p.store == nil || p.syncer == nil {
		return 0, ErrWebhookConfigMissing
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	processed := 0
	var joinedErr error
	for processed < limit {
		if err := ctx.Err(); err != nil {
			return processed, errors.Join(joinedErr, err)
		}
		events, err := p.store.ClaimBookingWebhooks(ctx, 1)
		if err != nil {
			return processed, errors.Join(joinedErr, err)
		}
		if len(events) == 0 {
			break
		}
		event := events[0]
		processed++
		itemCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeWorker, event.SalonID)
		startTime, endTime := webhookCalendarRepairRange(p.now().UTC(), event.BookingStartAt)
		repairCtx, cancel := context.WithTimeout(itemCtx, squareCalendarRepairTimeout)
		_, repairErr := p.syncer.SyncCalendar(repairCtx, event.SalonID, event.OwnerUserID, booking.CalendarSyncRequest{
			StartTime: startTime,
			EndTime:   endTime,
		})
		cancel()
		completeCtx, completeCancel := context.WithTimeout(context.WithoutCancel(itemCtx), 15*time.Second)
		completeErr := p.store.CompleteBookingWebhook(completeCtx, event.ID, event.ProcessingToken, event.ProcessingAttempts, repairErr)
		completeCancel()
		joinedErr = errors.Join(joinedErr, repairErr, completeErr)
	}
	return processed, joinedErr
}

func (p *WebhookProcessor) ProcessScheduledRepairs(ctx context.Context, limit int) (int, error) {
	if p == nil || p.store == nil || p.syncer == nil {
		return 0, ErrWebhookConfigMissing
	}
	targets, err := p.store.ClaimCalendarRepairTargets(ctx, limit)
	if err != nil {
		return 0, err
	}
	var joinedErr error
	for _, target := range targets {
		itemCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeWorker, target.SalonID)
		startTime, endTime := scheduledCalendarRepairRange(p.now().UTC())
		repairCtx, cancel := context.WithTimeout(itemCtx, squareCalendarRepairTimeout)
		_, repairErr := p.syncer.SyncCalendar(repairCtx, target.SalonID, target.OwnerUserID, booking.CalendarSyncRequest{
			StartTime: startTime,
			EndTime:   endTime,
		})
		cancel()
		completeCtx, completeCancel := context.WithTimeout(context.WithoutCancel(itemCtx), 15*time.Second)
		completeErr := p.store.CompleteCalendarRepair(completeCtx, target.SalonID, target.LeaseToken, repairErr)
		completeCancel()
		joinedErr = errors.Join(joinedErr, repairErr, completeErr)
	}
	return len(targets), joinedErr
}

func webhookCalendarRepairRange(now time.Time, bookingStartAt *time.Time) (time.Time, time.Time) {
	if bookingStartAt == nil || bookingStartAt.IsZero() {
		return scheduledCalendarRepairRange(now)
	}
	start := bookingStartAt.UTC()
	return start.Add(-24 * time.Hour), start.Add(24 * time.Hour)
}

func scheduledCalendarRepairRange(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	return now.Add(-24 * time.Hour), now.Add(90 * 24 * time.Hour)
}
