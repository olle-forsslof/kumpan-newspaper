package slack

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SlashCommandHandler handles incoming slack commands
type SlashCommandHandler struct {
	bot           Bot
	signingSecret string
}

type SlashCommandHandlerConfig struct {
	Bot           Bot
	SigningSecret string
}

// NewSlashCommandHandler creates a new handler for Slack slash commands
func NewSlashCommandHandler(bot Bot) *SlashCommandHandler {
	return &SlashCommandHandler{
		bot:           bot,
		signingSecret: "",
	}
}

func NewSlashCommandHandlerWithSecurity(bot Bot, signingSecret string) *SlashCommandHandler {
	return &SlashCommandHandler{
		bot:           bot,
		signingSecret: signingSecret,
	}
}

// ServeHTTP handles the incoming HTTP request from Slack
func (h *SlashCommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TEMPORARY: Ultra-minimal response for debugging
	slog.Info("Received slash command request - returning minimal response")

	// set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Return absolute minimal response
	response := SlashCommandResponse{
		Text:         "DEBUG: Minimal response working!",
		ResponseType: "ephemeral",
	}

	// Send json response back to Slack
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode response", "error", err)
		return
	}
}
