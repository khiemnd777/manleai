package config

import (
	"errors"
	"testing"
)

func TestRateLimitDefaultsFailClosedInProductionAndRemainOptInLocally(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENV", "production")
	t.Setenv("APP_ENV", "production")
	t.Setenv("RATE_LIMIT_ENABLED", "")
	production := Load()
	if !production.RateLimitEnabled || production.RateLimitClientIPHeader != "X-ManleAI-Client-IP" {
		t.Fatalf("production rate-limit config=%#v", production)
	}
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	if !Load().RateLimitEnabled {
		t.Fatal("production rate limiting must not be disabled by environment override")
	}

	t.Setenv("DEPLOYMENT_ENV", "local")
	t.Setenv("APP_ENV", "local")
	t.Setenv("RATE_LIMIT_ENABLED", "")
	local := Load()
	if local.RateLimitEnabled {
		t.Fatalf("local rate-limit config=%#v", local)
	}

	t.Setenv("RATE_LIMIT_ENABLED", "true")
	if !Load().RateLimitEnabled {
		t.Fatal("explicit local rate limiting was ignored")
	}
}

func TestDatabaseRLSDefaultsFailClosedInProductionAndRemainOptInLocally(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENV", "production")
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_RLS_ENFORCED", "false")
	if !Load().DatabaseRLSEnforced {
		t.Fatal("production RLS enforcement must not be disabled by environment override")
	}

	t.Setenv("DEPLOYMENT_ENV", "local")
	t.Setenv("APP_ENV", "local")
	t.Setenv("DATABASE_RLS_ENFORCED", "")
	if Load().DatabaseRLSEnforced {
		t.Fatal("local RLS enforcement must remain opt-in")
	}
	t.Setenv("DATABASE_RLS_ENFORCED", "true")
	if !Load().DatabaseRLSEnforced {
		t.Fatal("explicit local RLS enforcement was ignored")
	}
}

func TestEnvironmentDefaultsAndLocalProductionSimulation(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENV", "")
	t.Setenv("APP_ENV", "")
	local := Load()
	if local.DeploymentEnv != EnvironmentLocal || local.AppEnv != EnvironmentLocal {
		t.Fatalf("default environment=%q behavior=%q", local.DeploymentEnv, local.AppEnv)
	}
	if err := local.ValidateEnvironment(); err != nil {
		t.Fatalf("validate default local environment: %v", err)
	}

	t.Setenv("DEPLOYMENT_ENV", "local")
	t.Setenv("APP_ENV", "production")
	simulation := Load()
	if simulation.DeploymentEnv != EnvironmentLocal || !simulation.IsProductionBehavior() || simulation.IsProductionDeployment() {
		t.Fatalf("simulation environment=%q behavior=%q", simulation.DeploymentEnv, simulation.AppEnv)
	}
	if simulation.DatabaseRLSEnforced || simulation.RateLimitEnabled {
		t.Fatalf("local simulation unexpectedly enabled deployment-only infrastructure safeguards: %#v", simulation)
	}
	if err := simulation.ValidateEnvironment(); err != nil {
		t.Fatalf("validate local production simulation: %v", err)
	}
}

func TestEnvironmentValidationFailsClosedForInvalidOrDowngradedProduction(t *testing.T) {
	tests := []struct {
		name       string
		deployment string
		behavior   string
		want       error
	}{
		{name: "invalid deployment", deployment: "staging", behavior: "local", want: ErrDeploymentEnvironmentInvalid},
		{name: "invalid behavior", deployment: "local", behavior: "staging", want: ErrApplicationEnvironmentInvalid},
		{name: "production downgrade", deployment: "production", behavior: "local", want: ErrProductionEnvironmentMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DEPLOYMENT_ENV", test.deployment)
			t.Setenv("APP_ENV", test.behavior)
			if err := Load().ValidateEnvironment(); !errors.Is(err, test.want) {
				t.Fatalf("ValidateEnvironment() error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestApplicationEnvironmentAutoInheritsDeploymentEnvironment(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENV", "production")
	t.Setenv("APP_ENV", "auto")
	cfg := Load()
	if cfg.AppEnv != EnvironmentProduction || !cfg.IsProductionDeployment() {
		t.Fatalf("auto environment=%q behavior=%q", cfg.DeploymentEnv, cfg.AppEnv)
	}
	if err := cfg.ValidateEnvironment(); err != nil {
		t.Fatalf("validate inherited production environment: %v", err)
	}
}

func TestNormalizeOpenAIRealtimeModelDefaultsAndMigratesLegacyPreview(t *testing.T) {
	if got := NormalizeOpenAIRealtimeModel(""); got != DefaultOpenAIRealtimeModel {
		t.Fatalf("blank model = %q, want %q", got, DefaultOpenAIRealtimeModel)
	}
	if got := NormalizeOpenAIRealtimeModel(LegacyOpenAIRealtimePreviewModel); got != DefaultOpenAIRealtimeModel {
		t.Fatalf("legacy preview model = %q, want %q", got, DefaultOpenAIRealtimeModel)
	}
	if got := NormalizeOpenAIRealtimeModel("gpt-realtime-1.5"); got != "gpt-realtime-1.5" {
		t.Fatalf("explicit model = %q", got)
	}
}

func TestNormalizeOpenAIRealtimeNoiseProfileUsesLocationNeutralPolicies(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "blank defaults to automatic", want: OpenAIRealtimeNoiseAutomatic},
		{name: "automatic remains canonical", value: "automatic", want: OpenAIRealtimeNoiseAutomatic},
		{name: "standard remains canonical", value: "standard", want: OpenAIRealtimeNoiseStandard},
		{name: "strong remains canonical", value: "strong_noise_rejection", want: OpenAIRealtimeNoiseStrongRejection},
		{name: "minimal remains canonical", value: "minimal_processing", want: OpenAIRealtimeNoiseMinimal},
		{name: "legacy noisy salon preserves strong behavior", value: "noisy_salon", want: OpenAIRealtimeNoiseStrongRejection},
		{name: "legacy balanced preserves standard behavior", value: "balanced", want: OpenAIRealtimeNoiseStandard},
		{name: "legacy quiet room preserves minimal behavior", value: "quiet_room", want: OpenAIRealtimeNoiseMinimal},
		{name: "unknown fails safe to automatic", value: "somewhere_loud", want: OpenAIRealtimeNoiseAutomatic},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeOpenAIRealtimeNoiseProfile(test.value); got != test.want {
				t.Fatalf("NormalizeOpenAIRealtimeNoiseProfile(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
