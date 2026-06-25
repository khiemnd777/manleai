package integrationconfig

import (
	"strings"
	"testing"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/encryption"
)

func TestSquareResponseMasksEncryptedClientSecret(t *testing.T) {
	cipher, err := encryption.NewTokenCipher("test-secret")
	if err != nil {
		t.Fatalf("NewTokenCipher: %v", err)
	}
	service := NewService(nil, cipher, config.Config{
		Square: config.SquareConfig{
			Environment: "sandbox",
			RedirectURL: "https://api.example.com/api/integrations/square/callback",
			APIVersion:  "2026-05-20",
		},
	})
	encrypted := service.mustEncryptSecrets(map[string]string{"client_secret": "square-secret-value"})
	if encrypted == "" || strings.Contains(encrypted, "square-secret-value") {
		t.Fatalf("secret was not encrypted: %q", encrypted)
	}

	updatedAt := time.Now().UTC()
	response := service.squareResponse(&StoredConfig{
		SalonID:  "salon_1",
		Provider: ProviderSquare,
		Enabled:  true,
		Settings: map[string]string{
			"environment":  "sandbox",
			"client_id":    "square-client-id",
			"redirect_url": "https://api.example.com/api/integrations/square/callback",
			"api_version":  "2026-05-20",
		},
		SecretsEncrypted: encrypted,
		UpdatedAt:        updatedAt,
	})

	if !response.Configured || !response.ClientSecretConfigured {
		t.Fatalf("response should report configured secret: %#v", response)
	}
	if response.ClientSecretSource != SecretSourceDatabase {
		t.Fatalf("secret source = %q, want database", response.ClientSecretSource)
	}
	if strings.Contains(response.ClientID+response.RedirectURL+response.APIVersion+response.ClientSecretSource, "square-secret-value") {
		t.Fatalf("response leaked secret: %#v", response)
	}
}
