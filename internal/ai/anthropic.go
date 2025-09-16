package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
)

// AnthropicService implements the AIService interface using Anthropic's Claude API
type AnthropicService struct {
	client     anthropic.Client
	apiKey     string
	maxRetries int
	timeout    time.Duration
	model      anthropic.Model
}

// NewAnthropicService creates a new Anthropic AI service
func NewAnthropicService(apiKey string) *AnthropicService {
	client := anthropic.NewClient()

	return &AnthropicService{
		client:     client,
		apiKey:     apiKey,
		maxRetries: 3,
		timeout:    30 * time.Second,
		model:      anthropic.ModelClaude3_7SonnetLatest,
	}
}

// ProcessSubmission transforms a submission into a processed article using Claude
func (a *AnthropicService) ProcessSubmission(ctx context.Context, submission database.Submission, journalistType string) (*database.ProcessedArticle, error) {
	// Validate journalist type
	if !a.ValidateJournalistType(journalistType) {
		return nil, NewProcessingError("invalid_journalist_type",
			fmt.Sprintf("invalid journalist type: %s", journalistType), false, nil)
	}

	// Get journalist profile
	profile, err := GetJournalistProfile(journalistType)
	if err != nil {
		return nil, NewProcessingError("profile_error", "failed to get journalist profile", false, err)
	}

	// Build the prompt
	prompt, err := BuildPrompt(submission.Content, journalistType)
	if err != nil {
		return nil, NewProcessingError("prompt_error", "failed to build prompt", false, err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Call Anthropic API
	response, err := a.callAnthropicAPI(ctx, prompt)
	if err != nil {
		return nil, err // Already wrapped as ProcessingError
	}

	// Process the response
	processedContent := strings.TrimSpace(response.ProcessedContent)
	wordCount := countWords(processedContent)

	// Validate response length
	if wordCount > profile.MaxWords+50 { // Allow 50 word buffer
		return nil, NewProcessingError("content_too_long",
			fmt.Sprintf("generated content exceeds maximum words: %d > %d", wordCount, profile.MaxWords),
			true, nil)
	}

	if len(processedContent) < 10 {
		return nil, NewProcessingError("content_too_short",
			"generated content is too short", true, nil)
	}

	// Create processed article
	now := time.Now()
	article := &database.ProcessedArticle{
		SubmissionID:     submission.ID,
		JournalistType:   journalistType,
		ProcessedContent: processedContent,
		ProcessingPrompt: prompt,
		TemplateFormat:   profile.TemplateFormat,
		ProcessingStatus: database.ProcessingStatusSuccess,
		WordCount:        wordCount,
		ProcessedAt:      &now,
		RetryCount:       0,
	}

	return article, nil
}

// ProcessSubmissionWithUserInfo transforms a submission with user context into structured JSON article
func (a *AnthropicService) ProcessSubmissionWithUserInfo(ctx context.Context, submission database.Submission, authorName, authorDepartment, journalistType string) (*database.ProcessedArticle, error) {
	// Validate journalist type
	if !a.ValidateJournalistType(journalistType) {
		return nil, NewProcessingError("invalid_journalist_type",
			fmt.Sprintf("invalid journalist type: %s", journalistType), false, nil)
	}

	// Get journalist profile
	profile, err := GetJournalistProfile(journalistType)
	if err != nil {
		return nil, NewProcessingError("profile_error", "failed to get journalist profile", false, err)
	}

	// Build the JSON prompt with user information
	prompt, err := BuildJSONPrompt(submission.Content, authorName, authorDepartment, journalistType)
	if err != nil {
		return nil, NewProcessingError("prompt_error", "failed to build JSON prompt", false, err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Call Anthropic API
	response, err := a.callAnthropicAPI(ctx, prompt)
	if err != nil {
		return nil, err // Already wrapped as ProcessingError
	}

	// Process the JSON response
	processedContent := strings.TrimSpace(response.ProcessedContent)

	// Parse and validate the JSON response
	parsedResponse, err := ParseJSONResponse(processedContent, journalistType)
	if err != nil {
		return nil, NewProcessingError("invalid_json_response",
			"AI response is not valid JSON", true, err)
	}

	// Validate response length
	if parsedResponse.WordCount > profile.MaxWords+50 { // Allow 50 word buffer
		return nil, NewProcessingError("content_too_long",
			fmt.Sprintf("generated content exceeds maximum words: %d > %d", parsedResponse.WordCount, profile.MaxWords),
			true, nil)
	}

	if parsedResponse.WordCount < 5 {
		return nil, NewProcessingError("content_too_short",
			"generated content is too short", true, nil)
	}

	// Create processed article with JSON content
	now := time.Now()
	article := &database.ProcessedArticle{
		SubmissionID:     submission.ID,
		JournalistType:   journalistType,
		ProcessedContent: processedContent, // Store the raw JSON string
		ProcessingPrompt: prompt,
		TemplateFormat:   profile.TemplateFormat,
		ProcessingStatus: database.ProcessingStatusSuccess,
		WordCount:        parsedResponse.WordCount,
		ProcessedAt:      &now,
		RetryCount:       0,
	}

	return article, nil
}

// ProcessAndSaveSubmission processes a submission with AI and saves the result atomically to database
func (a *AnthropicService) ProcessAndSaveSubmission(
	ctx context.Context,
	db *database.DB,
	submission database.Submission,
	authorName, authorDepartment, journalistType string,
	newsletterIssueID *int,
) error {
	// First, process the submission using existing logic
	processedArticle, err := a.ProcessSubmissionWithUserInfo(ctx, submission, authorName, authorDepartment, journalistType)
	if err != nil {
		return fmt.Errorf("AI processing failed: %w", err)
	}

	// Set the newsletter issue ID for auto-assignment
	processedArticle.NewsletterIssueID = newsletterIssueID

	// Save the processed article to database atomically
	articleID, err := db.CreateProcessedArticle(*processedArticle)
	if err != nil {
		return fmt.Errorf("database save failed: %w", err)
	}

	// Update the in-memory object with the database ID
	processedArticle.ID = articleID

	slog.Info("ProcessAndSaveSubmission completed successfully",
		"submission_id", submission.ID,
		"processed_article_id", articleID,
		"newsletter_issue_id", newsletterIssueID,
		"journalist_type", journalistType,
		"word_count", processedArticle.WordCount)

	return nil
}

// callAnthropicAPI makes the actual API call with proper error handling
func (a *AnthropicService) callAnthropicAPI(ctx context.Context, prompt string) (*ProcessingResult, error) {
	response, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 600, // Reasonable limit for newsletter articles
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, a.handleAnthropicError(err)
	}

	// Extract content from response
	var content string
	if len(response.Content) > 0 {
		// Get the first content block and extract text
		contentBlock := response.Content[0]
		if textBlock := contentBlock.AsText(); textBlock.Type == "text" {
			content = textBlock.Text
		}
	}

	if content == "" {
		return nil, NewProcessingError("empty_response", "received empty response from API", true, nil)
	}

	return &ProcessingResult{
		ProcessedContent: content,
		WordCount:        countWords(content),
		ProcessingTime:   0, // Will be calculated by caller
		TokensUsed:       int(response.Usage.OutputTokens + response.Usage.InputTokens),
		Model:            string(a.model),
	}, nil
}

// handleAnthropicError converts Anthropic API errors to ProcessingError
func (a *AnthropicService) handleAnthropicError(err error) *ProcessingError {
	// Check for specific error types
	if strings.Contains(err.Error(), "rate_limit") {
		return NewProcessingError("rate_limit", "API rate limit exceeded", true, err)
	}
	if strings.Contains(err.Error(), "timeout") {
		return NewProcessingError("timeout", "API request timed out", true, err)
	}
	if strings.Contains(err.Error(), "content_filter") {
		return NewProcessingError("content_filter", "Content filtered by API", false, err)
	}
	if strings.Contains(err.Error(), "authentication") {
		return NewProcessingError("api_error", "Authentication failed", false, err)
	}

	// Generic API error
	return NewProcessingError("api_error", "API request failed", true, err)
}

// GetAvailableJournalists returns available journalist types
func (a *AnthropicService) GetAvailableJournalists() []string {
	return GetAvailableJournalistTypes()
}

// ValidateJournalistType checks if a journalist type is valid
func (a *AnthropicService) ValidateJournalistType(journalistType string) bool {
	return ValidateJournalistType(journalistType)
}

// GetJournalistProfile returns the profile for a journalist type
func (a *AnthropicService) GetJournalistProfile(journalistType string) (*JournalistProfile, error) {
	return GetJournalistProfile(journalistType)
}

// countWords provides a simple word count implementation
func countWords(text string) int {
	if text == "" {
		return 0
	}

	// Split by whitespace and count non-empty parts
	words := strings.Fields(text)
	return len(words)
}
