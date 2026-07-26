package pos_square

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WebhookStatusPending    = "pending"
	WebhookStatusProcessing = "processing"
	WebhookStatusSucceeded  = "succeeded"
	WebhookStatusFailed     = "failed"
	WebhookStatusDeadLetter = "dead_letter"

	MaxWebhookAttemptsPerCycle = 10
	MaxWebhookRequeues         = 3
	WebhookRecentSuccessWindow = 7 * 24 * time.Hour
)

var (
	ErrWebhookOperationsValidation = errors.New("square webhook operations request is invalid")
	ErrWebhookEventNotFound        = errors.New("square webhook event was not found")
	ErrWebhookActionConflict       = errors.New("square webhook action conflicts with an existing action")
	ErrWebhookRequeueBlocked       = errors.New("square webhook event cannot be safely requeued")
)

type WebhookEventRecord struct {
	ID                   string     `json:"id"`
	EventType            string     `json:"event_type"`
	ProcessingStatus     string     `json:"processing_status"`
	ProcessingAttempts   int        `json:"processing_attempts"`
	RequeueCount         int        `json:"requeue_count"`
	LastErrorClass       string     `json:"last_error_class,omitempty"`
	LastErrorCode        string     `json:"last_error_code,omitempty"`
	CanRequeue           bool       `json:"can_requeue"`
	RequeueBlockedReason string     `json:"requeue_blocked_reason,omitempty"`
	NextAttemptAt        time.Time  `json:"next_attempt_at"`
	DeliveredAt          *time.Time `json:"delivered_at,omitempty"`
	ProcessedAt          *time.Time `json:"processed_at,omitempty"`
	DeadLetteredAt       *time.Time `json:"dead_lettered_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type WebhookMetrics struct {
	Pending           int64      `json:"pending"`
	Processing        int64      `json:"processing"`
	Failed            int64      `json:"failed"`
	DeadLetter        int64      `json:"dead_letter"`
	SucceededRecent   int64      `json:"succeeded_recent"`
	RecentWindowHours int        `json:"recent_window_hours"`
	LastDeliveredAt   *time.Time `json:"last_delivered_at,omitempty"`
	LastSucceededAt   *time.Time `json:"last_succeeded_at,omitempty"`
}

type CalendarRepairHealth struct {
	Relevant       bool       `json:"relevant"`
	Status         string     `json:"status"`
	RepairAttempts int        `json:"repair_attempts"`
	LastErrorClass string     `json:"last_error_class,omitempty"`
	LastErrorCode  string     `json:"last_error_code,omitempty"`
	NextRepairAt   *time.Time `json:"next_repair_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	LastRepairedAt *time.Time `json:"last_repaired_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type WebhookEventListResponse struct {
	Events         []WebhookEventRecord `json:"events"`
	Metrics        WebhookMetrics       `json:"metrics"`
	CalendarRepair CalendarRepairHealth `json:"calendar_repair"`
	Limit          int                  `json:"limit"`
	Offset         int                  `json:"offset"`
	HasMore        bool                 `json:"has_more"`
}

type WebhookEventDetailResponse struct {
	Event WebhookEventRecord `json:"event"`
}

type WebhookRequeueRequest struct {
	ActionKey string `json:"action_key"`
}

type SquareWebhookOperationsStore interface {
	ListBookingWebhooksForOwner(context.Context, string, string, string, int, int) ([]WebhookEventRecord, WebhookMetrics, CalendarRepairHealth, error)
	GetBookingWebhookForOwner(context.Context, string, string, string) (*WebhookEventRecord, error)
	RequeueBookingWebhookForOwner(context.Context, string, string, string, string, string) (*WebhookEventRecord, bool, error)
}

type PlatformSquareWebhookOperationsStore interface {
	ListBookingWebhooksForSalon(context.Context, string, string, int, int) ([]WebhookEventRecord, WebhookMetrics, CalendarRepairHealth, error)
	GetBookingWebhookForSalon(context.Context, string, string) (*WebhookEventRecord, error)
	RequeueBookingWebhookForSalon(context.Context, string, string, string, string, string) (*WebhookEventRecord, bool, error)
}

func (s *Service) ListWebhookEvents(ctx context.Context, salonID, ownerUserID, status string, limit, offset int) (*WebhookEventListResponse, error) {
	salonID, ownerUserID, status = strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(status)
	if s == nil || s.webhookOperationsRepo == nil || !validWebhookOperationUUID(salonID) || !validWebhookOperationUUID(ownerUserID) || !validWebhookStatusFilter(status) || limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrWebhookOperationsValidation
	}
	events, metrics, repair, err := s.webhookOperationsRepo.ListBookingWebhooksForOwner(ctx, salonID, ownerUserID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	for index := range events {
		setWebhookRequeueState(&events[index])
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	return &WebhookEventListResponse{
		Events: events, Metrics: metrics, CalendarRepair: repair,
		Limit: limit, Offset: offset, HasMore: hasMore,
	}, nil
}

func (s *Service) GetWebhookEvent(ctx context.Context, salonID, ownerUserID, eventID string) (*WebhookEventDetailResponse, error) {
	salonID, ownerUserID, eventID = strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(eventID)
	if s == nil || s.webhookOperationsRepo == nil || !validWebhookOperationUUID(salonID) || !validWebhookOperationUUID(ownerUserID) || !validWebhookOperationUUID(eventID) {
		return nil, ErrWebhookOperationsValidation
	}
	event, err := s.webhookOperationsRepo.GetBookingWebhookForOwner(ctx, salonID, ownerUserID, eventID)
	if err != nil {
		return nil, err
	}
	setWebhookRequeueState(event)
	return &WebhookEventDetailResponse{Event: *event}, nil
}

func (s *Service) RequeueWebhookEvent(ctx context.Context, salonID, ownerUserID, eventID string, req WebhookRequeueRequest) (*WebhookEventDetailResponse, bool, error) {
	salonID, ownerUserID, eventID = strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(eventID)
	actionKey := strings.TrimSpace(req.ActionKey)
	if s == nil || s.webhookOperationsRepo == nil || !validWebhookOperationUUID(salonID) || !validWebhookOperationUUID(ownerUserID) || !validWebhookOperationUUID(eventID) || actionKey == "" || len(actionKey) > 256 {
		return nil, false, ErrWebhookOperationsValidation
	}
	event, replayed, err := s.webhookOperationsRepo.RequeueBookingWebhookForOwner(ctx, salonID, ownerUserID, eventID, actionKey, webhookRequeueFingerprint(eventID))
	if err != nil {
		return nil, false, err
	}
	setWebhookRequeueState(event)
	return &WebhookEventDetailResponse{Event: *event}, replayed, nil
}

func (s *Service) ListWebhookEventsForPlatform(ctx context.Context, salonID, status string, limit, offset int) (*WebhookEventListResponse, error) {
	salonID, status = strings.TrimSpace(salonID), strings.TrimSpace(status)
	store, ok := s.webhookOperationsRepo.(PlatformSquareWebhookOperationsStore)
	if s == nil || !ok || !validWebhookOperationUUID(salonID) || !validWebhookStatusFilter(status) || limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrWebhookOperationsValidation
	}
	events, metrics, repair, err := store.ListBookingWebhooksForSalon(ctx, salonID, status, limit, offset)
	if err != nil {
		return nil, err
	}
	for index := range events {
		setWebhookRequeueState(&events[index])
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	return &WebhookEventListResponse{Events: events, Metrics: metrics, CalendarRepair: repair, Limit: limit, Offset: offset, HasMore: hasMore}, nil
}

func (s *Service) GetWebhookEventForPlatform(ctx context.Context, salonID, eventID string) (*WebhookEventDetailResponse, error) {
	salonID, eventID = strings.TrimSpace(salonID), strings.TrimSpace(eventID)
	store, ok := s.webhookOperationsRepo.(PlatformSquareWebhookOperationsStore)
	if s == nil || !ok || !validWebhookOperationUUID(salonID) || !validWebhookOperationUUID(eventID) {
		return nil, ErrWebhookOperationsValidation
	}
	event, err := store.GetBookingWebhookForSalon(ctx, salonID, eventID)
	if err != nil {
		return nil, err
	}
	setWebhookRequeueState(event)
	return &WebhookEventDetailResponse{Event: *event}, nil
}

func (s *Service) RequeueWebhookEventForPlatform(ctx context.Context, salonID, actorUserID, eventID string, req WebhookRequeueRequest) (*WebhookEventDetailResponse, bool, error) {
	salonID, actorUserID, eventID = strings.TrimSpace(salonID), strings.TrimSpace(actorUserID), strings.TrimSpace(eventID)
	actionKey := strings.TrimSpace(req.ActionKey)
	store, ok := s.webhookOperationsRepo.(PlatformSquareWebhookOperationsStore)
	if s == nil || !ok || !validWebhookOperationUUID(salonID) || !validWebhookOperationUUID(actorUserID) || !validWebhookOperationUUID(eventID) || actionKey == "" || len(actionKey) > 256 {
		return nil, false, ErrWebhookOperationsValidation
	}
	event, replayed, err := store.RequeueBookingWebhookForSalon(ctx, salonID, actorUserID, eventID, actionKey, webhookRequeueFingerprint(eventID))
	if err != nil {
		return nil, false, err
	}
	setWebhookRequeueState(event)
	return &WebhookEventDetailResponse{Event: *event}, replayed, nil
}

func validWebhookStatusFilter(status string) bool {
	switch status {
	case "", WebhookStatusPending, WebhookStatusProcessing, WebhookStatusSucceeded, WebhookStatusFailed, WebhookStatusDeadLetter:
		return true
	default:
		return false
	}
}

func validWebhookOperationUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

func webhookRequeueFingerprint(eventID string) string {
	sum := sha256.Sum256([]byte("square_webhook_requeue\x1f" + strings.TrimSpace(eventID)))
	return hex.EncodeToString(sum[:])
}

func setWebhookRequeueState(event *WebhookEventRecord) {
	if event == nil {
		return
	}
	terminalFailed := event.ProcessingStatus == WebhookStatusFailed && event.ProcessingAttempts >= (event.RequeueCount+1)*MaxWebhookAttemptsPerCycle
	event.CanRequeue = (event.ProcessingStatus == WebhookStatusDeadLetter || terminalFailed) &&
		event.LastErrorCode != "" && event.RequeueCount < MaxWebhookRequeues
	if event.CanRequeue {
		return
	}
	switch {
	case event.RequeueCount >= MaxWebhookRequeues:
		event.RequeueBlockedReason = "The bounded webhook requeue limit has been reached."
	case event.ProcessingStatus == WebhookStatusProcessing:
		event.RequeueBlockedReason = "Webhook processing currently owns this event."
	case event.ProcessingStatus != WebhookStatusDeadLetter && !terminalFailed:
		event.RequeueBlockedReason = "Only terminal webhook failures can be requeued."
	default:
		event.RequeueBlockedReason = "This webhook failure does not contain safe replay evidence."
	}
}

func webhookOperationalError(err error, operation string) (string, string) {
	if err == nil {
		return "", ""
	}
	prefix := "SQUARE_WEBHOOK"
	if operation == "calendar_repair" {
		prefix = "SQUARE_CALENDAR_REPAIR"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", prefix + "_TIMEOUT"
	case errors.Is(err, context.Canceled):
		return "cancellation", prefix + "_CANCELLED"
	default:
		return "dependency", prefix + "_FAILED"
	}
}
