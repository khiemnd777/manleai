package voice_twilio

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
)

type Adapter struct {
	cfg           config.TwilioVoiceConfig
	publicBaseURL string
	httpClient    *http.Client
}

func NewAdapter(cfg config.TwilioVoiceConfig, publicBaseURL string) *Adapter {
	return &Adapter{
		cfg:           cfg,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *Adapter) WithConfig(cfg config.TwilioVoiceConfig, publicBaseURL string) *Adapter {
	return &Adapter{
		cfg:           cfg,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		httpClient:    a.httpClient,
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

func (a *Adapter) RecordingURL(fallbackBaseURL string) string {
	return a.urlForPath(a.cfg.RecordingPath, fallbackBaseURL)
}

func (a *Adapter) IncomingURL(fallbackBaseURL string) string {
	return a.urlForPath(a.cfg.IncomingPath, fallbackBaseURL)
}

func (a *Adapter) RequestURL(originalURL string, fallbackBaseURL string) string {
	return a.urlForPath(originalURL, fallbackBaseURL)
}

func (a *Adapter) GatherResponse(message string, actionURL string, audioURL string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response><Gather input="speech" action="`)
	writeEscaped(&b, actionURL)
	b.WriteString(`" method="POST" speechTimeout="auto">`)
	writePrompt(&b, message, audioURL)
	b.WriteString(`</Gather><Say>I did not hear anything. Please call the salon again or wait for the owner.</Say><Hangup/></Response>`)
	return b.String()
}

func (a *Adapter) RecordResponse(message string, actionURL string, audioURL string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response>`)
	writePrompt(&b, message, audioURL)
	b.WriteString(`<Record action="`)
	writeEscaped(&b, actionURL)
	b.WriteString(`" method="POST" maxLength="30" timeout="5" trim="trim-silence" playBeep="false"/>`)
	b.WriteString(`<Say>I did not hear anything. Please call the salon again or wait for the owner.</Say><Hangup/></Response>`)
	return b.String()
}

func (a *Adapter) FinalResponse(message string, audioURL string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response>`)
	writePrompt(&b, message, audioURL)
	b.WriteString(`<Hangup/></Response>`)
	return b.String()
}

func (a *Adapter) FetchRecording(ctx context.Context, recordingURL string, accountSID string) ([]byte, string, error) {
	recordingURL = strings.TrimSpace(recordingURL)
	if recordingURL == "" {
		return nil, "", errors.New("recording url is empty")
	}
	if !strings.HasSuffix(recordingURL, ".wav") && !strings.HasSuffix(recordingURL, ".mp3") {
		recordingURL += ".wav"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, recordingURL, nil)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(accountSID) != "" && strings.TrimSpace(a.cfg.AuthToken) != "" {
		req.SetBasicAuth(strings.TrimSpace(accountSID), strings.TrimSpace(a.cfg.AuthToken))
	}
	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", errors.New("recording download failed")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 10*1024*1024))
	if err != nil {
		return nil, "", err
	}
	contentType := strings.TrimSpace(res.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "audio/wav"
	}
	return body, contentType, nil
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

func writePrompt(b *strings.Builder, message string, audioURL string) {
	if strings.TrimSpace(audioURL) != "" {
		b.WriteString(`<Play>`)
		writeEscaped(b, audioURL)
		b.WriteString(`</Play>`)
		return
	}
	b.WriteString(`<Say>`)
	writeEscaped(b, message)
	b.WriteString(`</Say>`)
}

func writeEscaped(b *strings.Builder, value string) {
	_ = xml.EscapeText(b, []byte(value))
}
