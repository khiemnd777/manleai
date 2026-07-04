package voice_openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/voice"
)

const realtimeLegacyAudioFormat = "g711_ulaw"
const realtimeG711ULawFormat = "audio/pcmu"
const realtimeReadyTimeout = 5 * time.Second
const realtimeTranscriptionPromptMaxLength = 1024

func (a *Adapter) ConnectRealtime(ctx context.Context, salonID string, opts voice.RealtimeSessionOptions) (voice.RealtimeSession, error) {
	cfg, enabled, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	if !enabled || !cfg.RealtimeEnabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.RealtimeModel) == "" {
		return nil, voice.ErrProviderDisabled
	}
	header := realtimeHeaders(cfg)
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 15 * time.Second}).DialContext(ctx, realtimeURL(cfg), header)
	if err != nil {
		return nil, err
	}
	session := &realtimeSession{
		conn:           conn,
		legacyProtocol: realtimeUsesLegacyProtocol(cfg.RealtimeModel),
		events:         make(chan voice.RealtimeEvent, 128),
		done:           make(chan struct{}),
		ready:          make(chan error, 1),
	}
	go session.readLoop()
	if err := session.update(ctx, cfg, opts); err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := session.waitReady(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

type realtimeSession struct {
	conn           *websocket.Conn
	legacyProtocol bool
	writeMu        sync.Mutex
	closeOnce      sync.Once
	readyOnce      sync.Once
	events         chan voice.RealtimeEvent
	done           chan struct{}
	ready          chan error
}

func (s *realtimeSession) AppendInputAudio(ctx context.Context, base64Audio string) error {
	base64Audio = strings.TrimSpace(base64Audio)
	if base64Audio == "" {
		return nil
	}
	return s.write(ctx, map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": base64Audio,
	})
}

func (s *realtimeSession) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return s.write(ctx, realtimeResponseCreatePayload(s.legacyProtocol, text))
}

func (s *realtimeSession) Events() <-chan voice.RealtimeEvent {
	return s.events
}

func (s *realtimeSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
		close(s.done)
	})
	return err
}

func (s *realtimeSession) update(ctx context.Context, cfg config.OpenAIVoiceConfig, opts voice.RealtimeSessionOptions) error {
	return s.write(ctx, map[string]any{
		"type":    "session.update",
		"session": realtimeSessionConfig(cfg, opts),
	})
}

func realtimeSessionConfig(cfg config.OpenAIVoiceConfig, opts voice.RealtimeSessionOptions) map[string]any {
	voiceName := strings.TrimSpace(opts.Voice)
	if voiceName == "" {
		voiceName = strings.TrimSpace(cfg.RealtimeVoice)
	}
	if voiceName == "" {
		voiceName = strings.TrimSpace(cfg.SpeechVoice)
	}
	if realtimeUsesLegacyProtocol(cfg.RealtimeModel) {
		return map[string]any{
			"modalities":                []string{"text", "audio"},
			"instructions":              realtimeInstructions(opts.Instructions),
			"voice":                     voiceName,
			"input_audio_format":        realtimeLegacyAudioFormat,
			"output_audio_format":       realtimeLegacyAudioFormat,
			"input_audio_transcription": realtimeTranscriptionConfig(cfg, opts),
			"turn_detection":            realtimeTurnDetection(cfg),
		}
	}
	return map[string]any{
		"type":              "realtime",
		"output_modalities": []string{"audio"},
		"instructions":      realtimeInstructions(opts.Instructions),
		"audio": map[string]any{
			"input": map[string]any{
				"format":         map[string]any{"type": realtimeG711ULawFormat},
				"transcription":  realtimeTranscriptionConfig(cfg, opts),
				"turn_detection": realtimeTurnDetection(cfg),
			},
			"output": map[string]any{
				"format": map[string]any{"type": realtimeG711ULawFormat},
				"voice":  voiceName,
			},
		},
	}
}

func realtimeTranscriptionConfig(cfg config.OpenAIVoiceConfig, opts voice.RealtimeSessionOptions) map[string]any {
	out := map[string]any{"model": strings.TrimSpace(cfg.TranscriptionModel)}
	if prompt := strings.TrimSpace(opts.TranscriptionPrompt); prompt != "" {
		out["prompt"] = truncateRealtimeTranscriptionPrompt(prompt)
	}
	return out
}

func truncateRealtimeTranscriptionPrompt(value string) string {
	runes := []rune(value)
	if len(runes) <= realtimeTranscriptionPromptMaxLength {
		return value
	}
	return strings.TrimSpace(string(runes[:realtimeTranscriptionPromptMaxLength]))
}

func realtimeResponseCreatePayload(legacyProtocol bool, text string) map[string]any {
	response := map[string]any{
		"instructions": strings.Join([]string{
			"Read this backend-approved phone response exactly as written.",
			"Do not add, omit, paraphrase, translate, or change any words.",
			"Response:",
			strings.TrimSpace(text),
		}, "\n"),
	}
	if legacyProtocol {
		response["modalities"] = []string{"audio"}
	} else {
		response["output_modalities"] = []string{"audio"}
	}
	return map[string]any{
		"type":     "response.create",
		"response": response,
	}
}

type realtimeNoisePolicy struct {
	threshold         float64
	prefixPaddingMS   int
	silenceDurationMS int
}

func realtimeTurnDetection(cfg config.OpenAIVoiceConfig) map[string]any {
	policy := realtimeNoisePolicyForProfile(cfg.RealtimeNoiseProfile)
	return map[string]any{
		"type":                "server_vad",
		"threshold":           policy.threshold,
		"prefix_padding_ms":   policy.prefixPaddingMS,
		"silence_duration_ms": policy.silenceDurationMS,
		"create_response":     false,
		"interrupt_response":  false,
	}
}

func realtimeNoisePolicyForProfile(profile string) realtimeNoisePolicy {
	switch config.NormalizeOpenAIRealtimeNoiseProfile(profile) {
	case "quiet_room":
		return realtimeNoisePolicy{threshold: 0.5, prefixPaddingMS: 300, silenceDurationMS: 450}
	case "balanced":
		return realtimeNoisePolicy{threshold: 0.65, prefixPaddingMS: 300, silenceDurationMS: 650}
	default:
		return realtimeNoisePolicy{threshold: 0.78, prefixPaddingMS: 300, silenceDurationMS: 850}
	}
}

func (s *realtimeSession) waitReady(ctx context.Context) error {
	timeout := time.NewTimer(realtimeReadyTimeout)
	defer timeout.Stop()
	select {
	case err, ok := <-s.ready:
		if !ok {
			return errors.New("realtime session closed before update")
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
		return errors.New("realtime session update timed out")
	}
}

func (s *realtimeSession) write(ctx context.Context, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return s.conn.WriteMessage(websocket.TextMessage, raw)
}

func (s *realtimeSession) readLoop() {
	defer close(s.events)
	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) {
				s.markReady(err)
				s.emit(voice.RealtimeEvent{Type: voice.RealtimeEventError, Error: err.Error()})
			}
			return
		}
		event := parseRealtimeEvent(raw)
		if event.Type == "" {
			continue
		}
		if event.Type == voice.RealtimeEventSessionUpdated {
			s.markReady(nil)
			continue
		}
		if event.Type == voice.RealtimeEventError {
			if strings.TrimSpace(event.Error) == "" {
				s.markReady(errors.New("openai realtime error"))
			} else {
				s.markReady(errors.New(event.Error))
			}
		}
		s.emit(event)
	}
}

func (s *realtimeSession) markReady(err error) {
	s.readyOnce.Do(func() {
		s.ready <- err
		close(s.ready)
	})
}

func (s *realtimeSession) emit(event voice.RealtimeEvent) {
	select {
	case <-s.done:
		return
	case s.events <- event:
	}
}

func parseRealtimeEvent(raw []byte) voice.RealtimeEvent {
	var event struct {
		Type       string `json:"type"`
		ItemID     string `json:"item_id"`
		Delta      string `json:"delta"`
		Transcript string `json:"transcript"`
		Error      struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Param   string `json:"param"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return voice.RealtimeEvent{}
	}
	switch event.Type {
	case "response.audio.delta", "response.output_audio.delta":
		return voice.RealtimeEvent{Type: voice.RealtimeEventAudioDelta, AudioBase64: strings.TrimSpace(event.Delta)}
	case "conversation.item.input_audio_transcription.completed":
		return voice.RealtimeEvent{Type: voice.RealtimeEventTranscriptDone, ItemID: strings.TrimSpace(event.ItemID), Transcript: strings.TrimSpace(event.Transcript)}
	case "input_audio_buffer.speech_started":
		return voice.RealtimeEvent{Type: voice.RealtimeEventSpeechStarted}
	case "response.done":
		return voice.RealtimeEvent{Type: voice.RealtimeEventResponseDone}
	case "session.updated":
		return voice.RealtimeEvent{Type: voice.RealtimeEventSessionUpdated}
	case "error":
		return voice.RealtimeEvent{Type: voice.RealtimeEventError, Error: realtimeErrorMessage(event.Error.Type, event.Error.Code, event.Error.Param, event.Error.Message)}
	default:
		return voice.RealtimeEvent{}
	}
}

func realtimeErrorMessage(errorType string, code string, param string, message string) string {
	parts := []string{}
	for _, part := range []string{errorType, code, param, message} {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ": ")
}

func realtimeHeaders(cfg config.OpenAIVoiceConfig) http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	if realtimeUsesLegacyProtocol(cfg.RealtimeModel) {
		header.Set("OpenAI-Beta", "realtime=v1")
	}
	return header
}

func realtimeUsesLegacyProtocol(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "" || strings.Contains(model, "realtime-preview")
}

func realtimeURL(cfg config.OpenAIVoiceConfig) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	values := url.Values{}
	values.Set("model", strings.TrimSpace(cfg.RealtimeModel))
	return base + "/realtime?" + values.Encode()
}

func realtimeInstructions(extra string) string {
	base := []string{
		"You are connected to a phone call through Twilio Media Streams.",
		"Use server-side voice activity detection to identify caller turns.",
		"Do not autonomously answer salon questions, offer slots, or confirm bookings.",
		"Only produce audio when the backend sends an explicit response.create instruction.",
	}
	extra = strings.TrimSpace(extra)
	if extra != "" {
		base = append(base, extra)
	}
	return strings.Join(base, "\n")
}
