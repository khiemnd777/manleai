package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv          string
	ServerPort      string
	DatabaseURL     string
	RedisURL        string
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	EncryptionKey   string
	CORSOrigins     []string
	FrontendURL     string
	AutoMigrate     bool
	Square          SquareConfig
	Voice           VoiceConfig
}

type SquareConfig struct {
	Environment  string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	APIVersion   string
	APIBaseURL   string
}

type VoiceConfig struct {
	Provider      string
	PublicBaseURL string
	Twilio        TwilioVoiceConfig
	AI            VoiceAIConfig
}

type TwilioVoiceConfig struct {
	AuthToken      string
	IncomingPath   string
	TurnPath       string
	RecordingPath  string
	StreamPath     string
	VoiceTransport string
}

type VoiceAIConfig struct {
	Provider string
	OpenAI   OpenAIVoiceConfig
}

type OpenAIVoiceConfig struct {
	APIKey               string
	BaseURL              string
	TranscriptionModel   string
	ReplyModel           string
	SpeechModel          string
	SpeechVoice          string
	SpeechOutputMode     string
	RealtimeEnabled      bool
	RealtimeModel        string
	RealtimeVoice        string
	RealtimeNoiseProfile string
	RealtimeInstructions string
}

const (
	DefaultOpenAIRealtimeModel         = "gpt-realtime-2"
	LegacyOpenAIRealtimePreviewModel   = "gpt-4o-realtime-preview"
	DefaultOpenAIRealtimeNoiseProfile  = "noisy_salon"
	OpenAISpeechOutputStreamingTTS     = "streaming_tts"
	OpenAISpeechOutputBufferedRealtime = "buffered_realtime"
)

func NormalizeOpenAISpeechOutputMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OpenAISpeechOutputBufferedRealtime:
		return OpenAISpeechOutputBufferedRealtime
	default:
		return OpenAISpeechOutputStreamingTTS
	}
}

func NormalizeOpenAIRealtimeModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || model == LegacyOpenAIRealtimePreviewModel {
		return DefaultOpenAIRealtimeModel
	}
	return model
}

func NormalizeOpenAIRealtimeNoiseProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "quiet_room", "balanced", "noisy_salon":
		return strings.ToLower(strings.TrimSpace(profile))
	default:
		return DefaultOpenAIRealtimeNoiseProfile
	}
}

func Load() Config {
	return Config{
		AppEnv:          env("APP_ENV", "local"),
		ServerPort:      env("SERVER_PORT", "8080"),
		DatabaseURL:     env("DATABASE_URL", "postgres://ai_receptionist:ai_receptionist@localhost:55432/ai_receptionist?sslmode=disable"),
		RedisURL:        env("REDIS_URL", "redis://localhost:56379/0"),
		JWTSecret:       env("JWT_SECRET", "local-development-secret-change-me"),
		AccessTokenTTL:  time.Duration(envInt("ACCESS_TOKEN_TTL_MINUTES", 30)) * time.Minute,
		RefreshTokenTTL: time.Duration(envInt("REFRESH_TOKEN_TTL_HOURS", 720)) * time.Hour,
		EncryptionKey:   env("TOKEN_ENCRYPTION_KEY_BASE64", "local-development-token-encryption-key"),
		CORSOrigins:     splitCSV(env("CORS_ALLOWED_ORIGINS", "http://localhost:3088")),
		FrontendURL:     env("FRONTEND_URL", "http://localhost:3088"),
		AutoMigrate:     envBool("AUTO_MIGRATE", true),
		Square: SquareConfig{
			Environment:  env("SQUARE_ENVIRONMENT", "sandbox"),
			ClientID:     env("SQUARE_CLIENT_ID", ""),
			ClientSecret: env("SQUARE_CLIENT_SECRET", ""),
			RedirectURL:  env("SQUARE_REDIRECT_URL", "http://localhost:18089/api/integrations/square/callback"),
			APIVersion:   env("SQUARE_API_VERSION", "2026-05-20"),
			APIBaseURL:   env("SQUARE_API_BASE_URL", ""),
		},
		Voice: VoiceConfig{
			Provider:      env("VOICE_PROVIDER", "twilio"),
			PublicBaseURL: strings.TrimRight(env("VOICE_PUBLIC_BASE_URL", ""), "/"),
			Twilio: TwilioVoiceConfig{
				AuthToken:      env("VOICE_TWILIO_AUTH_TOKEN", ""),
				IncomingPath:   env("VOICE_TWILIO_INCOMING_PATH", "/api/voice/twilio/incoming"),
				TurnPath:       env("VOICE_TWILIO_TURN_PATH", "/api/voice/twilio/turn"),
				RecordingPath:  env("VOICE_TWILIO_RECORDING_PATH", "/api/voice/twilio/recording"),
				StreamPath:     env("VOICE_TWILIO_STREAM_PATH", "/api/voice/twilio/stream"),
				VoiceTransport: env("VOICE_TWILIO_VOICE_TRANSPORT", "recording"),
			},
			AI: VoiceAIConfig{
				Provider: env("VOICE_AI_PROVIDER", ""),
				OpenAI: OpenAIVoiceConfig{
					APIKey:               env("VOICE_OPENAI_API_KEY", ""),
					BaseURL:              strings.TrimRight(env("VOICE_OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
					TranscriptionModel:   env("VOICE_OPENAI_TRANSCRIPTION_MODEL", "gpt-4o-mini-transcribe"),
					ReplyModel:           env("VOICE_OPENAI_REPLY_MODEL", "gpt-4.1-mini"),
					SpeechModel:          env("VOICE_OPENAI_SPEECH_MODEL", "tts-1"),
					SpeechVoice:          env("VOICE_OPENAI_SPEECH_VOICE", "alloy"),
					RealtimeEnabled:      envBool("VOICE_OPENAI_REALTIME_ENABLED", false),
					RealtimeModel:        NormalizeOpenAIRealtimeModel(env("VOICE_OPENAI_REALTIME_MODEL", DefaultOpenAIRealtimeModel)),
					RealtimeVoice:        env("VOICE_OPENAI_REALTIME_VOICE", "alloy"),
					RealtimeNoiseProfile: NormalizeOpenAIRealtimeNoiseProfile(env("VOICE_OPENAI_REALTIME_NOISE_PROFILE", DefaultOpenAIRealtimeNoiseProfile)),
					RealtimeInstructions: env("VOICE_OPENAI_REALTIME_INSTRUCTIONS", ""),
				},
			},
		},
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
