package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// TDD: Test for JSON-structured prompt building
func TestBuildJSONPrompt(t *testing.T) {
	// This test should FAIL initially as we haven't implemented JSON prompt building
	submission := "Our team launched a new analytics dashboard!"
	authorName := "Sarah Johnson"
	authorDepartment := "Engineering"
	journalistType := "feature"

	prompt, err := BuildJSONPrompt(submission, authorName, authorDepartment, journalistType)
	if err != nil {
		t.Fatalf("BuildJSONPrompt() failed: %v", err)
	}

	// Verify prompt contains author information
	if !strings.Contains(prompt, authorName) {
		t.Errorf("Prompt should contain author name %s", authorName)
	}

	if !strings.Contains(prompt, authorDepartment) {
		t.Errorf("Prompt should contain author department %s", authorDepartment)
	}

	// Verify prompt requests JSON format
	if !strings.Contains(prompt, "JSON") {
		t.Error("Prompt should request JSON format")
	}

	if !strings.Contains(prompt, "headline") {
		t.Error("Prompt should mention required JSON fields like 'headline'")
	}
}

// TDD: Test JSON response structure validation
func TestValidateJSONResponse(t *testing.T) {
	testCases := []struct {
		name           string
		journalistType string
		jsonResponse   string
		shouldFail     bool
	}{
		{
			name:           "Valid feature article",
			journalistType: "feature",
			jsonResponse: `{
				"headline": "New Dashboard Transforms Team Workflow",
				"lead": "When Sarah Johnson from Engineering announced...",
				"body": "The analytics dashboard has revolutionized how we track metrics...",
				"byline": "Erik Lindqvist, Feature Writer"
			}`,
			shouldFail: false,
		},
		{
			name:           "Valid interview",
			journalistType: "interview",
			jsonResponse: `{
				"headline": "Meet Sarah Johnson: The Mind Behind Our New Dashboard",
				"introduction": "We sat down with Sarah Johnson to discuss...",
				"questions": [
					{"q": "What inspired this project?", "a": "We needed better analytics..."},
					{"q": "How long did it take?", "a": "About three months..."}
				],
				"byline": "Anna Bergström, Interview Specialist"
			}`,
			shouldFail: false,
		},
		{
			name:           "Invalid JSON format",
			journalistType: "feature",
			jsonResponse:   `{headline: "Missing quotes"}`,
			shouldFail:     true,
		},
		{
			name:           "Missing required fields",
			journalistType: "feature",
			jsonResponse:   `{"headline": "Title only"}`,
			shouldFail:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This should FAIL initially as ValidateJSONResponse doesn't exist
			err := ValidateJSONResponse(tc.jsonResponse, tc.journalistType)

			if tc.shouldFail && err == nil {
				t.Errorf("Expected validation to fail for %s", tc.name)
			}

			if !tc.shouldFail && err != nil {
				t.Errorf("Expected validation to pass for %s, got error: %v", tc.name, err)
			}
		})
	}
}

// TDD: Test specific JSON structures for each journalist type
func TestJournalistJSONStructures(t *testing.T) {
	testCases := []struct {
		journalistType string
		expectedFields []string
	}{
		{
			journalistType: "feature",
			expectedFields: []string{"headline", "lead", "body", "byline"},
		},
		{
			journalistType: "interview",
			expectedFields: []string{"headline", "introduction", "questions", "byline"},
		},
		{
			journalistType: "general",
			expectedFields: []string{"headline", "content", "byline"},
		},
		{
			journalistType: "body_mind",
			expectedFields: []string{"headline", "response", "signoff", "byline"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.journalistType, func(t *testing.T) {
			// This should FAIL initially as GetRequiredJSONFields doesn't exist
			fields := GetRequiredJSONFields(tc.journalistType)

			for _, expected := range tc.expectedFields {
				found := false
				for _, actual := range fields {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected field %s not found in %s journalist structure", expected, tc.journalistType)
				}
			}
		})
	}
}

// Helper test to ensure we can parse valid JSON
func TestJSONParsing(t *testing.T) {
	validJSON := `{
		"headline": "Test Headline",
		"lead": "Test lead paragraph",
		"body": "Test body content", 
		"byline": "Erik Lindqvist, Feature Writer"
	}`

	var result map[string]interface{}
	err := json.Unmarshal([]byte(validJSON), &result)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	expectedFields := []string{"headline", "lead", "body", "byline"}
	for _, field := range expectedFields {
		if _, exists := result[field]; !exists {
			t.Errorf("Expected field %s not found in parsed JSON", field)
		}
	}
}
