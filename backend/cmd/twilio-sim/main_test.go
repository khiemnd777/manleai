package main

import (
	"net/url"
	"testing"

	appconfig "github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/modules/voice_twilio"
)

func TestExpectedSignatureMatchesTwilioAdapter(t *testing.T) {
	form := url.Values{
		"CallSid":      {"CA123"},
		"From":         {"+13125550101"},
		"SpeechResult": {"I need a classic manicure tomorrow."},
		"To":           {"+13125550102"},
	}
	adapter := voice_twilio.NewAdapter(appconfig.TwilioVoiceConfig{AuthToken: "secret"}, "")

	got := expectedSignature("http://localhost:18089/api/voice/twilio/turn", form, "secret")
	want := adapter.ExpectedSignature("http://localhost:18089/api/voice/twilio/turn", map[string]string{
		"CallSid":      "CA123",
		"From":         "+13125550101",
		"SpeechResult": "I need a classic manicure tomorrow.",
		"To":           "+13125550102",
	})

	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}

func TestSplitTurnsTrimsEmptyParts(t *testing.T) {
	got := splitTurns(" I need a manicure. ; ; The first one works. ")
	if len(got) != 2 {
		t.Fatalf("turn count = %d, want 2: %#v", len(got), got)
	}
	if got[0] != "I need a manicure." || got[1] != "The first one works." {
		t.Fatalf("turns = %#v", got)
	}
}
