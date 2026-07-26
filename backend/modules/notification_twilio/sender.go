package notificationtwilio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

const maxTwilioResponseBytes = 64 * 1024

type Sender struct {
	cfg    integrationconfig.TwilioMessagingConfig
	client *http.Client
}

func NewSender(cfg integrationconfig.TwilioMessagingConfig, client *http.Client) *Sender {
	return &Sender{cfg: cfg, client: client}
}

func (s *Sender) Send(ctx context.Context, message notificationdelivery.OutboundMessage) (notificationdelivery.SendResult, error) {
	if s == nil || s.client == nil || strings.TrimSpace(message.Destination) == "" || strings.TrimSpace(message.Body) == "" {
		return notificationdelivery.SendResult{}, &notificationdelivery.SendError{Code: "DELIVERY_REQUEST_INVALID", Retryable: true, Err: errors.New("outbound message is incomplete")}
	}
	endpoint := "https://api.twilio.com/2010-04-01/Accounts/" + url.PathEscape(s.cfg.AccountSID) + "/Messages.json"
	form := url.Values{
		"To":             []string{message.Destination},
		"Body":           []string{message.Body},
		"StatusCallback": []string{s.cfg.StatusCallbackURL},
	}
	if strings.TrimSpace(s.cfg.MessagingServiceSID) != "" {
		form.Set("MessagingServiceSid", s.cfg.MessagingServiceSID)
	} else {
		form.Set("From", s.cfg.SenderPhone)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return notificationdelivery.SendResult{}, &notificationdelivery.SendError{Code: "DELIVERY_REQUEST_BUILD_FAILED", Retryable: true, Err: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.cfg.AccountSID, s.cfg.AuthToken)
	res, err := s.client.Do(req)
	if err != nil {
		return notificationdelivery.SendResult{}, &notificationdelivery.SendError{Code: "DELIVERY_OUTCOME_UNKNOWN", Ambiguous: true, Err: err}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxTwilioResponseBytes))
	if err != nil {
		return notificationdelivery.SendResult{}, &notificationdelivery.SendError{Code: "DELIVERY_OUTCOME_UNKNOWN", Ambiguous: true, Err: err}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return notificationdelivery.SendResult{}, &notificationdelivery.SendError{
			Code: "TWILIO_MESSAGE_REJECTED", Retryable: false,
			Err: fmt.Errorf("twilio messages api returned status %d", res.StatusCode),
		}
	}
	var payload struct {
		SID    string `json:"sid"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || strings.TrimSpace(payload.SID) == "" {
		return notificationdelivery.SendResult{}, &notificationdelivery.SendError{Code: "DELIVERY_OUTCOME_UNKNOWN", Ambiguous: true, Err: errors.New("twilio accepted response has no durable message sid")}
	}
	deliveryStatus, rank, known := mapTwilioStatus(payload.Status)
	if !known {
		deliveryStatus, rank = notificationdelivery.StatusProviderAccepted, 10
	}
	return notificationdelivery.SendResult{
		ProviderMessageID: strings.TrimSpace(payload.SID),
		ProviderStatus:    strings.ToLower(strings.TrimSpace(payload.Status)),
		DeliveryStatus:    deliveryStatus,
		StatusRank:        rank,
	}, nil
}

func mapTwilioStatus(status string) (deliveryStatus string, rank int, known bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "accepted", "scheduled", "queued":
		return notificationdelivery.StatusProviderAccepted, 10, true
	case "sending":
		return notificationdelivery.StatusProviderAccepted, 20, true
	case "sent":
		return notificationdelivery.StatusSent, 30, true
	case "delivered", "read":
		return notificationdelivery.StatusDelivered, 50, true
	case "failed", "undelivered", "canceled":
		return notificationdelivery.StatusDeadLetter, 50, true
	default:
		return notificationdelivery.StatusProviderAccepted, 5, false
	}
}
