package notificationtwilio

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type messagingConfigResolver interface {
	ResolveTwilioMessagingConfig(ctx context.Context, salonID string) (integrationconfig.TwilioMessagingConfig, error)
}

type Resolver struct {
	configs messagingConfigResolver
	client  *http.Client
}

func NewResolver(configs messagingConfigResolver) *Resolver {
	return &Resolver{configs: configs, client: &http.Client{Timeout: 15 * time.Second}}
}

func (r *Resolver) ResolveDeliveryChannel(ctx context.Context, salonID string) (notificationdelivery.DeliveryChannel, error) {
	if r == nil || r.configs == nil || strings.TrimSpace(salonID) == "" {
		return notificationdelivery.DeliveryChannel{}, notificationdelivery.ErrConfigNotReady
	}
	cfg, err := r.configs.ResolveTwilioMessagingConfig(ctx, salonID)
	if err != nil {
		if errors.Is(err, integrationconfig.ErrNotFound) || errors.Is(err, integrationconfig.ErrValidation) {
			return notificationdelivery.DeliveryChannel{}, notificationdelivery.ErrConfigNotReady
		}
		return notificationdelivery.DeliveryChannel{}, err
	}
	if !cfg.Enabled {
		return notificationdelivery.DeliveryChannel{}, notificationdelivery.ErrConfigDisabled
	}
	return notificationdelivery.DeliveryChannel{
		Enabled:           true,
		Provider:          notificationdelivery.ProviderTwilio,
		Destination:       cfg.OwnerSMSDestination,
		DestinationMasked: maskPhone(cfg.OwnerSMSDestination),
		Sender:            NewSender(cfg, r.client),
	}, nil
}

func maskPhone(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return ""
	}
	return "••••" + value[len(value)-4:]
}
