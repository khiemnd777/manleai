package voice_twilio

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/voice"
)

const (
	fallbackInputModeQuery       = "voice_fallback_mode"
	realtimeTerminalDrainTimeout = 12 * time.Second
	realtimeBargeInGuard         = 850 * time.Millisecond
	realtimeTurnTimeout          = 25 * time.Second
	realtimeBackendProgressDelay = 3 * time.Second
	realtimeBackendProgressReply = "Thanks. I'm checking that now."
	realtimeReplyQueueLimit      = 16
	realtimeBufferedAudioLimit   = 8 * 1024 * 1024
)

type Handler struct {
	adapter *Adapter
	service *voice.Service
}

func NewHandler(adapter *Adapter, service *voice.Service) *Handler {
	return &Handler{adapter: adapter, service: service}
}

func (h *Handler) Incoming(c *fiber.Ctx) error {
	params := formParams(c)
	adapter, err := h.requestAdapter(c, params)
	if err != nil {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !adapter.Configured() {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !h.verify(c, adapter, params) {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio webhook signature is invalid.")
	}

	reply, err := h.service.HandleIncomingCall(c.UserContext(), voice.IncomingCallRequest{
		Provider:       voice.ProviderTwilio,
		ProviderCallID: params["CallSid"],
		FromPhone:      params["From"],
		ToPhone:        params["To"],
		Payload:        params,
	})
	if errors.Is(err, voice.ErrProviderDisabled) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if errors.Is(err, voice.ErrRouteNotFound) {
		return h.twiml(c, adapter.FinalResponse("We could not route this call to a salon. Please call the salon directly.", ""))
	}
	if errors.Is(err, voice.ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "TWILIO_WEBHOOK_INVALID", "Twilio webhook request is invalid.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_INCOMING_FAILED", "Could not process incoming call.")
	}
	return h.twiml(c, h.responseForReply(c, adapter, reply))
}

func (h *Handler) Turn(c *fiber.Ctx) error {
	return h.handleTurn(c)
}

func (h *Handler) Recording(c *fiber.Ctx) error {
	return h.handleTurn(c)
}

func (h *Handler) StreamStatus(c *fiber.Ctx) error {
	params := formParams(c)
	adapter, err := h.requestAdapter(c, params)
	if err != nil {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !adapter.Configured() {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !h.verify(c, adapter, params) {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio webhook signature is invalid.")
	}
	eventType := realtimeStatusEventType(params["StreamEvent"])
	payload := copyStringMap(params)
	if payload["stage"] == "" {
		payload["stage"] = "twilio_stream_status"
	}
	if eventType == voice.EventRealtimeFailed && strings.EqualFold(strings.TrimSpace(params["StreamEvent"]), "stream-error") {
		payload["terminal"] = "true"
	}
	if err := h.service.RecordRealtimeEvent(c.UserContext(), voice.ProviderTwilio, params["CallSid"], "", eventType, payload); err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_STREAM_STATUS_FAILED", "Could not record stream status.")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) StreamFallback(c *fiber.Ctx) error {
	params := formParams(c)
	adapter, err := h.requestAdapter(c, params)
	if err != nil {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !adapter.Configured() {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !h.verify(c, adapter, params) {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio webhook signature is invalid.")
	}
	message, err := h.service.RealtimeFallbackMessage(c.UserContext(), voice.ProviderTwilio, params["CallSid"])
	if errors.Is(err, voice.ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "TWILIO_WEBHOOK_INVALID", "Twilio webhook request is invalid.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_STREAM_FALLBACK_FAILED", "Could not process stream fallback.")
	}
	if strings.TrimSpace(message) == "" {
		return h.twiml(c, adapter.HangupResponse())
	}
	recordingURL := appendQueryParam(adapter.RecordingURL(requestBaseURL(c)), fallbackInputModeQuery, voice.InputModeRecording)
	if strings.TrimSpace(recordingURL) != "" {
		return h.twiml(c, adapter.RecordResponse(message, recordingURL, ""))
	}
	return h.twiml(c, adapter.GatherResponse(message, adapter.TurnURL(requestBaseURL(c)), ""))
}

func (h *Handler) handleTurn(c *fiber.Ctx) error {
	params := formParams(c)
	adapter, err := h.requestAdapter(c, params)
	if err != nil {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !adapter.Configured() {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if !h.verify(c, adapter, params) {
		return respond.Error(c, fiber.StatusForbidden, "TWILIO_SIGNATURE_INVALID", "Twilio webhook signature is invalid.")
	}

	audio, audioContentType, fetchErr := h.recordingAudio(c, adapter, params)
	if fetchErr != nil {
		audio = nil
		audioContentType = ""
	}

	runtimeParams := copyStringMap(params)
	inputModeOverride := strings.TrimSpace(c.Query(fallbackInputModeQuery))
	if inputModeOverride != "" {
		runtimeParams[fallbackInputModeQuery] = inputModeOverride
	}
	if token := strings.TrimSpace(c.Get("I-Twilio-Idempotency-Token")); token != "" {
		runtimeParams["TwilioIdempotencyToken"] = token
	}

	reply, err := h.service.HandleSpeechTurn(c.UserContext(), voice.SpeechTurnRequest{
		Provider:          voice.ProviderTwilio,
		ProviderCallID:    params["CallSid"],
		FromPhone:         params["From"],
		ToPhone:           params["To"],
		SpeechText:        params["SpeechResult"],
		Audio:             audio,
		AudioContentType:  audioContentType,
		InputModeOverride: inputModeOverride,
		Payload:           runtimeParams,
	})
	if errors.Is(err, voice.ErrProviderDisabled) {
		return respond.Error(c, fiber.StatusServiceUnavailable, "VOICE_PROVIDER_NOT_CONFIGURED", "Voice provider is not configured.")
	}
	if errors.Is(err, voice.ErrRouteNotFound) {
		return h.twiml(c, adapter.FinalResponse("We could not continue this call. Please call the salon directly.", ""))
	}
	if errors.Is(err, voice.ErrValidation) {
		return respond.Error(c, fiber.StatusBadRequest, "TWILIO_WEBHOOK_INVALID", "Twilio webhook request is invalid.")
	}
	if err != nil {
		return respond.Error(c, fiber.StatusInternalServerError, "TWILIO_TURN_FAILED", "Could not process voice turn.")
	}
	return h.twiml(c, h.responseForReply(c, adapter, reply))
}

func (h *Handler) responseForReply(c *fiber.Ctx, adapter *Adapter, reply *voice.CallReply) string {
	if reply == nil {
		return adapter.FinalResponse("The owner can help with anything else.", "")
	}
	if !reply.Continue {
		return adapter.FinalResponse(reply.Message, reply.AudioURL)
	}
	baseURL := requestBaseURL(c)
	if reply.InputMode == voice.InputModeRecording {
		return adapter.RecordResponseWithOpeningNotice(reply.OpeningNotice, reply.Message, adapter.RecordingURL(baseURL), reply.AudioURL)
	}
	if reply.InputMode == voice.InputModeRealtimeStream && reply.Session != nil {
		streamURL := adapter.StreamURL(baseURL)
		if streamURL != "" {
			providerCallID := strings.TrimSpace(reply.Session.ProviderCallID)
			sessionID := strings.TrimSpace(reply.Session.ID)
			return adapter.StreamResponse(reply.OpeningNotice, streamURL, adapter.StreamStatusURL(baseURL), adapter.StreamFallbackURL(baseURL), reply.AudioURL, map[string]string{
				"call_sid":        providerCallID,
				"session_id":      sessionID,
				"stream_token":    adapter.StreamToken(providerCallID, sessionID),
				"from_phone":      reply.Session.InboundPhone,
				"to_phone":        reply.Session.OutboundPhone,
				"initial_message": reply.Message,
			})
		}
	}
	return adapter.GatherResponseWithOpeningNotice(reply.OpeningNotice, reply.Message, adapter.TurnURL(baseURL), reply.AudioURL)
}

func (h *Handler) Stream(c *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return c.WriteMessage(websocket.TextMessage, raw)
	}
	var closeOnce sync.Once
	closeStream := func(reason string) {
		closeOnce.Do(func() {
			writeMu.Lock()
			defer writeMu.Unlock()
			deadline := time.Now().Add(1 * time.Second)
			_ = c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), deadline)
			_ = c.Close()
		})
	}

	var realtime voice.RealtimeSession
	var streamSID string
	var providerCallID string
	var sessionID string
	var fromPhone string
	var toPhone string
	var initialMessage string
	var connected atomic.Bool
	seenTranscripts := map[string]struct{}{}
	twilioMarks := make(chan string, 8)

	defer func() {
		if realtime != nil {
			_ = realtime.Close()
		}
	}()

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}
		var msg twilioStreamMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Event {
		case "start":
			streamStartAt := time.Now()
			if msg.Start == nil {
				closeStream("missing_start")
				return
			}
			streamSID = firstNonEmpty(msg.StreamSid, msg.Start.StreamSid)
			providerCallID = firstNonEmpty(msg.Start.CustomParameters["call_sid"], msg.Start.CustomParameters["provider_call_id"], msg.Start.CallSid)
			sessionID = strings.TrimSpace(msg.Start.CustomParameters["session_id"])
			fromPhone = msg.Start.CustomParameters["from_phone"]
			toPhone = msg.Start.CustomParameters["to_phone"]
			initialMessage = msg.Start.CustomParameters["initial_message"]
			token := msg.Start.CustomParameters["stream_token"]
			if streamSID == "" || providerCallID == "" || sessionID == "" || token == "" {
				closeStream("invalid_start_parameters")
				return
			}

			adapter, err := h.streamAdapter(ctx, providerCallID)
			if err != nil || !adapter.VerifyStreamToken(providerCallID, sessionID, token) {
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "stream_token", err)
				closeStream("stream_auth_failed")
				return
			}
			route, err := h.service.StreamRoute(ctx, voice.ProviderTwilio, providerCallID, sessionID)
			if err != nil {
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "stream_route", err)
				closeStream("stream_route_failed")
				return
			}
			_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "stream_start", streamStartAt, nil)
			openAIConnectAt := time.Now()
			realtime, err = h.service.ConnectRealtime(ctx, route.SalonID, route.SessionID, providerCallID)
			if err != nil {
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_connect", err)
				closeStream("openai_connect_failed")
				return
			}
			connected.Store(true)
			connectedPayload := map[string]string{
				"stage":       "openai_connected",
				"stream_sid":  streamSID,
				"duration_ms": durationMilliseconds(openAIConnectAt, time.Now()),
			}
			for key, value := range realtimeTranscriptPolicyDiagnostics(realtime.TranscriptPolicy()) {
				connectedPayload[key] = value
			}
			_ = h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeConnected, connectedPayload)
			streamingEnabled, streamErr := h.service.StreamingSpeechEnabled(ctx, route.SalonID)
			if streamErr != nil {
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "speech_output_config", streamErr)
				closeStream("speech_output_config_failed")
				return
			}
			if streamingEnabled {
				go h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, initialMessage, seenTranscripts, twilioMarks, realtimeTerminalDrainTimeout, realtimeBargeInGuard, realtimeBackendProgressDelay, time.Now, &streamingSpeechOutput{salonID: route.SalonID})
			} else {
				go h.forwardRealtimeEventsWithInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, initialMessage, seenTranscripts, twilioMarks)
			}
		case "media":
			if realtime == nil || msg.Media == nil || strings.TrimSpace(msg.Media.Payload) == "" {
				continue
			}
			if msg.Media.Track != "" && msg.Media.Track != "inbound" {
				continue
			}
			if err := realtime.AppendInputAudio(ctx, msg.Media.Payload); err != nil {
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_audio_append", err)
				closeStream("openai_audio_append_failed")
				return
			}
		case "stop":
			if connected.Load() {
				_ = h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeStopped, map[string]string{
					"stage":      "twilio_stop",
					"stream_sid": streamSID,
				})
			}
			return
		case "mark":
			if msg.Mark == nil {
				continue
			}
			name := strings.TrimSpace(msg.Mark.Name)
			if name == "" {
				continue
			}
			select {
			case twilioMarks <- name:
			default:
			}
		}
	}
}

func (h *Handler) forwardRealtimeEvents(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(string),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	seenTranscripts map[string]struct{},
	twilioMarks <-chan string,
) {
	h.forwardRealtimeEventsWithInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks)
}

func (h *Handler) forwardRealtimeEventsWithInitialReply(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(string),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	initialMessage string,
	seenTranscripts map[string]struct{},
	twilioMarks <-chan string,
) {
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, initialMessage, seenTranscripts, twilioMarks, realtimeTerminalDrainTimeout, realtimeBargeInGuard, realtimeBackendProgressDelay, time.Now, nil)
}

func (h *Handler) forwardRealtimeEventsWithTerminalDrainTimeout(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(string),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	seenTranscripts map[string]struct{},
	twilioMarks <-chan string,
	terminalDrainTimeout time.Duration,
) {
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks, terminalDrainTimeout, realtimeBargeInGuard, realtimeBackendProgressDelay, time.Now, nil)
}

func (h *Handler) forwardRealtimeEventsWithRealtimePolicy(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(string),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	seenTranscripts map[string]struct{},
	twilioMarks <-chan string,
	terminalDrainTimeout time.Duration,
	bargeInGuard time.Duration,
	now func() time.Time,
) {
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks, terminalDrainTimeout, bargeInGuard, realtimeBackendProgressDelay, now, nil)
}

func (h *Handler) forwardRealtimeEventsWithRealtimePolicyAndProgress(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(string),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	seenTranscripts map[string]struct{},
	twilioMarks <-chan string,
	terminalDrainTimeout time.Duration,
	bargeInGuard time.Duration,
	progressDelay time.Duration,
	now func() time.Time,
) {
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks, terminalDrainTimeout, bargeInGuard, progressDelay, now, nil)
}

func (h *Handler) forwardRealtimeEventsWithRealtimePolicyAndInitialReply(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(string),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	initialMessage string,
	seenTranscripts map[string]struct{},
	twilioMarks <-chan string,
	terminalDrainTimeout time.Duration,
	bargeInGuard time.Duration,
	progressDelay time.Duration,
	now func() time.Time,
	streamingOutput *streamingSpeechOutput,
) {
	responseActive := false
	activeCloseAfter := false
	activeAudioSent := false
	activeInterrupted := false
	playbackAudioStartedAt := time.Time{}
	suppressAudioUntilDone := false
	activeReply := realtimeQueuedReply{}
	activeResponseID := ""
	activeAudioTranscript := strings.Builder{}
	activeAudioTranscriptDone := false
	activeAudioChunks := []string{}
	activeBufferedAudioBytes := 0
	pendingReplies := []realtimeQueuedReply{}
	pendingCloseMark := ""
	closeDrainTimer := (<-chan time.Time)(nil)
	markSequence := 0
	turnResults := make(chan realtimeTurnResult, 8)
	turnQueue := []realtimeTurnTask{}
	turnInFlight := false
	turnProgressTimer := (<-chan time.Time)(nil)
	turnProgressSpoken := false
	progressSpokenForCall := false
	vadStartByItem := map[string]int{}
	vadDurationByItem := map[string]int{}
	lastVADStartMS := 0
	activeResponseCreatedAt := time.Time{}
	responseSequence := 0
	streamingSpeech := streamingOutput != nil
	requireResponseIdentity := !streamingSpeech && realtime.RequiresResponseIdentity()
	speechEvents := make(chan streamingSpeechEvent, 64)
	var activeSpeechCancel context.CancelFunc
	activeSpeechGeneration := 0
	transcriptPolicy := normalizedRealtimeTranscriptPolicy(realtime.TranscriptPolicy())
	inputRecovery := realtimeInputRecovery{}
	inputRecoveryFinalized := false
	lastApprovedPrompt := strings.TrimSpace(initialMessage)
	if now == nil {
		now = time.Now
	}
	if progressDelay <= 0 {
		progressDelay = realtimeBackendProgressDelay
	}
	recordBackendClose := func(reason string) {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return
		}
		_ = h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeStopped, map[string]string{
			"stage":      "backend_stream_close",
			"stream_sid": strings.TrimSpace(streamSID),
			"reason":     reason,
		})
	}
	closeAfterBackendStop := func(reason string) {
		recordBackendClose(reason)
		closeStream(reason)
	}
	var speakReply func(string, bool) bool
	startNextTurn := func() {
		if turnInFlight || len(turnQueue) == 0 {
			return
		}
		task := turnQueue[0]
		turnQueue = turnQueue[1:]
		turnInFlight = true
		turnProgressSpoken = false
		turnProgressTimer = time.After(progressDelay)
		startedAt := time.Now()
		_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "backend_turn_start", time.Time{}, map[string]string{
			"item_id":   task.itemID,
			"queued_ms": durationMilliseconds(task.queuedAt, startedAt),
		})
		go func(task realtimeTurnTask, startedAt time.Time) {
			turnCtx, turnCancel := context.WithTimeout(ctx, realtimeTurnTimeout)
			reply, err := h.service.HandleSpeechTurn(turnCtx, voice.SpeechTurnRequest{
				Provider:          voice.ProviderTwilio,
				ProviderCallID:    providerCallID,
				FromPhone:         fromPhone,
				ToPhone:           toPhone,
				SpeechText:        task.transcript,
				InputModeOverride: voice.InputModeRealtimeStream,
				Payload: map[string]string{
					"CallSid":              providerCallID,
					"StreamSid":            streamSID,
					"CallSessionID":        sessionID,
					"RealtimeTranscriptID": task.itemID,
				},
			})
			turnCancel()
			result := realtimeTurnResult{
				task:        task,
				reply:       reply,
				err:         err,
				startedAt:   startedAt,
				completedAt: time.Now(),
			}
			select {
			case turnResults <- result:
			case <-ctx.Done():
			}
		}(task, startedAt)
	}
	handleTurnResult := func(result realtimeTurnResult) bool {
		turnInFlight = false
		turnProgressTimer = nil
		status := "ok"
		if result.err != nil {
			status = "error"
		}
		_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "backend_turn_done", result.startedAt, map[string]string{
			"duration_ms":      durationMilliseconds(result.startedAt, result.completedAt),
			"item_id":          result.task.itemID,
			"status":           status,
			"progress_spoken":  strconv.FormatBool(turnProgressSpoken),
			"queued_remaining": strconv.Itoa(len(turnQueue)),
		})
		if result.err != nil {
			if !speakReply("I could not process that clearly. Please say it again, or the owner can help directly.", false) {
				return false
			}
			startNextTurn()
			return true
		}
		if result.reply != nil && strings.TrimSpace(result.reply.Message) != "" {
			lastApprovedPrompt = strings.TrimSpace(result.reply.Message)
			if streamingSpeech && responseActive && activeReply.message == realtimeBackendProgressReply && activeSpeechCancel != nil {
				activeInterrupted = true
				suppressAudioUntilDone = true
				activeSpeechCancel()
				if streamSID != "" {
					_ = writeJSON(twilioClearMessage{Event: "clear", StreamSid: streamSID})
				}
			}
			if !speakReply(result.reply.Message, !result.reply.Continue) {
				return false
			}
			if !result.reply.Continue {
				turnQueue = nil
			}
		}
		startNextTurn()
		return true
	}

	speakReply = func(message string, closeAfter bool) bool {
		message = strings.TrimSpace(message)
		if message == "" {
			return true
		}
		if responseActive {
			if len(pendingReplies) >= realtimeReplyQueueLimit {
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "reply_queue_overflow", errors.New("realtime reply queue limit exceeded"))
				closeStream("realtime_reply_queue_overflow")
				return false
			}
			pendingReplies = append(pendingReplies, realtimeQueuedReply{message: message, closeAfter: closeAfter})
			return true
		}
		responseSequence++
		reply := realtimeQueuedReply{
			requestID:  "manleai-reply-" + strconv.Itoa(responseSequence),
			message:    message,
			closeAfter: closeAfter,
		}
		activeResponseCreatedAt = now()
		stage := "response_create"
		if streamingSpeech {
			stage = "tts_request_start"
		}
		_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, stage, time.Time{}, map[string]string{
			"close_after": strconv.FormatBool(closeAfter),
			"request_id":  reply.requestID,
		})
		responseActive = true
		activeReply = reply
		activeResponseID = ""
		activeAudioTranscript.Reset()
		activeAudioTranscriptDone = false
		activeAudioChunks = nil
		activeBufferedAudioBytes = 0
		activeCloseAfter = closeAfter
		activeAudioSent = false
		activeInterrupted = false
		playbackAudioStartedAt = time.Time{}
		suppressAudioUntilDone = false
		if streamingSpeech {
			activeSpeechGeneration++
			generation := activeSpeechGeneration
			speechCtx, speechCancel := context.WithCancel(ctx)
			activeSpeechCancel = speechCancel
			go func(reply realtimeQueuedReply, generation int) {
				result, err := h.service.StreamSpeech(speechCtx, streamingOutput.salonID, reply.requestID, reply.message, func(chunk voice.SpeechChunk) error {
					event := streamingSpeechEvent{generation: generation, requestID: reply.requestID, chunk: &chunk, arrivedAt: time.Now()}
					select {
					case speechEvents <- event:
						return nil
					case <-speechCtx.Done():
						return speechCtx.Err()
					}
				})
				select {
				case speechEvents <- streamingSpeechEvent{generation: generation, requestID: reply.requestID, result: result, err: err, done: true}:
				case <-ctx.Done():
				}
			}(reply, generation)
			return true
		}
		if err := realtime.Speak(ctx, voice.RealtimeSpeakRequest{RequestID: reply.requestID, Text: reply.message}); err != nil {
			_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_create", err)
			closeStream("openai_response_create_failed")
			return false
		}
		return true
	}

	flushPendingReply := func() bool {
		if len(pendingReplies) == 0 {
			return true
		}
		reply := pendingReplies[0]
		pendingReplies = pendingReplies[1:]
		return speakReply(reply.message, reply.closeAfter)
	}

	responseEventMatchesActive := func(event voice.RealtimeEvent) bool {
		if !responseActive {
			return false
		}
		if requestID := strings.TrimSpace(event.ResponseRequestID); requestID != "" && requestID != activeReply.requestID {
			return false
		}
		responseID := strings.TrimSpace(event.ResponseID)
		if requireResponseIdentity {
			return activeResponseID != "" && responseID == activeResponseID
		}
		if activeResponseID != "" {
			return responseID == "" || responseID == activeResponseID
		}
		return responseID == ""
	}

	clearPendingCloseMark := func() {
		pendingCloseMark = ""
		closeDrainTimer = nil
	}

	beginTerminalCloseDrain := func(audioSent bool) bool {
		if streamSID == "" || !audioSent {
			cancel()
			closeAfterBackendStop("response_complete")
			return false
		}
		markSequence++
		pendingCloseMark = "final-response-" + strconv.Itoa(markSequence)
		if err := writeJSON(twilioOutboundMark{
			Event:     "mark",
			StreamSid: streamSID,
			Mark:      twilioMarkPayload{Name: pendingCloseMark},
		}); err != nil {
			_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "twilio_mark_write", err)
			cancel()
			closeStream("twilio_mark_write_failed")
			return false
		}
		closeDrainTimer = time.After(terminalDrainTimeout)
		return true
	}

	shouldInterruptPlayback := func() bool {
		if playbackAudioStartedAt.IsZero() {
			return false
		}
		if bargeInGuard <= 0 {
			return true
		}
		return !now().Before(playbackAudioStartedAt.Add(bargeInGuard))
	}

	if !speakReply(initialMessage, false) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case markName := <-twilioMarks:
			if pendingCloseMark == "" || markName != pendingCloseMark {
				continue
			}
			_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "twilio_playback_done", time.Time{}, map[string]string{"mark_name": markName})
			clearPendingCloseMark()
			cancel()
			closeAfterBackendStop("response_complete")
			return
		case <-closeDrainTimer:
			clearPendingCloseMark()
			cancel()
			closeAfterBackendStop("response_playback_timeout")
			return
		case <-turnProgressTimer:
			turnProgressTimer = nil
			select {
			case result := <-turnResults:
				if !handleTurnResult(result) {
					return
				}
				continue
			default:
			}
			if turnInFlight && !turnProgressSpoken && !progressSpokenForCall && !responseActive && len(pendingReplies) == 0 {
				turnProgressSpoken = true
				progressSpokenForCall = true
				if !speakReply(realtimeBackendProgressReply, false) {
					return
				}
			}
		case result := <-turnResults:
			if !handleTurnResult(result) {
				return
			}
		case streamed := <-speechEvents:
			if !streamingSpeech || streamed.generation != activeSpeechGeneration || streamed.requestID != activeReply.requestID {
				continue
			}
			if streamed.chunk != nil {
				if suppressAudioUntilDone || len(streamed.chunk.Audio) == 0 {
					continue
				}
				if streamSID == "" {
					continue
				}
				payload := base64.StdEncoding.EncodeToString(streamed.chunk.Audio)
				if err := writeJSON(twilioOutboundMedia{Event: "media", StreamSid: streamSID, Media: twilioOutboundMediaPayload{Payload: payload}}); err != nil {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "twilio_media_write", err)
					if activeSpeechCancel != nil {
						activeSpeechCancel()
					}
					closeStream("twilio_media_write_failed")
					return
				}
				activeAudioSent = true
				if playbackAudioStartedAt.IsZero() {
					playbackAudioStartedAt = now()
					diagnostics := map[string]string{
						"request_id":     streamed.requestID,
						"audio_encoding": "audio/x-mulaw",
						"sample_rate":    strconv.Itoa(8000),
					}
					providerDiagnostics := copyStringMap(diagnostics)
					providerDiagnostics["duration_ms"] = durationMilliseconds(activeResponseCreatedAt, streamed.arrivedAt)
					_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "tts_first_provider_chunk", time.Time{}, providerDiagnostics)
					_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "twilio_first_media_sent", activeResponseCreatedAt, diagnostics)
				}
				continue
			}
			if !streamed.done {
				continue
			}
			completedCloseAfter := activeCloseAfter
			completedAudioSent := activeAudioSent
			completedInterrupted := activeInterrupted
			hadPendingReply := len(pendingReplies) > 0
			if streamed.err != nil && !completedInterrupted {
				if completedAudioSent && streamSID != "" {
					_ = writeJSON(twilioClearMessage{Event: "clear", StreamSid: streamSID})
				}
				_ = h.recordRealtimeTerminalFailureWithExtra(ctx, providerCallID, sessionID, streamSID, "openai_speech_stream", streamed.err, map[string]string{
					"request_id":          streamed.requestID,
					"provider_request_id": streamed.result.ProviderRequestID,
					"audio_chunk_count":   strconv.Itoa(streamed.result.ChunkCount),
					"audio_bytes":         strconv.Itoa(streamed.result.AudioBytes),
				})
				closeStream("openai_speech_stream_failed")
				return
			}
			_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "tts_stream_done", activeResponseCreatedAt, map[string]string{
				"request_id":          streamed.requestID,
				"provider_request_id": streamed.result.ProviderRequestID,
				"audio_chunk_count":   strconv.Itoa(streamed.result.ChunkCount),
				"audio_bytes":         strconv.Itoa(streamed.result.AudioBytes),
				"interrupted":         strconv.FormatBool(completedInterrupted),
				"close_after":         strconv.FormatBool(completedCloseAfter),
			})
			responseActive = false
			activeReply = realtimeQueuedReply{}
			activeCloseAfter = false
			activeAudioSent = false
			activeInterrupted = false
			activeResponseCreatedAt = time.Time{}
			activeSpeechCancel = nil
			suppressAudioUntilDone = false
			if !flushPendingReply() {
				return
			}
			if !hadPendingReply && completedCloseAfter && !completedInterrupted {
				if !beginTerminalCloseDrain(completedAudioSent) {
					return
				}
			}
		case event, ok := <-realtime.Events():
			if !ok {
				if ctx.Err() == nil {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_events_closed", nil)
					closeStream("openai_events_closed")
				}
				return
			}
			switch event.Type {
			case voice.RealtimeEventResponseCreated:
				if streamingSpeech {
					continue
				}
				if !responseActive {
					continue
				}
				requestID := strings.TrimSpace(event.ResponseRequestID)
				responseID := strings.TrimSpace(event.ResponseID)
				if requireResponseIdentity && (requestID == "" || responseID == "") {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_identity_missing", errors.New("realtime response identity missing"))
					closeStream("openai_response_identity_missing")
					return
				}
				if requestID != "" && requestID != activeReply.requestID {
					continue
				}
				if activeResponseID != "" && responseID != "" && responseID != activeResponseID {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_identity_conflict", errors.New("realtime response identity changed"))
					closeStream("openai_response_identity_conflict")
					return
				}
				if responseID != "" {
					activeResponseID = responseID
				}
			case voice.RealtimeEventAudioDelta:
				if streamingSpeech {
					continue
				}
				if !responseEventMatchesActive(event) {
					continue
				}
				if suppressAudioUntilDone {
					continue
				}
				if requireResponseIdentity && strings.TrimSpace(event.AudioBase64) != "" {
					activeBufferedAudioBytes += len(event.AudioBase64)
					if activeBufferedAudioBytes > realtimeBufferedAudioLimit {
						_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_audio_buffer_overflow", errors.New("realtime audio buffer limit exceeded"))
						closeStream("openai_audio_buffer_overflow")
						return
					}
					activeAudioChunks = append(activeAudioChunks, event.AudioBase64)
					continue
				}
				if strings.TrimSpace(event.AudioBase64) != "" && streamSID != "" {
					_ = writeJSON(twilioOutboundMedia{
						Event:     "media",
						StreamSid: streamSID,
						Media:     twilioOutboundMediaPayload{Payload: event.AudioBase64},
					})
					activeAudioSent = true
					if playbackAudioStartedAt.IsZero() {
						playbackAudioStartedAt = now()
						_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "first_audio_delta", activeResponseCreatedAt, nil)
					}
				}
			case voice.RealtimeEventAudioTranscriptDelta:
				if streamingSpeech {
					continue
				}
				if responseEventMatchesActive(event) {
					activeAudioTranscript.WriteString(event.AudioTranscript)
				}
			case voice.RealtimeEventAudioTranscriptDone:
				if streamingSpeech {
					continue
				}
				if responseEventMatchesActive(event) {
					activeAudioTranscriptDone = true
					if strings.TrimSpace(event.AudioTranscript) != "" {
						activeAudioTranscript.Reset()
						activeAudioTranscript.WriteString(event.AudioTranscript)
					}
				}
			case voice.RealtimeEventSpeechStarted:
				if event.AudioStartMS >= 0 {
					lastVADStartMS = event.AudioStartMS
					if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
						vadStartByItem[itemID] = event.AudioStartMS
					}
				}
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "speech_started", time.Time{}, nil)
				if inputRecoveryFinalized {
					continue
				}
				if !shouldInterruptPlayback() {
					continue
				}
				clearPendingCloseMark()
				if streamSID != "" {
					_ = writeJSON(twilioClearMessage{Event: "clear", StreamSid: streamSID})
				}
				if responseActive && !activeInterrupted {
					suppressAudioUntilDone = true
					activeInterrupted = true
					if streamingSpeech && activeSpeechCancel != nil {
						activeSpeechCancel()
						continue
					}
					if err := realtime.CancelResponse(ctx, activeResponseID); err != nil {
						_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_cancel", err)
						closeStream("openai_response_cancel_failed")
						return
					}
				}
			case voice.RealtimeEventSpeechStopped:
				itemID := strings.TrimSpace(event.ItemID)
				startMS := lastVADStartMS
				if value, ok := vadStartByItem[itemID]; ok {
					startMS = value
				}
				if itemID != "" && event.AudioEndMS >= startMS {
					vadDurationByItem[itemID] = event.AudioEndMS - startMS
				}
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "speech_stopped", time.Time{}, map[string]string{
					"item_id":         itemID,
					"audio_end_ms":    strconv.Itoa(event.AudioEndMS),
					"vad_duration_ms": strconv.Itoa(vadDurationByItem[itemID]),
				})
			case voice.RealtimeEventTranscriptDone:
				if inputRecoveryFinalized {
					continue
				}
				itemID := strings.TrimSpace(event.ItemID)
				clearVADDiagnostics := func() {
					delete(vadStartByItem, itemID)
					delete(vadDurationByItem, itemID)
				}
				transcript := strings.TrimSpace(event.Transcript)
				if transcript == "" {
					clearVADDiagnostics()
					continue
				}
				accepted, rejectionReason, diagnostics := realtimeTranscriptAdmission(event, vadDurationByItem[itemID], transcriptPolicy)
				if !accepted {
					recovery := inputRecovery.Reject(lastApprovedPrompt)
					diagnostics["decision"] = "rejected"
					diagnostics["reason"] = rejectionReason
					diagnostics["rejection_streak"] = strconv.Itoa(recovery.Streak)
					diagnostics["recovery_action"] = recovery.Action
					_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "transcript_rejected_low_confidence", time.Time{}, diagnostics)
					clearVADDiagnostics()
					if recovery.Handoff {
						reply, handoffErr := h.service.HandleUnintelligibleRealtimeInput(ctx, voice.ProviderTwilio, providerCallID, sessionID, itemID)
						if handoffErr != nil || reply == nil || strings.TrimSpace(reply.Message) == "" {
							_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "voice_input_handoff_failed", time.Time{}, map[string]string{
								"item_id": itemID,
								"status":  "error",
							})
							if !speakReply(recoveryMessage("I don't want to get your details wrong. Please move closer to the phone or somewhere quieter, then answer once more.", lastApprovedPrompt), false) {
								return
							}
							continue
						}
						_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "voice_input_handoff_done", time.Time{}, map[string]string{
							"item_id": itemID,
							"status":  "ok",
						})
						inputRecoveryFinalized = true
						if !speakReply(reply.Message, !reply.Continue) {
							return
						}
						continue
					}
					if !speakReply(recovery.Message, false) {
						return
					}
					continue
				}
				inputRecovery.Reset()
				key := itemID + "|" + strings.ToLower(transcript)
				if _, exists := seenTranscripts[key]; exists {
					clearVADDiagnostics()
					continue
				}
				seenTranscripts[key] = struct{}{}
				diagnostics["decision"] = "accepted"
				diagnostics["reason"] = "confidence_and_vad_admitted"
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "transcript_done", time.Time{}, diagnostics)
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "transcript_admitted", time.Time{}, diagnostics)
				clearVADDiagnostics()
				turnQueue = append(turnQueue, realtimeTurnTask{
					itemID:     itemID,
					transcript: transcript,
					queuedAt:   time.Now(),
				})
				startNextTurn()
			case voice.RealtimeEventResponseDone:
				if streamingSpeech {
					continue
				}
				if !responseEventMatchesActive(event) {
					continue
				}
				responseStatus := strings.ToLower(strings.TrimSpace(event.ResponseStatus))
				if requireResponseIdentity && responseStatus == "" {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_status_missing", errors.New("realtime response status missing"))
					closeStream("openai_response_status_missing")
					return
				}
				if responseStatus == "failed" || responseStatus == "incomplete" {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_"+responseStatus, errors.New("realtime response "+responseStatus))
					closeStream("openai_response_" + responseStatus)
					return
				}
				if responseStatus == "cancelled" && !activeInterrupted {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_unexpected_cancel", errors.New("realtime response cancelled without caller interruption"))
					closeStream("openai_response_unexpected_cancel")
					return
				}
				if requireResponseIdentity && responseStatus != "cancelled" && !activeInterrupted {
					if len(activeAudioChunks) == 0 {
						_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_audio_missing", errors.New("realtime response completed without audio"))
						closeStream("openai_audio_missing")
						return
					}
					if !activeAudioTranscriptDone {
						_ = h.recordRealtimeTerminalFailureWithExtra(ctx, providerCallID, sessionID, streamSID, "openai_output_transcript_missing", errors.New("realtime response completed without output transcript"), realtimeOutputDiagnostics(activeReply, activeResponseID, activeAudioTranscript.String(), activeAudioChunks, activeBufferedAudioBytes, activeInterrupted))
						closeStream("openai_output_transcript_missing")
						return
					}
					if !sameSpokenReply(activeReply.message, activeAudioTranscript.String()) {
						_ = h.recordRealtimeTerminalFailureWithExtra(ctx, providerCallID, sessionID, streamSID, "openai_spoken_fact_mismatch", errors.New("realtime audio transcript did not match backend reply"), realtimeOutputDiagnostics(activeReply, activeResponseID, activeAudioTranscript.String(), activeAudioChunks, activeBufferedAudioBytes, activeInterrupted))
						closeStream("openai_spoken_fact_mismatch")
						return
					}
					for _, payload := range activeAudioChunks {
						if streamSID == "" || strings.TrimSpace(payload) == "" {
							continue
						}
						if err := writeJSON(twilioOutboundMedia{Event: "media", StreamSid: streamSID, Media: twilioOutboundMediaPayload{Payload: payload}}); err != nil {
							_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "twilio_media_write", err)
							closeStream("twilio_media_write_failed")
							return
						}
						activeAudioSent = true
					}
					if activeAudioSent {
						playbackAudioStartedAt = now()
						_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "first_audio_delta", activeResponseCreatedAt, map[string]string{"buffered": "true"})
					}
				}
				completedCloseAfter := activeCloseAfter
				completedAudioSent := activeAudioSent
				completedInterrupted := activeInterrupted
				hadPendingReply := len(pendingReplies) > 0
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "response_done", activeResponseCreatedAt, map[string]string{
					"close_after": strconv.FormatBool(completedCloseAfter),
					"interrupted": strconv.FormatBool(completedInterrupted),
				})
				responseActive = false
				activeReply = realtimeQueuedReply{}
				activeResponseID = ""
				activeAudioTranscript.Reset()
				activeAudioTranscriptDone = false
				activeAudioChunks = nil
				activeBufferedAudioBytes = 0
				activeCloseAfter = false
				activeAudioSent = false
				activeInterrupted = false
				activeResponseCreatedAt = time.Time{}
				suppressAudioUntilDone = false
				if !flushPendingReply() {
					return
				}
				if !hadPendingReply && completedCloseAfter && !completedInterrupted {
					if !beginTerminalCloseDrain(completedAudioSent) {
						return
					}
				}
			case voice.RealtimeEventError:
				if strings.TrimSpace(event.Error) == "" {
					continue
				}
				if isActiveRealtimeResponseConflict(event) {
					_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_conflict", errors.New(event.Error))
					closeStream("openai_response_conflict")
					return
				}
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_event", errors.New(event.Error))
				closeStream("openai_event_error")
				return
			}
		}
	}
}

type realtimeQueuedReply struct {
	requestID  string
	message    string
	closeAfter bool
}

type realtimeTurnTask struct {
	itemID     string
	transcript string
	queuedAt   time.Time
}

type realtimeTurnResult struct {
	task        realtimeTurnTask
	reply       *voice.CallReply
	err         error
	startedAt   time.Time
	completedAt time.Time
}

type streamingSpeechOutput struct {
	salonID string
}

type streamingSpeechEvent struct {
	generation int
	requestID  string
	chunk      *voice.SpeechChunk
	result     voice.SpeechStreamResult
	err        error
	done       bool
	arrivedAt  time.Time
}

func isActiveRealtimeResponseConflict(event voice.RealtimeEvent) bool {
	return strings.EqualFold(strings.TrimSpace(event.ErrorCode), "conversation_already_has_active_response") ||
		strings.Contains(strings.ToLower(event.Error), "conversation_already_has_active_response")
}

func sameSpokenReply(expected string, actual string) bool {
	return normalizeSpokenReply(expected) != "" && normalizeSpokenReply(expected) == normalizeSpokenReply(actual)
}

var spokenReplyClockPattern = regexp.MustCompile(`(?i)\b([0-9]{1,2}):00\b`)

func normalizeSpokenReply(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(
		"’", "'",
		"i'm", "i am",
		"you're", "you are",
		"we're", "we are",
		"that's", "that is",
		"it's", "it is",
		"don't", "do not",
		"can't", "cannot",
		"won't", "will not",
		"p.m.", "pm",
		"p.m", "pm",
		"a.m.", "am",
		"a.m", "am",
	).Replace(value)
	value = spokenReplyClockPattern.ReplaceAllString(value, "$1")
	var normalized strings.Builder
	space := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
			space = false
			continue
		}
		if !space && normalized.Len() > 0 {
			normalized.WriteByte(' ')
			space = true
		}
	}
	rawTokens := strings.Fields(strings.TrimSpace(normalized.String()))
	tokens := make([]string, 0, len(rawTokens))
	for index := 0; index < len(rawTokens); index++ {
		token := rawTokens[index]
		if index+1 < len(rawTokens) && ((token == "p" && rawTokens[index+1] == "m") || (token == "a" && rawTokens[index+1] == "m")) {
			tokens = append(tokens, token+"m")
			index++
			continue
		}
		if token == "oclock" {
			continue
		}
		if ordinal, ok := spokenOrdinalCardinal[token]; ok {
			token = ordinal
		}
		if isASCIIDigits(token) {
			if len(token) >= 3 {
				for _, digit := range token {
					tokens = append(tokens, spokenDigitWords[digit-'0'])
				}
				continue
			}
			if number, err := strconv.Atoi(token); err == nil && number >= 0 && number < 100 {
				tokens = append(tokens, spokenNumberUnder100(number)...)
				continue
			}
		}
		tokens = append(tokens, token)
	}
	return strings.Join(tokens, " ")
}

var spokenDigitWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

var spokenOrdinalCardinal = map[string]string{
	"first": "one", "second": "two", "third": "three", "fourth": "four", "fifth": "five",
	"sixth": "six", "seventh": "seven", "eighth": "eight", "ninth": "nine", "tenth": "ten",
	"eleventh": "eleven", "twelfth": "twelve", "thirteenth": "thirteen", "fourteenth": "fourteen",
	"fifteenth": "fifteen", "sixteenth": "sixteen", "seventeenth": "seventeen", "eighteenth": "eighteen",
	"nineteenth": "nineteen", "twentieth": "twenty", "twentyfirst": "twenty one", "twentysecond": "twenty two",
	"twentythird": "twenty three", "twentyfourth": "twenty four", "twentyfifth": "twenty five",
	"twentysixth": "twenty six", "twentyseventh": "twenty seven", "twentyeighth": "twenty eight",
	"twentyninth": "twenty nine", "thirtieth": "thirty", "thirtyfirst": "thirty one",
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func spokenNumberUnder100(value int) []string {
	if value < 20 {
		return []string{[]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}[value]}
	}
	tens := []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
	if value%10 == 0 {
		return []string{tens[value/10]}
	}
	return []string{tens[value/10], spokenDigitWords[value%10]}
}

func realtimeOutputDiagnostics(reply realtimeQueuedReply, responseID string, actual string, audioChunks []string, bufferedAudioBytes int, interrupted bool) map[string]string {
	expectedCanonical := normalizeSpokenReply(reply.message)
	actualCanonical := normalizeSpokenReply(actual)
	classification := "canonical_fact_mismatch"
	if actualCanonical == "" {
		classification = "missing_output_transcript"
	}
	return map[string]string{
		"request_id":           strings.TrimSpace(reply.requestID),
		"response_id":          strings.TrimSpace(responseID),
		"expected_hash":        saltedRealtimeTextHash(reply.requestID, expectedCanonical),
		"actual_hash":          saltedRealtimeTextHash(reply.requestID, actualCanonical),
		"expected_token_count": strconv.Itoa(len(strings.Fields(expectedCanonical))),
		"actual_token_count":   strconv.Itoa(len(strings.Fields(actualCanonical))),
		"audio_chunk_count":    strconv.Itoa(len(audioChunks)),
		"buffered_audio_bytes": strconv.Itoa(bufferedAudioBytes),
		"interrupted":          strconv.FormatBool(interrupted),
		"match_classification": classification,
	}
}

func saltedRealtimeTextHash(requestID string, value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(requestID) + "\x00" + strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:8])
}

func realtimeTranscriptMeanLogProb(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	total := 0.0
	count := 0
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		total += value
		count++
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func realtimeTranscriptMinLogProb(values []float64) (float64, bool) {
	minimum := 0.0
	found := false
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		if !found || value < minimum {
			minimum = value
			found = true
		}
	}
	return minimum, found
}

func realtimeTranscriptDiagnostics(event voice.RealtimeEvent, vadDurationMS int) map[string]string {
	diagnostics := map[string]string{"item_id": strings.TrimSpace(event.ItemID)}
	if mean, ok := realtimeTranscriptMeanLogProb(event.TranscriptLogProbs); ok {
		diagnostics["mean_logprob"] = strconv.FormatFloat(mean, 'f', 4, 64)
	}
	if minimum, ok := realtimeTranscriptMinLogProb(event.TranscriptLogProbs); ok {
		diagnostics["min_logprob"] = strconv.FormatFloat(minimum, 'f', 4, 64)
	}
	if len(event.TranscriptLogProbs) > 0 {
		diagnostics["token_count"] = strconv.Itoa(len(event.TranscriptLogProbs))
	}
	if vadDurationMS > 0 {
		diagnostics["vad_duration_ms"] = strconv.Itoa(vadDurationMS)
	}
	return diagnostics
}

func normalizedRealtimeTranscriptPolicy(policy voice.RealtimeTranscriptPolicy) voice.RealtimeTranscriptPolicy {
	if strings.TrimSpace(policy.Profile) == "" {
		policy.Profile = "unspecified"
	}
	if policy.MinMeanLogProb == 0 {
		policy.MinMeanLogProb = -1.0
	}
	if policy.MinTokenLogProb == 0 {
		policy.MinTokenLogProb = -2.0
	}
	if policy.MaxTokensPerSecond < 0 {
		policy.MaxTokensPerSecond = 0
	}
	return policy
}

func realtimeTranscriptPolicyDiagnostics(policy voice.RealtimeTranscriptPolicy) map[string]string {
	policy = normalizedRealtimeTranscriptPolicy(policy)
	return map[string]string{
		"profile":               policy.Profile,
		"require_logprobs":      strconv.FormatBool(policy.RequireLogProbs),
		"min_mean_logprob":      strconv.FormatFloat(policy.MinMeanLogProb, 'f', 4, 64),
		"min_token_logprob":     strconv.FormatFloat(policy.MinTokenLogProb, 'f', 4, 64),
		"max_tokens_per_second": strconv.FormatFloat(policy.MaxTokensPerSecond, 'f', 2, 64),
	}
}

func realtimeTranscriptAdmission(event voice.RealtimeEvent, vadDurationMS int, policy voice.RealtimeTranscriptPolicy) (bool, string, map[string]string) {
	policy = normalizedRealtimeTranscriptPolicy(policy)
	diagnostics := realtimeTranscriptDiagnostics(event, vadDurationMS)
	diagnostics["profile"] = policy.Profile
	diagnostics["min_mean_logprob"] = strconv.FormatFloat(policy.MinMeanLogProb, 'f', 4, 64)
	diagnostics["min_token_logprob"] = strconv.FormatFloat(policy.MinTokenLogProb, 'f', 4, 64)
	mean, hasMean := realtimeTranscriptMeanLogProb(event.TranscriptLogProbs)
	minimum, hasMinimum := realtimeTranscriptMinLogProb(event.TranscriptLogProbs)
	if policy.RequireLogProbs && (!hasMean || !hasMinimum) {
		return false, "missing_confidence_metadata", diagnostics
	}
	if hasMean && mean < policy.MinMeanLogProb {
		return false, "mean_logprob_below_profile_threshold", diagnostics
	}
	if hasMinimum && minimum < policy.MinTokenLogProb {
		return false, "token_logprob_below_profile_threshold", diagnostics
	}
	if vadDurationMS > 0 && policy.MaxTokensPerSecond > 0 && len(event.TranscriptLogProbs) >= 4 {
		tokensPerSecond := float64(len(event.TranscriptLogProbs)) / (float64(vadDurationMS) / 1000)
		diagnostics["tokens_per_second"] = strconv.FormatFloat(tokensPerSecond, 'f', 2, 64)
		diagnostics["max_tokens_per_second"] = strconv.FormatFloat(policy.MaxTokensPerSecond, 'f', 2, 64)
		if tokensPerSecond > policy.MaxTokensPerSecond {
			return false, "transcript_density_incoherent_with_vad", diagnostics
		}
	}
	return true, "confidence_and_vad_admitted", diagnostics
}

func (h *Handler) recordRealtimeTiming(ctx context.Context, providerCallID string, sessionID string, streamSID string, stage string, startedAt time.Time, extra map[string]string) error {
	payload := map[string]string{
		"stage":      strings.TrimSpace(stage),
		"stream_sid": strings.TrimSpace(streamSID),
	}
	if !startedAt.IsZero() {
		payload["duration_ms"] = durationMilliseconds(startedAt, time.Now())
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		payload[key] = value
	}
	return h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeTiming, payload)
}

func durationMilliseconds(start time.Time, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return ""
	}
	if end.Before(start) {
		return "0"
	}
	return strconv.FormatInt(end.Sub(start).Milliseconds(), 10)
}

func (h *Handler) recordRealtimeFailure(ctx context.Context, providerCallID string, sessionID string, streamSID string, stage string, err error) error {
	return h.recordRealtimeFailureWithTerminal(ctx, providerCallID, sessionID, streamSID, stage, err, false)
}

func (h *Handler) recordRealtimeTerminalFailure(ctx context.Context, providerCallID string, sessionID string, streamSID string, stage string, err error) error {
	return h.recordRealtimeFailureWithTerminal(ctx, providerCallID, sessionID, streamSID, stage, err, true)
}

func (h *Handler) recordRealtimeTerminalFailureWithExtra(ctx context.Context, providerCallID string, sessionID string, streamSID string, stage string, err error, extra map[string]string) error {
	payload := map[string]string{
		"stage":      strings.TrimSpace(stage),
		"stream_sid": strings.TrimSpace(streamSID),
		"terminal":   "true",
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			payload[key] = value
		}
	}
	return h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeFailed, payload)
}

func (h *Handler) recordRealtimeFailureWithTerminal(ctx context.Context, providerCallID string, sessionID string, streamSID string, stage string, err error, terminal bool) error {
	payload := map[string]string{
		"stage":      strings.TrimSpace(stage),
		"stream_sid": strings.TrimSpace(streamSID),
	}
	if terminal {
		payload["terminal"] = "true"
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	return h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeFailed, payload)
}

func (h *Handler) streamAdapter(ctx context.Context, providerCallID string) (*Adapter, error) {
	cfg, publicBaseURL, err := h.service.TwilioWebhookConfig(ctx, providerCallID, "")
	if err != nil {
		return nil, err
	}
	return h.adapter.WithConfig(cfg, publicBaseURL), nil
}

func (h *Handler) recordingAudio(c *fiber.Ctx, adapter *Adapter, params map[string]string) ([]byte, string, error) {
	if strings.TrimSpace(params["SpeechResult"]) != "" || strings.TrimSpace(params["RecordingUrl"]) == "" {
		return nil, "", nil
	}
	return adapter.FetchRecording(c.UserContext(), params["RecordingUrl"], params["AccountSid"])
}

func (h *Handler) verify(c *fiber.Ctx, adapter *Adapter, params map[string]string) bool {
	return adapter.VerifyWebhook(adapter.RequestURL(c.OriginalURL(), requestBaseURL(c)), params, c.Get("X-Twilio-Signature"))
}

func (h *Handler) requestAdapter(c *fiber.Ctx, params map[string]string) (*Adapter, error) {
	cfg, publicBaseURL, err := h.service.TwilioWebhookConfig(c.UserContext(), params["CallSid"], params["To"])
	if err != nil {
		return nil, err
	}
	return h.adapter.WithConfig(cfg, publicBaseURL), nil
}

func (h *Handler) twiml(c *fiber.Ctx, body string) error {
	c.Type("xml", "utf-8")
	return c.Status(fiber.StatusOK).SendString(body)
}

func formParams(c *fiber.Ctx) map[string]string {
	params := map[string]string{}
	c.Request().PostArgs().VisitAll(func(key []byte, value []byte) {
		params[string(key)] = string(value)
	})
	return params
}

func requestBaseURL(c *fiber.Ctx) string {
	protocol := c.Protocol()
	host := c.Hostname()
	if strings.TrimSpace(host) == "" {
		return ""
	}
	return protocol + "://" + host
}

func appendQueryParam(rawURL string, key string, value string) string {
	rawURL = strings.TrimSpace(rawURL)
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if rawURL == "" || key == "" || value == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type twilioStreamMessage struct {
	Event     string             `json:"event"`
	StreamSid string             `json:"streamSid"`
	Start     *twilioStreamStart `json:"start,omitempty"`
	Media     *twilioStreamMedia `json:"media,omitempty"`
	Mark      *twilioStreamMark  `json:"mark,omitempty"`
}

type twilioStreamStart struct {
	AccountSid       string            `json:"accountSid"`
	CallSid          string            `json:"callSid"`
	StreamSid        string            `json:"streamSid"`
	CustomParameters map[string]string `json:"customParameters"`
}

type twilioStreamMedia struct {
	Track   string `json:"track"`
	Payload string `json:"payload"`
}

type twilioStreamMark struct {
	Name string `json:"name"`
}

type twilioOutboundMedia struct {
	Event     string                     `json:"event"`
	StreamSid string                     `json:"streamSid"`
	Media     twilioOutboundMediaPayload `json:"media"`
}

type twilioOutboundMediaPayload struct {
	Payload string `json:"payload"`
}

type twilioClearMessage struct {
	Event     string `json:"event"`
	StreamSid string `json:"streamSid"`
}

type twilioOutboundMark struct {
	Event     string            `json:"event"`
	StreamSid string            `json:"streamSid"`
	Mark      twilioMarkPayload `json:"mark"`
}

type twilioMarkPayload struct {
	Name string `json:"name"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func realtimeStatusEventType(streamEvent string) string {
	switch strings.ToLower(strings.TrimSpace(streamEvent)) {
	case "stream-started":
		return voice.EventRealtimeConnected
	case "stream-stopped":
		return voice.EventRealtimeStopped
	case "stream-error":
		return voice.EventRealtimeFailed
	default:
		return voice.EventRealtimeFailed
	}
}

func copyStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
