package voice_twilio

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
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
	return adapter.GatherResponse(reply.Message, adapter.TurnURL(baseURL), reply.AudioURL)
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
