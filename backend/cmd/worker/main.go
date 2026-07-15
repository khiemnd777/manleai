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
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
)

const (
	posSyncPollInterval                  = 30 * time.Second
	posSyncBatchLimit                    = 10
	bookingLeaseSweepPollInterval        = 30 * time.Second
	bookingLeaseSweepBatchLimit          = 50
	availabilityQuoteCleanupBatchLimit   = 250
	availabilityQuoteCleanupPollInterval = 5 * time.Minute
	squareWebhookPollInterval            = 30 * time.Second
	squareWebhookBatchLimit              = 20
	squareCalendarRepairPollInterval     = 30 * time.Second
	squareCalendarRepairBatchLimit       = 2
	conversationRetentionPollInterval    = time.Hour
	conversationRetentionRedactionLimit  = 50
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
	bookingRepo := booking.NewRepository(db)
	bookingService := booking.NewService(bookingRepo, []pos.POSProvider{squareAdapter})
	availabilityQuoteCleanup := booking.NewAvailabilityQuoteCleanupProcessor(bookingRepo)
	squareWebhookProcessor := pos_square.NewWebhookProcessor(pos_square.NewWebhookRepository(db), bookingService)
	conversationRetention := conversation.NewRetentionProcessor(conversation.NewRepository(db))

	logg.Info("worker started", "scope", "pos_sync_jobs,booking_lease_recovery,availability_quote_cleanup,square_booking_webhooks,square_calendar_repair,conversation_retention", "interval", posSyncPollInterval.String(), "batch_limit", posSyncBatchLimit)

	scheduler := newRecurringJobScheduler()
	scheduler.Run(ctx,
		recurringJob{
			name:     "pos_sync_jobs",
			interval: posSyncPollInterval,
			run: func(ctx context.Context) {
				processed, err := processor.ProcessOnce(ctx, posSyncBatchLimit)
				if err != nil {
					logg.Error("process POS sync jobs", slog.String("error", err.Error()))
				} else if processed > 0 {
					logg.Info("processed POS sync jobs", "count", processed)
				}
			},
		},
		recurringJob{
			name:     "booking_lease_recovery",
			interval: bookingLeaseSweepPollInterval,
			run: func(ctx context.Context) {
				expiredLeaseAttempts, err := bookingRepo.SweepExpiredBookingOperationLeases(ctx, bookingLeaseSweepBatchLimit)
				if err != nil {
					logg.Error("sweep expired booking operation leases", slog.String("error", err.Error()))
				} else if expiredLeaseAttempts > 0 {
					logg.Info("recovered expired booking lease attempts", "count", expiredLeaseAttempts)
				}
			},
		},
		recurringJob{
			name:     "availability_quote_cleanup",
			interval: availabilityQuoteCleanupPollInterval,
			run: func(ctx context.Context) {
				deletedQuotes, err := availabilityQuoteCleanup.ProcessOnce(ctx, availabilityQuoteCleanupBatchLimit)
				if err != nil {
					logg.Error("clean up retained availability quotes", slog.String("error", err.Error()))
				} else if deletedQuotes > 0 {
					logg.Info("cleaned up retained availability quotes", "count", deletedQuotes)
				}
			},
		},
		recurringJob{
			name:     "square_booking_webhooks",
			interval: squareWebhookPollInterval,
			run: func(ctx context.Context) {
				webhookEvents, err := squareWebhookProcessor.ProcessWebhookEvents(ctx, squareWebhookBatchLimit)
				if err != nil {
					logg.Error("process Square booking webhooks", slog.String("error", err.Error()))
				} else if webhookEvents > 0 {
					logg.Info("processed Square booking webhooks", "count", webhookEvents)
				}
			},
		},
		recurringJob{
			name:     "square_calendar_repair",
			interval: squareCalendarRepairPollInterval,
			run: func(ctx context.Context) {
				repairedCalendars, err := squareWebhookProcessor.ProcessScheduledRepairs(ctx, squareCalendarRepairBatchLimit)
				if err != nil {
					logg.Error("repair Square calendars", slog.String("error", err.Error()))
				} else if repairedCalendars > 0 {
					logg.Info("repaired Square calendars", "count", repairedCalendars)
				}
			},
		},
		recurringJob{
			name:     "conversation_retention",
			interval: conversationRetentionPollInterval,
			run: func(ctx context.Context) {
				redacted, err := conversationRetention.ProcessOnce(ctx, conversationRetentionRedactionLimit)
				if err != nil {
					logg.Error("process conversation retention", slog.String("error", err.Error()))
				} else if redacted > 0 {
					logg.Info("redacted expired conversation sessions", "count", redacted)
				}
			},
		},
	)

	logg.Info("worker stopped")
}
