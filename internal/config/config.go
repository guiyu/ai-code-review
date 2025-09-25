package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	AIKey         string
	WebhookSecret string
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		Port:          getEnv("PORT", "8080"),
		AIKey:         getEnv("AI_API_KEY", ""),
		WebhookSecret: getEnv("WEBHOOK_SECRET", ""),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	if fallback == "" {
		log.Fatalf("Missing required env: %s", key)
	}
	return fallback
}
