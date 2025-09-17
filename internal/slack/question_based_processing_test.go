package slack

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TDD: Test journalist type selection based on question category
func TestSlackBot_JournalistTypeFromQuestionCategory(t *testing.T) {
	// This test should FAIL initially until we implement question-based journalist selection

	testCases := []struct {
		name               string
		questionID         int
		expectedJournalist string
	}{
		{
			name:               "Interview question",
			questionID:         1,
			expectedJournalist: "interview",
		},
		{
			name:               "Feature question",
			questionID:         2,
			expectedJournalist: "feature",
		},
		{
			name:               "General question",
			questionID:         3,
			expectedJournalist: "general",
		},
		{
			name:               "Body/Mind question",
			questionID:         4,
			expectedJournalist: "body_mind",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSubmissionManager := &MockSubmissionManager{}
			mockAIService := &MockAIService{}
			mockDB := newMockDatabaseForQuestions()
			mockQuestionManager := &MockQuestionManager{
				questions: map[int]*database.Question{
					1: {ID: 1, Text: "Tell us about your experience", Category: "interview"},
					2: {ID: 2, Text: "What amazing thing happened this week?", Category: "feature"},
					3: {ID: 3, Text: "Any office updates?", Category: "general"},
					4: {ID: 4, Text: "How are you feeling?", Category: "body_mind"},
				},
			}

			// Mock submission manager should return submission with SPECIFIC question ID
			mockSubmissionManager.NextSubmission = &database.Submission{
				ID:         1,
				UserID:     "U987654321",
				QuestionID: &tc.questionID,
				Content:    "Test response to the question",
			}

			bot := NewBotWithDatabase(
				SlackConfig{Token: "test-token"},
				mockQuestionManager,
				[]string{"U1234567"},
				mockSubmissionManager,
				mockAIService,
				mockDB,
			)

			command := SlashCommand{
				Command: "/pp",
				Text:    "submit Test response to the question",
				UserID:  "U987654321",
			}

			_, err := bot.HandleSlashCommand(context.Background(), command)
			if err != nil {
				t.Fatalf("HandleSlashCommand failed: %v", err)
			}

			// Give async processing time to complete and retry if needed
			var processCallCount int
			for i := 0; i < 10; i++ { // Try up to 10 times with 200ms intervals
				time.Sleep(200 * time.Millisecond)
				processCallCount = len(mockAIService.ProcessAndSaveCalls)
				if processCallCount > 0 {
					break
				}
			}

			// Verify AI processing was called with correct journalist type
			if processCallCount != 1 {
				t.Fatalf("Expected 1 AI processing call, got %d", processCallCount)
			}

			processCall := mockAIService.ProcessAndSaveCalls[0]
			if processCall.JournalistType != tc.expectedJournalist {
				t.Errorf("Expected journalist type %s, got %s", tc.expectedJournalist, processCall.JournalistType)
			}
		})
	}
}

// TDD: Test general news submissions (no question) default to general journalist
func TestSlackBot_GeneralNewsDefaultJournalist(t *testing.T) {
	// This test should FAIL initially until we implement proper handling

	mockSubmissionManager := &MockSubmissionManager{}
	mockAIService := &MockAIService{}
	mockDB := newMockDatabaseForQuestions()

	// Mock submission manager should return submission with NO question ID
	mockSubmissionManager.NextSubmission = &database.Submission{
		ID:         1,
		UserID:     "U987654321",
		QuestionID: nil, // No question - general news submission
		Content:    "General news: Our office is moving next month",
	}

	bot := NewBotWithDatabase(
		SlackConfig{Token: "test-token"},
		nil, // No question selector needed for general news
		[]string{"U1234567"},
		mockSubmissionManager,
		mockAIService,
		mockDB,
	)

	command := SlashCommand{
		Command: "/pp",
		Text:    "submit General news: Our office is moving next month",
		UserID:  "U987654321",
	}

	_, err := bot.HandleSlashCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("HandleSlashCommand failed: %v", err)
	}

	// Give async processing time to complete and retry if needed
	var processCallCount int
	for i := 0; i < 10; i++ { // Try up to 10 times with 200ms intervals
		time.Sleep(200 * time.Millisecond)
		processCallCount = len(mockAIService.ProcessAndSaveCalls)
		if processCallCount > 0 {
			break
		}
	}

	// Verify AI processing was called with general journalist for news submissions
	if processCallCount != 1 {
		t.Fatalf("Expected 1 AI processing call, got %d", processCallCount)
	}

	processCall := mockAIService.ProcessAndSaveCalls[0]
	if processCall.JournalistType != "general" {
		t.Errorf("Expected journalist type 'general' for news submission, got %s", processCall.JournalistType)
	}
}

// Mock question manager for testing
type MockQuestionManager struct {
	questions map[int]*database.Question
}

// Copy MockDatabase from auto_processing_test.go for testing
type mockDatabaseForQuestions struct {
	*database.DB
}

func newMockDatabaseForQuestions() DatabaseInterface {
	// Create a temporary DB for embedding (won't be used, just for type compatibility)
	tempDB, _ := database.NewSimple(":memory:")
	return &mockDatabaseForQuestions{DB: tempDB}
}

func (m *mockDatabaseForQuestions) GetOrCreateWeeklyIssue(weekNumber, year int) (*database.WeeklyNewsletterIssue, error) {
	// Simple mock implementation for testing
	return &database.WeeklyNewsletterIssue{
		ID:         1,
		WeekNumber: weekNumber,
		Year:       year,
		Status:     database.IssueStatusDraft,
	}, nil
}

func (m *mockDatabaseForQuestions) CreateProcessedArticle(article database.ProcessedArticle) (int, error) {
	return 1, nil
}

func (m *mockDatabaseForQuestions) GetUnderlyingDB() *database.DB {
	return m.DB
}

func (m *MockQuestionManager) GetQuestionByID(ctx context.Context, questionID int) (*database.Question, error) {
	if question, exists := m.questions[questionID]; exists {
		return question, nil
	}
	return nil, fmt.Errorf("question not found: %d", questionID)
}

func (m *MockQuestionManager) SelectNextQuestion(ctx context.Context, category string) (*database.Question, error) {
	return nil, nil // Not needed for these tests
}

func (m *MockQuestionManager) MarkQuestionUsed(ctx context.Context, questionID int) error {
	return nil // Not needed for these tests
}

func (m *MockQuestionManager) GetQuestionsByCategory(ctx context.Context, category string) ([]database.Question, error) {
	return nil, nil // Not needed for these tests
}

func (m *MockQuestionManager) AddQuestion(ctx context.Context, text, category string) (*database.Question, error) {
	return nil, nil // Not needed for these tests
}

// MockSubmissionManager is defined in auto_processing_test.go
