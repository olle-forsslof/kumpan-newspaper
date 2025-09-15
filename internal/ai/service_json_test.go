package ai

import (
	"context"
	"testing"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// TDD: Test for JSON-based processing with user information
func TestAIService_ProcessSubmissionWithUserInfo(t *testing.T) {
	// This test should FAIL initially as we haven't implemented JSON processing
	service := NewAnthropicService("test-api-key")

	// Create a test submission with user information
	submission := database.Submission{
		ID:      1,
		UserID:  "U123456789",
		Content: "Our team launched a new analytics dashboard!",
	}

	// Mock user information
	authorName := "Sarah Johnson"
	authorDepartment := "Engineering"
	journalistType := "feature"

	// This should fail initially as ProcessSubmissionWithUserInfo doesn't exist
	article, err := service.ProcessSubmissionWithUserInfo(
		context.Background(),
		submission,
		authorName,
		authorDepartment,
		journalistType,
	)

	if err != nil {
		// For now, expect error due to test API key, but the method should exist
		t.Logf("Expected error due to test API key: %v", err)
		return
	}

	// Verify the processed article contains JSON structure
	if article == nil {
		t.Fatal("Expected processed article, got nil")
	}

	if article.JournalistType != journalistType {
		t.Errorf("Expected journalist type %s, got %s", journalistType, article.JournalistType)
	}

	if article.ProcessedContent == "" {
		t.Error("Expected non-empty processed content")
	}

	// The content should be stored as JSON string
	err = ValidateJSONResponse(article.ProcessedContent, journalistType)
	if err != nil {
		t.Errorf("Processed content should be valid JSON: %v", err)
	}
}

// TDD: Test enhanced AI service interface with JSON capabilities
func TestEnhancedAIService_Interface(t *testing.T) {
	service := NewAnthropicService("test-api-key")

	// Test that enhanced service implements the interface
	var enhancedService EnhancedAIService = service
	if enhancedService == nil {
		t.Fatal("Service should implement EnhancedAIService interface")
	}

	// Test new methods are available
	testSubmission := database.Submission{
		ID:      1,
		UserID:  "U123456789",
		Content: "Test content",
	}

	// This should not panic - methods should exist
	_, err := enhancedService.ProcessSubmissionWithUserInfo(
		context.Background(),
		testSubmission,
		"Test User",
		"Test Department",
		"general",
	)

	// Error is expected due to test API key, but method should exist
	if err == nil {
		t.Log("Unexpected success with test API key")
	}
}

// TDD: Test JSON parsing from AI responses
func TestParseJSONResponse(t *testing.T) {
	testCases := []struct {
		name           string
		jsonResponse   string
		journalistType string
		expectError    bool
	}{
		{
			name: "Valid feature JSON",
			jsonResponse: `{
				"headline": "New Dashboard Transforms Team Workflow",
				"lead": "Sarah Johnson from Engineering announced...",
				"body": "The analytics dashboard has revolutionized...",
				"byline": "Erik Lindqvist, Feature Writer"
			}`,
			journalistType: "feature",
			expectError:    false,
		},
		{
			name:           "Invalid JSON",
			jsonResponse:   `{invalid json}`,
			journalistType: "feature",
			expectError:    true,
		},
		{
			name: "Missing required fields",
			jsonResponse: `{
				"headline": "Test"
			}`,
			journalistType: "feature",
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This should FAIL initially as ParseJSONResponse doesn't exist
			result, err := ParseJSONResponse(tc.jsonResponse, tc.journalistType)

			if tc.expectError && err == nil {
				t.Errorf("Expected error for %s", tc.name)
			}

			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tc.name, err)
			}

			if !tc.expectError && result == nil {
				t.Errorf("Expected result for %s, got nil", tc.name)
			}
		})
	}
}
