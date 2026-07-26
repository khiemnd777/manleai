package schedulingload

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ReportSchemaVersion      = "scheduling-load-report/v1"
	RequiredAttestation      = "NON_PRODUCTION_ISOLATED_SCHEDULING_LOAD"
	DefaultDatabasePrefix    = "manleai_load_"
	MinConcurrency           = 2
	MaxConcurrency           = 64
	MinOperationsPerWorkload = 2
	MaxOperationsPerWorkload = 1000
	MinDuration              = 5 * time.Second
	MaxDuration              = 10 * time.Minute
)

var (
	ErrUnsafeTarget     = errors.New("scheduling load harness target is not an attested isolated non-production database")
	ErrInvalidConfig    = errors.New("invalid scheduling load harness configuration")
	ErrRunAlreadyExists = errors.New("scheduling load harness run id already exists")
)

type Config struct {
	DatabaseURL           string
	ExpectedDatabaseName  string
	ExpectedDatabaseUser  string
	DatabasePrefix        string
	Attestation           string
	Release               string
	RunID                 string
	Seed                  int64
	Concurrency           int
	OperationsPerWorkload int
	Duration              time.Duration
	BaseTime              time.Time
}

func (config Config) NormalizeAndValidate() (Config, error) {
	config.DatabaseURL = strings.TrimSpace(config.DatabaseURL)
	config.ExpectedDatabaseName = strings.TrimSpace(config.ExpectedDatabaseName)
	config.ExpectedDatabaseUser = strings.TrimSpace(config.ExpectedDatabaseUser)
	config.DatabasePrefix = strings.TrimSpace(config.DatabasePrefix)
	config.Attestation = strings.TrimSpace(config.Attestation)
	config.Release = strings.TrimSpace(config.Release)
	config.RunID = strings.ToLower(strings.TrimSpace(config.RunID))
	if config.DatabasePrefix == "" {
		config.DatabasePrefix = DefaultDatabasePrefix
	}
	if config.RunID == "" {
		config.RunID = uuid.NewString()
	}
	if config.BaseTime.IsZero() {
		config.BaseTime = time.Date(2030, time.January, 7, 14, 0, 0, 0, time.UTC)
	}
	config.BaseTime = config.BaseTime.UTC()

	if config.DatabaseURL == "" || config.ExpectedDatabaseName == "" || config.ExpectedDatabaseUser == "" || config.Release == "" ||
		config.Concurrency < MinConcurrency || config.Concurrency > MaxConcurrency ||
		config.OperationsPerWorkload < MinOperationsPerWorkload || config.OperationsPerWorkload > MaxOperationsPerWorkload ||
		config.Duration < MinDuration || config.Duration > MaxDuration {
		return Config{}, ErrInvalidConfig
	}
	if config.Attestation != RequiredAttestation || config.DatabasePrefix != strings.ToLower(config.DatabasePrefix) ||
		len(config.DatabasePrefix) < len(DefaultDatabasePrefix) || !strings.HasSuffix(config.DatabasePrefix, "_") ||
		!strings.HasPrefix(strings.ToLower(config.ExpectedDatabaseName), config.DatabasePrefix) || unsafeDatabaseName(config.ExpectedDatabaseName) {
		return Config{}, ErrUnsafeTarget
	}
	if _, err := uuid.Parse(config.RunID); err != nil {
		return Config{}, fmt.Errorf("%w: run id must be a UUID", ErrInvalidConfig)
	}
	return config, nil
}

func (config Config) workloadTime() time.Time {
	weeks := config.Seed % 4
	if weeks < 0 {
		weeks = -weeks
	}
	return config.BaseTime.Add(time.Duration(weeks) * 7 * 24 * time.Hour)
}

func unsafeDatabaseName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "postgres" || lower == "template0" || lower == "template1" || lower == "manleai" || lower == "ai_receptionist" {
		return true
	}
	for _, forbidden := range []string{"production", "prod", "primary", "live"} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return false
}
