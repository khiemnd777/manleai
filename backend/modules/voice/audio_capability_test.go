package voice

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/manleai/ai-receptionist/internal/config"
)

type audioCapabilityTestStore struct {
	*fakeVoiceStore
	metadataReads int
	contentReads  int
	saveExpiresAt time.Time
}

func (s *audioCapabilityTestStore) SaveAudioOutput(_ context.Context, record AudioOutputRecord) (*AudioOutput, error) {
	s.audio = &AudioOutput{
		ID:             "audio_1",
		SalonID:        record.SalonID,
		CallSessionID:  record.CallSessionID,
		Provider:       record.Provider,
		ProviderCallID: record.ProviderCallID,
		ContentType:    record.ContentType,
		Audio:          append([]byte(nil), record.Audio...),
		ExpiresAt:      s.saveExpiresAt,
	}
	return s.audio, nil
}

func (s *audioCapabilityTestStore) GetAudioOutputMetadata(_ context.Context, id string) (*AudioOutput, error) {
	s.metadataReads++
	if s.audio == nil || s.audio.ID != id {
		return nil, ErrNotFound
	}
	copyValue := *s.audio
	copyValue.Audio = nil
	return &copyValue, nil
}

func (s *audioCapabilityTestStore) GetAudioOutputContent(_ context.Context, id string) (*AudioOutput, error) {
	s.contentReads++
	if s.audio == nil || s.audio.ID != id {
		return nil, ErrNotFound
	}
	return &AudioOutput{ID: s.audio.ID, ContentType: s.audio.ContentType, Audio: append([]byte(nil), s.audio.Audio...)}, nil
}

type audioCapabilityConfigResolver struct {
	token         string
	publicBaseURL string
	storedErr     error
}

type audioCapabilityTTS struct{}

func (audioCapabilityTTS) Name() string { return ProviderOpenAI }

func (audioCapabilityTTS) Configured(context.Context, string) bool { return true }

func (audioCapabilityTTS) ContentType() string { return "audio/mpeg" }

func (audioCapabilityTTS) Synthesize(context.Context, string, string, string) ([]byte, error) {
	return []byte("audio-bytes"), nil
}

func (r *audioCapabilityConfigResolver) ResolveTwilioConfig(context.Context, string) (config.TwilioVoiceConfig, string, error) {
	return config.TwilioVoiceConfig{}, r.publicBaseURL, nil
}

func (r *audioCapabilityConfigResolver) ResolveStoredTwilioAuthToken(context.Context, string) (string, error) {
	return r.token, r.storedErr
}

func (r *audioCapabilityConfigResolver) ResolveOpenAIConfig(context.Context, string) (config.OpenAIVoiceConfig, bool, error) {
	return config.OpenAIVoiceConfig{}, false, nil
}

func TestAudioCapabilityAllowsRepeatedFetchWithinDatabaseTTL(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	output := testAudioCapabilityOutput(now.Add(10 * time.Minute))
	store := &audioCapabilityTestStore{fakeVoiceStore: newFakeVoiceStore()}
	store.audio = output
	resolver := &audioCapabilityConfigResolver{token: "database-twilio-token", publicBaseURL: "https://voice.example.com"}
	service := NewService(store, newFakeConversationEngine(), config.VoiceConfig{}, AIProviders{})
	service.SetConfigResolver(resolver)
	service.now = func() time.Time { return now }
	expires := output.ExpiresAt.Unix()
	signature := signAudioCapability(*output, expires, resolver.token)

	for attempt := 0; attempt < 2; attempt++ {
		loaded, err := service.Audio(context.Background(), output.ID, formatUnix(expires), signature)
		if err != nil {
			t.Fatalf("attempt %d: Audio returned error: %v", attempt+1, err)
		}
		if string(loaded.Audio) != "audio-bytes" || loaded.ContentType != "audio/mpeg" {
			t.Fatalf("attempt %d: output=%#v", attempt+1, loaded)
		}
	}
	if store.contentReads != 2 {
		t.Fatalf("content reads=%d, want repeated provider fetches to remain valid", store.contentReads)
	}
}

func TestAudioCapabilityRejectsInvalidRequestsBeforeLoadingBytes(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	token := "database-twilio-token"
	tests := []struct {
		name      string
		mutate    func(*AudioOutput)
		id        string
		expires   func(*AudioOutput) int64
		signature func(*AudioOutput, int64) string
	}{
		{name: "missing id", id: "", expires: databaseExpiry, signature: capabilitySignature(token)},
		{name: "tampered id", id: "audio_other", expires: databaseExpiry, signature: capabilitySignature(token)},
		{name: "missing expiry", id: "audio_1", expires: func(*AudioOutput) int64 { return 0 }, signature: capabilitySignature(token)},
		{name: "tampered expiry", id: "audio_1", expires: func(output *AudioOutput) int64 { return output.ExpiresAt.Add(-time.Minute).Unix() }, signature: func(output *AudioOutput, _ int64) string {
			return signAudioCapability(*output, output.ExpiresAt.Unix(), token)
		}},
		{name: "missing signature", id: "audio_1", expires: databaseExpiry, signature: func(*AudioOutput, int64) string { return "" }},
		{name: "tampered signature", id: "audio_1", expires: databaseExpiry, signature: func(*AudioOutput, int64) string { return strings.Repeat("A", 43) }},
		{name: "expired database row", id: "audio_1", mutate: func(output *AudioOutput) { output.ExpiresAt = now.Add(-time.Second) }, expires: databaseExpiry, signature: capabilitySignature(token)},
		{name: "expiry exceeds database row", id: "audio_1", expires: func(output *AudioOutput) int64 { return output.ExpiresAt.Add(time.Second).Unix() }, signature: capabilitySignature(token)},
		{name: "expiry exceeds maximum ttl", id: "audio_1", mutate: func(output *AudioOutput) { output.ExpiresAt = now.Add(20 * time.Minute) }, expires: func(*AudioOutput) int64 { return now.Add(16 * time.Minute).Unix() }, signature: capabilitySignature(token)},
		{name: "wrong audio binding", id: "audio_2", mutate: func(output *AudioOutput) { output.ID = "audio_2" }, expires: databaseExpiry, signature: func(output *AudioOutput, expires int64) string {
			original := *output
			original.ID = "audio_1"
			return signAudioCapability(original, expires, token)
		}},
		{name: "wrong salon binding", id: "audio_1", mutate: func(output *AudioOutput) { output.SalonID = "salon_2" }, expires: databaseExpiry, signature: func(output *AudioOutput, expires int64) string {
			original := *output
			original.SalonID = "salon_1"
			return signAudioCapability(original, expires, token)
		}},
		{name: "wrong provider binding", id: "audio_1", mutate: func(output *AudioOutput) { output.Provider = "other-provider" }, expires: databaseExpiry, signature: func(output *AudioOutput, expires int64) string {
			original := *output
			original.Provider = ProviderTwilio
			return signAudioCapability(original, expires, token)
		}},
		{name: "wrong provider call binding", id: "audio_1", mutate: func(output *AudioOutput) { output.ProviderCallID = "CA-other" }, expires: databaseExpiry, signature: func(output *AudioOutput, expires int64) string {
			original := *output
			original.ProviderCallID = "CA123"
			return signAudioCapability(original, expires, token)
		}},
		{name: "wrong session binding", id: "audio_1", mutate: func(output *AudioOutput) { output.CallSessionID = "session_2" }, expires: databaseExpiry, signature: func(output *AudioOutput, expires int64) string {
			original := *output
			original.CallSessionID = "session_1"
			return signAudioCapability(original, expires, token)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := testAudioCapabilityOutput(now.Add(10 * time.Minute))
			if test.mutate != nil {
				test.mutate(output)
			}
			store := &audioCapabilityTestStore{fakeVoiceStore: newFakeVoiceStore()}
			store.audio = output
			service := NewService(store, newFakeConversationEngine(), config.VoiceConfig{}, AIProviders{})
			service.SetConfigResolver(&audioCapabilityConfigResolver{token: token})
			service.now = func() time.Time { return now }
			expiresAt := test.expires(output)
			expires := formatUnix(expiresAt)
			if test.name == "missing expiry" {
				expires = ""
			}
			_, err := service.Audio(context.Background(), test.id, expires, test.signature(output, expiresAt))
			if !errors.Is(err, ErrAudioUnavailable) {
				t.Fatalf("error=%v, want uniform ErrAudioUnavailable", err)
			}
			if store.contentReads != 0 {
				t.Fatalf("content reads=%d, bytes must not load before authorization", store.contentReads)
			}
		})
	}
}

func TestAudioCapabilityTokenRotationInvalidatesOutstandingURL(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	output := testAudioCapabilityOutput(now.Add(10 * time.Minute))
	store := &audioCapabilityTestStore{fakeVoiceStore: newFakeVoiceStore()}
	store.audio = output
	resolver := &audioCapabilityConfigResolver{token: "rotated-token"}
	service := NewService(store, newFakeConversationEngine(), config.VoiceConfig{}, AIProviders{})
	service.SetConfigResolver(resolver)
	service.now = func() time.Time { return now }
	signature := signAudioCapability(*output, output.ExpiresAt.Unix(), "previous-token")

	_, err := service.Audio(context.Background(), output.ID, formatUnix(output.ExpiresAt.Unix()), signature)
	if !errors.Is(err, ErrAudioUnavailable) || store.contentReads != 0 {
		t.Fatalf("rotation error/content reads=%v/%d", err, store.contentReads)
	}
}

func TestAudioCapabilityURLUsesOnlyExpiryAndSignature(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	output := testAudioCapabilityOutput(now.Add(10 * time.Minute))
	resolver := &audioCapabilityConfigResolver{token: "database-twilio-token", publicBaseURL: "https://voice.example.com"}
	service := NewService(newFakeVoiceStore(), newFakeConversationEngine(), config.VoiceConfig{}, AIProviders{})
	service.SetConfigResolver(resolver)
	service.now = func() time.Time { return now }

	rawURL := service.audioURL(context.Background(), output)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if parsed.Path != "/api/voice/audio/audio_1" {
		t.Fatalf("path=%q", parsed.Path)
	}
	query := parsed.Query()
	if len(query) != 2 || query.Get("expires") == "" || query.Get("signature") == "" {
		t.Fatalf("query keys=%v", query)
	}
	if !verifyAudioCapability(*output, output.ExpiresAt.Unix(), resolver.token, query.Get("signature")) {
		t.Fatal("generated signature did not verify")
	}
	for _, forbidden := range []string{resolver.token, output.SalonID, output.CallSessionID, output.Provider, output.ProviderCallID, "+13125550101"} {
		if strings.Contains(rawURL, forbidden) {
			t.Fatalf("URL leaked bound secret or identity %q: %s", forbidden, rawURL)
		}
	}

	service.SetConfigResolver(&audioCapabilityConfigResolver{publicBaseURL: "https://voice.example.com"})
	if unsigned := service.audioURL(context.Background(), output); unsigned != "" {
		t.Fatalf("missing stored signing token produced URL %q", unsigned)
	}
	service.SetConfigResolver(nil)
	if unsigned := service.audioURL(context.Background(), output); unsigned != "" {
		t.Fatalf("missing resolver produced URL %q", unsigned)
	}
}

func TestPhoneReplyUsesSignedAudioAndFallsBackSafelyWithoutStoredToken(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	store := &audioCapabilityTestStore{fakeVoiceStore: newFakeVoiceStore(), saveExpiresAt: now.Add(10 * time.Minute)}
	resolver := &audioCapabilityConfigResolver{token: "database-twilio-token", publicBaseURL: "https://voice.example.com"}
	service := NewService(store, newFakeConversationEngine(), config.VoiceConfig{}, AIProviders{TTS: audioCapabilityTTS{}})
	service.SetConfigResolver(resolver)
	service.now = func() time.Time { return now }
	session := phoneSessionWithAIReply("Your request is ready for owner review.", "active", "pending_owner_review")

	reply := service.buildReply(context.Background(), CallReply{Message: "Your request is ready for owner review.", Continue: true}, session, ProviderTwilio, "CA123")
	if reply.AudioURL == "" || store.audio == nil || store.audio.Provider != ProviderTwilio {
		t.Fatalf("signed reply/audio metadata=%#v/%#v", reply, store.audio)
	}
	parsed, err := url.Parse(reply.AudioURL)
	if err != nil || parsed.Query().Get("expires") == "" || parsed.Query().Get("signature") == "" {
		t.Fatalf("signed reply URL=%q error=%v", reply.AudioURL, err)
	}

	resolver.token = ""
	fallback := service.buildReply(context.Background(), CallReply{Message: "Please wait for the owner.", Continue: true}, session, ProviderTwilio, "CA123")
	if fallback.AudioURL != "" || fallback.Message != "Please wait for the owner." || !fallback.Continue {
		t.Fatalf("fail-safe reply=%#v", fallback)
	}
}

func TestAudioHandlerUsesUniformNonEnumeratingFailures(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	output := testAudioCapabilityOutput(now.Add(10 * time.Minute))
	store := &audioCapabilityTestStore{fakeVoiceStore: newFakeVoiceStore()}
	store.audio = output
	resolver := &audioCapabilityConfigResolver{token: "database-twilio-token"}
	service := NewService(store, newFakeConversationEngine(), config.VoiceConfig{}, AIProviders{})
	service.SetConfigResolver(resolver)
	service.now = func() time.Time { return now }
	app := fiber.New()
	RegisterRoutes(app.Group("/api"), NewHandler(service), "test-jwt-secret")
	validExpiry := formatUnix(output.ExpiresAt.Unix())
	validSignature := signAudioCapability(*output, output.ExpiresAt.Unix(), resolver.token)

	paths := []string{
		"/api/voice/audio",
		"/api/voice/audio/audio_other?expires=" + validExpiry + "&signature=" + validSignature,
		"/api/voice/audio/audio_1",
		"/api/voice/audio/audio_1?expires=" + validExpiry + "&signature=invalid",
	}
	var firstBody string
	for _, path := range paths {
		response, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != fiber.StatusNotFound {
			t.Fatalf("GET %s status=%d body=%s", path, response.StatusCode, body)
		}
		if firstBody == "" {
			firstBody = string(body)
		} else if string(body) != firstBody {
			t.Fatalf("enumerating response for %s: %s != %s", path, body, firstBody)
		}
		if strings.Contains(string(body), output.ID) || strings.Contains(string(body), resolver.token) {
			t.Fatalf("failure exposed identity/secret: %s", body)
		}
	}

	validPath := "/api/voice/audio/audio_1?expires=" + validExpiry + "&signature=" + validSignature
	response, err := app.Test(httptest.NewRequest("GET", validPath, nil))
	if err != nil {
		t.Fatalf("valid GET: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != fiber.StatusOK || string(body) != "audio-bytes" || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("valid response status/body/cache=%d/%q/%q", response.StatusCode, body, response.Header.Get("Cache-Control"))
	}
}

func testAudioCapabilityOutput(expiresAt time.Time) *AudioOutput {
	return &AudioOutput{
		ID:             "audio_1",
		SalonID:        "salon_1",
		CallSessionID:  "session_1",
		Provider:       ProviderTwilio,
		ProviderCallID: "CA123",
		ContentType:    "audio/mpeg",
		Audio:          []byte("audio-bytes"),
		ExpiresAt:      expiresAt,
	}
}

func databaseExpiry(output *AudioOutput) int64 {
	return output.ExpiresAt.Unix()
}

func capabilitySignature(token string) func(*AudioOutput, int64) string {
	return func(output *AudioOutput, expiresAt int64) string {
		return signAudioCapability(*output, expiresAt, token)
	}
}

func formatUnix(value int64) string {
	return strconv.FormatInt(value, 10)
}
