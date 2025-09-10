package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/config"
)

func TestServer_SlackIntegration(t *testing.T) {
	// Create test configuration with Slack enabled
	cfg := &config.Config{
		Port:               "8080",
		SlackBotToken:      "xoxb-test-token",
		SlackSigningSecret: "test-signing-secret",
	}

	// Create server (this should initialize the Slack bot)
	srv := New(cfg, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	srv.SetupRoutes()

	// Test that Slack command endpoint exists and responds
	form := url.Values{}
	form.Add("token", "test-token")
	form.Add("command", "/newsletter")
	form.Add("text", "test submission")
	form.Add("user_id", "U123456")
	form.Add("channel_id", "C123456")

	req := httptest.NewRequest("POST", "/api/slack/commands",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Record the response
	w := httptest.NewRecorder()

	// Make the request through our server's router
	srv.Handler().ServeHTTP(w, req)

	// Verify we got a response (not 404)
	if w.Code == http.StatusNotFound {
		t.Fatal("Slack command endpoint not registered - integration failed")
	}
	// We expect either 200 (success) or 401 (signature verification failed)
	// 401 is fine because we're not signing our test request properly
	if w.Code != http.StatusOK && w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 200 or 401, got %d", w.Code)
	}

	t.Logf("Slack integration test passed - endpoint registered and responding")
}

func TestServer_SlackDisabled(t *testing.T) {
	// Test that server works when Slack is not configured
	cfg := &config.Config{
		Port: "8080",
		// No Slack tokens - should disable Slack integration
	}

	srv := New(cfg, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	srv.SetupRoutes()

	// Health check should still work
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health check failed when Slack disabled: got %d", w.Code)
	}

	// Slack commands should be handled by the root handler when Slack is disabled
	slackReq := httptest.NewRequest("POST", "/api/slack/commands", nil)
	slackW := httptest.NewRecorder()

	srv.Handler().ServeHTTP(slackW, slackReq)

	// The root handler should have taken over
	if slackW.Body.String() != "Newsletter service is running" {
		t.Errorf("Expected default handler response, got: %s", slackW.Body.String())
	}

	t.Log("Server gracefully handles disabled Slack integration")
}
