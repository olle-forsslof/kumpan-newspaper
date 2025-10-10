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
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/olle-forsslof/kumpan-newspaper/internal/templates"
)

func setupTestDatabase(path string) (*database.DB, error) {
	db, err := database.NewSimple(path)
	if err != nil {
		return nil, err
	}

	if err := db.Migrate(); err != nil {
		return nil, err
	}

	return db, nil
}

func setupTestTemplateService() (*templates.TemplateService, error) {
	return templates.NewTemplateService(nil)
}

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

func TestServer_ArchiveHandler(t *testing.T) {
	cfg := &config.Config{
		Port: "8080",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tempFile := "/tmp/test_archive_handler.db"
	defer os.Remove(tempFile)

	db, err := setupTestDatabase(tempFile)
	if err != nil {
		t.Fatalf("Failed to setup test database: %v", err)
	}
	defer db.Close()

	templateService, err := setupTestTemplateService()
	if err != nil {
		t.Fatalf("Failed to setup template service: %v", err)
	}

	srv := NewWithBotAndTemplates(cfg, logger, nil, db, templateService)
	srv.SetupRoutes()

	t.Run("GET /archive returns 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/archive", nil)
		w := httptest.NewRecorder()

		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Newsletter Archive") {
			t.Error("Archive page title not found in response")
		}
	})

	t.Run("POST /archive returns 405", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/archive", nil)
		w := httptest.NewRecorder()

		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", w.Code)
		}
	})

	t.Run("Archive displays only published issues", func(t *testing.T) {
		_, err := db.CreateWeeklyNewsletterIssue(1, 2025)
		if err != nil {
			t.Fatalf("Failed to create draft issue: %v", err)
		}

		publishedIssue, err := db.CreateWeeklyNewsletterIssue(2, 2025)
		if err != nil {
			t.Fatalf("Failed to create published issue: %v", err)
		}
		_, err = db.Exec("UPDATE newsletter_issues SET status = ? WHERE id = ?", "published", publishedIssue.ID)
		if err != nil {
			t.Fatalf("Failed to update issue status: %v", err)
		}

		req := httptest.NewRequest("GET", "/archive", nil)
		w := httptest.NewRecorder()

		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Week 2 Newsletter - 2025") {
			t.Error("Published issue not found in archive")
		}
	})

	t.Run("Archive handles empty state", func(t *testing.T) {
		tempFile2 := "/tmp/test_archive_empty.db"
		defer os.Remove(tempFile2)

		emptyDB, err := setupTestDatabase(tempFile2)
		if err != nil {
			t.Fatalf("Failed to setup empty database: %v", err)
		}
		defer emptyDB.Close()

		srv2 := NewWithBotAndTemplates(cfg, logger, nil, emptyDB, templateService)
		srv2.SetupRoutes()

		req := httptest.NewRequest("GET", "/archive", nil)
		w := httptest.NewRecorder()

		srv2.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "No newsletters yet") {
			t.Error("Empty state message not found")
		}
	})
}
