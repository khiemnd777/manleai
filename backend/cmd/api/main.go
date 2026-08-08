package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/logger"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/internal/ratelimit"
	"github.com/manleai/ai-receptionist/internal/respond"
	"github.com/manleai/ai-receptionist/modules/access"
	airuntimecontrol "github.com/manleai/ai-receptionist/modules/ai_runtime_control"
	"github.com/manleai/ai-receptionist/modules/auth"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/business"
	configtransfer "github.com/manleai/ai-receptionist/modules/config_transfer"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/customer"
	customernotification "github.com/manleai/ai-receptionist/modules/customer_notification"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
	notificationtwilio "github.com/manleai/ai-receptionist/modules/notification_twilio"
	openairuntimeverification "github.com/manleai/ai-receptionist/modules/openai_runtime_verification"
	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
	publiccatalog "github.com/manleai/ai-receptionist/modules/public_catalog"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	authorityswitch "github.com/manleai/ai-receptionist/modules/scheduling_authority_switch"
	"github.com/manleai/ai-receptionist/modules/scheduling_external_provider"
	manleaicalendar "github.com/manleai/ai-receptionist/modules/scheduling_manleai_calendar"
	"github.com/manleai/ai-receptionist/modules/scheduling_owner_manual"
	tenantprovisioning "github.com/manleai/ai-receptionist/modules/tenant_provisioning"
	tenantregistration "github.com/manleai/ai-receptionist/modules/tenant_registration"
	tenantruntime "github.com/manleai/ai-receptionist/modules/tenant_runtime"
	"github.com/manleai/ai-receptionist/modules/training"
	"github.com/manleai/ai-receptionist/modules/voice"
	"github.com/manleai/ai-receptionist/modules/voice_openai"
	"github.com/manleai/ai-receptionist/modules/voice_twilio"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()
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
	logg.Info("database ready", "rls_enforced", cfg.DatabaseRLSEnforced)

	cipher, err := encryption.NewTokenCipher(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("create token cipher: %v", err)
	}

	var rateLimitStore *ratelimit.RedisStore
	if cfg.RateLimitEnabled {
		rateLimitStore, err = ratelimit.NewRedisStore(cfg.RedisURL)
		if err != nil {
			log.Fatalf("configure Redis rate limiting: %v", err)
		}
		defer rateLimitStore.Close()
		pingContext, cancelPing := context.WithTimeout(ctx, 3*time.Second)
		err = rateLimitStore.Ping(pingContext)
		cancelPing()
		if err != nil {
			log.Fatalf("connect Redis rate limiting: %v", err)
		}
	}

	app := fiber.New(fiber.Config{
		AppName:      "AI Receptionist API",
		ErrorHandler: fiber.DefaultErrorHandler,
	})
	app.Use(recover.New(recover.Config{EnableStackTrace: cfg.AppEnv != "production"}))
	app.Use(requestid.New())
	app.Use(cors.New(apiCORSConfig(cfg)))
	if rateLimitStore != nil {
		app.Use(middleware.RateLimit(rateLimitStore, cfg.JWTSecret, cfg.RateLimitClientIPHeader))
	}

	app.Get("/healthz", func(c *fiber.Ctx) error {
		if rateLimitStore != nil {
			healthContext, cancelHealth := context.WithTimeout(c.UserContext(), time.Second)
			err := rateLimitStore.Ping(healthContext)
			cancelHealth()
			if err != nil {
				return respond.Error(c, fiber.StatusServiceUnavailable, "RATE_LIMIT_UNAVAILABLE", "Request protection is temporarily unavailable.")
			}
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	authRepo := auth.NewRepository(db)
	authService := auth.NewService(authRepo, cfg)
	api := app.Group("/api", middleware.WithAccessPrincipalResolver(authRepo))
	auth.RegisterRoutes(api, auth.NewHandler(authService), cfg.JWTSecret)
	accessRepo := access.NewRepository(db)
	accessService := access.NewService(accessRepo)
	access.RegisterRoutes(api, access.NewHandler(accessService), cfg.JWTSecret)
	tenantRuntimeRepo := tenantruntime.NewRepository(db)
	tenantRuntimeService := tenantruntime.NewService(tenantRuntimeRepo, accessService)
	tenantruntime.RegisterPlatformRoutes(api, tenantruntime.NewHandler(tenantRuntimeService), cfg.JWTSecret)
	businessRepo := business.NewRepository(db)
	businessService := business.NewService(businessRepo, accessService)
	business.RegisterTenantRoutes(api, business.NewHandler(businessService, access.SurfaceTenant), cfg.JWTSecret)
	business.RegisterPlatformRoutes(api, business.NewHandler(businessService, access.SurfacePlatform), accessService, cfg.JWTSecret)

	salonRepo := salon.NewRepository(db)
	salonService := salon.NewService(salonRepo)
	salon.RegisterRoutes(api, salon.NewHandler(salonService), cfg.JWTSecret)
	tenantRegistrationRepo := tenantregistration.NewRepository(db)
	tenantRegistrationService := tenantregistration.NewService(tenantRegistrationRepo, accessService)
	tenantregistration.RegisterRoutes(api, tenantregistration.NewHandler(tenantRegistrationService), cfg.JWTSecret)
	tenantProvisioningService := tenantprovisioning.NewService(tenantprovisioning.NewRepository(db, salonService), accessService)
	tenantprovisioning.RegisterRoutes(api, tenantprovisioning.NewHandler(tenantProvisioningService), cfg.JWTSecret)

	publicCatalogRepo := publiccatalog.NewRepository(db)
	publicCatalogService := publiccatalog.NewService(publicCatalogRepo)
	publiccatalog.RegisterRoutes(api, publiccatalog.NewHandler(publicCatalogService))

	integrationConfigRepo := integrationconfig.NewRepository(db)
	integrationConfigService := integrationconfig.NewService(integrationConfigRepo, cipher, cfg)
	integrationconfig.RegisterPlatformRoutes(api, integrationconfig.NewPlatformHandler(integrationConfigService, accessService), cfg.JWTSecret)
	notificationDeliveryRepo := notificationdelivery.NewRepository(db)
	notificationDeliveryService := notificationdelivery.NewService(notificationDeliveryRepo)
	notificationdelivery.RegisterPlatformRoutes(api, notificationdelivery.NewPlatformHandler(notificationDeliveryService, accessService), cfg.JWTSecret)
	customerNotificationRepo := customernotification.NewRepository(db)
	customerNotificationService := customernotification.NewService(customerNotificationRepo)
	customernotification.RegisterRoutes(api, customernotification.NewHandler(customerNotificationService), cfg.JWTSecret)
	customernotification.RegisterPlatformRoutes(api, customernotification.NewPlatformHandler(customerNotificationService, accessService), cfg.JWTSecret)
	callbackService := customernotification.NewCallbackMultiplexer(notificationDeliveryService, customerNotificationService)
	notificationtwilio.RegisterRoutes(api, notificationtwilio.NewHandler(callbackService, integrationConfigService, customerNotificationService))

	squareWebhookRepo := pos_square.NewWebhookRepository(db)
	operationsHealthRepo := operationshealth.NewRepository(db)
	operationsHealthService := operationshealth.NewService(operationsHealthRepo, squareWebhookRepo)
	operationshealth.RegisterPlatformRoutes(api, operationshealth.NewPlatformHandler(operationsHealthService, accessService), cfg.JWTSecret)

	posRepo := pos.NewRepository(db)
	aiRuntimeControlService := airuntimecontrol.NewService(posRepo)
	airuntimecontrol.RegisterPlatformRoutes(api, airuntimecontrol.NewPlatformHandler(aiRuntimeControlService, accessService), cfg.JWTSecret)

	squareAdapter, err := pos_square.NewSquareAdapter(integrationConfigService, posRepo, cipher)
	if err != nil {
		log.Fatalf("create tenant-bound Square adapter: %v", err)
	}
	posService := pos.NewService(posRepo, squareAdapter)
	pos.RegisterRoutes(api, pos.NewHandler(posService), cfg.JWTSecret)
	pos.RegisterPlatformRoutes(api, pos.NewHandler(posService), accessService, cfg.JWTSecret)
	pos.RegisterPlatformCallsCatalogRoutes(api, pos.NewHandler(posService), accessService, cfg.JWTSecret)

	bookingRepo := booking.NewRepository(db)
	bookingService := booking.NewService(bookingRepo, []pos.POSProvider{squareAdapter})
	schedulingRepo := scheduling.NewRepository(db)
	externalSchedulingAdapter := scheduling_external_provider.NewAdapter(bookingService)
	ownerManualSchedulingRepo := scheduling_owner_manual.NewRepository(db)
	ownerManualSchedulingService := scheduling_owner_manual.NewService(ownerManualSchedulingRepo)
	ownerManualSchedulingExecutor := scheduling_owner_manual.NewExecutor(ownerManualSchedulingService)
	manleaiCalendarRepo := manleaicalendar.NewRepository(db)
	manleaiCalendarExecutor := manleaicalendar.NewExecutor(manleaiCalendarRepo, nil)
	schedulingService := scheduling.NewService(schedulingRepo, bookingService, externalSchedulingAdapter, ownerManualSchedulingExecutor, manleaiCalendarExecutor)
	booking.RegisterRoutes(api, booking.NewHandler(schedulingService), cfg.JWTSecret)
	scheduling.RegisterRoutes(api, scheduling.NewHandler(schedulingService, ownerManualSchedulingService).SetTenantRuntimeLimiter(tenantRuntimeService), cfg.JWTSecret)
	manleaiCalendarService := manleaicalendar.NewService(manleaiCalendarRepo)
	manleaicalendar.RegisterPlatformRoutes(api, manleaicalendar.NewPlatformHandler(manleaiCalendarService, accessService), cfg.JWTSecret)

	customerRepo := customer.NewRepository(db)
	customerService := customer.NewService(customerRepo, []pos.POSProvider{squareAdapter})
	customer.RegisterRoutes(api, customer.NewHandler(customerService), cfg.JWTSecret)

	conversationRepo := conversation.NewRepository(db)
	conversationService := conversation.NewService(conversationRepo, schedulingService)
	conversationService.SetCustomerSMSConsentTool(customerNotificationService)
	conversation.RegisterRoutes(api, conversation.NewHandler(conversationService), cfg.JWTSecret)
	conversation.RegisterPlatformRoutes(api, conversation.NewPlatformHandler(conversationService, accessService), cfg.JWTSecret)
	conversation.RegisterPlatformV2Routes(api, conversation.NewPlatformHandler(conversationService, accessService), cfg.JWTSecret)

	trainingRepo := training.NewRepository(db)
	trainingService := training.NewService(trainingRepo, schedulingService)
	training.RegisterRoutes(api, training.NewHandler(trainingService).SetTenantRuntimeLimiter(tenantRuntimeService), cfg.JWTSecret)
	training.RegisterPlatformRoutes(api, training.NewHandler(trainingService), accessService, cfg.JWTSecret)
	training.RegisterPlatformV2Routes(api, training.NewNormalizedHandler(trainingService), accessService, cfg.JWTSecret)
	training.RegisterPlatformServiceAliasRoutes(api, training.NewHandler(trainingService), accessService, cfg.JWTSecret)
	training.RegisterPlatformCallsCorrectionRoute(api, training.NewHandler(trainingService), accessService, cfg.JWTSecret)
	configurationTransferRepo := configtransfer.NewRepository(db)
	configurationTransferService := configtransfer.NewService(salonService, integrationConfigService, posRepo, trainingService, configurationTransferRepo, posRepo)
	platformConfigurationTransferRepo := configtransfer.NewPlatformRepository(db)
	platformConfigurationTransferService := configtransfer.NewPlatformService(configurationTransferService, platformConfigurationTransferRepo, integrationConfigService)
	configtransfer.RegisterPlatformRoutes(api, configtransfer.NewPlatformHandler(platformConfigurationTransferService, accessService), cfg.JWTSecret)

	voiceRepo := voice.NewRepository(db)
	openAIVoiceAdapter, err := voice_openai.NewTenantBoundAdapter(integrationConfigService)
	if err != nil {
		log.Fatalf("create tenant-bound OpenAI adapter: %v", err)
	}
	openAIVerificationService := openairuntimeverification.NewService(
		openairuntimeverification.NewRepository(db), integrationConfigService, openAIVoiceAdapter,
	)
	openairuntimeverification.RegisterPlatformRoutes(
		api, openairuntimeverification.NewPlatformHandler(openAIVerificationService, accessService), cfg.JWTSecret,
	)
	aiProviders := voice.AIProviders{
		STT:          openAIVoiceAdapter,
		LLM:          openAIVoiceAdapter,
		TurnModel:    openAIVoiceAdapter,
		TTS:          openAIVoiceAdapter,
		StreamingTTS: openAIVoiceAdapter,
		Realtime:     openAIVoiceAdapter,
	}
	conversationService.SetReplyGenerator(voice.NewGuardedReplyGenerator(aiProviders.LLM))
	conversationService.SetTurnInterpreter(voice.NewGuardedTurnInterpreter(openAIVoiceAdapter))
	voiceService := voice.NewService(voiceRepo, conversationService, cfg.Voice, aiProviders)
	voiceService.SetConfigResolver(integrationConfigService)
	voiceService.SetTenantRuntimeLimiter(tenantRuntimeService)
	voice.RegisterRoutes(api, voice.NewHandler(voiceService), cfg.JWTSecret)
	voice.RegisterPlatformRoutes(api, voice.NewPlatformHandler(voiceService, accessService), cfg.JWTSecret)
	voice.RegisterTechnicalRoutes(api, voice.NewTechnicalHandler(voiceService, accessService), cfg.JWTSecret)
	twilioVoiceAdapter := voice_twilio.NewAdapter(cfg.Voice.Twilio, cfg.Voice.PublicBaseURL)
	voice_twilio.RegisterRoutes(api, voice_twilio.NewHandler(twilioVoiceAdapter, voiceService))

	squareService := pos_square.NewService(posRepo, squareAdapter, cfg.JWTSecret, schedulingService)
	squareService.SetWebhookRepository(squareWebhookRepo)
	squareService.SetWebhookConfigurationStatusResolver(integrationConfigService)
	voiceService.SetSchedulingReadinessProviders(ownerManualSchedulingService, manleaiCalendarService, squareService)
	pos_square.RegisterRoutes(api, pos_square.NewHandler(squareService, cfg.FrontendURL), cfg.JWTSecret)
	pos_square.RegisterPlatformRoutes(api, pos_square.NewPlatformHandler(squareService, accessService, tenantRuntimeService), cfg.JWTSecret)
	authoritySwitchRepo := authorityswitch.NewRepository(db)
	authoritySwitchService := authorityswitch.NewService(authoritySwitchRepo, manleaiCalendarService, squareService, ownerManualSchedulingExecutor != nil)
	authoritySwitchPlatformHandler := authorityswitch.NewPlatformHandler(authoritySwitchService, accessService)
	authorityswitch.RegisterPlatformRoutes(api, authoritySwitchPlatformHandler, cfg.JWTSecret)
	authorityswitch.RegisterPlatformV2Routes(api, authoritySwitchPlatformHandler, cfg.JWTSecret)

	logg.Info("api listening", "port", cfg.ServerPort, "env", cfg.AppEnv)
	if err := app.Listen(":" + cfg.ServerPort); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func apiCORSConfig(cfg config.Config) cors.Config {
	return cors.Config{
		AllowOrigins:     strings.Join(cfg.CORSOrigins, ","),
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Tenant-Salon-ID",
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		ExposeHeaders:    "X-Idempotent-Replay, RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset, TenantLimit-Limit, TenantLimit-Remaining, Retry-After",
		AllowCredentials: true,
	}
}
