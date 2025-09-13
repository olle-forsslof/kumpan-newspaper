package ai

import (
	"context"
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
