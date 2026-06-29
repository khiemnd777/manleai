package voice_twilio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestIncomingWebhookReturnsGatherGreeting(t *testing.T) {
	adapter, service, _ := testTwilioRuntime(phoneSessionWithAIReply("Thank you for calling. How can I help you today?", conversation.StatusActive, conversation.OutcomeCollecting))
	app := testTwilioApp(adapter, service)
	form := url.Values{
		"CallSid": {"CA123"},
		"From":    {"+13125550101"},
		"To":      {"+13125550102"},
	}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/incoming", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "<Gather") || !strings.Contains(body, "Thank you for calling") {
		t.Fatalf("incoming webhook should return gather greeting TwiML: %s", body)
	}
	if !strings.Contains(body, `action="http://voice.example.com/api/voice/twilio/turn"`) {
		t.Fatalf("gather action should route to turn webhook: %s", body)
	}
}

func TestTurnWebhookReturnsGatherWithAvailabilityOffer(t *testing.T) {
	adapter, service, engine := testTwilioRuntime(phoneSessionWithAIReply("I found these openings: first: Wed Jun 10 at 10:00 AM with Mai Nguyen. Which works?", conversation.StatusActive, conversation.OutcomeCollecting))
	app := testTwilioApp(adapter, service)
	form := url.Values{
		"CallSid":      {"CA123"},
		"From":         {"+13125550101"},
		"To":           {"+13125550102"},
		"SpeechResult": {"I need a classic manicure tomorrow."},
	}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if engine.lastMessage != "I need a classic manicure tomorrow." {
		t.Fatalf("speech text passed to conversation = %q", engine.lastMessage)
	}
	if !strings.Contains(body, "<Gather") || !strings.Contains(body, "I found these openings") {
		t.Fatalf("turn webhook should continue call with slot offer: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "confirmed") {
		t.Fatalf("slot offer TwiML should not confirm booking before selection: %s", body)
	}
}

func TestTurnWebhookReturnsFinalTwiMLAfterPOSConfirmedBooking(t *testing.T) {
	confirmed := phoneSessionWithAIReply("You are confirmed in Square Appointments for Wed Jun 10 at 10:00 AM.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	confirmed.BookingAttemptID = "attempt_voice"
	confirmed.AppointmentID = "appointment_voice"
	adapter, service, _ := testTwilioRuntime(confirmed)
	app := testTwilioApp(adapter, service)
	form := url.Values{
		"CallSid":      {"CA123"},
		"From":         {"+13125550101"},
		"To":           {"+13125550102"},
		"SpeechResult": {"The first one works. My name is Linh Tran."},
	}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if strings.Contains(body, "<Gather") {
		t.Fatalf("confirmed booking should end the Twilio gather loop: %s", body)
	}
	if !strings.Contains(body, "confirmed in Square Appointments") || !strings.Contains(body, "<Hangup/>") {
		t.Fatalf("confirmed booking should return final TwiML: %s", body)
	}
}

func TestStreamStatusWebhookRecordsRealtimeFailure(t *testing.T) {
	adapter, service, store, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Thank you for calling.", conversation.StatusActive, conversation.OutcomeCollecting))
	app := testTwilioApp(adapter, service)
	form := url.Values{
		"CallSid":     {"CA123"},
		"StreamSid":   {"MZ123"},
		"StreamEvent": {"stream-error"},
		"StreamError": {"Connection reset without closing handshake"},
	}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/stream/status", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if len(store.events) == 0 {
		t.Fatalf("expected stream status event to be recorded")
	}
	event := store.events[len(store.events)-1]
	if event.EventType != voice.EventRealtimeFailed {
		t.Fatalf("event type = %q, want %q", event.EventType, voice.EventRealtimeFailed)
	}
	if event.Payload["StreamError"] != "Connection reset without closing handshake" || event.Payload["stage"] != "twilio_stream_status" {
		t.Fatalf("payload = %#v", event.Payload)
	}
}

func TestStreamFallbackSaysConnectionProblemForActiveSession(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Thank you for calling.", conversation.StatusActive, conversation.OutcomeCollecting))
	app := testTwilioApp(adapter, service)
	form := url.Values{"CallSid": {"CA123"}}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/stream/fallback", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "live phone connection had a problem") ||
		!strings.Contains(body, "<Record") ||
		!strings.Contains(body, "voice_fallback_mode=recording") {
		t.Fatalf("active stream fallback should explain the connection problem and continue in recording mode: %s", body)
	}
}

func TestStreamFallbackOnlyHangsUpForCompletedSession(t *testing.T) {
	completed := phoneSessionWithAIReply("You are confirmed in Square Appointments.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	adapter, service, _, _ := testTwilioRuntimeWithStore(completed)
	app := testTwilioApp(adapter, service)
	form := url.Values{"CallSid": {"CA123"}}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/stream/fallback", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if body != adapter.HangupResponse() {
		t.Fatalf("completed stream fallback should only hang up, got: %s", body)
	}
}

func testTwilioRuntime(messageSession *conversation.Session) (*Adapter, *voice.Service, *fakeTwilioConversationEngine) {
	adapter, service, _, engine := testTwilioRuntimeWithStore(messageSession)
	return adapter, service, engine
}

func testTwilioRuntimeWithStore(messageSession *conversation.Session) (*Adapter, *voice.Service, *fakeTwilioVoiceStore, *fakeTwilioConversationEngine) {
	adapter := NewAdapter(config.TwilioVoiceConfig{
		AuthToken:     "secret",
		IncomingPath:  "/api/voice/twilio/incoming",
		TurnPath:      "/api/voice/twilio/turn",
		RecordingPath: "/api/voice/twilio/recording",
		StreamPath:    "/api/voice/twilio/stream",
	}, "")
	store := &fakeTwilioVoiceStore{
		salon: &voice.InboundSalon{
			SalonID:     "salon_1",
			OwnerUserID: "owner_1",
			SalonName:   "Lotus Nails",
			Phone:       "+13125550102",
		},
		route: &voice.CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"},
	}
	engine := &fakeTwilioConversationEngine{
		startSession:   phoneSessionWithAIReply("Thank you for calling. How can I help you today?", conversation.StatusActive, conversation.OutcomeCollecting),
		messageSession: messageSession,
	}
	service := voice.NewService(store, engine, config.VoiceConfig{
		Provider: voice.ProviderTwilio,
		Twilio: config.TwilioVoiceConfig{
			AuthToken:     "secret",
			IncomingPath:  "/api/voice/twilio/incoming",
			TurnPath:      "/api/voice/twilio/turn",
			RecordingPath: "/api/voice/twilio/recording",
			StreamPath:    "/api/voice/twilio/stream",
		},
	}, voice.AIProviders{})
	return adapter, service, store, engine
}

func testTwilioApp(adapter *Adapter, service *voice.Service) *fiber.App {
	app := fiber.New()
	handler := NewHandler(adapter, service)
	app.Post("/api/voice/twilio/incoming", handler.Incoming)
	app.Post("/api/voice/twilio/turn", handler.Turn)
	app.Post("/api/voice/twilio/stream/status", handler.StreamStatus)
	app.Post("/api/voice/twilio/stream/fallback", handler.StreamFallback)
	return app
}

func signedTwilioRequest(t *testing.T, app *fiber.App, adapter *Adapter, method string, path string, form url.Values) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, "http://voice.example.com"+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", adapter.ExpectedSignature("http://voice.example.com"+path, formParamsFromValues(form)))
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return res
}

func formParamsFromValues(values url.Values) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if len(value) > 0 {
			out[key] = value[0]
		}
	}
	return out
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(body)
}

func phoneSessionWithAIReply(reply string, status string, outcome string) *conversation.Session {
	return &conversation.Session{
		ID:             "session_phone",
		SalonID:        "salon_1",
		Channel:        conversation.ChannelPhone,
		Provider:       voice.ProviderTwilio,
		ProviderCallID: "CA123",
		InboundPhone:   "+13125550101",
		OutboundPhone:  "+13125550102",
		Status:         status,
		Intent:         conversation.IntentBooking,
		Outcome:        outcome,
		Transcript: []conversation.TranscriptMessage{{
			Speaker:   conversation.SpeakerAI,
			Body:      reply,
			CreatedAt: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
		}},
	}
}

type fakeTwilioVoiceStore struct {
	salon  *voice.InboundSalon
	route  *voice.CallRoute
	events []voice.WebhookEvent
}

func (f *fakeTwilioVoiceStore) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*voice.SalonVoiceStatus, error) {
	return &voice.SalonVoiceStatus{SalonID: salonID, Phone: "+13125550102"}, nil
}

func (f *fakeTwilioVoiceStore) GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*voice.PhoneBookingReadiness, error) {
	return &voice.PhoneBookingReadiness{
		Ready:              true,
		AIEnabled:          true,
		SquareConnected:    true,
		SquareSynced:       true,
		ServiceCount:       1,
		StaffCount:         1,
		BusinessHoursCount: 6,
	}, nil
}

func (f *fakeTwilioVoiceStore) FindSalonByPhone(ctx context.Context, phone string) (*voice.InboundSalon, error) {
	if f.salon == nil {
		return nil, voice.ErrNotFound
	}
	return f.salon, nil
}

func (f *fakeTwilioVoiceStore) FindCallRoute(ctx context.Context, provider string, providerCallID string) (*voice.CallRoute, error) {
	if f.route == nil {
		return nil, voice.ErrNotFound
	}
	return f.route, nil
}

func (f *fakeTwilioVoiceStore) RecordWebhookEvent(ctx context.Context, event voice.WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeTwilioVoiceStore) SaveAudioOutput(ctx context.Context, record voice.AudioOutputRecord) (*voice.AudioOutput, error) {
	return &voice.AudioOutput{ID: "audio_1", ContentType: record.ContentType, Audio: record.Audio}, nil
}

func (f *fakeTwilioVoiceStore) GetAudioOutput(ctx context.Context, id string) (*voice.AudioOutput, error) {
	return nil, voice.ErrNotFound
}

type fakeTwilioConversationEngine struct {
	startSession   *conversation.Session
	messageSession *conversation.Session
	lastMessage    string
}

func (f *fakeTwilioConversationEngine) StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req conversation.StartPhoneCallRequest) (*conversation.Session, error) {
	return f.startSession, nil
}

func (f *fakeTwilioConversationEngine) Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.MessageRequest) (*conversation.Session, error) {
	f.lastMessage = req.Message
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return f.startSession, nil
}

func (f *fakeTwilioConversationEngine) Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return f.startSession, nil
}
