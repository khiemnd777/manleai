package main

import (
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
)

func TestAPICORSExposesIdempotentReplayHeader(t *testing.T) {
	cfg := apiCORSConfig(config.Config{CORSOrigins: []string{"https://ai.example.com"}})
	if cfg.AllowOrigins != "https://ai.example.com" {
		t.Fatalf("allow origins = %q", cfg.AllowOrigins)
	}
	if !cfg.AllowCredentials {
		t.Fatal("browser refresh-cookie transport requires credentialed CORS")
	}
	exposed := map[string]bool{}
	for _, header := range strings.Split(cfg.ExposeHeaders, ",") {
		exposed[strings.ToLower(strings.TrimSpace(header))] = true
	}
	for _, required := range []string{"x-idempotent-replay", "ratelimit-limit", "ratelimit-remaining", "ratelimit-reset", "retry-after"} {
		if !exposed[required] {
			t.Fatalf("exposed headers = %q, missing %s", cfg.ExposeHeaders, required)
		}
	}
}
