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

	body := adapter.GatherResponse("Tom & Linh <confirm>", adapter.TurnURL(""))
	if !strings.Contains(body, "Tom &amp; Linh &lt;confirm&gt;") {
		t.Fatalf("GatherResponse did not XML-escape message: %s", body)
	}
	if !strings.Contains(body, `action="https://voice.example.com/api/voice/twilio/turn"`) {
		t.Fatalf("GatherResponse action URL is wrong: %s", body)
	}
}
