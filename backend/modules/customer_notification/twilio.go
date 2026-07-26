package customernotification

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
	notificationtwilio "github.com/manleai/ai-receptionist/modules/notification_twilio"
)

type twilioCustomerConfigResolver interface {
	ResolveTwilioCustomerMessagingConfig(context.Context, string) (integrationconfig.TwilioMessagingConfig, error)
}

type TwilioSenderResolver struct {
	configs twilioCustomerConfigResolver
	client  *http.Client
}

func NewTwilioSenderResolver(configs twilioCustomerConfigResolver) *TwilioSenderResolver {
	return &TwilioSenderResolver{configs: configs, client: &http.Client{Timeout: 15 * time.Second}}
}

func (r *TwilioSenderResolver) ResolveCustomerSender(ctx context.Context, salonID string) (notificationdelivery.Sender, error) {
	if r == nil || r.configs == nil || strings.TrimSpace(salonID) == "" {
		return nil, notificationdelivery.ErrConfigNotReady
	}
	cfg, err := r.configs.ResolveTwilioCustomerMessagingConfig(ctx, salonID)
	if err != nil {
		if errors.Is(err, integrationconfig.ErrNotFound) || errors.Is(err, integrationconfig.ErrValidation) {
			return nil, notificationdelivery.ErrConfigNotReady
		}
		return nil, err
	}
	if !cfg.Enabled {
		return nil, notificationdelivery.ErrConfigDisabled
	}
	return notificationtwilio.NewSender(cfg, r.client), nil
}

type CallbackMultiplexer struct {
	owner    callbackService
	customer callbackService
}

type callbackService interface {
	SalonIDForProviderMessage(context.Context, string, string) (string, error)
	ApplyProviderCallback(context.Context, notificationdelivery.ProviderCallback) error
}

func NewCallbackMultiplexer(owner, customer callbackService) *CallbackMultiplexer {
	return &CallbackMultiplexer{owner: owner, customer: customer}
}

func (m *CallbackMultiplexer) SalonIDForProviderMessage(ctx context.Context, provider, providerMessageID string) (string, error) {
	if m != nil && m.owner != nil {
		if salonID, err := m.owner.SalonIDForProviderMessage(ctx, provider, providerMessageID); err == nil {
			return salonID, nil
		} else if !errors.Is(err, notificationdelivery.ErrNotFound) {
			return "", err
		}
	}
	if m == nil || m.customer == nil {
		return "", notificationdelivery.ErrNotFound
	}
	return m.customer.SalonIDForProviderMessage(ctx, provider, providerMessageID)
}

func (m *CallbackMultiplexer) ApplyProviderCallback(ctx context.Context, callback notificationdelivery.ProviderCallback) error {
	if m != nil && m.owner != nil {
		if err := m.owner.ApplyProviderCallback(ctx, callback); err == nil {
			return nil
		} else if !errors.Is(err, notificationdelivery.ErrNotFound) {
			return err
		}
	}
	if m == nil || m.customer == nil {
		return notificationdelivery.ErrNotFound
	}
	return m.customer.ApplyProviderCallback(ctx, callback)
}
