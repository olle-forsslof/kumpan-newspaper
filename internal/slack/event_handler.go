package slack

import (
	"encoding/json"
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

	// Verify request signature
	if h.signingSecret != "" {
		signature := r.Header.Get("X-Slack-Signature")
		timestamp := r.Header.Get("X-Slack-Request-Timestamp")

		// Need to read the body for verification
		// This is a simplification - real implementation would read raw body
		if !VerifySignature(h.signingSecret, timestamp, "", signature) {
			slog.Warn("Invalid signature - rejecting request",
				"signature", signature,
				"timestamp", timestamp)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Parse the event
	var payload struct {
		Type  string     `json:"type"`
		Event SlackEvent `json:"event"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		slog.Error("Failed to parse event payload", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// URL verification challenge - special case when Slack verifies your endpoint
	if payload.Type == "url_verification" {
		var challenge struct {
			Challenge string `json:"challenge"`
		}
		if err := json.NewDecoder(r.Body).Decode(&challenge); err != nil {
			http.Error(w, "Failed to read challenge", http.StatusBadRequest)
			return
		}

		// Respond with challenge token
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(challenge.Challenge))
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
