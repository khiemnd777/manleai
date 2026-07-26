package notificationdelivery

import (
	"context"
	"errors"
	"time"
)

const (
	ProviderTwilio = "twilio"

	StatusQueued           = "queued"
	StatusDelivering       = "delivering"
	StatusProviderAccepted = "provider_accepted"
	StatusSent             = "sent"
	StatusDelivered        = "delivered"
	StatusFailed           = "failed"
	StatusUndelivered      = "undelivered"
	StatusDeadLetter       = "dead_letter"
	StatusDisabled         = "disabled"

	// These are delivery-engine protocol bounds. They are intentionally owned
	// here, not salon policy: changing them is an operational release change.
	MaxSafeDeliveryAttempts = 5
	DeliveryLeaseDuration   = 2 * time.Minute
	SafeRetryBaseDelay      = 30 * time.Second
	DefaultProcessBatch     = 20
)

var (
	ErrNotFound       = errors.New("owner notification delivery not found")
	ErrValidation     = errors.New("owner notification delivery validation failed")
	ErrConflict       = errors.New("owner notification delivery conflict")
	ErrRequeueBlocked = errors.New("owner notification delivery cannot be requeued")
	ErrClaimLost      = errors.New("owner notification delivery claim lost")
	ErrConfigDisabled = errors.New("owner notification delivery disabled")
	ErrConfigNotReady = errors.New("owner notification delivery config not ready")
	ErrOutcomeUnknown = errors.New("owner notification delivery outcome unknown")
)

type ClaimedNotification struct {
	ID            string
	SalonID       string
	Type          string
	Message       string
	ClaimToken    string
	AttemptNumber int
	RequeueCount  int
	CreatedAt     time.Time
}

type DeliveryChannel struct {
	Enabled           bool
	Provider          string
	Destination       string
	DestinationMasked string
	Sender            Sender
}

type ChannelResolver interface {
	ResolveDeliveryChannel(ctx context.Context, salonID string) (DeliveryChannel, error)
}

type OutboundMessage struct {
	NotificationID string
	SalonID        string
	Destination    string
	Body           string
}

type SendResult struct {
	ProviderMessageID string
	ProviderStatus    string
	DeliveryStatus    string
	StatusRank        int
}

type SendError struct {
	Code      string
	Retryable bool
	Ambiguous bool
	Err       error
}

func (e *SendError) Error() string {
	if e == nil || e.Err == nil {
		return "delivery send failed"
	}
	return e.Err.Error()
}

func (e *SendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Sender interface {
	Send(ctx context.Context, message OutboundMessage) (SendResult, error)
}

type DeliveryEvent struct {
	ID             string    `json:"id"`
	EventType      string    `json:"event_type"`
	DeliveryStatus string    `json:"delivery_status"`
	ProviderStatus string    `json:"provider_status,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type DeliveryRecord struct {
	ID                    string          `json:"id"`
	SalonID               string          `json:"salon_id"`
	NotificationType      string          `json:"notification_type"`
	InAppStatus           string          `json:"in_app_status"`
	DeliveryStatus        string          `json:"delivery_status"`
	DeliveryProvider      string          `json:"delivery_provider,omitempty"`
	DestinationMasked     string          `json:"destination_masked,omitempty"`
	DeliveryAttempts      int             `json:"delivery_attempts"`
	ProviderStatus        string          `json:"provider_status,omitempty"`
	LastDeliveryErrorCode string          `json:"last_delivery_error_code,omitempty"`
	CanRequeue            bool            `json:"can_requeue"`
	RequeueBlockedReason  string          `json:"requeue_blocked_reason,omitempty"`
	NextDeliveryAt        time.Time       `json:"next_delivery_at"`
	DeliveredAt           *time.Time      `json:"delivered_at,omitempty"`
	DeadLetteredAt        *time.Time      `json:"dead_lettered_at,omitempty"`
	LastProviderEventAt   *time.Time      `json:"last_provider_event_at,omitempty"`
	Redacted              bool            `json:"redacted"`
	RedactedAt            *time.Time      `json:"redacted_at,omitempty"`
	RedactionVersion      int             `json:"redaction_version,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	Events                []DeliveryEvent `json:"events,omitempty"`
}

type DeliveryMetrics struct {
	Queued           int `json:"queued"`
	Delivering       int `json:"delivering"`
	ProviderAccepted int `json:"provider_accepted"`
	Sent             int `json:"sent"`
	Delivered        int `json:"delivered"`
	DeadLetter       int `json:"dead_letter"`
	Disabled         int `json:"disabled"`
}

type ListResponse struct {
	Deliveries []DeliveryRecord `json:"deliveries"`
	Metrics    DeliveryMetrics  `json:"metrics"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
	HasMore    bool             `json:"has_more"`
}

type DetailResponse struct {
	Delivery DeliveryRecord `json:"delivery"`
}

type RequeueRequest struct {
	ActionKey string `json:"action_key"`
}

type ProviderCallback struct {
	Provider          string
	ProviderMessageID string
	ProviderStatus    string
	StatusRank        int
	DeliveryStatus    string
	EventKey          string
	EventFingerprint  string
	ErrorCode         string
	OccurredAt        time.Time
}
