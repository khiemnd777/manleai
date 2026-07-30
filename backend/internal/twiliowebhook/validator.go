package twiliowebhook

import (
	"strings"

	"github.com/twilio/twilio-go/client"
)

// VerifyForm verifies the exact externally configured callback URL together
// with every received form parameter. The Auth Token and signature are never
// retained or exposed by this package.
func VerifyForm(authToken, callbackURL string, params map[string]string, signature string) bool {
	authToken = strings.TrimSpace(authToken)
	callbackURL = strings.TrimSpace(callbackURL)
	signature = strings.TrimSpace(signature)
	if authToken == "" || callbackURL == "" || signature == "" {
		return false
	}
	validator := client.NewRequestValidator(authToken)
	return validator.Validate(callbackURL, params, signature)
}
