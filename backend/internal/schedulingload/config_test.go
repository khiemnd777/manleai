package schedulingload

import (
	"errors"
	"testing"
	"time"
)

func TestConfigNormalizeAndValidateAcceptsDedicatedBoundedTarget(t *testing.T) {
	config, err := validUnitConfig().NormalizeAndValidate()
	if err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if config.RunID == "" || config.BaseTime.IsZero() {
		t.Fatalf("normalized config missing generated identity or base time: %#v", config)
	}
	if got := config.workloadTime(); got.Weekday() != time.Monday || got.Before(config.BaseTime) || got.After(config.BaseTime.Add(21*24*time.Hour)) {
		t.Fatalf("workloadTime() = %v, want deterministic Monday in bounded horizon", got)
	}
}

func TestConfigRejectsUnsafeTargetsAndUnboundedWork(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want error
	}{
		{name: "missing attestation", edit: func(config *Config) { config.Attestation = "" }, want: ErrUnsafeTarget},
		{name: "production name", edit: func(config *Config) { config.ExpectedDatabaseName = "manleai_load_production" }, want: ErrUnsafeTarget},
		{name: "reserved name", edit: func(config *Config) { config.ExpectedDatabaseName = "postgres" }, want: ErrUnsafeTarget},
		{name: "broad prefix", edit: func(config *Config) { config.DatabasePrefix = "load_"; config.ExpectedDatabaseName = "load_test" }, want: ErrUnsafeTarget},
		{name: "too much concurrency", edit: func(config *Config) { config.Concurrency = MaxConcurrency + 1 }, want: ErrInvalidConfig},
		{name: "too many operations", edit: func(config *Config) { config.OperationsPerWorkload = MaxOperationsPerWorkload + 1 }, want: ErrInvalidConfig},
		{name: "too long", edit: func(config *Config) { config.Duration = MaxDuration + time.Second }, want: ErrInvalidConfig},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validUnitConfig()
			test.edit(&config)
			if _, err := config.NormalizeAndValidate(); !errors.Is(err, test.want) {
				t.Fatalf("NormalizeAndValidate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func validUnitConfig() Config {
	return Config{
		DatabaseURL: "postgres://not-used", ExpectedDatabaseName: "manleai_load_unit", ExpectedDatabaseUser: "manleai_load_runner", DatabasePrefix: DefaultDatabasePrefix,
		Attestation: RequiredAttestation, Release: "unit-test", Seed: 3,
		Concurrency: MinConcurrency, OperationsPerWorkload: MinOperationsPerWorkload, Duration: MinDuration,
	}
}
