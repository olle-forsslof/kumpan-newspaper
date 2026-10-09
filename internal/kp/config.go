package kp

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type Config struct {
	Port, DatabasePath, BaseURL, WorkspaceID, SigningSecret string
	BotToken, ClientID, ClientSecret, SessionSecret, APIKey string
	Model, PublishChannel                                   string
	EditorIDs                                               []string
	UnsplashAccessKey                                       string
}

// LoadConfig reads only the process environment. Errors name the invalid
// variable, never its value, so callers can safely log configuration failures.
func LoadConfig() (Config, error) {
	cfg := Config{
		Port:              os.Getenv("PORT"),
		BaseURL:           os.Getenv("BASE_URL"),
		WorkspaceID:       os.Getenv("SLACK_WORKSPACE_ID"),
		SigningSecret:     os.Getenv("SLACK_SIGNING_SECRET"),
		BotToken:          os.Getenv("SLACK_BOT_TOKEN"),
		ClientID:          os.Getenv("SLACK_CLIENT_ID"),
		ClientSecret:      os.Getenv("SLACK_CLIENT_SECRET"),
		SessionSecret:     os.Getenv("SESSION_SECRET"),
		APIKey:            os.Getenv("OPENAI_API_KEY"),
		Model:             os.Getenv("OPENAI_MODEL"),
		PublishChannel:    os.Getenv("SLACK_PUBLISH_CHANNEL"),
		UnsplashAccessKey: os.Getenv("UNSPLASH_ACCESS_KEY"),
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 || strings.ContainsFunc(cfg.Port, func(r rune) bool { return r < '0' || r > '9' }) {
		return Config{}, errors.New("PORT")
	}
	var present bool
	cfg.DatabasePath, present = os.LookupEnv("DATABASE_PATH")
	if !present {
		cfg.DatabasePath = "kp.db"
	}
	if strings.TrimSpace(cfg.DatabasePath) == "" {
		return Config{}, errors.New("DATABASE_PATH")
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4.1-mini"
	}
	for _, field := range []struct{ name, value string }{
		{"SLACK_WORKSPACE_ID", cfg.WorkspaceID},
		{"SLACK_SIGNING_SECRET", cfg.SigningSecret},
		{"SLACK_BOT_TOKEN", cfg.BotToken},
		{"SLACK_CLIENT_ID", cfg.ClientID},
		{"SLACK_CLIENT_SECRET", cfg.ClientSecret},
		{"SESSION_SECRET", cfg.SessionSecret},
		{"OPENAI_API_KEY", cfg.APIKey},
		{"OPENAI_MODEL", cfg.Model},
		{"SLACK_PUBLISH_CHANNEL", cfg.PublishChannel},
	} {
		if field.value == "" || strings.ContainsFunc(field.value, unicode.IsSpace) {
			return Config{}, errors.New(field.name)
		}
	}
	if len(cfg.SessionSecret) < 32 {
		return Config{}, errors.New("SESSION_SECRET")
	}
	if strings.ContainsFunc(cfg.UnsplashAccessKey, unicode.IsSpace) {
		return Config{}, errors.New("UNSPLASH_ACCESS_KEY")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.BaseURL, "#") || (u.Path != "" && u.Path != "/") || u.Opaque != "" || strings.ContainsFunc(cfg.BaseURL, unicode.IsSpace) {
		return Config{}, errors.New("BASE_URL")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return Config{}, errors.New("BASE_URL")
	}
	if strings.HasSuffix(u.Host, ":") {
		return Config{}, errors.New("BASE_URL")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return Config{}, errors.New("BASE_URL")
		}
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	seen := make(map[string]bool)
	for _, id := range strings.Split(os.Getenv("ADMIN_USERS"), ",") {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if strings.ContainsFunc(id, unicode.IsSpace) {
			return Config{}, errors.New("ADMIN_USERS")
		}
		seen[id] = true
		cfg.EditorIDs = append(cfg.EditorIDs, id)
	}
	if len(cfg.EditorIDs) == 0 {
		return Config{}, errors.New("ADMIN_USERS")
	}
	return cfg, nil
}
