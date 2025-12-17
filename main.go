package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/lonanche/geo-verification/config"
	"github.com/lonanche/geo-verification/internal/api"
	"github.com/lonanche/geo-verification/internal/geoguessr"
	"github.com/lonanche/geo-verification/internal/verification"
)

func main() {
	cfg := config.Load()

	if cfg.GeoGuessrNcfaToken == "" {
		log.Fatal("GEOGUESSR_NCFA_TOKEN environment variable must be set")
	}

	geoClient := geoguessr.NewClient(cfg.GeoGuessrNcfaToken)

	if err := geoClient.Login(); err != nil {
		log.Fatalf("Failed to login to GeoGuessr: %v", err)
	}

	verificationService := verification.NewService(
		geoClient,
		cfg.RateLimitPerHour,
		cfg.CodeExpiryDuration(),
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

	log.Printf("Starting GeoGuessr verification service on port %s", cfg.Port)
	log.Printf("Rate limit: %d requests per hour per user", cfg.RateLimitPerHour)
	log.Printf("Code expiry: %d minutes", cfg.CodeExpiryMinutes)

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}