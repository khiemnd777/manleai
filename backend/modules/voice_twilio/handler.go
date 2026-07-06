package voice_twilio

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	realtimeBackendProgressDelay = 1200 * time.Millisecond
	realtimeBackendProgressReply = "One moment while I check that."
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
		return h.twiml(c, adapter.RecordResponse(message+" Please tell me again how I can help.", recordingURL, ""))
	}
	return h.twiml(c, adapter.GatherResponse(message+" Please tell me again how I can help.", adapter.TurnURL(requestBaseURL(c)), ""))
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
			_ = h.service.RecordRealtimeEvent(ctx, voice.ProviderTwilio, providerCallID, sessionID, voice.EventRealtimeConnected, map[string]string{
				"stage":       "openai_connected",
				"stream_sid":  streamSID,
				"duration_ms": durationMilliseconds(openAIConnectAt, time.Now()),
			})
			go h.forwardRealtimeEventsWithInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, initialMessage, seenTranscripts, twilioMarks)
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
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, initialMessage, seenTranscripts, twilioMarks, realtimeTerminalDrainTimeout, realtimeBargeInGuard, realtimeBackendProgressDelay, time.Now)
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
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks, terminalDrainTimeout, realtimeBargeInGuard, realtimeBackendProgressDelay, time.Now)
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
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks, terminalDrainTimeout, bargeInGuard, realtimeBackendProgressDelay, now)
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
	h.forwardRealtimeEventsWithRealtimePolicyAndInitialReply(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, "", seenTranscripts, twilioMarks, terminalDrainTimeout, bargeInGuard, progressDelay, now)
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
) {
	responseActive := false
	activeCloseAfter := false
	activeAudioSent := false
	activeInterrupted := false
	playbackAudioStartedAt := time.Time{}
	suppressAudioUntilDone := false
	pendingReply := realtimeQueuedReply{}
	pendingCloseMark := ""
	closeDrainTimer := (<-chan time.Time)(nil)
	markSequence := 0
	turnResults := make(chan realtimeTurnResult, 8)
	turnQueue := []realtimeTurnTask{}
	turnInFlight := false
	turnProgressTimer := (<-chan time.Time)(nil)
	turnProgressSpoken := false
	activeResponseCreatedAt := time.Time{}
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
			pendingReply = realtimeQueuedReply{message: message, closeAfter: closeAfter}
			return true
		}
		if err := realtime.Speak(ctx, message); err != nil {
			_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_response_create", err)
			closeStream("openai_response_create_failed")
			return false
		}
		activeResponseCreatedAt = now()
		_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "response_create", time.Time{}, map[string]string{
			"close_after": strconv.FormatBool(closeAfter),
		})
		responseActive = true
		activeCloseAfter = closeAfter
		activeAudioSent = false
		activeInterrupted = false
		playbackAudioStartedAt = time.Time{}
		suppressAudioUntilDone = false
		return true
	}

	flushPendingReply := func() bool {
		if strings.TrimSpace(pendingReply.message) == "" {
			return true
		}
		reply := pendingReply
		pendingReply = realtimeQueuedReply{}
		return speakReply(reply.message, reply.closeAfter)
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
			if turnInFlight && !turnProgressSpoken && !responseActive && strings.TrimSpace(pendingReply.message) == "" {
				turnProgressSpoken = true
				if !speakReply(realtimeBackendProgressReply, false) {
					return
				}
			}
		case result := <-turnResults:
			if !handleTurnResult(result) {
				return
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
			case voice.RealtimeEventAudioDelta:
				if suppressAudioUntilDone {
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
			case voice.RealtimeEventSpeechStarted:
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "speech_started", time.Time{}, nil)
				if !shouldInterruptPlayback() {
					continue
				}
				clearPendingCloseMark()
				if streamSID != "" {
					_ = writeJSON(twilioClearMessage{Event: "clear", StreamSid: streamSID})
				}
				if responseActive {
					suppressAudioUntilDone = true
					activeInterrupted = true
				}
			case voice.RealtimeEventTranscriptDone:
				transcript := strings.TrimSpace(event.Transcript)
				if transcript == "" {
					continue
				}
				key := strings.TrimSpace(event.ItemID) + "|" + strings.ToLower(transcript)
				if _, exists := seenTranscripts[key]; exists {
					continue
				}
				seenTranscripts[key] = struct{}{}
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "transcript_done", time.Time{}, map[string]string{
					"item_id": strings.TrimSpace(event.ItemID),
				})
				turnQueue = append(turnQueue, realtimeTurnTask{
					itemID:     strings.TrimSpace(event.ItemID),
					transcript: transcript,
					queuedAt:   time.Now(),
				})
				startNextTurn()
			case voice.RealtimeEventResponseDone:
				completedCloseAfter := activeCloseAfter
				completedAudioSent := activeAudioSent
				completedInterrupted := activeInterrupted
				hadPendingReply := strings.TrimSpace(pendingReply.message) != ""
				_ = h.recordRealtimeTiming(ctx, providerCallID, sessionID, streamSID, "response_done", activeResponseCreatedAt, map[string]string{
					"close_after": strconv.FormatBool(completedCloseAfter),
					"interrupted": strconv.FormatBool(completedInterrupted),
				})
				responseActive = false
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
				if isActiveRealtimeResponseConflict(event.Error) {
					_ = h.recordRealtimeFailure(ctx, providerCallID, sessionID, streamSID, "openai_event", errors.New(event.Error))
					responseActive = true
					continue
				}
				_ = h.recordRealtimeTerminalFailure(ctx, providerCallID, sessionID, streamSID, "openai_event", errors.New(event.Error))
				closeStream("openai_event_error")
				return
			}
		}
	}
}

type realtimeQueuedReply struct {
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

func isActiveRealtimeResponseConflict(message string) bool {
	return strings.Contains(strings.ToLower(message), "conversation_already_has_active_response")
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
