package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

func TestGetJournalistProfile(t *testing.T) {
	tests := []struct {
		name           string
		journalistType string
		wantError      bool
		wantName       string
	}{
		{
			name:           "valid feature journalist",
			journalistType: "feature",
			wantError:      false,
			wantName:       "Feature Writer",
		},
		{
			name:           "valid interview journalist",
			journalistType: "interview",
			wantError:      false,
			wantName:       "Interview Specialist",
		},
		{
			name:           "valid sports journalist",
			journalistType: "sports",
			wantError:      false,
			wantName:       "Sports Reporter",
		},
		{
			name:           "valid general journalist",
			journalistType: "general",
			wantError:      false,
			wantName:       "Staff Reporter",
		},
		{
			name:           "valid body_mind journalist",
			journalistType: "body_mind",
			wantError:      false,
			wantName:       "Body and Mind Columnist",
		},
		{
			name:           "invalid journalist type",
			journalistType: "invalid",
			wantError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := GetJournalistProfile(tt.journalistType)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if profile.Name != tt.wantName {
				t.Errorf("Expected name %s, got %s", tt.wantName, profile.Name)
			}

			if profile.Type != tt.journalistType {
				t.Errorf("Expected type %s, got %s", tt.journalistType, profile.Type)
			}

			// Verify required fields are not empty
			if profile.SystemPrompt == "" {
				t.Error("SystemPrompt should not be empty")
			}

			if profile.StyleInstructions == "" {
				t.Error("StyleInstructions should not be empty")
			}

			if profile.MaxWords <= 0 {
				t.Error("MaxWords should be greater than 0")
			}

			if profile.TemplateFormat == "" {
				t.Error("TemplateFormat should not be empty")
			}
		})
	}
}

func TestGetAvailableJournalistTypes(t *testing.T) {
	types := GetAvailableJournalistTypes()

	expectedTypes := map[string]bool{
		"feature":   true,
		"interview": true,
		"sports":    true,
		"general":   true,
		"body_mind": true,
	}

	if len(types) != len(expectedTypes) {
		t.Errorf("Expected %d types, got %d", len(expectedTypes), len(types))
	}

	for _, journalistType := range types {
		if !expectedTypes[journalistType] {
			t.Errorf("Unexpected journalist type: %s", journalistType)
		}
	}
}

func TestValidateJournalistType(t *testing.T) {
	validTypes := []string{"feature", "interview", "sports", "general", "body_mind"}
	invalidTypes := []string{"invalid", "unknown", ""}

	for _, validType := range validTypes {
		if !ValidateJournalistType(validType) {
			t.Errorf("Expected %s to be valid", validType)
		}
	}

	for _, invalidType := range invalidTypes {
		if ValidateJournalistType(invalidType) {
			t.Errorf("Expected %s to be invalid", invalidType)
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	submission := "Great team lunch today! Everyone loved the new menu."
	journalistType := "feature"

	prompt, err := BuildPrompt(submission, journalistType)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}

	// Check that prompt contains required elements
	if !strings.Contains(prompt, submission) {
		t.Error("Prompt should contain the original submission")
	}

	profile, _ := GetJournalistProfile(journalistType)
	if !strings.Contains(prompt, profile.SystemPrompt) {
		t.Error("Prompt should contain the system prompt")
	}

	if !strings.Contains(prompt, profile.StyleInstructions) {
		t.Error("Prompt should contain style instructions")
	}

	// Test with invalid journalist type
	_, err = BuildPrompt(submission, "invalid")
	if err == nil {
		t.Error("Expected error for invalid journalist type")
	}
}

func TestGetJournalistTypeForCategory(t *testing.T) {
	tests := []struct {
		category string
		expected string
	}{
		{"feature", "feature"},
		{"interview", "interview"},
		{"sports", "sports"},
		{"tech", "general"},
		{"general", "general"},
		{"body_mind", "body_mind"},
		{"advice", "body_mind"}, // Alternative category name
		{"unknown", "general"},  // Should default to general
		{"", "general"},         // Should default to general
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			result := GetJournalistTypeForCategory(tt.category)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCountWords(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{"  hello   world  ", 2},
		{"The quick brown fox jumps over the lazy dog", 9},
		{"Hello,\nworld!\t\tHow are you?", 5},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			result := countWords(tt.text)
			if result != tt.expected {
				t.Errorf("countWords(%q) = %d, expected %d", tt.text, result, tt.expected)
			}
		})
	}
}

func TestAnthropicServiceInterface(t *testing.T) {
	// Test that AnthropicService implements AIService interface
	service := NewAnthropicService("dummy-key")

	// This will fail at compile time if AnthropicService doesn't implement AIService
	var _ AIService = service

	// Test basic methods without making API calls
	journalists := service.GetAvailableJournalists()
	if len(journalists) == 0 {
		t.Error("Should return available journalists")
	}

	if !service.ValidateJournalistType("feature") {
		t.Error("Should validate feature journalist type")
	}

	if service.ValidateJournalistType("invalid") {
		t.Error("Should not validate invalid journalist type")
	}

	profile, err := service.GetJournalistProfile("feature")
	if err != nil {
		t.Fatalf("Should return feature journalist profile: %v", err)
	}

	if profile.Type != "feature" {
		t.Error("Should return correct journalist profile")
	}
}

func TestProcessingError(t *testing.T) {
	// Test ProcessingError creation and methods
	err := NewProcessingError("rate_limit", "Rate limit exceeded", true, nil)

	if err.Type != "rate_limit" {
		t.Errorf("Expected type 'rate_limit', got %s", err.Type)
	}

	if err.Message != "Rate limit exceeded" {
		t.Errorf("Expected message 'Rate limit exceeded', got %s", err.Message)
	}

	if !err.Retryable {
		t.Error("Expected error to be retryable")
	}

	expectedError := "Rate limit exceeded"
	if err.Error() != expectedError {
		t.Errorf("Expected error string %s, got %s", expectedError, err.Error())
	}

	// Test with cause
	cause := errors.New("underlying error")
	err = NewProcessingError("api_error", "API failed", false, cause)

	expectedWithCause := "API failed: underlying error"
	if err.Error() != expectedWithCause {
		t.Errorf("Expected error string %s, got %s", expectedWithCause, err.Error())
	}
}

// Mock implementation for testing without real API calls
type MockAIService struct {
	shouldFail bool
	response   string
}

func (m *MockAIService) ProcessSubmission(ctx context.Context, submission database.Submission, journalistType string) (*database.ProcessedArticle, error) {
	if m.shouldFail {
		return nil, NewProcessingError("mock_error", "Mock processing failed", true, nil)
	}

	if !ValidateJournalistType(journalistType) {
		return nil, NewProcessingError("invalid_journalist_type", "Invalid journalist type", false, nil)
	}

	profile, _ := GetJournalistProfile(journalistType)
	now := time.Now()

	return &database.ProcessedArticle{
		SubmissionID:     submission.ID,
		JournalistType:   journalistType,
		ProcessedContent: m.response,
		ProcessingPrompt: "Mock prompt for testing",
		TemplateFormat:   profile.TemplateFormat,
		ProcessingStatus: database.ProcessingStatusSuccess,
		WordCount:        len(strings.Fields(m.response)),
		ProcessedAt:      &now,
	}, nil
}

func (m *MockAIService) GetAvailableJournalists() []string {
	return GetAvailableJournalistTypes()
}

func (m *MockAIService) ValidateJournalistType(journalistType string) bool {
	return ValidateJournalistType(journalistType)
}

func (m *MockAIService) GetJournalistProfile(journalistType string) (*JournalistProfile, error) {
	return GetJournalistProfile(journalistType)
}

func TestMockAIService(t *testing.T) {
	// Test successful processing
	mockService := &MockAIService{
		shouldFail: false,
		response:   "This is a mock processed article that simulates AI output.",
	}

	submission := database.Submission{
		ID:      1,
		UserID:  "U123456",
		Content: "Test submission content",
	}

	article, err := mockService.ProcessSubmission(context.Background(), submission, "feature")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if article.SubmissionID != submission.ID {
		t.Errorf("Expected SubmissionID %d, got %d", submission.ID, article.SubmissionID)
	}

	if article.JournalistType != "feature" {
		t.Errorf("Expected JournalistType 'feature', got %s", article.JournalistType)
	}

	if article.ProcessingStatus != database.ProcessingStatusSuccess {
		t.Errorf("Expected ProcessingStatus 'success', got %s", article.ProcessingStatus)
	}

	expectedWords := len(strings.Fields(mockService.response))
	if article.WordCount != expectedWords {
		t.Errorf("Expected WordCount %d, got %d", expectedWords, article.WordCount)
	}

	// Test failure
	mockService.shouldFail = true
	_, err = mockService.ProcessSubmission(context.Background(), submission, "feature")
	if err == nil {
		t.Error("Expected error when shouldFail is true")
	}

	processingErr, ok := err.(*ProcessingError)
	if !ok {
		t.Error("Expected ProcessingError type")
	}

	if !processingErr.Retryable {
		t.Error("Expected mock error to be retryable")
	}
}
