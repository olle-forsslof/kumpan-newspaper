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

	// Parse the form data
	if err := r.ParseForm(); err != nil {
		slog.Error("Failed to parse form data", "Error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Signature verification (only if signing secret is configured)
	if h.signingSecret != "" {
		// Extract signature headers
		signature := r.Header.Get("X-Slack-Signature")
		timestamp := r.Header.Get("X-Slack-Request-Timestamp")

		// Get the raw body for signature verification
		// Note: r.PostForm.Encode() recreates the original form-encoded body
		body := r.PostForm.Encode()

		// Verify the signature
		if !VerifySignature(h.signingSecret, timestamp, body, signature) {
			slog.Warn("Invalid signature - rejecting request",
				"signature", signature,
				"timestamp", timestamp)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		slog.Info("Signature verified successfully")
	}

	// Extract Slack commands from the form
	command := SlashCommand{
		Token:       r.FormValue("token"),
		Command:     r.FormValue("command"),
		Text:        r.FormValue("text"),
		UserID:      r.FormValue("user_id"),
		ChannelID:   r.FormValue("channel_id"),
		ResponseURL: r.FormValue("response_url"),
	}

	// Log the incoming command for debugging
	slog.Info("Received slash command",
		"command", command.Command,
		"user", command.UserID,
		"text", command.Text,
	)

	// Handle the command using our bot
	response, err := h.bot.HandleSlashCommand(r.Context(), command)
	if err != nil {
		slog.Error("Failed to handle slash command", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Send json response back to Slack
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode response", "error", err)
		return
	}
}
