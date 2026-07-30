package twiliowebhook

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"sort"
	"testing"
)

func TestVerifyFormCoversEveryReceivedParameterAndExactURL(t *testing.T) {
	const token = "tenant-auth-token"
	const callbackURL = "https://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/incoming"
	params := map[string]string{
		"AccountSid":      "AC00000000000000000000000000000000",
		"CallSid":         "CA00000000000000000000000000000000",
		"FutureParameter": "future-value",
		"To":              "+13125550123",
	}
	signature := signatureForTest(token, callbackURL, params)
	if !VerifyForm(token, callbackURL, params, signature) {
		t.Fatal("valid form signature rejected")
	}
	params["FutureParameter"] = "changed"
	if VerifyForm(token, callbackURL, params, signature) {
		t.Fatal("changed form parameter accepted")
	}
	params["FutureParameter"] = "future-value"
	if VerifyForm(token, "http://api.example.com/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/incoming", params, signature) {
		t.Fatal("signature for a different scheme accepted")
	}
}

func signatureForTest(token, callbackURL string, params map[string]string) string {
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
