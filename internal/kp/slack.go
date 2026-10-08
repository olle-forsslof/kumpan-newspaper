package kp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// NewCommandHandler accepts signed Slack commands without making Slack API calls.
func NewCommandHandler(store *Store, signingSecret, workspaceID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(signingSecret) == "" || strings.TrimSpace(workspaceID) == "" || store == nil || store.DB == nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			http.Error(w, "Invalid content type", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "Request too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "Invalid request", http.StatusBadRequest)
			}
			return
		}
		timestamp := r.Header.Get("X-Slack-Request-Timestamp")
		seconds, err := strconv.ParseInt(timestamp, 10, 64)
		now := time.Now().Unix()
		if err != nil || seconds < now-300 || seconds > now+60 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		signature := r.Header.Get("X-Slack-Signature")
		if len(signature) != 3+sha256.Size*2 || !strings.HasPrefix(signature, "v0=") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		provided, err := hex.DecodeString(signature[3:])
		mac := hmac.New(sha256.New, []byte(signingSecret))
		mac.Write([]byte("v0:" + timestamp + ":"))
		mac.Write(raw)
		if err != nil || !hmac.Equal(provided, mac.Sum(nil)) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil || !utf8.Valid(raw) {
			http.Error(w, "Invalid form encoding", http.StatusBadRequest)
			return
		}
		for _, field := range []string{"team_id", "user_id", "user_name", "command", "text"} {
			if len(form[field]) > 1 || !utf8.ValidString(form.Get(field)) {
				http.Error(w, "Invalid form encoding", http.StatusBadRequest)
				return
			}
		}
		if form.Get("team_id") != workspaceID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.TrimSpace(form.Get("user_id")) == "" {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		respond := func(text string) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(struct {
				ResponseType string `json:"response_type"`
				Text         string `json:"text"`
			}{"ephemeral", text})
		}
		var kind string
		switch form.Get("command") {
		case "/tellkp":
			kind = KindReport
		case "/askkp":
			kind = KindQuestion
		default:
			respond("Anv\u00e4nd /tellkp <nyhet eller rapport> eller /askkp <fr\u00e5ga>. Fr\u00e5gor sparas och kan publiceras anonymt.")
			return
		}
		text := strings.TrimSpace(form.Get("text"))
		if text == "" {
			if kind == KindQuestion {
				respond("Skriv /askkp <din fr\u00e5ga>. Fr\u00e5gan sparas utan avs\u00e4ndaruppgifter och kan publiceras anonymt. Slack ser avs\u00e4ndaren n\u00e4r kommandot skickas.")
			} else {
				respond("Skriv /tellkp <din nyhet eller rapport>. Ditt Slack-namn sparas med rapporten.")
			}
			return
		}
		if len(text) > 10000 {
			respond("Texten \u00e4r f\u00f6r l\u00e5ng. Skriv h\u00f6gst 10000 byte efter kommandot.")
			return
		}
		authorID, authorName := "", ""
		if kind == KindReport {
			authorID, authorName = form.Get("user_id"), form.Get("user_name")
			if strings.TrimSpace(authorName) == "" {
				authorName = authorID
			}
		}
		// Retain only a domain-separated keyed digest, never the Slack signature.
		mac = hmac.New(sha256.New, []byte(signingSecret))
		mac.Write([]byte("kp/slack/request-key/v1\x00"))
		mac.Write([]byte(signature))
		requestKey := hex.EncodeToString(mac.Sum(nil))
		if _, err := store.AddSubmission(kind, authorID, authorName, text, requestKey); err != nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		if kind == KindQuestion {
			respond("Tack! Din fr\u00e5ga har sparats utan avs\u00e4ndaruppgifter och kan publiceras anonymt. Slack ser avs\u00e4ndaren n\u00e4r kommandot skickas.")
		} else {
			respond("Tack! Din rapport har sparats med ditt Slack-namn f\u00f6r kommande publicering.")
		}
	})
}
