package voice_twilio

import (
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
)

func TestVerifyWebhookAcceptsExpectedSignature(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{AuthToken: "secret"}, "")
	params := map[string]string{
		"CallSid": "CA123",
		"From":    "+13125550101",
		"To":      "+13125550102",
	}
	url := "https://voice.example.com/api/voice/twilio/incoming"
	signature := adapter.ExpectedSignature(url, params)

	if !adapter.VerifyWebhook(url, params, signature) {
		t.Fatalf("VerifyWebhook rejected a valid signature")
	}
}

func TestVerifyWebhookRejectsInvalidSignature(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{AuthToken: "secret"}, "")
	params := map[string]string{"CallSid": "CA123"}

	if adapter.VerifyWebhook("https://voice.example.com/api/voice/twilio/incoming", params, "bad-signature") {
		t.Fatalf("VerifyWebhook accepted an invalid signature")
	}
}

func TestGatherResponseEscapesSpeechText(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{AuthToken: "secret", TurnPath: "/api/voice/twilio/turn"}, "https://voice.example.com")

	body := adapter.GatherResponse("Tom & Linh <confirm>", adapter.TurnURL(""), "")
	if !strings.Contains(body, "Tom &amp; Linh &lt;confirm&gt;") {
		t.Fatalf("GatherResponse did not XML-escape message: %s", body)
	}
	if !strings.Contains(body, `action="https://voice.example.com/api/voice/twilio/turn"`) {
		t.Fatalf("GatherResponse action URL is wrong: %s", body)
	}
}

func TestRecordResponseUsesPlayWhenAudioURLPresent(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{AuthToken: "secret", RecordingPath: "/api/voice/twilio/recording"}, "https://voice.example.com")

	body := adapter.RecordResponse("Fallback text", adapter.RecordingURL(""), "https://voice.example.com/api/voice/audio/audio_1")
	if !strings.Contains(body, "<Play>https://voice.example.com/api/voice/audio/audio_1</Play>") {
		t.Fatalf("RecordResponse should play synthesized audio: %s", body)
	}
	if !strings.Contains(body, `action="https://voice.example.com/api/voice/twilio/recording"`) {
		t.Fatalf("RecordResponse action URL is wrong: %s", body)
	}
	if strings.Contains(body, "<Say>Fallback text</Say>") {
		t.Fatalf("RecordResponse should not include Say prompt when audio is present: %s", body)
	}
}

func TestStreamResponseUsesSignedParametersAndWebSocketURL(t *testing.T) {
	adapter := NewAdapter(config.TwilioVoiceConfig{
		AuthToken:  "secret",
		StreamPath: "/api/voice/twilio/stream",
	}, "https://voice.example.com")
	token := adapter.StreamToken("CA123", "session_phone")

	body := adapter.StreamResponse("Thank you for calling.", adapter.StreamURL(""), "", map[string]string{
		"call_sid":     "CA123",
		"session_id":   "session_phone",
		"stream_token": token,
	})

	if !strings.Contains(body, `<Connect><Stream url="wss://voice.example.com/api/voice/twilio/stream">`) {
		t.Fatalf("StreamResponse should connect to wss stream URL: %s", body)
	}
	if !strings.Contains(body, `name="session_id" value="session_phone"`) || !strings.Contains(body, `name="stream_token"`) {
		t.Fatalf("StreamResponse should include signed custom parameters: %s", body)
	}
	if !adapter.VerifyStreamToken("CA123", "session_phone", token) {
		t.Fatalf("VerifyStreamToken rejected valid token")
	}
	if adapter.VerifyStreamToken("CA123", "other_session", token) {
		t.Fatalf("VerifyStreamToken accepted token for another session")
	}
}
