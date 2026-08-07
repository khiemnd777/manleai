package main

import (
	"context"
	"database/sql"
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
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: platform-access <bootstrap-admin|rename-tenant-email> [options]")
	}
	switch args[0] {
	case "bootstrap-admin":
		return runBootstrapAdmin(ctx, args[1:])
	case "rename-tenant-email":
		return runRenameTenantEmail(ctx, args[1:])
	default:
		return errors.New("usage: platform-access <bootstrap-admin|rename-tenant-email> [options]")
	}
}

func runBootstrapAdmin(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	email := flags.String("email", "", "email for the dedicated Platform identity")
	fullName := flags.String("full-name", "", "full name for the dedicated Platform identity")
	passwordFile := flags.String("password-file", "", "path to a regular 0600 file containing the initial password")
	actionKey := flags.String("action-key", "", "stable retry-safe action key")
	reason := flags.String("reason", "", "bounded operator change reference")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*email) == "" || strings.TrimSpace(*fullName) == "" || strings.TrimSpace(*passwordFile) == "" || strings.TrimSpace(*actionKey) == "" || strings.TrimSpace(*reason) == "" {
		return errors.New("email, full-name, password-file, action-key, and reason are required")
	}
	password, err := readPrivatePasswordFile(*passwordFile)
	if err != nil {
		return err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash Platform administrator password: %w", err)
	}

	db, repository, err := openAccessRepository(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := repository.BootstrapPlatformAdmin(ctx, access.BootstrapPlatformAdminRequest{
		Email: *email, FullName: *fullName, PasswordHash: string(passwordHash), ActionKey: *actionKey, Reason: *reason,
	})
	if err != nil {
		return fmt.Errorf("bootstrap platform administrator: %w", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}

func runRenameTenantEmail(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("rename-tenant-email", flag.ContinueOnError)
	currentEmail := flags.String("current-email", "", "current email for the active Tenant owner identity")
	newEmail := flags.String("new-email", "", "replacement email for the same Tenant owner identity")
	actionKey := flags.String("action-key", "", "stable retry-safe action key")
	reason := flags.String("reason", "", "bounded operator change reference")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*currentEmail) == "" || strings.TrimSpace(*newEmail) == "" || strings.TrimSpace(*actionKey) == "" || strings.TrimSpace(*reason) == "" {
		return errors.New("current-email, new-email, action-key, and reason are required")
	}

	db, repository, err := openAccessRepository(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := repository.RenameTenantEmail(ctx, access.RenameTenantEmailRequest{
		CurrentEmail: *currentEmail,
		NewEmail:     *newEmail,
		ActionKey:    *actionKey,
		Reason:       *reason,
	})
	if err != nil {
		return fmt.Errorf("rename Tenant email: %w", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}

func openAccessRepository(ctx context.Context) (*sql.DB, *access.Repository, error) {
	cfg := config.Load()
	databaseURL := cfg.MigrationDatabaseURL
	if databaseURL == "" {
		databaseURL = cfg.DatabaseURL
	}
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return db, access.NewRepository(db), nil
}

func readPrivatePasswordFile(filePath string) (string, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return "", fmt.Errorf("inspect password file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("password file must be a regular file readable only by its owner (mode 0600)")
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}
	password := strings.TrimRight(string(raw), "\r\n")
	if len(password) < 12 || strings.TrimSpace(password) == "" {
		return "", errors.New("Platform administrator password must contain at least 12 characters")
	}
	return password, nil
}
