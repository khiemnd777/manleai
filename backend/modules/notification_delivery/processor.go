package notificationdelivery

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

type Processor struct {
	repo     processorRepository
	resolver ChannelResolver
	now      func() time.Time
}

type processorRepository interface {
	RecoverExpiredLeases(context.Context, int) (int, error)
	ClaimBatch(context.Context, int, time.Duration) ([]ClaimedNotification, error)
	RecordDisabled(context.Context, ClaimedNotification) error
	RecordSafeFailure(context.Context, ClaimedNotification, string, time.Time) error
	RecordOutcomeUnknown(context.Context, ClaimedNotification, string) error
	RecordDefinitiveFailure(context.Context, ClaimedNotification, string) error
	MarkDispatchStarted(context.Context, ClaimedNotification, string) error
	RecordProviderResult(context.Context, ClaimedNotification, SendResult) error
}

func NewProcessor(repo processorRepository, resolver ChannelResolver) *Processor {
	return &Processor{repo: repo, resolver: resolver, now: time.Now}
}

func (p *Processor) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if p == nil || p.repo == nil || p.resolver == nil || limit <= 0 {
		return 0, ErrValidation
	}
	if _, err := p.repo.RecoverExpiredLeases(ctx, limit); err != nil {
		return 0, err
	}
	items, err := p.repo.ClaimBatch(ctx, limit, DeliveryLeaseDuration)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, item := range items {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		itemCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeWorker, item.SalonID)
		channel, resolveErr := p.resolver.ResolveDeliveryChannel(itemCtx, item.SalonID)
		if errors.Is(resolveErr, ErrConfigDisabled) || (resolveErr == nil && !channel.Enabled) {
			if err := p.repo.RecordDisabled(itemCtx, item); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		if resolveErr != nil || channel.Sender == nil || strings.TrimSpace(channel.Destination) == "" {
			if err := p.repo.RecordSafeFailure(itemCtx, item, "DELIVERY_CONFIG_NOT_READY", p.now().Add(SafeRetryDelay(DeliveryAttemptInCycle(item)))); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		if err := p.repo.MarkDispatchStarted(itemCtx, item, channel.DestinationMasked); err != nil {
			return processed, err
		}
		result, sendErr := channel.Sender.Send(itemCtx, OutboundMessage{
			NotificationID: item.ID,
			SalonID:        item.SalonID,
			Destination:    channel.Destination,
			Body:           item.Message,
		})
		if sendErr != nil {
			var classified *SendError
			if !errors.As(sendErr, &classified) || classified.Ambiguous {
				code := "DELIVERY_OUTCOME_UNKNOWN"
				if classified != nil && strings.TrimSpace(classified.Code) != "" {
					code = classified.Code
				}
				if err := p.repo.RecordOutcomeUnknown(itemCtx, item, code); err != nil {
					return processed, err
				}
			} else if classified.Retryable {
				if err := p.repo.RecordSafeFailure(itemCtx, item, classified.Code, p.now().Add(SafeRetryDelay(DeliveryAttemptInCycle(item)))); err != nil {
					return processed, err
				}
			} else {
				if err := p.repo.RecordDefinitiveFailure(itemCtx, item, classified.Code); err != nil {
					return processed, err
				}
			}
			processed++
			continue
		}
		if err := p.repo.RecordProviderResult(itemCtx, item, result); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
