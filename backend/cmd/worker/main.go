package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/logger"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
)

const (
	posSyncPollInterval = 30 * time.Second
	posSyncBatchLimit   = 10
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	logg := logger.New(cfg.AppEnv)

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if cfg.AutoMigrate {
		if err := database.Migrate(ctx, db); err != nil {
			log.Fatalf("run database migrations: %v", err)
		}
		logg.Info("database migrations ready")
	}

	cipher, err := encryption.NewTokenCipher(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("create token cipher: %v", err)
	}

	posRepo := pos.NewRepository(db)
	integrationConfigRepo := integrationconfig.NewRepository(db)
	integrationConfigService := integrationconfig.NewService(integrationConfigRepo, cipher, cfg)
	squareAdapter := pos_square.NewSquareAdapter(cfg.Square, posRepo, cipher)
	squareAdapter.SetConfigResolver(integrationConfigService)
	processor := pos.NewSyncProcessor(posRepo, []pos.POSProvider{squareAdapter})

	logg.Info("worker started", "scope", "pos_sync_jobs", "interval", posSyncPollInterval.String(), "batch_limit", posSyncBatchLimit)
	ticker := time.NewTicker(posSyncPollInterval)
	defer ticker.Stop()

	for {
		processed, err := processor.ProcessOnce(ctx, posSyncBatchLimit)
		if err != nil {
			logg.Error("process POS sync jobs", slog.String("error", err.Error()))
		} else if processed > 0 {
			logg.Info("processed POS sync jobs", "count", processed)
		}

		select {
		case <-ctx.Done():
			logg.Info("worker stopped")
			return
		case <-ticker.C:
		}
	}
}
