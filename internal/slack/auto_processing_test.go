package slack

import (
	"context"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TDD: Test automatic AI processing when submission is created
func TestSlackBot_AutoProcessSubmission(t *testing.T) {
	// This test should FAIL initially as auto-processing doesn't exist

	// Create mock services
	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}

	// Create enhanced bot with AI processing capability
	bot := NewBotWithAIProcessing(
		SlackConfig{Token: "test-token"},
		nil,                  // question selector
		[]string{"U1234567"}, // admin users
		mockSubmissionManager,
		mockAIService,
	)

	// Simulate news submission command
	command := SlashCommand{
		Command: "/pp",
		Text:    "submit Our team launched a new analytics dashboard!",
		UserID:  "U987654321",
	}

	response, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Verify submission was stored
	if len(mockSubmissionManager.CreatedSubmissions) != 1 {
		t.Errorf("Expected 1 created submission, got %d", len(mockSubmissionManager.CreatedSubmissions))
	}

	// Verify AI processing was triggered automatically
	if len(mockAIService.ProcessedWithUserInfo) != 1 {
		t.Errorf("Expected 1 processed submission, got %d", len(mockAIService.ProcessedWithUserInfo))
	}

	// Verify response indicates both storage and processing
	if response == nil {
		t.Fatal("Expected response, got nil")
	}

	// Response should indicate successful processing
	responseText := response.Text
	if responseText == "" {
		t.Error("Expected non-empty response text")
	}
}

// TDD: Test automatic processing with user information enrichment
func TestSlackBot_AutoProcessWithUserInfo(t *testing.T) {
	// This test should FAIL initially as user info enrichment doesn't exist in auto-processing

	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}

	bot := NewBotWithAIProcessing(
		SlackConfig{Token: "test-token"},
		nil,
		[]string{"U1234567"},
		mockSubmissionManager,
		mockAIService,
	)

	command := SlashCommand{
		Command: "/pp",
		Text:    "submit Our team launched a new analytics dashboard!",
		UserID:  "U987654321",
	}

	_, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Verify AI processing was called with user information
	if len(mockAIService.ProcessedWithUserInfo) != 1 {
		t.Errorf("Expected 1 user-enriched processing call, got %d", len(mockAIService.ProcessedWithUserInfo))
	}

	// Check that user info was passed correctly
	processCall := mockAIService.ProcessedWithUserInfo[0]
	if processCall.AuthorName == "" {
		t.Error("Expected non-empty author name in processing call")
	}

	if processCall.AuthorDepartment == "" {
		t.Error("Expected non-empty author department in processing call")
	}
}

// TDD: Test automatic journalist type selection for news submissions (no question)
// All news submissions without questions should default to "general"
func TestSlackBot_AutoJournalistSelection(t *testing.T) {
	testCases := []struct {
		name               string
		content            string
		expectedJournalist string
	}{
		{
			name:               "Feature story - should default to general",
			content:            "Our team launched an amazing new feature that transforms how users interact with our platform",
			expectedJournalist: "general", // Changed expectation - no question means general
		},
		{
			name:               "Interview content - should default to general",
			content:            "I'm Sarah Johnson, new software developer. I studied at UBC and worked at startups before joining here",
			expectedJournalist: "general", // Changed expectation - no question means general
		},
		{
			name:               "General announcement",
			content:            "The office parking lot will be closed next week for maintenance",
			expectedJournalist: "general", // Stays the same
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSubmissionManager := &MockSubmissionManager{}
			mockAIService := &MockAIService{}

			// Mock submission manager should return submission with NO question ID (news submission)
			mockSubmissionManager.NextSubmission = &database.Submission{
				ID:         1,
				UserID:     "U987654321",
				QuestionID: nil, // No question - general news submission
				Content:    tc.content,
			}

			bot := NewBotWithAIProcessing(
				SlackConfig{Token: "test-token"},
				nil, // No question selector needed for news submissions
				[]string{"U1234567"},
				mockSubmissionManager,
				mockAIService,
			)

			command := SlashCommand{
				Command: "/pp",
				Text:    "submit " + tc.content,
				UserID:  "U987654321",
			}

			_, err := bot.HandleSlashCommand(context.Background(), command)
			if err != nil {
				t.Fatalf("HandleSlashCommand failed: %v", err)
			}

			// Verify correct journalist type was selected
			if len(mockAIService.ProcessedWithUserInfo) != 1 {
				t.Fatalf("Expected 1 processing call, got %d", len(mockAIService.ProcessedWithUserInfo))
			}

			processCall := mockAIService.ProcessedWithUserInfo[0]
			if processCall.JournalistType != tc.expectedJournalist {
				t.Errorf("Expected journalist type %s, got %s", tc.expectedJournalist, processCall.JournalistType)
			}
		})
	}
}

// Mock structures for testing

type MockSubmissionManager struct {
	CreatedSubmissions []database.Submission
	NextSubmission     *database.Submission // Pre-configured submission for testing
	Error              error
}

func (m *MockSubmissionManager) CreateNewsSubmission(ctx context.Context, userID, content string) (*database.Submission, error) {
	if m.Error != nil {
		return nil, m.Error
	}

	var submission database.Submission
	if m.NextSubmission != nil {
		// Use the pre-configured submission for testing
		submission = *m.NextSubmission
		submission.UserID = userID
		submission.Content = content
	} else {
		// Default behavior
		submission = database.Submission{
			ID:      len(m.CreatedSubmissions) + 1,
			UserID:  userID,
			Content: content,
		}
	}

	m.CreatedSubmissions = append(m.CreatedSubmissions, submission)
	return &submission, nil
}

func (m *MockSubmissionManager) GetSubmissionsByUser(ctx context.Context, userID string) ([]database.Submission, error) {
	return nil, nil // Not needed for these tests
}

func (m *MockSubmissionManager) GetAllSubmissions(ctx context.Context) ([]database.Submission, error) {
	return nil, nil // Not needed for these tests
}

type MockAIService struct {
	ProcessedSubmissions  []database.Submission
	ProcessedWithUserInfo []ProcessWithUserInfoCall
	Error                 error
}

type ProcessWithUserInfoCall struct {
	Submission       database.Submission
	AuthorName       string
	AuthorDepartment string
	JournalistType   string
}

func (m *MockAIService) ProcessSubmissionWithUserInfo(ctx context.Context, submission database.Submission, authorName, authorDepartment, journalistType string) (*database.ProcessedArticle, error) {
	if m.Error != nil {
		return nil, m.Error
	}

	call := ProcessWithUserInfoCall{
		Submission:       submission,
		AuthorName:       authorName,
		AuthorDepartment: authorDepartment,
		JournalistType:   journalistType,
	}
	m.ProcessedWithUserInfo = append(m.ProcessedWithUserInfo, call)

	// Return mock processed article
	return &database.ProcessedArticle{
		ID:               1,
		SubmissionID:     submission.ID,
		JournalistType:   journalistType,
		ProcessedContent: `{"headline": "Test", "body": "Test content", "byline": "Test Writer"}`,
		ProcessingStatus: database.ProcessingStatusSuccess,
		WordCount:        10,
	}, nil
}

// Implement other AIService methods as no-ops for testing
func (m *MockAIService) ProcessSubmission(ctx context.Context, submission database.Submission, journalistType string) (*database.ProcessedArticle, error) {
	return nil, nil
}

func (m *MockAIService) GetAvailableJournalists() []string {
	return []string{"feature", "interview", "general", "body_mind"}
}

func (m *MockAIService) ValidateJournalistType(journalistType string) bool {
	return true
}

func (m *MockAIService) GetJournalistProfile(journalistType string) (*database.ProcessedArticle, error) {
	return nil, nil
}

// Ensure MockAIService implements AIProcessor interface
var _ AIProcessor = (*MockAIService)(nil)
