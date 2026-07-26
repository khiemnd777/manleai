package notificationtwilio

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"sort"
	"strings"
)

// VerifySignature validates the exact configured webhook URL together with all
// form parameters. Callers must pass the parameters unchanged so newly added
// Twilio fields remain covered by the signature.
func VerifySignature(authToken, callbackURL string, params map[string]string, signature string) bool {
	authToken, callbackURL, signature = strings.TrimSpace(authToken), strings.TrimSpace(callbackURL), strings.TrimSpace(signature)
	if authToken == "" || callbackURL == "" || signature == "" {
		return false
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	base := callbackURL
	for _, key := range keys {
		base += key + params[key]
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(base))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

func verifySignature(authToken, callbackURL string, params map[string]string, signature string) bool {
	return VerifySignature(authToken, callbackURL, params, signature)
}
