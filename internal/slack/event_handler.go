package slack

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

type EventCallbackHandler struct {
	bot           Bot
	signingSecret string
}

func NewEventCallbackHandler(bot Bot, signingSecret string) *EventCallbackHandler {
	return &EventCallbackHandler{
		bot:           bot,
		signingSecret: signingSecret,
	}
}

func (h *EventCallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the raw body first (needed for signature verification)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("Failed to read request body", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Verify request signature
	if h.signingSecret != "" {
		signature := r.Header.Get("X-Slack-Signature")
		timestamp := r.Header.Get("X-Slack-Request-Timestamp")

		// Use the raw body for signature verification
		if !VerifySignature(h.signingSecret, timestamp, string(rawBody), signature) {
			slog.Warn("Invalid signature - rejecting request",
				"signature", signature,
				"timestamp", timestamp)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Parse the event payload from raw body
	var payload struct {
		Type      string     `json:"type"`
		Event     SlackEvent `json:"event"`
		Challenge string     `json:"challenge"` // For URL verification
	}

	if err := json.Unmarshal(rawBody, &payload); err != nil {
		slog.Error("Failed to parse event payload", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// URL verification challenge - special case when Slack verifies your endpoint
	if payload.Type == "url_verification" {
		// Respond with challenge token
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(payload.Challenge))
		return
	}

	// Handle regular events
	if payload.Type == "event_callback" {
		if err := h.bot.HandleEventCallback(r.Context(), payload.Event); err != nil {
			slog.Error("Failed to handle event", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Slack expects a 200 OK response quickly
		w.WriteHeader(http.StatusOK)
		return
	}

	// Unhandled event type
	slog.Warn("Unhandled Slack event type", "type", payload.Type)
	w.WriteHeader(http.StatusOK) //  Still return 200 to Slack
}
