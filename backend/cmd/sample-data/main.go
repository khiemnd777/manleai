package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/sampledata"
)

const usage = "usage: sample-data apply --profile sample_test --confirm APPLY_SAMPLE_TEST_DATA --admin-email <email> --admin-name <name> --ops-email <email> --ops-name <name>"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "apply" {
		return errors.New(usage)
	}
	flags := flag.NewFlagSet("sample-data apply", flag.ContinueOnError)
	profile := flags.String("profile", "", "exact data profile")
	confirm := flags.String("confirm", "", "exact sample-data confirmation token")
	adminEmail := flags.String("admin-email", "", "sample Platform Admin email")
	adminName := flags.String("admin-name", "", "sample Platform Admin full name")
	opsEmail := flags.String("ops-email", "", "sample Platform Ops email")
	opsName := flags.String("ops-name", "", "sample Platform Ops full name")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New(usage)
	}

	adminPassword := os.Getenv("SAMPLE_PLATFORM_ADMIN_PASSWORD")
	opsPassword := os.Getenv("SAMPLE_PLATFORM_OPS_PASSWORD")
	ownerPassword := os.Getenv("SAMPLE_TENANT_OWNER_PASSWORD")
	_ = os.Unsetenv("SAMPLE_PLATFORM_ADMIN_PASSWORD")
	_ = os.Unsetenv("SAMPLE_PLATFORM_OPS_PASSWORD")
	_ = os.Unsetenv("SAMPLE_TENANT_OWNER_PASSWORD")

	cfg := config.Load()
	if err := cfg.ValidateEnvironment(); err != nil {
		return err
	}
	databaseURL, err := sampleDatabaseURL(cfg)
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()

	result, err := sampledata.Apply(ctx, db, sampledata.ApplyRequest{
		Profile:             *profile,
		Confirmation:        *confirm,
		AdminEmail:          *adminEmail,
		AdminName:           *adminName,
		AdminPassword:       adminPassword,
		OpsEmail:            *opsEmail,
		OpsName:             *opsName,
		OpsPassword:         opsPassword,
		TenantOwnerPassword: ownerPassword,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func sampleDatabaseURL(cfg config.Config) (string, error) {
	databaseURL := strings.TrimSpace(cfg.MigrationDatabaseURL)
	if databaseURL != "" {
		return databaseURL, nil
	}
	if cfg.DeploymentEnv != config.EnvironmentLocal {
		return "", errors.New("MIGRATION_DATABASE_URL is required outside local development")
	}
	return cfg.DatabaseURL, nil
}
