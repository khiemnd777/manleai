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

const realtimeAudioFormat = "g711_ulaw"

func (a *Adapter) ConnectRealtime(ctx context.Context, salonID string, opts voice.RealtimeSessionOptions) (voice.RealtimeSession, error) {
	cfg, enabled, err := a.configFor(ctx, salonID)
	if err != nil {
		return nil, err
	}
	if !enabled || !cfg.RealtimeEnabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.RealtimeModel) == "" {
		return nil, voice.ErrProviderDisabled
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	header.Set("OpenAI-Beta", "realtime=v1")
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 15 * time.Second}).DialContext(ctx, realtimeURL(cfg), header)
	if err != nil {
		return nil, err
	}
	session := &realtimeSession{
		conn:   conn,
		events: make(chan voice.RealtimeEvent, 128),
		done:   make(chan struct{}),
	}
	if err := session.update(ctx, cfg, opts); err != nil {
		_ = session.Close()
		return nil, err
	}
	go session.readLoop()
	return session, nil
}

type realtimeSession struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
	events    chan voice.RealtimeEvent
	done      chan struct{}
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
	return s.write(ctx, map[string]any{
		"type": "response.create",
		"response": map[string]any{
			"modalities": []string{"audio"},
			"instructions": strings.Join([]string{
				"Read this backend-approved phone response exactly as written.",
				"Do not add, omit, paraphrase, translate, or change any words.",
				"Response:",
				text,
			}, "\n"),
		},
	})
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
	voiceName := strings.TrimSpace(opts.Voice)
	if voiceName == "" {
		voiceName = strings.TrimSpace(cfg.RealtimeVoice)
	}
	if voiceName == "" {
		voiceName = strings.TrimSpace(cfg.SpeechVoice)
	}
	session := map[string]any{
		"modalities":                []string{"text", "audio"},
		"instructions":              realtimeInstructions(opts.Instructions),
		"voice":                     voiceName,
		"input_audio_format":        realtimeAudioFormat,
		"output_audio_format":       realtimeAudioFormat,
		"input_audio_transcription": map[string]any{"model": strings.TrimSpace(cfg.TranscriptionModel)},
		"turn_detection": map[string]any{
			"type":                "server_vad",
			"threshold":           0.5,
			"prefix_padding_ms":   300,
			"silence_duration_ms": 450,
			"create_response":     false,
		},
	}
	return s.write(ctx, map[string]any{
		"type":    "session.update",
		"session": session,
	})
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
				s.emit(voice.RealtimeEvent{Type: voice.RealtimeEventError, Error: err.Error()})
			}
			return
		}
		event := parseRealtimeEvent(raw)
		if event.Type == "" {
			continue
		}
		s.emit(event)
	}
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
	case "error":
		return voice.RealtimeEvent{Type: voice.RealtimeEventError, Error: strings.TrimSpace(event.Error.Message)}
	default:
		return voice.RealtimeEvent{}
	}
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
