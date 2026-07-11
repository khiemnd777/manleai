package voice_twilio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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
	if !strings.Contains(body, "This call may be recorded to help us manage appointments and improve service.") {
		t.Fatalf("incoming webhook should include recording notice: %s", body)
	}
	if !strings.Contains(body, `action="http://voice.example.com/api/voice/twilio/turn"`) {
		t.Fatalf("gather action should route to turn webhook: %s", body)
	}
}

func TestIncomingWebhookReturnsRealtimeNoticeAndDefersGreetingToOpenAI(t *testing.T) {
	adapter, _, store, engine := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Thank you for calling Lotus Nails. How can I help today?", conversation.StatusActive, conversation.OutcomeCollecting))
	service := voice.NewService(store, engine, config.VoiceConfig{
		Provider: voice.ProviderTwilio,
		Twilio: config.TwilioVoiceConfig{
			AuthToken:      "secret",
			IncomingPath:   "/api/voice/twilio/incoming",
			TurnPath:       "/api/voice/twilio/turn",
			RecordingPath:  "/api/voice/twilio/recording",
			StreamPath:     "/api/voice/twilio/stream",
			VoiceTransport: voice.InputModeRealtimeStream,
		},
		AI: config.VoiceAIConfig{
			Provider: voice.ProviderOpenAI,
			OpenAI: config.OpenAIVoiceConfig{
				APIKey:          "openai-key",
				RealtimeEnabled: true,
				RealtimeModel:   "gpt-realtime-2",
				RealtimeVoice:   "alloy",
			},
		},
	}, voice.AIProviders{Realtime: fakeTwilioRealtimeProvider{configured: true}})
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
	if !strings.Contains(body, `<Say>This call may be recorded to help us manage appointments and improve service.</Say>`) {
		t.Fatalf("realtime incoming should have Twilio recording notice only: %s", body)
	}
	if strings.Contains(body, `<Say>Thank you for calling`) {
		t.Fatalf("realtime incoming should not have Twilio say the AI greeting: %s", body)
	}
	if !strings.Contains(body, `<Connect><Stream`) || !strings.Contains(body, `name="initial_message" value="Thank you for calling Lotus Nails. How can I help today?"`) {
		t.Fatalf("realtime incoming should pass initial AI greeting to stream params: %s", body)
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
	confirmed := phoneSessionWithAIReply("You're confirmed with Lotus Nails for your Classic Manicure on Wednesday, June 10 at 10:00 AM with Mai Nguyen. The appointment is under Linh Tran.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
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
	if !strings.Contains(body, "confirmed with Lotus Nails") || !strings.Contains(body, "<Hangup/>") {
		t.Fatalf("confirmed booking should return final TwiML: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "square") || strings.Contains(strings.ToLower(body), "provider") || strings.Contains(strings.ToLower(body), "pos") {
		t.Fatalf("confirmed booking TwiML should not expose provider internals: %s", body)
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
	events := store.eventsSnapshot()
	if len(events) == 0 {
		t.Fatalf("expected stream status event to be recorded")
	}
	event := events[len(events)-1]
	if event.EventType != voice.EventRealtimeFailed {
		t.Fatalf("event type = %q, want %q", event.EventType, voice.EventRealtimeFailed)
	}
	if event.Payload["StreamError"] != "Connection reset without closing handshake" || event.Payload["stage"] != "twilio_stream_status" || event.Payload["terminal"] != "true" {
		t.Fatalf("payload = %#v", event.Payload)
	}
}

func TestStreamFallbackHangsUpForActiveSessionWithoutTerminalFailure(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Thank you for calling.", conversation.StatusActive, conversation.OutcomeCollecting))
	app := testTwilioApp(adapter, service)
	form := url.Values{"CallSid": {"CA123"}}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/stream/fallback", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if body != adapter.HangupResponse() {
		t.Fatalf("active stream fallback without terminal failure should hang up, got: %s", body)
	}
}

func TestStreamFallbackResumesApprovedPromptForTerminalFailure(t *testing.T) {
	adapter, service, store, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Thank you for calling.", conversation.StatusActive, conversation.OutcomeCollecting))
	_ = store.RecordWebhookEvent(context.Background(), voice.WebhookEvent{
		Provider:       voice.ProviderTwilio,
		ProviderCallID: "CA123",
		CallSessionID:  "session_phone",
		EventType:      voice.EventRealtimeFailed,
		Payload:        map[string]string{"stage": "openai_connect", "terminal": "true"},
	})
	app := testTwilioApp(adapter, service)
	form := url.Values{"CallSid": {"CA123"}}

	res := signedTwilioRequest(t, app, adapter, http.MethodPost, "/api/voice/twilio/stream/fallback", form)
	body := readBody(t, res)

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "I had an audio issue, but we can continue. Thank you for calling.") ||
		!strings.Contains(body, "<Record") ||
		!strings.Contains(body, "voice_fallback_mode=recording") {
		t.Fatalf("terminal stream fallback should resume the approved prompt in recording mode: %s", body)
	}
	if strings.Contains(body, "wait for the owner. Please tell me again") {
		t.Fatalf("terminal stream fallback should not imply an immediate owner handoff: %s", body)
	}
}

func TestStreamFallbackOnlyHangsUpForCompletedSession(t *testing.T) {
	completed := phoneSessionWithAIReply("You're confirmed with Lotus Nails.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
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

func TestForwardRealtimeEventsQueuesReplyUntilResponseDone(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	if got := waitForSpeak(t, realtime); got != "What name should I put on the appointment?" {
		t.Fatalf("first speak = %q", got)
	}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_2", Transcript: "yes"}
	assertNoSpeak(t, realtime)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	if got := waitForSpeak(t, realtime); got != "What name should I put on the appointment?" {
		t.Fatalf("queued speak = %q", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsKeepsAllBackendRepliesInFIFOOrder(t *testing.T) {
	adapter, service, _, engine := testTwilioRuntimeWithStore(phoneSessionWithAIReply("unused", conversation.StatusActive, conversation.OutcomeCollecting))
	engine.messageReplies = []string{"First backend reply.", "Second backend reply.", "Third backend reply."}
	engine.messageCompleted = make(chan struct{}, 3)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(make(chan string, 1)), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "first"}
	if got := waitForSpeak(t, realtime); got != "First backend reply." {
		t.Fatalf("first speak = %q", got)
	}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_2", Transcript: "second"}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_3", Transcript: "third"}
	for i := 0; i < 3; i++ {
		select {
		case <-engine.messageCompleted:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for backend turn %d", i+1)
		}
	}

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	if got := waitForSpeak(t, realtime); got != "Second backend reply." {
		t.Fatalf("second speak = %q", got)
	}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	if got := waitForSpeak(t, realtime); got != "Third backend reply." {
		t.Fatalf("third speak = %q", got)
	}
}

func TestForwardRealtimeEventsIgnoresStaleResponseIdentity(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Backend reply.", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 3)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(make(chan string, 1)), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "first"}
	_ = waitForSpeak(t, realtime)
	request := waitForSpeakRequest(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseCreated, ResponseID: "resp_current", ResponseRequestID: request.RequestID}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_2", Transcript: "second"}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, ResponseID: "resp_stale", AudioBase64: "stale"}
	assertNoWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone, ResponseID: "resp_stale", ResponseStatus: "completed"}
	assertNoSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, ResponseID: "resp_current", AudioBase64: "current"}
	_ = waitForWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone, ResponseID: "resp_current", ResponseStatus: "completed"}
	if got := waitForSpeak(t, realtime); got != "Backend reply." {
		t.Fatalf("queued speak = %q", got)
	}
}

func TestForwardRealtimeEventsBuffersStrictAudioUntilApprovedTranscriptMatches(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Backend reply.", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	realtime.strictIdentity = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 3)
	closed := make(chan string, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "first"}
	_ = waitForSpeak(t, realtime)
	request := waitForSpeakRequest(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseCreated, ResponseID: "resp_current", ResponseRequestID: request.RequestID}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, ResponseID: "resp_current", AudioBase64: "approved-audio"}
	assertNoWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioTranscriptDone, ResponseID: "resp_current", AudioTranscript: "Backend reply."}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone, ResponseID: "resp_current", ResponseRequestID: request.RequestID, ResponseStatus: "completed"}
	if got := waitForWrite(t, writes); got != (twilioOutboundMedia{Event: "media", StreamSid: "MZ123", Media: twilioOutboundMediaPayload{Payload: "approved-audio"}}) {
		t.Fatalf("write = %#v, want buffered approved audio", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsRejectsStrictSpokenFactMismatch(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Backend approved reply.", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	realtime.strictIdentity = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 3)
	closed := make(chan string, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "first"}
	_ = waitForSpeak(t, realtime)
	request := waitForSpeakRequest(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseCreated, ResponseID: "resp_current", ResponseRequestID: request.RequestID}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, ResponseID: "resp_current", AudioBase64: "unsafe-audio"}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioTranscriptDone, ResponseID: "resp_current", AudioTranscript: "Your appointment is confirmed."}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone, ResponseID: "resp_current", ResponseRequestID: request.RequestID, ResponseStatus: "completed"}
	assertNoWrite(t, writes)
	select {
	case reason := <-closed:
		if reason != "openai_spoken_fact_mismatch" {
			t.Fatalf("close reason = %q", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for spoken fact mismatch close")
	}
}

func TestSameSpokenReplyPreservesUnicodeFacts(t *testing.T) {
	if !sameSpokenReply("Dịch vụ gel, ba mươi phút.", "Dịch vụ gel ba mươi phút!") {
		t.Fatal("unicode operational facts should match across punctuation")
	}
	if sameSpokenReply("Dịch vụ gel", "Dịch vụ bột") {
		t.Fatal("different unicode service facts must not match")
	}
}

func TestForwardRealtimeEventsSpeaksProgressWhileBackendTurnIsPending(t *testing.T) {
	adapter, service, _, engine := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	engine.messageDelay = 80 * time.Millisecond
	engine.messageStarted = make(chan struct{}, 1)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)

	go handler.forwardRealtimeEventsWithRealtimePolicyAndProgress(ctx, cancel, closeStreamRecorder(closed), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil, realtimeTerminalDrainTimeout, 0, 10*time.Millisecond, time.Now)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	select {
	case <-engine.messageStarted:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for backend turn to start")
	}
	if got := waitForSpeak(t, realtime); got != realtimeBackendProgressReply {
		t.Fatalf("progress speak = %q", got)
	}
	if strings.Contains(strings.ToLower(realtimeBackendProgressReply), "confirmed") {
		t.Fatalf("progress reply must not confirm booking: %q", realtimeBackendProgressReply)
	}
	time.Sleep(120 * time.Millisecond)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	if got := waitForSpeak(t, realtime); got != "What name should I put on the appointment?" {
		t.Fatalf("final queued speak = %q", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsSpeaksProgressAtMostOncePerCall(t *testing.T) {
	adapter, service, _, engine := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	engine.messageDelay = 80 * time.Millisecond
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handler.forwardRealtimeEventsWithRealtimePolicyAndProgress(ctx, cancel, closeStreamRecorder(make(chan string, 1)), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil, realtimeTerminalDrainTimeout, 0, 10*time.Millisecond, time.Now)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	if got := waitForSpeak(t, realtime); got != realtimeBackendProgressReply {
		t.Fatalf("first progress speak = %q", got)
	}
	time.Sleep(100 * time.Millisecond)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	if got := waitForSpeak(t, realtime); got != "What name should I put on the appointment?" {
		t.Fatalf("first final speak = %q", got)
	}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_2", Transcript: "two p.m."}
	if got := waitForSpeak(t, realtime); got != "What name should I put on the appointment?" {
		t.Fatalf("second turn should skip repeated progress, got %q", got)
	}
}

func TestForwardRealtimeEventsRejectsLowConfidenceTranscriptWithoutMutatingConversation(t *testing.T) {
	adapter, service, store, engine := testTwilioRuntimeWithStore(phoneSessionWithAIReply("Which service would you like?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(make(chan string, 1)), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{
		Type: voice.RealtimeEventTranscriptDone, ItemID: "item_noise", Transcript: "gel removal",
		TranscriptLogProbs: []float64{-2.4, -1.8},
	}
	if got := waitForSpeak(t, realtime); got != realtimeLowConfidenceReply {
		t.Fatalf("low-confidence reply = %q", got)
	}
	if engine.lastMessage != "" {
		t.Fatalf("low-confidence transcript reached conversation engine: %q", engine.lastMessage)
	}
	waitForTimingStages(t, store, []string{"transcript_rejected_low_confidence"})
}

func TestForwardRealtimeEventsRecordsTimingStages(t *testing.T) {
	adapter, service, store, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 2)

	go handler.forwardRealtimeEventsWithRealtimePolicy(ctx, cancel, closeStreamRecorder(make(chan string, 1)), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil, realtimeTerminalDrainTimeout, 0, time.Now)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "first-audio"}
	_ = waitForWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}

	waitForTimingStages(t, store, []string{"transcript_done", "backend_turn_start", "backend_turn_done", "response_create", "first_audio_delta", "response_done"})
	for _, event := range store.eventsSnapshot() {
		if event.EventType != voice.EventRealtimeTiming {
			continue
		}
		if event.Payload["transcript"] != "" || event.Payload["audio"] != "" {
			t.Fatalf("timing event should not include transcript or audio payload: %#v", event)
		}
	}
}

func TestForwardRealtimeEventsSuppressesInterruptedAudioUntilResponseDone(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 4)
	closed := make(chan string, 1)

	go handler.forwardRealtimeEventsWithRealtimePolicy(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil, realtimeTerminalDrainTimeout, 0, time.Now)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "first-audio"}
	if got := waitForWrite(t, writes); got != (twilioOutboundMedia{
		Event:     "media",
		StreamSid: "MZ123",
		Media:     twilioOutboundMediaPayload{Payload: "first-audio"},
	}) {
		t.Fatalf("write = %#v, want first outbound media", got)
	}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventSpeechStarted}
	if got := waitForWrite(t, writes); got != (twilioClearMessage{Event: "clear", StreamSid: "MZ123"}) {
		t.Fatalf("write = %#v, want clear", got)
	}
	if got := waitForCancel(t, realtime); got != "" {
		t.Fatalf("cancel response id = %q, want empty legacy response id", got)
	}

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "stale-audio"}
	assertNoWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_2", Transcript: "please continue"}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "fresh-audio"}
	if got := waitForWrite(t, writes); got != (twilioOutboundMedia{
		Event:     "media",
		StreamSid: "MZ123",
		Media:     twilioOutboundMediaPayload{Payload: "fresh-audio"},
	}) {
		t.Fatalf("write = %#v, want fresh outbound media", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsSpeaksInitialGreetingBeforeCallerTranscript(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)

	go handler.forwardRealtimeEventsWithInitialReply(ctx, cancel, closeStreamRecorder(closed), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", "Thank you for calling Lotus Nails. How can I help today?", map[string]struct{}{}, nil)

	if got := waitForSpeak(t, realtime); got != "Thank you for calling Lotus Nails. How can I help today?" {
		t.Fatalf("initial speak = %q", got)
	}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "classic manicure"}
	if got := waitForSpeak(t, realtime); got != "What name should I put on the appointment?" {
		t.Fatalf("turn speak = %q", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsIgnoresSpeechStartedBeforeAudioPlayback(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 2)
	closed := make(chan string, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventSpeechStarted}
	assertNoWrite(t, writes)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "still-plays"}
	if got := waitForWrite(t, writes); got != (twilioOutboundMedia{
		Event:     "media",
		StreamSid: "MZ123",
		Media:     twilioOutboundMediaPayload{Payload: "still-plays"},
	}) {
		t.Fatalf("write = %#v, want outbound media after ignored noise", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsProtectsEarlyPlaybackNoise(t *testing.T) {
	adapter, service, _, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan any, 3)
	closed := make(chan string, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "first-audio"}
	_ = waitForWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventSpeechStarted}
	assertNoWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "protected-audio"}
	if got := waitForWrite(t, writes); got != (twilioOutboundMedia{
		Event:     "media",
		StreamSid: "MZ123",
		Media:     twilioOutboundMediaPayload{Payload: "protected-audio"},
	}) {
		t.Fatalf("write = %#v, want protected outbound media", got)
	}
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsFallsBackForActiveResponseConflict(t *testing.T) {
	adapter, service, store, _ := testTwilioRuntimeWithStore(phoneSessionWithAIReply("What name should I put on the appointment?", conversation.StatusActive, conversation.OutcomeCollecting))
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(any) error { return nil }, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "one p.m."}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventError, Error: "invalid_request_error: conversation_already_has_active_response: Conversation already has an active response in progress: resp_123."}

	if got := waitForClose(t, closed); got != "openai_response_conflict" {
		t.Fatalf("close reason = %q", got)
	}
	events := store.eventsSnapshot()
	if len(events) == 0 {
		t.Fatalf("expected recoverable active-response conflict to be recorded")
	}
	event := events[len(events)-1]
	if event.EventType != voice.EventRealtimeFailed || event.Payload["stage"] != "openai_response_conflict" || event.Payload["terminal"] != "true" || !strings.Contains(event.Payload["error"], "conversation_already_has_active_response") {
		t.Fatalf("event = %#v", event)
	}
}

func TestForwardRealtimeEventsClosesTerminalReplyOnlyAfterTwilioMark(t *testing.T) {
	completed := phoneSessionWithAIReply("You're confirmed with Lotus Nails for your Classic Manicure on Wednesday, June 10 at 10:00 AM with Mai Nguyen. The appointment is under Linh Tran. Thank you, goodbye.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	completed.BookingAttemptID = "attempt_voice"
	completed.AppointmentID = "appointment_voice"
	adapter, service, store, _ := testTwilioRuntimeWithStore(completed)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)
	writes := make(chan any, 4)
	marks := make(chan string, 4)

	go handler.forwardRealtimeEventsWithRealtimePolicy(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, marks, realtimeTerminalDrainTimeout, 0, time.Now)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "Sim"}
	if got := waitForSpeak(t, realtime); !strings.Contains(got, "Thank you, goodbye.") {
		t.Fatalf("terminal speak = %q, want closing confirmation", got)
	}
	assertNoClose(t, closed)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "confirmed-audio"}
	if got := waitForWrite(t, writes); got != (twilioOutboundMedia{
		Event:     "media",
		StreamSid: "MZ123",
		Media:     twilioOutboundMediaPayload{Payload: "confirmed-audio"},
	}) {
		t.Fatalf("write = %#v, want confirmation audio", got)
	}

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	markWrite, ok := waitForWrite(t, writes).(twilioOutboundMark)
	if !ok {
		t.Fatalf("terminal response should write a Twilio mark")
	}
	if markWrite.Event != "mark" || markWrite.StreamSid != "MZ123" || markWrite.Mark.Name != "final-response-1" {
		t.Fatalf("mark write = %#v", markWrite)
	}
	assertNoClose(t, closed)

	marks <- "stale-final-response"
	assertNoClose(t, closed)
	marks <- markWrite.Mark.Name
	if got := waitForClose(t, closed); got != "response_complete" {
		t.Fatalf("close reason = %q, want response_complete", got)
	}
	events := store.eventsSnapshot()
	event := events[len(events)-1]
	if event.EventType != voice.EventRealtimeStopped || event.Payload["stage"] != "backend_stream_close" || event.Payload["reason"] != "response_complete" {
		t.Fatalf("backend close event = %#v", event)
	}
}

func TestForwardRealtimeEventsClosesTerminalReplyImmediatelyWhenNoAudioWasSent(t *testing.T) {
	completed := phoneSessionWithAIReply("You're confirmed with Lotus Nails. Thank you, goodbye.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	completed.BookingAttemptID = "attempt_voice"
	completed.AppointmentID = "appointment_voice"
	adapter, service, _, _ := testTwilioRuntimeWithStore(completed)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)
	writes := make(chan any, 1)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, nil)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "Sim"}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}

	if got := waitForClose(t, closed); got != "response_complete" {
		t.Fatalf("close reason = %q, want response_complete", got)
	}
	assertNoWrite(t, writes)
}

func TestForwardRealtimeEventsClosesTerminalReplyOnPlaybackTimeout(t *testing.T) {
	completed := phoneSessionWithAIReply("You're confirmed with Lotus Nails for your Classic Manicure. Thank you, goodbye.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	completed.BookingAttemptID = "attempt_voice"
	completed.AppointmentID = "appointment_voice"
	adapter, service, _, _ := testTwilioRuntimeWithStore(completed)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)
	writes := make(chan any, 4)
	marks := make(chan string, 4)

	go handler.forwardRealtimeEventsWithTerminalDrainTimeout(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, marks, 10*time.Millisecond)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "Sim"}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "confirmed-audio"}
	_ = waitForWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	if _, ok := waitForWrite(t, writes).(twilioOutboundMark); !ok {
		t.Fatalf("terminal response should write a Twilio mark")
	}

	if got := waitForClose(t, closed); got != "response_playback_timeout" {
		t.Fatalf("close reason = %q, want response_playback_timeout", got)
	}
}

func TestForwardRealtimeEventsInvalidatesTerminalMarkOnCallerInterruption(t *testing.T) {
	completed := phoneSessionWithAIReply("You're confirmed with Lotus Nails for your Classic Manicure. Thank you, goodbye.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	completed.BookingAttemptID = "attempt_voice"
	completed.AppointmentID = "appointment_voice"
	adapter, service, _, _ := testTwilioRuntimeWithStore(completed)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)
	writes := make(chan any, 4)
	marks := make(chan string, 4)

	go handler.forwardRealtimeEventsWithRealtimePolicy(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, marks, realtimeTerminalDrainTimeout, 0, time.Now)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "Sim"}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "confirmed-audio"}
	_ = waitForWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	markWrite, ok := waitForWrite(t, writes).(twilioOutboundMark)
	if !ok {
		t.Fatalf("terminal response should write a Twilio mark")
	}

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventSpeechStarted}
	if got := waitForWrite(t, writes); got != (twilioClearMessage{Event: "clear", StreamSid: "MZ123"}) {
		t.Fatalf("write = %#v, want clear", got)
	}
	marks <- markWrite.Mark.Name
	assertNoClose(t, closed)
}

func TestForwardRealtimeEventsKeepsTerminalMarkOnEarlyPlaybackNoise(t *testing.T) {
	completed := phoneSessionWithAIReply("You're confirmed with Lotus Nails for your Classic Manicure. Thank you, goodbye.", conversation.StatusCompleted, conversation.OutcomeBookingConfirmed)
	completed.BookingAttemptID = "attempt_voice"
	completed.AppointmentID = "appointment_voice"
	adapter, service, _, _ := testTwilioRuntimeWithStore(completed)
	handler := NewHandler(adapter, service)
	realtime := newFakeRealtimeSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan string, 1)
	writes := make(chan any, 4)
	marks := make(chan string, 4)

	go handler.forwardRealtimeEvents(ctx, cancel, closeStreamRecorder(closed), realtime, func(value any) error {
		writes <- value
		return nil
	}, "MZ123", "CA123", "session_phone", "+13125550101", "+13125550102", map[string]struct{}{}, marks)

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: "item_1", Transcript: "Sim"}
	_ = waitForSpeak(t, realtime)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: "confirmed-audio"}
	_ = waitForWrite(t, writes)
	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	markWrite, ok := waitForWrite(t, writes).(twilioOutboundMark)
	if !ok {
		t.Fatalf("terminal response should write a Twilio mark")
	}

	realtime.events <- voice.RealtimeEvent{Type: voice.RealtimeEventSpeechStarted}
	assertNoWrite(t, writes)
	marks <- markWrite.Mark.Name
	if got := waitForClose(t, closed); got != "response_complete" {
		t.Fatalf("close reason = %q, want response_complete", got)
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
			SalonID:                 "salon_1",
			OwnerUserID:             "owner_1",
			SalonName:               "Lotus Nails",
			Phone:                   "+13125550102",
			RecordingEnabled:        true,
			RecordingConsentMessage: "This call may be recorded to help us manage appointments and improve service.",
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
	mu     sync.Mutex
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

func (f *fakeTwilioVoiceStore) HasTerminalRealtimeFailure(ctx context.Context, provider string, providerCallID string, sessionID string) (bool, error) {
	for _, event := range f.eventsSnapshot() {
		if event.Provider != provider || event.ProviderCallID != providerCallID || event.EventType != voice.EventRealtimeFailed {
			continue
		}
		if sessionID != "" && event.CallSessionID != "" && event.CallSessionID != sessionID {
			continue
		}
		if event.Payload["terminal"] == "true" || strings.EqualFold(event.Payload["StreamEvent"], "stream-error") || strings.EqualFold(event.Payload["stream_event"], "stream-error") {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeTwilioVoiceStore) eventsSnapshot() []voice.WebhookEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]voice.WebhookEvent(nil), f.events...)
}

func (f *fakeTwilioVoiceStore) SaveAudioOutput(ctx context.Context, record voice.AudioOutputRecord) (*voice.AudioOutput, error) {
	return &voice.AudioOutput{ID: "audio_1", ContentType: record.ContentType, Audio: record.Audio}, nil
}

func (f *fakeTwilioVoiceStore) GetAudioOutput(ctx context.Context, id string) (*voice.AudioOutput, error) {
	return nil, voice.ErrNotFound
}

type fakeTwilioConversationEngine struct {
	startSession     *conversation.Session
	messageSession   *conversation.Session
	lastMessage      string
	messageDelay     time.Duration
	messageStarted   chan struct{}
	messageCompleted chan struct{}
	messageReplies   []string
	messageCalls     int
}

func (f *fakeTwilioConversationEngine) StartPhoneCall(ctx context.Context, salonID string, ownerUserID string, req conversation.StartPhoneCallRequest) (*conversation.Session, error) {
	return f.startSession, nil
}

func (f *fakeTwilioConversationEngine) Message(ctx context.Context, salonID string, ownerUserID string, sessionID string, req conversation.MessageRequest) (*conversation.Session, error) {
	if f.messageStarted != nil {
		select {
		case f.messageStarted <- struct{}{}:
		default:
		}
	}
	if f.messageDelay > 0 {
		select {
		case <-time.After(f.messageDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.lastMessage = req.Message
	if f.messageCompleted != nil {
		select {
		case f.messageCompleted <- struct{}{}:
		default:
		}
	}
	if f.messageCalls < len(f.messageReplies) {
		session := *f.messageSession
		session.Transcript = append([]conversation.TranscriptMessage(nil), f.messageSession.Transcript...)
		session.Transcript[len(session.Transcript)-1].Body = f.messageReplies[f.messageCalls]
		f.messageCalls++
		return &session, nil
	}
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

func (f *fakeTwilioConversationEngine) TranscriptionContext(ctx context.Context, salonID string, ownerUserID string, sessionID string) (conversation.TranscriptionContext, error) {
	return conversation.TranscriptionContext{}, nil
}

type fakeTwilioRealtimeProvider struct {
	configured bool
}

func (f fakeTwilioRealtimeProvider) Name() string {
	return voice.ProviderOpenAI
}

func (f fakeTwilioRealtimeProvider) Configured(ctx context.Context, salonID string) bool {
	return f.configured
}

func (f fakeTwilioRealtimeProvider) ConnectRealtime(ctx context.Context, salonID string, opts voice.RealtimeSessionOptions) (voice.RealtimeSession, error) {
	return nil, voice.ErrProviderDisabled
}

type fakeRealtimeSession struct {
	events         chan voice.RealtimeEvent
	speaks         chan string
	appends        chan string
	cancels        chan string
	speakRequests  chan voice.RealtimeSpeakRequest
	strictIdentity bool
}

func newFakeRealtimeSession() *fakeRealtimeSession {
	return &fakeRealtimeSession{
		events:        make(chan voice.RealtimeEvent, 8),
		speaks:        make(chan string, 8),
		appends:       make(chan string, 8),
		cancels:       make(chan string, 8),
		speakRequests: make(chan voice.RealtimeSpeakRequest, 8),
	}
}

func (f *fakeRealtimeSession) AppendInputAudio(ctx context.Context, base64Audio string) error {
	f.appends <- base64Audio
	return nil
}

func (f *fakeRealtimeSession) Speak(ctx context.Context, req voice.RealtimeSpeakRequest) error {
	f.speakRequests <- req
	f.speaks <- req.Text
	return nil
}

func waitForSpeakRequest(t *testing.T, realtime *fakeRealtimeSession) voice.RealtimeSpeakRequest {
	t.Helper()
	select {
	case got := <-realtime.speakRequests:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for realtime speak request")
		return voice.RealtimeSpeakRequest{}
	}
}

func waitForCancel(t *testing.T, realtime *fakeRealtimeSession) string {
	t.Helper()
	select {
	case got := <-realtime.cancels:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for realtime cancel")
		return ""
	}
}

func (f *fakeRealtimeSession) CancelResponse(ctx context.Context, responseID string) error {
	f.cancels <- responseID
	return nil
}

func (f *fakeRealtimeSession) RequiresResponseIdentity() bool {
	return f.strictIdentity
}

func (f *fakeRealtimeSession) Events() <-chan voice.RealtimeEvent {
	return f.events
}

func (f *fakeRealtimeSession) Close() error {
	return nil
}

func closeStreamRecorder(closed chan string) func(string) {
	return func(reason string) {
		select {
		case closed <- reason:
		default:
		}
	}
}

func waitForSpeak(t *testing.T, realtime *fakeRealtimeSession) string {
	t.Helper()
	select {
	case got := <-realtime.speaks:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for realtime speak")
		return ""
	}
}

func assertNoSpeak(t *testing.T, realtime *fakeRealtimeSession) {
	t.Helper()
	select {
	case got := <-realtime.speaks:
		t.Fatalf("unexpected realtime speak: %q", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func waitForTimingStages(t *testing.T, store *fakeTwilioVoiceStore, required []string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		stages := map[string]bool{}
		events := store.eventsSnapshot()
		for _, event := range events {
			if event.EventType == voice.EventRealtimeTiming {
				stages[event.Payload["stage"]] = true
			}
		}
		missing := ""
		for _, stage := range required {
			if !stages[stage] {
				missing = stage
				break
			}
		}
		if missing == "" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("missing realtime timing stage %q in events: %#v", missing, events)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForWrite(t *testing.T, writes <-chan any) any {
	t.Helper()
	select {
	case got := <-writes:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for websocket write")
		return nil
	}
}

func assertNoWrite(t *testing.T, writes <-chan any) {
	t.Helper()
	select {
	case got := <-writes:
		t.Fatalf("unexpected websocket write: %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func assertNoClose(t *testing.T, closed <-chan string) {
	t.Helper()
	select {
	case reason := <-closed:
		t.Fatalf("stream closed unexpectedly: %s", reason)
	case <-time.After(100 * time.Millisecond):
	}
}

func waitForClose(t *testing.T, closed <-chan string) string {
	t.Helper()
	select {
	case reason := <-closed:
		return reason
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for stream close")
		return ""
	}
}
