package notificationtwilio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/respond"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type callbackConfigResolver interface {
	ResolveTwilioMessagingConfig(ctx context.Context, salonID string) (integrationconfig.TwilioMessagingConfig, error)
}

type customerCallbackConfigResolver interface {
	ResolveTwilioCustomerMessagingConfig(ctx context.Context, salonID string) (integrationconfig.TwilioMessagingConfig, error)
}

type callbackService interface {
	SalonIDForProviderMessage(ctx context.Context, provider, providerMessageID string) (string, error)
	ApplyProviderCallback(ctx context.Context, callback notificationdelivery.ProviderCallback) error
}

type InboundOptOutConsumer interface {
	ApplyInboundOptOut(
		ctx context.Context,
		salonID, from, to, configuredSender, optOutType, providerMessageID, eventFingerprint string,
	) error
}

type Handler struct {
	service callbackService
	configs callbackConfigResolver
	inbound InboundOptOutConsumer
	now     func() time.Time
}

func NewHandler(service callbackService, configs callbackConfigResolver, inbound ...InboundOptOutConsumer) *Handler {
	handler := &Handler{service: service, configs: configs, now: time.Now}
	if len(inbound) > 0 {
		handler.inbound = inbound[0]
	}
	return handler
}

func (h *Handler) Status(c *fiber.Ctx) error {
	params := formParams(c)
	messageSID := strings.TrimSpace(params["MessageSid"])
	if messageSID == "" {
		messageSID = strings.TrimSpace(params["SmsSid"])
	}
	if messageSID == "" {
		return invalidCallback(c)
	}
	salonID, err := h.service.SalonIDForProviderMessage(c.UserContext(), notificationdelivery.ProviderTwilio, messageSID)
	if err != nil {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio callback could not be authenticated.")
	}
	ctx := databasecontext.WithSystemSalon(c.UserContext(), databasecontext.ScopeProvider, salonID)
	cfg, err := h.resolveCallbackConfig(ctx, salonID)
	if err != nil || !verifySignature(cfg.AuthToken, cfg.StatusCallbackURL, params, c.Get("X-Twilio-Signature")) ||
		!twilioAccountMatches(cfg, params) {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio callback signature is invalid.")
	}
	providerStatus := strings.ToLower(strings.TrimSpace(params["MessageStatus"]))
	if providerStatus == "" {
		providerStatus = strings.ToLower(strings.TrimSpace(params["SmsStatus"]))
	}
	deliveryStatus, rank, _ := mapTwilioStatus(providerStatus)
	errorCode := sanitizeProviderCode(params["ErrorCode"])
	if deliveryStatus == notificationdelivery.StatusDeadLetter && errorCode == "" {
		errorCode = "TWILIO_DELIVERY_FAILED"
	}
	fingerprint := paramsFingerprint(params)
	eventIdentity := strings.TrimSpace(params["EventSid"])
	if eventIdentity == "" {
		eventIdentity = messageSID + ":" + providerStatus + ":" + fingerprint
	}
	err = h.service.ApplyProviderCallback(ctx, notificationdelivery.ProviderCallback{
		Provider: notificationdelivery.ProviderTwilio, ProviderMessageID: messageSID,
		ProviderStatus: providerStatus, StatusRank: rank, DeliveryStatus: deliveryStatus,
		EventKey: "twilio-status:" + eventIdentity, EventFingerprint: fingerprint,
		ErrorCode: errorCode, OccurredAt: h.now().UTC(),
	})
	if errors.Is(err, notificationdelivery.ErrConflict) {
		return respond.Error(c, fiber.StatusConflict, "TWILIO_CALLBACK_CONFLICT", "Twilio callback identity conflicts with prior data.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_CALLBACK_FAILED", "Could not record Twilio callback.")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) Inbound(c *fiber.Ctx) error {
	params := formParams(c)
	salonID := strings.TrimSpace(c.Params("salon_id"))
	ctx := databasecontext.WithSystemSalon(c.UserContext(), databasecontext.ScopeProvider, salonID)
	cfg, err := h.resolveCallbackConfig(ctx, salonID)
	if err != nil || !verifySignature(cfg.AuthToken, cfg.InboundCallbackURL, params, c.Get("X-Twilio-Signature")) ||
		!twilioAccountMatches(cfg, params) || !twilioInboundTransportMatches(cfg, params) {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio callback signature is invalid.")
	}
	// Twilio Advanced Opt-Out has already interpreted the message and sent the
	// provider-owned reply. Never inspect Body and never send a second reply.
	optOutType := strings.ToUpper(strings.TrimSpace(params["OptOutType"]))
	if h.inbound == nil || (optOutType != "START" && optOutType != "STOP" && optOutType != "HELP") {
		return c.SendStatus(fiber.StatusNoContent)
	}
	messageSID := strings.TrimSpace(params["MessageSid"])
	if messageSID == "" {
		messageSID = strings.TrimSpace(params["SmsSid"])
	}
	from, to := strings.TrimSpace(params["From"]), strings.TrimSpace(params["To"])
	if messageSID == "" || from == "" || to == "" {
		return invalidCallback(c)
	}
	if err := h.inbound.ApplyInboundOptOut(
		ctx, salonID, from, to, strings.TrimSpace(cfg.SenderPhone),
		optOutType, messageSID, paramsFingerprint(params),
	); err != nil {
		if errors.Is(err, notificationdelivery.ErrConflict) {
			return respond.Error(c, fiber.StatusConflict, "TWILIO_CALLBACK_CONFLICT", "Twilio callback identity conflicts with prior data.")
		}
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_CALLBACK_FAILED", "Could not record Twilio callback.")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func twilioAccountMatches(cfg integrationconfig.TwilioMessagingConfig, params map[string]string) bool {
	return strings.TrimSpace(cfg.AccountSID) != "" && strings.TrimSpace(params["AccountSid"]) == strings.TrimSpace(cfg.AccountSID)
}

func twilioInboundTransportMatches(cfg integrationconfig.TwilioMessagingConfig, params map[string]string) bool {
	if serviceSID := strings.TrimSpace(cfg.MessagingServiceSID); serviceSID != "" {
		return strings.TrimSpace(params["MessagingServiceSid"]) == serviceSID
	}
	sender := strings.TrimSpace(cfg.SenderPhone)
	return sender != "" && strings.TrimSpace(params["To"]) == sender
}

func (h *Handler) resolveCallbackConfig(ctx context.Context, salonID string) (integrationconfig.TwilioMessagingConfig, error) {
	if customerConfigs, ok := h.configs.(customerCallbackConfigResolver); ok {
		return customerConfigs.ResolveTwilioCustomerMessagingConfig(ctx, salonID)
	}
	return h.configs.ResolveTwilioMessagingConfig(ctx, salonID)
}

func formParams(c *fiber.Ctx) map[string]string {
	params := map[string]string{}
	c.Request().PostArgs().VisitAll(func(key, value []byte) { params[string(key)] = string(value) })
	return params
}

func paramsFingerprint(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(0)
		b.WriteString(params[key])
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func sanitizeProviderCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 16 {
		return ""
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return "TWILIO_" + value
}

func invalidCallback(c *fiber.Ctx) error {
	return respond.Error(c, fiber.StatusBadRequest, "TWILIO_CALLBACK_INVALID", "Twilio callback is invalid.")
}
