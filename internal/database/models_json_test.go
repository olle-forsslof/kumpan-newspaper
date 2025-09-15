package database

import (
	"testing"
	"time"
)

// TDD: Test ProcessedArticle with JSON content parsing
func TestProcessedArticle_ParseJSONContent(t *testing.T) {
	// This test should FAIL initially as ParseJSONContent doesn't exist
	now := time.Now()
	article := ProcessedArticle{
		ID:             1,
		SubmissionID:   1,
		JournalistType: "feature",
		ProcessedContent: `{
			"headline": "New Dashboard Transforms Team Workflow",
			"lead": "Sarah Johnson from Engineering announced...",
			"body": "The analytics dashboard has revolutionized how we track metrics...",
			"byline": "Erik Lindqvist, Feature Writer"
		}`,
		ProcessingPrompt: "Test prompt",
		TemplateFormat:   "hero",
		ProcessingStatus: ProcessingStatusSuccess,
		WordCount:        45,
		ProcessedAt:      &now,
		CreatedAt:        now,
	}

	// Parse JSON content
	content, err := article.ParseJSONContent()
	if err != nil {
		t.Fatalf("ParseJSONContent() failed: %v", err)
	}

	// Verify parsed content
	if content["headline"] != "New Dashboard Transforms Team Workflow" {
		t.Errorf("Expected headline, got %v", content["headline"])
	}

	if content["lead"] != "Sarah Johnson from Engineering announced..." {
		t.Errorf("Expected lead, got %v", content["lead"])
	}

	if content["body"] != "The analytics dashboard has revolutionized how we track metrics..." {
		t.Errorf("Expected body, got %v", content["body"])
	}

	if content["byline"] != "Erik Lindqvist, Feature Writer" {
		t.Errorf("Expected byline, got %v", content["byline"])
	}
}

// TDD: Test ProcessedArticle with invalid JSON content
func TestProcessedArticle_ParseInvalidJSON(t *testing.T) {
	article := ProcessedArticle{
		ProcessedContent: `{invalid json}`,
		JournalistType:   "feature",
	}

	_, err := article.ParseJSONContent()
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// TDD: Test ProcessedArticle helper methods for structured content
func TestProcessedArticle_GetHeadline(t *testing.T) {
	// This test should FAIL initially as GetHeadline doesn't exist
	article := ProcessedArticle{
		ProcessedContent: `{
			"headline": "Amazing New Feature Launch",
			"body": "Our team has been working on..."
		}`,
		JournalistType: "general",
	}

	headline, err := article.GetHeadline()
	if err != nil {
		t.Fatalf("GetHeadline() failed: %v", err)
	}

	expected := "Amazing New Feature Launch"
	if headline != expected {
		t.Errorf("Expected headline %s, got %s", expected, headline)
	}
}

// TDD: Test ProcessedArticle helper methods for getting byline
func TestProcessedArticle_GetByline(t *testing.T) {
	// This test should FAIL initially as GetByline doesn't exist
	article := ProcessedArticle{
		ProcessedContent: `{
			"headline": "Test Article",
			"byline": "Lars Petersson, Staff Reporter"
		}`,
		JournalistType: "general",
	}

	byline, err := article.GetByline()
	if err != nil {
		t.Fatalf("GetByline() failed: %v", err)
	}

	expected := "Lars Petersson, Staff Reporter"
	if byline != expected {
		t.Errorf("Expected byline %s, got %s", expected, byline)
	}
}

// TDD: Test interview-specific JSON structure parsing
func TestProcessedArticle_ParseInterviewQuestions(t *testing.T) {
	// This test should FAIL initially as ParseInterviewQuestions doesn't exist
	article := ProcessedArticle{
		ProcessedContent: `{
			"headline": "Meet Our New Developer",
			"introduction": "We sat down with Sarah...",
			"questions": [
				{"q": "What brought you here?", "a": "The exciting projects..."},
				{"q": "What's your background?", "a": "I studied computer science..."}
			],
			"byline": "Anna Bergström, Interview Specialist"
		}`,
		JournalistType: "interview",
	}

	questions, err := article.ParseInterviewQuestions()
	if err != nil {
		t.Fatalf("ParseInterviewQuestions() failed: %v", err)
	}

	if len(questions) != 2 {
		t.Errorf("Expected 2 questions, got %d", len(questions))
	}

	if questions[0].Question != "What brought you here?" {
		t.Errorf("Expected first question, got %s", questions[0].Question)
	}

	if questions[0].Answer != "The exciting projects..." {
		t.Errorf("Expected first answer, got %s", questions[0].Answer)
	}
}

// TDD: Test validation with JSON content
func TestProcessedArticle_ValidateJSONContent(t *testing.T) {
	// This test should FAIL initially as ValidateJSONContent doesn't exist
	testCases := []struct {
		name       string
		article    ProcessedArticle
		shouldFail bool
	}{
		{
			name: "Valid feature article",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessedContent: `{"headline": "Test", "lead": "Test lead", "body": "Test body", "byline": "Erik Lindqvist, Feature Writer"}`,
				ProcessingStatus: ProcessingStatusSuccess,
				TemplateFormat:   "hero",
				WordCount:        10,
			},
			shouldFail: false,
		},
		{
			name: "Invalid JSON format",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessedContent: `{invalid json}`,
				ProcessingStatus: ProcessingStatusSuccess,
				TemplateFormat:   "hero",
				WordCount:        10,
			},
			shouldFail: true,
		},
		{
			name: "Missing required JSON fields",
			article: ProcessedArticle{
				SubmissionID:     1,
				JournalistType:   "feature",
				ProcessedContent: `{"headline": "Test only"}`,
				ProcessingStatus: ProcessingStatusSuccess,
				TemplateFormat:   "hero",
				WordCount:        10,
			},
			shouldFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.article.ValidateJSONContent()

			if tc.shouldFail && err == nil {
				t.Errorf("Expected validation to fail for %s", tc.name)
			}

			if !tc.shouldFail && err != nil {
				t.Errorf("Expected validation to pass for %s, got error: %v", tc.name, err)
			}
		})
	}
}
