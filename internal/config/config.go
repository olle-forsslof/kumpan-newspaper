package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port               string
	LogLevel           string
	Env                string
	SlackBotToken      string
	SlackSigningSecret string
	AdminUsers         []string
	DatabasePath       string
	AnthropicAPIKey    string
}

func Load() *Config {
	var adminUsers []string
	if admins := getEnv("ADMIN_USERS", ""); admins != "" {
		adminUsers = strings.Split(admins, ",")
	}
	return &Config{
		Port:               getEnv("PORT", "8080"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		Env:                getEnv("ENVIRONMENT", "development"),
		SlackBotToken:      getEnv("SLACK_BOT_TOKEN", ""),
		SlackSigningSecret: getEnv("SLACK_SIGNING_SECRET", ""),
		AdminUsers:         adminUsers,
		DatabasePath:       getEnv("DATABASE_PATH", "newsletter.db"),
		AnthropicAPIKey:    getEnv("ANTHROPIC_API_KEY", ""),
	}
}

func (c *Config) Validate() error {
	if c.SlackBotToken == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN is required")
	}
	if c.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
