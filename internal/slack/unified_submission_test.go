package slack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// Helper function to create test database
func createTestDB(t *testing.T) *database.DB {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.NewSimple(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Run migrations
	err = db.Migrate()
	if err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return db
}

// Test TDD Cycle 1: Categorized submission parsing and routing
func TestCategorizedSubmissionParsing(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectValid    bool
		expectCategory string
		expectContent  string
	}{
		{
			name:           "feature submission",
			input:          "submit feature My team built a new dashboard",
			expectValid:    true,
			expectCategory: "feature",
			expectContent:  "My team built a new dashboard",
		},
		{
			name:           "general submission",
			input:          "submit general Found great article on Go performance",
			expectValid:    true,
			expectCategory: "general",
			expectContent:  "Found great article on Go performance",
		},
		{
			name:           "interview submission routed as general",
			input:          "submit interview Q: What's your favorite debugging technique? A: I use...",
			expectValid:    true,
			expectCategory: "interview",
			expectContent:  "Q: What's your favorite debugging technique? A: I use...",
		},
		{
			name:           "body_mind submission",
			input:          "submit body_mind How do you manage stress during deployments?",
			expectValid:    true,
			expectCategory: "body_mind",
			expectContent:  "How do you manage stress during deployments?",
		},
		{
			name:           "backward compatibility - no category defaults to general",
			input:          "submit This is a general news story",
			expectValid:    true,
			expectCategory: "general",
			expectContent:  "This is a general news story",
		},
		{
			name:           "invalid category",
			input:          "submit invalid_category Some content",
			expectValid:    false,
			expectCategory: "",
			expectContent:  "",
		},
		{
			name:           "empty content",
			input:          "submit feature",
			expectValid:    false,
			expectCategory: "",
			expectContent:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, content, valid := parseCategorizedSubmission(tt.input)

			if valid != tt.expectValid {
				t.Errorf("Expected valid=%v, got valid=%v", tt.expectValid, valid)
			}

			if valid && category != tt.expectCategory {
				t.Errorf("Expected category=%s, got category=%s", tt.expectCategory, category)
			}

			if valid && content != tt.expectContent {
				t.Errorf("Expected content=%s, got content=%s", tt.expectContent, content)
			}
		})
	}
}

// Test TDD Cycle 2: Database methods for assignment lookup and linking
func TestGetActiveAssignmentByUser(t *testing.T) {
	// Setup test database
	testDB := createTestDB(t)
	defer testDB.Close()

	// Create a test newsletter issue for current week
	now := time.Now()
	year, week := now.ISOWeek()
	issue, err := testDB.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Create test assignments for different users (business rule: one assignment per user per week)
	featureAssignment := database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    "U123456",
		ContentType: database.ContentTypeFeature,
		AssignedAt:  time.Now(),
		CreatedAt:   time.Now(),
	}

	generalAssignment := database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    "U789012", // Different user to avoid constraint violation
		ContentType: database.ContentTypeGeneral,
		AssignedAt:  time.Now(),
		CreatedAt:   time.Now(),
	}

	// Create assignments in database
	_, err = testDB.CreatePersonAssignment(featureAssignment)
	if err != nil {
		t.Fatalf("Failed to create feature assignment: %v", err)
	}

	_, err = testDB.CreatePersonAssignment(generalAssignment)
	if err != nil {
		t.Fatalf("Failed to create general assignment: %v", err)
	}

	// Test getting active assignment by user and content type
	tests := []struct {
		name        string
		userID      string
		contentType database.ContentType
		expectFound bool
	}{
		{
			name:        "find existing feature assignment",
			userID:      "U123456",
			contentType: database.ContentTypeFeature,
			expectFound: true,
		},
		{
			name:        "find existing general assignment",
			userID:      "U789012",
			contentType: database.ContentTypeGeneral,
			expectFound: true,
		},
		{
			name:        "no assignment for different user",
			userID:      "U999999",
			contentType: database.ContentTypeFeature,
			expectFound: false,
		},
		{
			name:        "no assignment for body_mind type",
			userID:      "U123456",
			contentType: database.ContentTypeBodyMind,
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignment, err := testDB.GetActiveAssignmentByUser(tt.userID, tt.contentType)

			if tt.expectFound {
				if err != nil {
					t.Errorf("Expected to find assignment, got error: %v", err)
				}
				if assignment == nil {
					t.Error("Expected assignment object, got nil")
				}
				if assignment != nil && assignment.PersonID != tt.userID {
					t.Errorf("Expected PersonID=%s, got PersonID=%s", tt.userID, assignment.PersonID)
				}
				if assignment != nil && assignment.ContentType != tt.contentType {
					t.Errorf("Expected ContentType=%s, got ContentType=%s", tt.contentType, assignment.ContentType)
				}
			} else {
				if err == nil {
					t.Error("Expected error for non-existent assignment, got nil")
				}
				if assignment != nil {
					t.Error("Expected nil assignment, got object")
				}
			}
		})
	}
}

// Test TDD Cycle 3: Linking submissions to assignments
func TestLinkSubmissionToAssignment(t *testing.T) {
	// Setup test database
	testDB := createTestDB(t)
	defer testDB.Close()

	// Create test newsletter issue
	issue, err := testDB.GetOrCreateWeeklyIssue(37, 2025)
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	// Create test assignment
	assignment := database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    "U123456",
		ContentType: database.ContentTypeFeature,
		AssignedAt:  time.Now(),
		CreatedAt:   time.Now(),
	}

	assignmentID, err := testDB.CreatePersonAssignment(assignment)
	if err != nil {
		t.Fatalf("Failed to create assignment: %v", err)
	}

	// Create test submission
	submission := database.Submission{
		UserID:    "U123456",
		Content:   "My feature submission content",
		CreatedAt: time.Now(),
	}

	submissionID, err := testDB.CreateSubmission(&submission)
	if err != nil {
		t.Fatalf("Failed to create submission: %v", err)
	}

	// Test linking submission to assignment
	err = testDB.LinkSubmissionToAssignment(assignmentID, submissionID)
	if err != nil {
		t.Fatalf("Failed to link submission to assignment: %v", err)
	}

	// Verify the link was created
	updatedAssignment, err := testDB.GetPersonAssignmentByID(assignmentID)
	if err != nil {
		t.Fatalf("Failed to get updated assignment: %v", err)
	}

	if updatedAssignment.SubmissionID == nil {
		t.Error("Expected SubmissionID to be set, got nil")
	}

	if updatedAssignment.SubmissionID != nil && *updatedAssignment.SubmissionID != submissionID {
		t.Errorf("Expected SubmissionID=%d, got SubmissionID=%d", submissionID, *updatedAssignment.SubmissionID)
	}
}

// Test TDD Cycle 4: Anonymous body/mind submission handling
func TestAnonymousBodyMindSubmission(t *testing.T) {
	// Setup test database
	testDB := createTestDB(t)
	defer testDB.Close()

	// Test creating anonymous submission
	content := "How do you manage work-life balance?"
	category := "body_mind"

	submission, err := testDB.CreateAnonymousSubmission(content, category)
	if err != nil {
		t.Fatalf("Failed to create anonymous submission: %v", err)
	}

	// Verify submission was created anonymously
	if submission.UserID != "" {
		t.Errorf("Expected empty UserID for anonymous submission, got: %s", submission.UserID)
	}

	if submission.Content != content {
		t.Errorf("Expected content=%s, got content=%s", content, submission.Content)
	}

	// Verify we can retrieve anonymous submissions
	submissions, err := testDB.GetAnonymousSubmissionsByCategory(category)
	if err != nil {
		t.Fatalf("Failed to get anonymous submissions: %v", err)
	}

	if len(submissions) != 1 {
		t.Errorf("Expected 1 anonymous submission, got %d", len(submissions))
	}

	if submissions[0].ID != submission.ID {
		t.Error("Retrieved submission doesn't match created submission")
	}
}

// Test TDD Cycle 5: End-to-end unified submission handling
func TestUnifiedSubmissionHandler(t *testing.T) {
	// Setup test components
	testDB := createTestDB(t)
	defer testDB.Close()

	mockQuestionSelector := &MockQuestionSelector{}
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIProcessor := &MockAIService{}

	// Create bot with all components
	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		mockQuestionSelector,
		[]string{"U123456"}, // admin users
		mockSubmissionManager,
		mockAIProcessor,
		testDB,
	)

	// Create newsletter issue and assignment for current week
	now := time.Now()
	year, week := now.ISOWeek()
	issue, err := testDB.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		t.Fatalf("Failed to create test issue: %v", err)
	}

	assignment := database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    "U123456",
		ContentType: database.ContentTypeFeature,
		AssignedAt:  time.Now(),
		CreatedAt:   time.Now(),
	}

	_, err = testDB.CreatePersonAssignment(assignment)
	if err != nil {
		t.Fatalf("Failed to create assignment: %v", err)
	}

	// Test feature submission with assignment linking
	cmd := SlashCommand{
		Text:   "submit feature My team built a new dashboard this week",
		UserID: "U123456",
	}

	response, err := bot.HandleSlashCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Failed to handle categorized submission: %v", err)
	}

	if response.ResponseType != "ephemeral" {
		t.Errorf("Expected ephemeral response, got: %s", response.ResponseType)
	}

	// Check if response indicates assignment linking worked
	if !strings.Contains(response.Text, "Linked to your feature assignment") {
		t.Errorf("Expected response to indicate assignment linking, got: %s", response.Text)
	}

	// Wait a moment for async processing to complete
	time.Sleep(100 * time.Millisecond)

	// Verify submission was linked to assignment
	assignments, err := testDB.GetPersonAssignmentsByIssue(issue.ID)
	if err != nil {
		t.Fatalf("Failed to get assignments: %v", err)
	}

	if len(assignments) == 0 {
		t.Fatal("No assignments found for issue")
	}

	featureAssignment := findAssignmentByContentType(assignments, database.ContentTypeFeature)
	if featureAssignment == nil {
		t.Fatalf("Feature assignment not found. Available assignments: %+v", assignments)
	}

	if featureAssignment.SubmissionID == nil {
		t.Errorf("Expected SubmissionID to be linked, got nil. Assignment: %+v", featureAssignment)
	}
}

// Helper function to find assignment by content type
func findAssignmentByContentType(assignments []database.PersonAssignment, contentType database.ContentType) *database.PersonAssignment {
	for i := range assignments {
		if assignments[i].ContentType == contentType {
			return &assignments[i]
		}
	}
	return nil
}

func TestReplyToBotSubmission(t *testing.T) {
	// Create test database
	tempFile := "/tmp/test_reply_to_bot.db"
	defer os.Remove(tempFile)

	testDB, err := database.NewSimple(tempFile)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testDB.Close()

	if err := testDB.Migrate(); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create newsletter issue and assignment for current week
	year, week := time.Now().ISOWeek()
	issue, err := testDB.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	userID := "U123456"
	assignment := database.PersonAssignment{
		IssueID:     issue.ID,
		PersonID:    userID,
		ContentType: database.ContentTypeFeature,
		AssignedAt:  time.Now(),
	}

	_, err = testDB.CreatePersonAssignment(assignment)
	if err != nil {
		t.Fatalf("Failed to create assignment: %v", err)
	}

	// Create bot with test dependencies
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIProcessor := &MockAIService{}

	bot := NewBotWithDatabase(SlackConfig{
		Token:         "test-token",
		SigningSecret: "test-secret",
	}, nil, []string{}, mockSubmissionManager, mockAIProcessor, testDB)

	// Test direct message event (DM channel starts with "D")
	event := SlackEvent{
		Type:    "message",
		User:    userID,
		Text:    "Here's my feature story about our new dashboard",
		Channel: "D123456", // DM channel
	}

	// Handle the event - expect error due to mock Slack API but core logic should work
	err = bot.HandleEventCallback(context.Background(), event)
	// We expect an error here because we're using a mock bot that can't send real Slack messages
	// But the submission processing should still work
	if err == nil {
		t.Log("Unexpected success - expected Slack API error")
	}

	// Verify submission was created
	if len(mockSubmissionManager.CreatedSubmissions) != 1 {
		t.Errorf("Expected 1 submission to be created, got %d", len(mockSubmissionManager.CreatedSubmissions))
	}

	submission := mockSubmissionManager.CreatedSubmissions[0]
	if submission.UserID != userID {
		t.Errorf("Expected userID %s, got %s", userID, submission.UserID)
	}

	expectedContent := "Here's my feature story about our new dashboard"
	if submission.Content != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, submission.Content)
	}
}

func TestReplyToBotNoAssignment(t *testing.T) {
	// Create test database
	tempFile := "/tmp/test_reply_no_assignment.db"
	defer os.Remove(tempFile)

	testDB, err := database.NewSimple(tempFile)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer testDB.Close()

	if err := testDB.Migrate(); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	// Create newsletter issue but NO assignment for user
	year, week := time.Now().ISOWeek()
	_, err = testDB.GetOrCreateWeeklyIssue(week, year)
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create bot with test dependencies
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIProcessor := &MockAIService{}

	bot := NewBotWithDatabase(SlackConfig{
		Token:         "test-token",
		SigningSecret: "test-secret",
	}, nil, []string{}, mockSubmissionManager, mockAIProcessor, testDB)

	userID := "U123456"

	// Test direct message event
	event := SlackEvent{
		Type:    "message",
		User:    userID,
		Text:    "Some content",
		Channel: "D123456", // DM channel
	}

	// Handle the event - may error due to Slack API but core logic should work
	err = bot.HandleEventCallback(context.Background(), event)
	// Error is expected due to mock Slack API, focus on core functionality

	// Verify NO submission was created (user has no assignment)
	if len(mockSubmissionManager.CreatedSubmissions) != 0 {
		t.Errorf("Expected 0 submissions to be created, got %d", len(mockSubmissionManager.CreatedSubmissions))
	}
}
