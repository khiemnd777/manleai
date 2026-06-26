package voice_twilio

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/voice"
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

	reply, err := h.service.HandleSpeechTurn(c.UserContext(), voice.SpeechTurnRequest{
		Provider:         voice.ProviderTwilio,
		ProviderCallID:   params["CallSid"],
		FromPhone:        params["From"],
		ToPhone:          params["To"],
		SpeechText:       params["SpeechResult"],
		Audio:            audio,
		AudioContentType: audioContentType,
		Payload:          params,
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
		return adapter.RecordResponse(reply.Message, adapter.RecordingURL(baseURL), reply.AudioURL)
	}
	if reply.InputMode == voice.InputModeRealtimeStream && reply.Session != nil {
		streamURL := adapter.StreamURL(baseURL)
		if streamURL != "" {
			providerCallID := strings.TrimSpace(reply.Session.ProviderCallID)
			sessionID := strings.TrimSpace(reply.Session.ID)
			return adapter.StreamResponse(reply.Message, streamURL, reply.AudioURL, map[string]string{
				"call_sid":     providerCallID,
				"session_id":   sessionID,
				"stream_token": adapter.StreamToken(providerCallID, sessionID),
				"from_phone":   reply.Session.InboundPhone,
				"to_phone":     reply.Session.OutboundPhone,
			})
		}
	}
	return adapter.GatherResponse(reply.Message, adapter.TurnURL(baseURL), reply.AudioURL)
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
	closeStream := func() {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = c.WriteMessage(websocket.CloseMessage, []byte{})
		_ = c.Close()
	}

	var realtime voice.RealtimeSession
	var streamSID string
	var providerCallID string
	var sessionID string
	var fromPhone string
	var toPhone string
	var closeAfterResponse atomic.Bool
	seenTranscripts := map[string]struct{}{}

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
			if msg.Start == nil {
				return
			}
			streamSID = firstNonEmpty(msg.StreamSid, msg.Start.StreamSid)
			providerCallID = firstNonEmpty(msg.Start.CustomParameters["call_sid"], msg.Start.CustomParameters["provider_call_id"], msg.Start.CallSid)
			sessionID = strings.TrimSpace(msg.Start.CustomParameters["session_id"])
			fromPhone = msg.Start.CustomParameters["from_phone"]
			toPhone = msg.Start.CustomParameters["to_phone"]
			token := msg.Start.CustomParameters["stream_token"]
			if streamSID == "" || providerCallID == "" || sessionID == "" || token == "" {
				return
			}

			adapter, err := h.streamAdapter(ctx, providerCallID)
			if err != nil || !adapter.VerifyStreamToken(providerCallID, sessionID, token) {
				return
			}
			route, err := h.service.StreamRoute(ctx, voice.ProviderTwilio, providerCallID, sessionID)
			if err != nil {
				return
			}
			realtime, err = h.service.ConnectRealtime(ctx, route.SalonID, route.SessionID, providerCallID)
			if err != nil {
				return
			}
			go h.forwardRealtimeEvents(ctx, cancel, closeStream, realtime, writeJSON, streamSID, providerCallID, sessionID, fromPhone, toPhone, seenTranscripts, &closeAfterResponse)
		case "media":
			if realtime == nil || msg.Media == nil || strings.TrimSpace(msg.Media.Payload) == "" {
				continue
			}
			if msg.Media.Track != "" && msg.Media.Track != "inbound" {
				continue
			}
			if err := realtime.AppendInputAudio(ctx, msg.Media.Payload); err != nil {
				return
			}
		case "stop":
			return
		}
	}
}

func (h *Handler) forwardRealtimeEvents(
	ctx context.Context,
	cancel context.CancelFunc,
	closeStream func(),
	realtime voice.RealtimeSession,
	writeJSON func(any) error,
	streamSID string,
	providerCallID string,
	sessionID string,
	fromPhone string,
	toPhone string,
	seenTranscripts map[string]struct{},
	closeAfterResponse *atomic.Bool,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-realtime.Events():
			if !ok {
				return
			}
			switch event.Type {
			case voice.RealtimeEventAudioDelta:
				if strings.TrimSpace(event.AudioBase64) != "" && streamSID != "" {
					_ = writeJSON(twilioOutboundMedia{
						Event:     "media",
						StreamSid: streamSID,
						Media:     twilioOutboundMediaPayload{Payload: event.AudioBase64},
					})
				}
			case voice.RealtimeEventSpeechStarted:
				if streamSID != "" {
					_ = writeJSON(twilioClearMessage{Event: "clear", StreamSid: streamSID})
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
				turnCtx, turnCancel := context.WithTimeout(ctx, 25*time.Second)
				reply, err := h.service.HandleSpeechTurn(turnCtx, voice.SpeechTurnRequest{
					Provider:       voice.ProviderTwilio,
					ProviderCallID: providerCallID,
					FromPhone:      fromPhone,
					ToPhone:        toPhone,
					SpeechText:     transcript,
					Payload: map[string]string{
						"CallSid":              providerCallID,
						"StreamSid":            streamSID,
						"CallSessionID":        sessionID,
						"RealtimeTranscriptID": event.ItemID,
					},
				})
				turnCancel()
				if err != nil {
					_ = realtime.Speak(ctx, "I could not process that clearly. Please say it again, or the owner can help directly.")
					continue
				}
				if reply == nil || strings.TrimSpace(reply.Message) == "" {
					continue
				}
				if !reply.Continue {
					closeAfterResponse.Store(true)
				}
				_ = realtime.Speak(ctx, reply.Message)
			case voice.RealtimeEventResponseDone:
				if closeAfterResponse.Load() {
					cancel()
					closeStream()
					return
				}
			case voice.RealtimeEventError:
				continue
			}
		}
	}
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

type twilioStreamMessage struct {
	Event     string             `json:"event"`
	StreamSid string             `json:"streamSid"`
	Start     *twilioStreamStart `json:"start,omitempty"`
	Media     *twilioStreamMedia `json:"media,omitempty"`
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
