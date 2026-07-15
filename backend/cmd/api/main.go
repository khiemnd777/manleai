package main

import (
	"context"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/logger"
	"github.com/manleai/ai-receptionist/modules/auth"
	"github.com/manleai/ai-receptionist/modules/booking"
	configtransfer "github.com/manleai/ai-receptionist/modules/config_transfer"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/customer"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
	publiccatalog "github.com/manleai/ai-receptionist/modules/public_catalog"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/training"
	"github.com/manleai/ai-receptionist/modules/voice"
	"github.com/manleai/ai-receptionist/modules/voice_openai"
	"github.com/manleai/ai-receptionist/modules/voice_twilio"
)

func main() {
	ctx := context.Background()
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

	app := fiber.New(fiber.Config{
		AppName:      "AI Receptionist API",
		ErrorHandler: fiber.DefaultErrorHandler,
	})
	app.Use(recover.New(recover.Config{EnableStackTrace: cfg.AppEnv != "production"}))
	app.Use(requestid.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: strings.Join(cfg.CORSOrigins, ","),
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
	}))

	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	api := app.Group("/api")

	authRepo := auth.NewRepository(db)
	authService := auth.NewService(authRepo, cfg)
	auth.RegisterRoutes(api, auth.NewHandler(authService), cfg.JWTSecret)

	salonRepo := salon.NewRepository(db)
	salonService := salon.NewService(salonRepo)
	salon.RegisterRoutes(api, salon.NewHandler(salonService), cfg.JWTSecret)

	publicCatalogRepo := publiccatalog.NewRepository(db)
	publicCatalogService := publiccatalog.NewService(publicCatalogRepo)
	publiccatalog.RegisterRoutes(api, publiccatalog.NewHandler(publicCatalogService))

	integrationConfigRepo := integrationconfig.NewRepository(db)
	integrationConfigService := integrationconfig.NewService(integrationConfigRepo, cipher, cfg)
	integrationconfig.RegisterRoutes(api, integrationconfig.NewHandler(integrationConfigService), cfg.JWTSecret)

	posRepo := pos.NewRepository(db)

	squareAdapter := pos_square.NewSquareAdapter(cfg.Square, posRepo, cipher)
	squareAdapter.SetConfigResolver(integrationConfigService)
	posService := pos.NewService(posRepo, squareAdapter)
	pos.RegisterRoutes(api, pos.NewHandler(posService), cfg.JWTSecret)

	bookingRepo := booking.NewRepository(db)
	bookingService := booking.NewService(bookingRepo, []pos.POSProvider{squareAdapter})
	booking.RegisterRoutes(api, booking.NewHandler(bookingService), cfg.JWTSecret)

	customerRepo := customer.NewRepository(db)
	customerService := customer.NewService(customerRepo, []pos.POSProvider{squareAdapter})
	customer.RegisterRoutes(api, customer.NewHandler(customerService), cfg.JWTSecret)

	conversationRepo := conversation.NewRepository(db)
	conversationService := conversation.NewService(conversationRepo, bookingService)
	conversation.RegisterRoutes(api, conversation.NewHandler(conversationService), cfg.JWTSecret)

	trainingRepo := training.NewRepository(db)
	trainingService := training.NewService(trainingRepo)
	training.RegisterRoutes(api, training.NewHandler(trainingService), cfg.JWTSecret)

	configTransferRepo := configtransfer.NewRepository(db)
	configTransferService := configtransfer.NewService(salonService, integrationConfigService, posRepo, trainingService, configTransferRepo)
	configtransfer.RegisterRoutes(api, configtransfer.NewHandler(configTransferService), cfg.JWTSecret)

	voiceRepo := voice.NewRepository(db)
	openAIVoiceAdapter := voice_openai.NewAdapter(cfg.Voice.AI.OpenAI)
	openAIVoiceAdapter.SetConfigResolver(integrationConfigService)
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
	voice.RegisterRoutes(api, voice.NewHandler(voiceService), cfg.JWTSecret)
	twilioVoiceAdapter := voice_twilio.NewAdapter(cfg.Voice.Twilio, cfg.Voice.PublicBaseURL)
	voice_twilio.RegisterRoutes(api, voice_twilio.NewHandler(twilioVoiceAdapter, voiceService))

	squareService := pos_square.NewService(posRepo, squareAdapter, cfg.JWTSecret, bookingService)
	squareService.SetWebhookRepository(pos_square.NewWebhookRepository(db))
	pos_square.RegisterRoutes(api, pos_square.NewHandler(squareService, cfg), cfg.JWTSecret)

	logg.Info("api listening", "port", cfg.ServerPort, "env", cfg.AppEnv)
	if err := app.Listen(":" + cfg.ServerPort); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
