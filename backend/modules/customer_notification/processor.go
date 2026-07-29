package customernotification

import (
	"context"
	"errors"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type Processor struct {
	repo     processorRepository
	resolver SenderResolver
	now      func() time.Time
}

type processorRepository interface {
	RecoverExpiredLeases(context.Context, int) (int, error)
	ClaimBatch(context.Context, int, time.Duration) ([]ClaimedDelivery, error)
	ResolveDispatchReadiness(context.Context, ClaimedDelivery) (DispatchReadiness, error)
	RecordSuppressed(context.Context, ClaimedDelivery, string) error
	RecordSafeFailure(context.Context, ClaimedDelivery, string, time.Time) error
	RecordQuietHours(context.Context, ClaimedDelivery, time.Time) error
	MarkDispatchStarted(context.Context, ClaimedDelivery) error
	RecordOutcomeUnknown(context.Context, ClaimedDelivery, string) error
	RecordDefinitiveFailure(context.Context, ClaimedDelivery, string) error
	RecordProviderResult(context.Context, ClaimedDelivery, notificationdelivery.SendResult) error
}

func NewProcessor(repo processorRepository, resolver SenderResolver) *Processor {
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
		readiness, err := p.repo.ResolveDispatchReadiness(itemCtx, item)
		if err != nil {
			return processed, err
		}
		if !readiness.Eligible {
			if err := p.repo.RecordSuppressed(itemCtx, item, readiness.ReasonCode); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		quietEnd, quiet, err := quietHoursEnd(readiness.Now, readiness.Timezone, readiness.QuietStart, readiness.QuietEnd)
		if err != nil {
			if err := p.repo.RecordSafeFailure(itemCtx, item, "CUSTOMER_SMS_POLICY_NOT_READY", p.now().Add(retryDelay(attemptInCycle(item)))); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		if quiet {
			if err := p.repo.RecordQuietHours(itemCtx, item, quietEnd); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		sender, resolveErr := p.resolver.ResolveCustomerSender(itemCtx, item.SalonID)
		if resolveErr != nil || sender == nil {
			code := "DELIVERY_CONFIG_NOT_READY"
			if errors.Is(resolveErr, notificationdelivery.ErrConfigDisabled) {
				code = "TWILIO_TRANSPORT_DISABLED"
			}
			if err := p.repo.RecordSafeFailure(itemCtx, item, code, p.now().Add(retryDelay(attemptInCycle(item)))); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		if err := p.repo.MarkDispatchStarted(itemCtx, item); err != nil {
			if errors.Is(err, ErrDispatchBlocked) {
				if suppressErr := p.repo.RecordSuppressed(itemCtx, item, "CUSTOMER_SMS_PRE_DISPATCH_REVALIDATION_FAILED"); suppressErr != nil {
					return processed, suppressErr
				}
				processed++
				continue
			}
			return processed, err
		}
		result, sendErr := sender.Send(itemCtx, notificationdelivery.OutboundMessage{
			NotificationID: item.ID, SalonID: item.SalonID, Destination: item.Destination, Body: item.Body,
		})
		if sendErr != nil {
			var classified *notificationdelivery.SendError
			if !errors.As(sendErr, &classified) || classified.Ambiguous {
				err = p.repo.RecordOutcomeUnknown(itemCtx, item, "DELIVERY_OUTCOME_UNKNOWN")
			} else if classified.Retryable {
				err = p.repo.RecordSafeFailure(itemCtx, item, classified.Code, p.now().Add(retryDelay(attemptInCycle(item))))
			} else {
				err = p.repo.RecordDefinitiveFailure(itemCtx, item, classified.Code)
			}
		} else {
			err = p.repo.RecordProviderResult(itemCtx, item, result)
		}
		if err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
