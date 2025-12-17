package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/lonanche/geo-verification/config"
	"github.com/lonanche/geo-verification/internal/api"
	"github.com/lonanche/geo-verification/internal/geoguessr"
	"github.com/lonanche/geo-verification/internal/logger"
	"github.com/lonanche/geo-verification/internal/verification"
)

func main() {
	appLogger := logger.New("geo-verification")
	cfg := config.Load()

	if cfg.GeoGuessrNcfaToken == "" {
		log.Fatal("[geo-verification] GEOGUESSR_NCFA_TOKEN environment variable must be set")
	}

	geoClient := geoguessr.NewClient(cfg.GeoGuessrNcfaToken, appLogger)

	if err := geoClient.Login(); err != nil {
		appLogger.Fatalf("Failed to login to GeoGuessr: %v", err)
	}

	verificationService := verification.NewService(
		geoClient,
		cfg.RateLimitPerHour,
		cfg.CodeExpiryDuration(),
		appLogger,
	)

	handler := api.NewHandler(verificationService)

	router := gin.New()

	api.SetupMiddleware(router)

	v1 := router.Group("/api/v1")
	{
		v1.POST("/verify/start", handler.StartVerification)
		v1.GET("/verify/status/:session_id", handler.GetVerificationStatus)
	}

	router.GET("/health", handler.HealthCheck)

	appLogger.Printf("Starting GeoGuessr verification service on port %s", cfg.Port)
	appLogger.Printf("Rate limit: %d requests per hour per user", cfg.RateLimitPerHour)
	appLogger.Printf("Code expiry: %d minutes", cfg.CodeExpiryMinutes)

	if err := router.Run(":" + cfg.Port); err != nil {
		appLogger.Fatalf("Failed to start server: %v", err)
	}
}
