package openairuntime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"golang.org/x/crypto/hkdf"
)

const (
	DestinationProfile       = "openai_public"
	DestinationPolicyVersion = "openai-public-v1"
	CanonicalBaseURL         = "https://api.openai.com/v1"
	VerificationContract     = "openai-voice-v1"
	CapabilityTranscription  = "transcription"
	CapabilitySemanticFull   = "semantic_full"
	CapabilitySemanticGuide  = "semantic_guidance"
	CapabilityReply          = "reply"
	CapabilitySpeech         = "speech"
	CapabilitySpeechStream   = "speech_stream"
	CapabilityRealtime       = "realtime"
)

var CapabilityOrder = []string{
	CapabilityTranscription, CapabilitySemanticFull, CapabilitySemanticGuide,
	CapabilityReply, CapabilitySpeech, CapabilitySpeechStream, CapabilityRealtime,
}

var (
	ErrInvalidSalon      = errors.New("OpenAI runtime requires a salon tenant")
	ErrInvalidConfig     = errors.New("OpenAI runtime configuration is invalid")
	ErrDestinationDenied = errors.New("OpenAI destination is not allowed")
)

type ResolvedConfig struct {
	SalonID                       string
	IntegrationConfigID           string
	ConfigVersion                 int64
	CredentialRevision            int64
	CredentialIdentityEstablished bool
	DestinationProfile            string
	Enabled                       bool
	Config                        config.OpenAIVoiceConfig
}

type Resolver interface {
	ResolveOpenAIRuntimeConfig(context.Context, string) (ResolvedConfig, error)
}

type ValidationResult struct {
	Ready    bool
	Blockers []string
}

// Validate is the single readiness contract shared by writes, runtime
// resolution, status surfaces, and live-verification preflight.
func Validate(resolved ResolvedConfig) ValidationResult {
	blockers := make([]string, 0, 8)
	if strings.TrimSpace(resolved.SalonID) == "" {
		blockers = append(blockers, "tenant_required")
	}
	if !resolved.Enabled {
		blockers = append(blockers, "integration_disabled")
	}
	if strings.TrimSpace(resolved.IntegrationConfigID) == "" {
		blockers = append(blockers, "integration_config_missing")
	}
	if resolved.ConfigVersion <= 0 {
		blockers = append(blockers, "config_version_missing")
	}
	if resolved.CredentialRevision <= 0 {
		blockers = append(blockers, "credential_revision_missing")
	}
	if !resolved.CredentialIdentityEstablished {
		blockers = append(blockers, "credential_identity_missing")
	}
	if strings.TrimSpace(resolved.Config.APIKey) == "" {
		blockers = append(blockers, "api_key_missing")
	}
	if strings.TrimSpace(resolved.DestinationProfile) != DestinationProfile || ValidateBaseURL(resolved.Config.BaseURL) != nil {
		blockers = append(blockers, "destination_policy_invalid")
	}
	if strings.TrimSpace(resolved.Config.TranscriptionModel) == "" {
		blockers = append(blockers, "transcription_model_missing")
	}
	if strings.TrimSpace(resolved.Config.ReplyModel) == "" {
		blockers = append(blockers, "reply_model_missing")
	}
	if strings.TrimSpace(resolved.Config.SpeechModel) == "" || strings.TrimSpace(resolved.Config.SpeechVoice) == "" {
		blockers = append(blockers, "speech_config_missing")
	}
	speechOutputMode := config.NormalizeOpenAISpeechOutputMode(resolved.Config.SpeechOutputMode)
	if speechOutputMode == config.OpenAISpeechOutputBufferedRealtime && !resolved.Config.RealtimeEnabled {
		blockers = append(blockers, "speech_output_dependency_missing")
	}
	if resolved.Config.RealtimeEnabled && (strings.TrimSpace(resolved.Config.RealtimeModel) == "" || strings.TrimSpace(resolved.Config.RealtimeVoice) == "") {
		blockers = append(blockers, "realtime_config_missing")
	}
	return ValidationResult{Ready: len(blockers) == 0, Blockers: blockers}
}

func ValidateBaseURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "api.openai.com") || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.TrimRight(parsed.EscapedPath(), "/") != "/v1" {
		return ErrDestinationDenied
	}
	return nil
}

func CredentialFingerprint(rootSecret, apiKey string) (string, error) {
	rootSecret = strings.TrimSpace(rootSecret)
	apiKey = strings.TrimSpace(apiKey)
	if rootSecret == "" || apiKey == "" {
		return "", ErrInvalidConfig
	}
	reader := hkdf.New(sha256.New, []byte(rootSecret), []byte("manleai/openai-credential-identity/v1"), []byte("salon-integration-config"))
	key := make([]byte, sha256.Size)
	if _, err := io.ReadFull(reader, key); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(apiKey))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func ConfigIdentity(resolved ResolvedConfig, schemaIdentity string) string {
	value := strings.Join([]string{
		strings.TrimSpace(resolved.SalonID),
		fmt.Sprintf("%d", resolved.ConfigVersion),
		fmt.Sprintf("%d", resolved.CredentialRevision),
		DestinationPolicyVersion,
		strings.TrimSpace(resolved.Config.ReplyModel),
		strings.TrimSpace(schemaIdentity),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:12])
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = SafeDialContext
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("OpenAI redirects are disabled")
		},
	}
}

func SafeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), "api.openai.com") || port != "443" {
		return nil, ErrDestinationDenied
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	for _, candidate := range addresses {
		if unsafeIP(candidate.IP) {
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	if err == nil {
		err = ErrDestinationDenied
	}
	return nil, err
}

func unsafeIP(ip net.IP) bool {
	return ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
