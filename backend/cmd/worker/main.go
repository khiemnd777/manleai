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
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/logger"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/conversation"
	customernotification "github.com/manleai/ai-receptionist/modules/customer_notification"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
	notificationtwilio "github.com/manleai/ai-receptionist/modules/notification_twilio"
	openairuntimeverification "github.com/manleai/ai-receptionist/modules/openai_runtime_verification"
	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
	schedulingretention "github.com/manleai/ai-receptionist/modules/scheduling_retention"
	tenantregistration "github.com/manleai/ai-receptionist/modules/tenant_registration"
	"github.com/manleai/ai-receptionist/modules/voice_openai"
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
	tenantRegistrationRetentionInterval  = 5 * time.Minute
	tenantRegistrationRetentionLimit     = tenantregistration.DefaultRetentionBatch
	openAIVerificationPollInterval       = 15 * time.Second
	openAIVerificationBatchLimit         = 2
)

func main() {
	ctx, stop := signal.NotifyContext(databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	if err := cfg.ValidateEnvironment(); err != nil {
		log.Fatalf("validate runtime environment: %v", err)
	}
	logg := logger.New(cfg.AppEnv)

	db, err := database.OpenApplication(
		ctx,
		cfg.DatabaseURL,
		cfg.MigrationDatabaseURL,
		cfg.DatabaseRuntimeRole,
		cfg.AutoMigrate,
		cfg.DatabaseRLSEnforced,
	)
	if err != nil {
		log.Fatalf("prepare application database: %v", err)
	}
	defer db.Close()
	logg.Info("database ready", "deployment_env", cfg.DeploymentEnv, "app_env", cfg.AppEnv, "rls_enforced", cfg.DatabaseRLSEnforced)

	cipher, err := encryption.NewTokenCipher(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("create token cipher: %v", err)
	}

	posRepo := pos.NewRepository(db)
	integrationConfigRepo := integrationconfig.NewRepository(db)
	integrationConfigService := integrationconfig.NewService(integrationConfigRepo, cipher, cfg)
	openAIAdapter, err := voice_openai.NewTenantBoundAdapter(integrationConfigService)
	if err != nil {
		log.Fatalf("create tenant-bound OpenAI adapter: %v", err)
	}
	openAIVerificationProcessor := openairuntimeverification.NewService(
		openairuntimeverification.NewRepository(db), integrationConfigService, openAIAdapter,
	)
	squareAdapter, err := pos_square.NewSquareAdapter(integrationConfigService, posRepo, cipher)
	if err != nil {
		log.Fatalf("create tenant-bound Square adapter: %v", err)
	}
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
	tenantRegistrationRetention := tenantregistration.NewRetentionProcessor(tenantregistration.NewRepository(db))

	logg.Info("worker started", "scope", "pos_sync_jobs,booking_lease_recovery,availability_quote_cleanup,square_booking_webhooks,square_calendar_repair,conversation_retention,notification_delivery,customer_notification_delivery,scheduling_pii_retention,tenant_registration_retention,openai_runtime_verification", "interval", posSyncPollInterval.String(), "batch_limit", posSyncBatchLimit)

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
			name:     "openai_runtime_verification",
			interval: openAIVerificationPollInterval,
			run: func(ctx context.Context) (int, error) {
				return openAIVerificationProcessor.ProcessOnce(ctx, openAIVerificationBatchLimit)
			},
		},
		recurringJob{
			name:     "scheduling_pii_retention",
			interval: schedulingPIIRetentionPollInterval,
			run: func(ctx context.Context) (int, error) {
				return schedulingPIIRetention.ProcessOnce(ctx, schedulingPIIRetentionBatchLimit)
			},
		},
		recurringJob{
			name:     "tenant_registration_retention",
			interval: tenantRegistrationRetentionInterval,
			run: func(ctx context.Context) (int, error) {
				return tenantRegistrationRetention.ProcessOnce(ctx, tenantRegistrationRetentionLimit)
			},
		},
	)

	logg.Info("worker stopped")
}
