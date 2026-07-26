package notificationtwilio

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSenderTreatsQueuedAsProviderAcceptedNotDelivered(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.Host; got != "api.twilio.com" {
			t.Fatalf("host=%q", got)
		}
		if user, pass, ok := req.BasicAuth(); !ok || user != "AC1234567890" || pass != "token" {
			t.Fatalf("basic auth user=%q pass=%q ok=%t", user, pass, ok)
		}
		body, _ := io.ReadAll(req.Body)
		form := string(body)
		for _, required := range []string{"To=%2B15555550100", "MessagingServiceSid=MG1234567890", "StatusCallback=https%3A%2F%2Fapi.example.com%2Fapi%2Fnotifications%2Ftwilio%2Fstatus"} {
			if !strings.Contains(form, required) {
				t.Fatalf("form missing %q: %s", required, form)
			}
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"sid":"SM123","status":"queued"}`)), Header: make(http.Header)}, nil
	})}
	sender := NewSender(integrationconfig.TwilioMessagingConfig{AccountSID: "AC1234567890", AuthToken: "token", MessagingServiceSID: "MG1234567890", StatusCallbackURL: "https://api.example.com/api/notifications/twilio/status"}, client)
	result, err := sender.Send(context.Background(), notificationdelivery.OutboundMessage{Destination: "+15555550100", Body: "Owner message"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.DeliveryStatus != notificationdelivery.StatusProviderAccepted || result.ProviderMessageID != "SM123" {
		t.Fatalf("result=%#v", result)
	}
}

func TestSenderClassifiesNetworkFailureAsAmbiguous(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })}
	sender := NewSender(integrationconfig.TwilioMessagingConfig{AccountSID: "AC1234567890", AuthToken: "token", SenderPhone: "+15555550000", StatusCallbackURL: "https://api.example.com/status"}, client)
	_, err := sender.Send(context.Background(), notificationdelivery.OutboundMessage{Destination: "+15555550100", Body: "Owner message"})
	classified, ok := err.(*notificationdelivery.SendError)
	if !ok || !classified.Ambiguous || classified.Retryable {
		t.Fatalf("error=%#v", err)
	}
}

func TestMapTwilioStatusIsMonotonicByRank(t *testing.T) {
	status, rank, known := mapTwilioStatus("delivered")
	if !known || status != notificationdelivery.StatusDelivered || rank != 50 {
		t.Fatalf("delivered mapping=%s/%d/%t", status, rank, known)
	}
	status, rank, known = mapTwilioStatus("sent")
	if !known || status != notificationdelivery.StatusSent || rank != 30 {
		t.Fatalf("sent mapping=%s/%d/%t", status, rank, known)
	}
}
