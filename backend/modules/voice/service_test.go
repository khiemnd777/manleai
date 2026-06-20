package voice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/conversation"
)

func TestStatusReportsPhoneBookingReadiness(t *testing.T) {
	store := newFakeVoiceStore()
	service := NewService(store, newFakeConversationEngine(), testVoiceConfig(), AIProviders{})

	status, err := service.Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Ready {
		t.Fatalf("voice status should be ready: %#v", status)
	}
	if !status.PhoneBookingReady {
		t.Fatalf("phone booking should be ready: %#v", status.Booking)
	}
	if !status.Booking.AIEnabled || !status.Booking.SquareConnected || !status.Booking.SquareSynced {
		t.Fatalf("booking readiness flags = %#v", status.Booking)
	}
	if status.Booking.ServiceCount != 1 || status.Booking.StaffCount != 1 || status.Booking.BusinessHoursCount != 6 {
		t.Fatalf("booking readiness counts = %#v", status.Booking)
	}
}

func TestStatusKeepsWebhookReadyWhenPhoneBookingIsBlocked(t *testing.T) {
	store := newFakeVoiceStore()
	store.bookingReadiness = &PhoneBookingReadiness{
		Ready:           false,
		AIEnabled:       false,
		SquareConnected: false,
		SquareSynced:    false,
		Checks: []ReadinessCheck{{
			Key:      "enable_ai_booking",
			Label:    "Enable AI booking",
			Complete: false,
			Message:  "AI booking is disabled for this salon.",
		}},
		BlockedReason: "AI booking is disabled for this salon.",
	}
	service := NewService(store, newFakeConversationEngine(), testVoiceConfig(), AIProviders{})

	status, err := service.Status(context.Background(), "salon_1", "owner_1")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Ready {
		t.Fatalf("Twilio webhook should still be ready: %#v", status)
	}
	if status.PhoneBookingReady {
		t.Fatalf("phone booking should not be ready when booking prerequisites fail: %#v", status.Booking)
	}
	if status.BlockedReason != "" {
		t.Fatalf("voice blocked reason = %q, want empty because webhook is ready", status.BlockedReason)
	}
	if status.Booking.BlockedReason != "AI booking is disabled for this salon." {
		t.Fatalf("booking blocked reason = %q", status.Booking.BlockedReason)
	}
}

func TestIncomingCallStartsPhoneSessionAndReturnsGreeting(t *testing.T) {
	store := newFakeVoiceStore()
	engine := newFakeConversationEngine()
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleIncomingCall(context.Background(), IncomingCallRequest{
		ProviderCallID: "CA123",
		FromPhone:      "(312) 555-0101",
		ToPhone:        "+1 312-555-0102",
		Payload:        map[string]string{"CallSid": "CA123"},
	})
	if err != nil {
		t.Fatalf("HandleIncomingCall returned error: %v", err)
	}
	if !reply.Continue || reply.InputMode != InputModeGather {
		t.Fatalf("reply continue/input = %v/%s, want true/gather", reply.Continue, reply.InputMode)
	}
	if reply.Message != "Thank you for calling. How can I help you today?" {
		t.Fatalf("message = %q", reply.Message)
	}
	if engine.startCalls != 1 {
		t.Fatalf("start phone calls = %d, want 1", engine.startCalls)
	}
	if engine.startRequest.Provider != ProviderTwilio || engine.startRequest.ProviderCallID != "CA123" {
		t.Fatalf("start request provider/call = %s/%s", engine.startRequest.Provider, engine.startRequest.ProviderCallID)
	}
	if engine.startRequest.FromPhone != "3125550101" || engine.startRequest.ToPhone != "+13125550102" {
		t.Fatalf("normalized phones = %s/%s", engine.startRequest.FromPhone, engine.startRequest.ToPhone)
	}
	if len(store.events) != 1 || store.events[0].EventType != EventIncomingCall || store.events[0].CallSessionID != "session_phone" {
		t.Fatalf("events = %#v, want routed incoming call event", store.events)
	}
}

func TestSpeechTurnRoutesLiveCallThroughAvailabilityOffer(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("I found these openings: first: Wed Jun 10 at 10:00 AM with Mai Nguyen. Which works?", conversation.StatusActive, conversation.OutcomeCollecting)
	engine.messageSession.Intent = conversation.IntentBooking
	engine.messageSession.ServiceID = "service_1"
	engine.messageSession.ServiceName = "Classic Manicure"
	engine.messageSession.OfferedSlots = []conversation.OfferedSlot{{
		StartTime: time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 6, 10, 15, 45, 0, 0, time.UTC),
		StaffID:   "staff_1",
		StaffName: "Mai Nguyen",
	}}
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		SpeechText:     "I need a classic manicure tomorrow.",
		Payload:        map[string]string{"SpeechResult": "I need a classic manicure tomorrow."},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn returned error: %v", err)
	}
	if engine.messageCalls != 1 {
		t.Fatalf("conversation message calls = %d, want 1", engine.messageCalls)
	}
	if engine.lastMessage != "I need a classic manicure tomorrow." {
		t.Fatalf("speech text passed to conversation = %q", engine.lastMessage)
	}
	if !reply.Continue {
		t.Fatalf("reply should continue while caller chooses a slot")
	}
	if !strings.Contains(reply.Message, "I found these openings") || strings.Contains(strings.ToLower(reply.Message), "confirmed") {
		t.Fatalf("reply should offer slots without confirmation: %q", reply.Message)
	}
	if len(reply.Session.OfferedSlots) != 1 {
		t.Fatalf("offered slots = %#v, want one", reply.Session.OfferedSlots)
	}
	if len(store.events) != 1 || store.events[0].EventType != EventSpeechTurn || store.events[0].CallSessionID != "session_phone" {
		t.Fatalf("events = %#v, want speech turn event for call session", store.events)
	}
}

func TestSpeechTurnEndsOnlyAfterConversationReturnsPOSConfirmedBooking(t *testing.T) {
	store := newFakeVoiceStore()
	store.route = &CallRoute{SalonID: "salon_1", OwnerUserID: "owner_1", SessionID: "session_phone"}
	engine := newFakeConversationEngine()
	engine.messageSession = phoneSessionWithAIReply("You are confirmed in Square Appointments for Wed Jun 10 at 10:00 AM.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	engine.messageSession.Intent = conversation.IntentBooking
	engine.messageSession.BookingAttemptID = "attempt_voice"
	engine.messageSession.AppointmentID = "appointment_voice"
	start := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	engine.messageSession.RequestedStartTime = &start
	service := NewService(store, engine, testVoiceConfig(), AIProviders{})

	reply, err := service.HandleSpeechTurn(context.Background(), SpeechTurnRequest{
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		SpeechText:     "The first one works. My name is Linh Tran and my phone is 312-555-0101.",
		Payload:        map[string]string{"SpeechResult": "The first one works."},
	})
	if err != nil {
		t.Fatalf("HandleSpeechTurn returned error: %v", err)
	}
	if reply.Continue {
		t.Fatalf("reply should stop after confirmed booking")
	}
	if !strings.Contains(reply.Message, "confirmed in Square Appointments") {
		t.Fatalf("reply should use confirmed conversation wording: %q", reply.Message)
	}
	if reply.Session.BookingAttemptID != "attempt_voice" || reply.Session.AppointmentID != "appointment_voice" {
		t.Fatalf("booking linkage = %s/%s, want confirmed attempt and appointment", reply.Session.BookingAttemptID, reply.Session.AppointmentID)
	}
}

func testVoiceConfig() config.VoiceConfig {
	return config.VoiceConfig{
		Provider:      ProviderTwilio,
		PublicBaseURL: "https://voice.example.com",
		Twilio: config.TwilioVoiceConfig{
			AuthToken:     "secret",
			IncomingPath:  "/api/voice/twilio/incoming",
			TurnPath:      "/api/voice/twilio/turn",
			RecordingPath: "/api/voice/twilio/recording",
		},
	}
}

func phoneSessionWithAIReply(reply string, status string, outcome string) *conversation.Session {
	return &conversation.Session{
		ID:             "session_phone",
		SalonID:        "salon_1",
		Channel:        conversation.ChannelPhone,
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		InboundPhone:   "+13125550101",
		OutboundPhone:  "+13125550102",
		Status:         status,
		Intent:         conversation.IntentUnknown,
		Outcome:        outcome,
		Transcript: []conversation.TranscriptMessage{{
			Speaker: conversation.SpeakerAI,
			Body:    reply,
		}},
	}
}

type fakeVoiceStore struct {
	salon            *InboundSalon
	route            *CallRoute
	bookingReadiness *PhoneBookingReadiness
	events           []WebhookEvent
	audio            *AudioOutput
}

func newFakeVoiceStore() *fakeVoiceStore {
	return &fakeVoiceStore{
		salon: &InboundSalon{
			SalonID:     "salon_1",
			OwnerUserID: "owner_1",
			SalonName:   "Lotus Nails",
			Phone:       "+13125550102",
		},
		bookingReadiness: &PhoneBookingReadiness{
			Ready:              true,
			AIEnabled:          true,
			SquareConnected:    true,
			SquareSynced:       true,
			ServiceCount:       1,
			StaffCount:         1,
			BusinessHoursCount: 6,
			Checks: []ReadinessCheck{
				{Key: "enable_ai_booking", Label: "Enable AI booking", Complete: true},
				{Key: "connect_square", Label: "Connect Square Appointments", Complete: true},
				{Key: "sync_square", Label: "Sync Square calendar", Complete: true},
				{Key: "bookable_services", Label: "AI-bookable services", Complete: true},
				{Key: "bookable_staff", Label: "AI-bookable staff", Complete: true},
				{Key: "business_hours", Label: "Business hours", Complete: true},
			},
		},
	}
}

func (f *fakeVoiceStore) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*SalonVoiceStatus, error) {
	return &SalonVoiceStatus{SalonID: salonID, Phone: "+13125550102"}, nil
}

func (f *fakeVoiceStore) GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*PhoneBookingReadiness, error) {
	if f.bookingReadiness == nil {
		return nil, ErrNotFound
	}
	return f.bookingReadiness, nil
}

func (f *fakeVoiceStore) FindSalonByPhone(ctx context.Context, phone string) (*InboundSalon, error) {
	if f.salon == nil {
		return nil, ErrNotFound
	}
	return f.salon, nil
}

func (f *fakeVoiceStore) FindCallRoute(ctx context.Context, provider string, providerCallID string) (*CallRoute, error) {
	if f.route == nil {
		return nil, ErrNotFound
	}
	return f.route, nil
}

func (f *fakeVoiceStore) RecordWebhookEvent(ctx context.Context, event WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeVoiceStore) SaveAudioOutput(ctx context.Context, record AudioOutputRecord) (*AudioOutput, error) {
	f.audio = &AudioOutput{ID: "audio_1", ContentType: record.ContentType, Audio: record.Audio}
	return f.audio, nil
}

func (f *fakeVoiceStore) GetAudioOutput(ctx context.Context, id string) (*AudioOutput, error) {
	if f.audio == nil || f.audio.ID != id {
		return nil, ErrNotFound
	}
	return f.audio, nil
}

type fakeConversationEngine struct {
	startCalls     int
	messageCalls   int
	startRequest   conversation.StartPhoneCallRequest
	lastMessage    string
	startSession   *conversation.Session
	messageSession *conversation.Session
}

func newFakeConversationEngine() *fakeConversationEngine {
	return &fakeConversationEngine{
		startSession: phoneSessionWithAIReply("Thank you for calling. How can I help you today?", conversation.StatusActive, conversation.OutcomeCollecting),
	}
}

func (f *fakeConversationEngine) StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req conversation.StartPhoneCallRequest) (*conversation.Session, error) {
	f.startCalls++
	f.startRequest = req
	return f.startSession, nil
}

func (f *fakeConversationEngine) Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.MessageRequest) (*conversation.Session, error) {
	f.messageCalls++
	f.lastMessage = req.Message
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return phoneSessionWithAIReply("I can help with appointments. What service would you like to book?", conversation.StatusActive, conversation.OutcomeCollecting), nil
}

func (f *fakeConversationEngine) Get(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	if f.messageSession != nil {
		return f.messageSession, nil
	}
	return f.startSession, nil
}
