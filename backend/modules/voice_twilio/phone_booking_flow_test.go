package voice_twilio

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

func TestSignedTwilioWebhookDrivesPhoneBookingFlowThroughConversation(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{
		AuthToken:     "secret",
		IncomingPath:  "/api/voice/twilio/incoming",
		TurnPath:      "/api/voice/twilio/turn",
		RecordingPath: "/api/voice/twilio/recording",
	}, "")
	conversationStore := newPhoneFlowConversationStore()
	bookingTool := &phoneFlowBookingTool{
		attempt: &booking.BookingAttempt{
			ID:           "attempt_voice",
			Status:       booking.StatusConfirmed,
			POSBookingID: "square_booking_voice",
			Appointment:  &booking.Appointment{ID: "appointment_voice", Status: booking.StatusConfirmed},
		},
	}
	conversationService := conversation.NewService(conversationStore, bookingTool)
	voiceStore := newPhoneFlowVoiceStore(conversationStore)
	voiceService := voice.NewService(voiceStore, conversationService, config.VoiceConfig{
		Provider: voice.ProviderTwilio,
		Twilio: config.TwilioVoiceConfig{
			AuthToken:     "secret",
			IncomingPath:  "/api/voice/twilio/incoming",
			TurnPath:      "/api/voice/twilio/turn",
			RecordingPath: "/api/voice/twilio/recording",
		},
	}, voice.AIProviders{})
	app := testTwilioApp(adapter, voiceService)

	incoming := url.Values{
		"CallSid": {"CA_FLOW"},
		"From":    {"+13125550199"},
		"To":      {"+13125550101"},
	}
	incomingRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/incoming", incoming)
	incomingBody := readBody(t, incomingRes)
	if incomingRes.StatusCode != fiber.StatusOK {
		t.Fatalf("incoming status = %d, body = %s", incomingRes.StatusCode, incomingBody)
	}
	if !strings.Contains(incomingBody, "<Gather") || !strings.Contains(incomingBody, "Thank you for calling") {
		t.Fatalf("incoming should return gather greeting: %s", incomingBody)
	}

	firstTurn := url.Values{
		"CallSid":      {"CA_FLOW"},
		"From":         {"+13125550199"},
		"To":           {"+13125550101"},
		"SpeechResult": {"I need a classic manicure on 2026-06-10."},
	}
	firstTurnRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", firstTurn)
	firstTurnBody := readBody(t, firstTurnRes)
	if firstTurnRes.StatusCode != fiber.StatusOK {
		t.Fatalf("first turn status = %d, body = %s", firstTurnRes.StatusCode, firstTurnBody)
	}
	if !strings.Contains(firstTurnBody, "<Gather") || !strings.Contains(firstTurnBody, "I found these openings") || !strings.Contains(firstTurnBody, "10:00 AM") {
		t.Fatalf("first turn should offer available slots: %s", firstTurnBody)
	}
	if strings.Contains(firstTurnBody, "Mai Nguyen") {
		t.Fatalf("anyone availability offer should not present the assigned technician as customer-chosen: %s", firstTurnBody)
	}
	if strings.Contains(strings.ToLower(firstTurnBody), "confirmed") {
		t.Fatalf("availability offer must not confirm booking: %s", firstTurnBody)
	}
	if bookingTool.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", bookingTool.availabilityCalls)
	}
	if bookingTool.calls != 0 {
		t.Fatalf("booking calls = %d, want 0 before slot selection", bookingTool.calls)
	}

	secondTurn := url.Values{
		"CallSid":      {"CA_FLOW"},
		"From":         {"+13125550199"},
		"To":           {"+13125550101"},
		"SpeechResult": {"The first one works. My name is Linh Tran and my phone is 312-555-0101."},
	}
	secondTurnRes := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/turn", secondTurn)
	secondTurnBody := readBody(t, secondTurnRes)
	if secondTurnRes.StatusCode != fiber.StatusOK {
		t.Fatalf("second turn status = %d, body = %s", secondTurnRes.StatusCode, secondTurnBody)
	}
	if strings.Contains(secondTurnBody, "<Gather") {
		t.Fatalf("confirmed booking should end gather loop: %s", secondTurnBody)
	}
	if !strings.Contains(secondTurnBody, "confirmed in Square Appointments") || !strings.Contains(secondTurnBody, "<Hangup/>") {
		t.Fatalf("second turn should return final confirmed TwiML: %s", secondTurnBody)
	}
	if bookingTool.calls != 1 {
		t.Fatalf("booking calls = %d, want 1 after slot selection", bookingTool.calls)
	}
	if bookingTool.request.Source != booking.SourceAIVoiceCall {
		t.Fatalf("booking source = %s, want %s", bookingTool.request.Source, booking.SourceAIVoiceCall)
	}
	if bookingTool.request.StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("booking staff selection mode = %s, want anyone", bookingTool.request.StaffSelectionMode)
	}
	if got := bookingTool.request.Segments; len(got) != 1 || got[0].ServiceID != "service_1" || got[0].StaffID != "staff_1" || got[0].StaffSelectionMode != booking.StaffSelectionAnyone {
		t.Fatalf("booking segments = %#v, want selected staff assignment with anyone mode", got)
	}
	if bookingTool.request.StaffID != "staff_1" || !bookingTool.request.StartTime.Equal(phoneFlowFirstSlotStart()) {
		t.Fatalf("booking request = %#v, want selected offered slot", bookingTool.request)
	}
	if len(voiceStore.events) != 3 {
		t.Fatalf("webhook events = %#v, want incoming plus two speech turns", voiceStore.events)
	}
}

type phoneFlowConversationStore struct {
	cfg         conversation.RuntimeConfig
	session     conversation.Session
	services    []conversation.ServiceOption
	staff       []conversation.StaffOption
	activeStaff []conversation.StaffOption
	knowledge   []conversation.KnowledgeSnippet
	eventKeys   map[string]bool
}

func newPhoneFlowConversationStore() *phoneFlowConversationStore {
	return &phoneFlowConversationStore{
		cfg: conversation.RuntimeConfig{
			SalonName:      "Lotus Nails",
			Timezone:       "America/Chicago",
			AIEnabled:      true,
			HandoffEnabled: true,
			AIGreeting:     "Thank you for calling. How can I help you today?",
		},
		services: []conversation.ServiceOption{{
			ID:              "service_1",
			Name:            "Classic Manicure",
			DurationMinutes: 45,
			PriceFrom:       35,
		}},
		staff: []conversation.StaffOption{{
			ID:         "staff_1",
			Name:       "Mai Nguyen",
			AIBookable: true,
		}},
		eventKeys: map[string]bool{},
	}
}

func (f *phoneFlowConversationStore) GetRuntimeConfig(ctx context.Context, salonID string, ownerUserID string) (*conversation.RuntimeConfig, error) {
	return &f.cfg, nil
}

func (f *phoneFlowConversationStore) CreateSession(ctx context.Context, record conversation.NewSessionRecord) (*conversation.Session, error) {
	now := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	f.session = conversation.Session{
		ID:             "session_phone_flow",
		SalonID:        record.SalonID,
		Channel:        record.Channel,
		Provider:       record.Provider,
		ProviderCallID: record.ProviderCallID,
		InboundPhone:   record.InboundPhone,
		OutboundPhone:  record.OutboundPhone,
		Status:         conversation.StatusActive,
		Intent:         conversation.IntentUnknown,
		Outcome:        conversation.OutcomeCollecting,
		CustomerPhone:  record.CustomerPhone,
		StartedAt:      now,
		CreatedAt:      now,
		UpdatedAt:      now,
		Transcript: []conversation.TranscriptMessage{{
			ID:        "msg_1",
			SessionID: "session_phone_flow",
			SalonID:   record.SalonID,
			Speaker:   conversation.SpeakerAI,
			Body:      record.InitialReply,
			Sequence:  1,
			CreatedAt: now,
		}},
	}
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) GetSessionForOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) GetSessionByTurnEventKey(ctx context.Context, salonID string, ownerUserID string, sessionID string, eventKey string) (*conversation.Session, bool, error) {
	if f.eventKeys[eventKey] {
		return f.copySession(), true, nil
	}
	return nil, false, nil
}

func (f *phoneFlowConversationStore) ListSessions(ctx context.Context, salonID string, ownerUserID string, lifecycleStatus string, limit int) ([]conversation.Session, error) {
	session := f.session
	return []conversation.Session{session}, nil
}

func (f *phoneFlowConversationStore) ListWebhookEvents(ctx context.Context, salonID string, ownerUserID string, sessionID string, limit int) ([]conversation.WebhookEventLog, error) {
	return []conversation.WebhookEventLog{}, nil
}

func (f *phoneFlowConversationStore) ArchiveSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	session := f.session
	session.LifecycleStatus = conversation.LifecycleArchived
	f.session = session
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) RedactSession(ctx context.Context, salonID string, ownerUserID string, sessionID string) (*conversation.Session, error) {
	session := f.session
	session.LifecycleStatus = conversation.LifecycleRedacted
	session.CustomerName = ""
	session.CustomerPhone = ""
	session.CustomerEmail = ""
	f.session = session
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) ListBookableServices(ctx context.Context, salonID string) ([]conversation.ServiceOption, error) {
	return f.services, nil
}

func (f *phoneFlowConversationStore) ListBookableStaff(ctx context.Context, salonID string) ([]conversation.StaffOption, error) {
	return f.staff, nil
}

func (f *phoneFlowConversationStore) ListActiveStaff(ctx context.Context, salonID string) ([]conversation.StaffOption, error) {
	if f.activeStaff != nil {
		return f.activeStaff, nil
	}
	staff := append([]conversation.StaffOption(nil), f.staff...)
	for i := range staff {
		staff[i].AIBookable = true
	}
	return staff, nil
}

func (f *phoneFlowConversationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]conversation.KnowledgeSnippet, error) {
	return f.knowledge, nil
}

func (f *phoneFlowConversationStore) SaveTurn(ctx context.Context, record conversation.TurnRecord) (*conversation.Session, error) {
	if record.EventKey != "" {
		f.eventKeys[record.EventKey] = true
	}
	session := record.Session
	session.Status = record.Update.Status
	session.Intent = record.Update.Intent
	session.Outcome = record.Update.Outcome
	session.CustomerName = record.Update.CustomerName
	session.CustomerPhone = record.Update.CustomerPhone
	session.CustomerEmail = record.Update.CustomerEmail
	session.ServiceID = record.Update.ServiceID
	session.StaffID = record.Update.StaffID
	session.StaffSelectionMode = record.Update.StaffSelectionMode
	for _, service := range f.services {
		if service.ID == session.ServiceID {
			session.ServiceName = service.Name
		}
	}
	for _, member := range f.staff {
		if member.ID == session.StaffID {
			session.StaffName = member.Name
		}
	}
	session.RequestedStartTime = record.Update.RequestedStartTime
	session.RequestedDate = record.Update.RequestedDate
	session.OfferedSlots = record.Update.OfferedSlots
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), record.Update.BookingSegments...)
	session.BookingAttemptID = record.Update.BookingAttemptID
	session.AppointmentID = record.Update.AppointmentID
	session.Summary = record.Update.Summary
	if record.Update.EndSession {
		session.Status = record.Update.Status
		endedAt := time.Date(2026, 6, 10, 14, 5, 0, 0, time.UTC)
		session.EndedAt = &endedAt
	}
	nextSequence := len(session.Transcript) + 1
	session.Transcript = append(session.Transcript, conversation.TranscriptMessage{
		ID:        "msg_customer",
		SessionID: session.ID,
		SalonID:   session.SalonID,
		Speaker:   conversation.SpeakerCustomer,
		Body:      record.CustomerMessage,
		Sequence:  nextSequence,
		CreatedAt: time.Date(2026, 6, 10, 14, 1, 0, 0, time.UTC),
	})
	if strings.TrimSpace(record.ToolMessage) != "" {
		nextSequence++
		session.Transcript = append(session.Transcript, conversation.TranscriptMessage{
			ID:        "msg_tool",
			SessionID: session.ID,
			SalonID:   session.SalonID,
			Speaker:   conversation.SpeakerTool,
			Body:      record.ToolMessage,
			Sequence:  nextSequence,
			CreatedAt: time.Date(2026, 6, 10, 14, 1, 5, 0, time.UTC),
		})
	}
	nextSequence++
	session.Transcript = append(session.Transcript, conversation.TranscriptMessage{
		ID:        "msg_ai",
		SessionID: session.ID,
		SalonID:   session.SalonID,
		Speaker:   conversation.SpeakerAI,
		Body:      record.AIMessage,
		Sequence:  nextSequence,
		CreatedAt: time.Date(2026, 6, 10, 14, 1, 10, 0, time.UTC),
	})
	f.session = session
	return f.copySession(), nil
}

func (f *phoneFlowConversationStore) copySession() *conversation.Session {
	session := f.session
	session.Transcript = append([]conversation.TranscriptMessage(nil), f.session.Transcript...)
	session.OfferedSlots = append([]conversation.OfferedSlot(nil), f.session.OfferedSlots...)
	session.BookingSegments = append([]booking.BookingSegmentRequest(nil), f.session.BookingSegments...)
	return &session
}

type phoneFlowBookingTool struct {
	calls               int
	availabilityCalls   int
	request             booking.CreateBookingRequest
	availabilityRequest booking.AvailabilityRequest
	attempt             *booking.BookingAttempt
}

func (f *phoneFlowBookingTool) AvailableSlots(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest) (*booking.AvailabilityResult, error) {
	f.availabilityCalls++
	f.availabilityRequest = req
	return &booking.AvailabilityResult{
		ServiceID:          "service_1",
		ServiceName:        "Classic Manicure",
		StaffSelectionMode: req.StaffSelectionMode,
		Segments: []booking.AvailabilitySegment{
			{
				ServiceID:          "service_1",
				ServiceName:        "Classic Manicure",
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: req.StaffSelectionMode,
				DurationMinutes:    45,
			},
		},
		PreferredDate:   req.PreferredDate,
		DurationMinutes: 45,
		Timezone:        "America/Chicago",
		Slots: []booking.AvailabilitySlot{
			{
				StartTime:          phoneFlowFirstSlotStart(),
				EndTime:            phoneFlowFirstSlotStart().Add(45 * time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: req.StaffSelectionMode,
				Segments: []booking.AvailabilitySegment{
					{
						ServiceID:          "service_1",
						ServiceName:        "Classic Manicure",
						StaffID:            "staff_1",
						StaffName:          "Mai Nguyen",
						StaffSelectionMode: req.StaffSelectionMode,
						DurationMinutes:    45,
					},
				},
			},
			{
				StartTime:          phoneFlowFirstSlotStart().Add(time.Hour),
				EndTime:            phoneFlowFirstSlotStart().Add(time.Hour + 45*time.Minute),
				StaffID:            "staff_1",
				StaffName:          "Mai Nguyen",
				StaffSelectionMode: req.StaffSelectionMode,
				Segments: []booking.AvailabilitySegment{
					{
						ServiceID:          "service_1",
						ServiceName:        "Classic Manicure",
						StaffID:            "staff_1",
						StaffName:          "Mai Nguyen",
						StaffSelectionMode: req.StaffSelectionMode,
						DurationMinutes:    45,
					},
				},
			},
		},
	}, nil
}

func (f *phoneFlowBookingTool) Create(ctx context.Context, salonID string, ownerUserID string, req booking.CreateBookingRequest) (*booking.BookingAttempt, error) {
	f.calls++
	f.request = req
	return f.attempt, nil
}

func phoneFlowFirstSlotStart() time.Time {
	return time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
}

type phoneFlowVoiceStore struct {
	conversationStore *phoneFlowConversationStore
	salon             *voice.InboundSalon
	events            []voice.WebhookEvent
}

func newPhoneFlowVoiceStore(conversationStore *phoneFlowConversationStore) *phoneFlowVoiceStore {
	return &phoneFlowVoiceStore{
		conversationStore: conversationStore,
		salon: &voice.InboundSalon{
			SalonID:     "salon_1",
			OwnerUserID: "owner_1",
			SalonName:   "Lotus Nails",
			Phone:       "+13125550101",
		},
	}
}

func (f *phoneFlowVoiceStore) GetSalonVoiceStatus(ctx context.Context, salonID string, ownerUserID string) (*voice.SalonVoiceStatus, error) {
	return &voice.SalonVoiceStatus{SalonID: salonID, Phone: "+13125550101"}, nil
}

func (f *phoneFlowVoiceStore) GetPhoneBookingReadiness(ctx context.Context, salonID string, ownerUserID string) (*voice.PhoneBookingReadiness, error) {
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

func (f *phoneFlowVoiceStore) FindSalonByPhone(ctx context.Context, phone string) (*voice.InboundSalon, error) {
	if strings.TrimSpace(phone) == "" {
		return nil, voice.ErrNotFound
	}
	return f.salon, nil
}

func (f *phoneFlowVoiceStore) FindCallRoute(ctx context.Context, provider string, providerCallID string) (*voice.CallRoute, error) {
	session := f.conversationStore.session
	if session.ID == "" || session.Provider != provider || session.ProviderCallID != providerCallID {
		return nil, voice.ErrNotFound
	}
	return &voice.CallRoute{SalonID: session.SalonID, OwnerUserID: "owner_1", SessionID: session.ID}, nil
}

func (f *phoneFlowVoiceStore) RecordWebhookEvent(ctx context.Context, event voice.WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *phoneFlowVoiceStore) SaveAudioOutput(ctx context.Context, record voice.AudioOutputRecord) (*voice.AudioOutput, error) {
	return &voice.AudioOutput{ID: "audio_1", ContentType: record.ContentType, Audio: record.Audio}, nil
}

func (f *phoneFlowVoiceStore) GetAudioOutput(ctx context.Context, id string) (*voice.AudioOutput, error) {
	return nil, voice.ErrNotFound
}
