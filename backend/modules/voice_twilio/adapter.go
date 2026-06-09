package voice_twilio

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/xml"
	"sort"
	"strings"

	"github.com/manleai/ai-receptionist/internal/config"
)

type Adapter struct {
	cfg           config.TwilioVoiceConfig
	publicBaseURL string
}

func NewAdapter(cfg config.TwilioVoiceConfig, publicBaseURL string) *Adapter {
	return &Adapter{
		cfg:           cfg,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

func (a *Adapter) Configured() bool {
	return strings.TrimSpace(a.cfg.AuthToken) != ""
}

func (a *Adapter) VerifyWebhook(url string, params map[string]string, signature string) bool {
	signature = strings.TrimSpace(signature)
	if !a.Configured() || signature == "" {
		return false
	}
	expected := a.ExpectedSignature(url, params)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

func (a *Adapter) ExpectedSignature(url string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	base := url
	for _, key := range keys {
		base += key + params[key]
	}
	mac := hmac.New(sha1.New, []byte(a.cfg.AuthToken))
	_, _ = mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (a *Adapter) TurnURL(fallbackBaseURL string) string {
	return a.urlForPath(a.cfg.TurnPath, fallbackBaseURL)
}

func (a *Adapter) IncomingURL(fallbackBaseURL string) string {
	return a.urlForPath(a.cfg.IncomingPath, fallbackBaseURL)
}

func (a *Adapter) RequestURL(originalURL string, fallbackBaseURL string) string {
	return a.urlForPath(originalURL, fallbackBaseURL)
}

func (a *Adapter) GatherResponse(message string, actionURL string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response><Gather input="speech" action="`)
	writeEscaped(&b, actionURL)
	b.WriteString(`" method="POST" speechTimeout="auto"><Say>`)
	writeEscaped(&b, message)
	b.WriteString(`</Say></Gather><Say>I did not hear anything. Please call the salon again or wait for the owner.</Say><Hangup/></Response>`)
	return b.String()
}

func (a *Adapter) FinalResponse(message string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response><Say>`)
	writeEscaped(&b, message)
	b.WriteString(`</Say><Hangup/></Response>`)
	return b.String()
}

func (a *Adapter) urlForPath(path string, fallbackBaseURL string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	baseURL := a.publicBaseURL
	if baseURL == "" {
		baseURL = strings.TrimRight(fallbackBaseURL, "/")
	}
	if baseURL == "" {
		return path
	}
	return baseURL + "/" + strings.TrimLeft(path, "/")
}

func writeEscaped(b *strings.Builder, value string) {
	_ = xml.EscapeText(b, []byte(value))
}
