package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                 string
	GeoGuessrNcfaToken   string
	CodeExpiryMinutes    int
	RateLimitPerHour     int
	AllowedCallbackHosts string
}

func Load() *Config {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found, using environment variables")
	}

	return &Config{
		Port:                 getEnv("PORT", "8080"),
		GeoGuessrNcfaToken:   getEnv("GEOGUESSR_NCFA_TOKEN", ""),
		CodeExpiryMinutes:    getEnvInt("CODE_EXPIRY_MINUTES", 5),
		RateLimitPerHour:     getEnvInt("RATE_LIMIT_PER_HOUR", 3),
		AllowedCallbackHosts: getEnv("ALLOWED_CALLBACK_HOSTS", "localhost,127.0.0.1,::1"),
	}
}

func (c *Config) CodeExpiryDuration() time.Duration {
	return time.Duration(c.CodeExpiryMinutes) * time.Minute
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
