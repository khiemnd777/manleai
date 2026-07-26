package main

import (
	"context"
	"log"
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
	customernotification "github.com/manleai/ai-receptionist/modules/customer_notification"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
	notificationtwilio "github.com/manleai/ai-receptionist/modules/notification_twilio"
	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
	schedulingretention "github.com/manleai/ai-receptionist/modules/scheduling_retention"
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
	notificationDeliveryPollInterval     = 15 * time.Second
	notificationDeliveryBatchLimit       = notificationdelivery.DefaultProcessBatch
	customerNotificationPollInterval     = 15 * time.Second
	customerNotificationBatchLimit       = customernotification.DefaultProcessBatch
	schedulingPIIRetentionPollInterval   = 5 * time.Minute
	schedulingPIIRetentionBatchLimit     = schedulingretention.DefaultProcessBatch
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
	notificationDeliveryRepo := notificationdelivery.NewRepository(db)
	notificationDeliveryProcessor := notificationdelivery.NewProcessor(
		notificationDeliveryRepo,
		notificationtwilio.NewResolver(integrationConfigService),
	)
	customerNotificationRepo := customernotification.NewRepository(db)
	customerNotificationProcessor := customernotification.NewProcessor(
		customerNotificationRepo,
		customernotification.NewTwilioSenderResolver(integrationConfigService),
	)
	schedulingPIIRetention := schedulingretention.NewProcessor(schedulingretention.NewRepository(db))

	logg.Info("worker started", "scope", "pos_sync_jobs,booking_lease_recovery,availability_quote_cleanup,square_booking_webhooks,square_calendar_repair,conversation_retention,notification_delivery,customer_notification_delivery,scheduling_pii_retention", "interval", posSyncPollInterval.String(), "batch_limit", posSyncBatchLimit)

	operationsHealthRepo := operationshealth.NewRepository(db)
	scheduler := newRecurringJobScheduler(operationsHealthRepo)
	scheduler.Run(ctx,
		recurringJob{
			name:     "pos_sync_jobs",
			interval: posSyncPollInterval,
			run:      func(ctx context.Context) (int, error) { return processor.ProcessOnce(ctx, posSyncBatchLimit) },
		},
		recurringJob{
			name:     "booking_lease_recovery",
			interval: bookingLeaseSweepPollInterval,
			run: func(ctx context.Context) (int, error) {
				return bookingRepo.SweepExpiredBookingOperationLeases(ctx, bookingLeaseSweepBatchLimit)
			},
		},
		recurringJob{
			name:     "availability_quote_cleanup",
			interval: availabilityQuoteCleanupPollInterval,
			run: func(ctx context.Context) (int, error) {
				return availabilityQuoteCleanup.ProcessOnce(ctx, availabilityQuoteCleanupBatchLimit)
			},
		},
		recurringJob{
			name:     "square_booking_webhooks",
			interval: squareWebhookPollInterval,
			run: func(ctx context.Context) (int, error) {
				return squareWebhookProcessor.ProcessWebhookEvents(ctx, squareWebhookBatchLimit)
			},
		},
		recurringJob{
			name:     "square_calendar_repair",
			interval: squareCalendarRepairPollInterval,
			run: func(ctx context.Context) (int, error) {
				return squareWebhookProcessor.ProcessScheduledRepairs(ctx, squareCalendarRepairBatchLimit)
			},
		},
		recurringJob{
			name:     "notification_delivery",
			interval: notificationDeliveryPollInterval,
			run: func(ctx context.Context) (int, error) {
				return notificationDeliveryProcessor.ProcessOnce(ctx, notificationDeliveryBatchLimit)
			},
		},
		recurringJob{
			name:     "customer_notification_delivery",
			interval: customerNotificationPollInterval,
			run: func(ctx context.Context) (int, error) {
				return customerNotificationProcessor.ProcessOnce(ctx, customerNotificationBatchLimit)
			},
		},
		recurringJob{
			name:     "conversation_retention",
			interval: conversationRetentionPollInterval,
			run: func(ctx context.Context) (int, error) {
				return conversationRetention.ProcessOnce(ctx, conversationRetentionRedactionLimit)
			},
		},
		recurringJob{
			name:     "scheduling_pii_retention",
			interval: schedulingPIIRetentionPollInterval,
			run: func(ctx context.Context) (int, error) {
				return schedulingPIIRetention.ProcessOnce(ctx, schedulingPIIRetentionBatchLimit)
			},
		},
	)

	logg.Info("worker stopped")
}
