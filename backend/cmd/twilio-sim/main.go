package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type config struct {
	baseURL          string
	signatureBaseURL string
	authToken        string
	fromPhone        string
	toPhone          string
	callSID          string
	incomingPath     string
	turnPath         string
	turns            []string
}

func main() {
	cfg := parseConfig()
	if err := cfg.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "twilio-sim: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	callParams := url.Values{
		"CallSid": {cfg.callSID},
		"From":    {cfg.fromPhone},
		"To":      {cfg.toPhone},
	}
	if err := postTwilioWebhook(client, cfg, "incoming call", cfg.incomingPath, callParams); err != nil {
		fmt.Fprintf(os.Stderr, "twilio-sim: %v\n", err)
		os.Exit(1)
	}

	for i, turn := range cfg.turns {
		turnParams := url.Values{
			"CallSid":      {cfg.callSID},
			"From":         {cfg.fromPhone},
			"To":           {cfg.toPhone},
			"SpeechResult": {turn},
		}
		label := fmt.Sprintf("speech turn %d", i+1)
		if err := postTwilioWebhook(client, cfg, label, cfg.turnPath, turnParams); err != nil {
			fmt.Fprintf(os.Stderr, "twilio-sim: %v\n", err)
			os.Exit(1)
		}
	}
}

func parseConfig() config {
	defaultBaseURL := env("TWILIO_SIM_BASE_URL", env("VOICE_PUBLIC_BASE_URL", "http://localhost:18089"))
	defaultSignatureBaseURL := env("TWILIO_SIM_SIGNATURE_BASE_URL", env("VOICE_PUBLIC_BASE_URL", defaultBaseURL))
	var rawTurns string
	var repeatedTurns stringList

	cfg := config{}
	flag.StringVar(&cfg.baseURL, "base-url", defaultBaseURL, "API base URL to POST webhooks to")
	flag.StringVar(&cfg.signatureBaseURL, "signature-base-url", defaultSignatureBaseURL, "public base URL the backend uses when verifying Twilio signatures")
	flag.StringVar(&cfg.authToken, "auth-token", env("VOICE_TWILIO_AUTH_TOKEN", ""), "Twilio auth token used to sign webhooks")
	flag.StringVar(&cfg.fromPhone, "from", env("TWILIO_SIM_FROM", "+13125550101"), "customer caller phone number")
	flag.StringVar(&cfg.toPhone, "to", env("TWILIO_SIM_TO", ""), "salon/Twilio phone number routed by the backend")
	flag.StringVar(&cfg.callSID, "call-sid", env("TWILIO_SIM_CALL_SID", fmt.Sprintf("CA_LOCAL_%d", time.Now().Unix())), "Twilio CallSid for the simulated call")
	flag.StringVar(&cfg.incomingPath, "incoming-path", env("VOICE_TWILIO_INCOMING_PATH", "/api/voice/twilio/incoming"), "incoming-call webhook path")
	flag.StringVar(&cfg.turnPath, "turn-path", env("VOICE_TWILIO_TURN_PATH", "/api/voice/twilio/turn"), "speech-turn webhook path")
	flag.StringVar(&rawTurns, "turns", env("TWILIO_SIM_TURNS", ""), "semicolon-separated customer speech turns")
	flag.Var(&repeatedTurns, "turn", "customer speech turn; may be repeated")
	flag.Parse()

	cfg.turns = append(splitTurns(rawTurns), repeatedTurns...)
	return cfg
}

func (c config) validate() error {
	if strings.TrimSpace(c.authToken) == "" {
		return errors.New("-auth-token or VOICE_TWILIO_AUTH_TOKEN is required")
	}
	if strings.TrimSpace(c.toPhone) == "" {
		return errors.New("-to or TWILIO_SIM_TO is required and must match the salon phone Twilio sends as To")
	}
	if strings.TrimSpace(c.fromPhone) == "" {
		return errors.New("-from or TWILIO_SIM_FROM is required")
	}
	if strings.TrimSpace(c.callSID) == "" {
		return errors.New("-call-sid or TWILIO_SIM_CALL_SID is required")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return errors.New("-base-url or TWILIO_SIM_BASE_URL is required")
	}
	if strings.TrimSpace(c.signatureBaseURL) == "" {
		return errors.New("-signature-base-url or TWILIO_SIM_SIGNATURE_BASE_URL is required")
	}
	if strings.TrimSpace(c.incomingPath) == "" {
		return errors.New("-incoming-path or VOICE_TWILIO_INCOMING_PATH is required")
	}
	if strings.TrimSpace(c.turnPath) == "" {
		return errors.New("-turn-path or VOICE_TWILIO_TURN_PATH is required")
	}
	return nil
}

func postTwilioWebhook(client *http.Client, cfg config, label string, path string, form url.Values) error {
	requestURL := joinURL(cfg.baseURL, path)
	signatureURL := joinURL(cfg.signatureBaseURL, path)
	req, err := http.NewRequest(http.MethodPost, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create %s request: %w", label, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", expectedSignature(signatureURL, form, cfg.authToken))

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s webhook: %w", label, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 128*1024))
	if err != nil {
		return fmt.Errorf("read %s response: %w", label, err)
	}

	fmt.Printf("\n== %s ==\n", label)
	fmt.Printf("POST %s\n", requestURL)
	if requestURL != signatureURL {
		fmt.Printf("Signed as %s\n", signatureURL)
	}
	fmt.Printf("Status: %s\n", res.Status)
	fmt.Printf("%s\n", strings.TrimSpace(string(body)))

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("%s webhook returned %s", label, res.Status)
	}
	return nil
}

func expectedSignature(rawURL string, values url.Values, authToken string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	base := rawURL
	for _, key := range keys {
		if len(values[key]) > 0 {
			base += key + values[key][0]
		}
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func joinURL(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return baseURL + "/" + strings.TrimLeft(path, "/")
}

func splitTurns(raw string) []string {
	parts := strings.Split(raw, ";")
	turns := make([]string, 0, len(parts))
	for _, part := range parts {
		turn := strings.TrimSpace(part)
		if turn != "" {
			turns = append(turns, turn)
		}
	}
	return turns
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, "; ")
}

func (s *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*s = append(*s, value)
	}
	return nil
}
