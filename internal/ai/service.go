package ai

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// AIService defines the interface for AI content processing
type AIService interface {
	// ProcessSubmission transforms a submission into a processed article using AI
	ProcessSubmission(ctx context.Context, submission database.Submission, journalistType string) (*database.ProcessedArticle, error)

	// GetAvailableJournalists returns a list of available journalist types
	GetAvailableJournalists() []string

	// ValidateJournalistType checks if a journalist type is valid
	ValidateJournalistType(journalistType string) bool

	// GetJournalistProfile returns the profile for a given journalist type
	GetJournalistProfile(journalistType string) (*JournalistProfile, error)
}

// EnhancedAIService extends AIService with user information and JSON processing
type EnhancedAIService interface {
	AIService

	// ProcessSubmissionWithUserInfo transforms a submission with user context into structured JSON article
	ProcessSubmissionWithUserInfo(ctx context.Context, submission database.Submission, authorName, authorDepartment, journalistType string) (*database.ProcessedArticle, error)
}

// ProcessingResult contains the AI processing result details
type ProcessingResult struct {
	ProcessedContent string
	WordCount        int
	ProcessingTime   time.Duration
	TokensUsed       int
	Model            string
}

// ProcessingError represents AI processing failures with context
type ProcessingError struct {
	Type      string // "api_error", "rate_limit", "content_filter", "timeout", "invalid_response"
	Message   string
	Retryable bool
	Cause     error
}

func (e ProcessingError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// NewProcessingError creates a new processing error
func NewProcessingError(errorType, message string, retryable bool, cause error) *ProcessingError {
	return &ProcessingError{
		Type:      errorType,
		Message:   message,
		Retryable: retryable,
		Cause:     cause,
	}
}

// CategoryToJournalistMapping maps submission categories to journalist types
var CategoryToJournalistMapping = map[string]string{
	"feature":   "feature",
	"interview": "interview",
	"sports":    "sports",
	"tech":      "general",
	"general":   "general",
	"body_mind": "body_mind",
	"advice":    "body_mind", // Alternative category name
}

// GetJournalistTypeForCategory returns the journalist type for a given category
func GetJournalistTypeForCategory(category string) string {
	if journalistType, exists := CategoryToJournalistMapping[category]; exists {
		return journalistType
	}
	return "general" // Default fallback
}

// ParsedJSONResponse represents a parsed JSON article response
type ParsedJSONResponse struct {
	Content        map[string]interface{} `json:"content"`
	JournalistType string                 `json:"journalist_type"`
	WordCount      int                    `json:"word_count"`
}

// ParseJSONResponse parses and validates JSON response from AI
func ParseJSONResponse(jsonResponse, journalistType string) (*ParsedJSONResponse, error) {
	// Validate JSON format and required fields
	if err := ValidateJSONResponse(jsonResponse, journalistType); err != nil {
		return nil, err
	}

	// Parse JSON content
	var content map[string]interface{}
	if err := json.Unmarshal([]byte(jsonResponse), &content); err != nil {
		return nil, NewProcessingError("invalid_response", "Failed to parse JSON response", false, err)
	}

	// Calculate approximate word count from all text fields
	wordCount := calculateWordCount(content)

	return &ParsedJSONResponse{
		Content:        content,
		JournalistType: journalistType,
		WordCount:      wordCount,
	}, nil
}

// calculateWordCount estimates word count from JSON content
func calculateWordCount(content map[string]interface{}) int {
	totalWords := 0

	for _, value := range content {
		switch v := value.(type) {
		case string:
			totalWords += len(strings.Fields(v))
		case []interface{}:
			// Handle interview questions array
			for _, item := range v {
				if qa, ok := item.(map[string]interface{}); ok {
					if q, ok := qa["q"].(string); ok {
						totalWords += len(strings.Fields(q))
					}
					if a, ok := qa["a"].(string); ok {
						totalWords += len(strings.Fields(a))
					}
				}
			}
		}
	}

	return totalWords
}
