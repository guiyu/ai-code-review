package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	AIBaseURL     string
	AIModel       string
	AIKey         string
	WebhookSecret string
	GiteaToken    string
	GiteaBaseURL  string
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		Port:          getEnv("PORT", "8080"),
		AIBaseURL:     getEnv("AI_BASE_URL", "https://api.deepseek.com"),
		AIModel:       getEnv("AI_MODEL", "deepseek-chat"),
		AIKey:         getEnv("AI_API_KEY", ""),
		WebhookSecret: getEnv("WEBHOOK_SECRET", ""),
		GiteaToken:    getEnv("GITEA_TOKEN", ""),
		GiteaBaseURL:  getEnv("GITEA_BASE_URL", "http://localhost:3000/api/v1"),
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
