package database

import (
	"fmt"
	"time"
)

// Submission represents a newsletter submission from a team member
type Submission struct {
	ID         int       `json:"id"`
	UserID     string    `json:"user_id"`
	QuestionID *int      `json:"question_id,omitempty"` // Nullable for general news submissions
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// Question represents a prompt question for newsletter submissions
type Question struct {
	ID         int        `json:"id"`
	Text       string     `json:"text"`
	Category   string     `json:"category"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewsletterIssue represents a generated newsletter
type NewsletterIssue struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ProcessingStatus constants for processed articles
const (
	ProcessingStatusPending    = "pending"
	ProcessingStatusProcessing = "processing"
	ProcessingStatusSuccess    = "success"
	ProcessingStatusFailed     = "failed"
	ProcessingStatusRetry      = "retry"
)

// ValidProcessingStatuses map for validation
var ValidProcessingStatuses = map[string]bool{
	ProcessingStatusPending:    true,
	ProcessingStatusProcessing: true,
	ProcessingStatusSuccess:    true,
	ProcessingStatusFailed:     true,
	ProcessingStatusRetry:      true,
}

// ProcessedArticle represents an AI-processed article from a submission
type ProcessedArticle struct {
	ID                int  `json:"id"`
	SubmissionID      int  `json:"submission_id"`
	NewsletterIssueID *int `json:"newsletter_issue_id,omitempty"`

	// AI Processing data
	JournalistType   string `json:"journalist_type"`
	ProcessedContent string `json:"processed_content"`
	ProcessingPrompt string `json:"processing_prompt"`

	// Template formatting (separate from content)
	TemplateFormat string `json:"template_format"`

	// Manual retry system
	ProcessingStatus string  `json:"processing_status"`
	ErrorMessage     *string `json:"error_message,omitempty"`
	RetryCount       int     `json:"retry_count"`

	// Metadata
	WordCount   int        `json:"word_count"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Validate checks if the ProcessedArticle has valid data
func (pa *ProcessedArticle) Validate() error {
	if !ValidProcessingStatuses[pa.ProcessingStatus] {
		return fmt.Errorf("invalid processing status: %s", pa.ProcessingStatus)
	}

	if pa.SubmissionID <= 0 {
		return fmt.Errorf("submission_id is required")
	}

	if pa.ProcessedContent == "" && pa.ProcessingStatus == ProcessingStatusSuccess {
		return fmt.Errorf("processed_content required for successful articles")
	}

	if pa.JournalistType == "" {
		return fmt.Errorf("journalist_type is required")
	}

	if pa.TemplateFormat == "" {
		return fmt.Errorf("template_format is required")
	}

	return nil
}
