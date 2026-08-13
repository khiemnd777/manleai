package main

import (
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
)

func TestSampleDatabaseURLUsesPhysicalDeploymentEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantURL string
		wantErr bool
	}{
		{
			name: "local behavior uses local fallback",
			cfg: config.Config{
				DeploymentEnv: config.EnvironmentLocal,
				AppEnv:        config.EnvironmentLocal,
				DatabaseURL:   "postgres://local-runtime",
			},
			wantURL: "postgres://local-runtime",
		},
		{
			name: "production behavior on local still uses local fallback",
			cfg: config.Config{
				DeploymentEnv: config.EnvironmentLocal,
				AppEnv:        config.EnvironmentProduction,
				DatabaseURL:   "postgres://local-runtime",
			},
			wantURL: "postgres://local-runtime",
		},
		{
			name: "production deployment requires migration owner URL",
			cfg: config.Config{
				DeploymentEnv: config.EnvironmentProduction,
				AppEnv:        config.EnvironmentProduction,
				DatabaseURL:   "postgres://production-runtime",
			},
			wantErr: true,
		},
		{
			name: "explicit migration URL wins in production",
			cfg: config.Config{
				DeploymentEnv:        config.EnvironmentProduction,
				AppEnv:               config.EnvironmentProduction,
				DatabaseURL:          "postgres://production-runtime",
				MigrationDatabaseURL: "postgres://production-owner",
			},
			wantURL: "postgres://production-owner",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := sampleDatabaseURL(test.cfg)
			if test.wantErr {
				if err == nil {
					t.Fatalf("sampleDatabaseURL()=%q, want error", got)
				}
				return
			}
			if err != nil || got != test.wantURL {
				t.Fatalf("sampleDatabaseURL()=%q error=%v, want %q", got, err, test.wantURL)
			}
		})
	}
}
