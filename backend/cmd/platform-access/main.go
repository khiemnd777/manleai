package main

import (
	"context"
	"crypto/sha256"
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
		return errors.New("usage: platform-access <bootstrap-admin|rename-tenant-email|rotate-single-admin-password|rotate-tenant-owner-password> [options]")
	}
	switch args[0] {
	case "bootstrap-admin":
		return runBootstrapAdmin(ctx, args[1:])
	case "rename-tenant-email":
		return runRenameTenantEmail(ctx, args[1:])
	case "rotate-single-admin-password":
		return runRotateSingleAdminPassword(ctx, args[1:])
	case "rotate-tenant-owner-password":
		return runRotateTenantOwnerPassword(ctx, args[1:])
	default:
		return errors.New("usage: platform-access <bootstrap-admin|rename-tenant-email|rotate-single-admin-password|rotate-tenant-owner-password> [options]")
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

func runRotateSingleAdminPassword(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("rotate-single-admin-password", flag.ContinueOnError)
	passwordFile := flags.String("password-file", "", "path to a regular 0600 file containing the replacement password")
	actionKey := flags.String("action-key", "", "stable retry-safe action key")
	reason := flags.String("reason", "", "bounded operator change reference")
	outputFile := flags.String("output-file", "", "new private file for the bounded recovery result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*passwordFile) == "" || strings.TrimSpace(*actionKey) == "" || strings.TrimSpace(*reason) == "" || strings.TrimSpace(*outputFile) == "" {
		return errors.New("password-file, action-key, reason, and output-file are required")
	}
	password, err := readPrivatePasswordFile(*passwordFile)
	if err != nil {
		return err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash Platform administrator password: %w", err)
	}
	passwordFingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))

	db, repository, err := openAccessRepository(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := repository.RotateSinglePlatformAdminPassword(ctx, access.RotateSinglePlatformAdminPasswordRequest{
		PasswordHash:        string(passwordHash),
		PasswordFingerprint: passwordFingerprint,
		ActionKey:           *actionKey,
		Reason:              *reason,
	})
	if err != nil {
		return fmt.Errorf("rotate single Platform administrator password: %w", err)
	}
	if err := writePrivateJSONFile(*outputFile, result); err != nil {
		return fmt.Errorf("write recovery result: %w", err)
	}
	return nil
}

func runRotateTenantOwnerPassword(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("rotate-tenant-owner-password", flag.ContinueOnError)
	salonID := flags.String("salon-id", "", "exact salon owned by the Tenant identity")
	emailFile := flags.String("email-file", "", "path to a regular 0600 file containing the exact Tenant owner email")
	passwordFile := flags.String("password-file", "", "path to a regular 0600 file containing the replacement password")
	actionKey := flags.String("action-key", "", "stable retry-safe action key")
	reason := flags.String("reason", "", "bounded operator change reference")
	outputFile := flags.String("output-file", "", "new private file for the bounded recovery result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*salonID) == "" || strings.TrimSpace(*emailFile) == "" || strings.TrimSpace(*passwordFile) == "" || strings.TrimSpace(*actionKey) == "" || strings.TrimSpace(*reason) == "" || strings.TrimSpace(*outputFile) == "" {
		return errors.New("salon-id, email-file, password-file, action-key, reason, and output-file are required")
	}
	email, err := readPrivateEmailFile(*emailFile)
	if err != nil {
		return err
	}
	password, err := readPrivateTenantPasswordFile(*passwordFile)
	if err != nil {
		return err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash Tenant owner password: %w", err)
	}
	passwordFingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))

	db, repository, err := openAccessRepository(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := repository.RotateTenantOwnerPassword(ctx, access.RotateTenantOwnerPasswordRequest{
		SalonID:             *salonID,
		Email:               email,
		PasswordHash:        string(passwordHash),
		PasswordFingerprint: passwordFingerprint,
		ActionKey:           *actionKey,
		Reason:              *reason,
	})
	if err != nil {
		return fmt.Errorf("rotate Tenant owner password: %w", err)
	}
	if err := writePrivateJSONFile(*outputFile, result); err != nil {
		return fmt.Errorf("write recovery result: %w", err)
	}
	return nil
}

func openAccessRepository(ctx context.Context) (*sql.DB, *access.Repository, error) {
	cfg := config.Load()
	if err := cfg.ValidateEnvironment(); err != nil {
		return nil, nil, err
	}
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
	password, err := readPrivateCredentialFile(filePath, "password")
	if err != nil {
		return "", err
	}
	if len(password) < 12 || strings.TrimSpace(password) == "" {
		return "", errors.New("Platform administrator password must contain at least 12 characters")
	}
	return password, nil
}

func readPrivateTenantPasswordFile(filePath string) (string, error) {
	password, err := readPrivateCredentialFile(filePath, "password")
	if err != nil {
		return "", err
	}
	if len(password) < 12 || strings.TrimSpace(password) == "" {
		return "", errors.New("Tenant owner password must contain at least 12 characters")
	}
	return password, nil
}

func readPrivateEmailFile(filePath string) (string, error) {
	email, err := readPrivateCredentialFile(filePath, "email")
	if err != nil {
		return "", err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || strings.ContainsAny(email, "\r\n") {
		return "", errors.New("email file must contain exactly one non-empty line")
	}
	return email, nil
}

func readPrivateCredentialFile(filePath, label string) (string, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return "", fmt.Errorf("inspect %s file: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s file must be a regular file readable only by its owner (mode 0600)", label)
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", label, err)
	}
	return strings.TrimRight(string(raw), "\r\n"), nil
}

func writePrivateJSONFile(filePath string, value any) (returnErr error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return errors.New("output file is required")
	}
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	succeeded := false
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
		if !succeeded {
			_ = os.Remove(filePath)
		}
	}()
	if err := json.NewEncoder(file).Encode(value); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	succeeded = true
	return nil
}
