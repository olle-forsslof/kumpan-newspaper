package slack

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TestWeeklyAutomationAdminCommands tests the admin commands for weekly automation
func TestWeeklyAutomationAdminCommands(t *testing.T) {
	// Create a temporary database for testing
	tempFile := "/tmp/test_admin_weekly.db"
	defer os.Remove(tempFile)

	db, err := database.NewSimple(tempFile)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	ctx := context.Background()

	t.Run("AdminPoolStatusCommand", testAdminPoolStatusCommand(ctx, db))
	t.Run("AdminWeekStatusCommand", testAdminWeekStatusCommand(ctx, db))
	t.Run("AdminAssignQuestionCommand", testAdminAssignQuestionCommand(ctx, db))
	t.Run("AdminBroadcastCommand", testAdminBroadcastCommand(ctx, db))
	t.Run("AdminAuthorization", testAdminAuthorization(ctx, db))
}

func testAdminPoolStatusCommand(ctx context.Context, db *database.DB) func(t *testing.T) {
	return func(t *testing.T) {
		// Create admin handler with weekly automation capabilities
		adminUsers := []string{"U123ADMIN"}
		handler := NewAdminHandlerWithWeeklyAutomation(
			&mockQuestionSelector{},
			adminUsers,
			&mockSubmissionManager{},
			db,
			"fake-token",
		)

		// Add some questions to the pool first
		poolManager := database.NewBodyMindPoolManager(db)
		testQuestions := []struct {
			Text     string
			Category string
		}{
			{"How do you manage stress?", "wellness"},
			{"What's your mindfulness practice?", "mental_health"},
		}

		_, err := poolManager.BulkAddQuestions(testQuestions)
		if err != nil {
			t.Fatalf("Failed to add test questions: %v", err)
		}

		// Test pool status command
		cmd := &AdminCommand{
			Action: "pool-status",
			Args:   []string{},
		}

		response, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd)
		if err != nil {
			t.Fatalf("Failed to handle pool-status command: %v", err)
		}

		if response.ResponseType != "ephemeral" {
			t.Errorf("Expected ephemeral response, got %s", response.ResponseType)
		}

		// Debug: Print the actual response
		t.Logf("Pool status response: %s", response.Text)

		// Response should contain pool status information
		expectedStrings := []string{
			"Body/Mind Question Pool Status",
			"Available Questions:* 2",
			"Wellness: 1 questions",
			"Mental Health: 1 questions",
		}

		for _, expected := range expectedStrings {
			if !strings.Contains(response.Text, expected) {
				t.Errorf("Pool status response missing expected text: %s", expected)
			}
		}
	}
}

func testAdminWeekStatusCommand(ctx context.Context, db *database.DB) func(t *testing.T) {
	return func(t *testing.T) {
		adminUsers := []string{"U123ADMIN"}
		handler := NewAdminHandlerWithWeeklyAutomation(
			&mockQuestionSelector{},
			adminUsers,
			&mockSubmissionManager{},
			db,
			"fake-token",
		)

		// Create a test issue and assignments
		issue, err := db.CreateWeeklyNewsletterIssue(42, 2025)
		if err != nil {
			t.Fatalf("Failed to create test issue: %v", err)
		}

		assignment := database.PersonAssignment{
			IssueID:     issue.ID,
			PersonID:    "U456USER",
			ContentType: database.ContentTypeFeature,
			AssignedAt:  issue.CreatedAt, // Use the issue creation time
		}

		_, err = db.CreatePersonAssignment(assignment)
		if err != nil {
			t.Fatalf("Failed to create test assignment: %v", err)
		}

		// Test week status command
		cmd := &AdminCommand{
			Action: "week-status",
			Args:   []string{},
		}

		response, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd)
		if err != nil {
			t.Fatalf("Failed to handle week-status command: %v", err)
		}

		// Response should be a placeholder since we haven't fully implemented it
		if !strings.Contains(response.Text, "Week status not fully implemented") {
			t.Error("Expected placeholder response for week status")
		}
	}
}

func testAdminAssignQuestionCommand(ctx context.Context, db *database.DB) func(t *testing.T) {
	return func(t *testing.T) {
		adminUsers := []string{"U123ADMIN"}
		mockQuestionSel := &mockQuestionSelector{}
		handler := NewAdminHandlerWithWeeklyAutomation(
			mockQuestionSel,
			adminUsers,
			&mockSubmissionManager{},
			db,
			"fake-token",
		)

		// Add some body_mind questions for testing
		poolManager := database.NewBodyMindPoolManager(db)
		testQuestions := []struct {
			Text     string
			Category string
		}{
			{"How do you manage stress at work?", "wellness"},
			{"What's your favorite mindfulness practice?", "mental_health"},
		}

		_, err := poolManager.BulkAddQuestions(testQuestions)
		if err != nil {
			t.Fatalf("Failed to add test body_mind questions: %v", err)
		}

		// Test assign-question command with feature category
		cmd := &AdminCommand{
			Action: "assign-question",
			Args:   []string{"feature", "@U789USER"},
		}

		response, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd)
		if err != nil {
			t.Fatalf("Failed to handle assign-question command: %v", err)
		}

		// Should have successful assignment response
		if !strings.Contains(response.Text, "Successfully assigned") {
			t.Errorf("Expected successful assignment, got: %s", response.Text)
		}

		// Should contain question and user info
		if !strings.Contains(response.Text, "feature") || !strings.Contains(response.Text, "U789USER") {
			t.Errorf("Response should contain category and user info: %s", response.Text)
		}

		// Verify question was selected
		if !mockQuestionSel.selectNextQuestionCalled {
			t.Error("Expected SelectNextQuestion to be called")
		}

		if mockQuestionSel.lastCategory != "feature" {
			t.Errorf("Expected category 'feature', got '%s'", mockQuestionSel.lastCategory)
		}

		// Verify question was marked as used
		if !mockQuestionSel.markQuestionUsedCalled {
			t.Error("Expected MarkQuestionUsed to be called")
		}

		// Verify assignment was created in database
		// We'll need the current week issue to check assignments
		now := time.Now()
		currentYear, currentWeek := now.ISOWeek()
		issue, err := db.GetOrCreateWeeklyIssue(currentWeek, currentYear)
		if err != nil {
			t.Fatalf("Failed to get current week issue: %v", err)
		}

		assignments, err := db.GetPersonAssignmentsByIssue(issue.ID)
		if err != nil {
			t.Fatalf("Failed to get assignments: %v", err)
		}

		if len(assignments) == 0 {
			t.Error("Expected at least one assignment to be created")
		}

		assignment := assignments[0]
		if assignment.PersonID != "U789USER" {
			t.Errorf("Expected assignment to U789USER, got %s", assignment.PersonID)
		}

		if assignment.ContentType != database.ContentTypeFeature {
			t.Errorf("Expected feature content type, got %s", assignment.ContentType)
		}

		if assignment.QuestionID == nil {
			t.Error("Expected assignment to have a question ID")
		}

		// Test body_mind category support
		bodyMindCmd := &AdminCommand{
			Action: "assign-question",
			Args:   []string{"body_mind", "@U456WELLNESS"},
		}

		bodyMindResponse, err := handler.HandleAdminCommand(ctx, "U123ADMIN", bodyMindCmd)
		if err != nil {
			t.Fatalf("Failed to handle body_mind assign-question command: %v", err)
		}

		if !strings.Contains(bodyMindResponse.Text, "Successfully assigned") {
			t.Errorf("Expected successful body_mind assignment, got: %s", bodyMindResponse.Text)
		}

		// Test invalid content type
		invalidCmd := &AdminCommand{
			Action: "assign-question",
			Args:   []string{"invalid_type", "@U789USER"},
		}

		invalidResponse, err := handler.HandleAdminCommand(ctx, "U123ADMIN", invalidCmd)
		if err != nil {
			t.Fatalf("Failed to handle invalid assign-question command: %v", err)
		}

		if !strings.Contains(invalidResponse.Text, "Content type must be") {
			t.Error("Expected validation error for invalid content type")
		}

		// Test insufficient arguments
		shortCmd := &AdminCommand{
			Action: "assign-question",
			Args:   []string{"feature"},
		}

		shortResponse, err := handler.HandleAdminCommand(ctx, "U123ADMIN", shortCmd)
		if err != nil {
			t.Fatalf("Failed to handle short assign-question command: %v", err)
		}

		if !strings.Contains(shortResponse.Text, "Usage: admin assign-question") {
			t.Error("Expected usage message for insufficient arguments")
		}

		// Test multiple users
		multiCmd := &AdminCommand{
			Action: "assign-question",
			Args:   []string{"general", "@U111USER", "@U222USER"},
		}

		multiResponse, err := handler.HandleAdminCommand(ctx, "U123ADMIN", multiCmd)
		if err != nil {
			t.Fatalf("Failed to handle multi-user assign-question command: %v", err)
		}

		if !strings.Contains(multiResponse.Text, "Successfully assigned") {
			t.Error("Expected successful multi-user assignment")
		}
	}
}

func testAdminBroadcastCommand(ctx context.Context, db *database.DB) func(t *testing.T) {
	return func(t *testing.T) {
		adminUsers := []string{"U123ADMIN"}
		handler := NewAdminHandlerWithWeeklyAutomation(
			&mockQuestionSelector{},
			adminUsers,
			&mockSubmissionManager{},
			db,
			"fake-token",
		)

		// Test broadcast command
		cmd := &AdminCommand{
			Action: "broadcast-bodymind",
			Args:   []string{},
		}

		response, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd)
		if err != nil {
			t.Fatalf("Failed to handle broadcast-bodymind command: %v", err)
		}

		// Debug: Print the actual response
		t.Logf("Broadcast response: %s", response.Text)

		// Since we're using a fake token, the broadcast should fail with auth error
		// This is actually the correct behavior - testing that the system attempts the broadcast
		if !strings.Contains(response.Text, "Failed to send broadcast") {
			t.Error("Expected broadcast to fail with authentication error when using fake token")
		}

		// The error should be related to authentication
		if !strings.Contains(response.Text, "invalid_auth") {
			t.Error("Expected authentication error in broadcast response")
		}
	}
}

func testAdminAuthorization(ctx context.Context, db *database.DB) func(t *testing.T) {
	return func(t *testing.T) {
		adminUsers := []string{"U123ADMIN"}
		handler := NewAdminHandlerWithWeeklyAutomation(
			&mockQuestionSelector{},
			adminUsers,
			&mockSubmissionManager{},
			db,
			"fake-token",
		)

		// Test unauthorized user
		cmd := &AdminCommand{
			Action: "pool-status",
			Args:   []string{},
		}

		response, err := handler.HandleAdminCommand(ctx, "U999UNAUTHORIZED", cmd)
		if err != nil {
			t.Fatalf("Failed to handle command from unauthorized user: %v", err)
		}

		if !strings.Contains(response.Text, "not authorized to use admin commands") {
			t.Error("Expected authorization error for unauthorized user")
		}

		// Test authorized user can access commands
		authorizedResponse, err := handler.HandleAdminCommand(ctx, "U123ADMIN", cmd)
		if err != nil {
			t.Fatalf("Failed to handle command from authorized user: %v", err)
		}

		if strings.Contains(authorizedResponse.Text, "not authorized") {
			t.Error("Authorized user should be able to access admin commands")
		}
	}
}

// TestBroadcastManagerFunctionality tests the broadcast messaging system
func TestBroadcastManagerFunctionality(t *testing.T) {
	t.Run("BroadcastManagerCreation", testBroadcastManagerCreation)
	t.Run("WellnessMessageGeneration", testWellnessMessageGeneration)
	t.Run("BroadcastResultHandling", testBroadcastResultHandling)
}

func testBroadcastManagerCreation(t *testing.T) {
	manager := NewBroadcastManager("test-token")
	if manager == nil {
		t.Error("Expected non-nil broadcast manager")
	}

	if manager.client == nil {
		t.Error("Expected broadcast manager to have Slack client")
	}
}

func testWellnessMessageGeneration(t *testing.T) {
	manager := NewBroadcastManager("test-token")
	message := manager.createWellnessBroadcastMessage()

	if message == "" {
		t.Error("Expected non-empty wellness broadcast message")
	}

	// Message should contain key elements
	expectedStrings := []string{
		"Help us expand our wellness content pool",
		"/pp submit-wellness",
		"wellness",
		"mental_health",
		"work_life_balance",
		"anonymous",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(message, expected) {
			t.Errorf("Wellness message missing expected text: %s", expected)
		}
	}
}

func testBroadcastResultHandling(t *testing.T) {
	// Test successful broadcast result
	successResult := &BroadcastResult{
		TotalUsers:      10,
		SuccessfulSends: 10,
		FailedSends:     0,
		Errors:          []string{},
	}

	summary := successResult.GetSummary()
	if !strings.Contains(summary, "Successfully sent wellness question request to all 10") {
		t.Errorf("Expected success summary, got: %s", summary)
	}

	// Test partial failure result
	partialResult := &BroadcastResult{
		TotalUsers:      10,
		SuccessfulSends: 8,
		FailedSends:     2,
		Errors:          []string{"Failed to send to user1", "Failed to send to user2"},
	}

	partialSummary := partialResult.GetSummary()
	if !strings.Contains(partialSummary, "Sent to 8 of 10 users (2 failed)") {
		t.Errorf("Expected partial failure summary, got: %s", partialSummary)
	}

	// Test detailed report with errors
	detailedReport := partialResult.GetDetailedReport()
	if !strings.Contains(detailedReport, "Errors encountered") {
		t.Error("Expected detailed report to include errors section")
	}

	if !strings.Contains(detailedReport, "Failed to send to user1") {
		t.Error("Expected detailed report to include specific error messages")
	}
}

// Mock implementations for testing

type mockQuestionSelector struct {
	selectNextQuestionCalled bool
	markQuestionUsedCalled   bool
	lastCategory             string
	lastQuestionID           int
}

func (m *mockQuestionSelector) SelectNextQuestion(ctx context.Context, category string) (*database.Question, error) {
	m.selectNextQuestionCalled = true
	m.lastCategory = category
	return &database.Question{
		ID:       1,
		Text:     "Mock question for " + category,
		Category: category,
	}, nil
}

func (m *mockQuestionSelector) MarkQuestionUsed(ctx context.Context, questionID int) error {
	m.markQuestionUsedCalled = true
	m.lastQuestionID = questionID
	return nil
}

func (m *mockQuestionSelector) GetQuestionsByCategory(ctx context.Context, category string) ([]database.Question, error) {
	return []database.Question{
		{ID: 1, Text: "Mock question 1", Category: category},
		{ID: 2, Text: "Mock question 2", Category: category},
	}, nil
}

func (m *mockQuestionSelector) AddQuestion(ctx context.Context, text, category string) (*database.Question, error) {
	return &database.Question{
		ID:       3,
		Text:     text,
		Category: category,
	}, nil
}

func (m *mockQuestionSelector) GetQuestionByID(ctx context.Context, questionID int) (*database.Question, error) {
	return &database.Question{
		ID:       questionID,
		Text:     "Mock question by ID",
		Category: "general",
	}, nil
}

type mockSubmissionManager struct{}

func (m *mockSubmissionManager) CreateNewsSubmission(ctx context.Context, userID, content string) (*database.Submission, error) {
	return &database.Submission{
		ID:      1,
		UserID:  userID,
		Content: content,
	}, nil
}

func (m *mockSubmissionManager) GetSubmissionsByUser(ctx context.Context, userID string) ([]database.Submission, error) {
	return []database.Submission{
		{ID: 1, UserID: userID, Content: "Mock submission"},
	}, nil
}

func (m *mockSubmissionManager) GetAllSubmissions(ctx context.Context) ([]database.Submission, error) {
	return []database.Submission{
		{ID: 1, UserID: "U123", Content: "Mock submission 1"},
		{ID: 2, UserID: "U456", Content: "Mock submission 2"},
	}, nil
}

func (m *mockSubmissionManager) DeleteSubmission(ctx context.Context, id int) error {
	return nil // Mock implementation - always succeeds
}

type mockBroadcastManager struct {
	sendDirectMessageCalled bool
	lastUserID              string
	lastMessage             string
	lastAssignmentIssueID   int
}

func (m *mockBroadcastManager) sendDirectMessage(ctx context.Context, userID, message string) error {
	m.sendDirectMessageCalled = true
	m.lastUserID = userID
	m.lastMessage = message
	return nil
}

func (m *mockBroadcastManager) BroadcastBodyMindRequest(ctx context.Context) (*BroadcastResult, error) {
	return &BroadcastResult{
		TotalUsers:      1,
		SuccessfulSends: 1,
		FailedSends:     0,
		Errors:          []string{},
	}, nil
}
