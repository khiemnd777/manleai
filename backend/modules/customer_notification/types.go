package customernotification

import (
	"context"
	"errors"
	"time"

	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

const (
	ConsentPending   = "pending"
	ConsentConsented = "consented"
	ConsentDeclined  = "declined"
	ConsentOptedOut  = "opted_out"

	ConsentSourceConversation = "conversation_explicit"
	ConsentSourceOwner        = "owner_attested"
	ConsentSourceTwilio       = "twilio_advanced_opt_out"

	ConsentEventRequested = "consent_requested"
	ConsentEventConsented = "consented"
	ConsentEventDeclined  = "declined"
	ConsentEventOptOut    = "opt_out"
	ConsentEventOptIn     = "opt_in"
	ConsentEventHelp      = "help"

	StatusQueued           = "queued"
	StatusQuietHours       = "quiet_hours"
	StatusDelivering       = "delivering"
	StatusProviderAccepted = "provider_accepted"
	StatusSent             = "sent"
	StatusDelivered        = "delivered"
	StatusFailed           = "failed"
	StatusUndelivered      = "undelivered"
	StatusDeadLetter       = "dead_letter"
	StatusSuppressed       = "suppressed"

	NotificationRequestReceived = "request_received"
	NotificationConfirmed       = "confirmed"
	NotificationRescheduled     = "rescheduled"
	NotificationCancelled       = "cancelled"

	MaxSafeDeliveryAttempts = 5
	MaxOwnerRequeues        = 1
	DeliveryLeaseDuration   = 2 * time.Minute
	SafeRetryBaseDelay      = 30 * time.Second
	DefaultProcessBatch     = 20
)

var (
	ErrValidation      = errors.New("customer notification validation failed")
	ErrNotFound        = errors.New("customer notification not found")
	ErrConflict        = errors.New("customer notification conflict")
	ErrClaimLost       = errors.New("customer notification claim lost")
	ErrDispatchBlocked = errors.New("customer notification dispatch blocked")
	ErrRequeueBlocked  = errors.New("customer notification cannot be requeued")
)

type Policy struct {
	Enabled    bool   `json:"enabled"`
	QuietStart string `json:"quiet_start,omitempty"`
	QuietEnd   string `json:"quiet_end,omitempty"`
	Timezone   string `json:"timezone"`
	Version    int64  `json:"version"`
	Ready      bool   `json:"ready"`
}

type UpdatePolicyRequest struct {
	Enabled         bool   `json:"enabled"`
	QuietStart      string `json:"quiet_start"`
	QuietEnd        string `json:"quiet_end"`
	ExpectedVersion int64  `json:"expected_version"`
}

type Consent struct {
	ID                string     `json:"id"`
	Status            string     `json:"status"`
	DestinationMasked string     `json:"destination_masked"`
	Version           int        `json:"version"`
	Source            string     `json:"source"`
	RequestedAt       *time.Time `json:"requested_at,omitempty"`
	ConsentedAt       *time.Time `json:"consented_at,omitempty"`
	DeclinedAt        *time.Time `json:"declined_at,omitempty"`
	OptedOutAt        *time.Time `json:"opted_out_at,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ConsentEvent struct {
	EventType string    `json:"event_type"`
	Version   int       `json:"version"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type RecordConversationConsentRequest struct {
	SalonID           string
	CallSessionID     string
	Destination       string
	Granted           bool
	EventKey          string
	EvidenceReference string
}

type AttestConsentRequest struct {
	Destination string `json:"destination"`
	ActionKey   string `json:"action_key"`
	Attested    bool   `json:"attested"`
}

type InboundOptOut struct {
	SalonID           string
	From              string
	To                string
	ConfiguredSender  string
	OptOutType        string
	ProviderMessageID string
	EventFingerprint  string
}

type ClaimedDelivery struct {
	ID                string
	SalonID           string
	ConsentID         string
	NotificationType  string
	Body              string
	Destination       string
	DestinationMasked string
	ConsentVersion    int
	PolicyVersion     int64
	ClaimToken        string
	AttemptNumber     int
	RequeueCount      int
}

type DispatchReadiness struct {
	Eligible   bool
	ReasonCode string
	Timezone   string
	QuietStart string
	QuietEnd   string
	Now        time.Time
}

type SenderResolver interface {
	ResolveCustomerSender(context.Context, string) (notificationdelivery.Sender, error)
}

type DeliveryEvent struct {
	EventType      string    `json:"event_type"`
	DeliveryStatus string    `json:"delivery_status"`
	ProviderStatus string    `json:"provider_status,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Delivery struct {
	ID                    string          `json:"id"`
	NotificationType      string          `json:"notification_type"`
	DeliveryStatus        string          `json:"delivery_status"`
	DestinationMasked     string          `json:"destination_masked"`
	DeliveryAttempts      int             `json:"delivery_attempts"`
	ProviderStatus        string          `json:"provider_status,omitempty"`
	LastDeliveryErrorCode string          `json:"last_delivery_error_code,omitempty"`
	CanRequeue            bool            `json:"can_requeue"`
	RequeueBlockedReason  string          `json:"requeue_blocked_reason,omitempty"`
	NextDeliveryAt        time.Time       `json:"next_delivery_at"`
	DeliveredAt           *time.Time      `json:"delivered_at,omitempty"`
	DeadLetteredAt        *time.Time      `json:"dead_lettered_at,omitempty"`
	Redacted              bool            `json:"redacted"`
	RedactedAt            *time.Time      `json:"redacted_at,omitempty"`
	RedactionVersion      int             `json:"redaction_version,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	Events                []DeliveryEvent `json:"events,omitempty"`
}

type Detail struct {
	Consent    *Consent   `json:"consent,omitempty"`
	Deliveries []Delivery `json:"deliveries"`
}

type RequeueRequest struct {
	ActionKey string `json:"action_key"`
}

type appointmentDetailRepository interface {
	DetailForAppointment(context.Context, string, string, string) (*Detail, error)
}

func retryDelay(attemptInCycle int) time.Duration {
	if attemptInCycle < 1 {
		attemptInCycle = 1
	}
	shift := attemptInCycle - 1
	if shift > 6 {
		shift = 6
	}
	return SafeRetryBaseDelay * time.Duration(1<<shift)
}

func attemptInCycle(item ClaimedDelivery) int {
	return item.AttemptNumber - item.RequeueCount*MaxSafeDeliveryAttempts
}
