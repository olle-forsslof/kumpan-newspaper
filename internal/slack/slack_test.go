package slack

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

func TestSlackBot_SendMessage(t *testing.T) {
	// We want our bot to be able to send messages
	bot := NewMockBot()

	err := bot.SendMessage(context.Background(), "C1234567", "Hello, World!")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
}

func TestSlackBot_HandleSlashCommand(t *testing.T) {
	// We want to handle slash commands like /submit
	// Use nil for now since we're just testing basic functionality
	bot := NewBot(SlackConfig{Token: "xoxb-test-token"}, nil, []string{"U1234567"})

	// Simulate a slash command payload
	command := SlashCommand{
		Command: "/submit",
		Text:    "This is my newsletter submission",
		UserID:  "U1234567",
	}

	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	if response.Text == "" {
		t.Fatal("Expected response text, got empty string")
	}
}

// TDD: Test that news submissions get stored in database
func TestSlackBot_StoreNewsSubmission(t *testing.T) {
	// Set up test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("NewSimple() failed: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	// Create SubmissionManager
	submissionManager := database.NewSubmissionManager(db.DB)

	// Create bot with the submission manager - THIS WILL FAIL initially
	bot := NewBotWithSubmissions(SlackConfig{Token: "xoxb-test-token"}, nil, []string{"U1234567"}, submissionManager)

	// Simulate news submission command
	command := SlashCommand{
		Command: "/pp",
		Text:    "submit Our team shipped the new analytics dashboard!",
		UserID:  "U987654321",
	}

	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Verify response indicates successful submission
	if !strings.Contains(response.Text, "submission received") {
		t.Errorf("Expected success message, got: %s", response.Text)
	}

	// Verify submission was actually stored in database
	submissions, err := submissionManager.GetSubmissionsByUser(context.Background(), "U987654321")
	if err != nil {
		t.Fatalf("GetSubmissionsByUser failed: %v", err)
	}

	if len(submissions) != 1 {
		t.Errorf("Expected 1 stored submission, got %d", len(submissions))
	}

	if len(submissions) > 0 {
		stored := submissions[0]
		if stored.UserID != "U987654321" {
			t.Errorf("Expected UserID U987654321, got %s", stored.UserID)
		}

		expectedContent := "Our team shipped the new analytics dashboard!"
		if stored.Content != expectedContent {
			t.Errorf("Expected content '%s', got '%s'", expectedContent, stored.Content)
		}

		if stored.QuestionID != nil {
			t.Errorf("Expected QuestionID to be nil for news submission, got %v", *stored.QuestionID)
		}
	}
}
