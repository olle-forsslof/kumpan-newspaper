// Package slack provides functionality for interacting with the Slack API.
// It offers tools for creating and managing Slack bots, handling slash commands,
// and other Slack-related operations.
package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/olle-forsslof/kumpan-newspaper/internal/ai"
	"github.com/olle-forsslof/kumpan-newspaper/internal/database"
	"github.com/slack-go/slack"
)

type slackBot struct {
	client            *slack.Client
	config            SlackConfig
	adminHandler      *AdminHandler
	submissionManager SubmissionManager
	aiProcessor       AIProcessor
	questionSelector  QuestionSelector
	db                DatabaseInterface // Add database interface for testing
}

type QuestionSelector interface {
	SelectNextQuestion(ctx context.Context, category string) (*database.Question, error)
	MarkQuestionUsed(ctx context.Context, questionID int) error
	GetQuestionsByCategory(ctx context.Context, category string) ([]database.Question, error)
	AddQuestion(ctx context.Context, text, category string) (*database.Question, error)
	GetQuestionByID(ctx context.Context, questionID int) (*database.Question, error)
}

type SubmissionManager interface {
	CreateNewsSubmission(ctx context.Context, userID, content string) (*database.Submission, error)
	GetSubmissionsByUser(ctx context.Context, userID string) ([]database.Submission, error)
	GetAllSubmissions(ctx context.Context) ([]database.Submission, error)
}

func NewBot(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string) Bot {
	// Don't initialize the client immediately - only when needed
	return &slackBot{
		client:            nil, // Initialize as nil
		config:            cfg,
		adminHandler:      NewAdminHandler(questionSelector, adminUsers),
		submissionManager: nil, // No submission manager for basic bot
		aiProcessor:       nil, // No AI processor for basic bot
		questionSelector:  questionSelector,
	}
}

// NewBotWithSubmissions creates a bot with news submission storage capabilities
func NewBotWithSubmissions(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      NewAdminHandler(questionSelector, adminUsers),
		submissionManager: submissionManager,
		aiProcessor:       nil,
		questionSelector:  questionSelector,
	}
}

// NewBotWithAIProcessing creates a bot with automatic AI processing capabilities
func NewBotWithAIProcessing(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager, aiProcessor AIProcessor) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      NewAdminHandler(questionSelector, adminUsers),
		submissionManager: submissionManager,
		aiProcessor:       aiProcessor,
		questionSelector:  questionSelector,
	}
}

// NewBotWithWeeklyAutomation creates a bot with full weekly automation capabilities
func NewBotWithWeeklyAutomation(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager, aiProcessor AIProcessor, db *database.DB) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      NewAdminHandlerWithWeeklyAutomation(questionSelector, adminUsers, submissionManager, db, cfg.Token),
		submissionManager: submissionManager,
		aiProcessor:       aiProcessor,
		questionSelector:  questionSelector,
		db:                db, // Store database reference
	}
}

// NewBotWithDatabase creates a bot with database capabilities for testing
func NewBotWithDatabase(cfg SlackConfig, questionSelector QuestionSelector, adminUsers []string, submissionManager SubmissionManager, aiProcessor AIProcessor, db DatabaseInterface) Bot {
	return &slackBot{
		client:            nil,
		config:            cfg,
		adminHandler:      nil, // Not needed for basic testing
		submissionManager: submissionManager,
		aiProcessor:       aiProcessor,
		questionSelector:  questionSelector,
		db:                db,
	}
}

func (b *slackBot) SendMessage(ctx context.Context, channelID, text string) error {
	// Initialize client only when actually needed
	if b.client == nil {
		b.client = slack.New(b.config.Token)
	}

	_, _, err := b.client.PostMessageContext(ctx, channelID,
		slack.MsgOptionText(text, false))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
}

func (b *slackBot) HandleSlashCommand(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	// Handle empty commands or help requests
	if cmd.Text == "" || cmd.Text == "help" {
		return b.handleRegularHelp(), nil
	}

	// Handle admin commands
	if strings.HasPrefix(cmd.Text, "admin ") {
		adminCmd, err := parseAdminCommand(cmd.Text)
		if err != nil {
			return &SlashCommandResponse{
				Text:         "Invalid admin command format. Type 'admin help' for admin usage or just 'help' for regular commands.",
				ResponseType: "ephemeral",
			}, nil
		}

		return b.adminHandler.HandleAdminCommand(ctx, cmd.UserID, adminCmd)
	}

	// Handle news story submissions for regular users
	if strings.HasPrefix(cmd.Text, "submit ") {
		return b.handleNewsSubmission(ctx, cmd)
	}

	// Handle regular newsletter functionality
	return &SlashCommandResponse{
		Text:         fmt.Sprintf("I received: '%s'\n\nFor help with commands, type `help`\nFor admin commands, type `admin help`", cmd.Text),
		ResponseType: "ephemeral",
	}, nil
}

func (b *slackBot) HandleEventCallback(ctx context.Context, event SlackEvent) error {
	// skip messages from bots to avoid infinite loops
	if event.BotID != "" {
		return nil
	}

	// for now echo the message
	if event.Type == "message" && event.Text != "" {
		return b.SendMessage(ctx, event.Channel, fmt.Sprintf("You said %s", event.Text))
	}

	return nil
}

func (b *slackBot) handleRegularHelp() *SlashCommandResponse {
	help := "*Newsletter Bot Help*\n\n" +
		"This bot helps manage daily/weekly newsletter questions and collect news stories.\n\n" +
		"*Available Commands:*\n" +
		"• `help` - Show this help message\n" +
		"• `submit [your news story]` - Submit a news story for the newsletter\n" +
		"• `admin help` - Show admin commands (authorized users only)\n\n" +
		"*Examples:*\n" +
		"• `submit Check out this cool new Go library: https://github.com/example/repo`\n" +
		"• `submit Our team shipped the new user dashboard this week!`\n\n" +
		"*For Admins:*\n" +
		"Admin users can manage newsletter questions, view submissions, and configure the bot."

	return &SlashCommandResponse{
		Text:         help,
		ResponseType: "ephemeral",
	}
}

func (b *slackBot) GetUserInfo(ctx context.Context, userID string) (*UserInfo, error) {
	// Initialize client only when actually needed
	if b.client == nil {
		b.client = slack.New(b.config.Token)
	}

	user, err := b.client.GetUserInfoContext(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	return &UserInfo{
		ID:       user.ID,
		Name:     user.Name,
		RealName: user.RealName,
		Profile: UserProfile{
			Email:     user.Profile.Email,
			Title:     user.Profile.Title,
			RealName:  user.Profile.RealName,
			FirstName: user.Profile.FirstName,
			LastName:  user.Profile.LastName,
		},
	}, nil
}

func (b *slackBot) EnrichSubmissionWithUserInfo(ctx context.Context, userID, content string) (*EnrichedSubmission, error) {
	userInfo, err := b.GetUserInfo(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Use title as department, fallback to email domain if title is empty
	department := userInfo.Profile.Title
	if department == "" && userInfo.Profile.Email != "" {
		// Extract domain from email as fallback department info
		if atIndex := strings.Index(userInfo.Profile.Email, "@"); atIndex > 0 {
			department = strings.Split(userInfo.Profile.Email[atIndex+1:], ".")[0]
		}
	}

	return &EnrichedSubmission{
		UserID:           userID,
		Content:          content,
		AuthorName:       userInfo.RealName,
		AuthorEmail:      userInfo.Profile.Email,
		AuthorDepartment: department,
	}, nil
}

func (b *slackBot) handleNewsSubmission(ctx context.Context, cmd SlashCommand) (*SlashCommandResponse, error) {
	// Extract the news content (everything after "submit ")
	newsContent := strings.TrimSpace(strings.TrimPrefix(cmd.Text, "submit "))

	if newsContent == "" {
		return &SlashCommandResponse{
			Text:         "Please provide some content for your news submission.\n\nExample: `submit Our team launched a new feature this week!`",
			ResponseType: "ephemeral",
		}, nil
	}

	var responseText string
	var submission *database.Submission

	// Store the news submission in database if SubmissionManager is available
	if b.submissionManager != nil {
		var err error
		submission, err = b.submissionManager.CreateNewsSubmission(ctx, cmd.UserID, newsContent)
		if err != nil {
			return &SlashCommandResponse{
				Text:         fmt.Sprintf("❌ Failed to store your submission: %v", err),
				ResponseType: "ephemeral",
			}, nil
		}
		responseText = fmt.Sprintf("📰 *News submission received!*\n\n> %s\n\n", newsContent)
	} else {
		responseText = fmt.Sprintf("📰 *News submission received!*\n\n> %s\n\n", newsContent)
	}

	// Launch async AI processing if AIProcessor is available
	if b.aiProcessor != nil && submission != nil {
		responseText += "🤖 Processing with AI in the background...\n"

		// Launch goroutine for async processing
		go b.processSubmissionAsync(context.Background(), *submission, cmd.UserID, cmd.ResponseURL)
	}

	responseText += "✅ Thanks for contributing!"

	return &SlashCommandResponse{
		Text:         responseText,
		ResponseType: "ephemeral",
	}, nil
}

// determineJournalistTypeFromSubmission determines journalist type based on question category
func (b *slackBot) determineJournalistTypeFromSubmission(ctx context.Context, submission *database.Submission) string {
	// If no question ID, this is a general news submission
	if submission.QuestionID == nil {
		return "general"
	}

	// Get the question to find its category
	if b.questionSelector != nil {
		question, err := b.questionSelector.GetQuestionByID(ctx, *submission.QuestionID)
		if err != nil {
			// If we can't get the question, default to general
			return "general"
		}

		// Map question category to journalist type using existing mapping
		return ai.GetJournalistTypeForCategory(question.Category)
	}

	// Fallback to general if no question selector available
	return "general"
}

// determineJournalistType is deprecated - use determineJournalistTypeFromSubmission instead

// processSubmissionAsync handles AI processing in the background
func (b *slackBot) processSubmissionAsync(ctx context.Context, submission database.Submission, userID string, responseURL string) {
	// Log start of processing
	slog.Info("Starting async AI processing",
		"submission_id", submission.ID,
		"user_id", userID)

	// Get user information for enriched processing
	enrichedSubmission, err := b.EnrichSubmissionWithUserInfo(ctx, userID, submission.Content)

	// Use fallback user info if enrichment fails
	authorName := "Team Member"
	authorDepartment := "Unknown"

	if err != nil {
		slog.Warn("Using fallback user info for async processing",
			"error", err,
			"submission_id", submission.ID)
	} else {
		authorName = enrichedSubmission.AuthorName
		authorDepartment = enrichedSubmission.AuthorDepartment
		slog.Info("Successfully enriched submission with user info",
			"author_name", authorName,
			"author_department", authorDepartment,
			"submission_id", submission.ID)
	}

	// Determine journalist type from question category
	journalistType := b.determineJournalistTypeFromSubmission(ctx, &submission)

	// Get current newsletter issue for auto-assignment
	var newsletterIssueID *int
	if b.db != nil {
		now := time.Now()
		year, week := now.ISOWeek()

		issue, err := b.db.GetOrCreateWeeklyIssue(week, year)
		if err != nil {
			slog.Error("Failed to get/create weekly newsletter issue for auto-assignment",
				"error", err,
				"week", week,
				"year", year,
				"submission_id", submission.ID)
			// Continue without newsletter assignment
		} else {
			newsletterIssueID = &issue.ID
			slog.Info("Retrieved newsletter issue for auto-assignment",
				"newsletter_issue_id", issue.ID,
				"week", week,
				"year", year)
		}
	}

	// Process with AI and save to database atomically using new architecture
	err = b.aiProcessor.ProcessAndSaveSubmission(
		ctx,
		b.db,              // Database connection
		submission,        // Submission to process
		authorName,        // Author name
		authorDepartment,  // Author department
		journalistType,    // Journalist type
		newsletterIssueID, // Newsletter issue ID for auto-assignment
	)

	if err != nil {
		// Log error - processing failed
		slog.Error("ProcessAndSaveSubmission failed",
			"error", err,
			"submission_id", submission.ID,
			"journalist_type", journalistType)

		// Send failure notification to user via response_url
		b.sendFollowupMessage(responseURL, fmt.Sprintf("❌ AI processing failed: %v", err))
		return
	}

	// Success! Article has been processed AND saved to database with newsletter assignment
	slog.Info("ProcessAndSaveSubmission completed successfully",
		"submission_id", submission.ID,
		"journalist_type", journalistType,
		"newsletter_issue_id", newsletterIssueID)

	// Send success notification to user
	message := fmt.Sprintf("🤖 ✅ Your submission has been processed by our %s journalist and added to the newsletter!\n\n_Processing completed in the background_",
		journalistType)

	b.sendFollowupMessage(responseURL, message)
}

// sendFollowupMessage sends a follow-up message to Slack using the response_url
func (b *slackBot) sendFollowupMessage(responseURL string, message string) {
	if responseURL == "" {
		slog.Warn("No response URL provided for follow-up message")
		return
	}

	// Log that we're sending a follow-up (in production this would make actual HTTP request)
	slog.Info("Sending follow-up message to Slack",
		"response_url_provided", true,
		"message_length", len(message))

	// TODO: Implement actual HTTP POST to responseURL with message payload
	// For now, just log the message that would be sent
	slog.Info("Follow-up message content", "message", message)
}
