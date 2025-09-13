package slack

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TDD: Test admin command to list all submissions
func TestAdminHandler_ListSubmissions(t *testing.T) {
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

	// Create SubmissionManager and add test data
	submissionManager := database.NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Add some test submissions
	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "First news story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	_, err = submissionManager.CreateNewsSubmission(ctx, "U222222222", "Second news story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create admin handler - THIS WILL FAIL initially because it doesn't accept SubmissionManager
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test admin list-submissions command
	cmd := &AdminCommand{
		Action: "list-submissions",
		Args:   []string{},
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify response contains both submissions
	if !strings.Contains(response.Text, "First news story") {
		t.Error("Expected response to contain first news story")
	}

	if !strings.Contains(response.Text, "Second news story") {
		t.Error("Expected response to contain second news story")
	}

	if !strings.Contains(response.Text, "U111111111") {
		t.Error("Expected response to contain first user ID")
	}

	if !strings.Contains(response.Text, "U222222222") {
		t.Error("Expected response to contain second user ID")
	}
}

// TDD: Test admin command to list submissions by user
func TestAdminHandler_ListSubmissionsByUser(t *testing.T) {
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

	// Create SubmissionManager and add test data
	submissionManager := database.NewSubmissionManager(db.DB)
	ctx := context.Background()

	// Add submissions from different users
	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "User 1 story 1")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	_, err = submissionManager.CreateNewsSubmission(ctx, "U111111111", "User 1 story 2")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	_, err = submissionManager.CreateNewsSubmission(ctx, "U222222222", "User 2 story")
	if err != nil {
		t.Fatalf("Failed to create test submission: %v", err)
	}

	// Create admin handler
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"}
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test admin list-submissions command with user filter
	cmd := &AdminCommand{
		Action: "list-submissions",
		Args:   []string{"U111111111"}, // Filter by specific user
	}

	response, err := adminHandler.HandleAdminCommand(ctx, "U999999999", cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify response contains only User 1's submissions
	if !strings.Contains(response.Text, "User 1 story 1") {
		t.Error("Expected response to contain User 1 story 1")
	}

	if !strings.Contains(response.Text, "User 1 story 2") {
		t.Error("Expected response to contain User 1 story 2")
	}

	if strings.Contains(response.Text, "User 2 story") {
		t.Error("Expected response to NOT contain User 2 story when filtering by User 1")
	}
}

// TDD: Test unauthorized user cannot access submission management
func TestAdminHandler_UnauthorizedSubmissionAccess(t *testing.T) {
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

	// Create admin handler
	submissionManager := database.NewSubmissionManager(db.DB)
	questionSelector := database.NewQuestionSelector(db.DB)
	adminUsers := []string{"U999999999"} // Only this user is authorized
	adminHandler := NewAdminHandlerWithSubmissions(questionSelector, adminUsers, submissionManager)

	// Test unauthorized user trying to list submissions
	cmd := &AdminCommand{
		Action: "list-submissions",
		Args:   []string{},
	}

	unauthorizedUserID := "U888888888"
	response, err := adminHandler.HandleAdminCommand(context.Background(), unauthorizedUserID, cmd)
	if err != nil {
		t.Fatalf("HandleAdminCommand failed: %v", err)
	}

	// Verify unauthorized access is denied
	if !strings.Contains(response.Text, "not authorized") {
		t.Errorf("Expected unauthorized message, got: %s", response.Text)
	}
}
