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
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/pos_square"
	"github.com/manleai/ai-receptionist/modules/salon"
	"github.com/manleai/ai-receptionist/modules/voice"
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
	app.Use(recover.New())
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

	posRepo := pos.NewRepository(db)
	squareAdapter := pos_square.NewSquareAdapter(cfg.Square, posRepo, cipher)
	posService := pos.NewService(posRepo)
	pos.RegisterRoutes(api, pos.NewHandler(posService), cfg.JWTSecret)

	bookingRepo := booking.NewRepository(db)
	bookingService := booking.NewService(bookingRepo, []pos.POSProvider{squareAdapter})
	booking.RegisterRoutes(api, booking.NewHandler(bookingService), cfg.JWTSecret)

	conversationRepo := conversation.NewRepository(db)
	conversationService := conversation.NewService(conversationRepo, bookingService)
	conversation.RegisterRoutes(api, conversation.NewHandler(conversationService), cfg.JWTSecret)

	voiceRepo := voice.NewRepository(db)
	voiceService := voice.NewService(voiceRepo, conversationService, cfg.Voice)
	voice.RegisterRoutes(api, voice.NewHandler(voiceService), cfg.JWTSecret)
	twilioVoiceAdapter := voice_twilio.NewAdapter(cfg.Voice.Twilio, cfg.Voice.PublicBaseURL)
	voice_twilio.RegisterRoutes(api, voice_twilio.NewHandler(twilioVoiceAdapter, voiceService))

	squareService := pos_square.NewService(posRepo, squareAdapter, cfg.JWTSecret, bookingService)
	pos_square.RegisterRoutes(api, pos_square.NewHandler(squareService, cfg), cfg.JWTSecret)

	logg.Info("api listening", "port", cfg.ServerPort, "env", cfg.AppEnv)
	if err := app.Listen(":" + cfg.ServerPort); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
