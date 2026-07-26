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
	"github.com/manleai/ai-receptionist/modules/access"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "bootstrap-admin" {
		return errors.New("usage: platform-access bootstrap-admin --email <existing-active-user> --action-key <stable-key> --reason <change-reference>")
	}
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	email := flags.String("email", "", "exact email of an existing active user")
	actionKey := flags.String("action-key", "", "stable retry-safe action key")
	reason := flags.String("reason", "", "bounded operator change reference")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*email) == "" || strings.TrimSpace(*actionKey) == "" || strings.TrimSpace(*reason) == "" {
		return errors.New("email, action-key, and reason are required")
	}

	cfg := config.Load()
	databaseURL := cfg.MigrationDatabaseURL
	if databaseURL == "" {
		databaseURL = cfg.DatabaseURL
	}
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	repository := access.NewRepository(db)
	result, err := repository.BootstrapPlatformAdmin(ctx, *email, *actionKey, *reason)
	if err != nil {
		return fmt.Errorf("bootstrap platform administrator: %w", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}
