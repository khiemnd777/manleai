package notificationtwilio

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type fakeCallbackService struct {
	callback notificationdelivery.ProviderCallback
	calls    int
	access   databasecontext.AccessContext
}

func (*fakeCallbackService) SalonIDForProviderMessage(context.Context, string, string) (string, error) {
	return "salon-1", nil
}
func (f *fakeCallbackService) ApplyProviderCallback(ctx context.Context, callback notificationdelivery.ProviderCallback) error {
	f.calls++
	f.callback = callback
	f.access = databasecontext.FromContext(ctx)
	return nil
}

type fakeCallbackConfigResolver struct {
	cfg integrationconfig.TwilioMessagingConfig
}

type fakeInboundOptOutConsumer struct {
	calls      int
	optOutType string
	from       string
	to         string
	sender     string
	messageID  string
}

func (f *fakeInboundOptOutConsumer) ApplyInboundOptOut(_ context.Context, _ string, from, to, sender, optOutType, messageID, _ string) error {
	f.calls++
	f.optOutType, f.from, f.to, f.sender, f.messageID = optOutType, from, to, sender, messageID
	return nil
}

func (f fakeCallbackConfigResolver) ResolveTwilioMessagingConfig(context.Context, string) (integrationconfig.TwilioMessagingConfig, error) {
	return f.cfg, nil
}

func TestStatusCallbackVerifiesAllEvolvingParamsAndMapsDelivered(t *testing.T) {
	const callbackURL = "https://api.example.com/api/notifications/twilio/status"
	const token = "auth-token"
	service := &fakeCallbackService{}
	handler := NewHandler(service, fakeCallbackConfigResolver{cfg: integrationconfig.TwilioMessagingConfig{AccountSID: "AC123", AuthToken: token, StatusCallbackURL: callbackURL}})
	app := fiber.New()
	app.Post("/api/notifications/twilio/status", handler.Status)
	form := url.Values{"AccountSid": {"AC123"}, "MessageSid": {"SM123"}, "MessageStatus": {"delivered"}, "FutureParameter": {"future-value"}}
	params := map[string]string{"AccountSid": "AC123", "MessageSid": "SM123", "MessageStatus": "delivered", "FutureParameter": "future-value"}
	request := httptest.NewRequest("POST", "/api/notifications/twilio/status", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Twilio-Signature", expectedSignatureForTest(token, callbackURL, params))
	response, err := app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status=%d err=%v", response.StatusCode, err)
	}
	if service.calls != 1 || service.callback.DeliveryStatus != notificationdelivery.StatusDelivered || service.callback.StatusRank != 50 {
		t.Fatalf("callback=%#v calls=%d", service.callback, service.calls)
	}
	if service.access.Scope != databasecontext.ScopeProvider || service.access.SystemSalonID != "salon-1" || service.access.ActorUserID != "" {
		t.Fatalf("callback access context = %#v", service.access)
	}
}

func TestStatusCallbackRejectsInvalidSignatureBeforeMutation(t *testing.T) {
	service := &fakeCallbackService{}
	handler := NewHandler(service, fakeCallbackConfigResolver{cfg: integrationconfig.TwilioMessagingConfig{AccountSID: "AC123", AuthToken: "token", StatusCallbackURL: "https://api.example.com/status"}})
	app := fiber.New()
	app.Post("/api/notifications/twilio/status", handler.Status)
	request := httptest.NewRequest("POST", "/api/notifications/twilio/status", strings.NewReader("MessageSid=SM123&MessageStatus=sent"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Twilio-Signature", "invalid")
	response, err := app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusForbidden || service.calls != 0 {
		t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, service.calls)
	}
}

func TestInboundUsesSignedAdvancedOptOutTypeAndIgnoresBody(t *testing.T) {
	const callbackURL = "https://api.example.com/api/notifications/twilio/inbound/salon-1"
	const token = "auth-token"
	consumer := &fakeInboundOptOutConsumer{}
	handler := NewHandler(&fakeCallbackService{}, fakeCallbackConfigResolver{cfg: integrationconfig.TwilioMessagingConfig{
		AccountSID: "AC123", AuthToken: token, InboundCallbackURL: callbackURL, SenderPhone: "+13125550100",
	}}, consumer)
	app := fiber.New()
	app.Post("/api/notifications/twilio/inbound/:salon_id", handler.Inbound)
	form := url.Values{
		"AccountSid": {"AC123"}, "MessageSid": {"SMSTOP1"}, "From": {"+13125550123"}, "To": {"+13125550100"},
		"OptOutType": {"STOP"}, "Body": {"This body must never be parsed as consent"},
	}
	params := map[string]string{}
	for key := range form {
		params[key] = form.Get(key)
	}
	request := httptest.NewRequest("POST", "/api/notifications/twilio/inbound/salon-1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Twilio-Signature", expectedSignatureForTest(token, callbackURL, params))
	response, err := app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusNoContent || consumer.calls != 1 || consumer.optOutType != "STOP" || consumer.messageID != "SMSTOP1" {
		t.Fatalf("status=%d err=%v consumer=%#v", response.StatusCode, err, consumer)
	}
}

func TestInboundBodyKeywordWithoutOptOutTypeDoesNothing(t *testing.T) {
	const callbackURL = "https://api.example.com/api/notifications/twilio/inbound/salon-1"
	const token = "auth-token"
	consumer := &fakeInboundOptOutConsumer{}
	handler := NewHandler(&fakeCallbackService{}, fakeCallbackConfigResolver{cfg: integrationconfig.TwilioMessagingConfig{AccountSID: "AC123", AuthToken: token, InboundCallbackURL: callbackURL, SenderPhone: "+13125550100"}}, consumer)
	app := fiber.New()
	app.Post("/api/notifications/twilio/inbound/:salon_id", handler.Inbound)
	form := url.Values{"AccountSid": {"AC123"}, "MessageSid": {"SMBODY"}, "From": {"+13125550123"}, "To": {"+13125550100"}, "Body": {"STOP"}}
	params := map[string]string{}
	for key := range form {
		params[key] = form.Get(key)
	}
	request := httptest.NewRequest("POST", "/api/notifications/twilio/inbound/salon-1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Twilio-Signature", expectedSignatureForTest(token, callbackURL, params))
	response, err := app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusNoContent || consumer.calls != 0 {
		t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, consumer.calls)
	}
}

func TestInboundRejectsSignatureForDifferentURL(t *testing.T) {
	const configuredURL = "https://api.example.com/api/notifications/twilio/inbound/salon-1"
	consumer := &fakeInboundOptOutConsumer{}
	handler := NewHandler(&fakeCallbackService{}, fakeCallbackConfigResolver{cfg: integrationconfig.TwilioMessagingConfig{AccountSID: "AC123", AuthToken: "token", InboundCallbackURL: configuredURL, SenderPhone: "+13125550100"}}, consumer)
	app := fiber.New()
	app.Post("/api/notifications/twilio/inbound/:salon_id", handler.Inbound)
	form := url.Values{"AccountSid": {"AC123"}, "MessageSid": {"SMSTART"}, "From": {"+13125550123"}, "To": {"+13125550100"}, "OptOutType": {"START"}}
	params := map[string]string{}
	for key := range form {
		params[key] = form.Get(key)
	}
	request := httptest.NewRequest("POST", "/api/notifications/twilio/inbound/salon-1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Twilio-Signature", expectedSignatureForTest("token", configuredURL+"/wrong", params))
	response, err := app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusForbidden || consumer.calls != 0 {
		t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, consumer.calls)
	}
}

func TestInboundRejectsSignedCallbackRoutedToWrongSalonTransport(t *testing.T) {
	const callbackURL = "https://api.example.com/api/notifications/twilio/inbound/salon-1"
	const sharedToken = "shared-auth-token"
	tests := []struct {
		name   string
		cfg    integrationconfig.TwilioMessagingConfig
		params map[string]string
	}{
		{
			name:   "wrong account",
			cfg:    integrationconfig.TwilioMessagingConfig{AccountSID: "AC_SALON_1", AuthToken: sharedToken, SenderPhone: "+13125550100", InboundCallbackURL: callbackURL},
			params: map[string]string{"AccountSid": "AC_SALON_2", "MessageSid": "SMWRONGACCOUNT", "From": "+13125550123", "To": "+13125550100", "OptOutType": "STOP"},
		},
		{
			name:   "wrong messaging service",
			cfg:    integrationconfig.TwilioMessagingConfig{AccountSID: "AC_SHARED", AuthToken: sharedToken, MessagingServiceSID: "MG_SALON_1", InboundCallbackURL: callbackURL},
			params: map[string]string{"AccountSid": "AC_SHARED", "MessagingServiceSid": "MG_SALON_2", "MessageSid": "SMWRONGSERVICE", "From": "+13125550123", "To": "+13125550100", "OptOutType": "STOP"},
		},
		{
			name:   "wrong sender destination",
			cfg:    integrationconfig.TwilioMessagingConfig{AccountSID: "AC_SHARED", AuthToken: sharedToken, SenderPhone: "+13125550100", InboundCallbackURL: callbackURL},
			params: map[string]string{"AccountSid": "AC_SHARED", "MessageSid": "SMWRONGTO", "From": "+13125550123", "To": "+13125550200", "OptOutType": "STOP"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			consumer := &fakeInboundOptOutConsumer{}
			handler := NewHandler(&fakeCallbackService{}, fakeCallbackConfigResolver{cfg: test.cfg}, consumer)
			app := fiber.New()
			app.Post("/api/notifications/twilio/inbound/:salon_id", handler.Inbound)
			form := url.Values{}
			for key, value := range test.params {
				form.Set(key, value)
			}
			request := httptest.NewRequest("POST", "/api/notifications/twilio/inbound/salon-1", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("X-Twilio-Signature", expectedSignatureForTest(sharedToken, callbackURL, test.params))
			response, err := app.Test(request)
			if err != nil || response.StatusCode != fiber.StatusForbidden || consumer.calls != 0 {
				t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, consumer.calls)
			}
		})
	}
}

func TestStatusRejectsSignedCallbackFromWrongAccount(t *testing.T) {
	const callbackURL = "https://api.example.com/api/notifications/twilio/status"
	service := &fakeCallbackService{}
	handler := NewHandler(service, fakeCallbackConfigResolver{cfg: integrationconfig.TwilioMessagingConfig{AccountSID: "AC_SALON_1", AuthToken: "shared-token", StatusCallbackURL: callbackURL}})
	app := fiber.New()
	app.Post("/api/notifications/twilio/status", handler.Status)
	params := map[string]string{"AccountSid": "AC_SALON_2", "MessageSid": "SM123", "MessageStatus": "delivered"}
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	request := httptest.NewRequest("POST", "/api/notifications/twilio/status", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Twilio-Signature", expectedSignatureForTest("shared-token", callbackURL, params))
	response, err := app.Test(request)
	if err != nil || response.StatusCode != fiber.StatusForbidden || service.calls != 0 {
		t.Fatalf("status=%d err=%v calls=%d", response.StatusCode, err, service.calls)
	}
}
