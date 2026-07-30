package notificationtwilio

import "github.com/manleai/ai-receptionist/internal/twiliowebhook"

// VerifySignature validates the exact configured webhook URL together with all
// form parameters. Callers must pass the parameters unchanged so newly added
// Twilio fields remain covered by the signature.
func VerifySignature(authToken, callbackURL string, params map[string]string, signature string) bool {
	return twiliowebhook.VerifyForm(authToken, callbackURL, params, signature)
}

func verifySignature(authToken, callbackURL string, params map[string]string, signature string) bool {
	return VerifySignature(authToken, callbackURL, params, signature)
}
