package pos_square

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
)

const maxSquareWebhookBodyBytes = 1 << 20

var (
	ErrWebhookPayloadInvalid   = errors.New("square webhook payload is invalid")
	ErrWebhookSignatureInvalid = errors.New("square webhook signature is invalid")
	ErrWebhookConfigMissing    = errors.New("square webhook configuration is missing")
)

type SquareWebhookReceipt struct {
	Accepted  bool `json:"accepted"`
	Duplicate bool `json:"duplicate"`
}

type SquareWebhookStore interface {
	FindWebhookTarget(ctx context.Context, merchantID string, locationID string) (*SquareWebhookTarget, error)
	EnqueueBookingWebhook(ctx context.Context, event SquareBookingWebhookEvent) (bool, error)
}

type squareWebhookEnvelope struct {
	MerchantID string    `json:"merchant_id"`
	LocationID string    `json:"location_id"`
	Type       string    `json:"type"`
	EventID    string    `json:"event_id"`
	CreatedAt  time.Time `json:"created_at"`
	Data       struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Object struct {
			Booking squareBooking `json:"booking"`
		} `json:"object"`
	} `json:"data"`
}

func (s *Service) ReceiveBookingWebhook(ctx context.Context, rawBody []byte, signature string) (*SquareWebhookReceipt, error) {
	if s.webhookRepo == nil || s.adapter == nil {
		return nil, ErrWebhookConfigMissing
	}
	if len(rawBody) == 0 || len(rawBody) > maxSquareWebhookBodyBytes {
		return nil, ErrWebhookPayloadInvalid
	}
	var envelope squareWebhookEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return nil, ErrWebhookPayloadInvalid
	}
	envelope.MerchantID = strings.TrimSpace(envelope.MerchantID)
	rootLocationID := strings.TrimSpace(envelope.LocationID)
	bookingLocationID := strings.TrimSpace(envelope.Data.Object.Booking.LocationID)
	if rootLocationID != "" && bookingLocationID != "" && rootLocationID != bookingLocationID {
		return nil, ErrWebhookPayloadInvalid
	}
	envelope.LocationID = firstNonEmptySquareValue(rootLocationID, bookingLocationID)
	if envelope.MerchantID == "" || envelope.LocationID == "" {
		return nil, ErrWebhookPayloadInvalid
	}
	target, err := s.webhookRepo.FindWebhookTarget(ctx, envelope.MerchantID, envelope.LocationID)
	if err != nil {
		if errors.Is(err, ErrWebhookTargetNotFound) || errors.Is(err, ErrWebhookTargetAmbiguous) {
			return nil, ErrWebhookSignatureInvalid
		}
		return nil, err
	}
	ctx = databasecontext.WithSystemSalon(ctx, databasecontext.ScopeProvider, target.SalonID)
	cfg, err := s.adapter.configFor(ctx, target.SalonID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.WebhookNotificationURL) == "" || strings.TrimSpace(cfg.WebhookSignatureKey) == "" {
		return nil, ErrWebhookConfigMissing
	}
	if !verifySquareWebhookSignature(cfg.WebhookNotificationURL, cfg.WebhookSignatureKey, rawBody, signature) {
		return nil, ErrWebhookSignatureInvalid
	}

	eventType := strings.TrimSpace(envelope.Type)
	if eventType != "booking.created" && eventType != "booking.updated" {
		return &SquareWebhookReceipt{Accepted: true}, nil
	}
	booking := envelope.Data.Object.Booking
	bookingID := firstNonEmptySquareValue(booking.ID, envelope.Data.ID)
	eventID := strings.TrimSpace(envelope.EventID)
	if eventID == "" || bookingID == "" || envelope.Data.Type != "booking" {
		return nil, ErrWebhookPayloadInvalid
	}
	var bookingStartAt *time.Time
	if value := strings.TrimSpace(booking.StartAt); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, ErrWebhookPayloadInvalid
		}
		parsed = parsed.UTC()
		bookingStartAt = &parsed
	}
	var deliveredAt *time.Time
	if !envelope.CreatedAt.IsZero() {
		value := envelope.CreatedAt.UTC()
		deliveredAt = &value
	}
	version := booking.Version
	if version < 0 {
		version = 0
	}
	inserted, err := s.webhookRepo.EnqueueBookingWebhook(ctx, SquareBookingWebhookEvent{
		SalonID:           target.SalonID,
		EventID:           eventID,
		EventType:         eventType,
		MerchantID:        envelope.MerchantID,
		LocationID:        envelope.LocationID,
		POSBookingID:      bookingID,
		POSBookingVersion: version,
		BookingStatus:     strings.TrimSpace(booking.Status),
		BookingStartAt:    bookingStartAt,
		DeliveredAt:       deliveredAt,
	})
	if err != nil {
		return nil, err
	}
	return &SquareWebhookReceipt{Accepted: true, Duplicate: !inserted}, nil
}

func verifySquareWebhookSignature(notificationURL string, signatureKey string, rawBody []byte, signatureHeader string) bool {
	notificationURL = strings.TrimSpace(notificationURL)
	signatureKey = strings.TrimSpace(signatureKey)
	signatureHeader = strings.TrimSpace(signatureHeader)
	if notificationURL == "" || signatureKey == "" || signatureHeader == "" {
		return false
	}
	received, err := base64.StdEncoding.DecodeString(signatureHeader)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(signatureKey))
	_, _ = mac.Write([]byte(notificationURL))
	_, _ = mac.Write(rawBody)
	return hmac.Equal(mac.Sum(nil), received)
}

func firstNonEmptySquareValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
