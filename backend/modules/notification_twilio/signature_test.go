package notificationtwilio

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"sort"
	"testing"
)

func TestVerifySignatureAcceptsEvolvingParameters(t *testing.T) {
	params := map[string]string{"MessageSid": "SM123", "MessageStatus": "delivered", "FutureParameter": "future-value"}
	const token = "auth-token"
	const callbackURL = "https://api.example.com/api/notifications/twilio/status"
	signature := expectedSignatureForTest(token, callbackURL, params)
	if !verifySignature(token, callbackURL, params, signature) {
		t.Fatal("valid signature rejected")
	}
	params["FutureParameter"] = "changed"
	if verifySignature(token, callbackURL, params, signature) {
		t.Fatal("changed callback accepted")
	}
}

func expectedSignatureForTest(token, callbackURL string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	base := callbackURL
	for _, key := range keys {
		base += key + params[key]
	}
	mac := hmac.New(sha1.New, []byte(token))
	_, _ = mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
