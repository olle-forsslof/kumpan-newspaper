package database

import (
	"encoding/json"
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

// InterviewQuestion represents a Q&A pair in an interview article
type InterviewQuestion struct {
	Question string `json:"q"`
	Answer   string `json:"a"`
}

// ParseJSONContent parses the processed content as JSON
func (pa *ProcessedArticle) ParseJSONContent() (map[string]interface{}, error) {
	var content map[string]interface{}
	if err := json.Unmarshal([]byte(pa.ProcessedContent), &content); err != nil {
		return nil, fmt.Errorf("failed to parse JSON content: %w", err)
	}
	return content, nil
}

// GetHeadline extracts the headline from JSON content
func (pa *ProcessedArticle) GetHeadline() (string, error) {
	content, err := pa.ParseJSONContent()
	if err != nil {
		return "", err
	}

	if headline, ok := content["headline"].(string); ok {
		return headline, nil
	}

	return "", fmt.Errorf("headline not found or not a string")
}

// GetByline extracts the byline from JSON content
func (pa *ProcessedArticle) GetByline() (string, error) {
	content, err := pa.ParseJSONContent()
	if err != nil {
		return "", err
	}

	if byline, ok := content["byline"].(string); ok {
		return byline, nil
	}

	return "", fmt.Errorf("byline not found or not a string")
}

// ParseInterviewQuestions extracts Q&A pairs from interview articles
func (pa *ProcessedArticle) ParseInterviewQuestions() ([]InterviewQuestion, error) {
	content, err := pa.ParseJSONContent()
	if err != nil {
		return nil, err
	}

	questionsInterface, ok := content["questions"]
	if !ok {
		return nil, fmt.Errorf("questions field not found")
	}

	questionsArray, ok := questionsInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("questions field is not an array")
	}

	var questions []InterviewQuestion
	for _, questionInterface := range questionsArray {
		questionMap, ok := questionInterface.(map[string]interface{})
		if !ok {
			continue // Skip invalid question format
		}

		q, qOk := questionMap["q"].(string)
		a, aOk := questionMap["a"].(string)

		if qOk && aOk {
			questions = append(questions, InterviewQuestion{
				Question: q,
				Answer:   a,
			})
		}
	}

	return questions, nil
}

// ValidateJSONContent validates the JSON structure matches the journalist type
func (pa *ProcessedArticle) ValidateJSONContent() error {
	// First run the standard validation
	if err := pa.Validate(); err != nil {
		return err
	}

	// If status is not success, skip JSON validation
	if pa.ProcessingStatus != ProcessingStatusSuccess {
		return nil
	}

	// Validate JSON can be parsed
	content, err := pa.ParseJSONContent()
	if err != nil {
		return fmt.Errorf("invalid JSON content: %w", err)
	}

	// Validate required fields based on journalist type
	requiredFields := getRequiredFieldsForJournalistType(pa.JournalistType)
	for _, field := range requiredFields {
		if _, exists := content[field]; !exists {
			return fmt.Errorf("missing required JSON field for %s journalist: %s", pa.JournalistType, field)
		}

		// Ensure field is not empty string
		if str, ok := content[field].(string); ok && str == "" {
			return fmt.Errorf("required JSON field %s cannot be empty", field)
		}
	}

	return nil
}

// getRequiredFieldsForJournalistType returns required fields for each journalist type
func getRequiredFieldsForJournalistType(journalistType string) []string {
	switch journalistType {
	case "feature":
		return []string{"headline", "lead", "body", "byline"}
	case "interview":
		return []string{"headline", "introduction", "questions", "byline"}
	case "general":
		return []string{"headline", "body", "byline"}
	case "body_mind":
		return []string{"headline", "response", "signoff", "byline"}
	default:
		return []string{"headline", "body", "byline"}
	}
}

// Newsletter automation models for weekly assignment system

// NewsletterIssueStatus represents the status of a newsletter issue
type NewsletterIssueStatus string

const (
	IssueStatusDraft      NewsletterIssueStatus = "draft"
	IssueStatusAssigning  NewsletterIssueStatus = "assigning"
	IssueStatusInProgress NewsletterIssueStatus = "in_progress"
	IssueStatusReady      NewsletterIssueStatus = "ready"
	IssueStatusPublished  NewsletterIssueStatus = "published"
)

// ValidIssueStatuses map for validation
var ValidIssueStatuses = map[NewsletterIssueStatus]bool{
	IssueStatusDraft:      true,
	IssueStatusAssigning:  true,
	IssueStatusInProgress: true,
	IssueStatusReady:      true,
	IssueStatusPublished:  true,
}

// ContentType represents the type of content assignment
type ContentType string

const (
	ContentTypeFeature  ContentType = "feature"
	ContentTypeGeneral  ContentType = "general"
	ContentTypeBodyMind ContentType = "body_mind"
)

// ValidContentTypes map for validation
var ValidContentTypes = map[ContentType]bool{
	ContentTypeFeature:  true,
	ContentTypeGeneral:  true,
	ContentTypeBodyMind: true,
}

// WeeklyNewsletterIssue represents an enhanced newsletter issue for weekly automation
type WeeklyNewsletterIssue struct {
	ID              int                   `json:"id"`
	WeekNumber      int                   `json:"week_number"`
	Year            int                   `json:"year"`
	Title           string                `json:"title"`
	Content         string                `json:"content"`
	Status          NewsletterIssueStatus `json:"status"`
	PublicationDate time.Time             `json:"publication_date"`
	PublishedAt     *time.Time            `json:"published_at,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
}

// PersonAssignment represents a content assignment to a person for a specific week
type PersonAssignment struct {
	ID           int         `json:"id"`
	IssueID      int         `json:"issue_id"`
	PersonID     string      `json:"person_id"` // Slack user ID
	ContentType  ContentType `json:"content_type"`
	QuestionID   *int        `json:"question_id,omitempty"`
	SubmissionID *int        `json:"submission_id,omitempty"`
	AssignedAt   time.Time   `json:"assigned_at"`
	CreatedAt    time.Time   `json:"created_at"`
}

// BodyMindQuestion represents an anonymous wellness question for the pool
type BodyMindQuestion struct {
	ID           int        `json:"id"`
	QuestionText string     `json:"question_text"`
	Category     string     `json:"category"` // wellness, mental_health, work_life_balance
	Status       string     `json:"status"`   // active, used, archived
	CreatedAt    time.Time  `json:"created_at"`
	UsedAt       *time.Time `json:"used_at,omitempty"`
}

// PersonRotationHistory tracks assignment history for intelligent rotation
type PersonRotationHistory struct {
	ID          int         `json:"id"`
	PersonID    string      `json:"person_id"`
	ContentType ContentType `json:"content_type"`
	WeekNumber  int         `json:"week_number"`
	Year        int         `json:"year"`
	CreatedAt   time.Time   `json:"created_at"`
}

// Validate checks if the WeeklyNewsletterIssue has valid data
func (wni *WeeklyNewsletterIssue) Validate() error {
	if !ValidIssueStatuses[wni.Status] {
		return fmt.Errorf("invalid issue status: %s", wni.Status)
	}

	if wni.WeekNumber < 1 || wni.WeekNumber > 53 {
		return fmt.Errorf("invalid week number: %d (must be 1-53)", wni.WeekNumber)
	}

	if wni.Year < 2020 || wni.Year > 2100 {
		return fmt.Errorf("invalid year: %d", wni.Year)
	}

	return nil
}

// Validate checks if the PersonAssignment has valid data
func (pa *PersonAssignment) Validate() error {
	if !ValidContentTypes[pa.ContentType] {
		return fmt.Errorf("invalid content type: %s", pa.ContentType)
	}

	if pa.PersonID == "" {
		return fmt.Errorf("person_id is required")
	}

	if pa.IssueID <= 0 {
		return fmt.Errorf("issue_id is required")
	}

	return nil
}

// Validate checks if the BodyMindQuestion has valid data
func (bmq *BodyMindQuestion) Validate() error {
	if bmq.QuestionText == "" {
		return fmt.Errorf("question_text is required")
	}

	validStatuses := map[string]bool{
		"active":   true,
		"used":     true,
		"archived": true,
	}

	if !validStatuses[bmq.Status] {
		return fmt.Errorf("invalid status: %s", bmq.Status)
	}

	validCategories := map[string]bool{
		"wellness":          true,
		"mental_health":     true,
		"work_life_balance": true,
	}

	if !validCategories[bmq.Category] {
		return fmt.Errorf("invalid category: %s", bmq.Category)
	}

	return nil
}
